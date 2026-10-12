package creativelibrary_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func rate(t *testing.T, a store.AccountScope, items ...creativelibrary.RateItem) creativelibrary.RateResult {
	t.Helper()
	r, err := creativelibrary.BatchRate(t.Context(), a, cmd(t, creativelibrary.RateInput{Items: items}))
	return decode[creativelibrary.RateResult](t, r, err)
}

func current(t *testing.T, a store.AccountScope, id string) creativelibrary.Asset {
	t.Helper()
	v, err := creativelibrary.GetAsset(t.Context(), a, id)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestBatchRateValidatesAndWritesAllOrNothing(t *testing.T) {
	_, a, b := setup(t)
	one, two := createAsset(t, a, "one", nil), createAsset(t, a, "two", nil)
	for _, bad := range []any{0.0, 5.5, 0.3, -1.0} {
		_, err := creativelibrary.BatchRate(t.Context(), a, cmd(t, map[string]any{"items": []map[string]any{{"asset_id": one.ID, "expected_revision": "1", "rating": bad}}}))
		if !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("rating %v accepted: %v", bad, err)
		}
	}
	_, err := creativelibrary.BatchRate(t.Context(), a, cmd(t, map[string]any{"items": []map[string]any{{"asset_id": one.ID, "expected_revision": "1"}}}))
	if !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("missing rating treated as clear", err)
	}
	// One stale revision stops the whole batch.
	_, err = creativelibrary.BatchRate(t.Context(), a, cmd(t, creativelibrary.RateInput{Items: []creativelibrary.RateItem{{ID: one.ID, ExpectedRevision: 1, Rating: ptr(4.5)}, {ID: two.ID, ExpectedRevision: 9, Rating: ptr(2.0)}}}))
	if !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal("stale batch", err)
	}
	if current(t, a, one.ID).Rating != nil {
		t.Fatal("partial batch written")
	}
	// Each item keeps its own value, so an undo can put back mixed ratings.
	command := cmd(t, creativelibrary.RateInput{Items: []creativelibrary.RateItem{{ID: one.ID, ExpectedRevision: 1, Rating: ptr(4.5)}, {ID: two.ID, ExpectedRevision: 1, Rating: ptr(0.5)}}})
	r, err := creativelibrary.BatchRate(t.Context(), a, command)
	result := decode[creativelibrary.RateResult](t, r, err)
	if len(result.Items) != 2 || result.Items[0].Revision != 2 || result.Items[1].Revision != 2 {
		t.Fatalf("result %+v", result)
	}
	if *current(t, a, one.ID).Rating != 4.5 || *current(t, a, two.ID).Rating != 0.5 {
		t.Fatal("ratings not written")
	}
	replay, err := creativelibrary.BatchRate(t.Context(), a, command)
	if err != nil || !sameJSON(t, replay.Outcome.Response, r.Outcome.Response) {
		t.Fatal("replay differs", err)
	}
	cleared := rate(t, a, creativelibrary.RateItem{ID: one.ID, ExpectedRevision: 2, Rating: nil})
	if current(t, a, one.ID).Rating != nil || cleared.Items[0].Revision != 3 {
		t.Fatal("null did not clear")
	}
	_, err = creativelibrary.BatchRate(t.Context(), b, cmd(t, creativelibrary.RateInput{Items: []creativelibrary.RateItem{{ID: two.ID, ExpectedRevision: 2, Rating: ptr(1.0)}}}))
	if !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatal("foreign asset", err)
	}
	r, err = creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: two.ID, Revision: 2}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	_, err = creativelibrary.BatchRate(t.Context(), a, cmd(t, creativelibrary.RateInput{Items: []creativelibrary.RateItem{{ID: two.ID, ExpectedRevision: 3, Rating: ptr(1.0)}}}))
	if !errors.Is(err, creativelibrary.ErrTrashed) {
		t.Fatal("trashed asset rated", err)
	}
}

