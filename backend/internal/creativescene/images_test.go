package creativescene_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativereference"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// mediaRevision writes a ready revision of kind whose only holder is a
// revision hold, which the test may delete (hold-<revision id>). An image is
// 2048 × 1024 PNG.
func mediaRevision(t *testing.T, f fixture, scope store.AccountScope, kind string) string {
	t.Helper()
	account, blob := scope.AccountID(), "ccbl_"+uuid.NewString()
	mime := map[string]string{"image": "image/png", "video": "video/mp4", "model": "model/gltf-binary"}[kind]
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,width,height,state,verified_at)
 VALUES($1,$2,'local','creative-v2/'||$2||'/blobs/'||$1,'v1','sha256-'||repeat('1',64),100,$3,2048,1024,'ready',clock_timestamp())`, blob, account, mime); err != nil {
		t.Fatal(err)
	}
	draft := creativecontent.Draft{Kind: kind}
	if kind == "model" {
		payload, err := json.Marshal(map[string]any{"format": "glb", "manifest": basicManifest(t)})
		if err != nil {
			t.Fatal(err)
		}
		draft.Payload.Model = payload
	}
	var id string
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		r, err := creativecontent.WriteMediaAndRetain(t.Context(), tx, draft, []creativecontent.ObjectBinding{{Role: "original", BlobID: blob}}, func(r creativecontent.Revision) error {
			return tx.Insert(t.Context(), "creative_revision_holds", []string{"id", "content_revision_id", "reason"}, "hold-"+r.ID, r.ID, "recovery")
		})
		id = r.ID
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func texture(id string) map[string]any { return map[string]any{"content_revision_id": id} }

func principled(extra map[string]any) map[string]any {
	slot := map[string]any{"preset": "principled@1", "params": map[string]any{}}
	for k, v := range extra {
		slot[k] = v
	}
	return slot
}

// texturedBox is a scene with one box; slots writes its material slots.
type texturedBox struct {
	doc, box string
	f        fixture
}

func newTexturedBox(t *testing.T) texturedBox {
	t.Helper()
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	addObjects(t, f.a, doc, box(id, nil, 1))
	return texturedBox{doc: doc, box: id, f: f}
}

func (x texturedBox) slots(t *testing.T, slots map[string]any) (creativedocument.CommandResult, error) {
	t.Helper()
	v := readScene(t, x.f.a, x.doc)
	return send(t, x.f.a, batch{DocumentID: x.doc, Expected: v.expect(content(x.box)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": x.box, "material_slots": slots}}}}})
}

// wantField requires err to be target naming field, and returns the content
// it names.
func wantField(t *testing.T, err, target error, field string) string {
	t.Helper()
	var f *creativeops.FieldError
	if !errors.Is(err, target) || !errors.As(err, &f) || f.Field() != field {
		t.Fatalf("want %v at %s, got %v", target, field, err)
	}
	return f.ContentRevisionID
}

// Every rule of data: 场景文档「材质贴图」 refuses with the field it is about.
func TestTextureFieldsAreCheckedAndNamed(t *testing.T) {
	x := newTexturedBox(t)
	const image = "ccrv_not-checked-before-the-shape"
	base := map[string]any{"base_color": texture(image)}
	at := "operations[0].updates[0].material_slots.main."
	for name, tc := range map[string]struct {
		slot  map[string]any
		field string
	}{
		"maps on a procedural preset":   {map[string]any{"preset": "stone_brick@1", "params": map[string]any{}, "maps": base}, "maps"},
		"alpha on a procedural preset":  {map[string]any{"preset": "paint@1", "params": map[string]any{}, "alpha_mode": "clip"}, "alpha_mode"},
		"maps on a textured material":   {map[string]any{"preset": "wood_floor@1", "params": map[string]any{}, "maps": base}, "maps"},
		"mapping on an unknown preset":  {map[string]any{"preset": "lava@1", "params": map[string]any{}, "mapping": map[string]any{"size_m": 2}}, "mapping"},
		"textured mapping out of range": {map[string]any{"preset": "wood_floor@1", "params": map[string]any{}, "mapping": map[string]any{"size_m": 101}}, "mapping.size_m"},
		"unknown texture":               {principled(map[string]any{"maps": map[string]any{"albedo": texture(image)}}), "maps.albedo"},
		"no texture":                    {principled(map[string]any{"maps": map[string]any{}}), "maps"},
		"no image":                      {principled(map[string]any{"maps": map[string]any{"normal": map[string]any{}}}), "maps.normal.content_revision_id"},
		"not a revision":                {principled(map[string]any{"maps": map[string]any{"normal": texture("ccnt_1")}}), "maps.normal.content_revision_id"},
		"channel on base colour":        {principled(map[string]any{"maps": map[string]any{"base_color": map[string]any{"content_revision_id": image, "channel": "a"}}}), "maps.base_color.channel"},
		"unknown channel":               {principled(map[string]any{"maps": map[string]any{"ao": map[string]any{"content_revision_id": image, "channel": "x"}}}), "maps.ao.channel"},
		"mapping without maps":          {principled(map[string]any{"mapping": map[string]any{"kind": "uv"}}), "mapping"},
		"unknown mapping":               {principled(map[string]any{"maps": base, "mapping": map[string]any{"kind": "sphere"}}), "mapping.kind"},
		"size with uv":                  {principled(map[string]any{"maps": base, "mapping": map[string]any{"kind": "uv", "size_m": 2}}), "mapping.size_m"},
		"repeat with triplanar":         {principled(map[string]any{"maps": base, "mapping": map[string]any{"repeat": []any{2, 2}}}), "mapping.repeat"},
		"size out of range":             {principled(map[string]any{"maps": base, "mapping": map[string]any{"size_m": 101}}), "mapping.size_m"},
		"repeat out of range":           {principled(map[string]any{"maps": base, "mapping": map[string]any{"kind": "uv", "repeat": []any{0, 1}}}), "mapping.repeat"},
		"rotation out of range":         {principled(map[string]any{"maps": base, "mapping": map[string]any{"rotation_deg": 361}}), "mapping.rotation_deg"},
		"strength out of range":         {principled(map[string]any{"maps": base, "mapping": map[string]any{"normal_strength": 4.5}}), "mapping.normal_strength"},
		"unknown alpha mode":            {principled(map[string]any{"alpha_mode": "mask"}), "alpha_mode"},
		"cutoff without clip":           {principled(map[string]any{"alpha_mode": "blend", "alpha_cutoff": 0.3}), "alpha_cutoff"},
		"cutoff out of range":           {principled(map[string]any{"alpha_mode": "clip", "alpha_cutoff": 1.5}), "alpha_cutoff"},
		"unknown slot field":            {principled(map[string]any{"maps": base, "uv": true}), "uv"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := x.slots(t, map[string]any{"main": tc.slot})
			wantField(t, err, creativeops.ErrValidation, at+tc.field)
		})
	}

	// The field is the request's: a rename before it moves the operation.
	v := readScene(t, x.f.a, x.doc)
	_, err := send(t, x.f.a, batch{DocumentID: x.doc, Expected: map[string]any{"meta": v.UnitRevs["meta"], content(x.box): v.UnitRevs[content(x.box)]}, Operations: []any{
		op{"op": "rename", "title": "石台"},
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": x.box, "material_slots": map[string]any{"main": principled(map[string]any{"maps": map[string]any{}})}}}}}})
	wantField(t, err, creativeops.ErrValidation, "operations[1].updates[0].material_slots.main.maps")
}

// A slot with maps is stored with every default filled in; one without
// stores what it always did. The read lists each image.
func TestTexturesAreCompletedAndRead(t *testing.T) {
	x := newTexturedBox(t)
	leaf, orm := mediaRevision(t, x.f, x.f.a, "image"), mediaRevision(t, x.f, x.f.a, "image")
	if _, err := x.slots(t, map[string]any{
		"main": principled(map[string]any{
			"maps":       map[string]any{"base_color": texture(leaf), "roughness": texture(orm), "metalness": map[string]any{"content_revision_id": orm, "channel": "b"}},
			"mapping":    map[string]any{"kind": "uv", "rotation_deg": 90},
			"alpha_mode": "clip"}),
		"side":  principled(map[string]any{"maps": map[string]any{"normal": texture(leaf)}}),
		"glass": principled(map[string]any{"alpha_mode": "blend"}),
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := creativedocument.Read(t.Context(), x.f.a, x.doc)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Scene struct {
			Objects []struct {
				ID            string         `json:"id"`
				MaterialSlots map[string]any `json:"material_slots"`
			} `json:"objects"`
		} `json:"scene"`
		Images map[string]any `json:"images"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var slots map[string]any
	for _, o := range v.Scene.Objects {
		if o.ID == x.box {
			slots = o.MaterialSlots
		}
	}
	want := map[string]any{
		"main": map[string]any{"preset": "principled@1", "params": map[string]any{},
			"maps": map[string]any{"base_color": texture(leaf), "roughness": map[string]any{"content_revision_id": orm, "channel": "r"},
				"metalness": map[string]any{"content_revision_id": orm, "channel": "b"}},
			"mapping":    map[string]any{"kind": "uv", "repeat": []any{1.0, 1.0}, "rotation_deg": 90.0, "normal_strength": 1.0},
			"alpha_mode": "clip", "alpha_cutoff": 0.5},
		"side": map[string]any{"preset": "principled@1", "params": map[string]any{}, "maps": map[string]any{"normal": texture(leaf)},
			"mapping":    map[string]any{"kind": "triplanar", "size_m": 1.0, "rotation_deg": 0.0, "normal_strength": 1.0},
			"alpha_mode": "opaque"},
		"glass": map[string]any{"preset": "principled@1", "params": map[string]any{}, "alpha_mode": "blend"},
	}
	if !reflect.DeepEqual(slots, want) {
		t.Fatalf("stored slots\n%v\nwant\n%v", slots, want)
	}
	image := map[string]any{"usable": true, "width": 2048.0, "height": 1024.0, "mime": "image/png"}
	if !reflect.DeepEqual(v.Images, map[string]any{leaf: image, orm: image}) {
		t.Fatalf("images %v", v.Images)
	}

	// Writing the stored form back changes nothing.
	before := readScene(t, x.f.a, x.doc).object(t, x.box).MaterialSlots
	if _, err := x.slots(t, slots); err != nil {
		t.Fatal(err)
	}
	if after := readScene(t, x.f.a, x.doc).object(t, x.box).MaterialSlots; !reflect.DeepEqual(before, after) {
		t.Fatalf("round trip changed the slots:\n%v\n%v", before, after)
	}
}

