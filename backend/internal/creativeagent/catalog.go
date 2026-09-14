package creativeagent

import (
	"context"
	"strconv"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// SkillEntry is the discovery projection: enough to choose a skill, never the
// instructions. Those are loaded only for a run that fixed this version.
type SkillEntry struct {
	Key         string `json:"key"`
	Version     int    `json:"version"`
	Digest      string `json:"digest"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	MaxInputs   int    `json:"max_inputs"`
	Available   bool   `json:"available"`
	// UnavailableWhy keeps a switched-off skill visible with its reason rather
	// than letting it silently disappear from the picker.
	UnavailableWhy string `json:"unavailable_reason,omitempty"`
}

// ToolEntry describes one registered tool. Effect class and impact are what a
// photographer needs to judge a skill's reach before starting it.
type ToolEntry struct {
	Key         string `json:"key"`
	Version     int    `json:"version"`
	DisplayName string `json:"display_name"`
	EffectClass string `json:"effect_class"`
	MaxImpact   int    `json:"max_impact"`
}

type CatalogView struct {
	CatalogVersion string                    `json:"catalog_version"`
	LimitsVersion  string                    `json:"limits_version"`
	PolicyVersion  string                    `json:"policy_version"`
	Vendors        []string                  `json:"vendors"`
	Models         []llmgateway.CatalogEntry `json:"models"`
	Skills         []SkillEntry              `json:"skills"`
	Tools          []ToolEntry               `json:"tools"`
	Limits         Limits                    `json:"limits"`
}

// Catalog answers what this account may actually start right now. Capabilities
// come from the gateway and the registry; the browser never guesses them.
func (s *Service) Catalog(ctx context.Context, scope store.AccountScope) (CatalogView, error) {
	// Capability first: a caller without creative access learns nothing about
	// this deployment, not even an empty shape built from real configuration.
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.RequireCreativeCapability(ctx, "creative_read")
	}); err != nil {
		return CatalogView{}, err
	}
	view := CatalogView{
		CatalogVersion: s.models.Version(),
		LimitsVersion:  s.limits.Version,
		PolicyVersion:  policyVersion,
		Vendors:        s.models.Vendors(),
		Models:         s.models.List(),
		Skills:         []SkillEntry{},
		Tools:          s.registeredTools(),
		Limits:         s.limits,
	}
	registered := make(map[string]bool, len(view.Tools))
	for _, tool := range view.Tools {
		registered[toolRef(tool.Key, tool.Version)] = true
	}
	for _, p := range s.skills.packagesInOrder() {
		entry := SkillEntry{
			Key: p.Manifest.Key, Version: p.Manifest.Version, Digest: p.Digest,
			DisplayName: p.Manifest.DisplayName, Description: p.Manifest.Description,
			MaxInputs: p.Manifest.MaxInputs, Available: true,
		}
		// A skill whose tools this deployment has not registered cannot reach
		// its own completion standard. Saying so here is better than letting a
		// run discover it halfway through and stop as partial.
		for _, tool := range p.Manifest.ToolAllowlist {
			if !registered[tool] {
				entry.Available = false
				entry.UnavailableWhy = "所需工具尚未在本部署注册：" + tool
				break
			}
		}
		view.Skills = append(view.Skills, entry)
	}
	return view, nil
}

func toolRef(key string, version int) string {
	return key + "@" + strconv.Itoa(version)
}

// registeredTools reports what this deployment can really dispatch. It is empty
// until the read-only runtime tools and the canvas tools are registered, and
// the catalog reflects that honestly instead of advertising an unreachable set.
func (s *Service) registeredTools() []ToolEntry { return []ToolEntry{} }
