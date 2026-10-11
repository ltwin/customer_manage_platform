package store_test

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackConversationManage(t *testing.T, url string) {
	t.Helper()
	rollbackProjectHome(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback agent-conversation-manage", err)
	}
}

// Rolling back would quietly undo what the photographer did: archived
// conversations return to the list and a chosen title loses its protection.
func TestConversationManageRollbackKeepsThePhotographersChoices(t *testing.T) {
	for _, choice := range []string{"none", "archived", "manual"} {
		t.Run(choice, func(t *testing.T) {
			url := startPostgres(t)
			if err := store.MigrateUp(url); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("pgx", url)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			if _, err := db.Exec(`INSERT INTO creative_agent_conversations(id,account_id,project_id,canvas_id,title)
VALUES('ccco_00000000-0000-0000-0000-000000000001','a','p','c','新对话'),
      ('ccco_00000000-0000-0000-0000-000000000002','a','p','c','雪山婚纱布光')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`UPDATE creative_agent_conversations SET title_source='other'`); err == nil {
				t.Fatal("an unknown title source was accepted")
			}
			first := `WHERE id='ccco_00000000-0000-0000-0000-000000000001'`
			switch choice {
			case "archived":
				_, err = db.Exec(`UPDATE creative_agent_conversations SET archived_at=now() ` + first)
			case "manual":
				_, err = db.Exec(`UPDATE creative_agent_conversations SET title='我的标题', title_source='manual' ` + first)
			}
			if err != nil {
				t.Fatal(err)
			}
			rollbackProjectHome(t, url)
			err = store.MigrateDownOneForTest(url)
			if choice != "none" {
				if err == nil || !strings.Contains(err.Error(), "archived or renamed conversations exist") {
					t.Fatalf("lossy rollback accepted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := store.MigrateUp(url); err != nil {
				t.Fatal(err)
			}
			// Before the column the client always created 新对话, so any other
			// title was written by naming.
			var sources string
			if err := db.QueryRow(`SELECT string_agg(title_source, ',' ORDER BY id) FROM creative_agent_conversations`).Scan(&sources); err != nil || sources != "default,generated" {
				t.Fatalf("title sources after re-migration: %q, %v", sources, err)
			}
		})
	}
}