func TestRatingFiltersSortAndPagesWithoutGaps(t *testing.T) {
	_, a, _ := setup(t)
	ratings := []*float64{ptr(5.0), ptr(3.0), nil, ptr(3.0), ptr(5.0), nil, ptr(1.0)}
	ids := make([]string, len(ratings))
	for i, r := range ratings {
		ids[i] = createAsset(t, a, "asset", nil).ID
		if r != nil {
			rate(t, a, creativelibrary.RateItem{ID: ids[i], ExpectedRevision: 1, Rating: r})
		}
	}
	// Higher first, unrated last, newer first within a rating.
	want := []string{ids[4], ids[0], ids[3], ids[1], ids[6], ids[5], ids[2]}
	got := []string{}
	cursor := ""
	for pages := 0; ; pages++ {
		page, err := creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Sort: "rating", Limit: 3, Cursor: cursor})
		if err != nil || page.TotalCount != 7 {
			t.Fatalf("page %d: %+v %v", pages, page, err)
		}
		for _, item := range page.Items {
			got = append(got, item.ID)
		}
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order %v want %v", got, want)
	}
	if n := search(t, a, creativelibrary.Search{MinRating: ptr(3.0)}).TotalCount; n != 4 {
		t.Fatal("min_rating 3", n)
	}
	if n := search(t, a, creativelibrary.Search{MinRating: ptr(5.0)}).TotalCount; n != 2 {
		t.Fatal("min_rating 5", n)
	}
	if n := search(t, a, creativelibrary.Search{Unrated: true}).TotalCount; n != 2 {
		t.Fatal("unrated", n)
	}
	for _, bad := range []creativelibrary.Search{{MinRating: ptr(0.3)}, {MinRating: ptr(6.0)}, {MinRating: ptr(0.0)}, {MinRating: ptr(0.0), Unrated: true}, {MinRating: ptr(2.0), Unrated: true}, {Sort: "stars"}} {
		bad.Limit = 30
		if _, err := creativelibrary.SearchAssets(t.Context(), a, bad); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("accepted %+v: %v", bad, err)
		}
	}
}

func TestRecentViewIsTheLastSevenDays(t *testing.T) {
	db, a, _ := setup(t)
	old, fresh := createAsset(t, a, "old", nil), createAsset(t, a, "fresh", nil)
	if _, err := db.Exec(`UPDATE creative_assets SET created_at=now()-interval '7 days'-interval '1 minute' WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE creative_assets SET created_at=now()-interval '7 days'+interval '1 minute' WHERE id=$1`, fresh.ID); err != nil {
		t.Fatal(err)
	}
	page := search(t, a, creativelibrary.Search{View: "recent"})
	if page.TotalCount != 1 || page.Items[0].ID != fresh.ID {
		t.Fatalf("recent %+v", page)
	}
	r, err := creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: fresh.ID, Revision: 1}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if search(t, a, creativelibrary.Search{View: "recent"}).TotalCount != 0 {
		t.Fatal("trashed asset in recent")
	}
}

