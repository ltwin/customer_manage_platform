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
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
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

// Service is application composition. The model catalog and the skill registry
// are deployment facts; neither is ever selected by a request field.
type Service struct {
	models *llmgateway.Catalog
	skills *SkillRegistry
	limits Limits
}

func NewService(models *llmgateway.Catalog, skills *SkillRegistry) (*Service, error) {
	if models == nil || skills == nil {
		return nil, errors.New("creative agent needs a model catalog and a skill registry")
	}
	return &Service{models: models, skills: skills, limits: DefaultLimits()}, nil
}

// Limits reports the frozen baseline so callers record the same numbers a run
// will be judged by.
func (s *Service) Limits() Limits { return s.limits }

// Compose builds the assistant from the skill packages embedded in this binary
// and the catalog the composition root resolved. The catalog is injected rather
// than loaded again here: the consent whitelist must name exactly the companies
// the dispatcher can reach, and two independent loads can disagree.
func Compose(models *llmgateway.Catalog) (*Service, error) {
	packages, err := LoadEmbeddedSkills()
	if err != nil {
		return nil, err
	}
	skills, err := NewSkillRegistry(packages)
	if err != nil {
		return nil, err
	}
	return NewService(models, skills)
}
