package digest

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

const (
	maxIntentUnicodeScalars = 4096
	maxIntentOctets         = 16384
	callTechnicalLease      = 5 * time.Second
)

var (
	errIntentGuardFailed      = errors.New("digest_intent_guard_failed")
	errCallBudgetExhausted    = errors.New("digest_call_budget_exhausted")
	errRecipientMissingIntent = errors.New("digest_recipient_missing")
)

// ErrCallBudgetExhausted is returned when StartAt / monotonic remainder is exhausted.
func ErrCallBudgetExhausted() error { return errCallBudgetExhausted }

// ImmutableDigestIntent is the active (or newly created) immutable intent revision.
type ImmutableDigestIntent struct {
	DeliveryID                    string
	IntentRevision                int64
	ProjectionGeneration          int64
	PayloadFingerprint            string
	RecipientChatIDSnapshot       string
	RecipientBindingRevision      int64
	RecipientFingerprint          string
	PlanningMembershipFingerprint string
	EarliestValidUntil            *time.Time
	PayloadText                   string
}

// CallStartPermit is the non-persisted local budget returned by BeginCurrentCall.
type CallStartPermit struct {
	AttemptID            string
	IntentRevision       int64
	StartDeadline        time.Time
	MonotonicStartBudget time.Duration
	PayloadText          string
	RecipientChatID      string
}

// DigestIntentService owns PrepareCurrentIntentInScope and BeginCurrentCall.
type DigestIntentService struct {
	Messages  MessageBuilder
	Freshness *reminderdomain.AssignmentReminderFreshness
	monoNow   func() time.Time
}

func NewDigestIntentService(messages MessageBuilder, freshness *reminderdomain.AssignmentReminderFreshness) *DigestIntentService {
	return &DigestIntentService{Messages: messages, Freshness: freshness, monoNow: time.Now}
}

func (s *DigestIntentService) WithMonoClock(now func() time.Time) *DigestIntentService {
	if now != nil {
		s.monoNow = now
	}
	return s
}

