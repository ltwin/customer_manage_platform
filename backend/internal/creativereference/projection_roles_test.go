package creativereference

import (
	"regexp"
	"strings"
	"testing"
)

// columnRole is a decision recorded for media GC (media.md §6.2), not a
// description: a column either keeps what it names alive or it does not.
type columnRole int

const (
	// revisionHolder must appear in LiveOwnerCondition.
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
	// Every document revision keeps its snapshot and what it referenced, so
	// restoring an old version still finds its images (until the document is purged).
	"creative_document_revisions.content_revision_id":      revisionHolder,
	"creative_document_content_refs.content_revision_id":   revisionHolder,
	"creative_execution_refs.content_revision_id":          revisionHolder,
	"creative_message_content_refs.content_revision_id":    revisionHolder,
	"creative_node_inputs.content_revision_id":             revisionHolder,
	"creative_node_prompt_refs.content_revision_id":        revisionHolder,
	"creative_node_version_input_refs.content_revision_id": revisionHolder,
	"creative_node_versions.content_revision_id":           revisionHolder,
	"creative_nodes.content_revision_id":                   revisionHolder,
	"creative_revision_holds.content_revision_id":          revisionHolder,
	"creative_run_inputs.content_revision_id":              revisionHolder,
	"creative_upload_candidates.content_revision_id":       revisionHolder,
	"creative_image_edit_renders.source_revision_id":       revisionHolder,

	"creative_content_objects.blob_id":          blobHolder,
	"creative_uploads.blob_id":                  blobHolder,
	"creative_generation_imports.original_blob": blobHolder,
	"creative_generation_imports.display_blob":  blobHolder,
	"creative_blob_read_pins.blob_id":           blobHolder,
	// An unfinished edit candidate's own output, not yet bound to a revision.
	"creative_image_edit_renders.original_blob_id": blobHolder,
	"creative_image_edit_renders.display_blob_id":  blobHolder,

	// The revision's own binding and rights: they go with the revision.
	"creative_content_objects.content_revision_id":         notHolder,
	"creative_content_required_grants.content_revision_id": notHolder,
	// An approval record; dispatch re-examines only what a run sends.
	"creative_egress_consent_contents.content_revision_id": notHolder,
	// Written in the same transaction that retains the revision elsewhere.
	"creative_generation_imports.content_revision_id": notHolder,
	"llm_generation_outputs.content_revision_id":      notHolder,
	// Applying writes it in the same transaction as the node version holding it.
	"creative_image_edit_renders.result_revision_id": notHolder,
	// Derived facts, deleted with the blob.
	"creative_blob_palettes.blob_id": notHolder,
	"creative_media_facts.blob_id":   notHolder,
	"creative_media_probes.blob_id":  notHolder,
	// What a finished upload published, kept for its receipt.
	"creative_uploads.published_blob_id_snapshot": notHolder,
}

func TestReferenceProjectionMatchesColumnRoles(t *testing.T) {
	for column, role := range referenceColumns {
		if held := projectionHoldsColumn(LiveOwnerCondition, column); held != (role == revisionHolder) {
			t.Errorf("%s: classified as %v but projection holds it: %v", column, role, held)
		}
	}
}

// Each top-level EXISTS describes one owning table. Match the column that
// actually points at the revision, not another role in the same table.
func projectionHoldsColumn(condition, column string) bool {
	table, name, _ := strings.Cut(column, ".")
	comparison := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*=\s*creative_content_revisions\.id\b`)
	for _, term := range strings.Split(condition, "\nOR EXISTS") {
		if strings.Contains(term, "SELECT 1 FROM "+table+" ") && comparison.MatchString(term) {
			return true
		}
	}
	return false
}

func TestProjectionColumnGuardDistinguishesRolesWithinOneTable(t *testing.T) {
	const column = "creative_image_edit_renders.source_revision_id"
	for _, tc := range []struct {
		name, condition string
		want            bool
	}{
		{"source", `EXISTS (SELECT 1 FROM creative_image_edit_renders WHERE source_revision_id=creative_content_revisions.id)`, true},
		{"aliased source", `EXISTS (SELECT 1 FROM creative_image_edit_renders r WHERE r.source_revision_id = creative_content_revisions.id)`, true},
		{"wrong column", `EXISTS (SELECT 1 FROM creative_image_edit_renders WHERE result_revision_id=creative_content_revisions.id)`, false},
		{"wrong table", `EXISTS (SELECT 1 FROM other_table WHERE source_revision_id=creative_content_revisions.id)`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := projectionHoldsColumn(tc.condition, column); got != tc.want {
				t.Fatalf("held %v, want %v", got, tc.want)
			}
		})
	}
}
