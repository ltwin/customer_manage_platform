package creativescene_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func importModel(t *testing.T, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, creativelibrary.AssetResult, error) {
	t.Helper()
	receipt, err := creativescene.ImportModelToLibrary(t.Context(), scope, c)
	if err != nil {
		return receipt, creativelibrary.AssetResult{}, err
	}
	var out creativelibrary.AssetResult
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		t.Fatal(err)
	}
	return receipt, out, nil
}

// stagedRevision is the image revision a staged upload holds.
func stagedRevision(t *testing.T, f fixture, upload string) string {
	t.Helper()
	var id string
	if err := f.db.QueryRow(`SELECT content_revision_id FROM creative_upload_candidates WHERE upload_id=$1`, upload).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// glbImport is one import's uploads: the GLB, its thumbnail and two
// extracted images, with the slot of basic.glb they texture.
type glbImport struct {
	glb, thumb, base, glow string
	slot                   string
}

func stageImport(t *testing.T, f fixture) glbImport {
	t.Helper()
	return glbImport{glb: stage(t, f, f.a, "model"), thumb: stage(t, f, f.a, "image"), base: stage(t, f, f.a, "image"), glow: stage(t, f, f.a, "image"), slot: basicManifest(t).Slots[0].ID}
}

func (u glbImport) input(t *testing.T, f fixture) creativescene.ImportModelInput {
	t.Helper()
	var materials map[string]creativescene.MaterialSlot
	raw, err := json.Marshal(map[string]any{u.slot: principled(map[string]any{
		"params":  map[string]any{"vertex_color": true, "emissive": "#ffffff"},
		"maps":    map[string]any{"base_color": texture(stagedRevision(t, f, u.base)), "emissive": texture(stagedRevision(t, f, u.glow))},
		"mapping": map[string]any{"kind": "uv"}})})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &materials); err != nil {
		t.Fatal(err)
	}
	return creativescene.ImportModelInput{GLBUploadID: u.glb, ThumbnailUploadID: u.thumb, TextureUploadIDs: []string{u.base, u.glow}, Mount: "wall",
		Import:    creativescene.ModelImport{FileName: "dragon_sword.glb", Attribution: "Dragon Sword, CC BY 4.0", SourceURL: "https://example.com/dragon-sword"},
		Materials: materials, Target: creativelibrary.ImportTarget{Title: strings.Repeat("剑", 90)}}
}

