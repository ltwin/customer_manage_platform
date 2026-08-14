package planningreminder

import (
	"context"
	"errors"
	"time"
)

// GenerationWorkState is the durable generation work lifecycle.
type GenerationWorkState string

const (
	WorkPending     GenerationWorkState = "pending"
	WorkApplied     GenerationWorkState = "applied"
	WorkQuarantined GenerationWorkState = "quarantined"
)

// GenerationWork is one contiguous account generation unit.
type GenerationWork struct {
	Generation    int64
	PlanID        string
	MutationKind  MutationKind
	SourceEventID *string
	State         GenerationWorkState
	CreatedAt     time.Time
	AppliedAt     *time.Time
}

// PlanningGenerationWorkReader is the core-owned narrow port for listing and
// claiming generation work. Implementations are bound to one physical account
// transaction via BindTrustedWorkReader (same pattern as FenceTxView).
type PlanningGenerationWorkReader interface {
	ListPlanIDsThroughTarget(ctx context.Context, targetGeneration int64) ([]string, error)
	ClaimPending(ctx context.Context, targetGeneration int64, limit int) ([]GenerationWork, error)
	MarkQuarantined(ctx context.Context, generation int64) error
	LoadWork(ctx context.Context, generation int64) (GenerationWork, error)
	planningGenerationWorkReaderSeal()
}

// TrustedWorkOperations is implemented only by the store adapter.
type TrustedWorkOperations struct {
	ListPlanIDs     func(context.Context, int64) ([]string, error)
	ClaimPending    func(context.Context, int64, int) ([]GenerationWork, error)
	MarkQuarantined func(context.Context, int64) error
	LoadWork        func(context.Context, int64) (GenerationWork, error)
}

type boundWorkReader struct {
	ops TrustedWorkOperations
}

// BindTrustedWorkReader seals the store-provided operations behind the port.
func BindTrustedWorkReader(ops TrustedWorkOperations) PlanningGenerationWorkReader {
	return boundWorkReader{ops: ops}
}

func (boundWorkReader) planningGenerationWorkReaderSeal() {}

func (r boundWorkReader) ListPlanIDsThroughTarget(ctx context.Context, targetGeneration int64) ([]string, error) {
	if r.ops.ListPlanIDs == nil {
		return nil, errors.New("planning generation work reader incomplete")
	}
	return r.ops.ListPlanIDs(ctx, targetGeneration)
}

func (r boundWorkReader) ClaimPending(ctx context.Context, targetGeneration int64, limit int) ([]GenerationWork, error) {
	if r.ops.ClaimPending == nil {
		return nil, errors.New("planning generation work reader incomplete")
	}
	return r.ops.ClaimPending(ctx, targetGeneration, limit)
}

func (r boundWorkReader) MarkQuarantined(ctx context.Context, generation int64) error {
	if r.ops.MarkQuarantined == nil {
		return errors.New("planning generation work reader incomplete")
	}
	return r.ops.MarkQuarantined(ctx, generation)
}

func (r boundWorkReader) LoadWork(ctx context.Context, generation int64) (GenerationWork, error) {
	if r.ops.LoadWork == nil {
		return GenerationWork{}, errors.New("planning generation work reader incomplete")
	}
	return r.ops.LoadWork(ctx, generation)
}
