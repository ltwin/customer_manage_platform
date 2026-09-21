package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// 事实迁移是纯增量：旧行不动，新表只做加法，CHECK 约束在存储边界锁定
// 已知/未知契约（NULL 度量列、按模态适用的事实）。
func TestGenerationMediaFactsMigrationShape(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_media_facts(id,account_id,blob_id,object_version,digest,extractor_version,facts_schema_version,kind,mime,byte_size,dimension_basis,duration_ms,probed_at,facts_sha256)
		VALUES('f1','a','b1','v1','sha256-` + strings.Repeat("a", 64) + `','facts-v1',1,'audio','audio/mpeg',100,'none',NULL,now(),'sha256-` + strings.Repeat("b", 64) + `')`); err != nil {
		t.Fatal(err)
	}
	// 时长未知可表达；零是另一个已知值。
	if _, err = db.Exec(`UPDATE creative_media_facts SET duration_ms=0 WHERE id='f1'`); err != nil {
		t.Fatal(err)
	}
	// 模态不适用的事实被存储契约拒绝。
	if _, err = db.Exec(`UPDATE creative_media_facts SET width=10,height=10 WHERE id='f1'`); err == nil {
		t.Fatal("audio facts with dimensions accepted")
	}
	// 每个精确对象版本 + 提取器版本只有一行事实。
	if _, err = db.Exec(`INSERT INTO creative_media_facts(id,account_id,blob_id,object_version,digest,extractor_version,facts_schema_version,kind,mime,byte_size,dimension_basis,duration_ms,probed_at,facts_sha256)
		VALUES('f2','a','b1','v1','sha256-` + strings.Repeat("a", 64) + `','facts-v1',1,'audio','audio/mpeg',100,'none',NULL,now(),'sha256-` + strings.Repeat("b", 64) + `')`); err == nil {
		t.Fatal("duplicate object+extractor facts accepted")
	}
	// 同一对象的第二个提取器版本是独立的事实集。
	if _, err = db.Exec(`INSERT INTO creative_media_facts(id,account_id,blob_id,object_version,digest,extractor_version,facts_schema_version,kind,mime,byte_size,dimension_basis,duration_ms,probed_at,facts_sha256)
		VALUES('f3','a','b1','v1','sha256-` + strings.Repeat("a", 64) + `','facts-v2',1,'audio','audio/mpeg',100,'none',NULL,now(),'sha256-` + strings.Repeat("b", 64) + `')`); err != nil {
		t.Fatal(err)
	}
	// 探测任务按精确对象版本 + 提取器版本去重：语义版本升级在
	// 自己的键下重新探测。
	if _, err = db.Exec(`INSERT INTO creative_media_probes(id,account_id,blob_id,object_version,extractor_version) VALUES('p1','a','b1','v1','facts-v1')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_media_probes(id,account_id,blob_id,object_version,extractor_version) VALUES('p2','a','b1','v1','facts-v1')`); err == nil {
		t.Fatal("duplicate probe task accepted")
	}
	if _, err = db.Exec(`INSERT INTO creative_media_probes(id,account_id,blob_id,object_version,extractor_version) VALUES('p3','a','b1','v1','facts-v2')`); err != nil {
		t.Fatal(err)
	}
	var round int64
	if err := db.QueryRow(`SELECT retry_round FROM creative_media_probes WHERE id='p1'`).Scan(&round); err != nil || round != 0 {
		t.Fatalf("initial retry round: %d %v", round, err)
	}
	if _, err := db.Exec(`UPDATE creative_media_probes SET retry_round=-1 WHERE id='p1'`); err == nil {
		t.Fatal("negative retry round accepted")
	}
	if _, err := db.Exec(`UPDATE creative_media_probes SET retry_round=NULL WHERE id='p1'`); err == nil {
		t.Fatal("null retry round accepted")
	}
	if _, err := db.Exec(`UPDATE creative_media_probes SET retry_round=retry_round+1 WHERE id='p1'`); err != nil {
		t.Fatal(err)
	}
	// 对这些增量而言回退无损：还没有任何历史数据派生自它们。
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("additive rollback failed: %v", err)
	}
	var n int
	if err = db.QueryRow(`SELECT count(*) FROM pg_tables WHERE tablename IN ('creative_media_facts','creative_media_probes')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("tables survived rollback: %d %v", n, err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}
