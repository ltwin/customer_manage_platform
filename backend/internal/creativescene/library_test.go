package creativescene_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
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

// slotted gives an object these material slots.
func slotted(o map[string]any, slots map[string]any) map[string]any {
	o["material_slots"] = slots
	return o
}

func material(preset string) map[string]any {
	return map[string]any{"preset": preset, "params": map[string]any{}}
}

// readRevision reads a model revision with its code and media.
func readRevision(t *testing.T, scope store.AccountScope, id string) (creativecontent.Revision, creativescene.ModelPayload) {
	t.Helper()
	r, err := creativecontent.Read(t.Context(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	var p creativescene.ModelPayload
	if err := json.Unmarshal(r.Payload.Model, &p); err != nil {
		t.Fatal(err)
	}
	return r, p
}

func blobOf(t *testing.T, r creativecontent.Revision, role string) string {
	t.Helper()
	for _, m := range r.Media {
		if m.Role == role {
			return m.BlobID
		}
	}
	t.Fatalf("revision %s has no %s", r.ID, role)
	return ""
}

// staged is a staged upload's state and what adopted it.
type staged struct{ state, kind, target, blob string }

func stagedUpload(t *testing.T, f fixture, upload string) staged {
	t.Helper()
	var s staged
	var kind, target, blob sql.NullString
	if err := f.db.QueryRow(`SELECT c.state, c.adopted_target_kind, c.adopted_target_id, o.blob_id FROM creative_upload_candidates c
 LEFT JOIN creative_content_objects o ON o.account_id=c.account_id AND o.content_revision_id=c.content_revision_id AND o.role='original'
 WHERE c.upload_id=$1`, upload).Scan(&s.state, &kind, &target, &blob); err != nil {
		t.Fatal(err)
	}
	s.kind, s.target, s.blob = kind.String, target.String, blob.String
	return s
}

// An instance's model is copied into the library with its materials: new
// content over the same GLB and the uploaded thumbnail. Saving the same model
// with the same materials again, from any instance, only adds groups.
func TestSaveModelToLibraryCopiesModelAndMaterials(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id, twin := objectID(1), objectID(2)
	wood, paint := material("wood@1"), material("paint@1")
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect("objects"), Operations: []any{
		commit(t, "pillar", stagePair(t, f, f.a), pillarGenerator(2), basicManifest(t), nil),
		op{"op": "add_objects", "objects": []any{
			// legacy is a slot the model lacks; the copy leaves it out.
			slotted(instance(id, 1, map[string]any{"ref": "pillar"}), map[string]any{"stone": wood, "legacy": paint}),
			slotted(instance(twin, 2, map[string]any{"ref": "pillar"}), map[string]any{"stone": wood}),
		}},
	}})
	placed := receiptModels(t, res)["pillar"]
	v := readScene(t, f.a, doc)
	first := libraryGroup(t, f.a, "柱子", 1)
	second := libraryGroup(t, f.a, "哥特", first.HierarchyRevision)
	thumb := stage(t, f, f.a, "image")
	thumbBlob := stagedUpload(t, f, thumb).blob

	c := command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, v, id), ThumbnailUploadID: thumb,
		Target: creativelibrary.ImportTarget{Title: "哥特束柱", GroupIDs: []string{first.ID}}})
	receipt, created, err := saveModel(t, f.a, c)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome.HTTPStatus != 201 || created.Reused || created.ContentRevisionID == placed.ContentRevisionID {
		t.Fatalf("created %d %+v", receipt.Outcome.HTTPStatus, created)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, created.ID)
	if err != nil || asset.Kind != "model" || asset.ContentRevisionID != created.ContentRevisionID || asset.ContentID == placed.ContentID || asset.Title != "哥特束柱" {
		t.Fatalf("asset %+v %v", asset, err)
	}
	src, _ := readRevision(t, f.a, placed.ContentRevisionID)
	copied, p := readRevision(t, f.a, created.ContentRevisionID)
	if p.SourceRevisionID != placed.ContentRevisionID || len(p.Materials) != 1 || p.Materials["stone"].Preset != "wood@1" ||
		p.Generator == nil || p.Generator.Code != pillarCode || p.NameHint != "立柱" {
		t.Fatalf("copy payload %+v", p)
	}
	if blobOf(t, copied, "original") != blobOf(t, src, "original") || blobOf(t, copied, "thumbnail") != thumbBlob || blobOf(t, src, "thumbnail") == thumbBlob {
		t.Fatalf("copy media %+v, source %+v", copied.Media, src.Media)
	}
	if got := stagedUpload(t, f, thumb); got.state != "applied" || got.kind != "asset" || got.target != created.ID {
		t.Fatalf("thumbnail upload %+v", got)
	}
	if after := readScene(t, f.a, doc); after.Revision != v.Revision {
		t.Fatal("saving wrote the scene")
	}

	// The same key replays the same answer.
	replay, again, err := saveModel(t, f.a, c)
	if err != nil || replay.Outcome.HTTPStatus != 201 || again.ID != created.ID || again.LibraryRevision != created.LibraryRevision {
		t.Fatalf("replay %d %+v %v", replay.Outcome.HTTPStatus, again, err)
	}

	// The twin has the same model and materials: the asset only gains a group,
	// and its thumbnail upload stays staged.
	save := func(object, upload, title, group string) (int, creativelibrary.AssetResult) {
		t.Helper()
		v := readScene(t, f.a, doc)
		receipt, out, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: object, ExpectedObjectRev: contentRev(t, v, object),
			ThumbnailUploadID: upload, Target: creativelibrary.ImportTarget{Title: title, GroupIDs: []string{group}}}))
		if err != nil {
			t.Fatal(err)
		}
		return receipt.Outcome.HTTPStatus, out
	}
	other := stage(t, f, f.a, "image")
	status, reused := save(twin, other, "新标题", second.ID)
	if status != 200 || !reused.Reused || reused.ID != created.ID || reused.Title != "哥特束柱" {
		t.Fatalf("reused %d %+v", status, reused)
	}
	if asset, err = creativelibrary.GetAsset(t.Context(), f.a, created.ID); err != nil || len(asset.GroupIDs) != 2 {
		t.Fatalf("groups %+v %v", asset.GroupIDs, err)
	}
	if got := stagedUpload(t, f, other); got.state != "pending" {
		t.Fatalf("a reused save took the upload: %+v", got)
	}

	// Other materials are another asset.
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(twin)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": twin, "material_slots": map[string]any{"stone": paint}}}},
	}})
	status, painted := save(twin, other, "漆柱", second.ID)
	if status != 201 || painted.Reused || painted.ID == created.ID {
		t.Fatalf("other materials %d %+v", status, painted)
	}
	if _, p := readRevision(t, f.a, painted.ContentRevisionID); p.SourceRevisionID != placed.ContentRevisionID || p.Materials["stone"].Preset != "paint@1" {
		t.Fatalf("painted copy %+v", p)
	}

	// An instance placed from the library, saved with the asset's materials,
	// is the same asset: the copy names the scene's revision as its source.
	third := objectID(3)
	addObjects(t, f.a, doc, slotted(instance(third, 3, map[string]any{"content_revision_id": created.ContentRevisionID}), map[string]any{"stone": wood}))
	if status, out := save(third, stage(t, f, f.a, "image"), "又一次", first.ID); status != 200 || out.ID != created.ID {
		t.Fatalf("saved from a placed copy %d %+v", status, out)
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
	thumb := stage(t, f, f.a, "image")
	input := func(change func(*creativescene.SaveModelInput)) creativeops.Command {
		in := creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: rev, ThumbnailUploadID: thumb, Target: creativelibrary.ImportTarget{Title: "柱"}}
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
		"missing object":         {f.a, func(in *creativescene.SaveModelInput) { in.ObjectID = objectID(9) }, creativedocument.ErrNotFound},
		"missing scene":          {f.a, func(in *creativescene.SaveModelInput) { in.DocumentID = "cwdoc_missing" }, creativedocument.ErrNotFound},
		"missing group":          {f.a, func(in *creativescene.SaveModelInput) { in.Target.GroupIDs = []string{"cagr_missing"} }, creativelibrary.ErrNotFound},
		"other account":          {f.b, func(*creativescene.SaveModelInput) {}, creativedocument.ErrNotFound},
		"empty title":            {f.a, func(in *creativescene.SaveModelInput) { in.Target.Title = " " }, creativeops.ErrValidation},
		"no thumbnail":           {f.a, func(in *creativescene.SaveModelInput) { in.ThumbnailUploadID = "" }, creativeops.ErrValidation},
		"missing upload":         {f.a, func(in *creativescene.SaveModelInput) { in.ThumbnailUploadID = "ccup_missing" }, creativescene.ErrUploadUnavailable},
		"other account's upload": {f.a, func(in *creativescene.SaveModelInput) { in.ThumbnailUploadID = stage(t, f, f.b, "image") }, creativescene.ErrUploadUnavailable},
		"thumbnail not an image": {f.a, func(in *creativescene.SaveModelInput) { in.ThumbnailUploadID = stage(t, f, f.a, "model") }, creativeops.ErrValidation},
		"thumbnail not a PNG": {f.a, func(in *creativescene.SaveModelInput) {
			in.ThumbnailUploadID = stage(t, f, f.a, "image")
			if _, err := f.db.Exec(`UPDATE creative_blobs SET mime='image/jpeg' WHERE id=$1`, stagedUpload(t, f, in.ThumbnailUploadID).blob); err != nil {
				t.Fatal(err)
			}
		}, creativeops.ErrValidation},
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
	if got := stagedUpload(t, f, thumb); got.state != "pending" {
		t.Fatalf("rejected saves took the upload: %+v", got)
	}
}

