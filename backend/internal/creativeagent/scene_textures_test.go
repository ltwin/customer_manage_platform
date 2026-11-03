package creativeagent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// textureSlot is a principled slot with a base colour texture.
func textureSlot(image string) map[string]any {
	return map[string]any{"preset": "principled@1", "params": map[string]any{}, "maps": map[string]any{"base_color": map[string]any{"content_revision_id": image}}}
}

// unlistedImage is an image that has left the canvas and the library: only
// what still holds it can make it readable.
func (f *fixture) unlistedImage() string {
	f.t.Helper()
	id := f.mediaRevision(f.alice, "image", "image/png")
	if _, err := f.db.Exec(`UPDATE creative_assets SET deleted_at=now() WHERE content_revision_id=$1`, id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

// materialAsset files a material asset holding its textures, as a save from
// a scene does: the slot, its thumbnail and one holder row per image.
func (f *fixture) materialAsset(title string, images ...string) (assetID, revisionID string) {
	f.t.Helper()
	thumb := "ccbl_" + uuid.NewString()
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
 VALUES ($1,$2,'local','blobs/'||$1,'v1','sha256-'||repeat('3',64),50,'image/png','ready',clock_timestamp())`, thumb, f.alice.AccountID()); err != nil {
		f.t.Fatal(err)
	}
	kinds := []string{"base_color", "roughness", "metalness"}
	maps := map[string]any{}
	for i, id := range images {
		maps[kinds[i]] = map[string]any{"content_revision_id": id}
	}
	payload, err := json.Marshal(map[string]any{"slot": map[string]any{"preset": "principled@1", "params": map[string]any{}, "maps": maps,
		"mapping": map[string]any{"kind": "triplanar", "size_m": 1, "rotation_deg": 0, "normal_strength": 1}, "alpha_mode": "opaque"},
		"source": map[string]any{"document_id": "cdoc_scene", "object_id": "sobj_source", "slot_id": "main", "revision": 1}})
	if err != nil {
		f.t.Fatal(err)
	}
	err = f.alice.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		r, err := creativecontent.WriteMaterialAndRetain(f.t.Context(), tx, creativecontent.Draft{Kind: "material", Payload: creativecontent.Payload{Material: payload}},
			[]creativecontent.ObjectBinding{{Role: "thumbnail", BlobID: thumb}},
			func(r creativecontent.Revision) error {
				if err := creativecontent.HoldTextures(f.t.Context(), tx, r.ID, images); err != nil {
					return err
				}
				saved, err := creativelibrary.SaveRevisionInTx(f.t.Context(), tx, r, creativelibrary.ImportTarget{Title: title})
				assetID = saved.ID
				return err
			})
		revisionID = r.ID
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return assetID, revisionID
}

// texturedHandler is a claimed run granted the account's library.
func texturedHandler(t *testing.T, f *fixture, conversation string) *runtimeHandler {
	t.Helper()
	consent := libraryConsent(t, f, conversation)
	run, err := f.startRun(f.alice, conversation, consent.ID, InstructionSegment{Type: "text", Text: "给柱子贴上石材"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	return execution.runtime
}

// docRevision is the scene's current revision, the baseline of a write.
func (h *runtimeHandler) docRevision(t *testing.T, doc string) int64 {
	t.Helper()
	var rev int64
	if err := h.scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return tx.QueryRow(t.Context(), "creative_documents", "revision", "id=$2", doc).Scan(&rev)
	}); err != nil {
		t.Fatal(err)
	}
	return rev
}

// paintBox adds a box whose main slot shows the image, as edit_scene does,
// and gives what the tool would answer.
func paintBox(t *testing.T, h *runtimeHandler, doc, tag, image string) (string, error) {
	t.Helper()
	args, err := json.Marshal(map[string]any{"document_id": doc, "based_on_revision": h.docRevision(t, doc), "operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{
		"id": sceneObjectID(tag), "kind": "primitive", "name": tag, "parent_id": nil, "order": len(tag), "transform": identityAt(0), "visible": true, "locked": false,
		"primitive": map[string]any{"builtin": "box@1"}, "material_slots": map[string]any{"main": textureSlot(image)}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := h.editScene(t.Context(), "paint-"+tag, string(args))
	if err != nil {
		return "", sceneRefusal(err)
	}
	return string(out.(json.RawMessage)), nil
}

// An image goes into a slot only when the run may read it. One the canvas
// and the library no longer have is still usable through a readable
// material asset holding it; a revocation of the image or of the material
// wins over that holder. An image the scene already shows needs no asking.
func TestTexturesUseImagesTheRunMayRead(t *testing.T) {
	f, c, _, doc := visualScene(t, true)
	gone, held, revokedImage, viaRevoked, spare := f.unlistedImage(), f.unlistedImage(), f.unlistedImage(), f.unlistedImage(), f.unlistedImage()
	stone, _ := f.materialAsset("石材", held, revokedImage, spare)
	_, revokedMaterial := f.materialAsset("旧木纹", viaRevoked)
	withdrawn := contextConsent(t, f, c.ID, revokedImage, revokedMaterial)
	if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": withdrawn.ID, "expected_revision": withdrawn.Revision})); err != nil {
		t.Fatal(err)
	}
	h := texturedHandler(t, f, c.ID)

	for tag, image := range map[string]string{"gone": gone, "revokedimage": revokedImage, "revokedmaterial": viaRevoked} {
		out, err := paintBox(t, h, doc, tag, image)
		if err == nil || !strings.Contains(err.Error(), image) || !strings.Contains(err.Error(), "读不到") {
			t.Fatalf("%s: an unreadable image was written or not explained: %s %v", tag, out, err)
		}
	}
	if out, err := paintBox(t, h, doc, "held", held); err != nil {
		t.Fatalf("an image the material asset holds was refused: %s %v", out, err)
	}

	// Once the scene shows it, copying it to another slot asks nothing, even
	// after the material holding it is gone.
	if _, err := f.db.Exec(`UPDATE creative_assets SET deleted_at=now() WHERE id=$1`, stone); err != nil {
		t.Fatal(err)
	}
	if out, err := paintBox(t, h, doc, "copied", held); err != nil {
		t.Fatalf("a texture already in the scene was asked for again: %s %v", out, err)
	}
	if out, err := paintBox(t, h, doc, "spare", spare); err == nil {
		t.Fatalf("an image only a trashed material holds was written: %s", out)
	}
}

// The assistant finds a material by kind, reads it whole with the size of
// its textures and its thumbnail, and writes the slot into a scene as read.
func TestAssistantFindsReadsAndAppliesALibraryMaterial(t *testing.T) {
	f, c, _, doc := visualScene(t, true)
	image := f.unlistedImage()
	assetID, material := f.materialAsset("青石板", image)
	h := texturedHandler(t, f, c.ID)

	found, err := h.searchAssets(t.Context(), `{"kind":"material","limit":10}`)
	if err != nil || !strings.Contains(found, assetID) || !strings.Contains(found, `"preset":"principled@1"`) || !strings.Contains(found, image) {
		t.Fatalf("search for materials: %s %v", found, err)
	}
	read, err := h.readAsset(t.Context(), fmt.Sprintf(`{"content_revision_id":%q,"offset":0,"limit":100}`, material))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Material creativecontent.MaterialFacts `json:"material"`
		Image    string                        `json:"image_revision_id"`
	}
	if err := json.Unmarshal([]byte(read), &got); err != nil {
		t.Fatal(err)
	}
	size := got.Material.Images[image]
	if got.Image != material || !size.Usable || size.Width == nil || *size.Width != 800 || size.Height == nil || *size.Height != 600 {
		t.Fatalf("read_asset of a material: %s", read)
	}
	var slot map[string]any
	if err := json.Unmarshal(got.Material.Slot, &slot); err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{"document_id": doc, "based_on_revision": h.docRevision(t, doc), "operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{
		"id": sceneObjectID("slab"), "kind": "primitive", "name": "石板", "parent_id": nil, "order": 1, "transform": identityAt(0), "visible": true, "locked": false,
		"primitive": map[string]any{"builtin": "box@1"}, "material_slots": map[string]any{"main": slot}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.editScene(t.Context(), "apply", string(args)); err != nil {
		t.Fatalf("the slot as read was refused: %v", sceneRefusal(err))
	}
	for _, o := range f.objectsOf(doc) {
		if o.ID == sceneObjectID("slab") {
			if maps := o.MaterialSlots["main"].Maps; maps == nil || maps.BaseColor == nil || maps.BaseColor.ContentRevisionID != image {
				t.Fatalf("slab after applying the material: %+v", o.MaterialSlots)
			}
			return
		}
	}
	t.Fatal("the slab was not written")
}

// read_scene shows a slot's textures as stored, defaults filled, and whether
// each light casts shadows.
func TestReadSceneShowsTexturesAndShadows(t *testing.T) {
	f, c, _, doc := visualScene(t, true)
	image := f.mediaRevision(f.alice, "image", "image/png")
	h := texturedHandler(t, f, c.ID)
	if out, err := paintBox(t, h, doc, "box", image); err != nil {
		t.Fatalf("%s %v", out, err)
	}
	lamp := func(id string, cast bool) map[string]any {
		return map[string]any{"id": sceneObjectID(id), "kind": "light", "name": id, "parent_id": nil, "order": len(id) + 10, "transform": identityAt(0), "visible": true, "locked": false,
			"light": map[string]any{"type": "point", "cast_shadow": cast}}
	}
	args, err := json.Marshal(map[string]any{"document_id": doc, "based_on_revision": h.docRevision(t, doc), "operations": []any{map[string]any{"op": "add_objects", "objects": []any{lamp("lit", true), lamp("unlit", false)}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.editScene(t.Context(), "lamps", string(args)); err != nil {
		t.Fatal(sceneRefusal(err))
	}
	out, err := h.readScene(t.Context(), fmt.Sprintf(`{"document_id":%q}`, doc))
	if err != nil {
		t.Fatal(err)
	}
	read := out.(creativescene.SceneRead)
	views := map[string]creativescene.ObjectView{}
	for _, o := range read.Objects {
		views[o.ID] = o
	}
	slot := views[sceneObjectID("box")].MaterialSlots["main"]
	if slot.Maps == nil || slot.Maps.BaseColor.ContentRevisionID != image || slot.Mapping == nil || slot.Mapping.Kind != "triplanar" || slot.AlphaMode != "opaque" {
		t.Fatalf("textured slot in read_scene: %+v", slot)
	}
	if lit, unlit := views[sceneObjectID("lit")].Light, views[sceneObjectID("unlit")].Light; lit == nil || lit.Shadow != creativescene.ShadowOn || unlit == nil || unlit.Shadow != creativescene.ShadowOff {
		t.Fatalf("light shadows in read_scene: %+v %+v", lit, unlit)
	}
}