func TestFacetsCountKindsAndTagsLikeTheList(t *testing.T) {
	_, a, b := setup(t)
	one := createAsset(t, a, "one", []creativelibrary.NewTag{{ClientKey: "x", Name: "x", Color: "#111111"}, {ClientKey: "y", Name: "y", Color: "#222222"}})
	x, y := one.TagMapping["x"], one.TagMapping["y"]
	link := textDraft("")
	link.Kind, link.Payload.Body, link.Payload.URL = "link", nil, ptr("https://example.com/ref")
	r, err := creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "two", Content: link, TagIDs: []string{x}}))
	_ = decode[creativelibrary.AssetResult](t, r, err)
	r, err = creativelibrary.CreateTag(t.Context(), a, cmd(t, creativelibrary.TagInput{Name: "unused", Color: "#333333", HierarchyRevision: 2}))
	unused := decode[creativelibrary.LibraryResult](t, r, err)
	createAsset(t, b, "foreign", []creativelibrary.NewTag{{ClientKey: "x", Name: "x", Color: "#111111"}})

	counts := func(q creativelibrary.Search) (creativelibrary.AssetPage, map[string]int64) {
		t.Helper()
		q.IncludeFacets = true
		p := search(t, a, q)
		if p.Facets == nil {
			t.Fatal("no facets")
		}
		tags := map[string]int64{}
		for _, c := range p.Facets.Tags {
			tags[c.TagID] = c.Count
		}
		return p, tags
	}
	p, tags := counts(creativelibrary.Search{Kind: "text"})
	if p.TotalCount != 1 || p.Facets.Kinds["text"] != 1 || p.Facets.Kinds["link"] != 1 || p.Facets.Kinds["image"] != 0 {
		t.Fatalf("kinds ignore the kind filter: %+v", p.Facets.Kinds)
	}
	if tags[x] != 1 || tags[y] != 1 || tags[unused.ID] != 0 || len(tags) != 3 {
		t.Fatalf("tags within kind %v", tags)
	}
	// all: ticking a tag leaves that many; the selected tag equals the total.
	p, tags = counts(creativelibrary.Search{TagIDs: []string{x}})
	if p.TotalCount != 2 || tags[x] != p.TotalCount || tags[y] != 1 {
		t.Fatalf("all mode %d %v", p.TotalCount, tags)
	}
	p, tags = counts(creativelibrary.Search{TagIDs: []string{y}})
	if p.TotalCount != 1 || tags[x] != 1 || tags[y] != 1 {
		t.Fatalf("all mode narrowed %d %v", p.TotalCount, tags)
	}
	// any: selected tags are ignored when counting.
	p, tags = counts(creativelibrary.Search{TagIDs: []string{y}, TagMode: "any"})
	if p.TotalCount != 1 || tags[x] != 2 || tags[y] != 1 {
		t.Fatalf("any mode %d %v", p.TotalCount, tags)
	}
	page, err := creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Limit: 1})
	if err != nil || page.NextCursor == "" {
		t.Fatal(err)
	}
	if _, err = creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Limit: 1, Cursor: page.NextCursor, IncludeFacets: true}); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatal("facets on a later page", err)
	}
	if p := search(t, a, creativelibrary.Search{}); p.Facets != nil {
		t.Fatal("facets without asking")
	}
}

// The page, its total and its facets come from one snapshot: with writers
// trashing and restoring in between, the counts never disagree with the total.
func TestFacetsShareTheListSnapshot(t *testing.T) {
	_, a, _ := setup(t)
	assets := []creativelibrary.AssetResult{}
	for range 6 {
		assets = append(assets, createAsset(t, a, "asset", []creativelibrary.NewTag{{ClientKey: "x", Name: "x", Color: "#111111"}}))
	}
	tag := assets[0].TagMapping["x"]
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, asset := range assets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			revision := creativeops.Revision(1)
			for {
				select {
				case <-stop:
					return
				default:
				}
				for _, op := range []func(store.AccountScope) (creativeops.Receipt, error){
					func(s store.AccountScope) (creativeops.Receipt, error) {
						return creativelibrary.Trash(t.Context(), s, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: revision}))
					},
					func(s store.AccountScope) (creativeops.Receipt, error) {
						return creativelibrary.Restore(t.Context(), s, cmd(t, creativelibrary.AssetVersion{ID: asset.ID, Revision: revision}))
					},
				} {
					if _, err := op(a); err == nil {
						revision++
					}
				}
			}
		}()
	}
	for range 40 {
		p := search(t, a, creativelibrary.Search{TagIDs: []string{tag}, IncludeFacets: true})
		var kinds int64
		for _, n := range p.Facets.Kinds {
			kinds += n
		}
		var tagged int64
		for _, c := range p.Facets.Tags {
			if c.TagID == tag {
				tagged = c.Count
			}
		}
		if kinds != p.TotalCount || tagged != p.TotalCount || int64(len(p.Items)) != p.TotalCount {
			close(stop)
			wg.Wait()
			t.Fatalf("snapshot torn: total=%d items=%d kinds=%d tag=%d", p.TotalCount, len(p.Items), kinds, tagged)
		}
	}
	close(stop)
	wg.Wait()
}