// PrepareCurrentIntentInScope builds/reuses the active intent under the shared lock order.
// Caller must already hold account fence; this locks Settings → Delivery claim → groups.
func (s *DigestIntentService) PrepareCurrentIntentInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	locked planningreminder.LockedFenceTx,
	claim AttemptClaim,
) (ImmutableDigestIntent, error) {
	if locked == nil {
		return ImmutableDigestIntent{}, planningreminder.ErrFenceNotLocked
	}
	chatID, bindingRev, timezone, ok, err := lockSettingsRecipient(ctx, tx)
	if err != nil {
		return ImmutableDigestIntent{}, err
	}
	if !ok {
		return ImmutableDigestIntent{}, errRecipientMissingIntent
	}
	delivery, err := reloadDeliveryForUpdate(ctx, tx, claim)
	if err != nil {
		return ImmutableDigestIntent{}, err
	}
	if delivery.Status != DeliveryStatusPending || delivery.ClaimID != claim.ClaimID {
		return ImmutableDigestIntent{}, errIntentGuardFailed
	}

	text, membershipFP, earliest, projectionGen, err := s.buildMixedPayload(ctx, tx, delivery, timezone)
	if err != nil {
		return ImmutableDigestIntent{}, err
	}
	if err := validateIntentPayload(text); err != nil {
		return ImmutableDigestIntent{}, err
	}
	payloadFP := fingerprintText(text)
	recipientFP := fingerprintText(fmt.Sprintf("%d\n%s", bindingRev, chatID))

	// Final guarded reuse / insert using clock_timestamp().
	var active sql.NullInt64
	_ = tx.QueryRow(ctx, "telegram_deliveries", "active_intent_revision", "id = $2", delivery.ID).Scan(&active)
	if active.Valid {
		var existing ImmutableDigestIntent
		var earliestSQL sql.NullTime
		var payload sql.NullString
		err := tx.QueryRow(ctx, "plan_assignment_reminder_digest_intents",
			"intent_revision, projection_generation, payload_fingerprint, recipient_chat_id_snapshot, recipient_binding_revision, recipient_fingerprint, planning_membership_fingerprint, earliest_valid_until, payload_text",
			"delivery_id = $2 AND intent_revision = $3", delivery.ID, active.Int64,
		).Scan(
			&existing.IntentRevision, &existing.ProjectionGeneration, &existing.PayloadFingerprint,
			&existing.RecipientChatIDSnapshot, &existing.RecipientBindingRevision, &existing.RecipientFingerprint,
			&existing.PlanningMembershipFingerprint, &earliestSQL, &payload,
		)
		if err == nil && payload.Valid &&
			existing.PayloadFingerprint == payloadFP &&
			existing.RecipientFingerprint == recipientFP &&
			existing.PlanningMembershipFingerprint == membershipFP &&
			existing.RecipientBindingRevision == bindingRev &&
			existing.RecipientChatIDSnapshot == chatID {
			existing.DeliveryID = delivery.ID
			existing.PayloadText = payload.String
			if earliestSQL.Valid {
				t := earliestSQL.Time.UTC()
				existing.EarliestValidUntil = &t
			}
			return existing, nil
		}
	}

	nextRev := int64(1)
	if active.Valid {
		nextRev = active.Int64 + 1
	}
	var earliestArg any
	if earliest != nil {
		earliestArg = earliest.UTC()
	}
	// Guarded insert: claim + binding + (optional) earliest still valid under clock_timestamp().
	okInsert, err := tx.Exists(ctx, "telegram_deliveries",
		"id = $2 AND status = $3 AND claim_id = $4 AND lease_until > clock_timestamp()",
		delivery.ID, string(DeliveryStatusPending), claim.ClaimID)
	if err != nil {
		return ImmutableDigestIntent{}, err
	}
	if !okInsert {
		return ImmutableDigestIntent{}, errIntentGuardFailed
	}
	bindOK, err := tx.Exists(ctx, "settings",
		"telegram_chat_id = $2 AND telegram_binding_revision = $3", chatID, bindingRev)
	if err != nil {
		return ImmutableDigestIntent{}, err
	}
	if !bindOK {
		return ImmutableDigestIntent{}, errIntentGuardFailed
	}
	if earliest != nil {
		stillFresh, err := tx.Exists(ctx, "settings",
			"telegram_binding_revision = $2 AND clock_timestamp() < $3", bindingRev, earliest.UTC())
		if err != nil {
			return ImmutableDigestIntent{}, err
		}
		if !stillFresh {
			return ImmutableDigestIntent{}, errIntentGuardFailed
		}
	}
	if err := tx.Insert(ctx, "plan_assignment_reminder_digest_intents", []string{
		"delivery_id", "intent_revision", "projection_generation", "payload_fingerprint",
		"recipient_chat_id_snapshot", "recipient_binding_revision", "recipient_fingerprint",
		"planning_membership_fingerprint", "earliest_valid_until", "payload_text",
	}, delivery.ID, nextRev, projectionGen, payloadFP, chatID, bindingRev, recipientFP, membershipFP, earliestArg, text); err != nil {
		return ImmutableDigestIntent{}, err
	}
	if _, err := tx.Update(ctx, "telegram_deliveries",
		"active_intent_revision = $2, updated_at = clock_timestamp()",
		"id = $3 AND status = $4 AND claim_id = $5",
		nextRev, delivery.ID, string(DeliveryStatusPending), claim.ClaimID); err != nil {
		return ImmutableDigestIntent{}, err
	}
	out := ImmutableDigestIntent{
		DeliveryID:                    delivery.ID,
		IntentRevision:                nextRev,
		ProjectionGeneration:          projectionGen,
		PayloadFingerprint:            payloadFP,
		RecipientChatIDSnapshot:       chatID,
		RecipientBindingRevision:      bindingRev,
		RecipientFingerprint:          recipientFP,
		PlanningMembershipFingerprint: membershipFP,
		PayloadText:                   text,
		EarliestValidUntil:            earliest,
	}
	return out, nil
}

