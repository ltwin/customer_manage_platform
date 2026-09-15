// Package creativeagent owns the photographer's assistant: the conversations
// and durable messages they keep, the explicit record of what was authorised to
// leave the account, and the runs that answer them.
//
// The account's egress consent is the photographer's own act and is recorded
// here; the model deployment's capability is the gateway's and is checked at
// dispatch. Neither substitutes for the other, and a prompt can never open
// either one.
package creativeagent

import (
	"context"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrNotFound = errors.New("creative agent record not found")
	// ErrRevisionConflict means another window moved the record first. The
	// caller's draft is still theirs; it was simply never saved.
	ErrRevisionConflict = errors.New("creative agent revision conflict")
	// ErrConsentRevoked is returned once an authorisation has been withdrawn.
	// Content already sent cannot be recalled, and we never pretend otherwise.
	ErrConsentRevoked = errors.New("creative egress consent revoked")
	ErrLimit          = errors.New("creative agent limit exceeded")
)

// limitsVersion is frozen with every run. Raising any number below is a
// configuration change that must bump this string, because a run recovered
// tomorrow has to be judged by the limits it was created under.
const limitsVersion = "creative-agent-1"

// policyVersion identifies the egress disclosure a photographer agreed to.
// A new disclosure needs a new consent, never a silent rewrite of an old row.
const policyVersion = "egress-2026-09-1"

// Limits are the deployment's development baseline, not measured capacity.
// Byte counts are UTF-8 bytes; a rune budget would let one emoji cost as much
// as a paragraph on the wire.
type Limits struct {
	Version string `json:"version"`
	// InputTextBytes bounds everything sent to the model in one request:
	// photographer text, frozen inputs, history, skill instructions and tool
	// definitions together.
	InputTextBytes    int `json:"input_text_bytes"`
	MessageBodyBytes  int `json:"message_body_bytes"`
	InputNodes        int `json:"input_nodes"`
	UpstreamDepth     int `json:"upstream_depth"`
	Attachments       int `json:"attachments"`
	ModelResultBytes  int `json:"model_result_bytes"`
	ToolArgumentBytes int `json:"tool_argument_bytes"`
	ToolResultBytes   int `json:"tool_result_bytes"`
	ToolCallsPerTurn  int `json:"tool_calls_per_turn"`
	ToolCalls         int `json:"tool_calls"`
	// ModelTurns counts every real model call, including summarisation and any
	// sub-agent, not only the main ReAct rounds.
	ModelTurns             int   `json:"model_turns"`
	RunDurationSeconds     int   `json:"run_duration_seconds"`
	ImagePreviewBytes      int64 `json:"image_preview_bytes"`
	ImagePreviewCount      int   `json:"image_preview_count"`
	ImagePreviewTotalBytes int64 `json:"image_preview_total_bytes"`
}

// RunDuration is the bounded execution window of one run. Waiting and recovery
// never extend it.
func (l Limits) RunDuration() time.Duration {
	return time.Duration(l.RunDurationSeconds) * time.Second
}

// DefaultLimits reproduces the first-release baseline of the harness design.
func DefaultLimits() Limits {
	return Limits{
		Version:                limitsVersion,
		InputTextBytes:         256 << 10,
		MessageBodyBytes:       32 << 10,
		InputNodes:             50,
		UpstreamDepth:          2,
		Attachments:            10,
		ModelResultBytes:       256 << 10,
		ToolArgumentBytes:      64 << 10,
		ToolResultBytes:        64 << 10,
		ToolCallsPerTurn:       4,
		ToolCalls:              12,
		ModelTurns:             13,
		RunDurationSeconds:     300,
		ImagePreviewBytes:      2 << 20,
		ImagePreviewCount:      8,
		ImagePreviewTotalBytes: 12 << 20,
	}
}

// SkillDirectory is the slice of the skill store this assistant consumes. The
// write ports — import, activate, disable — are deliberately absent: publishing
// is a deployment act reached through the administrative command, and no HTTP
// handler should be one interface assertion away from it.
//
// Cross-account resolution of a platform skill happens behind this port, in the
// skill package, which is why nothing here takes an account other than the
// caller's own (§5).
type SkillDirectory interface {
	ListAccessibleSkills(ctx context.Context, scope store.AccountScope, query, cursor string, limit int) (creativeskill.CatalogPage, error)
	ResolveVersion(ctx context.Context, scope store.AccountScope, skillID, versionID string) (creativeskill.Snapshot, error)
}

// Service is application composition. The model catalog and the skill directory
// are deployment facts; neither is ever selected by a request field.
//
// Skills no longer ship inside the binary. Until an administrator has run the
// import, the directory is simply empty and the catalog says so — which is a
// better answer than a copy compiled in months ago.
type Service struct {
	models *llmgateway.Catalog
	skills SkillDirectory
	limits Limits
}

func NewService(models *llmgateway.Catalog, skills SkillDirectory) (*Service, error) {
	if models == nil || skills == nil {
		return nil, errors.New("creative agent needs a model catalog and a skill directory")
	}
	return &Service{models: models, skills: skills, limits: DefaultLimits()}, nil
}

// Limits reports the frozen baseline so callers record the same numbers a run
// will be judged by.
func (s *Service) Limits() Limits { return s.limits }