// readableContent must say "readable" exactly when the display read guard lets
// the content through; otherwise a filter could reveal what the list hides.
func TestReadableConditionMatchesTheDisplayGuard(t *testing.T) {
	db, a, b := setup(t)
	ok := createAsset(t, a, "ok", nil)
	revoked := createAsset(t, a, "revoked", nil)
	dependent := createAsset(t, a, "dependent", nil)
	undeclared := createAsset(t, a, "undeclared", nil)
	deleting := createAsset(t, a, "deleting", nil)
	foreign := createAsset(t, b, "foreign", nil)
	declaration := func(revision string) string {
		var id string
		if err := db.QueryRow(`SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1`, revision).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	steps := []struct {
		sql  string
		args []any
	}{
		{`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE declaration_id=$1 AND purpose='display'`, []any{declaration(revoked.ContentRevisionID)}},
		// A revision derived from revoked content loses its required grant.
		{`INSERT INTO creative_content_required_grants(account_id,content_revision_id,declaration_id) VALUES('library-a',$1,$2)`, []any{dependent.ContentRevisionID, declaration(revoked.ContentRevisionID)}},
		{`UPDATE creative_rights_declarations SET source_class='unknown_class' WHERE id=$1`, []any{declaration(undeclared.ContentRevisionID)}},
		{`UPDATE creative_content_revisions SET state='deleting' WHERE id=$1`, []any{deleting.ContentRevisionID}},
	}
	for _, s := range steps {
		if _, err := db.Exec(s.sql, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{ok.ContentRevisionID, revoked.ContentRevisionID, dependent.ContentRevisionID, undeclared.ContentRevisionID, deleting.ContentRevisionID, foreign.ContentRevisionID}
	err := a.WithReadSnapshot(t.Context(), func(tx store.ReadTxAccountScope) error {
		readable, err := creativelibrary.ReadableAssetsForTest(t.Context(), tx)
		if err != nil {
			return err
		}
		guard, err := creativecontent.ReadDisplayBatch(t.Context(), tx, ids)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if readable[id] != (guard[id].Err == nil) {
				t.Errorf("revision %s: condition=%v guard=%v", id, readable[id], guard[id].Err)
			}
		}
		if !readable[ok.ContentRevisionID] || readable[foreign.ContentRevisionID] {
			t.Errorf("baseline wrong: %v", readable)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The keyword body match follows the same condition.
	if n := search(t, a, creativelibrary.Search{Q: "100%"}).TotalCount; n != 1 {
		t.Fatal("body matched unreadable content", n)
	}
}

func TestEmptyTrashPurgesExactlyTheConfirmedBatch(t *testing.T) {
	db, a, _ := setup(t)
	createAsset(t, a, "keep", nil)
	one, two, three := createAsset(t, a, "one", nil), createAsset(t, a, "two", nil), createAsset(t, a, "three", nil)
	trash := func(id string, revision creativeops.Revision) {
		t.Helper()
		r, err := creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: id, Revision: revision}))
		_ = decode[creativelibrary.LibraryResult](t, r, err)
	}
	trash(one.ID, 1)
	trash(two.ID, 1)
	// The dialog read the count and revision; then another window trashed one more.
	seen := search(t, a, creativelibrary.Search{View: "trash"})
	trash(three.ID, 1)
	_, err := creativelibrary.EmptyTrash(t.Context(), a, cmd(t, creativelibrary.EmptyTrashInput{ExpectedLibraryRevision: seen.LibraryRevision}))
	if !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal("stale trash emptied", err)
	}
	// ...or restored one.
	seen = search(t, a, creativelibrary.Search{View: "trash"})
	r, err := creativelibrary.Restore(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: three.ID, Revision: 2}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	_, err = creativelibrary.EmptyTrash(t.Context(), a, cmd(t, creativelibrary.EmptyTrashInput{ExpectedLibraryRevision: seen.LibraryRevision}))
	if !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal("stale trash emptied after restore", err)
	}
	if n := search(t, a, creativelibrary.Search{View: "trash"}).TotalCount; n != 2 {
		t.Fatal("a refused empty deleted something", n)
	}
	// A node on a canvas still owns one of the trashed contents.
	if _, err = db.Exec(`INSERT INTO creative_projects(id,account_id,name) VALUES('p','library-a','test'); INSERT INTO creative_canvases(id,account_id,project_id,name) VALUES('c','library-a','p','test')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,content_id,content_revision_id) SELECT 'n',account_id,'c','core.text',0,0,content_id,content_revision_id FROM creative_assets WHERE id=$1`, one.ID); err != nil {
		t.Fatal(err)
	}
	seen = search(t, a, creativelibrary.Search{View: "trash"})
	command := cmd(t, creativelibrary.EmptyTrashInput{ExpectedLibraryRevision: seen.LibraryRevision})
	r, err = creativelibrary.EmptyTrash(t.Context(), a, command)
	result := decode[creativelibrary.EmptyTrashResult](t, r, err)
	if result.PurgedCount != 2 || result.LibraryRevision != seen.LibraryRevision+1 {
		t.Fatalf("empty %+v", result)
	}
	replay, err := creativelibrary.EmptyTrash(t.Context(), a, command)
	if err != nil || !sameJSON(t, replay.Outcome.Response, r.Outcome.Response) {
		t.Fatal("replay differs", err)
	}
	if search(t, a, creativelibrary.Search{View: "trash"}).TotalCount != 0 || search(t, a, creativelibrary.Search{}).TotalCount != 2 {
		t.Fatal("wrong entries purged")
	}
	if _, err = creativecontent.Read(t.Context(), a, one.ContentRevisionID); err != nil {
		t.Fatal("emptying the trash destroyed node content", err)
	}
	// An empty bin is a no-op that keeps the revision.
	seen = search(t, a, creativelibrary.Search{View: "trash"})
	r, err = creativelibrary.EmptyTrash(t.Context(), a, cmd(t, creativelibrary.EmptyTrashInput{ExpectedLibraryRevision: seen.LibraryRevision}))
	if result = decode[creativelibrary.EmptyTrashResult](t, r, err); result.PurgedCount != 0 || result.LibraryRevision != seen.LibraryRevision {
		t.Fatalf("empty bin %+v", result)
	}
}