// A built-in textured material takes a mapping and an alpha mode but no
// maps; what it leaves out is stored as its catalog entry says, and it
// references no image.
func TestTexturedMaterialIsCompleted(t *testing.T) {
	x := newTexturedBox(t)
	textured := func(extra map[string]any) map[string]any {
		slot := map[string]any{"preset": "wood_floor@1", "params": map[string]any{"color": "#eeeeee"}}
		maps.Copy(slot, extra)
		return slot
	}
	if _, err := x.slots(t, map[string]any{
		"main":  textured(nil),
		"side":  textured(map[string]any{"mapping": map[string]any{"kind": "uv", "repeat": []any{2, 2}}, "alpha_mode": "clip"}),
		"plain": map[string]any{"preset": "paint@1", "params": map[string]any{}},
	}); err != nil {
		t.Fatal(err)
	}
	v := readScene(t, x.f.a, x.doc)
	want := map[string]any{
		"main": map[string]any{"preset": "wood_floor@1", "params": map[string]any{"color": "#eeeeee"},
			"mapping":    map[string]any{"kind": "triplanar", "size_m": 1.7, "rotation_deg": 0.0, "normal_strength": 1.0},
			"alpha_mode": "opaque"},
		"side": map[string]any{"preset": "wood_floor@1", "params": map[string]any{"color": "#eeeeee"},
			"mapping":    map[string]any{"kind": "uv", "repeat": []any{2.0, 2.0}, "rotation_deg": 0.0, "normal_strength": 1.0},
			"alpha_mode": "clip", "alpha_cutoff": 0.5},
		"plain": map[string]any{"preset": "paint@1", "params": map[string]any{}},
	}
	got := map[string]any{}
	b, _ := json.Marshal(v.object(t, x.box).MaterialSlots)
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored slots\n%v\nwant\n%v", got, want)
	}
	raw, err := creativedocument.Read(t.Context(), x.f.a, x.doc)
	if err != nil {
		t.Fatal(err)
	}
	var read struct {
		Images map[string]any `json:"images"`
	}
	if err := json.Unmarshal(raw, &read); err != nil || len(read.Images) != 0 {
		t.Fatalf("images %v %v", read.Images, err)
	}
}

