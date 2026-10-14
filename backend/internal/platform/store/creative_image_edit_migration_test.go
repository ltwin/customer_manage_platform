package store_test

import (
	"database/sql"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackImageEdit(t *testing.T, url string) {
	t.Helper()
	rollbackAssetDigestLookup(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback agent-library-organize", err)
	}
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-image-edit", err)
	}
}

// One node can have only one unfinished candidate; finished ones never block
// the next edit. The table holds nothing a rollback would lose: applied results
// are held by node versions, unapplied ones were never booked.
func TestImageEditMigrationLimitsOneActiveCandidatePerNodeAndRollsBack(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	insert := func(id, state string) error {
		_, err := db.Exec(`INSERT INTO creative_image_edit_renders(account_id,id,operation_id,request_hash,canvas_id,node_id,source_revision_id,action,parameters,encoder_policy,caller_kind,state,expires_at)
			VALUES('ie',$1,$1,'h','canvas','node','rev','compress','{"quality":80}','image-edit-v1','photographer',$2,clock_timestamp()+interval '2 minutes')`, id, state)
		return err
	}
	if err := insert("done", "expired"); err != nil {
		t.Fatal(err)
	}
	if err := insert("first", "queued"); err != nil {
		t.Fatal(err)
	}
	if err := insert("second", "running"); err == nil {
		t.Fatal("a second unfinished candidate was accepted for the same node")
	}
	if _, err := db.Exec(`UPDATE creative_image_edit_renders SET state='applied' WHERE id='first'`); err == nil {
		t.Fatal("an applied candidate without a result revision was accepted")
	}
	rollbackImageEdit(t, url)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_name='creative_image_edit_renders'`).Scan(&n); err != nil || n != 0 {
		t.Fatal("rollback left the table", err)
	}
}