func TestGroupDeletionRestoresTreeAndMembers(t *testing.T) {
	_, a, b := setup(t)
	group := func(name string, parent *string, hierarchy creativeops.Revision) creativelibrary.LibraryResult {
		t.Helper()
		r, err := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: name, ParentID: parent, HierarchyRevision: hierarchy}))
		return decode[creativelibrary.LibraryResult](t, r, err)
	}
	root := group("root", nil, 1)
	other := group("other", nil, root.HierarchyRevision)
	c1 := group("c1", &root.ID, other.HierarchyRevision)
	c2 := group("c2", &root.ID, c1.HierarchyRevision)
	r, err := creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "x", Content: textDraft("x"), GroupIDs: []string{root.ID}}))
	x := decode[creativelibrary.AssetResult](t, r, err)
	r, err = creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "y", Content: textDraft("y"), GroupIDs: []string{root.ID, c1.ID}}))
	y := decode[creativelibrary.AssetResult](t, r, err)
	groups, err := creativelibrary.ListGroups(t.Context(), a)
	if err != nil || groups.AssetCounts[root.ID] != 2 || groups.AssetCounts[c1.ID] != 1 || groups.AssetCounts[c2.ID] != 0 || groups.AssetCounts[other.ID] != 0 {
		t.Fatalf("counts %+v %v", groups.AssetCounts, err)
	}
	before := groups.Items

	r, err = creativelibrary.DeleteGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: root.ID, ExpectedRevision: 1, HierarchyRevision: c2.HierarchyRevision}))
	deleted := decode[creativelibrary.LibraryResult](t, r, err)
	if deleted.DeletionID == "" {
		t.Fatal("no deletion id")
	}
	// Filing in the meantime is not a structural change and survives the undo.
	r, err = creativelibrary.Organize(t.Context(), a, cmd(t, creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: x.ID, Revision: 2}}, AddGroups: []string{other.ID}}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if _, err = creativelibrary.RestoreGroup(t.Context(), b, cmd(t, creativelibrary.RestoreGroupInput{DeletionID: deleted.DeletionID})); !errors.Is(err, creativelibrary.ErrNotFound) {
		t.Fatal("foreign restore", err)
	}
	command := cmd(t, creativelibrary.RestoreGroupInput{DeletionID: deleted.DeletionID})
	r, err = creativelibrary.RestoreGroup(t.Context(), a, command)
	restored := decode[creativelibrary.LibraryResult](t, r, err)
	if restored.ID != root.ID || restored.HierarchyRevision != deleted.HierarchyRevision+1 {
		t.Fatalf("restore %+v", restored)
	}
	groups, err = creativelibrary.ListGroups(t.Context(), a)
	if err != nil || len(groups.Items) != len(before) {
		t.Fatal(err)
	}
	for i, g := range groups.Items {
		w := before[i]
		if g.ID != w.ID || parentKey(g.ParentID) != parentKey(w.ParentID) || g.Position != w.Position || g.Name != w.Name {
			t.Fatalf("tree differs at %d: %+v want %+v", i, g, w)
		}
	}
	if gx := current(t, a, x.ID).GroupIDs; !slices.Contains(gx, root.ID) || !slices.Contains(gx, other.ID) {
		t.Fatalf("x memberships %v", gx)
	}
	if gy := current(t, a, y.ID).GroupIDs; len(gy) != 2 {
		t.Fatalf("y memberships %v", gy)
	}
	replay, err := creativelibrary.RestoreGroup(t.Context(), a, command)
	if err != nil || !sameJSON(t, replay.Outcome.Response, r.Outcome.Response) {
		t.Fatal("replay differs", err)
	}
	if _, err = creativelibrary.RestoreGroup(t.Context(), a, cmd(t, creativelibrary.RestoreGroupInput{DeletionID: deleted.DeletionID})); !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal("restored twice", err)
	}

	// A structural change after the delete blocks its undo.
	groups, _ = creativelibrary.ListGroups(t.Context(), a)
	r, err = creativelibrary.DeleteGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: c2.ID, ExpectedRevision: 3, HierarchyRevision: groups.HierarchyRevision}))
	again := decode[creativelibrary.LibraryResult](t, r, err)
	group("later", nil, again.HierarchyRevision)
	if _, err = creativelibrary.RestoreGroup(t.Context(), a, cmd(t, creativelibrary.RestoreGroupInput{DeletionID: again.DeletionID})); !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal("restore over a newer structure", err)
	}

	// Trashed assets leave the counts.
	r, err = creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: y.ID, Revision: current(t, a, y.ID).Revision}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	groups, _ = creativelibrary.ListGroups(t.Context(), a)
	if groups.AssetCounts[root.ID] != 1 || groups.AssetCounts[c1.ID] != 0 {
		t.Fatalf("counts after trash %+v", groups.AssetCounts)
	}
}