// Only an image of this account that it may display can be a texture; any
// other revision is the same refusal, naming the field and the revision.
func TestUnavailableImagesAreRefused(t *testing.T) {
	x := newTexturedBox(t)
	for name, id := range map[string]string{
		"another account's image": mediaRevision(t, x.f, x.f.b, "image"),
		"a video":                 mediaRevision(t, x.f, x.f.a, "video"),
		"a model":                 mediaRevision(t, x.f, x.f.a, "model"),
		"nothing":                 "ccrv_" + uuid.NewString(),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := x.slots(t, map[string]any{"main": principled(map[string]any{"maps": map[string]any{"base_color": texture(id)}})})
			if named := wantField(t, err, creativescene.ErrImageUnavailable, "operations[0].updates[0].material_slots.main.maps.base_color.content_revision_id"); named != id {
				t.Fatalf("names %s, want %s", named, id)
			}
		})
	}

	// The first unavailable texture in request order is the one named.
	good, bad := mediaRevision(t, x.f, x.f.a, "image"), mediaRevision(t, x.f, x.f.b, "image")
	_, err := x.slots(t, map[string]any{
		"a": principled(map[string]any{"maps": map[string]any{"base_color": texture(good), "normal": texture(bad)}}),
		"b": principled(map[string]any{"maps": map[string]any{"base_color": texture(bad)}}),
	})
	wantField(t, err, creativescene.ErrImageUnavailable, "operations[0].updates[0].material_slots.a.maps.normal.content_revision_id")
}

