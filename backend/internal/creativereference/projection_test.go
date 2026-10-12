package creativereference

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

// columnRole is a decision recorded for media GC (media.md §6.2), not a
// description: a column either keeps what it names alive or it does not.
type columnRole int

const (
	// revisionHolder must appear in liveOwnerCondition.
	revisionHolder columnRole = iota
	// blobHolder must keep the blob out of GC while its row is unfinished.
	blobHolder
	// notHolder records, snapshots or derives; it never blocks reclamation.
	notHolder
)

var referenceColumns = map[string]columnRole{
	"creative_assets.content_revision_id":                    revisionHolder,
	"creative_change_content_refs.content_revision_id":       revisionHolder,
	"creative_checkpoint_content_refs.content_revision_id":   revisionHolder,
	"creative_context_item_content_refs.content_revision_id": revisionHolder,
	"creative_documents.content_revision_id":                 revisionHolder,
	"creative_execution_refs.content_revision_id":            revisionHolder,
	"creative_message_content_refs.content_revision_id":      revisionHolder,
	"creative_node_inputs.content_revision_id":               revisionHolder,
	"creative_node_prompt_refs.content_revision_id":          revisionHolder,
	"creative_node_version_input_refs.content_revision_id":   revisionHolder,
	"creative_node_versions.content_revision_id":             revisionHolder,
	"creative_nodes.content_revision_id":                     revisionHolder,
	"creative_revision_holds.content_revision_id":            revisionHolder,
	"creative_run_inputs.content_revision_id":                revisionHolder,
	"creative_upload_candidates.content_revision_id":         revisionHolder,

	"creative_content_objects.blob_id":          blobHolder,
	"creative_uploads.blob_id":                  blobHolder,
	"creative_generation_imports.original_blob": blobHolder,
	"creative_generation_imports.display_blob":  blobHolder,
	"creative_blob_read_pins.blob_id":           blobHolder,

	// The revision's own binding and rights: they go with the revision.
	"creative_content_objects.content_revision_id":         notHolder,
	"creative_content_required_grants.content_revision_id": notHolder,
	// An approval record; dispatch re-examines only what a run sends.
	"creative_egress_consent_contents.content_revision_id": notHolder,
	// Written in the same transaction that retains the revision elsewhere.
	"creative_generation_imports.content_revision_id": notHolder,
	"llm_generation_outputs.content_revision_id":      notHolder,
	// Derived facts, deleted with the blob.
	"creative_blob_palettes.blob_id": notHolder,
	"creative_media_facts.blob_id":   notHolder,
	"creative_media_probes.blob_id":  notHolder,
	// What a finished upload published, kept for its receipt.
	"creative_uploads.published_blob_id_snapshot": notHolder,
}

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
	for column, role := range referenceColumns {
		if !found[column] {
			t.Errorf("%s is classified but no longer exists", column)
		}
		table, _, _ := strings.Cut(column, ".")
		if held := strings.Contains(liveOwnerCondition, " "+table+" "); held != (role == revisionHolder) {
			t.Errorf("%s: classified as %v but liveOwnerCondition mention is %v", column, role, held)
		}
	}
}
