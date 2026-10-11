package store_test

import (
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackChatFailover(t *testing.T, url string) {
	t.Helper()
	rollbackConversationManage(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback chat-failover", err)
	}
}

func TestChatFailoverMigrationRollback(t *testing.T) {
	for _, evidence := range []string{"empty", "route", "http_status"} {
		t.Run(evidence, func(t *testing.T) {
			url := startPostgres(t)
			if err := store.MigrateUp(url); err != nil {
				t.Fatal(err)
			}
			db := openSQLDatabase(t, url)
			switch evidence {
			case "route":
				if _, err := db.Exec(`INSERT INTO llm_chat_routes(account_id,root_request_id,deployments)
VALUES ('acc','root','["primary","backup"]')`); err != nil {
					t.Fatal(err)
				}
			case "http_status":
				if _, err := db.Exec(`INSERT INTO llm_requests
(id,account_id,caller_service,caller_operation_id,caller_group_id,request_hash,model_key,
catalog_version,price_version,model_snapshot,price_snapshot,output_limit,deadline_at,retained_until,failure_http_status)
VALUES ('llmr_00000000-0000-4000-8000-000000000001','acc','test','00000000-0000-4000-8000-000000000001',
'group',$1,'model','catalog','price','{}','{}',100,now(),now(),503)`, "sha256-"+strings.Repeat("a", 64)); err != nil {
					t.Fatal(err)
				}
			}
			rollbackConversationManage(t, url)
			err := store.MigrateDownOneForTest(url)
			if evidence != "empty" {
				if err == nil || !strings.Contains(err.Error(), "chat failover evidence exists") {
					t.Fatalf("lossy rollback accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var leftovers int
			if err := db.QueryRow(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public'
AND (table_name='llm_chat_routes' OR (table_name='llm_requests' AND column_name='failure_http_status'))`).Scan(&leftovers); err != nil || leftovers != 0 {
				t.Fatalf("rollback left %d columns: %v", leftovers, err)
			}
			if err := store.MigrateUp(url); err != nil {
				t.Fatal(err)
			}
		})
	}
}
