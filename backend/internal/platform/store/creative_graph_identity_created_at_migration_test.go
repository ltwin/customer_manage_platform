package store_test

import (
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackGraphIdentityCreatedAt undoes 0085 and then 0084.
func rollbackGraphIdentityCreatedAt(t *testing.T, url string) {
	t.Helper()
	rollbackAgentToolTasks(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-graph-identity-created-at", err)
	}
}

func TestCreativeGraphIdentityCreatedAtMigration(t *testing.T) {
	url, db := openMaterialTextures(t)
	rollbackGraphIdentityCreatedAt(t, url)
	if _, err := db.Exec(`SELECT created_at FROM creative_graph_identities`); err == nil {
		t.Fatal("created_at survived rollback")
	}

	const live = "cwnode_00000000-0000-0000-0000-000000000001"
	const dead = "cwnode_00000000-0000-0000-0000-000000000002"
	// Upgrading from before 0040: the node has no identity yet; the seed that
	// runs after the migrations creates it.
	const unseeded = "cwnode_00000000-0000-0000-0000-000000000004"
	seed := `INSERT INTO accounts(id,password_hash,status) VALUES('acct-ca','test','active');
INSERT INTO creative_projects(id,account_id,name) VALUES('ca-project','acct-ca','p');
INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES('ca-canvas','acct-ca','ca-project','c');
INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,created_at) VALUES
 ('` + live + `','acct-ca','ca-canvas','core.text',0,0,'2026-01-02T03:04:05.123456Z'),
 ('` + unseeded + `','acct-ca','ca-canvas','core.text',0,0,'2025-06-07T08:09:10.654321Z');
UPDATE creative_graph_migration_state SET completed=false WHERE id=1;
INSERT INTO creative_graph_identities(id,account_id,canvas_id,kind,is_live,effect_heads) VALUES
 ('` + live + `','acct-ca','ca-canvas','node',true,'{}'),
 ('` + dead + `','acct-ca','ca-canvas','node',false,'{}')`
	if _, err := db.Exec(seed); err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}

	var got time.Time
	if err := db.QueryRow(`SELECT created_at FROM creative_graph_identities WHERE id=$1`, live).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC); !got.Equal(want) {
		t.Fatalf("live node identity backfilled to %v, want the node row time %v", got, want)
	}
	if err := db.QueryRow(`SELECT created_at FROM creative_graph_identities WHERE id=$1`, unseeded).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2025, 6, 7, 8, 9, 10, 654321000, time.UTC); !got.Equal(want) {
		t.Fatalf("seeded identity got %v, want the node row time %v", got, want)
	}
	// A deleted node has no row to copy from and keeps the migration time.
	if err := db.QueryRow(`SELECT created_at FROM creative_graph_identities WHERE id=$1`, dead).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got.Before(before.Add(-time.Minute)) {
		t.Fatalf("deleted node identity got %v, want the migration time", got)
	}
	// New identities take the default.
	if _, err := db.Exec(`INSERT INTO creative_graph_identities(id,account_id,canvas_id,kind,is_live,effect_heads) VALUES('cwnode_00000000-0000-0000-0000-000000000003','acct-ca','ca-canvas','node',true,'{}')`); err != nil {
		t.Fatal(err)
	}
}
