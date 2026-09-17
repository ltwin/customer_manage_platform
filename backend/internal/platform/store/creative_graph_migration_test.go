package store_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestCanvasMigrationSeedsOnceAndRefusesLossyRollback(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	// Named rather than counted, for the reason rollbackTo0038 spells out: a
	// bare count silently stops short once another migration is stacked above,
	// and the test keeps passing while no longer reaching what it is about.
	for _, label := range []string{
		"0048 agent-checkpoints", "0047 agent-context", "0046 agent-runs", "0045 skill-foundation", "0044 agent-conversations",
		"0043 llm-gateway",
		"0042 creative-canvas-events", "0041 creative-media", "0040 creative-canvas-commands",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("rollback %s: %v", label, err)
		}
	}
	// 0040 is now off, so the old node has no graph identity.
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	id := "cwnode_" + uuid.NewString()
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('graph-old','test','active');INSERT INTO creative_projects(id,account_id,name) VALUES('old-project','graph-old','old');INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES('old-canvas','graph-old','old-project','canvas')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y) VALUES($1,'graph-old','old-canvas','core.text',125.5,-30)`, id); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	scope := st.ScopeFor(auth.AccountContext{AccountID: "graph-old"})
	move := func(revision creativeops.Revision) error {
		raw, _ := json.Marshal(creativecanvas.MoveNodeInput{CanvasID: "old-canvas", NodeID: id, ExpectedPlacementRevision: revision, X: 150, Y: -30})
		_, err := creativecanvas.MoveNode(t.Context(), scope, creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw})
		return err
	}
	if err = move(1); err != nil {
		t.Fatal("seed differs from runtime canonical state", err)
	}
	var before, after string
	if err = db.QueryRow(`SELECT effect_heads::text FROM creative_graph_identities WHERE id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT effect_heads::text FROM creative_graph_identities WHERE id=$1`, id).Scan(&after); err != nil || before != after {
		t.Fatal("restart regenerated effect heads", err)
	}
	if _, err = db.Exec(`DELETE FROM creative_graph_identities WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	if err = move(2); !errors.Is(err, creativecanvas.ErrGraphIntegrity) {
		t.Fatal("missing heads silently reconstructed", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent checkpoint rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent context rollback", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent runs rollback with no runs", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("skill foundation rollback with no versions", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("agent conversations rollback with no history", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("llm gateway rollback on an empty ledger", err)
	}
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("events rollback", err)
	}
	// 0041 (media) rolls back cleanly on an empty media schema; 0040 must refuse.
	if err = store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("media rollback on empty media schema", err)
	}
	if err = store.MigrateDownOneForTest(url); err == nil {
		t.Fatal("lossy graph rollback accepted")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM creative_nodes WHERE id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("rollback lost old node", err)
	}
}