// BeginCurrentCall is the sole physical Telegram call authorization entry.
func (s *DigestIntentService) BeginCurrentCall(ctx context.Context, scope store.AccountScope, claim AttemptClaim) (CallStartPermit, error) {
	methodStarted := s.monoNow()
	var (
		permit      CallStartPermit
		beforeFinal time.Time
		authResult  store.DigestCallPermitResult
		intent      ImmutableDigestIntent
	)
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		target, err := tx.CaptureFenceTargetGeneration(ctx)
		if err != nil {
			return err
		}
		if s.Freshness != nil {
			// Ensure is typically done outside; re-check watermark here fail-closed.
			var applied int64
			if err := tx.QueryRow(ctx, "planning_reminder_account_generations",
				"applied_generation", "TRUE").Scan(&applied); err != nil {
				return err
			}
			unresolved, err := reminderdomain.HasUnresolvedQuarantineInScope(ctx, tx)
			if err != nil {
				return err
			}
			if unresolved || applied < target {
				return reminderdomain.ErrFreshnessNotCurrent
			}
		}
		intent, err = s.PrepareCurrentIntentInScope(ctx, tx, locked, claim)
		if err != nil {
			return err
		}
		beforeFinal = s.monoNow()
		insert := store.DigestCallPermitInsert{
			DeliveryID:               claim.Delivery.ID,
			AttemptID:                "att_" + uuid.NewString(),
			IntentRevision:           intent.IntentRevision,
			RecipientBindingRevision: intent.RecipientBindingRevision,
			ClaimID:                  claim.ClaimID,
			RecipientChatID:          intent.RecipientChatIDSnapshot,
		}
		if intent.EarliestValidUntil != nil {
			insert.HasEarliestValidUntil = true
			insert.EarliestValidUntil = *intent.EarliestValidUntil
		}
		authResult, err = tx.BeginDigestCallingPermitInScope(ctx, insert)
		return err
	})
	methodReturned := s.monoNow()
	if err != nil {
		return CallStartPermit{}, err
	}
	technicalRemaining := callTechnicalLease - methodReturned.Sub(methodStarted)
	businessRemaining := time.Duration(1<<63 - 1) // +infinity for legacy-only
	if intent.EarliestValidUntil != nil {
		businessRemaining = authResult.BusinessRemainingAtAuthorization - methodReturned.Sub(beforeFinal)
	}
	budget := technicalRemaining
	if businessRemaining < budget {
		budget = businessRemaining
	}
	if budget < 0 {
		budget = 0
	}
	if budget == 0 {
		return CallStartPermit{}, errCallBudgetExhausted
	}
	permit = CallStartPermit{
		AttemptID:            authResult.AttemptID,
		IntentRevision:       authResult.IntentRevision,
		StartDeadline:        authResult.StartDeadline,
		MonotonicStartBudget: budget,
		PayloadText:          intent.PayloadText,
		RecipientChatID:      intent.RecipientChatIDSnapshot,
	}
	return permit, nil
}

func (s *DigestIntentService) buildMixedPayload(
	ctx context.Context,
	tx store.TxAccountScope,
	delivery Delivery,
	currentTimezone string,
) (text string, membershipFP string, earliest *time.Time, projectionGen int64, err error) {
	_ = tx.QueryRow(ctx, "planning_reminder_account_generations", "applied_generation", "TRUE").Scan(&projectionGen)
	base, err := s.Messages.Build(ctx, tx.BoundAccountScope(), delivery)
	if err != nil {
		return "", "", nil, 0, err
	}
	if delivery.MessageKind != MessageKindDigest || delivery.TargetLocalDate == nil {
		return base, "legacy-only", nil, projectionGen, nil
	}
	window, err := WindowFor(LocalTarget{LocalDate: *delivery.TargetLocalDate, Timezone: delivery.TimezoneAtEnqueue})
	if err != nil {
		return "", "", nil, 0, err
	}
	planningItems, earliestValid, err := loadPlanningDigestItems(ctx, tx, currentTimezone, window)
	if err != nil {
		return "", "", nil, 0, err
	}
	if len(planningItems) == 0 {
		return base, "legacy-only", nil, projectionGen, nil
	}
	lines := []string{base, "", fmt.Sprintf("认领项核对（%d）", len(planningItems))}
	memberParts := make([]string, 0, len(planningItems))
	for i, item := range planningItems {
		if i < 3 {
			lines = append(lines, fmt.Sprintf("- %s", item.Content))
		}
		memberParts = append(memberParts, item.ReminderID+"|"+item.GroupID+"|"+item.ValidUntil.UTC().Format(time.RFC3339Nano))
	}
	if overflow := len(planningItems) - 3; overflow > 0 {
		lines = append(lines, fmt.Sprintf("- 等%d项", overflow))
	}
	text = truncateIntent(joinLines(lines))
	membershipFP = fingerprintText(joinLines(memberParts))
	earliest = earliestValid
	return text, membershipFP, earliest, projectionGen, nil
}

type planningDigestItem struct {
	ReminderID string
	GroupID    string
	Content    string
	ValidUntil time.Time
	DueDate    time.Time
}

