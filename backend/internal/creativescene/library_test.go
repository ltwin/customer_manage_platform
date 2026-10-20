package creativescene_test

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func contentRev(t *testing.T, v sceneView, id string) creativeops.Revision {
	t.Helper()
	n, err := strconv.ParseInt(v.UnitRevs[content(id)], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return creativeops.Revision(n)
}

func saveModel(t *testing.T, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, creativelibrary.AssetResult, error) {
	t.Helper()
	receipt, err := creativescene.SaveModelToLibrary(t.Context(), scope, c)
	if err != nil {
		return receipt, creativelibrary.AssetResult{}, err
	}
	var out creativelibrary.AssetResult
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	return receipt, out, nil
}

func libraryGroup(t *testing.T, scope store.AccountScope, name string, hierarchy creativeops.Revision) creativelibrary.LibraryResult {
	t.Helper()
	r, err := creativelibrary.CreateGroup(t.Context(), scope, command(t, creativelibrary.GroupInput{Name: name, HierarchyRevision: hierarchy}))
	if err != nil {
		t.Fatal(err)
	}
	var out creativelibrary.LibraryResult
	if err := json.Unmarshal(r.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSaveModelToLibraryCreatesThenReuses(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	placed := newPillar(t, f, doc, id, 1)
	addObjects(t, f.a, doc, instance(objectID(2), 2, map[string]any{"content_revision_id": placed.ContentRevisionID}))
	v := readScene(t, f.a, doc)
	first := libraryGroup(t, f.a, "柱子", 1)
	second := libraryGroup(t, f.a, "哥特", first.HierarchyRevision)

	c := command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, v, id),
		Target: creativelibrary.ImportTarget{Title: "哥特束柱", GroupIDs: []string{first.ID}}})
	receipt, created, err := saveModel(t, f.a, c)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome.HTTPStatus != 201 || created.Reused || created.ContentRevisionID != placed.ContentRevisionID {
		t.Fatalf("created %d %+v", receipt.Outcome.HTTPStatus, created)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, created.ID)
	if err != nil || asset.Kind != "model" || asset.ContentRevisionID != placed.ContentRevisionID || asset.Title != "哥特束柱" {
		t.Fatalf("asset %+v %v", asset, err)
	}
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatal("saving wrote the scene")
	}

	// The same key replays the same answer.
	replay, again, err := saveModel(t, f.a, c)
	if err != nil || replay.Outcome.HTTPStatus != 201 || again.ID != created.ID || again.LibraryRevision != created.LibraryRevision {
		t.Fatalf("replay %d %+v %v", replay.Outcome.HTTPStatus, again, err)
	}

	// Another instance of the same revision reuses the asset and only adds the group.
	other := objectID(2)
	receipt, reused, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: other, ExpectedObjectRev: contentRev(t, v, other),
		Target: creativelibrary.ImportTarget{Title: "新标题", GroupIDs: []string{second.ID}}}))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome.HTTPStatus != 200 || !reused.Reused || reused.ID != created.ID || reused.Title != "哥特束柱" {
		t.Fatalf("reused %d %+v", receipt.Outcome.HTTPStatus, reused)
	}
	asset, err = creativelibrary.GetAsset(t.Context(), f.a, created.ID)
	if err != nil || len(asset.GroupIDs) != 2 {
		t.Fatalf("groups %+v %v", asset.GroupIDs, err)
	}
}

func TestSaveModelToLibraryRejections(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id, boxID := objectID(1), objectID(2)
	placed := newPillar(t, f, doc, id, 1)
	addObjects(t, f.a, doc, box(boxID, nil, 2))
	v := readScene(t, f.a, doc)
	rev := contentRev(t, v, id)
	input := func(change func(*creativescene.SaveModelInput)) creativeops.Command {
		in := creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: rev, Target: creativelibrary.ImportTarget{Title: "柱"}}
		change(&in)
		return command(t, in)
	}
	assets := func() int {
		t.Helper()
		var n int
		if err := f.db.QueryRow(`SELECT count(*) FROM creative_assets`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	_, _, err := saveModel(t, f.a, input(func(in *creativescene.SaveModelInput) { in.ExpectedObjectRev = rev + 1 }))
	var conflict *creativedocument.ConflictError
	if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Unit != content(id) ||
		*conflict.Conflicts[0].ExpectedRev != rev+1 || conflict.Conflicts[0].CurrentRev == nil || *conflict.Conflicts[0].CurrentRev != rev {
		t.Fatalf("stale rev: %v", err)
	}
	cases := map[string]struct {
		scope  store.AccountScope
		change func(*creativescene.SaveModelInput)
		want   error
	}{
		"not a model instance": {f.a, func(in *creativescene.SaveModelInput) {
			in.ObjectID, in.ExpectedObjectRev = boxID, contentRev(t, v, boxID)
		}, creativeops.ErrValidation},
		"missing object": {f.a, func(in *creativescene.SaveModelInput) { in.ObjectID = objectID(9) }, creativedocument.ErrNotFound},
		"missing scene":  {f.a, func(in *creativescene.SaveModelInput) { in.DocumentID = "cwdoc_missing" }, creativedocument.ErrNotFound},
		"missing group":  {f.a, func(in *creativescene.SaveModelInput) { in.Target.GroupIDs = []string{"cagr_missing"} }, creativelibrary.ErrNotFound},
		"other account":  {f.b, func(*creativescene.SaveModelInput) {}, creativedocument.ErrNotFound},
		"empty title":    {f.a, func(in *creativescene.SaveModelInput) { in.Target.Title = " " }, creativeops.ErrValidation},
	}
	for name, tc := range cases {
		if _, _, err := saveModel(t, tc.scope, input(tc.change)); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", name, tc.want, err)
		}
	}

	// A revision that lost its generator is not a model a scene can use.
	if _, err := f.db.Exec(`UPDATE creative_content_revisions SET payload = payload #- '{model,generator}' WHERE id=$1`, placed.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := saveModel(t, f.a, input(func(*creativescene.SaveModelInput) {})); !errors.Is(err, creativescene.ErrModelUnavailable) {
		t.Fatalf("staged form: %v", err)
	}
	if _, err := creativedocument.Trash(t.Context(), f.a, command(t, map[string]string{"document_id": doc})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := saveModel(t, f.a, input(func(*creativescene.SaveModelInput) {})); !errors.Is(err, creativedocument.ErrTrashed) {
		t.Fatalf("trashed scene: %v", err)
	}
	if n := assets(); n != 0 {
		t.Fatalf("%d assets written by rejected saves", n)
	}
}
