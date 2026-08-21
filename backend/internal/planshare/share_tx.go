package planshare

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/planningmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

const shareAssetRefByteLen = 18

type shareTxScope struct {
	tx          store.TxAccountScope
	fingerprint string
	fence       planningreminder.FenceTxView
	media       *planningmedia.Application
}

func BindShareTxScope(
	tx store.TxAccountScope,
	fingerprint string,
	media *planningmedia.Application,
) ShareTxScope {
	return shareTxScope{
		tx:          tx,
		fingerprint: fingerprint,
		fence:       tx.PlanningReminderFence(),
		media:       media,
	}
}

func (s shareTxScope) PlanningReminderFence() planningreminder.FenceTxView { return s.fence }
func (s shareTxScope) Generations() ShareGenerationStore {
	return shareGenerationStore{tx: s.tx, fingerprint: s.fingerprint}
}
func (s shareTxScope) Feedback() ShareFeedbackStore {
	return shareFeedbackStore{tx: s.tx}
}
func (s shareTxScope) Assignments() ShareAssignmentStore {
	return shareAssignmentStore{tx: s.tx}
}
func (s shareTxScope) SharedAssetRefs() SharedAssetRefStore {
	return shareAssetRefStore{tx: s.tx}
}
func (s shareTxScope) Observations() ShareObservationStore {
	return shareObservationStore{tx: s.tx}
}
func (s shareTxScope) AssignmentSourceEvents() ShareAssignmentSourceEventStore {
	return shareAssignmentSourceEventStore{tx: s.tx}
}
func (s shareTxScope) ReplayAdmissions() ShareReplayAdmissionStore {
	return shareReplayAdmissionStore{tx: s.tx}
}
func (s shareTxScope) PlanSources() PlanShareSourceReader { return shareSourceReader{tx: s.tx} }
func (s shareTxScope) Eligibility() ShareEligibilityReader {
	return shareEligibilityReader{tx: s.tx, fingerprint: s.fingerprint}
}
func (s shareTxScope) Media() ShareMediaReader {
	return shareMediaReader{tx: s.tx, media: s.media}
}

type shareGenerationStore struct {
	tx          store.TxAccountScope
	fingerprint string
}

func (shareGenerationStore) shareGenerationStoreSeal() {}

func (s shareGenerationStore) LoadByValidatedContext(ctx context.Context) (ShareGeneration, error) {
	return loadGenerationByFingerprint(ctx, s.tx, s.fingerprint)
}

func (s shareGenerationStore) TouchFirstOpenedAt(ctx context.Context, now time.Time) error {
	_, err := s.tx.Update(ctx, "share_generations",
		"first_opened_at = COALESCE(first_opened_at, $2)",
		"fingerprint = $3 AND state = 'active' AND first_opened_at IS NULL",
		now.UTC(), s.fingerprint)
	return err
}

func (s shareGenerationStore) LatchEligibilityInvalidated(ctx context.Context, now time.Time) error {
	_, err := s.tx.Update(ctx, "share_generations",
		"state = $2, invalidated_at = $3, revision = revision + 1",
		"fingerprint = $4 AND state = 'active'",
		string(GenerationStateEligibilityInvalidated), now.UTC(), s.fingerprint)
	return err
}

func loadGenerationByFingerprint(
	ctx context.Context,
	tx store.TxAccountScope,
	fingerprint string,
) (ShareGeneration, error) {
	row := tx.QueryRow(ctx, "share_generations", generationColumns, "fingerprint = $2", fingerprint)
	gen, err := scanGeneration(row)
	if errors.Is(err, store.ErrNoRows) {
		return ShareGeneration{}, ErrShareNotFound
	}
	return gen, err
}

type shareAssetRefStore struct{ tx store.TxAccountScope }

func (shareAssetRefStore) sharedAssetRefStoreSeal() {}

