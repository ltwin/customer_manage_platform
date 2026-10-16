package creativeagent

import (
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
)

// Like Product.Models, readiness appears only after the generation queue binds.
type deferredGenerator struct {
	stubGenerator
	ready bool
}

func (g *deferredGenerator) Models() []creativegeneration.Model {
	if !g.ready {
		return nil
	}
	return g.stubGenerator.Models()
}

func TestToolCatalogTracksRuntimeReadinessAfterBinding(t *testing.T) {
	f := setup(t)
	generator := &deferredGenerator{}
	f.service.SetGenerator(generator)
	req := skillRequest("platform", "generation-method", "生成方法")
	req.Manifest.ToolAllowlist = []string{"set_node_prompt@1"}
	req.Activate = true
	version := f.runImport(f.platform, req)
	h := &runtimeHandler{service: f.service}
	for _, ready := range []bool{false, true, false} {
		generator.ready = ready
		view, err := f.service.Catalog(t.Context(), f.alice)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range view.Tools {
			if entry.Key == setNodePromptTool {
				found = true
			}
		}
		if got := h.allowed(setNodePromptTool); got != ready || found != got {
			t.Errorf("ready=%v: runtime allowed=%v, catalog has tool=%v", ready, got, found)
		}
		skill, err := f.service.SkillVersion(t.Context(), f.alice, version.SkillID, version.ID)
		if err != nil {
			t.Fatal(err)
		}
		if skill.Available != ready {
			t.Errorf("ready=%v: dependent skill available=%v (%s)", ready, skill.Available, skill.UnavailableWhy)
		}
	}
}
