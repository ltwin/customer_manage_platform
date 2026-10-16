package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func rollbackGenerationRoutes(t *testing.T, url string) {
	t.Helper()
	rollbackGenerationBillingOptional(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback generation routes", err)
	}
}
func TestGenerationRoutesEmptyRollback(t *testing.T) {
	url := storetest.NewURL(t)
	rollbackGenerationRoutes(t, url)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
}

func TestGenerationRoutesConstraintsAndPopulatedRollback(t *testing.T) {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,caller_group_id,request_hash,model_key,catalog_version,price_version,model_snapshot,price_snapshot,output_limit,deadline_at,retained_until)
 VALUES('llmr_00000000-0000-0000-0000-000000000001','a','test','00000000-0000-0000-0000-000000000001','g','sha256-'||repeat('a',64),'model','v','v','{}','{}',1,now()+interval '1 hour',now()+interval '1 day')`)
	if err != nil {
		t.Fatal(err)
	}
	const insert = `INSERT INTO llm_generation_routes(account_id,root_request_id,current_request_id,plan,plan_digest,max_cost_micros,max_submissions) VALUES($1,'llmr_00000000-0000-0000-0000-000000000001','llmr_00000000-0000-0000-0000-000000000001','plan','digest',1,2)`
	var foreignKeys int
	if err = db.QueryRow(`SELECT count(*) FROM pg_constraint WHERE contype='f' AND conrelid IN ('llm_generation_routes'::regclass,'llm_generation_route_calls'::regclass)`).Scan(&foreignKeys); err != nil || foreignKeys != 0 {
		t.Fatalf("ADR-008 explicit keys: %d, %v", foreignKeys, err)
	}
	if _, err = db.Exec(insert, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE llm_generation_routes SET candidate_attempt=3`); err == nil {
		t.Fatal("unbounded attempts accepted")
	}
	if _, err = db.Exec(`UPDATE llm_generation_routes SET state='finished'`); err == nil {
		t.Fatal("finished route without final call accepted")
	}
	rollbackGenerationBillingOptional(t, url)
	if err = store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "generation routes exist") {
		t.Fatalf("lossy rollback allowed: %v", err)
	}
}
