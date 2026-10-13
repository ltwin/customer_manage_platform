package creativereference

import (
	"database/sql"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

// TestEveryReferenceColumnIsClassified fails when a migration adds a column
// that names a revision or a blob without deciding whether it holds it. A
// silent miss here is how GC would delete something still in use.
func TestEveryReferenceColumnIsClassified(t *testing.T) {
	db, err := sql.Open("pgx", storetest.NewURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT table_name||'.'||column_name FROM information_schema.columns
		WHERE table_schema='public' AND data_type='text'
		AND (column_name LIKE '%revision\_id' OR column_name LIKE '%blob%')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	found := map[string]bool{}
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		found[column] = true
		if _, ok := referenceColumns[column]; !ok {
			t.Errorf("%s names a revision or blob but is not classified; decide holder or not in media.md §6.2 and here", column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for column := range referenceColumns {
		if !found[column] {
			t.Errorf("%s is classified but no longer exists", column)
		}
	}
}
