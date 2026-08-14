package store

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/securitybudget"
)

type securityDigestKey struct {
	version string
	hmac    []byte
}

// ConsumeSecurityAttemptBudget persists a fixed-window counter for one closed
// policy/action/dimension using candidate guard locking. It never stores
// account_id, raw IP, token, key, body, or receipt material.
func (s *Store) ConsumeSecurityAttemptBudget(
	ctx context.Context,
	policy securitybudget.PolicyVersion,
	action securitybudget.Action,
	dimension securitybudget.Dimension,
	candidates securitybudget.DigestCandidates,
	window time.Duration,
	limit int,
	now time.Time,
) (time.Duration, bool, error) {
	if s == nil || s.pool == nil {
		return 0, false, securitybudget.ErrUnavailable
	}
	if err := securitybudget.ValidateClosed(policy, action, dimension); err != nil {
		return 0, false, err
	}
	now = now.UTC()
	if err := securitybudget.ValidateCandidates(candidates, now); err != nil {
		return 0, false, err
	}
	if window <= 0 || limit < 1 {
		return 0, false, securitybudget.ErrInvalidCandidates
	}

	keys := []securityDigestKey{{
		version: string(candidates.Current.Version),
		hmac:    append([]byte(nil), candidates.Current.HMAC...),
	}}
	if candidates.Previous != nil {
		keys = append(keys, securityDigestKey{
			version: string(candidates.Previous.Version),
			hmac:    append([]byte(nil), candidates.Previous.HMAC...),
		})
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].version != keys[j].version {
			return keys[i].version < keys[j].version
		}
		return bytes.Compare(keys[i].hmac, keys[j].hmac) < 0
	})

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("%w: begin security attempt budget", securitybudget.ErrUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, key := range keys {
		if _, err := tx.Exec(ctx, `
			INSERT INTO security_attempt_budget_guards_v1 (
				policy_version, action, dimension, digest_version, hmac_digest, last_used_at
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (policy_version, action, dimension, digest_version, hmac_digest) DO NOTHING`,
			policy, action, dimension, key.version, key.hmac, now,
		); err != nil {
			return 0, false, fmt.Errorf("%w: insert security attempt guard", securitybudget.ErrUnavailable)
		}
	}
	for _, key := range keys {
		var locked int
		if err := tx.QueryRow(ctx, `
			SELECT 1
			FROM security_attempt_budget_guards_v1
			WHERE policy_version = $1
			  AND action = $2
			  AND dimension = $3
			  AND digest_version = $4
			  AND hmac_digest = $5
			FOR UPDATE`, policy, action, dimension, key.version, key.hmac).Scan(&locked); err != nil {
			return 0, false, fmt.Errorf("%w: lock security attempt guard", securitybudget.ErrUnavailable)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE security_attempt_budget_guards_v1
			SET last_used_at = $6
			WHERE policy_version = $1
			  AND action = $2
			  AND dimension = $3
			  AND digest_version = $4
			  AND hmac_digest = $5`,
			policy, action, dimension, key.version, key.hmac, now,
		); err != nil {
			return 0, false, fmt.Errorf("%w: touch security attempt guard", securitybudget.ErrUnavailable)
		}
	}

	type counterRow struct {
		key         securityDigestKey
		windowStart time.Time
		attempts    int
		active      bool
	}
	rows := make([]counterRow, 0, len(keys))
	for _, key := range keys {
		var windowStart time.Time
		var attempts int
		err := tx.QueryRow(ctx, `
			SELECT window_start, attempts
			FROM security_attempt_budgets_v1
			WHERE policy_version = $1
			  AND action = $2
			  AND dimension = $3
			  AND digest_version = $4
			  AND hmac_digest = $5
			FOR UPDATE`, policy, action, dimension, key.version, key.hmac,
		).Scan(&windowStart, &attempts)
		if err == pgx.ErrNoRows {
			rows = append(rows, counterRow{key: key})
			continue
		}
		if err != nil {
			return 0, false, fmt.Errorf("%w: lock security attempt counter", securitybudget.ErrUnavailable)
		}
		active := now.Before(windowStart.Add(window))
		rows = append(rows, counterRow{
			key: key, windowStart: windowStart, attempts: attempts, active: active,
		})
	}

	var previous *counterRow
	var current *counterRow
	for i := range rows {
		row := &rows[i]
		if candidates.Previous != nil &&
			row.key.version == string(candidates.Previous.Version) &&
			bytes.Equal(row.key.hmac, candidates.Previous.HMAC) {
			previous = row
			continue
		}
		if row.key.version == string(candidates.Current.Version) &&
			bytes.Equal(row.key.hmac, candidates.Current.HMAC) {
			current = row
		}
	}
	if previous != nil && previous.active && current != nil && current.active {
		return 0, false, securitybudget.ErrInvariant
	}

	target := current
	if previous != nil && previous.active {
		target = previous
	}
	if target == nil {
		return 0, false, securitybudget.ErrInvariant
	}

	if !target.active {
		if _, err := tx.Exec(ctx, `
			INSERT INTO security_attempt_budgets_v1 (
				policy_version, action, dimension, digest_version, hmac_digest, window_start, attempts
			) VALUES ($1, $2, $3, $4, $5, $6, 1)
			ON CONFLICT (policy_version, action, dimension, digest_version, hmac_digest)
			DO UPDATE SET window_start = EXCLUDED.window_start, attempts = 1`,
			policy, action, dimension, target.key.version, target.key.hmac, now,
		); err != nil {
			return 0, false, fmt.Errorf("%w: initialize security attempt counter", securitybudget.ErrUnavailable)
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, false, fmt.Errorf("%w: commit security attempt budget", securitybudget.ErrUnavailable)
		}
		return 0, true, nil
	}

	if target.attempts >= limit {
		retryAfter := target.windowStart.Add(window).Sub(now)
		if err := tx.Commit(ctx); err != nil {
			return 0, false, fmt.Errorf("%w: commit security attempt budget", securitybudget.ErrUnavailable)
		}
		return retryAfter, false, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE security_attempt_budgets_v1
		SET attempts = attempts + 1
		WHERE policy_version = $1
		  AND action = $2
		  AND dimension = $3
		  AND digest_version = $4
		  AND hmac_digest = $5`,
		policy, action, dimension, target.key.version, target.key.hmac,
	); err != nil {
		return 0, false, fmt.Errorf("%w: consume security attempt counter", securitybudget.ErrUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, fmt.Errorf("%w: commit security attempt budget", securitybudget.ErrUnavailable)
	}
	return 0, true, nil
}