func (s shareAssetRefStore) UpsertMoodboardRef(
	ctx context.Context,
	input SharedAssetRefUpsert,
) (SharedMoodboardItemV1, error) {
	refRaw := make([]byte, shareAssetRefByteLen)
	if _, err := rand.Read(refRaw); err != nil {
		return SharedMoodboardItemV1{}, fmt.Errorf("generate share asset ref: %w", err)
	}
	ref := "sar_" + base64.RawURLEncoding.EncodeToString(refRaw)
	id := "sarid_" + uuid.NewString()
	row := s.tx.InsertOnConflictDoNothingReturning(ctx, "share_asset_access_refs",
		[]string{
			"id", "plan_id", "token_generation_id", "binding_id", "asset_id",
			"exact_generation", "display_checksum", "ref", "state",
		},
		[]string{
			"account_id", "token_generation_id", "binding_id", "exact_generation", "display_checksum",
		},
		[]string{"ref", "display_checksum"},
		id,
		input.PlanID,
		input.TokenGenerationID,
		input.Binding.BindingID,
		input.Binding.AssetID,
		input.Binding.ExactGeneration,
		input.Binding.DisplayChecksum,
		ref,
		"active",
	)
	var storedRef, checksum string
	err := row.Scan(&storedRef, &checksum)
	if err == nil {
		return SharedMoodboardItemV1{Ref: storedRef, Checksum: checksum, Caption: input.Binding.Caption, UsageNote: input.Binding.UsageNote}, nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return SharedMoodboardItemV1{}, err
	}
	err = s.tx.QueryRow(ctx, "share_asset_access_refs",
		"ref, display_checksum",
		"token_generation_id = $2 AND binding_id = $3 AND exact_generation = $4 AND display_checksum = $5 AND state = 'active'",
		input.TokenGenerationID, input.Binding.BindingID, input.Binding.ExactGeneration, input.Binding.DisplayChecksum,
	).Scan(&storedRef, &checksum)
	if errors.Is(err, store.ErrNoRows) {
		return SharedMoodboardItemV1{}, fmt.Errorf("share asset ref upsert conflict without row")
	}
	if err != nil {
		return SharedMoodboardItemV1{}, err
	}
	return SharedMoodboardItemV1{Ref: storedRef, Checksum: checksum, Caption: input.Binding.Caption, UsageNote: input.Binding.UsageNote}, nil
}

func (s shareAssetRefStore) LoadActiveByRef(
	ctx context.Context,
	tokenGenerationID, ref string,
) (SharedAssetRefRow, error) {
	var row SharedAssetRefRow
	err := s.tx.QueryRow(ctx, "share_asset_access_refs",
		"plan_id, token_generation_id, binding_id, asset_id, exact_generation, display_checksum, ref",
		"token_generation_id = $2 AND ref = $3 AND state = 'active'",
		tokenGenerationID, ref,
	).Scan(
		&row.PlanID, &row.TokenGenerationID, &row.BindingID, &row.AssetID,
		&row.ExactGeneration, &row.DisplayChecksum, &row.Ref,
	)
	if errors.Is(err, store.ErrNoRows) {
		return SharedAssetRefRow{}, ErrShareNotFound
	}
	if err != nil {
		return SharedAssetRefRow{}, err
	}
	return row, nil
}

type shareObservationStore struct{ tx store.TxAccountScope }

func (shareObservationStore) shareObservationStoreSeal() {}

func (s shareObservationStore) EnsureFullOpen(ctx context.Context, input FullOpenObservationInput) error {
	var existing string
	err := s.tx.QueryRow(ctx, "share_interaction_observations", "id",
		"token_generation_id = $2 AND kind = 'full_open'", input.TokenGenerationID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return err
	}
	insertErr := s.tx.Insert(ctx, "share_interaction_observations",
		[]string{
			"id", "plan_id", "token_generation_id", "slot_id", "execution_window_revision",
			"kind", "policy_version", "occurred_at",
		},
		"sio_"+uuid.NewString(),
		input.PlanID,
		input.TokenGenerationID,
		nullableString(input.SlotID),
		nullableInt64(input.ExecutionWindowRevision),
		"full_open",
		input.PolicyVersion,
		input.OccurredAt.UTC(),
	)
	if store.IsUniqueViolation(insertErr, "share_interaction_observations_full_open_uniq") {
		return nil
	}
	return insertErr
}