func loadPlanningDigestItems(
	ctx context.Context,
	tx store.TxAccountScope,
	currentTimezone string,
	window Window,
) ([]planningDigestItem, *time.Time, error) {
	loc, err := time.LoadLocation(currentTimezone)
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.Query(ctx, "plan_assignment_reminder_groups",
		"group_id, reminder_id, due_date, valid_until",
		"state = $2", reminderdomain.GroupStateCurrent)
	if err != nil {
		return nil, nil, err
	}
	type pending struct {
		GroupID    string
		ReminderID string
		DueDate    time.Time
		ValidUntil time.Time
	}
	pendingRows := make([]pending, 0)
	for rows.Next() {
		var row pending
		var due time.Time
		if err := rows.Scan(&row.GroupID, &row.ReminderID, &due, &row.ValidUntil); err != nil {
			rows.Close()
			return nil, nil, err
		}
		row.ValidUntil = row.ValidUntil.UTC()
		row.DueDate = time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
		if row.DueDate.After(window.LocalDate) {
			continue
		}
		pendingRows = append(pendingRows, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	items := make([]planningDigestItem, 0, len(pendingRows))
	var earliest *time.Time
	for _, row := range pendingRows {
		_ = loc
		var status string
		var content string
		if err := tx.QueryRow(ctx, "reminders", "status, content",
			"id = $2 AND type = $3 AND status = $4",
			row.ReminderID, reminderdomain.TypePlanAssignmentChecklist, reminderdomain.StatusPending,
		).Scan(&status, &content); err != nil {
			if errors.Is(err, store.ErrNoRows) {
				continue
			}
			return nil, nil, err
		}
		item := planningDigestItem{
			ReminderID: row.ReminderID,
			GroupID:    row.GroupID,
			Content:    content,
			ValidUntil: row.ValidUntil,
			DueDate:    row.DueDate,
		}
		items = append(items, item)
		if earliest == nil || item.ValidUntil.Before(*earliest) {
			t := item.ValidUntil
			earliest = &t
		}
	}
	return items, earliest, nil
}

func lockSettingsRecipient(ctx context.Context, tx store.TxAccountScope) (chatID string, bindingRev int64, timezone string, ok bool, err error) {
	var chat sql.NullString
	var rev sql.NullInt64
	var tz sql.NullString
	err = tx.QueryRowForUpdate(ctx, "settings",
		"telegram_chat_id, telegram_binding_revision, timezone", "TRUE").
		Scan(&chat, &rev, &tz)
	if errors.Is(err, store.ErrNoRows) {
		return "", 0, "", false, nil
	}
	if err != nil {
		return "", 0, "", false, err
	}
	if !chat.Valid || chat.String == "" || !rev.Valid {
		return "", 0, "", false, nil
	}
	if tz.Valid {
		timezone = tz.String
	}
	return chat.String, rev.Int64, timezone, true, nil
}

func reloadDeliveryForUpdate(ctx context.Context, tx store.TxAccountScope, claim AttemptClaim) (Delivery, error) {
	row := tx.QueryRowForUpdate(ctx, "telegram_deliveries",
		"id,source,source_key,message_kind,target_local_date,timezone_at_enqueue,status,attempts,next_attempt_at,sent_at,last_error_code,claim_id,lease_until,active_intent_revision",
		"id = $2", claim.Delivery.ID)
	return scanDeliveryExtended(row)
}

func scanDeliveryExtended(row interface{ Scan(...any) error }) (Delivery, error) {
	var (
		d          Delivery
		source     string
		kind       string
		status     string
		targetDate sql.NullTime
		tz         sql.NullString
		sentAt     sql.NullTime
		lastErr    sql.NullString
		claimID    sql.NullString
		leaseUntil sql.NullTime
		active     sql.NullInt64
	)
	if err := row.Scan(
		&d.ID, &source, &d.SourceKey, &kind, &targetDate, &tz, &status, &d.Attempts,
		&d.NextAttemptAt, &sentAt, &lastErr, &claimID, &leaseUntil, &active,
	); err != nil {
		return Delivery{}, err
	}
	d.Source = DeliverySource(source)
	d.MessageKind = MessageKind(kind)
	d.Status = DeliveryStatus(status)
	if targetDate.Valid {
		t := time.Date(targetDate.Time.Year(), targetDate.Time.Month(), targetDate.Time.Day(), 0, 0, 0, 0, time.UTC)
		d.TargetLocalDate = &t
	}
	if tz.Valid {
		d.TimezoneAtEnqueue = tz.String
	}
	if sentAt.Valid {
		t := sentAt.Time.UTC()
		d.SentAt = &t
	}
	if lastErr.Valid {
		d.LastErrorCode = lastErr.String
	}
	if claimID.Valid {
		d.ClaimID = claimID.String
	}
	if leaseUntil.Valid {
		t := leaseUntil.Time.UTC()
		d.LeaseUntil = &t
	}
	_ = active
	return d, nil
}

func validateIntentPayload(text string) error {
	if utf8.RuneCountInString(text) > maxIntentUnicodeScalars {
		return errors.New("intent payload exceeds 4096 unicode scalars")
	}
	if len(text) > maxIntentOctets {
		return errors.New("intent payload exceeds 16384 octets")
	}
	return nil
}

func fingerprintText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		if i > 0 {
			out += "\n"
		}
		out += line
	}
	return out
}

func truncateIntent(text string) string {
	runes := []rune(text)
	if len(runes) <= maxIntentUnicodeScalars {
		return text
	}
	return string(runes[:maxIntentUnicodeScalars])
}
