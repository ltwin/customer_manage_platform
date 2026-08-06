// Package planningreminder owns the neutral, transaction-bound account
// generation fence shared by planning-related reminder writers.
package planningreminder

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

type MutationKind string

const (
	MutationCRMPlanLinkChanged       MutationKind = "crm_plan_link_changed"
	MutationCRMOrderLifecycleChanged MutationKind = "crm_order_lifecycle_changed"
	MutationCRMScheduleChanged       MutationKind = "crm_schedule_changed"
	MutationSettingsTimezoneChanged  MutationKind = "settings_timezone_changed"
	MutationPlanArchived             MutationKind = "plan_archived"
	MutationAssignmentActivated      MutationKind = "assignment_activated"
	MutationAssignmentRevoked        MutationKind = "assignment_revoked"
	MutationShootStarted             MutationKind = "shoot_started"
)

var (
	ErrFenceNotLocked      = errors.New("planning reminder fence is not locked")
	ErrDuplicateFact       = errors.New("planning reminder fact already reserved by this token")
	ErrFactOrder           = errors.New("planning reminder facts must be reserved by plan id ascending")
	ErrInvalidMutationFact = errors.New("invalid planning reminder mutation fact")
)

type MutationFact struct {
	PlanID        string
	MutationKind  MutationKind
	SourceEventID *string
}

type FenceTxView interface {
	LockCurrentAccount(context.Context) (LockedFenceTx, error)
	planningReminderFenceTxViewSeal()
}

type LockedFenceTx interface {
	ReserveGeneration(context.Context, MutationFact) (int64, error)
	MarkApplied(context.Context, int64) error
	planningReminderLockedFenceTxSeal()
}

// TrustedFenceOperations is implemented only by the store adapter. Its
// functions are already bound to one physical transaction and current account.
type TrustedFenceOperations struct {
	Lock    func(context.Context) error
	Reserve func(context.Context, MutationFact) (int64, error)
	Apply   func(context.Context, int64) error
}

type boundFence struct {
	ops TrustedFenceOperations
}

type lockedFence struct {
	ops      TrustedFenceOperations
	mu       sync.Mutex
	reserved map[string]struct{}
	lastPlan string
}

func BindTrustedFenceTxView(ops TrustedFenceOperations) FenceTxView {
	return &boundFence{ops: ops}
}

func (*boundFence) planningReminderFenceTxViewSeal() {}

func (f *boundFence) LockCurrentAccount(ctx context.Context) (LockedFenceTx, error) {
	if f == nil || f.ops.Lock == nil || f.ops.Reserve == nil || f.ops.Apply == nil {
		return nil, errors.New("planning reminder fence adapter is incomplete")
	}
	if err := f.ops.Lock(ctx); err != nil {
		return nil, err
	}
	return &lockedFence{ops: f.ops, reserved: make(map[string]struct{})}, nil
}

func (*lockedFence) planningReminderLockedFenceTxSeal() {}

func (f *lockedFence) ReserveGeneration(ctx context.Context, fact MutationFact) (int64, error) {
	if f == nil {
		return 0, ErrFenceNotLocked
	}
	if err := ValidateMutationFact(fact); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lastPlan != "" && fact.PlanID < f.lastPlan {
		return 0, ErrFactOrder
	}
	key := factKey(fact)
	if _, exists := f.reserved[key]; exists {
		return 0, ErrDuplicateFact
	}
	generation, err := f.ops.Reserve(ctx, fact)
	if err != nil {
		return 0, err
	}
	f.reserved[key] = struct{}{}
	f.lastPlan = fact.PlanID
	return generation, nil
}

func (f *lockedFence) MarkApplied(ctx context.Context, generation int64) error {
	if f == nil {
		return ErrFenceNotLocked
	}
	if generation < 1 {
		return errors.New("generation must be positive")
	}
	return f.ops.Apply(ctx, generation)
}

func ValidateMutationFact(fact MutationFact) error {
	if fact.PlanID == "" || !validMutationKind(fact.MutationKind) {
		return ErrInvalidMutationFact
	}
	assignment := fact.MutationKind == MutationAssignmentActivated || fact.MutationKind == MutationAssignmentRevoked
	if assignment != (fact.SourceEventID != nil && *fact.SourceEventID != "") {
		return ErrInvalidMutationFact
	}
	return nil
}

func SortFacts(facts []MutationFact) {
	sort.Slice(facts, func(i, j int) bool { return facts[i].PlanID < facts[j].PlanID })
}

func factKey(fact MutationFact) string {
	source := ""
	if fact.SourceEventID != nil {
		source = *fact.SourceEventID
	}
	return fmt.Sprintf("%s\x00%s\x00%s", fact.PlanID, fact.MutationKind, source)
}

func validMutationKind(kind MutationKind) bool {
	switch kind {
	case MutationCRMPlanLinkChanged, MutationCRMOrderLifecycleChanged, MutationCRMScheduleChanged,
		MutationSettingsTimezoneChanged, MutationPlanArchived, MutationAssignmentActivated,
		MutationAssignmentRevoked, MutationShootStarted:
		return true
	default:
		return false
	}
}
