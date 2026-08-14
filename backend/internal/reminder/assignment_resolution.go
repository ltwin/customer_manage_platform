package reminder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

const (
	ResolutionEventApplied     = "event_applied"
	ResolutionRebuildVerified  = "rebuild_verified"
	ResolutionLifecycleApplied = "lifecycle_applied"
	ResolutionSettingsRebuilt  = "settings_rebuilt"
	ResolutionTemporalApplied  = "temporal_applied"

	InboxResolutionEventApplied    = ResolutionEventApplied
	InboxResolutionRebuildVerified = ResolutionRebuildVerified

	QuarantineErrorCorruptPayload   = "corrupt_payload"
	QuarantineErrorIdentityMismatch = "identity_mismatch"
	QuarantineErrorSafeReaderFailed = "safe_reader_failed"

	EpochStatusOpen       = "open"
	EpochStatusComplete   = "complete"
	EpochStatusSuperseded = "superseded"

	EpochPlanPending = "pending"
	EpochPlanLeased  = "leased"
	EpochPlanDone    = "done"
)

var (
	ErrCorruptAssignmentPayload   = errors.New("corrupt_assignment_payload")
	ErrGenerationIdentityMismatch = errors.New("generation_identity_mismatch")
	ErrEpochCannotComplete        = errors.New("epoch_cannot_complete")
	ErrResolutionRequired         = errors.New("resolution_required_before_mark_applied")
)

// GenerationResolutionInput writes one matching universal resolution.
type GenerationResolutionInput struct {
	Generation     int64
	PlanID         string
	MutationKind   planningreminder.MutationKind
	ResolutionKind string
	SourceEventID  *string
}

// WriteGenerationResolutionInScope inserts the universal resolution that must
// exist before MarkApplied may advance the contiguous watermark.
func WriteGenerationResolutionInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	in GenerationResolutionInput,
) error {
	if in.Generation < 1 || in.PlanID == "" || in.MutationKind == "" || in.ResolutionKind == "" {
		return errors.New("generation resolution input incomplete")
	}
	assignment := in.MutationKind == planningreminder.MutationAssignmentActivated ||
		in.MutationKind == planningreminder.MutationAssignmentRevoked
	switch in.ResolutionKind {
	case ResolutionEventApplied, ResolutionRebuildVerified:
		if !assignment || in.SourceEventID == nil || *in.SourceEventID == "" {
			return errors.New("assignment resolution requires source_event_id")
		}
	case ResolutionLifecycleApplied, ResolutionSettingsRebuilt, ResolutionTemporalApplied:
		if assignment || in.SourceEventID != nil {
			return errors.New("non-assignment resolution must not carry source_event_id")
		}
	default:
		return fmt.Errorf("unsupported resolution kind %q", in.ResolutionKind)
	}
	var source any
	if in.SourceEventID != nil {
		source = *in.SourceEventID
	}
	if err := tx.Insert(ctx, "planning_reminder_generation_resolutions",
		[]string{"generation", "plan_id", "mutation_kind", "resolution_kind", "source_event_id", "resolved_at"},
		in.Generation, in.PlanID, string(in.MutationKind), in.ResolutionKind, source, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("insert generation resolution: %w", err)
	}
	return nil
}

// MarkAppliedAfterResolution requires a matching resolution then marks work applied.
func MarkAppliedAfterResolution(
	ctx context.Context,
	locked planningreminder.LockedFenceTx,
	tx store.TxAccountScope,
	generation int64,
) error {
	exists, err := tx.Exists(ctx, "planning_reminder_generation_resolutions", "generation = $2", generation)
	if err != nil {
		return err
	}
	if !exists {
		return ErrResolutionRequired
	}
	return locked.MarkApplied(ctx, generation)
}

// AssignmentInboxRecord is the durable consume ack (no body).
type AssignmentInboxRecord struct {
	SourceEventID           string
	PlanID                  string
	AssignmentID            string
	AssignmentRevision      int64
	AccountSourceGeneration int64
	EventKind               string
	PayloadFingerprint      string
	ResolutionKind          string
}

// UpsertAssignmentInboxInScope writes the assignment-only consume ack.
func UpsertAssignmentInboxInScope(ctx context.Context, tx store.TxAccountScope, row AssignmentInboxRecord) error {
	if row.SourceEventID == "" || row.PlanID == "" || row.AssignmentID == "" ||
		row.AccountSourceGeneration < 1 || row.PayloadFingerprint == "" {
		return errors.New("assignment inbox row incomplete")
	}
	switch row.ResolutionKind {
	case InboxResolutionEventApplied, InboxResolutionRebuildVerified:
	default:
		return fmt.Errorf("invalid inbox resolution kind %q", row.ResolutionKind)
	}
	return tx.Upsert(ctx, "plan_assignment_reminder_inbox",
		[]string{
			"source_event_id", "plan_id", "assignment_id", "assignment_revision",
			"account_source_generation", "event_kind", "payload_fingerprint", "resolution_kind", "consumed_at",
		},
		[]string{"account_id", "source_event_id"},
		[]string{
			"plan_id", "assignment_id", "assignment_revision", "account_source_generation",
			"event_kind", "payload_fingerprint", "resolution_kind", "consumed_at",
		},
		row.SourceEventID, row.PlanID, row.AssignmentID, row.AssignmentRevision,
		row.AccountSourceGeneration, row.EventKind, row.PayloadFingerprint, row.ResolutionKind, time.Now().UTC(),
	)
}

// CountInboxInScope returns inbox rows for tests / mixed-kind guards.
func CountInboxInScope(ctx context.Context, tx store.TxAccountScope) (int64, error) {
	return tx.Count(ctx, "plan_assignment_reminder_inbox", "TRUE")
}