// sameJSON compares receipts by value; a replay comes back from JSONB storage.
func sameJSON(t *testing.T, x, y []byte) bool {
	t.Helper()
	var u, v any
	if json.Unmarshal(x, &u) != nil || json.Unmarshal(y, &v) != nil {
		t.Fatal("receipt is not JSON")
	}
	return reflect.DeepEqual(u, v)
}

func parentKey(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestUsageReportsCountAndQuota(t *testing.T) {
	db, a, _ := setup(t)
	one := createAsset(t, a, "one", nil)
	createAsset(t, a, "two", nil)
	r, err := creativelibrary.Trash(t.Context(), a, cmd(t, creativelibrary.AssetVersion{ID: one.ID, Revision: 1}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	u, err := creativelibrary.GetUsage(t.Context(), a, 123)
	if err != nil || u.AssetCount != 1 || u.StoredBytes != 0 || u.LimitBytes != 123 {
		t.Fatalf("no quota row %+v %v", u, err)
	}
	if _, err = db.Exec(`INSERT INTO creative_media_quotas(account_id,stored_bytes,limit_bytes) VALUES('library-a',40,500)`); err != nil {
		t.Fatal(err)
	}
	u, err = creativelibrary.GetUsage(t.Context(), a, 123)
	if err != nil || u.StoredBytes != 40 || u.LimitBytes != 500 {
		t.Fatalf("quota %+v %v", u, err)
	}
}

// V6-016 AC-02: names sort by code point after NFKC, lower case and trimming,
// so digits come before Latin letters before Chinese, and pages stay stable.
func TestNameSortIsCodePointOrderAcrossPages(t *testing.T) {
	_, a, _ := setup(t)
	for _, title := range []string{"张三", "  b ", "阿", "Ｂ2", "10", "2", "a"} {
		createAsset(t, a, title, nil)
	}
	want := []string{"10", "2", "a", "  b ", "Ｂ2", "张三", "阿"}
	got := []string{}
	cursor := ""
	for {
		page, err := creativelibrary.SearchAssets(t.Context(), a, creativelibrary.Search{Sort: "name", Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			got = append(got, item.Title)
		}
		if cursor = page.NextCursor; cursor == "" {
			break
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("order %q want %q", got, want)
	}
}

// An undo that would push an asset past 100 groups is refused as a whole:
// nothing is restored and the same deletion can still be undone later.
func TestGroupRestoreRefusesToDropMembersAtTheGroupLimit(t *testing.T) {
	_, a, _ := setup(t)
	hierarchy := creativeops.Revision(1)
	others := []string{}
	for i := range 100 {
		r, err := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "g" + strconv.Itoa(i), HierarchyRevision: hierarchy}))
		g := decode[creativelibrary.LibraryResult](t, r, err)
		hierarchy = g.HierarchyRevision
		others = append(others, g.ID)
	}
	r, err := creativelibrary.CreateGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{Name: "target", HierarchyRevision: hierarchy}))
	target := decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.CreateAsset(t.Context(), a, cmd(t, creativelibrary.CreateAssetInput{Title: "x", Content: textDraft("x"), GroupIDs: []string{target.ID}}))
	x := decode[creativelibrary.AssetResult](t, r, err)
	r, err = creativelibrary.DeleteGroup(t.Context(), a, cmd(t, creativelibrary.GroupInput{ID: target.ID, ExpectedRevision: 1, HierarchyRevision: target.HierarchyRevision}))
	deleted := decode[creativelibrary.LibraryResult](t, r, err)
	// Filing is not structural, so the undo is still possible — but x is now full.
	r, err = creativelibrary.Organize(t.Context(), a, cmd(t, creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: x.ID, Revision: 2}}, AddGroups: others}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	before, _ := creativelibrary.ListGroups(t.Context(), a)
	if _, err = creativelibrary.RestoreGroup(t.Context(), a, cmd(t, creativelibrary.RestoreGroupInput{DeletionID: deleted.DeletionID})); !errors.Is(err, creativelibrary.ErrGroupRestoreLimit) {
		t.Fatal("over-limit restore", err)
	}
	after, _ := creativelibrary.ListGroups(t.Context(), a)
	if len(after.Items) != len(before.Items) || after.HierarchyRevision != before.HierarchyRevision || current(t, a, x.ID).Revision != 3 {
		t.Fatal("refused restore changed something")
	}
	// Make room; the same deletion can now be undone in full.
	r, err = creativelibrary.Organize(t.Context(), a, cmd(t, creativelibrary.OrganizeInput{Assets: []creativelibrary.AssetVersion{{ID: x.ID, Revision: 3}}, RemoveGroups: others[:1]}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	r, err = creativelibrary.RestoreGroup(t.Context(), a, cmd(t, creativelibrary.RestoreGroupInput{DeletionID: deleted.DeletionID}))
	_ = decode[creativelibrary.LibraryResult](t, r, err)
	if !slices.Contains(current(t, a, x.ID).GroupIDs, target.ID) {
		t.Fatal("membership not restored")
	}
}
