// Package planningcapability owns the closed deployment capability marker and
// its sealed read/promote interfaces. It intentionally imports neither store
// nor pgx, keeping the dependency direction store -> planningcapability.
package planningcapability

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ArchiveCapability string

const (
	ArchiveCapabilityCore          ArchiveCapability = "core-v1"
	ArchiveCapabilityPlanningShare ArchiveCapability = "planning-share-v1"
	ArchiveCapabilityReminder      ArchiveCapability = "planning-share-reminder-v1"
	SingletonKey                                     = "planning-archive-v1"
)

var (
	ErrInvalidState      = errors.New("invalid planning archive capability state")
	ErrInvalidPromotion  = errors.New("invalid planning archive capability promotion")
	ErrPromotionConflict = errors.New("planning archive capability revision conflict")
	ErrReadiness         = errors.New("planning archive release readiness is invalid")
)

type ArchiveCapabilityState struct {
	SingletonKey string
	Capability   ArchiveCapability
	Revision     int64
	PromotedAt   time.Time
}

type ArchiveCapabilityTransition struct {
	Before ArchiveCapabilityState
	After  ArchiveCapabilityState
}

type ReleaseReadinessEvidence struct {
	Digest               string
	Environment          string
	Deployment           string
	Target               ArchiveCapability
	CurrentRevision      int64
	GeneratedAt          time.Time
	ExpiresAt            time.Time
	LiveBuildsCompatible bool
	TargetWiringReady    bool
}

type ArchiveCapabilityTxView interface {
	Current(context.Context) (ArchiveCapabilityState, error)
	archiveCapabilityTxViewSeal()
}

type ArchiveCapabilityStartupReader interface {
	Current(context.Context) (ArchiveCapabilityState, error)
}

type ArchiveCapabilityPromoter interface {
	Show(context.Context) (ArchiveCapabilityState, error)
	Promote(context.Context, int64, ArchiveCapability, ReleaseReadinessEvidence) (ArchiveCapabilityTransition, error)
}

type txView struct {
	read func(context.Context) (ArchiveCapabilityState, error)
}
type startupReader struct {
	read func(context.Context) (ArchiveCapabilityState, error)
}
type promoter struct {
	show func(context.Context) (ArchiveCapabilityState, error)
	cas  func(context.Context, int64, ArchiveCapability) (ArchiveCapabilityState, error)
	now  func() time.Time
}

func BindTrustedTxReader(read func(context.Context) (ArchiveCapabilityState, error)) ArchiveCapabilityTxView {
	return &txView{read: read}
}

func BindTrustedStartupReader(read func(context.Context) (ArchiveCapabilityState, error)) ArchiveCapabilityStartupReader {
	return &startupReader{read: read}
}

func BindTrustedPromoter(
	show func(context.Context) (ArchiveCapabilityState, error),
	cas func(context.Context, int64, ArchiveCapability) (ArchiveCapabilityState, error),
) ArchiveCapabilityPromoter {
	return &promoter{show: show, cas: cas, now: time.Now}
}

func (*txView) archiveCapabilityTxViewSeal() {}

func (v *txView) Current(ctx context.Context) (ArchiveCapabilityState, error) {
	if v == nil || v.read == nil {
		return ArchiveCapabilityState{}, ErrInvalidState
	}
	return readAndValidate(ctx, v.read)
}

func (r *startupReader) Current(ctx context.Context) (ArchiveCapabilityState, error) {
	if r == nil || r.read == nil {
		return ArchiveCapabilityState{}, ErrInvalidState
	}
	return readAndValidate(ctx, r.read)
}

func (p *promoter) Show(ctx context.Context) (ArchiveCapabilityState, error) {
	if p == nil || p.show == nil {
		return ArchiveCapabilityState{}, ErrInvalidState
	}
	return readAndValidate(ctx, p.show)
}

func (p *promoter) Promote(
	ctx context.Context,
	expectedRevision int64,
	target ArchiveCapability,
	readiness ReleaseReadinessEvidence,
) (ArchiveCapabilityTransition, error) {
	if p == nil || p.cas == nil || p.now == nil {
		return ArchiveCapabilityTransition{}, ErrInvalidState
	}
	before, err := p.Show(ctx)
	if err != nil {
		return ArchiveCapabilityTransition{}, err
	}
	if before.Revision != expectedRevision {
		return ArchiveCapabilityTransition{}, ErrPromotionConflict
	}
	if !Adjacent(before.Capability, target) {
		return ArchiveCapabilityTransition{}, ErrInvalidPromotion
	}
	if err := validateReadiness(readiness, before, target, p.now().UTC()); err != nil {
		return ArchiveCapabilityTransition{}, err
	}
	after, err := p.cas(ctx, expectedRevision, target)
	if err != nil {
		return ArchiveCapabilityTransition{}, err
	}
	if err := ValidateState(after); err != nil {
		return ArchiveCapabilityTransition{}, err
	}
	if after.Capability != target || after.Revision != expectedRevision+1 {
		return ArchiveCapabilityTransition{}, fmt.Errorf("%w: authoritative readback mismatch", ErrInvalidPromotion)
	}
	return ArchiveCapabilityTransition{Before: before, After: after}, nil
}

func ValidateState(state ArchiveCapabilityState) error {
	if state.SingletonKey != SingletonKey || state.Revision < 1 || state.PromotedAt.IsZero() || !Known(state.Capability) {
		return ErrInvalidState
	}
	return nil
}

func Known(capability ArchiveCapability) bool {
	switch capability {
	case ArchiveCapabilityCore, ArchiveCapabilityPlanningShare, ArchiveCapabilityReminder:
		return true
	default:
		return false
	}
}

func Adjacent(current, target ArchiveCapability) bool {
	return current == ArchiveCapabilityCore && target == ArchiveCapabilityPlanningShare ||
		current == ArchiveCapabilityPlanningShare && target == ArchiveCapabilityReminder
}

func readAndValidate(ctx context.Context, read func(context.Context) (ArchiveCapabilityState, error)) (ArchiveCapabilityState, error) {
	state, err := read(ctx)
	if err != nil {
		return ArchiveCapabilityState{}, err
	}
	if err := ValidateState(state); err != nil {
		return ArchiveCapabilityState{}, err
	}
	return state, nil
}

func validateReadiness(evidence ReleaseReadinessEvidence, current ArchiveCapabilityState, target ArchiveCapability, now time.Time) error {
	if evidence.Digest == "" || evidence.Environment == "" || evidence.Deployment == "" ||
		evidence.Target != target || evidence.CurrentRevision != current.Revision ||
		evidence.GeneratedAt.IsZero() || evidence.ExpiresAt.IsZero() ||
		now.Before(evidence.GeneratedAt) || !now.Before(evidence.ExpiresAt) ||
		!evidence.LiveBuildsCompatible || !evidence.TargetWiringReady {
		return ErrReadiness
	}
	return nil
}
