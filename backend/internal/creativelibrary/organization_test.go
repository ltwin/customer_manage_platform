package creativelibrary_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }
func setup(t *testing.T) (*sql.DB, store.AccountScope, store.AccountScope) {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('library-a','test','active'),('library-b','test','active')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return db, st.ScopeFor(auth.AccountContext{AccountID: "library-a"}), st.ScopeFor(auth.AccountContext{AccountID: "library-b"})
}
func cmd(t *testing.T, in any) creativeops.Command {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
}
func textDraft(body string) creativecontent.Draft {
	return creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}, Rights: creativecontent.RightsDeclarationInput{SourceClass: planningmedia.SourcePhotographerOwned, RightsBasis: planningmedia.RightsOwnershipAttested}}
}
func decode[T any](t *testing.T, r creativeops.Receipt, err error) T {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	var v T
	if err = json.Unmarshal(r.Outcome.Response, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func TestGroupCycleDeletePromotesChildrenWithoutDeletingAssets(t *testing.T) {
	_, a, b := setup(t)
	r, err := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "参考", HierarchyRevision: 1}))
	root := decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "布光", ParentID: &root.ID, HierarchyRevision: root.HierarchyRevision}))
	child := decode[creativelibrary.LibraryResult](t, r, err)
	_, err = creativelibrary.MoveGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: root.ID, ParentID: &child.ID, ExpectedRevision: 1, HierarchyRevision: child.HierarchyRevision}))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("cycle %v", err)
	}
	_, err = creativelibrary.DeleteGroup(t.Context(), b, cmd(t, creativelibrary.GroupInput{ID: root.ID, ExpectedRevision: 1, HierarchyRevision: 1}))
	if !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatalf("cross account %v", err)
	}
	r, err = creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "日落", Content: textDraft("金色的光"), GroupIDs: []string{root.ID}}))
	asset := decode[creativelibrary.AssetResult](t, r, err)
	r, err = creativelibrary.DeleteGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: root.ID, ExpectedRevision: 1, HierarchyRevision: child.HierarchyRevision}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	groups, err := creativelibrary.ListGroups(t.Context(), a)
	if err != nil || len(groups.Items) != 1 || groups.Items[0].ParentID != nil {
		t.Fatalf("promote %+v %v", groups, err)
	}
	page, err := creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{View: "unclassified", Limit: 30})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != asset.ID || page.Items[0].Revision != 2 {
		t.Fatalf("asset retained %+v %v", page, err)
	}
}