// An import writes an imported model owned by the library over the staged
// GLB and thumbnail, holding the extracted images and an image the account
// already had, and consumes every upload. Its operation ID replayed returns
// the same asset and takes nothing again.
func TestImportModelToLibrary(t *testing.T) {
	f := setup(t)
	u := stageImport(t, f)
	in := u.input(t, f)
	owned := mediaRevision(t, f, f.a, "image")
	slot := in.Materials[u.slot]
	slot.Maps.Normal = &creativescene.TextureMap{ContentRevisionID: owned}
	in.Materials[u.slot] = slot
	want := []string{stagedRevision(t, f, u.base), stagedRevision(t, f, u.glow), owned}
	glow := want[1]
	glbBlob, thumbBlob := stagedUpload(t, f, u.glb).blob, stagedUpload(t, f, u.thumb).blob
	slices.Sort(want)
	c := command(t, in)
	receipt, asset, err := importModel(t, f.a, c)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Outcome.HTTPStatus != 201 || asset.Reused || asset.ID == "" {
		t.Fatalf("receipt %+v asset %+v", receipt.Outcome, asset)
	}
	r, p := readRevision(t, f.a, asset.ContentRevisionID)
	if !p.Generator.Imported() || p.Generator.Mount != "wall" || p.SourceRevisionID != "" ||
		!reflect.DeepEqual(p.Import, &creativescene.ModelImport{FileName: "dragon_sword.glb", Attribution: "Dragon Sword, CC BY 4.0", SourceURL: "https://example.com/dragon-sword"}) {
		t.Fatalf("payload %+v import %+v", p.Generator, p.Import)
	}
	if p.NameHint != strings.Repeat("剑", 80) || !reflect.DeepEqual(p.Manifest, basicManifest(t)) {
		t.Fatalf("name hint %q manifest %+v", p.NameHint, p.Manifest)
	}
	// The materials are stored as a scene stores slots: completed.
	got := p.Materials[u.slot]
	if got.Mapping == nil || got.Mapping.Kind != "uv" || len(got.Mapping.Repeat) != 2 || got.AlphaMode != "opaque" ||
		got.Maps.Emissive.ContentRevisionID != glow || got.Params["vertex_color"] != true {
		t.Fatalf("materials %+v", got)
	}
	if blobOf(t, r, "original") != glbBlob || blobOf(t, r, "thumbnail") != thumbBlob {
		t.Fatal("the model is not over the staged blobs")
	}
	if held := heldTextures(t, f, asset.ContentRevisionID); !reflect.DeepEqual(held, want) {
		t.Fatalf("held %v want %v", held, want)
	}
	for _, upload := range []string{u.glb, u.thumb, u.base, u.glow} {
		if s := stagedUpload(t, f, upload); s.state != "applied" || s.kind != "asset" || s.target != asset.ID {
			t.Fatalf("upload %s: %+v", upload, s)
		}
	}
	// The extracted images live on as the model's: the uploads gave them up.
	for _, image := range want {
		if !live(t, f.a, image) {
			t.Fatalf("image %s was dropped", image)
		}
	}

	replayed, again, err := importModel(t, f.a, c)
	if err != nil || again.ID != asset.ID || replayed.Outcome.HTTPStatus != 201 {
		t.Fatalf("replay: %v %+v", err, again)
	}
	var assets int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_assets`).Scan(&assets); err != nil || assets != 1 {
		t.Fatalf("%d assets, %v", assets, err)
	}
}

// Every refusal of api: 导入模型到资产库 names what it is about, writes
// nothing and leaves the uploads to a corrected retry.
func TestImportModelToLibraryRejections(t *testing.T) {
	f := setup(t)
	u := stageImport(t, f)
	slotPath := "materials." + u.slot
	fake := func(n int) map[string]creativescene.MaterialSlot {
		out := map[string]creativescene.MaterialSlot{}
		for i := range n {
			slot := fmt.Sprintf("s%02d", i/6)
			s, ok := out[slot]
			if !ok {
				s = creativescene.MaterialSlot{Preset: "principled@1", Params: map[string]any{}, Maps: &creativescene.TextureMaps{}}
			}
			tex := &creativescene.TextureMap{ContentRevisionID: fmt.Sprintf("ccrv_limit-%02d", i)}
			switch i % 6 {
			case 0:
				s.Maps.BaseColor = tex
			case 1:
				s.Maps.Normal = tex
			case 2:
				s.Maps.Roughness = tex
			case 3:
				s.Maps.Metalness = tex
			case 4:
				s.Maps.AO = tex
			case 5:
				s.Maps.Emissive = tex
			}
			out[slot] = s
		}
		return out
	}
	cases := map[string]struct {
		change func(*creativescene.ImportModelInput)
		want   error
		field  string // details.field of a refusal at a field, or upload_id of an unavailable upload
	}{
		"bad mount":        {func(in *creativescene.ImportModelInput) { in.Mount = "roof" }, creativeops.ErrValidation, "mount"},
		"no file name":     {func(in *creativescene.ImportModelInput) { in.Import.FileName = "" }, creativeops.ErrValidation, "import.file_name"},
		"long attribution": {func(in *creativescene.ImportModelInput) { in.Import.Attribution = strings.Repeat("a", 2001) }, creativeops.ErrValidation, "import.attribution"},
		"source not http":  {func(in *creativescene.ImportModelInput) { in.Import.SourceURL = "javascript:alert(1)" }, creativeops.ErrValidation, "import.source_url"},
		"duplicate image":  {func(in *creativescene.ImportModelInput) { in.TextureUploadIDs = []string{u.base, u.base} }, creativeops.ErrValidation, "texture_upload_ids[1]"},
		"image not used": {func(in *creativescene.ImportModelInput) {
			in.TextureUploadIDs = append(in.TextureUploadIDs, stage(t, f, f.a, "image"))
		}, creativeops.ErrValidation, "texture_upload_ids[2]"},
		"not a slot of the GLB": {func(in *creativescene.ImportModelInput) { in.Materials["nope"] = in.Materials[u.slot] }, creativeops.ErrValidation, "materials.nope"},
		"emissive channel": {func(in *creativescene.ImportModelInput) {
			in.Materials[u.slot].Maps.Emissive.Channel = "r"
		}, creativeops.ErrValidation, slotPath + ".maps.emissive.channel"},
		"thumbnail not a PNG": {func(in *creativescene.ImportModelInput) {
			in.ThumbnailUploadID = stage(t, f, f.a, "image")
			if _, err := f.db.Exec(`UPDATE creative_blobs SET mime='image/jpeg' WHERE id=$1`, stagedUpload(t, f, in.ThumbnailUploadID).blob); err != nil {
				t.Fatal(err)
			}
		}, creativeops.ErrValidation, "thumbnail_upload_id"},
		"image unavailable": {func(in *creativescene.ImportModelInput) {
			in.Materials[u.slot].Maps.Normal = &creativescene.TextureMap{ContentRevisionID: "ccrv_missing"}
		}, creativescene.ErrImageUnavailable, slotPath + ".maps.normal"},
		"missing group": {func(in *creativescene.ImportModelInput) { in.Target.GroupIDs = []string{"cagr_missing"} }, creativelibrary.ErrNotFound, ""},
		"image upload gone": {func(in *creativescene.ImportModelInput) {
			in.TextureUploadIDs[1] = "ccup_missing"
		}, creativescene.ErrUploadUnavailable, "ccup_missing"},
		"GLB upload not a model": {func(in *creativescene.ImportModelInput) { in.GLBUploadID = u.thumb }, creativescene.ErrUploadUnavailable, u.thumb},
		"another account's GLB":  {func(in *creativescene.ImportModelInput) { in.GLBUploadID = stage(t, f, f.b, "model") }, creativescene.ErrUploadUnavailable, ""},
		"65 uploads": {func(in *creativescene.ImportModelInput) {
			in.TextureUploadIDs = make([]string, 65)
			for i := range in.TextureUploadIDs {
				in.TextureUploadIDs[i] = fmt.Sprintf("ccup_%02d", i)
			}
		}, creativedocument.ErrSizeLimit, ""},
		"65 images": {func(in *creativescene.ImportModelInput) { in.Materials = fake(65) }, creativedocument.ErrSizeLimit, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := u.input(t, f)
			in.TextureUploadIDs = slices.Clone(in.TextureUploadIDs)
			tc.change(&in)
			_, _, err := importModel(t, f.a, command(t, in))
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
			var field *creativeops.FieldError
			var upload *creativescene.UploadUnavailableError
			switch {
			case tc.field == "":
			case errors.As(err, &field):
				if field.Field() != tc.field {
					t.Fatalf("field %s, want %s", field.Field(), tc.field)
				}
			case errors.As(err, &upload):
				if upload.UploadID != tc.field {
					t.Fatalf("upload %s, want %s", upload.UploadID, tc.field)
				}
			default:
				t.Fatalf("%v names no field", err)
			}
			var limit *creativedocument.LimitError
			if errors.Is(tc.want, creativedocument.ErrSizeLimit) && (!errors.As(err, &limit) || limit.Limit != 64) {
				t.Fatalf("limit %v", err)
			}
		})
	}
	var assets int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_assets`).Scan(&assets); err != nil || assets != 0 {
		t.Fatalf("%d assets written by rejected imports, %v", assets, err)
	}
	for _, upload := range []string{u.glb, u.thumb, u.base, u.glow} {
		if s := stagedUpload(t, f, upload); s.state != "pending" {
			t.Fatalf("a rejected import took %s: %+v", upload, s)
		}
	}
	// The corrected retry goes through with the same uploads.
	if _, _, err := importModel(t, f.a, command(t, u.input(t, f))); err != nil {
		t.Fatal(err)
	}
}