// A scene references at most 64 distinct images, however they are spread.
func TestSceneImageLimit(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	boxes := func(images int) []any {
		var out []any
		for i := 0; i*5 < images; i++ {
			maps := map[string]any{}
			for j, kind := range []string{"base_color", "normal", "roughness", "metalness", "ao"} {
				if n := i*5 + j; n < images {
					maps[kind] = texture(fmt.Sprintf("ccrv_limit-%02d", n))
				}
			}
			o := box(objectID(10+i), nil, 10+i)
			o["material_slots"] = map[string]any{"main": principled(map[string]any{"maps": maps})}
			out = append(out, o)
		}
		return out
	}
	v := readScene(t, f.a, doc)
	_, err := send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": boxes(65)}}})
	var limit *creativedocument.LimitError
	if !errors.Is(err, creativedocument.ErrSizeLimit) || !errors.As(err, &limit) || limit.Limit != 64 {
		t.Fatalf("65 images: %v", err)
	}
	// 64 are within the limit; the (made up) images themselves are not.
	_, err = send(t, f.a, batch{DocumentID: doc, Expected: v.expect("objects"), Operations: []any{op{"op": "add_objects", "objects": boxes(64)}}})
	if !errors.Is(err, creativescene.ErrImageUnavailable) {
		t.Fatalf("64 images: %v", err)
	}
}

