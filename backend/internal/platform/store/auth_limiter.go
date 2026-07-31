package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

const (
	attemptDimensionSubject = "subject"
	attemptDimensionSource  = "source"
)

type authAttemptBudget struct {
	subjectLimit int
	sourceLimit  int
	window       time.Duration
}

func (s *Store) Consume(
	ctx context.Context,
	action auth.AuthAction,
	subjectDigest, sourceDigest string,
	now time.Time,
) (time.Duration, bool, error) {
	budget, err := authAttemptBudgetFor(action)
	if err != nil {
		return 0, false, err
	}
	if !validLimiterDigest(subjectDigest) || !validLimiterDigest(sourceDigest) {
		return 0, false, fmt.Errorf("invalid limiter digest")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("begin auth attempt budget: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now = now.UTC()
	subjectRetry, subjectAllowed, err := consumeAuthAttemptDimension(
		ctx, tx, action, attemptDimensionSubject, subjectDigest, budget.subjectLimit, budget.window, now,
	)
	if err != nil {
		return 0, false, err
	}
	sourceRetry, sourceAllowed, err := consumeAuthAttemptDimension(
		ctx, tx, action, attemptDimensionSource, sourceDigest, budget.sourceLimit, budget.window, now,
	)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, fmt.Errorf("commit auth attempt budget: %w", err)
	}

	retryAfter := subjectRetry
	if sourceRetry > retryAfter {
		retryAfter = sourceRetry
	}
	return retryAfter, subjectAllowed && sourceAllowed, nil
}

func (s *Store) ResetSubject(ctx context.Context, action auth.AuthAction, subjectDigest string) error {
	if _, err := authAttemptBudgetFor(action); err != nil {
		return err
	}
	if !validLimiterDigest(subjectDigest) {
		return fmt.Errorf("invalid limiter digest")
	}
	if _, err := s.pool.Exec(ctx, `
		DELETE FROM auth_attempt_budgets
		WHERE action = $1 AND dimension = 'subject' AND digest = $2`, action, subjectDigest); err != nil {
		return fmt.Errorf("reset auth attempt subject: %w", err)
	}
	return nil
}

func authAttemptBudgetFor(action auth.AuthAction) (authAttemptBudget, error) {
	switch action {
	case auth.AuthActionLogin, auth.AuthActionChangePassword:
		return authAttemptBudget{subjectLimit: 5, sourceLimit: 30, window: 15 * time.Minute}, nil
	case auth.AuthActionRegister, auth.AuthActionResendVerification, auth.AuthActionForgotPassword:
		return authAttemptBudget{subjectLimit: 3, sourceLimit: 20, window: time.Hour}, nil
	case auth.AuthActionVerifyToken, auth.AuthActionResetToken:
		return authAttemptBudget{subjectLimit: 10, sourceLimit: 30, window: 15 * time.Minute}, nil
	default:
		return authAttemptBudget{}, fmt.Errorf("unsupported auth action")
	}
}

func consumeAuthAttemptDimension(
	ctx context.Context,
	tx pgx.Tx,
	action auth.AuthAction,
	dimension, digest string,
	limit int,
	window time.Duration,
	now time.Time,
) (time.Duration, bool, error) {
	tag, err := tx.Exec(ctx, `
		INSERT INTO auth_attempt_budgets (action, dimension, digest, window_start, attempts)
		VALUES ($1, $2, $3, $4, 1)
		ON CONFLICT (action, dimension, digest) DO NOTHING`, action, dimension, digest, now)
	if err != nil {
		return 0, false, fmt.Errorf("initialize auth attempt %s budget: %w", dimension, err)
	}
	if tag.RowsAffected() == 1 {
		return 0, true, nil
	}

	var windowStart time.Time
	var attempts int
	if err := tx.QueryRow(ctx, `
		SELECT window_start, attempts
		FROM auth_attempt_budgets
		WHERE action = $1 AND dimension = $2 AND digest = $3
		FOR UPDATE`, action, dimension, digest).Scan(&windowStart, &attempts); err != nil {
		return 0, false, fmt.Errorf("lock auth attempt %s budget: %w", dimension, err)
	}
	windowEnd := windowStart.Add(window)
	if !now.Before(windowEnd) {
		if _, err := tx.Exec(ctx, `
			UPDATE auth_attempt_budgets
			SET window_start = $4, attempts = 1
			WHERE action = $1 AND dimension = $2 AND digest = $3`, action, dimension, digest, now); err != nil {
			return 0, false, fmt.Errorf("reset auth attempt %s window: %w", dimension, err)
		}
		return 0, true, nil
	}
	if attempts >= limit {
		return windowEnd.Sub(now), false, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE auth_attempt_budgets
		SET attempts = attempts + 1
		WHERE action = $1 AND dimension = $2 AND digest = $3`, action, dimension, digest); err != nil {
		return 0, false, fmt.Errorf("consume auth attempt %s budget: %w", dimension, err)
	}
	return 0, true, nil
}

func validLimiterDigest(value string) bool {
	if len(value) != 46 || !strings.HasPrefix(value, "v1:") {
		return false
	}
	for _, char := range value[3:] {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}
