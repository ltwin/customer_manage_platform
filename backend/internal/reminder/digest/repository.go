package digest

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const deliveryColumns = "id,source,source_key,message_kind,target_local_date,timezone_at_enqueue,status,attempts,next_attempt_at,sent_at,last_error_code,claim_id,lease_until"

type PostgresDeliveryRepository struct {
	// afterClaim is a test-only fault seam for a successful commit whose result is unknown to the caller.
	afterClaim func() error
}

func NewPostgresDeliveryRepository() PostgresDeliveryRepository { return PostgresDeliveryRepository{} }

func (r PostgresDeliveryRepository) ClaimDueAttempt(
	ctx context.Context,
	scope store.AccountScope,
	now time.Time,
	leaseUntil time.Time,
) (AttemptClaim, ClaimDueOutcome, error) {
	var claimed AttemptClaim
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		rows, err := tx.QueryPage(ctx, "telegram_deliveries", deliveryColumns,
			"status = $2 AND next_attempt_at <= $3 AND (claim_id IS NULL OR lease_until <= $3)",
			[]store.OrderBy{{Column: "next_attempt_at"}, {Column: "id"}}, 1, 0,
			string(DeliveryStatusPending), now)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return rows.Err()
		}
		delivery, err := scanDelivery(rows)
		if err != nil {
			return err
		}
		rows.Close()
		locked, err := scanDelivery(tx.QueryRowForUpdate(ctx, "telegram_deliveries", deliveryColumns, "id = $2", delivery.ID))
		if err != nil {
			return err
		}
		if locked.Status != DeliveryStatusPending || locked.NextAttemptAt.After(now) || (locked.LeaseUntil != nil && locked.LeaseUntil.After(now)) {
			return nil
		}
		claimID, err := randomClaimID()
		if err != nil {
			return err
		}
		n, err := tx.Update(ctx, "telegram_deliveries",
			"claim_id = $2, lease_until = $3, updated_at = $4",
			"id = $5 AND status = $6 AND (claim_id IS NULL OR lease_until <= $7)",
			claimID, leaseUntil, now, locked.ID, string(DeliveryStatusPending), now)
		if err != nil {
			return err
		}
		if n != 1 {
			return nil
		}
		locked.ClaimID = claimID
		locked.LeaseUntil = &leaseUntil
		claimed = AttemptClaim{Delivery: locked, ClaimID: claimID, LeaseUntil: leaseUntil}
		return nil
	})
	if err == nil && r.afterClaim != nil {
		err = r.afterClaim()
	}
	if err != nil {
		if claimed.ClaimID != "" {
			recovered, recoverErr := scanDelivery(scope.QueryRow(
				ctx,
				"telegram_deliveries",
				deliveryColumns,
				"id = $2",
				claimed.Delivery.ID,
			))
			if recoverErr == nil && recovered.Status == DeliveryStatusPending &&
				recovered.ClaimID == claimed.ClaimID && recovered.LeaseUntil != nil {
				claimed.Delivery = recovered
				claimed.LeaseUntil = *recovered.LeaseUntil
				return claimed, ClaimDueClaimed, nil
			}
		}
		return AttemptClaim{}, ClaimDueNone, err
	}
	if claimed.ClaimID == "" {
		return AttemptClaim{}, ClaimDueNone, nil
	}
	return claimed, ClaimDueClaimed, nil
}

func (PostgresDeliveryRepository) ReloadClaim(
	ctx context.Context,
	scope store.AccountScope,
	deliveryID string,
	_ string,
) (Delivery, error) {
	return scanDelivery(scope.QueryRow(ctx, "telegram_deliveries", deliveryColumns, "id = $2", deliveryID))
}

func (PostgresDeliveryRepository) AssertCallableClaim(
	ctx context.Context,
	scope store.AccountScope,
	claim AttemptClaim,
	now time.Time,
	minLeaseUntil time.Time,
) (bool, error) {
	return scope.Exists(ctx, "telegram_deliveries",
		"id = $2 AND status = $3 AND claim_id = $4 AND lease_until > $5 AND lease_until >= $6",
		claim.Delivery.ID, string(DeliveryStatusPending), claim.ClaimID, now, minLeaseUntil)
}