func createAsset(t *testing.T, a store.AccountScope, title string, tags []creativelibrary.NewTag) creativelibrary.AssetResult {
	t.Helper()
	r, err := creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: title, Content: textDraft("窗边 光线 与100%真实参考"), NewTags: tags}))
	return decode[creativelibrary.AssetResult](t, r, err)
}
func search(t *testing.T, a store.AccountScope, q creativelibrary.Search) creativelibrary.AssetPage {
	t.Helper()
	q.Limit = 30
	p, err := creativelibrary.SearchAssets(t.Context(), a, q)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestNormalizedTagsQueriesAndCategoryDeletion(t *testing.T) {
	_, a, b := setup(t)
	r, err := creativelibrary.CreateCategory(t.Context(), a, cmd(t, creativelibrary.CategoryInput{Name: "风格", HierarchyRevision: 1}))
	category := decode[creativelibrary.LibraryResult](t, r, err)
	first := createAsset(t, a, "日光 100%", []creativelibrary.NewTag{{ClientKey: "one", Name: "ＡＩ", Color: "#AABBCC", CategoryID: &category.ID}, {ClientKey: "two", Name: "布光", Color: "#123456"}})
	second := createAsset(t, a, "窗边", []creativelibrary.NewTag{{ClientKey: "same", Name: "ai", Color: "#FFFFFF"}})
	if first.TagMapping["one"] != second.TagMapping["same"] {
		t.Fatal("normalized tag not reused")
	}
	tags, err := creativelibrary.ListTags(t.Context(), a)
	if err != nil || len(tags.Items) != 2 {
		t.Fatalf("tags %+v %v", tags, err)
	}
	for _, tt := range []struct {
		q     creativelibrary.Search
		count int64
	}{{creativelibrary.Search{TagIDs: []string{first.TagMapping["one"], first.TagMapping["two"]}, TagMode: "all"}, 1}, {creativelibrary.Search{TagIDs: []string{first.TagMapping["one"], first.TagMapping["two"]}, TagMode: "any"}, 2}, {creativelibrary.Search{Q: "ＡＩ"}, 2}, {creativelibrary.Search{Q: "100%"}, 2}, {creativelibrary.Search{Q: "100_"}, 0}, {creativelibrary.Search{Q: "\\"}, 0}} {
		if p := search(t, a, tt.q); p.TotalCount != tt.count {
			t.Fatalf("query %+v count %d", tt.q, p.TotalCount)
		}
	}
	_, err = creativelibrary.SearchAssets(t.Context(), b, creativelibrary.Search{TagIDs: []string{first.TagMapping["one"]}, Limit: 30})
	if !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatalf("foreign filter %v", err)
	}
	r, err = creativelibrary.DeleteCategory(t.Context(), a, cmd(t, creativelibrary.CategoryInput{ID: category.ID, ExpectedRevision: 1, HierarchyRevision: tags.HierarchyRevision}))
	result := decode[creativelibrary.LibraryResult](t, r, err)
	tags, err = creativelibrary.ListTags(t.Context(), a)
	if err != nil || len(tags.Items) != 2 {
		t.Fatal(err)
	}
	var tag creativelibrary.Tag
	for _, t := range tags.Items {
		if t.ID == first.TagMapping["one"] {
			tag = t
		}
	}
	if tag.CategoryID != nil || tag.Color != "#AABBCC" {
		t.Fatalf("category delete or reuse mutated tag %+v", tag)
	}
	r, err = creativelibrary.EditTag(t.Context(), a, cmd(t, creativelibrary.TagInput{ID: tag.ID, Name: "人工智能", Color: tag.Color, ExpectedRevision: tag.Revision, HierarchyRevision: result.HierarchyRevision}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if search(t, a, creativelibrary.Search{Q: "人工智能"}).TotalCount != 2 || search(t, a, creativelibrary.Search{Q: "ＡＩ"}).TotalCount != 0 {
		t.Fatal("rename did not update query")
	}
}
func TestAtomicOrganizeAndTagDeleteProtectStaleAssetVersions(t *testing.T) {
	_, a, b := setup(t)
	first := createAsset(t, a, "one", []creativelibrary.NewTag{{ClientKey: "t", Name: "红色", Color: "#AA0000"}})
	second := createAsset(t, a, "two", nil)
	foreign := createAsset(t, b, "foreign", nil)
	input := creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: first.ID, Revision: 1}, {ID: foreign.ID, Revision: 1}}, AddTags: []string{first.TagMapping["t"]}}
	_, err := creativelibrary.Organize(t.Context(), a, cmd(t, input))
	if !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatal(err)
	}
	input.Assets = []creativelibrary.AssetVersion{{ID: second.ID, Revision: 1}}
	r, err := creativelibrary.Organize(t.Context(), a, cmd(t, input))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	tags, err := creativelibrary.ListTags(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	r, err = creativelibrary.DeleteTag(t.Context(), a, cmd(t, creativelibrary.TagInput{ID: tags.Items[0].ID, ExpectedRevision: 1, HierarchyRevision: tags.HierarchyRevision}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	_, err = creativelibrary.Metadata(t.Context(), a, cmd(t, creativelibrary.MetadataInput{ID: first.ID, ExpectedRevision: 1, Title: ptr("stale")}))
	if !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatalf("stale asset after tag delete %v", err)
	}
	v, err := creativelibrary.GetAsset(t.Context(), a, first.ID)
	if err != nil || v.Title != "one" || len(v.TagIDs) != 0 {
		t.Fatalf("bad asset %+v %v", v, err)
	}
}
func ptr[T any](v T) *T { return &v }
func TestTrashRetentionRestoreAndPurgeKeepCanvasReference(t *testing.T) {
	db, a, _ := setup(t)
	asset := createAsset(t, a, "保留引用", nil)
	r, err := creativelibrary.SaveSettings(t.Context(), a, cmd(t, creativelibrary.SettingsInput{ExpectedRevision: 1, RetentionDays: ptr(7)}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	_, err = creativelibrary.Purge(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 1}))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("active purge", err)
	}
	r, err = creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 1}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	trash := search(t, a, creativelibrary.Search{View: "trash"})
	if trash.TotalCount != 1 || trash.Items[0].PurgeAfter == nil {
		t.Fatal("trash missing deadline")
	}
	deadline := *trash.Items[0].PurgeAfter
	r, err = creativelibrary.SaveSettings(t.Context(), a, cmd(t, creativelibrary.SettingsInput{ExpectedRevision: 2, RetentionDays: ptr(90)}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if *search(t, a, creativelibrary.Search{View: "trash"}).Items[0].PurgeAfter != deadline {
		t.Fatal("settings changed old deadline")
	}
	r, err = creativelibrary.Restore(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 2}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	current, err := creativelibrary.GetAsset(t.Context(), a, asset.ID)
	if err != nil || current.DeletedAt != nil || current.PurgeAfter != nil {
		t.Fatal(err)
	}
	// A real content-owning node survives release of its independent personal-library root.
	_, err = db.Exec(`INSERT INTO creative_projects(id,account_id,name) VALUES('p','library-a','test'); INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES('c','library-a','p','test')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,content_id,content_revision_id) SELECT 'n',account_id,'c','core.text',0,0,content_id,content_revision_id FROM creative_assets WHERE account_id='library-a' AND id=$1`, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 3}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	purge := cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: 4})
	r, err = creativelibrary.Purge(t.Context(), a, purge)
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if _, err = creativelibrary.Purge(t.Context(), a, purge); err != nil {
		t.Fatal("purge replay", err)
	}
	if _, err = creativecontent.Read(t.Context(), a, asset.ContentRevisionID); err != nil {
		t.Fatal("purge destroyed node content", err)
	}
	if search(t, a, creativelibrary.Search{View: "trash"}).TotalCount != 0 {
		t.Fatal("purge left asset")
	}
}
