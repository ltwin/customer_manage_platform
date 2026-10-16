package store_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// rollbackDocumentFoundation is the head of the rollback chain: every older
// migration's rollback helper undoes this one first.
func rollbackDocumentFoundation(t *testing.T, url string) {
	t.Helper()
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-document-foundation", err)
	}
}

// Every existing document gets exactly one system revision matching its head,
// so no code path has to handle a document without history.
func TestDocumentFoundationMigrationBackfillsOneSystemRevision(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	rollbackDocumentFoundation(t, url)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_documents(id,account_id,type_key,title,content_revision_id,revision)
 VALUES('cwdoc_internal','acct','internal.document','说明','ccrv_internal',3),
       ('cwdoc_other','acct','photo.shoot_plan','策划','ccrv_other',2)`); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		id, units string
		revision  int64
	}{
		{"cwdoc_internal", `{"meta":3,"body":3}`, 3},
		{"cwdoc_other", `{"meta":2}`, 2},
	} {
		var count int
		var revision int64
		var actor, contentRevision, title string
		var changed []byte
		var rowUnits, headUnits []byte
		if err := db.QueryRow(`SELECT count(*) FROM creative_document_revisions WHERE document_id=$1`, c.id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s has %d revisions, want 1", c.id, count)
		}
		if err := db.QueryRow(`SELECT r.revision,r.actor_kind,r.content_revision_id,r.title,array_to_json(r.changed_units)::text,r.unit_revs::text,d.unit_revs::text
 FROM creative_document_revisions r JOIN creative_documents d ON d.account_id=r.account_id AND d.id=r.document_id WHERE r.document_id=$1`, c.id).
			Scan(&revision, &actor, &contentRevision, &title, &changed, &rowUnits, &headUnits); err != nil {
			t.Fatal(err)
		}
		if revision != c.revision || actor != "system" || string(changed) != `["*"]` {
			t.Fatalf("%s revision row = %d %s %s", c.id, revision, actor, changed)
		}
		if !sameJSON(t, rowUnits, c.units) || !sameJSON(t, headUnits, c.units) {
			t.Fatalf("%s unit_revs row=%s head=%s, want %s", c.id, rowUnits, headUnits, c.units)
		}
	}
	var holder bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM creative_document_revisions WHERE account_id='acct' AND content_revision_id='ccrv_internal')`).Scan(&holder); err != nil || !holder {
		t.Fatalf("backfilled revision does not point at the current snapshot: %v %v", holder, err)
	}
	// A backfill-only state rolls back cleanly.
	rollbackDocumentFoundation(t, url)
	var exists bool
	if err := db.QueryRow(`SELECT to_regclass('creative_document_revisions') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("revision table survived rollback: %v %v", exists, err)
	}
}

// Rolling back would drop history somebody wrote, or bring a trashed document
// back as live, so either refuses. A refused down leaves the schema dirty, so
// each case gets its own database; the clean rollback is covered by the
// backfill test above.
func TestDocumentFoundationMigrationRefusesLossyRollback(t *testing.T) {
	for _, tc := range []struct{ name, seed string }{
		{"photographer revision", `INSERT INTO creative_documents(id,account_id,type_key,title,content_revision_id,revision,unit_revs)
 VALUES('cwdoc_a','acct','internal.document','说明','ccrv_a',1,'{"meta":1,"body":1}');
 INSERT INTO creative_document_revisions(account_id,document_id,revision,content_revision_id,title,actor_kind,changed_units,unit_revs)
 VALUES('acct','cwdoc_a',1,'ccrv_a','说明','photographer','{*}','{"meta":1,"body":1}')`},
		{"trashed document", `INSERT INTO creative_documents(id,account_id,type_key,title,content_revision_id,revision,unit_revs,trashed_at)
 VALUES('cwdoc_a','acct','internal.document','说明','ccrv_a',1,'{"meta":1,"body":1}',now())`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			url := startPostgres(t)
			if err := store.MigrateUp(url); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("pgx", url)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			if _, err = db.Exec(tc.seed); err != nil {
				t.Fatal(err)
			}
			if err := store.MigrateDownOneForTest(url); err == nil || !strings.Contains(err.Error(), "before rollback") {
				t.Fatal("lossy rollback", err)
			}
		})
	}
}

// An agent revision always names its run; the database refuses one that does not.
func TestDocumentFoundationMigrationRequiresRunForAgentRevision(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_document_revisions(account_id,document_id,revision,content_revision_id,title,actor_kind,unit_revs)
 VALUES('acct','cwdoc_a',1,'ccrv_a','说明','agent','{"meta":1}')`); err == nil {
		t.Fatal("agent revision without run_id accepted")
	}
}

func sameJSON(t *testing.T, got []byte, want string) bool {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &b); err != nil {
		t.Fatal(err)
	}
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}
