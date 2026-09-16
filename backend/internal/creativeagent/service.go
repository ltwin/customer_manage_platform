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
	"log/slog"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
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
//
// Adding a dimension bumps it too, even though no existing number moved. A run
// created under -1 was never judged against a segment count or a skill package
// bound; replaying it against -2's set would apply rules it never agreed to.
// What is frozen is the whole judgement, not the individual numbers.
const limitsVersion = "creative-agent-3"

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
	InputTextBytes   int `json:"input_text_bytes"`
	MessageBodyBytes int `json:"message_body_bytes"`
	// InstructionSegments bounds one submission's ordered pieces. It is the
	// same number as the message body's block count because one is projected
	// from the other: a submission that parses must be able to render.
	InstructionSegments int `json:"instruction_segments"`
	// SkillRefsPerInstruction is 1 in this phase. Two skills in one request is
	// refused rather than concatenated — stitching two sets of instructions
	// together produces a third that neither author wrote.
	SkillRefsPerInstruction int `json:"skill_refs_per_instruction"`
	// SkillResourceFiles and SkillPackageBytes are the skill store's own bounds,
	// published here because a run is judged by the limits it was created under.
	SkillResourceFiles int `json:"skill_resource_files"`
	SkillPackageBytes  int `json:"skill_package_bytes"`
	// HistoryMessages bounds how many earlier turns one run freezes. The real
	// cap is InputTextBytes; this only bounds the number of rows written, since
	// a thousand one-word turns would fit the byte budget and still be a
	// thousand rows.
	HistoryMessages   int `json:"history_messages"`
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
		Version:          limitsVersion,
		InputTextBytes:   256 << 10,
		MessageBodyBytes: 32 << 10,
		// Projected, never retyped. Two spellings of one bound is how a package
		// that imports cleanly starts failing at execution.
		InstructionSegments:     maxBlocks,
		SkillRefsPerInstruction: maxSkillRefs,
		SkillResourceFiles:      creativeskill.MaxResourceCount,
		SkillPackageBytes:       creativeskill.MaxPackageBytes,
		HistoryMessages:         20,
		InputNodes:              50,
		UpstreamDepth:           2,
		Attachments:             10,
		ModelResultBytes:        256 << 10,
		ToolArgumentBytes:       64 << 10,
		ToolResultBytes:         64 << 10,
		ToolCallsPerTurn:        4,
		ToolCalls:               12,
		ModelTurns:              13,
		RunDurationSeconds:      300,
		ImagePreviewBytes:       2 << 20,
		ImagePreviewCount:       8,
		ImagePreviewTotalBytes:  12 << 20,
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
	// RequireRunnableInTx re-checks the mutable half — may this version still
	// start work — inside the caller's own transaction, holding the version
	// control lock. It takes a transaction rather than a scope because that is
	// the whole point: an answer in a transaction of its own is already stale
	// by the time the dispatch intent commits.
	RequireRunnableInTx(ctx context.Context, tx store.TxAccountScope, skillID, versionID string) error
}

// Service is application composition. The model catalog, the gateway and the
// skill directory are deployment facts; none is ever selected by a request
// field.
//
// Skills no longer ship inside the binary. Until an administrator has run the
// import, the directory is simply empty and the catalog says so — which is a
// better answer than a copy compiled in months ago.
type Service struct {
	gateway *llmgateway.Service
	models  *llmgateway.Catalog
	skills  SkillDirectory
	// tools is what this deployment can dispatch. Empty until FND-08 registers
	// the runtime and canvas tools; a skill declaring anything is reported as
	// unavailable with its reason until then.
	tools  []ToolEntry
	limits Limits
	// runtime is the queue this service enqueues into inside its own
	// transaction. A deployment whose queue schema is absent leaves it nil, and
	// creating a run then fails in the transaction rather than leaving a run
	// nobody will ever execute.
	runtime jobs.Runtime
	logger  *slog.Logger
}

// NewService takes the gateway as well as its catalog. The two must be the same
// deployment's: a consent whitelist built from one directory and a dispatcher
// reading another could authorise a vendor this process never reaches.
func NewService(gateway *llmgateway.Service, models *llmgateway.Catalog, skills SkillDirectory) (*Service, error) {
	if gateway == nil || models == nil || skills == nil {
		return nil, errors.New("creative agent needs a gateway, a model catalog and a skill directory")
	}
	return &Service{gateway: gateway, models: models, skills: skills, limits: DefaultLimits(), logger: slog.Default()}, nil
}

// SetRuntime binds the queue used for same-transaction enqueue.
func (s *Service) SetRuntime(runtime jobs.Runtime) { s.runtime = runtime }

// SetLogger receives the process logger. Failure detail goes here, never into a
// client-visible error code.
func (s *Service) SetLogger(logger *slog.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// Handlers registers the worker stages this service owns. One run advances
// through one task kind; the payload names the run and nothing else, so the
// worker re-reads every fact from the database.
func (s *Service) Handlers() []store.JobHandler {
	return []store.JobHandler{
		// The attempt budget covers a whole bounded run, which is allowed to
		// spend its full execution window inside one model turn.
		{Kind: jobRunKind, Work: s.workRun, Timeout: s.limits.RunDuration() + time.Minute},
	}
}

// Limits reports the frozen baseline so callers record the same numbers a run
// will be judged by.
func (s *Service) Limits() Limits { return s.limits }
