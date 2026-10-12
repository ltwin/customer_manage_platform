package store_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rollbackProjectHome(t *testing.T, url string) {
	t.Helper()
	rollbackAssetLibrary(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatal("rollback creative-project-home", err)
	}
}

// The new columns are filled by the database, so an application one release
// behind keeps creating and renaming projects on the new schema.
func TestProjectHomeMigrationAcceptsOldWriters(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_projects(id,account_id,name) VALUES('p-old','a','  ＡＢＣ 婚纱  ')`); err != nil {
		t.Fatal("old insert rejected", err)
	}
	var normalized string
	var edited time.Time
	if err = db.QueryRow(`SELECT normalized_name,edited_at FROM creative_projects WHERE id='p-old'`).Scan(&normalized, &edited); err != nil {
		t.Fatal(err)
	}
	if normalized != "abc 婚纱" || edited.IsZero() {
		t.Fatalf("normalized=%q edited=%v", normalized, edited)
	}
	if _, err = db.Exec(`UPDATE creative_projects SET name='Studio',revision=revision+1,updated_at=clock_timestamp() WHERE id='p-old'`); err != nil {
		t.Fatal("old rename rejected", err)
	}
	if err = db.QueryRow(`SELECT normalized_name FROM creative_projects WHERE id='p-old'`).Scan(&normalized); err != nil || normalized != "studio" {
		t.Fatalf("rename not followed: %q %v", normalized, err)
	}
}

// Existing projects start from their latest edit: a canvas commit counts, not
// only a rename.
func TestProjectHomeMigrationBackfillsEditedAt(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	rollbackProjectHome(t, url)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`INSERT INTO creative_projects(id,account_id,name,updated_at) VALUES('p1','a','旧项目','2026-01-01T00:00:00Z');
INSERT INTO creative_canvases(id,account_id,project_id,name,updated_at) VALUES('c1','a','p1','画布','2026-03-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	var edited time.Time
	if err = db.QueryRow(`SELECT edited_at FROM creative_projects WHERE id='p1'`).Scan(&edited); err != nil {
		t.Fatal(err)
	}
	if !edited.Equal(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("edited_at=%v", edited)
	}
}
