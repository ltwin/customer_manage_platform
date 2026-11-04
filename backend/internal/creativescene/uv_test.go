package creativescene_test

import (
	"slices"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
)

// read_scene says which slots mapped by uv show triplanar because the model
// has a mesh without UVs. A model baked before manifests recorded uv says
// nothing either way.
func TestAgentReadsTheUVFallback(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	id := objectID(1)
	pillar := newPillar(t, f, doc, id, 10) // basic.glb: one slot, stone, and no UVs
	image := mediaRevision(t, f, f.a, "image")
	mustSend(t, f.a, batch{DocumentID: doc, Expected: readScene(t, f.a, doc).expect(content(id)), Operations: []any{
		op{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "material_slots": map[string]any{
			"stone": principled(map[string]any{"maps": map[string]any{"base_color": texture(image)}, "mapping": map[string]any{"kind": "uv"}})}}}}}})
	model := func() *creativescene.ModelView {
		t.Helper()
		got, err := agentRead(t, f.a, creativescene.ReadInput{DocumentID: doc, ObjectIDs: []string{id}})
		if err != nil {
			t.Fatal(err)
		}
		return got.Objects[0].Model
	}

	if m := model(); m.UV == nil || *m.UV || !slices.Equal(m.UVFallbackSlots, []string{"stone"}) {
		t.Fatalf("a model without UVs: uv %v, fallback %v", m.UV, m.UVFallbackSlots)
	}
	if _, err := f.db.Exec(`UPDATE creative_content_revisions SET payload = payload #- '{model,manifest,uv}' WHERE id=$1`, pillar.ContentRevisionID); err != nil {
		t.Fatal(err)
	}
	if m := model(); m.UV != nil || m.UVFallbackSlots != nil {
		t.Fatalf("a model from before uv: uv %v, fallback %v", m.UV, m.UVFallbackSlots)
	}
}
