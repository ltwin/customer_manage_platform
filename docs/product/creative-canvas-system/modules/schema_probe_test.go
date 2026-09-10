// 设计探针：由 run-schema-probe.py 临时复制到 backend/internal 后执行。
// 只验证草案 DDL 与 SQL 锁协议，不代替尚未实现的领域服务测试。
package cmdesignprobe

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

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
	body, err := os.ReadFile(os.Getenv("CREATIVE_MODULE_SCHEMA"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSchemaConstraints(t *testing.T) {
	db := setup(t)
	var count int
	if err := db.QueryRow("SELECT count(*) FROM pg_constraint WHERE contype='f' AND connamespace='public'::regnamespace").Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign keys: %d, %v", count, err)
	}
	if err := db.QueryRow("SELECT count(*) FROM pg_tables WHERE schemaname='public'").Scan(&count); err != nil || count != 13 {
		t.Fatalf("tables: %d, %v", count, err)
	}
	// 原始 SQL 可以插入悬空 key：这正是领域守卫必须承担的责任。
	const rev = `INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id) VALUES('r1','a','absent-content',1,1,'{}','absent-rights')`
	if _, err := db.Exec(rev); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id) VALUES('r2','a','absent-content',1,1,'{}','absent-rights')`); err == nil {
		t.Fatal("duplicate sequence accepted")
	}
	if _, err := db.Exec(`INSERT INTO creative_upload_candidates(id,account_id,upload_id,target_snapshot,expires_at) VALUES('c1','a','u1','{}',now()+interval '1 day')`); err == nil {
		t.Fatal("pending candidate without revision accepted")
	}
	if _, err := db.Exec(`INSERT INTO creative_media_quotas(account_id,limit_bytes,reserved_bytes,stored_bytes) VALUES('a',10,8,3)`); err == nil {
		t.Fatal("over quota accepted")
	}
	if _, err := db.Exec(`INSERT INTO creative_uploads(id,account_id,operation_id,publish_operation_id,rights_declaration_id,state,declared_kind,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot,blob_id,handed_off_at,published_blob_id_snapshot,publication_result_kind,publication_result_id) VALUES('u1','a','o1','publish1','rights1','ready','image',1,1,'a.jpg','creative-v2/a/staging/u1',now()+interval '1 day','node','{}','b1',now(),'b1','node','node1')`); err == nil {
		t.Fatal("ready upload retaining blob accepted")
	}
	if _, err := db.Exec(`INSERT INTO creative_uploads(id,account_id,operation_id,publish_operation_id,rights_declaration_id,state,declared_kind,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot,blob_id,handed_off_at,published_blob_id_snapshot,publication_result_kind,publication_result_id) VALUES('u2','a','o2','publish2','rights1','ready','image',1,1,'a.jpg','creative-v2/a/staging/u2',now()+interval '1 day','node','{}',NULL,now(),'b1','node','node1')`); err != nil {
		t.Fatalf("valid ready upload rejected: %v", err)
	}
}

func TestReferenceLockProtocol(t *testing.T) {
	db := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `INSERT INTO creative_content_revisions(id,account_id,content_id,sequence,schema_version,payload,rights_declaration_id) VALUES('r1','a','content1',1,1,'{}','rights1')`); err != nil {
		t.Fatal(err)
	}
	tx1, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx1.Rollback()
	var state string
	if err = tx1.QueryRowContext(ctx, `SELECT state FROM creative_content_revisions WHERE account_id='a' AND id='r1' FOR UPDATE`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx2.Rollback()
	if _, err = tx2.ExecContext(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		t.Fatal(err)
	}
	err = tx2.QueryRowContext(ctx, `SELECT state FROM creative_content_revisions WHERE account_id='a' AND id='r1' FOR UPDATE`).Scan(&state)
	// SQLSTATE 55P03 表示持有者未释放时，另一事务无法取得同一修订锁。
	sqlErr, ok := err.(interface{ SQLState() string })
	if !ok || sqlErr.SQLState() != "55P03" {
		t.Fatalf("expected lock timeout, got %v", err)
	}
	_ = tx2.Rollback()
	if _, err = tx1.ExecContext(ctx, `INSERT INTO creative_upload_candidates(id,account_id,upload_id,content_id,content_revision_id,target_snapshot,expires_at) VALUES('candidate1','a','upload1','content1','r1','{}',now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	if err = tx1.Commit(); err != nil {
		t.Fatal(err)
	}
	gc, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer gc.Rollback()
	if err = gc.QueryRowContext(ctx, `SELECT state FROM creative_content_revisions WHERE account_id='a' AND id='r1' FOR UPDATE`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	var roots int
	if err = gc.QueryRowContext(ctx, `SELECT count(*) FROM creative_upload_candidates WHERE account_id='a' AND content_revision_id='r1' AND state='pending'`).Scan(&roots); err != nil || roots != 1 {
		t.Fatalf("post-lock roots=%d: %v", roots, err)
	}
	// 模拟到期释放后清理先取得锁；后续绑定必须看到 deleting 并拒绝。
	if _, err = gc.ExecContext(ctx, `UPDATE creative_upload_candidates SET state='expired',content_id=NULL,content_revision_id=NULL WHERE id='candidate1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = gc.ExecContext(ctx, `UPDATE creative_content_revisions SET state='deleting' WHERE id='r1'`); err != nil {
		t.Fatal(err)
	}
	if err = gc.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT state FROM creative_content_revisions WHERE account_id='a' AND id='r1'`).Scan(&state); err != nil || state != "deleting" {
		t.Fatalf("state=%s: %v", state, err)
	}
}

func TestDurableSourceAndPublicationIdentity(t *testing.T) {
	db := setup(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO creative_rights_declarations(id,account_id,source_class,rights_basis) VALUES('rights1','a','photographer_owned','ownership_attested')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO creative_uploads(id,account_id,operation_id,publish_operation_id,rights_declaration_id,declared_kind,declared_size,reserved_bytes,original_name,staging_key,expires_at,target_kind,target_snapshot) VALUES('u1','a','create1','publish1','rights1','image',1,1,'a.jpg','creative-v2/a/staging/u1',now()+interval '1 day','node','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO creative_operation_receipts(account_id,operation_id,operation_type,client_created_at,request_hash,http_status,response,result_kind,result_id,retained_until) VALUES('a','create1','create_upload',now(),repeat('a',64),202,'{"state":"created"}','upload','u1',now()+interval '90 days')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var source, publishID string
	if err = db.QueryRow(`SELECT d.source_class,u.publish_operation_id FROM creative_uploads u JOIN creative_rights_declarations d ON d.account_id=u.account_id AND d.id=u.rights_declaration_id WHERE u.account_id='a' AND u.id='u1'`).Scan(&source, &publishID); err != nil || source != "photographer_owned" || publishID != "publish1" {
		t.Fatalf("durable source=%s publish=%s err=%v", source, publishID, err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// 只验证本模块的结果记录；节点真实绑定由后续领域联合事务测试覆盖。
	if _, err = tx.Exec(`UPDATE creative_uploads SET state='ready',handed_off_at=now(),published_blob_id_snapshot='b1',publication_result_kind='node',publication_result_id='node1' WHERE account_id='a' AND id='u1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO creative_operation_receipts(account_id,operation_id,operation_type,client_created_at,request_hash,http_status,response,result_kind,result_id,retained_until) VALUES('a','publish1','publish_upload',now(),repeat('b',64),200,'{"state":"ready"}','node','node1',now()+interval '90 days')`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var accepted, published int
	var result string
	if err = db.QueryRow(`SELECT a.http_status,p.http_status,u.publication_result_id FROM creative_uploads u JOIN creative_operation_receipts a ON a.account_id=u.account_id AND a.operation_id=u.operation_id JOIN creative_operation_receipts p ON p.account_id=u.account_id AND p.operation_id=u.publish_operation_id WHERE u.account_id='a' AND u.id='u1'`).Scan(&accepted, &published, &result); err != nil || accepted != 202 || published != 200 || result != "node1" {
		t.Fatalf("receipt mapping %d/%d/%s: %v", accepted, published, result, err)
	}
}
