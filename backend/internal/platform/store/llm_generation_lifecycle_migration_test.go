package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// 旧迁移的形状/回退验证先剥离新head，再继续验证原来的目标层。
func rollbackGenerationLifecycle(t *testing.T, url string) {
	t.Helper()
	rollbackGenerationHandoff(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("rollback generation-lifecycle: %v", err)
	}
}

func TestGenerationLifecycleMigration(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, table := range []string{"llm_generation_deployments", "llm_generation_tasks", "llm_generation_observations", "llm_generation_outputs"} {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: %d %v", table, n, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO llm_generation_tasks(account_id,request_id,attempt_id,deployment_id,adapter_version,limit_key,action,created_at) VALUES('a','r','attempt','dep','v1','lim','submit',now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE llm_generation_tasks SET task_digest='digest' WHERE account_id='a'`); err == nil {
		t.Fatal("unpaired protected task identity accepted")
	}
	if _, err := db.Exec(`UPDATE llm_generation_tasks SET epoch=0 WHERE account_id='a'`); err == nil {
		t.Fatal("zero epoch accepted")
	}
	if _, err := db.Exec(`UPDATE llm_generation_tasks SET failures=6 WHERE account_id='a'`); err == nil {
		t.Fatal("unbounded retries accepted")
	}
	if _, err := db.Exec(`INSERT INTO llm_generation_observations(account_id,id,attempt_id,event_key,evidence_digest,evidence_cipher,disposition,claim_epoch,created_at) VALUES ('a','e1','attempt','event','d1','cipher','applied',1,now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO llm_generation_observations(account_id,id,attempt_id,event_key,evidence_digest,evidence_cipher,disposition,claim_epoch,created_at) VALUES ('a','e2','attempt','event','d2','cipher','applied',1,now())`); err == nil {
		t.Fatal("second normal event identity accepted")
	}
	if _, err := db.Exec(`INSERT INTO llm_generation_observations(account_id,id,attempt_id,event_key,evidence_digest,evidence_cipher,disposition,claim_epoch,created_at) VALUES ('a','e2','attempt','event','d2','cipher','conflict',1,now())`); err != nil {
		t.Fatal(err)
	}
	rollbackGenerationHandoff(t, url)
	if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "generation lifecycle") {
		t.Fatalf("live task down guard: %v", err)
	}
	clean := startPostgres(t)
	if err := store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
	rollbackGenerationLifecycle(t, clean)
	if err := store.MigrateUp(clean); err != nil {
		t.Fatal(err)
	}
}