func (PostgresDeliveryRepository) FinalizeAttempt(
	ctx context.Context,
	scope store.AccountScope,
	claim AttemptClaim,
	result AttemptResult,
	now time.Time,
) (FinalizeOutcome, error) {
	var (
		n   int64
		err error
	)
	if result.Sent {
		n, err = scope.Update(ctx, "telegram_deliveries",
			"status = $2, sent_at = $3, last_error_code = NULL, claim_id = NULL, lease_until = NULL, updated_at = $3",
			"id = $4 AND status = $5 AND claim_id = $6 AND lease_until > $3",
			string(DeliveryStatusSent), now, claim.Delivery.ID, string(DeliveryStatusPending), claim.ClaimID)
	} else {
		if result.ErrorKind == "" {
			return FinalizeStale, errors.New("attempt result requires sent or error kind")
		}
		attempts := claim.Delivery.Attempts + 1
		status := DeliveryStatusPending
		next := now
		if attempts >= 4 {
			status = DeliveryStatusFailed
		} else {
			delays := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute}
			delay := delays[attempts-1]
			if result.RetryAfter > delay {
				delay = result.RetryAfter
			}
			next = now.Add(delay)
		}
		n, err = scope.Update(ctx, "telegram_deliveries",
			"status = $2, attempts = $3, next_attempt_at = $4, last_error_code = $5, claim_id = NULL, lease_until = NULL, updated_at = $6",
			"id = $7 AND status = $8 AND claim_id = $9 AND lease_until > $6",
			string(status), attempts, next, string(result.ErrorKind), now,
			claim.Delivery.ID, string(DeliveryStatusPending), claim.ClaimID)
	}
	if err != nil {
		return FinalizeStale, err
	}
	if n != 1 {
		return FinalizeStale, nil
	}
	return FinalizeApplied, nil
}

func (PostgresDeliveryRepository) ReleaseClaim(
	ctx context.Context,
	scope store.AccountScope,
	claim AttemptClaim,
	release ClaimRelease,
	now time.Time,
) (FinalizeOutcome, error) {
	n, err := scope.Update(ctx, "telegram_deliveries",
		"next_attempt_at = $2, last_error_code = $3, claim_id = NULL, lease_until = NULL, updated_at = $4",
		"id = $5 AND status = $6 AND claim_id = $7",
		release.NextAttemptAt, release.ErrorCode, now,
		claim.Delivery.ID, string(DeliveryStatusPending), claim.ClaimID)
	if err != nil {
		return FinalizeStale, err
	}
	if n != 1 {
		return FinalizeStale, nil
	}
	return FinalizeApplied, nil
}

type rowScanner interface{ Scan(...any) error }

func scanDelivery(row rowScanner) (Delivery, error) {
	var (
		delivery  Delivery
		localDate sql.NullTime
		timezone  sql.NullString
		sentAt    sql.NullTime
		lastError sql.NullString
		claimID   sql.NullString
		lease     sql.NullTime
	)
	err := row.Scan(
		&delivery.ID, &delivery.Source, &delivery.SourceKey, &delivery.MessageKind,
		&localDate, &timezone, &delivery.Status, &delivery.Attempts, &delivery.NextAttemptAt,
		&sentAt, &lastError, &claimID, &lease,
	)
	if err != nil {
		return Delivery{}, err
	}
	if localDate.Valid {
		delivery.TargetLocalDate = &localDate.Time
	}
	if timezone.Valid {
		delivery.TimezoneAtEnqueue = timezone.String
	}
	if sentAt.Valid {
		delivery.SentAt = &sentAt.Time
	}
	if lastError.Valid {
		delivery.LastErrorCode = lastError.String
	}
	if claimID.Valid {
		delivery.ClaimID = claimID.String
	}
	if lease.Valid {
		delivery.LeaseUntil = &lease.Time
	}
	return delivery, nil
}

func randomClaimID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate delivery claim id: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
