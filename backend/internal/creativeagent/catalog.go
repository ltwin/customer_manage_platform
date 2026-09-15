package creativeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// defaultCatalogSkills bounds the summary the catalog carries. The catalog
// answers "what can this deployment do"; browsing the whole directory is what
// the skills endpoint is for, and inlining an unbounded list here would make
// one request grow with the account.
const defaultCatalogSkills = 20

// SkillEntry is the discovery projection: enough to choose a skill and to name
// it in a later request, never the instructions. Those are loaded only for a
// run that fixed this version.
type SkillEntry struct {
	SkillID   string `json:"skill_id"`
	VersionID string `json:"skill_version_id"`
	// Origin tells a platform skill from one the account published itself. Two
	// skills may share a slug across those two catalogs.
	Origin        string `json:"origin"`
	Slug          string `json:"slug"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	VersionNumber int    `json:"version_number"`
	Digest        string `json:"digest"`
	MaxInputs     int    `json:"max_inputs"`
	Available     bool   `json:"available"`
	// UnavailableWhy keeps a skill this deployment cannot yet run visible with
	// its reason rather than letting it silently disappear from the picker.
	UnavailableWhy string `json:"unavailable_reason,omitempty"`
}

type SkillPage struct {
	Items      []SkillEntry `json:"items"`
	NextCursor string       `json:"next_cursor"`
}

// SkillVersionView is one fixed version's summary and declaration. It is what a
// client gets when it asks about a version a message already references; the
// instructions and the storage addresses stay on the server.
type SkillVersionView struct {
	SkillID        string                   `json:"skill_id"`
	VersionID      string                   `json:"skill_version_id"`
	Origin         string                   `json:"origin"`
	DisplayName    string                   `json:"display_name"`
	Description    string                   `json:"description"`
	VersionNumber  int                      `json:"version_number"`
	SchemaVersion  int                      `json:"schema_version"`
	Digest         string                   `json:"digest"`
	Available      bool                     `json:"available"`
	UnavailableWhy string                   `json:"unavailable_reason,omitempty"`
	Manifest       creativeskill.Manifest   `json:"manifest"`
	Resources      []creativeskill.Resource `json:"resources"`
	CreatedAt      time.Time                `json:"created_at"`
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
	CatalogVersion string `json:"catalog_version"`
	// SkillCatalogRevision changes whenever the skill summary below changes. It
	// is deliberately not the gateway's catalog_version: models come from a
	// configuration file the deployment ships, skills from a database any
	// import can move, and one string cannot answer both questions.
	SkillCatalogRevision string                    `json:"skill_catalog_revision"`
	LimitsVersion        string                    `json:"limits_version"`
	PolicyVersion        string                    `json:"policy_version"`
	Vendors              []string                  `json:"vendors"`
	Models               []llmgateway.CatalogEntry `json:"models"`
	Skills               []SkillEntry              `json:"skills"`
	Tools                []ToolEntry               `json:"tools"`
	Limits               Limits                    `json:"limits"`
}

// Catalog answers what this account may actually start right now. Capabilities
// come from the gateway and the skill directory; the browser never guesses them.
func (s *Service) Catalog(ctx context.Context, scope store.AccountScope) (CatalogView, error) {
	// Capability first: a caller without creative access learns nothing about
	// this deployment, not even an empty shape built from real configuration.
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.RequireCreativeCapability(ctx, "creative_read")
	}); err != nil {
		return CatalogView{}, err
	}
	skills, err := s.listSkills(ctx, scope, "", "", defaultCatalogSkills)
	if err != nil {
		return CatalogView{}, err
	}
	revision, err := skillCatalogRevision(skills.Items)
	if err != nil {
		return CatalogView{}, err
	}
	return CatalogView{
		CatalogVersion:       s.models.Version(),
		SkillCatalogRevision: revision,
		LimitsVersion:        s.limits.Version,
		PolicyVersion:        policyVersion,
		Vendors:              s.models.Vendors(),
		Models:               s.models.List(),
		Skills:               skills.Items,
		Tools:                s.registeredTools(),
		Limits:               s.limits,
	}, nil
}

// ListSkills is the picker's own query: the same directory the catalog summarises,
// searchable and paged. The account's own skills and the trusted platform
// catalog arrive as one list, because a photographer choosing one does not care
// which side of that line it came from.
func (s *Service) ListSkills(ctx context.Context, scope store.AccountScope, query, cursor string, limit int) (SkillPage, error) {
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.RequireCreativeCapability(ctx, "creative_read")
	}); err != nil {
		return SkillPage{}, err
	}
	return s.listSkills(ctx, scope, query, cursor, limit)
}

func (s *Service) listSkills(ctx context.Context, scope store.AccountScope, query, cursor string, limit int) (SkillPage, error) {
	page, err := s.skills.ListAccessibleSkills(ctx, scope, query, cursor, limit)
	if err != nil {
		return SkillPage{}, err
	}
	registered := s.registeredToolRefs()
	items := make([]SkillEntry, 0, len(page.Items))
	for _, entry := range page.Items {
		available, why := runnableHere(registered, entry.ToolAllowlist)
		items = append(items, SkillEntry{
			SkillID: entry.SkillID, VersionID: entry.VersionID, Origin: entry.Origin,
			Slug: entry.Slug, DisplayName: entry.DisplayName, Description: entry.Description,
			VersionNumber: entry.VersionNumber, Digest: entry.Digest, MaxInputs: entry.MaxInputs,
			Available: available, UnavailableWhy: why,
		})
	}
	return SkillPage{Items: items, NextCursor: page.NextCursor}, nil
}

// SkillVersion answers about one fixed version, including one a message froze
// before it was withdrawn. Discovery hides a withdrawn version; a message that
// already references it still has to render.
func (s *Service) SkillVersion(ctx context.Context, scope store.AccountScope, skillID, versionID string) (SkillVersionView, error) {
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return tx.RequireCreativeCapability(ctx, "creative_read")
	}); err != nil {
		return SkillVersionView{}, err
	}
	snapshot, err := s.skills.ResolveVersion(ctx, scope, skillID, versionID)
	if err != nil {
		return SkillVersionView{}, err
	}
	available, why := runnableHere(s.registeredToolRefs(), snapshot.Manifest.ToolAllowlist)
	if !snapshot.Usable() {
		// The author's own switch comes first: a version nobody may start is not
		// made available by the fact that its tools happen to be registered.
		available = false
		why = withdrawnReason(snapshot)
	}
	return SkillVersionView{
		SkillID: snapshot.SkillID, VersionID: snapshot.ID, Origin: snapshot.Origin,
		DisplayName: snapshot.DisplayName, Description: snapshot.Description,
		VersionNumber: snapshot.VersionNumber, SchemaVersion: snapshot.SchemaVersion,
		Digest: snapshot.Digest, Available: available, UnavailableWhy: why,
		Manifest: snapshot.Manifest, Resources: snapshot.Resources, CreatedAt: snapshot.CreatedAt,
	}, nil
}

func withdrawnReason(snapshot creativeskill.Snapshot) string {
	if snapshot.DisabledReason != "" {
		return snapshot.DisabledReason
	}
	return "该 Skill 版本已停用"
}

// runnableHere reports whether this deployment can actually dispatch what a
// skill declares. A skill whose tools are not registered cannot reach its own
// completion standard, and saying so in the picker is better than letting a run
// discover it halfway through and stop as partial.
func runnableHere(registered map[string]bool, allowlist []string) (bool, string) {
	for _, tool := range allowlist {
		if !registered[tool] {
			return false, "所需工具尚未在本部署注册：" + tool
		}
	}
	return true, ""
}

// skillCatalogRevision is a digest of the summary, not a counter. What a client
// needs is "has this changed since I cached it", and the honest answer for a
// per-account view assembled from several rows is a hash of the answer itself.
//
// It hashes the serialised entries rather than a hand-picked set of fields.
// Picking fields means every field added to SkillEntry later has to be
// remembered here, and forgetting one is silent: the client keeps rendering a
// stale name because the revision it cached still matches. Renaming a skill
// without activating a new version changes the entry but not its version id,
// which is exactly the case a field list gets wrong.
func skillCatalogRevision(entries []SkillEntry) (string, error) {
	// Field order in the encoding is the struct's declaration order, so this is
	// stable for a given build; reordering the struct only costs clients one
	// refetch.
	body, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:32], nil
}

func toolRef(key string, version int) string {
	return key + "@" + strconv.Itoa(version)
}

func (s *Service) registeredToolRefs() map[string]bool {
	tools := s.registeredTools()
	refs := make(map[string]bool, len(tools))
	for _, tool := range tools {
		refs[toolRef(tool.Key, tool.Version)] = true
	}
	return refs
}

// registeredTools reports what this deployment can really dispatch. It is empty
// until the read-only runtime tools and the canvas tools are registered, and
// the catalog reflects that honestly instead of advertising an unreachable set.
func (s *Service) registeredTools() []ToolEntry { return []ToolEntry{} }
