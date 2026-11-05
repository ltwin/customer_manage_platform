package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// A conversation without the library grant still finds a built-in material,
// reads it and is shown its thumbnail (C5c2 data: 内置贴图库「助手」).
func TestBuiltinMaterialsNeedNoLibraryGrant(t *testing.T) {
	f := setup(t)
	useModel(t, f, func(m *llmgateway.ModelConfig) { m.Capability.ImageInput = true })
	f.service.SetImageReader(testImageReader{})
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var found, read map[string]any
	sawThumbnail := false
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		switch n {
		case 1:
			if !offered(r, searchAssetsTool) || offered(r, readLibraryTool) {
				t.Errorf("without the grant search_assets is offered, read_library not")
			}
			return scriptedResult(r, call("find", 0, searchAssetsTool, `{"source":"builtin","query":"红砖","limit":5}`)), nil
		case 2:
			found = lastTool(r)
			return scriptedResult(r, call("look", 0, readAssetTool, `{"builtin_id":"red_brick@1","offset":0,"limit":1}`)), nil
		}
		read = lastTool(r)
		for _, m := range r.Chat.Messages {
			for _, b := range m.Blocks {
				sawThumbnail = sawThumbnail || b.Kind == llmgateway.BlockImageInput && b.Image.RevisionID == "builtin:red_brick@1" && b.Image.MIME == "image/webp"
			}
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "墙面换成红砖"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := f.service.ReadRun(t.Context(), f.alice, run.ID); err != nil || got.State != RunSucceeded {
		t.Fatalf("run %+v %v", got, err)
	}
	items, _ := found["items"].([]any)
	if len(items) != 1 || found["has_more"] != false {
		t.Fatalf("search_assets source:builtin: %v", found)
	}
	item := items[0].(map[string]any)
	slot, _ := item["slot"].(map[string]any)
	if item["builtin_id"] != "red_brick@1" || item["category"] != "brick" || item["size_m"] == nil || slot["preset"] != "red_brick@1" || item["asset_id"] != nil || item["content_revision_id"] != nil {
		t.Fatalf("built-in item: %v", item)
	}
	material, _ := read["material"].(map[string]any)
	defaults, _ := material["defaults"].(map[string]any)
	if read["builtin_id"] != "red_brick@1" || material["slot"] == nil || defaults["color"] == nil || read["thumbnail_omitted"] != nil {
		t.Fatalf("read_asset builtin_id: %v", read)
	}
	if !sawThumbnail {
		t.Fatalf("the thumbnail was not attached: %s", f.vendor.prompt())
	}
}

func TestBuiltinSourceRefusals(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	h := lookupHandler(t, f, c.ID, contextConsent(t, f, c.ID))
	var refusal *toolRefusal
	// The account's library stays closed without the grant, as a refusal the
	// model can act on rather than an egress request that ends the run.
	if _, err := h.searchAssets(t.Context(), `{"limit":5}`); !errors.As(err, &refusal) || !strings.Contains(refusal.message, "builtin") {
		t.Fatalf("account search without the grant: %v", err)
	}
	if _, err := h.searchAssets(t.Context(), `{"source":"shared","limit":5}`); !errors.As(err, &refusal) {
		t.Fatalf("unknown source: %v", err)
	}
	if out, err := h.searchAssets(t.Context(), `{"source":"builtin","kind":"model","limit":5}`); err != nil || out != `{"has_more":false,"items":[],"total_count":0}` {
		t.Fatalf("built-in models: %s %v", out, err)
	}
	if out, err := h.searchAssets(t.Context(), `{"source":"builtin","limit":1}`); err != nil || strings.Count(out, `"builtin_id"`) != len(creativescene.BuiltinMaterialIDs()) {
		t.Fatalf("all built-ins come at once, whatever the limit: %s %v", out, err)
	}
	if _, err := h.readAsset(t.Context(), `{"builtin_id":"glass@1","offset":0,"limit":1}`); !errors.As(err, &refusal) || !strings.Contains(refusal.message, "wood_floor@1") {
		t.Fatalf("unknown built-in: %v", err)
	}
	if _, err := h.readAsset(t.Context(), `{"builtin_id":"principled@1","offset":0,"limit":1}`); !errors.As(err, &refusal) {
		t.Fatalf("a preset is not a built-in material: %v", err)
	}
	if _, err := h.readAsset(t.Context(), `{"builtin_id":"wood_floor@1","content_revision_id":"ccrv_x","offset":0,"limit":1}`); !errors.As(err, &refusal) {
		t.Fatalf("both IDs: %v", err)
	}
	// A model that cannot see images gets the facts without the thumbnail.
	h.held.Model.Capability.ImageInput = false
	out, err := h.readAsset(t.Context(), `{"builtin_id":"wood_floor@1","offset":0,"limit":1}`)
	if err != nil || !strings.Contains(out, `"thumbnail_omitted":"model_cannot_see_images"`) || strings.Contains(out, "image_revision_id") {
		t.Fatalf("read without vision: %s %v", out, err)
	}
	if _, err := f.service.agentImage(t.Context(), f.alice, "builtin:glass@1", 1<<20); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("unknown built-in thumbnail: %v", err)
	}
}

// The slot a search gives goes into a scene as it is; the store gives it the
// material's own size (spec: 助手选用内置贴图材质).
func TestAssistantAppliesABuiltinMaterial(t *testing.T) {
	f, c, _, doc := visualScene(t, true)
	h := texturedHandler(t, f, c.ID)
	found, err := h.searchAssets(t.Context(), `{"source":"builtin","query":"红砖","limit":5}`)
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []creativescene.BuiltinMaterial `json:"items"`
	}
	if err := json.Unmarshal([]byte(found), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("%s %v", found, err)
	}
	m := page.Items[0]
	args, err := json.Marshal(map[string]any{"document_id": doc, "based_on_revision": h.docRevision(t, doc), "operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{
		"id": sceneObjectID("wall"), "kind": "primitive", "name": "墙", "parent_id": nil, "order": 1, "transform": identityAt(0), "visible": true, "locked": false,
		"primitive": map[string]any{"builtin": "box@1"}, "material_slots": map[string]any{"main": m.Slot}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.editScene(t.Context(), "brick", string(args)); err != nil {
		t.Fatalf("the built-in slot was refused: %v", sceneRefusal(err))
	}
	for _, o := range f.objectsOf(doc) {
		if o.ID == sceneObjectID("wall") {
			s := o.MaterialSlots["main"]
			if s.Preset != "red_brick@1" || s.Maps != nil || s.Mapping == nil || s.Mapping.SizeM == nil || *s.Mapping.SizeM != m.SizeM {
				t.Fatalf("wall after applying the brick: %+v", s)
			}
			return
		}
	}
	t.Fatal("the wall was not written")
}