// AC-10: an imported model belongs to the library like a library copy. A face
// assignment on an instance placed from it is new content, still imported and
// with its import, and the asset keeps its revision; that new content is the
// scene's, so the next assignment continues it.
func TestDeriveModelFromAnImport(t *testing.T) {
	f := setup(t)
	u := stageImport(t, f)
	_, asset, err := importModel(t, f.a, command(t, u.input(t, f)))
	if err != nil {
		t.Fatal(err)
	}
	imported, source := readRevision(t, f.a, asset.ContentRevisionID)
	doc := createScene(t, f.a)
	id := objectID(1)
	addObjects(t, f.a, doc, instance(id, 1, map[string]any{"content_revision_id": imported.ID}))
	assign := func(ref, from, base string) committed {
		t.Helper()
		res := mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(id)), Operations: []any{
			derive(ref, from, brickFaces(), map[string]any{"base_content_id": base}),
			op{"op": "set_instance_model", "object_id": id, "model": map[string]any{"ref": ref}},
		}})
		return receiptModels(t, res)[ref]
	}
	mine := assign("mine", imported.ID, imported.ContentID)
	if mine.ContentID == imported.ContentID {
		t.Fatal("an import is never continued")
	}
	_, p := readRevision(t, f.a, mine.ContentRevisionID)
	if !p.Generator.Imported() || p.Generator.Mount != "wall" || !reflect.DeepEqual(p.Import, source.Import) || p.Materials != nil || p.Faces == nil {
		t.Fatalf("derived %+v import %+v materials %v", p.Generator, p.Import, p.Materials)
	}
	var held string
	var revisions int
	if err := f.db.QueryRow(`SELECT content_revision_id, (SELECT count(*) FROM creative_content_revisions WHERE content_id=$2) FROM creative_assets WHERE id=$1`, asset.ID, imported.ContentID).Scan(&held, &revisions); err != nil {
		t.Fatal(err)
	}
	if held != imported.ID || revisions != 1 {
		t.Fatalf("the asset holds %s, the import has %d revisions", held, revisions)
	}
	if again := assign("again", mine.ContentRevisionID, mine.ContentID); again.ContentID != mine.ContentID {
		t.Fatalf("the scene's derived import forked into %s", again.ContentID)
	}
}

