package creativescene_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func saveMaterial(t *testing.T, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, creativelibrary.AssetResult, error) {
	t.Helper()
	receipt, err := creativescene.SaveMaterialToLibrary(t.Context(), scope, c)
	if err != nil {
		return receipt, creativelibrary.AssetResult{}, err
	}
	var out creativelibrary.AssetResult
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	return receipt, out, nil
}

// heldTextures are the images a revision holds as textures.
func heldTextures(t *testing.T, f fixture, holder string) []string {
	t.Helper()
	rows, err := f.db.Query(`SELECT content_revision_id FROM creative_content_revision_refs WHERE holder_revision_id=$1 AND role='texture' ORDER BY 1`, holder)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	return out
}

func materialPayload(t *testing.T, scope store.AccountScope, id string) (creativecontent.Revision, creativescene.MaterialPayload) {
	t.Helper()
	r, err := creativecontent.Read(t.Context(), scope, id)
	if err != nil {
		t.Fatal(err)
	}
	var p creativescene.MaterialPayload
	if err := json.Unmarshal(r.Payload.Material, &p); err != nil {
		t.Fatal(err)
	}
	return r, p
}

// A slot's material is copied into the library as new material content that
// holds the slot's textures. The same slot saved again, from anywhere, only
// adds groups and leaves the thumbnail upload alone.
func TestSaveMaterialToLibrary(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	leaf, orm := mediaRevision(t, f, f.a, "image"), mediaRevision(t, f, f.a, "image")
	slab := principled(map[string]any{"maps": map[string]any{"base_color": texture(leaf), "roughness": texture(orm), "ao": map[string]any{"content_revision_id": orm, "channel": "g"}},
		"mapping": map[string]any{"size_m": 2}, "alpha_mode": "clip"})
	id, twin := objectID(1), objectID(2)
	addObjects(t, f.a, doc, slotted(box(id, nil, 1), map[string]any{"main": slab}), slotted(box(twin, nil, 2), map[string]any{"main": slab}))
	v := readScene(t, f.a, doc)
	group := libraryGroup(t, f.a, "地面", 1)
	thumb := stage(t, f, f.a, "image")
	thumbBlob := stagedUpload(t, f, thumb).blob

	c := command(t, creativescene.SaveMaterialInput{DocumentID: doc, ObjectID: id, SlotID: "main", ExpectedObjectRev: contentRev(t, v, id), ThumbnailUploadID: thumb,
		Target: creativelibrary.ImportTarget{Title: "旧石板地面", GroupIDs: []string{group.ID}}})
	receipt, created, err := saveMaterial(t, f.a, c)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome.HTTPStatus != 201 || created.Reused {
		t.Fatalf("created %d %+v", receipt.Outcome.HTTPStatus, created)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, created.ID)
	if err != nil || asset.Kind != "material" || asset.ContentRevisionID != created.ContentRevisionID || asset.Title != "旧石板地面" {
		t.Fatalf("asset %+v %v", asset, err)
	}
	r, p := materialPayload(t, f.a, created.ContentRevisionID)
	if stored := v.object(t, id).MaterialSlots["main"]; !reflect.DeepEqual(p.Slot, stored) {
		t.Fatalf("saved slot %+v, scene has %+v", p.Slot, stored)
	}
	if p.Source != (creativescene.MaterialSource{DocumentID: doc, ObjectID: id, SlotID: "main", Revision: v.Revision}) {
		t.Fatalf("source %+v", p.Source)
	}
	if blobOf(t, r, "thumbnail") != thumbBlob || len(r.Media) != 1 {
		t.Fatalf("material media %+v", r.Media)
	}
	if got := heldTextures(t, f, created.ContentRevisionID); !reflect.DeepEqual(got, sorted([]string{leaf, orm})) {
		t.Fatalf("held textures %v", got)
	}
	if got := stagedUpload(t, f, thumb); got.state != "applied" || got.kind != "asset" || got.target != created.ID {
		t.Fatalf("thumbnail upload %+v", got)
	}
	if readScene(t, f.a, doc).Revision != v.Revision {
		t.Fatal("saving wrote the scene")
	}

	replay, again, err := saveMaterial(t, f.a, c)
	if err != nil || replay.Outcome.HTTPStatus != 201 || again.ID != created.ID {
		t.Fatalf("replay %d %+v %v", replay.Outcome.HTTPStatus, again, err)
	}

	save := func(object, upload, title string) (int, creativelibrary.AssetResult) {
		t.Helper()
		v := readScene(t, f.a, doc)
		receipt, out, err := saveMaterial(t, f.a, command(t, creativescene.SaveMaterialInput{DocumentID: doc, ObjectID: object, SlotID: "main", ExpectedObjectRev: contentRev(t, v, object),
			ThumbnailUploadID: upload, Target: creativelibrary.ImportTarget{Title: title}}))
		if err != nil {
			t.Fatal(err)
		}
		return receipt.Outcome.HTTPStatus, out
	}
	other := stage(t, f, f.a, "image")
	if status, reused := save(twin, other, "新标题"); status != 200 || !reused.Reused || reused.ID != created.ID || reused.Title != "旧石板地面" {
		t.Fatalf("reused %d %+v", status, reused)
	}
	if got := stagedUpload(t, f, other); got.state != "pending" {
		t.Fatalf("a reused save took the upload: %+v", got)
	}

	// Another assignment is another asset.
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(twin)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": twin, "material_slots": map[string]any{"main": principled(map[string]any{"maps": map[string]any{"base_color": texture(leaf)}})}}}}}})
	if status, out := save(twin, other, "树叶"); status != 201 || out.ID == created.ID {
		t.Fatalf("another assignment %d %+v", status, out)
	}

	// Using the asset copies its slot into a slot, where it is the scene's.
	third := objectID(3)
	addObjects(t, f.a, doc, box(third, nil, 3))
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(third)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": third, "material_slots": map[string]any{"floor": p.Slot}}}}}})
	if got := readScene(t, f.a, doc).object(t, third).MaterialSlots["floor"]; !reflect.DeepEqual(got, p.Slot) {
		t.Fatalf("placed slot %+v", got)
	}

	// A same material that can no longer be shown is passed over: a new one, not a failure.
	if _, err := f.db.Exec(`UPDATE creative_content_revisions SET state='deleting' WHERE id=$1`, created.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	if status, out := save(id, stage(t, f, f.a, "image"), "重存"); status != 201 || out.Reused || out.ID == created.ID {
		t.Fatalf("over an unusable copy %d %+v", status, out)
	}
}

func TestSaveMaterialToLibraryRejections(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	image := mediaRevision(t, f, f.a, "image")
	id, light := objectID(1), objectID(2)
	addObjects(t, f.a, doc, slotted(box(id, nil, 1), map[string]any{"main": principled(map[string]any{"maps": map[string]any{"normal": texture(image)}})}),
		withContent(light, "light", 2, map[string]any{"type": "point"}))
	v := readScene(t, f.a, doc)
	rev := contentRev(t, v, id)
	thumb := stage(t, f, f.a, "image")
	input := func(change func(*creativescene.SaveMaterialInput)) creativeops.Command {
		in := creativescene.SaveMaterialInput{DocumentID: doc, ObjectID: id, SlotID: "main", ExpectedObjectRev: rev, ThumbnailUploadID: thumb, Target: creativelibrary.ImportTarget{Title: "石板"}}
		change(&in)
		return command(t, in)
	}

	_, _, err := saveMaterial(t, f.a, input(func(in *creativescene.SaveMaterialInput) { in.ExpectedObjectRev = rev + 1 }))
	var conflict *creativedocument.ConflictError
	if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Unit != content(id) || *conflict.Conflicts[0].CurrentRev != rev {
		t.Fatalf("stale rev: %v", err)
	}
	_, _, err = saveMaterial(t, f.a, input(func(in *creativescene.SaveMaterialInput) { in.SlotID = "trim" }))
	var empty *creativescene.SlotEmptyError
	if !errors.Is(err, creativescene.ErrMaterialSlotEmpty) || !errors.As(err, &empty) || empty.ObjectID != id || empty.SlotID != "trim" {
		t.Fatalf("empty slot: %v", err)
	}
	for name, tc := range map[string]struct {
		scope  store.AccountScope
		change func(*creativescene.SaveMaterialInput)
		want   error
	}{
		"a light": {f.a, func(in *creativescene.SaveMaterialInput) {
			in.ObjectID, in.ExpectedObjectRev = light, contentRev(t, v, light)
		}, creativeops.ErrValidation},
		"missing object":    {f.a, func(in *creativescene.SaveMaterialInput) { in.ObjectID = objectID(9) }, creativedocument.ErrNotFound},
		"missing scene":     {f.a, func(in *creativescene.SaveMaterialInput) { in.DocumentID = "cwdoc_missing" }, creativedocument.ErrNotFound},
		"missing group":     {f.a, func(in *creativescene.SaveMaterialInput) { in.Target.GroupIDs = []string{"cagr_missing"} }, creativelibrary.ErrNotFound},
		"other account":     {f.b, func(*creativescene.SaveMaterialInput) {}, creativedocument.ErrNotFound},
		"empty title":       {f.a, func(in *creativescene.SaveMaterialInput) { in.Target.Title = " " }, creativeops.ErrValidation},
		"bad slot id":       {f.a, func(in *creativescene.SaveMaterialInput) { in.SlotID = "" }, creativeops.ErrValidation},
		"no thumbnail":      {f.a, func(in *creativescene.SaveMaterialInput) { in.ThumbnailUploadID = "" }, creativeops.ErrValidation},
		"missing upload":    {f.a, func(in *creativescene.SaveMaterialInput) { in.ThumbnailUploadID = "ccup_missing" }, creativescene.ErrUploadUnavailable},
		"thumbnail a model": {f.a, func(in *creativescene.SaveMaterialInput) { in.ThumbnailUploadID = stage(t, f, f.a, "model") }, creativeops.ErrValidation},
		"thumbnail not a PNG": {f.a, func(in *creativescene.SaveMaterialInput) {
			in.ThumbnailUploadID = stage(t, f, f.a, "image")
			if _, err := f.db.Exec(`UPDATE creative_blobs SET mime='image/jpeg' WHERE id=$1`, stagedUpload(t, f, in.ThumbnailUploadID).blob); err != nil {
				t.Fatal(err)
			}
		}, creativeops.ErrValidation},
	} {
		if _, _, err := saveMaterial(t, tc.scope, input(tc.change)); !errors.Is(err, tc.want) {
			t.Errorf("%s: want %v, got %v", name, tc.want, err)
		}
	}

	revokeDisplay(t, f, image)
	_, _, err = saveMaterial(t, f.a, input(func(*creativescene.SaveMaterialInput) {}))
	if named := wantField(t, err, creativescene.ErrImageUnavailable, "maps.normal"); named != image {
		t.Fatalf("names %s", named)
	}
	if _, err := creativedocument.Trash(t.Context(), f.a, command(t, map[string]string{"document_id": doc})); err != nil {
		t.Fatal(err)
	}
	if _, _, err := saveMaterial(t, f.a, input(func(*creativescene.SaveMaterialInput) {})); !errors.Is(err, creativedocument.ErrTrashed) {
		t.Fatalf("trashed scene: %v", err)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_assets`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d assets written by rejected saves %v", n, err)
	}
	if got := stagedUpload(t, f, thumb); got.state != "pending" {
		t.Fatalf("rejected saves took the upload: %+v", got)
	}
}

// A library model copy holds the textures its materials use, so placing it
// after the originals are gone still finds them; one no longer displayable
// refuses the save.
func TestSavedModelHoldsItsTextures(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	newPillar(t, f, doc, id, 1)
	image := mediaRevision(t, f, f.a, "image")
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(id)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "material_slots": map[string]any{"stone": principled(map[string]any{"maps": map[string]any{"base_color": texture(image)}})}}}}}})
	v := readScene(t, f.a, doc)
	save := func() (creativelibrary.AssetResult, error) {
		_, out, err := saveModel(t, f.a, command(t, creativescene.SaveModelInput{DocumentID: doc, ObjectID: id, ExpectedObjectRev: contentRev(t, v, id),
			ThumbnailUploadID: stage(t, f, f.a, "image"), Target: creativelibrary.ImportTarget{Title: "砖柱"}}))
		return out, err
	}
	created, err := save()
	if err != nil {
		t.Fatal(err)
	}
	if got := heldTextures(t, f, created.ContentRevisionID); !reflect.DeepEqual(got, []string{image}) {
		t.Fatalf("copy holds %v", got)
	}
	if _, p := readRevision(t, f.a, created.ContentRevisionID); p.Materials["stone"].Maps == nil || p.Materials["stone"].Maps.BaseColor.ContentRevisionID != image {
		t.Fatalf("copy materials %+v", p.Materials)
	}

	revokeDisplay(t, f, image)
	_, err = save()
	if named := wantField(t, err, creativescene.ErrImageUnavailable, "materials.stone.maps.base_color"); named != image {
		t.Fatalf("names %s", named)
	}
}