func revokeDisplay(t *testing.T, f fixture, revision string) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE creative_usage_grants SET revoked_at=clock_timestamp() WHERE purpose='display' AND declaration_id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE id=$1)`, revision); err != nil {
		t.Fatal(err)
	}
}

// An image the scene already holds is checked again when a write uses it,
// in one read; a write that leaves it alone does not look at it.
func TestHeldImagesAreCheckedWhenWritten(t *testing.T) {
	x := newTexturedBox(t)
	image := mediaRevision(t, x.f, x.f.a, "image")
	slot := map[string]any{"main": principled(map[string]any{"maps": map[string]any{"base_color": texture(image)}})}
	if _, err := x.slots(t, slot); err != nil {
		t.Fatal(err)
	}
	other := objectID(2)
	addObjects(t, x.f.a, x.doc, box(other, nil, 2))
	copyTo := func(id string) error {
		v := readScene(t, x.f.a, x.doc)
		_, err := send(t, x.f.a, batch{DocumentID: x.doc, Expected: v.expect(content(id)), Operations: []any{
			op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "material_slots": slot}}}}})
		return err
	}
	if err := copyTo(other); err != nil {
		t.Fatal(err)
	}

	revokeDisplay(t, x.f, image)
	v := readScene(t, x.f.a, x.doc)
	if _, err := send(t, x.f.a, batch{DocumentID: x.doc, Expected: v.expect(placement(x.box)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": x.box, "name": "石台"}}}}}); err != nil {
		t.Fatalf("an edit not touching the texture: %v", err)
	}
	third := objectID(3)
	addObjects(t, x.f.a, x.doc, box(third, nil, 3))
	err := copyTo(third)
	wantField(t, err, creativescene.ErrImageUnavailable, "operations[0].updates[0].material_slots.main.maps.base_color.content_revision_id")
}

func live(t *testing.T, scope store.AccountScope, revision string) bool {
	t.Helper()
	var out map[string]bool
	if err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		out, err = creativereference.Live(t.Context(), tx, []string{revision})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out[revision]
}

// The scene holds its images as it holds its models: through every revision
// that referenced them, until the scene is purged.
func TestSceneHoldsItsImages(t *testing.T) {
	x := newTexturedBox(t)
	image := mediaRevision(t, x.f, x.f.a, "image")
	if _, err := x.slots(t, map[string]any{"main": principled(map[string]any{"maps": map[string]any{"ao": texture(image)}})}); err != nil {
		t.Fatal(err)
	}
	// What held it before it was placed (a canvas node, an asset) is gone.
	if _, err := x.f.db.Exec(`DELETE FROM creative_revision_holds WHERE id=$1`, "hold-"+image); err != nil {
		t.Fatal(err)
	}
	if !live(t, x.f.a, image) {
		t.Fatal("a texture of the scene is not held")
	}
	if _, err := x.slots(t, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if !live(t, x.f.a, image) {
		t.Fatal("history stopped holding a removed texture")
	}
	if _, err := creativedocument.Trash(t.Context(), x.f.a, command(t, map[string]any{"document_id": x.doc})); err != nil {
		t.Fatal(err)
	}
	if _, err := creativedocument.Purge(t.Context(), x.f.a, command(t, map[string]any{"document_id": x.doc})); err != nil {
		t.Fatal(err)
	}
	if live(t, x.f.a, image) {
		t.Fatal("a purged scene still holds its texture")
	}
}

// Restoring brings textures back under the same rule: one no longer
// displayable refuses the restore, as a path in the scene.
func TestRestoreChecksItsImages(t *testing.T) {
	x := newTexturedBox(t)
	image := mediaRevision(t, x.f, x.f.a, "image")
	if _, err := x.slots(t, map[string]any{"main": principled(map[string]any{"maps": map[string]any{"base_color": texture(image)}})}); err != nil {
		t.Fatal(err)
	}
	textured := readScene(t, x.f.a, x.doc).Revision
	if _, err := x.slots(t, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	revokeDisplay(t, x.f, image)
	v := readScene(t, x.f.a, x.doc)

	_, err := send(t, x.f.a, batch{DocumentID: x.doc, Expected: v.expect(content(x.box)), Operations: []any{
		op{"op": "restore_units", "units": []string{content(x.box)}, "from_revision": textured}}})
	var f *creativeops.FieldError
	if !errors.Is(err, creativescene.ErrImageUnavailable) || !errors.As(err, &f) || f.Op != -1 ||
		!strings.HasSuffix(f.Path, ".material_slots.main.maps.base_color.content_revision_id") || f.ContentRevisionID != image {
		t.Fatalf("restore_units: %v", err)
	}

	whole := map[string]any{"document_id": x.doc, "expected_revision": v.Revision, "operations": []any{op{"op": "restore", "revision": textured}}}
	_, err = creativedocument.CommandAs(t.Context(), x.f.a, creativedocument.Photographer, command(t, whole))
	if !errors.Is(err, creativescene.ErrImageUnavailable) {
		t.Fatalf("whole restore: %v", err)
	}
}