// AC-11: the assistant reads an imported model as one, without code or
// parameters, and cannot bake it with new parameters; it can still replace it.
func TestAssistantReadsAnImport(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, &fakeWaiter{})
	_, asset, err := importModel(t, f.a, command(t, stageImport(t, f).input(t, f)))
	if err != nil {
		t.Fatal(err)
	}
	doc := createScene(t, f.a)
	id := objectID(1)
	addObjects(t, f.a, doc, instance(id, 1, map[string]any{"content_revision_id": asset.ContentRevisionID}))
	for _, budget := range []int{0, 64 << 10} {
		got, err := agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, IncludeCode: []string{asset.ContentRevisionID}, MaxBytes: budget})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.Code[asset.ContentRevisionID]; ok || len(got.CodeOmitted) != 0 {
			t.Fatalf("budget %d: code %v omitted %v", budget, got.Code, got.CodeOmitted)
		}
		var m *creativescene.ModelView
		for _, o := range got.Objects {
			if o.ID == id {
				m = o.Model
			}
		}
		if m == nil || !m.Usable || !m.Imported || m.Params != nil || m.ParamsSchema != nil || m.BoundsM == nil {
			t.Fatalf("budget %d: model %+v", budget, m)
		}
	}

	based := int64(readScene(t, f.a, doc).Revision)
	_, _, err = bakeJobInTx(t, f.a, creativescene.BakeInput{DocumentID: doc, RunID: "ccrn_agent", ToolCallID: "step-1", BasedOn: based,
		BaseContentRevisionID: asset.ContentRevisionID, Params: map[string]any{}, Replace: &creativescene.ReplaceTarget{ObjectID: id}})
	var public interface{ PublicMessage() string }
	if !errors.Is(err, creativeops.ErrValidation) || !errors.As(err, &public) || !strings.Contains(public.PublicMessage(), "导入的模型") {
		t.Fatalf("bake from an import: %v", err)
	}
	if _, _, err := bakeJobInTx(t, f.a, creativescene.BakeInput{DocumentID: doc, RunID: "ccrn_agent", ToolCallID: "step-2", BasedOn: based,
		Code: pillarCode, Params: map[string]any{"height": 3}, Replace: &creativescene.ReplaceTarget{ObjectID: id}}); err != nil {
		t.Fatalf("replacing an import: %v", err)
	}
}
