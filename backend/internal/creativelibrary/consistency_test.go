package creativelibrary_test

import (
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestConcurrentInlineTagReuseAndHierarchyConflict(t *testing.T) {
	_, a, _ := setup(t)
	commands := []creativeops.Command{
		cmd(t, creativelibrary.CreateAssetInput{Title: "one", Content: textDraft("one"), NewTags: []creativelibrary.NewTag{{ClientKey: "tag", Name: "ＡＩ", Color: "#ABCDEF"}}}),
		cmd(t, creativelibrary.CreateAssetInput{Title: "two", Content: textDraft("two"), NewTags: []creativelibrary.NewTag{{ClientKey: "tag", Name: "ai", Color: "#123456"}}}),
	}
	type result struct {
		r   creativeops.Receipt
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, command := range commands {
		go func() { <-start; r, e := creativelibrary.CreateAsset(t.Context(), a, command); results <- result{r, e} }()
	}
	close(start)
	one, two := <-results, <-results
	v1 := decode[creativelibrary.AssetResult](t, one.r, one.err)
	v2 := decode[creativelibrary.AssetResult](t, two.r, two.err)
	if v1.TagMapping["tag"] != v2.TagMapping["tag"] {
		t.Fatal("concurrent normalized names created separate tags")
	}
	tags, err := creativelibrary.ListTags(t.Context(), a)
	if err != nil || len(tags.Items) != 1 {
		t.Fatalf("tags %+v %v", tags, err)
	}
	commands = []creativeops.Command{cmd(t, creativelibrary.GroupInput{Name: "one", HierarchyRevision: tags.HierarchyRevision}), cmd(t, creativelibrary.GroupInput{Name: "two", HierarchyRevision: tags.HierarchyRevision})}
	start = make(chan struct{})
	for _, command := range commands {
		go func() { <-start; r, e := creativelibrary.CreateGroup(t.Context(), a, command); results <- result{r, e} }()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		r := <-results
		if r.err == nil {
			successes++
		} else if errors.Is(r.err, creativelibrary.ErrVersionConflict) {
			conflicts++
		} else {
			t.Fatal(r.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
}
func TestSearchCursorBindingWatermarkAndRevokedBody(t *testing.T) {
	db, a, b := setup(t)
	first := createAsset(t, a, "Ａ", nil)
	createAsset(t, a, "B", nil)
	createAsset(t, a, "C", nil)
	page, err := creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Limit: 1, Sort: "name"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != first.ID || page.NextCursor == "" {
		t.Fatalf("first page %+v %v", page, err)
	}
	_, err = creativelibrary.SearchAssets(t.Context(), b, creativelibrary.Search{Limit: 1, Sort: "name", Cursor: page.NextCursor})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("foreign cursor", err)
	}
	_, err = creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Limit: 1, Sort: "recent", Cursor: page.NextCursor})
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("changed query cursor", err)
	}
	next, err := creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Limit: 1, Sort: "name", Cursor: page.NextCursor})
	if err != nil || next.Items[0].Title != "B" || next.TotalCount != 3 || next.LibraryRevision != page.LibraryRevision {
		t.Fatalf("second page %+v %v", next, err)
	}
	r, err := creativelibrary.Metadata(t.Context(), a, cmd(t, creativelibrary.MetadataInput{ID: first.ID, ExpectedRevision: 1, Title: ptr("AA")}))
	decode[creativelibrary.LibraryResult](t, r, err)
	next, err = creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Limit: 1, Sort: "name", Cursor: page.NextCursor})
	if err != nil || next.LibraryRevision == page.LibraryRevision {
		t.Fatal("no watermark after write", err)
	}
	if search(t, a, creativelibrary.Search{Q: "100%"}).TotalCount != 3 {
		t.Fatal("body missing")
	}
	if _, err = db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='library-a' AND purpose='display'`); err != nil {
		t.Fatal(err)
	}
	if search(t, a, creativelibrary.Search{Q: "100%"}).TotalCount != 0 {
		t.Fatal("revoked body leaked through search")
	}
	if p := search(t, a, creativelibrary.Search{Q: "AA"}); p.TotalCount != 1 || !p.Items[0].Unavailable {
		t.Fatalf("metadata should remain, preview must not %+v", p)
	}
}
func TestMissingAndNullMetadataFieldsRejectWithoutChanges(t *testing.T) {
	_, a, _ := setup(t)
	asset := createAsset(t, a, "original", nil)
	for _, value := range []map[string]any{{"asset_id": asset.ID, "expected_revision": "1", "title": nil}, {"asset_id": asset.ID, "expected_revision": "1", "is_favorite": nil}} {
		_, err := creativelibrary.Metadata(t.Context(), a, cmd(t, value))
		if !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("null accepted %v", err)
		}
	}
	_, err := creativelibrary.SaveSettings(t.Context(), a, cmd(t, map[string]any{"expected_revision": "1"}))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("missing retention accepted", err)
	}
	current, err := creativelibrary.GetAsset(t.Context(), a, asset.ID)
	if err != nil || current.Revision != 1 || current.Title != "original" {
		t.Fatalf("invalid metadata changed asset %+v %v", current, err)
	}
}

func TestBatchReceiptKeepsIdentityAndRevisionTogether(t *testing.T) {
	_, a, _ := setup(t)
	one := createAsset(t, a, "one", nil)
	two := createAsset(t, a, "two", nil)
	r, err := creativelibrary.Metadata(t.Context(), a, cmd(t, creativelibrary.MetadataInput{ID: two.ID, ExpectedRevision: 1, Title: ptr("second version")}))
	decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.CreateTag(t.Context(), a, cmd(t, creativelibrary.TagInput{Name: "tag", Color: "#ABCDEF", HierarchyRevision: 1}))
	tag := decode[creativelibrary.LibraryResult](t, r, err)
	command := cmd(t, creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: one.ID, Revision: 1}, {ID: two.ID, Revision: 2}}, AddTags: []string{tag.ID}})
	r, err = creativelibrary.Organize(t.Context(), a, command)
	result := decode[creativelibrary.LibraryResult](t, r, err)
	current, err := creativelibrary.GetAsset(t.Context(), a, result.ID)
	if err != nil || result.Revision != current.Revision {
		t.Fatalf("organize receipt mismatches asset: %+v actual=%d %v", result, current.Revision, err)
	}
	command = cmd(t, creativelibrary.BatchAssetInput{Assets: []creativelibrary.AssetVersion{{ID: one.ID, Revision: 2}, {ID: two.ID, Revision: 3}}})
	r, err = creativelibrary.BatchTrash(t.Context(), a, command)
	result = decode[creativelibrary.LibraryResult](t, r, err)
	current, err = creativelibrary.GetAsset(t.Context(), a, result.ID)
	if err != nil || result.Revision != current.Revision || *r.Outcome.ResultID != current.ID || *r.Outcome.ResultRevision != current.Revision {
		t.Fatalf("trash receipt mismatches asset: %+v actual=%d %v", result, current.Revision, err)
	}
	replay, err := creativelibrary.BatchTrash(t.Context(), a, command)
	if err != nil || *replay.Outcome.ResultRevision != current.Revision {
		t.Fatal("replayed inconsistent revision", err)
	}
}
func TestUnavailableAssetDetailRetainsOrganizingRelations(t *testing.T) {
	db, a, _ := setup(t)
	r, err := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "group", HierarchyRevision: 1}))
	group := decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "reference", Content: textDraft("private body"), GroupIDs: []string{group.ID}, NewTags: []creativelibrary.NewTag{{ClientKey: "tag", Name: "tag", Color: "#ABCDEF"}}}))
	asset := decode[creativelibrary.AssetResult](t, r, err)
	if _, err = db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE account_id='library-a' AND purpose='display'`); err != nil {
		t.Fatal(err)
	}
	detail, err := creativelibrary.GetAsset(t.Context(), a, asset.ID)
	if err != nil || !detail.Unavailable || detail.Content != nil || len(detail.GroupIDs) != 1 || detail.GroupIDs[0] != group.ID || len(detail.TagIDs) != 1 || detail.TagIDs[0] != asset.TagMapping["tag"] {
		t.Fatalf("denied content lost organizing relations: %+v %v", detail, err)
	}
	page := search(t, a, creativelibrary.Search{})
	if len(page.Items) != 1 || len(page.Items[0].GroupIDs) != len(detail.GroupIDs) || len(page.Items[0].TagIDs) != len(detail.TagIDs) {
		t.Fatal("list/detail disagree")
	}
}
