// 第二批设计探针：验证 SQL 约束/过滤与版本协议，不代替领域服务验收。
package cmdesignprobe

import (
	"database/sql"
	"os"
	"reflect"
	"sort"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, func(string) error { return nil }) }
func setup(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, key := range []string{"CREATIVE_MODULE_SCHEMA", "CREATIVE_LIBRARY_CANVAS_SCHEMA"} {
		b, e := os.ReadFile(os.Getenv(key))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(b)); e != nil {
			t.Fatal(e)
		}
	}
	return db
}
func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func TestLibraryFilterSQL(t *testing.T) {
	db := setup(t)
	exec(t, db, `INSERT INTO creative_assets(id,account_id,kind,title,normalized_title,content_id,content_revision_id,is_favorite,deleted_at,created_at) VALUES
 ('a1','a','image','海风 100%','海风 100%','c1','r1',true,NULL,'2026-09-01'),
 ('a2','a','text','blue_line','blue_line','c2','r2',true,NULL,'2026-09-02'),
 ('a3','a','image','海风 1000','海风 1000','c3','r3',false,NULL,'2026-09-03'),
 ('a4','a','image','trash','trash','c4','r4',true,now(),'2026-09-04'),
 ('b1','b','image','海风','海风','cb','rb',false,NULL,'2026-09-05')`)
	exec(t, db, `INSERT INTO creative_asset_search SELECT account_id,id,normalized_title,revision FROM creative_assets`)
	exec(t, db, `INSERT INTO creative_asset_groups(id,account_id,parent_id,name,position) VALUES('g1','a',NULL,'parent',0),('g2','a','g1','child',0),('gb','b',NULL,'other',0)`)
	exec(t, db, `INSERT INTO creative_asset_group_members(account_id,asset_id,group_id) VALUES('a','a1','g1'),('a','a1','g2'),('a','a3','g2'),('a','a4','g1'),('b','b1','gb')`)
	exec(t, db, `INSERT INTO creative_tags(id,account_id,name,normalized_name,color) VALUES('t1','a','自然光','自然光','#123456'),('t2','a','海','海','#234567'),('tb','b','海','海','#345678')`)
	exec(t, db, `INSERT INTO creative_asset_tags VALUES('a','a1','t1'),('a','a1','t2'),('a','a2','t1'),('a','a3','t2'),('a','a4','t1'),('b','b1','tb')`)
	q, err := os.ReadFile(os.Getenv("CREATIVE_LIBRARY_QUERY"))
	if err != nil {
		t.Fatal(err)
	}
	query := func(account, tags, mode, view string, group, pattern any, desc bool) []string {
		t.Helper()
		rows, e := db.Query(string(q), account, tags, mode, pattern, view, group, desc, nil, nil, nil, 100)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		got := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				t.Fatal(e)
			}
			got = append(got, id)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		return got
	}
	cases := []struct {
		name, tags, mode, view string
		group, pattern         any
		desc                   bool
		want                   []string
	}{
		{"all-active", "{}", "all", "all", nil, nil, false, []string{"a3", "a2", "a1"}},
		{"tag-all", "{t1,t2}", "all", "all", nil, nil, false, []string{"a1"}},
		{"tag-any", "{t2}", "any", "all", nil, nil, false, []string{"a3", "a1"}},
		{"favorites", "{}", "all", "favorites", nil, nil, false, []string{"a2", "a1"}},
		{"unclassified-ignores-favorite", "{}", "all", "unclassified", nil, nil, false, []string{"a2"}},
		{"parent-direct", "{}", "all", "all", "g1", nil, false, []string{"a1"}},
		{"subtree-no-duplicates", "{}", "all", "all", "g1", nil, true, []string{"a3", "a1"}},
		{"trash", "{}", "all", "trash", nil, nil, false, []string{"a4"}},
		{"literal-percent", "{}", "all", "all", nil, `%100\%%`, false, []string{"a1"}},
		{"literal-underscore", "{}", "all", "all", nil, `%\_%`, false, []string{"a2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := query("a", c.tags, c.mode, c.view, c.group, c.pattern, c.desc); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
	if got := query("b", "{}", "all", "all", nil, nil, false); !reflect.DeepEqual(got, []string{"b1"}) {
		t.Fatal(got)
	}
	exec(t, db, `UPDATE creative_tags SET name='落日',normalized_name='落日' WHERE account_id='a' AND id='t1'`)
	got := query("a", "{}", "all", "all", nil, "%落日%", false)
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"a1", "a2"}) {
		t.Fatal(got)
	}
}
func TestGraphSingleRowConstraints(t *testing.T) {
	db := setup(t)
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("FK=%d %v", count, err)
	}
	exec(t, db, `INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES('c1','a','p1','one')`)
	if _, err := db.Exec(`INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES('c2','a','p1','two')`); err == nil {
		t.Fatal("two default canvases accepted")
	}
	exec(t, db, `INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,type_version,x,y,width,height) VALUES('valid','a','c1','core.image',1,-10,20,200,300)`)
	for _, v := range []string{"NaN", "Infinity", "-Infinity"} {
		if _, err := db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,type_version,x,y,width,height) VALUES($1,'a','c1','core.image',1,$2::float8,0,100,100)`, v, v); err == nil {
			t.Fatalf("invalid x %s accepted", v)
		}
	}
	if _, err := db.Exec(`INSERT INTO creative_node_inputs(id,account_id,canvas_id,node_id,slot_key,position,source_node_id,content_id,content_revision_id,role) VALUES('i1','a','c1','n1','style',0,'n2','ct1','r1','style')`); err == nil {
		t.Fatal("ambiguous source accepted")
	}
	exec(t, db, `INSERT INTO creative_node_inputs(id,account_id,canvas_id,node_id,slot_key,position,content_id,content_revision_id,role) VALUES('i2','a','c1','n1','style',0,'ct1','r1','style')`)
	if _, err := db.Exec(`INSERT INTO creative_edges(id,account_id,canvas_id,source_node_id,source_port,target_node_id,target_port,role,target_ordinal) VALUES('e1','a','c1','n1','out','n1','in','reference',0)`); err == nil {
		t.Fatal("self reference accepted")
	}
}
func TestGraphIdentityRestoreProtocol(t *testing.T) {
	db := setup(t)
	exec(t, db, `INSERT INTO creative_graph_identities(object_id,account_id,canvas_id,object_kind,is_live,last_placement_revision,last_data_revision) VALUES('n1','a','c1','node',true,3,5)`)
	exec(t, db, `INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,type_version,x,y,width,height,placement_revision,data_revision) VALUES('n1','a','c1','core.text',1,0,0,100,100,3,5)`)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM creative_nodes WHERE account_id='a' AND id='n1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE creative_graph_identities SET is_live=false WHERE account_id='a' AND object_id='n1'`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_graph_identities(object_id,account_id,canvas_id,object_kind,is_live) VALUES('n1','a','c1','node',true)`); err == nil {
		t.Fatal("reused identity accepted")
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var placement, data int64
	if err = tx.QueryRow(`UPDATE creative_graph_identities SET is_live=true,last_placement_revision=last_placement_revision+1,last_data_revision=last_data_revision+1 WHERE account_id='a' AND object_id='n1' AND NOT is_live RETURNING last_placement_revision,last_data_revision`).Scan(&placement, &data); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,type_version,x,y,width,height,placement_revision,data_revision) VALUES('n1','a','c1','core.text',1,0,0,100,100,$1,$2)`, placement, data); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`UPDATE creative_nodes SET title='stale' WHERE account_id='a' AND id='n1' AND data_revision=5`)
	if err != nil {
		t.Fatal(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n != 0 {
		t.Fatalf("stale affected=%d: %v", n, err)
	}
	if placement != 4 || data != 6 {
		t.Fatalf("versions %d %d", placement, data)
	}
}

func TestUndoStateAndReceiptProtocol(t *testing.T) {
	db := setup(t)
	exec(t, db, `INSERT INTO creative_graph_identities(object_id,account_id,canvas_id,object_kind,is_live,effect_heads) VALUES('n1','a','c1','node',true,jsonb_build_object('data',jsonb_build_object('state_token','seed','value_hash',md5('seed'))))`)
	exec(t, db, `INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,type_version,x,y,width,height,title) VALUES('n1','a','c1','core.text',1,0,0,100,100,'seed')`)
	// 只验证单字段组状态承接的 SQL 原型；不是生产 Undo 实现。
	apply := func(expectedToken string, expectedVersion int64, nextValue, nextToken string) (int64, error) {
		var version int64
		err := db.QueryRow(`WITH locked AS MATERIALIZED (
    SELECT n.data_revision,n.title,i.effect_heads FROM creative_nodes n JOIN creative_graph_identities i ON i.account_id=n.account_id AND i.object_id=n.id
    WHERE n.account_id='a' AND n.id='n1' FOR UPDATE OF n,i
   ), changed AS (
    UPDATE creative_nodes n SET title=$3,data_revision=n.data_revision+1 FROM locked l
    WHERE n.account_id='a' AND n.id='n1' AND l.data_revision=$2
    AND l.effect_heads#>>'{data,state_token}'=$1 AND l.effect_heads#>>'{data,value_hash}'=md5(l.title)
    RETURNING n.data_revision
   ) UPDATE creative_graph_identities i SET last_data_revision=c.data_revision,
    effect_heads=jsonb_build_object('data',jsonb_build_object('state_token',$4::text,'value_hash',md5($3::text)))
    FROM changed c WHERE i.account_id='a' AND i.object_id='n1' RETURNING c.data_revision`, expectedToken, expectedVersion, nextValue, nextToken).Scan(&version)
		return version, err
	}
	steps := []struct {
		from         string
		version      int64
		value, token string
	}{
		{"seed", 1, "A", "a"}, {"a", 2, "B", "b"},
		{"b", 3, "A", "a"}, {"a", 4, "seed", "seed"},
		{"seed", 5, "A", "a"}, {"a", 6, "B", "b"},
		{"b", 7, "A", "manual"},
	}
	for _, s := range steps {
		version, err := apply(s.from, s.version, s.value, s.token)
		if err != nil || version != s.version+1 {
			t.Fatalf("step %v -> %d: %v", s, version, err)
		}
	}
	if _, err := apply("a", 8, "seed", "seed"); err != sql.ErrNoRows {
		t.Fatalf("manual same-value write incorrectly treated as undo: %v", err)
	}
	// A 的回执固定对象版本8；随后外部编辑到9，不能拿新快照把依赖改为9。
	exec(t, db, `INSERT INTO creative_operation_receipts(account_id,operation_id,operation_type,client_created_at,request_hash,http_status,response,result_kind,result_id,result_revision,retained_until)
 VALUES('a','op1','canvas_commands',now(),repeat('a',64),200,'{"before_topology_revision":"7","result_topology_revision":"8","object_results":[{"id":"n1","kind":"node","is_live":true,"data_revision":"8","placement_revision":"1"}]}','canvas','c1',8,now()+interval '90 days')`)
	if _, err := apply("manual", 8, "external", "external"); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `INSERT INTO creative_canvases(id,account_id,project_id,name,topology_revision) VALUES('c1','a','p1','canvas',8)`)
	var receiptVersion, currentVersion int64
	if err := db.QueryRow(`SELECT (r.response#>>'{object_results,0,data_revision}')::bigint,n.data_revision FROM creative_operation_receipts r JOIN creative_nodes n ON n.account_id=r.account_id AND n.id='n1' WHERE r.account_id='a' AND r.operation_id='op1'`).Scan(&receiptVersion, &currentVersion); err != nil {
		t.Fatal(err)
	}
	if receiptVersion != 8 || currentVersion != 9 {
		t.Fatalf("receipt/current=%d/%d", receiptVersion, currentVersion)
	}
	var beforeTopology, resultTopology, currentTopology int64
	readTopology := func() {
		t.Helper()
		if err := db.QueryRow(`SELECT (r.response->>'before_topology_revision')::bigint,(r.response->>'result_topology_revision')::bigint,c.topology_revision FROM creative_operation_receipts r JOIN creative_canvases c ON c.account_id=r.account_id AND c.id=r.result_id WHERE r.account_id='a' AND r.operation_id='op1'`).Scan(&beforeTopology, &resultTopology, &currentTopology); err != nil {
			t.Fatal(err)
		}
	}
	readTopology()
	if beforeTopology != 7 || resultTopology != 8 || currentTopology != 8 {
		t.Fatalf("topology chain %d/%d/%d", beforeTopology, resultTopology, currentTopology)
	}
	// 无关内容改变未改变拓扑；外部结构变化后，回执仍固定8而快照为9。
	exec(t, db, `UPDATE creative_canvases SET topology_revision=9 WHERE account_id='a' AND id='c1'`)
	readTopology()
	if resultTopology != 8 || currentTopology != 9 {
		t.Fatalf("topology result/snapshot %d/%d", resultTopology, currentTopology)
	}
	if _, err := apply("manual", receiptVersion, "draft", "draft"); err != sql.ErrNoRows {
		t.Fatalf("stale dependent draft accepted: %v", err)
	}
}