func (s shareObservationStore) EnsureFeedback(ctx context.Context, input FeedbackObservationInput) error {
	var existing string
	err := s.tx.QueryRow(ctx, "share_interaction_observations", "id",
		"kind = $2 AND source_fact_id = $3", input.Kind, input.SourceFactID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return err
	}
	insertErr := s.tx.Insert(ctx, "share_interaction_observations",
		[]string{
			"id", "plan_id", "token_generation_id", "slot_id", "execution_window_revision",
			"kind", "source_fact_id", "policy_version", "occurred_at",
		},
		"sio_"+uuid.NewString(),
		input.PlanID,
		input.TokenGenerationID,
		nullableString(input.SlotID),
		nullableInt64(input.WindowRevision),
		input.Kind,
		input.SourceFactID,
		input.PolicyVersion,
		input.OccurredAt.UTC(),
	)
	if store.IsUniqueViolation(insertErr, "share_interaction_observations_source_fact_uniq") {
		return nil
	}
	return insertErr
}

func (s shareObservationStore) EnsureAssignment(ctx context.Context, input AssignmentObservationInput) error {
	var existing string
	err := s.tx.QueryRow(ctx, "share_interaction_observations", "id",
		"kind = $2 AND source_fact_id = $3", input.Kind, input.SourceFactID).Scan(&existing)
	if err == nil {
		return nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return err
	}
	insertErr := s.tx.Insert(ctx, "share_interaction_observations",
		[]string{
			"id", "plan_id", "token_generation_id", "slot_id", "execution_window_revision",
			"kind", "source_fact_id", "source_fact_revision", "policy_version", "occurred_at",
		},
		"sio_"+uuid.NewString(),
		input.PlanID,
		input.TokenGenerationID,
		nullableString(input.SlotID),
		nullableInt64(input.WindowRevision),
		input.Kind,
		input.SourceFactID,
		nullableInt64(input.SourceFactRevision),
		input.PolicyVersion,
		input.OccurredAt.UTC(),
	)
	if store.IsUniqueViolation(insertErr, "share_interaction_observations_source_fact_uniq") {
		return nil
	}
	return insertErr
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

type shareFeedbackStore struct{ tx store.TxAccountScope }

func (shareFeedbackStore) shareFeedbackStoreSeal() {}

func (s shareFeedbackStore) Insert(ctx context.Context, row ShareFeedbackInsert) (ShareFeedbackRow, error) {
	err := s.tx.Insert(ctx, "share_feedbacks",
		[]string{
			"id", "plan_id", "token_generation_id", "target_kind", "target_id", "target_revision",
			"author_display_name", "content", "disposition", "revision",
			"deep_link_kind", "deep_link_shot_id", "created_at",
		},
		row.ID,
		row.PlanID,
		row.TokenGenerationID,
		string(row.TargetKind),
		nullableString(row.TargetID),
		row.TargetRevision,
		row.AuthorDisplayName,
		row.Content,
		string(FeedbackDispositionPending),
		int64(1),
		string(row.DeepLinkKind),
		nullableString(row.DeepLinkShotID),
		row.CreatedAt.UTC(),
	)
	if err != nil {
		return ShareFeedbackRow{}, err
	}
	return ShareFeedbackRow{
		ID:                row.ID,
		PlanID:            row.PlanID,
		TokenGenerationID: row.TokenGenerationID,
		TargetKind:        row.TargetKind,
		TargetID:          row.TargetID,
		TargetRevision:    row.TargetRevision,
		AuthorDisplayName: row.AuthorDisplayName,
		Content:           row.Content,
		Disposition:       FeedbackDispositionPending,
		Revision:          1,
		DeepLinkKind:      row.DeepLinkKind,
		DeepLinkShotID:    row.DeepLinkShotID,
		CreatedAt:         row.CreatedAt.UTC(),
	}, nil
}

func (s shareFeedbackStore) LockByID(ctx context.Context, planID, feedbackID string) (ShareFeedbackRow, error) {
	row := s.tx.QueryRowForUpdate(ctx, "share_feedbacks", feedbackColumns,
		"plan_id = $2 AND id = $3", planID, feedbackID)
	return scanFeedbackRow(row)
}

func (s shareFeedbackStore) UpdateDisposition(
	ctx context.Context,
	planID, feedbackID string,
	expectedRevision int64,
	disposition FeedbackDisposition,
	now time.Time,
) (ShareFeedbackRow, error) {
	n, err := s.tx.Update(ctx, "share_feedbacks",
		"disposition = $2, disposition_at = $3, revision = revision + 1",
		"plan_id = $4 AND id = $5 AND revision = $6",
		string(disposition), now.UTC(), planID, feedbackID, expectedRevision,
	)
	if err != nil {
		return ShareFeedbackRow{}, err
	}
	if n == 0 {
		return ShareFeedbackRow{}, ErrFeedbackStale
	}
	row := s.tx.QueryRow(ctx, "share_feedbacks", feedbackColumns,
		"plan_id = $2 AND id = $3", planID, feedbackID)
	return scanFeedbackRow(row)
}

const feedbackColumns = "id, plan_id, token_generation_id, target_kind, target_id, target_revision, " +
	"author_display_name, content, disposition, revision, deep_link_kind, deep_link_shot_id, created_at, disposition_at"

func scanFeedbackRow(row interface{ Scan(...any) error }) (ShareFeedbackRow, error) {
	var out ShareFeedbackRow
	var targetKind, disposition, deepLinkKind string
	var targetID, deepLinkShotID sql.NullString
	var dispositionAt sql.NullTime
	err := row.Scan(
		&out.ID, &out.PlanID, &out.TokenGenerationID, &targetKind, &targetID, &out.TargetRevision,
		&out.AuthorDisplayName, &out.Content, &disposition, &out.Revision, &deepLinkKind, &deepLinkShotID,
		&out.CreatedAt, &dispositionAt,
	)
	if errors.Is(err, store.ErrNoRows) {
		return ShareFeedbackRow{}, ErrFeedbackNotFound
	}
	if err != nil {
		return ShareFeedbackRow{}, err
	}
	out.TargetKind = FeedbackTargetKind(targetKind)
	out.Disposition = FeedbackDisposition(disposition)
	out.DeepLinkKind = DeepLinkKind(deepLinkKind)
	out.CreatedAt = out.CreatedAt.UTC()
	if targetID.Valid {
		value := targetID.String
		out.TargetID = &value
	}
	if deepLinkShotID.Valid {
		value := deepLinkShotID.String
		out.DeepLinkShotID = &value
	}
	if dispositionAt.Valid {
		value := dispositionAt.Time.UTC()
		out.DispositionAt = &value
	}
	return out, nil
}

type shareAssignmentStore struct{ tx store.TxAccountScope }

func (shareAssignmentStore) shareAssignmentStoreSeal() {}

const assignmentColumns = "id, plan_id, token_generation_id, assignment_kind, readiness_item_id, offer_id, " +
	"content_snapshot, claimed_by_display_name, preparation_lead_days_snapshot, lead_rule_version, " +
	"status, claim_receipt_commitment, revision, claimed_at, revoked_at, revoked_by"

func (s shareAssignmentStore) Insert(ctx context.Context, row ShareAssignmentInsert) (ShareAssignmentRow, error) {
	err := s.tx.Insert(ctx, "share_assignments",
		[]string{
			"id", "plan_id", "token_generation_id", "assignment_kind", "readiness_item_id", "offer_id",
			"content_snapshot", "claimed_by_display_name", "preparation_lead_days_snapshot", "lead_rule_version",
			"status", "claim_receipt_commitment", "revision", "claimed_at",
		},
		row.ID,
		row.PlanID,
		row.TokenGenerationID,
		string(row.AssignmentKind),
		nullableString(row.ReadinessItemID),
		nullableString(row.OfferID),
		row.ContentSnapshot,
		row.ClaimedByDisplayName,
		nullableInt(row.PreparationLeadDaysSnapshot),
		nullableString(row.LeadRuleVersion),
		string(AssignmentStatusActive),
		row.ClaimReceiptCommitment[:],
		int64(1),
		row.ClaimedAt.UTC(),
	)
	if store.IsUniqueViolation(err, "share_assignments_active_target_uniq") {
		return ShareAssignmentRow{}, ErrAssignmentAlreadyClaimed
	}
	if err != nil {
		return ShareAssignmentRow{}, err
	}
	return s.LockByID(ctx, row.PlanID, row.ID)
}

func (s shareAssignmentStore) LockByID(ctx context.Context, planID, assignmentID string) (ShareAssignmentRow, error) {
	row := s.tx.QueryRowForUpdate(ctx, "share_assignments", assignmentColumns,
		"plan_id = $2 AND id = $3", planID, assignmentID)
	out, err := scanAssignmentRow(row)
	if errors.Is(err, store.ErrNoRows) {
		return ShareAssignmentRow{}, ErrAssignmentNotFound
	}
	return out, err
}

func (s shareAssignmentStore) LockActiveByTarget(
	ctx context.Context,
	planID string,
	kind AssignmentKind,
	readinessItemID, offerID *string,
) (ShareAssignmentRow, error) {
	return s.loadActiveByTarget(ctx, planID, kind, readinessItemID, offerID, true)
}

func (s shareAssignmentStore) FindActiveByTarget(
	ctx context.Context,
	planID string,
	kind AssignmentKind,
	readinessItemID, offerID *string,
) (ShareAssignmentRow, error) {
	return s.loadActiveByTarget(ctx, planID, kind, readinessItemID, offerID, false)
}

func (s shareAssignmentStore) loadActiveByTarget(
	ctx context.Context,
	planID string,
	kind AssignmentKind,
	readinessItemID, offerID *string,
	forUpdate bool,
) (ShareAssignmentRow, error) {
	var row store.Row
	query := func(cond string, args ...any) store.Row {
		if forUpdate {
			return s.tx.QueryRowForUpdate(ctx, "share_assignments", assignmentColumns, cond, args...)
		}
		return s.tx.QueryRow(ctx, "share_assignments", assignmentColumns, cond, args...)
	}
	switch kind {
	case AssignmentKindReadiness:
		if readinessItemID == nil {
			return ShareAssignmentRow{}, validationError("readiness target required")
		}
		row = query(
			"plan_id = $2 AND assignment_kind = 'readiness' AND readiness_item_id = $3 AND status = 'active'",
			planID, *readinessItemID,
		)
	case AssignmentKindOnSiteSupport:
		if offerID == nil {
			return ShareAssignmentRow{}, validationError("offer target required")
		}
		row = query(
			"plan_id = $2 AND assignment_kind = 'on_site_support' AND offer_id = $3 AND status = 'active'",
			planID, *offerID,
		)
	default:
		return ShareAssignmentRow{}, validationError("assignment kind invalid")
	}
	out, err := scanAssignmentRow(row)
	if errors.Is(err, store.ErrNoRows) {
		return ShareAssignmentRow{}, ErrAssignmentNotFound
	}
	return out, err
}

func (s shareAssignmentStore) RevokeCAS(
	ctx context.Context,
	planID, assignmentID string,
	expectedRevision int64,
	revokedBy AssignmentRevokedBy,
	now time.Time,
) (ShareAssignmentRow, error) {
	n, err := s.tx.Update(ctx, "share_assignments",
		"status = $2, revoked_at = $3, revoked_by = $4, revision = revision + 1",
		"plan_id = $5 AND id = $6 AND status = 'active' AND revision = $7",
		string(AssignmentStatusRevoked),
		now.UTC(),
		string(revokedBy),
		planID,
		assignmentID,
		expectedRevision,
	)
	if err != nil {
		return ShareAssignmentRow{}, err
	}
	if n == 0 {
		return ShareAssignmentRow{}, ErrAssignmentStale
	}
	return s.LockByID(ctx, planID, assignmentID)
}

func (s shareAssignmentStore) ExistsActiveReadiness(ctx context.Context, planID, readinessItemID string) (bool, error) {
	return s.tx.Exists(ctx, "share_assignments",
		"plan_id = $2 AND assignment_kind = 'readiness' AND readiness_item_id = $3 AND status = 'active'",
		planID, readinessItemID)
}

func scanAssignmentRow(row interface{ Scan(...any) error }) (ShareAssignmentRow, error) {
	var out ShareAssignmentRow
	var kind string
	var readinessID, offerID, leadRule, revokedBy sql.NullString
	var leadDays sql.NullInt64
	var commitment []byte
	var revokedAt sql.NullTime
	var statusText string
	err := row.Scan(
		&out.ID,
		&out.PlanID,
		&out.TokenGenerationID,
		&kind,
		&readinessID,
		&offerID,
		&out.ContentSnapshot,
		&out.ClaimedByDisplayName,
		&leadDays,
		&leadRule,
		&statusText,
		&commitment,
		&out.Revision,
		&out.ClaimedAt,
		&revokedAt,
		&revokedBy,
	)
	if err != nil {
		return ShareAssignmentRow{}, err
	}
	if len(commitment) != receiptSecretByteLen {
		return ShareAssignmentRow{}, fmt.Errorf("invalid claim_receipt_commitment length %d", len(commitment))
	}
	copy(out.ClaimReceiptCommitment[:], commitment)
	out.AssignmentKind = AssignmentKind(kind)
	out.Status = AssignmentStatus(statusText)
	out.ClaimedAt = out.ClaimedAt.UTC()
	if readinessID.Valid {
		value := readinessID.String
		out.ReadinessItemID = &value
	}
	if offerID.Valid {
		value := offerID.String
		out.OfferID = &value
	}
	if leadDays.Valid {
		value := int(leadDays.Int64)
		out.PreparationLeadDaysSnapshot = &value
	}
	if leadRule.Valid {
		value := leadRule.String
		out.LeadRuleVersion = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time.UTC()
		out.RevokedAt = &value
	}
	if revokedBy.Valid {
		value := AssignmentRevokedBy(revokedBy.String)
		out.RevokedBy = &value
	}
	return out, nil
}

type shareAssignmentSourceEventStore struct{ tx store.TxAccountScope }

func (shareAssignmentSourceEventStore) shareAssignmentSourceEventStoreSeal() {}

func (s shareAssignmentSourceEventStore) Insert(ctx context.Context, row ShareAssignmentSourceEventInsert) error {
	return s.tx.Insert(ctx, "share_assignment_source_event_v1",
		[]string{
			"event_id", "plan_id", "assignment_id", "assignment_revision",
			"account_source_generation", "event_kind", "assignment_kind", "readiness_item_id",
			"preparation_lead_days_snapshot", "lead_rule_version", "content_fingerprint",
			"occurred_at", "source_version",
		},
		row.EventID,
		row.PlanID,
		row.AssignmentID,
		row.AssignmentRevision,
		row.AccountSourceGeneration,
		row.EventKind,
		string(row.AssignmentKind),
		nullableString(row.ReadinessItemID),
		nullableInt(row.PreparationLeadDaysSnapshot),
		nullableString(row.LeadRuleVersion),
		row.ContentFingerprint,
		row.OccurredAt.UTC(),
		1,
	)
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

type shareReplayAdmissionStore struct{ tx store.TxAccountScope }

func (s shareReplayAdmissionStore) RecordForCurrentLedgerClaim(
	ctx context.Context,
	frame CanonicalAnonymousMutationFrameV1,
) error {
	if frame.Operation == "" || frame.FrameHash == "" || frame.IdempotencyKey == "" ||
		frame.TokenGenerationID == "" || len(frame.ExactFrameFingerprint) != 32 {
		return errors.New("anonymous mutation admission frame incomplete")
	}
	var requestHash string
	var expiresAt time.Time
	err := s.tx.QueryRowForUpdate(ctx, "idempotency_records",
		"request_hash, expires_at",
		"operation = $2 AND key = $3",
		frame.Operation, frame.IdempotencyKey,
	).Scan(&requestHash, &expiresAt)
	if errors.Is(err, store.ErrNoRows) {
		return errors.New("anonymous mutation admission missing ledger claim")
	}
	if err != nil {
		return err
	}
	if requestHash != frame.FrameHash {
		return errors.New("anonymous mutation admission fingerprint mismatch")
	}
	keyDigest := IdempotencyKeyDigest(frame.IdempotencyKey)
	var existingFingerprint []byte
	var existingExpires time.Time
	err = s.tx.QueryRowForUpdate(ctx, "share_replay_admissions",
		"exact_frame_fingerprint, expires_at",
		"token_generation_id = $2 AND operation = $3 AND idempotency_key_digest = $4",
		frame.TokenGenerationID, frame.Operation, keyDigest,
	).Scan(&existingFingerprint, &existingExpires)
	now := time.Now().UTC()
	switch {
	case err == nil:
		if existingExpires.After(now) {
			if !bytesEqual(existingFingerprint, frame.ExactFrameFingerprint) {
				return errors.New("anonymous mutation admission invariant violated")
			}
			return nil
		}
		_, err = s.tx.Update(ctx, "share_replay_admissions",
			"exact_frame_fingerprint = $2, admitted_at = $3, expires_at = $4",
			"token_generation_id = $5 AND operation = $6 AND idempotency_key_digest = $7",
			frame.ExactFrameFingerprint, now, expiresAt.UTC(),
			frame.TokenGenerationID, frame.Operation, keyDigest,
		)
		return err
	case errors.Is(err, store.ErrNoRows):
		return s.tx.Insert(ctx, "share_replay_admissions",
			[]string{
				"token_generation_id", "operation", "idempotency_key_digest",
				"exact_frame_fingerprint", "admitted_at", "expires_at",
			},
			frame.TokenGenerationID,
			frame.Operation,
			keyDigest,
			frame.ExactFrameFingerprint,
			now,
			expiresAt.UTC(),
		)
	default:
		return err
	}
}

func (s shareReplayAdmissionStore) HasExactUnexpired(
	ctx context.Context,
	tokenGenerationID string,
	operation string,
	keyDigest []byte,
	fingerprint []byte,
	now time.Time,
) (bool, error) {
	var stored []byte
	var expiresAt time.Time
	err := s.tx.QueryRow(ctx, "share_replay_admissions",
		"exact_frame_fingerprint, expires_at",
		"token_generation_id = $2 AND operation = $3 AND idempotency_key_digest = $4",
		tokenGenerationID, operation, keyDigest,
	).Scan(&stored, &expiresAt)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !expiresAt.After(now.UTC()) {
		return false, nil
	}
	return bytesEqual(stored, fingerprint), nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func (r shareMediaReader) IssueDisplayPermitInShare(
	ctx context.Context,
	req SharedDisplayPermitRequest,
) (planningmedia.ContentPermit, error) {
	if r.media == nil {
		return planningmedia.ContentPermit{}, ErrShareNotFound
	}
	permit, err := r.media.IssueShareDisplayPermit(ctx, r.tx, planningmedia.ShareDisplayPermitRequest{
		PlanID:          req.PlanID,
		BindingID:       req.BindingID,
		AssetID:         req.AssetID,
		ExactGeneration: req.ExactGeneration,
		DisplayChecksum: req.DisplayChecksum,
	})
	if err != nil {
		return planningmedia.ContentPermit{}, mapShareMediaError(err)
	}
	return permit, nil
}

func mapShareMediaError(err error) error {
	switch {
	case errors.Is(err, planningmedia.ErrAssetCorrupt):
		return err
	case errors.Is(err, planningmedia.ErrNotFound),
		errors.Is(err, planningmedia.ErrAssetGCPending),
		errors.Is(err, planningmedia.ErrAssetReferenceStale),
		errors.Is(err, planningmedia.ErrAssetState),
		errors.Is(err, planningmedia.ErrBindingAlreadyReleased),
		errors.Is(err, planningmedia.ErrPlanArchived):
		return ErrShareNotFound
	default:
		return err
	}
}