// AC-4: the asset holds its revision. A scene that moved on, or is gone,
// leaves the saved model readable from the library.
func TestSavedModelOutlivesItsScene(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	placed := newPillar(t, f, doc, id, 1)
	v := readScene(t, f.a, doc)
	_, saved, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, v, id),
		ThumbnailUploadID: stage(t, f, f.a, "image"), Target: creativelibrary.ImportTarget{Title: "柱"}}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := creativedocument.Trash(t.Context(), f.a, command(t, map[string]string{"document_id": doc})); err != nil {
		t.Fatal(err)
	}
	if _, err := creativedocument.Purge(t.Context(), f.a, command(t, map[string]string{"document_id": doc})); err != nil {
		t.Fatal(err)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, saved.ID)
	if err != nil || asset.Unavailable || asset.Content == nil || asset.Content.ID != saved.ContentRevisionID {
		t.Fatalf("asset after its scene is gone: %+v %v", asset, err)
	}
	if _, p := readRevision(t, f.a, saved.ContentRevisionID); p.SourceRevisionID != placed.ContentRevisionID {
		t.Fatalf("the library copy is no longer readable: %+v", p)
	}
}

// A library copy belongs to the library: editing an instance placed from it,
// by the photographer or the assistant, forks a model of the scene's own and
// the copy keeps its one revision. Other models still gain revisions.
func TestLibraryCopyIsNeverContinued(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, &fakeWaiter{})
	doc := createScene(t, f.a)
	id, mine, theirs := objectID(1), objectID(2), objectID(3)
	placed := newPillar(t, f, doc, id, 1)
	_, saved, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, readScene(t, f.a, doc), id),
		ThumbnailUploadID: stage(t, f, f.a, "image"), Target: creativelibrary.ImportTarget{Title: "柱"}}))
	if err != nil {
		t.Fatal(err)
	}
	copied, _ := readRevision(t, f.a, saved.ContentRevisionID)
	addObjects(t, f.a, doc, instance(mine, 2, map[string]any{"content_revision_id": copied.ID}), instance(theirs, 3, map[string]any{"content_revision_id": copied.ID}))

	// The editor follows R4 and names the copy as the base.
	res := mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(mine)), Operations: []any{
		commit(t, "edit", stagePair(t, f, f.a), pillarGenerator(3), basicManifest(t), map[string]any{"base_content_id": copied.ContentID}),
		op{"op": "set_instance_model", "object_id": mine, "model": map[string]any{"ref": "edit"}},
	}})
	edited := receiptModels(t, res)["edit"]
	if edited.ContentID == copied.ContentID || edited.ContentID == placed.ContentID {
		t.Fatalf("the photographer's edit continued %s", edited.ContentID)
	}

	// So does the assistant's write_model, completed through the same commit.
	job, _, err := bakeJobInTx(t, f.a, creativescene.BakeInput{DocumentID: doc, RunID: "ccrn_agent", ToolCallID: "step-1", BasedOn: int64(readScene(t, f.a, doc).Revision),
		Code: pillarCode, Params: map[string]any{"height": 3}, Replace: &creativescene.ReplaceTarget{ObjectID: theirs}})
	if err != nil {
		t.Fatal(err)
	}
	if base := bakeRequest(t, job).Target.BaseContentID; base != copied.ContentID {
		t.Fatalf("the bake names %q as its base", base)
	}
	claimed := claimOne(t, f.a)
	resp, err := completeJob(t, f.a, uuid.NewString(), map[string]any{"job_id": job.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded", "bake": bakeOutput(stagePair(t, f, f.a), basicManifest(t))})
	if err != nil || resp.State != creativescene.JobSucceeded || resp.Result.Bake == nil {
		t.Fatalf("bake %+v %v", resp, err)
	}
	if got := resp.Result.Bake.ContentID; got == copied.ContentID || got == edited.ContentID {
		t.Fatalf("the assistant's edit continued %s", got)
	}
	var revisions int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_content_revisions WHERE content_id=$1`, copied.ContentID).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatalf("the library copy has %d revisions %v", revisions, err)
	}

	res = mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(id)), Operations: []any{
		commit(t, "again", stagePair(t, f, f.a), pillarGenerator(3), basicManifest(t), map[string]any{"base_content_id": placed.ContentID}),
		op{"op": "set_instance_model", "object_id": id, "model": map[string]any{"ref": "again"}},
	}})
	if got := receiptModels(t, res)["again"].ContentID; got != placed.ContentID {
		t.Fatalf("an ordinary model forked into %s", got)
	}
}

// Only a live asset counts as already saved: with the asset in the trash, the
// same model and materials are saved anew.
func TestSavingAModelWhoseAssetIsTrashed(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	newPillar(t, f, doc, id, 1)
	save := func() (int, creativelibrary.AssetResult) {
		t.Helper()
		receipt, out, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, readScene(t, f.a, doc), id),
			ThumbnailUploadID: stage(t, f, f.a, "image"), Target: creativelibrary.ImportTarget{Title: "柱"}}))
		if err != nil {
			t.Fatal(err)
		}
		return receipt.Outcome.HTTPStatus, out
	}
	_, first := save()
	if _, err := creativelibrary.Trash(t.Context(), f.a, command(t, creativelibrary.AssetVersion{ID: first.ID, Revision: first.Revision})); err != nil {
		t.Fatal(err)
	}
	if status, again := save(); status != 201 || again.Reused || again.ID == first.ID || again.ContentRevisionID == first.ContentRevisionID {
		t.Fatalf("saved with its asset in the trash: %d %+v", status, again)
	}
}
