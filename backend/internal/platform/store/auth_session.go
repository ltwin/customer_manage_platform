package store

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func (s *Store) CreateRefreshSession(ctx context.Context, accountID string, seed auth.RefreshSeed) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create refresh session: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var active bool
	if err := tx.QueryRow(ctx, `SELECT status = 'active' FROM accounts WHERE id = $1 FOR UPDATE`, accountID).Scan(&active); err != nil {
		return fmt.Errorf("lock refresh account: %w", err)
	}
	if !active {
		return auth.ErrEmailVerificationRequired
	}
	if err := insertRefreshSession(ctx, tx, accountID, seed); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create refresh session: %w", err)
	}
	return nil
}

func (s *Store) RotateRefresh(
	ctx context.Context,
	proof auth.RefreshProof,
	successor auth.RefreshSeed,
	cipher *auth.ReplayCipher,
	now time.Time,
) (auth.RefreshRotation, error) {
	if cipher == nil {
		return auth.RefreshRotation{}, auth.ErrInvalidToken
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.RefreshRotation{}, fmt.Errorf("begin rotate refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var familyID, accountID string
	var storedHash []byte
	var idleExpiresAt, absoluteExpiresAt time.Time
	var usedAt, replayUntil, revokedAt *time.Time
	var successorID *string
	var replayCiphertext []byte
	err = tx.QueryRow(ctx, `
		SELECT g.family_id, f.account_id, g.token_hash, g.idle_expires_at,
		       g.used_at, g.successor_generation_id, g.replay_until, g.replay_ciphertext,
		       f.absolute_expires_at, f.revoked_at
		FROM refresh_session_generations g
		JOIN refresh_session_families f ON f.id = g.family_id
		WHERE g.id = $1
		FOR UPDATE OF g, f`, proof.GenerationID).Scan(
		&familyID, &accountID, &storedHash, &idleExpiresAt,
		&usedAt, &successorID, &replayUntil, &replayCiphertext,
		&absoluteExpiresAt, &revokedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.RefreshRotation{}, auth.ErrInvalidToken
		}
		return auth.RefreshRotation{}, fmt.Errorf("lock refresh generation: %w", err)
	}
	if subtle.ConstantTimeCompare(storedHash, proof.TokenHash) != 1 {
		return auth.RefreshRotation{}, auth.ErrInvalidToken
	}
	if revokedAt != nil || !now.Before(idleExpiresAt) || !now.Before(absoluteExpiresAt) {
		if revokedAt == nil {
			if _, err := tx.Exec(ctx, `UPDATE refresh_session_families SET revoked_at = $2 WHERE id = $1`, familyID, now); err != nil {
				return auth.RefreshRotation{}, fmt.Errorf("revoke expired refresh family: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return auth.RefreshRotation{}, fmt.Errorf("commit expired refresh family: %w", err)
			}
		}
		return auth.RefreshRotation{}, auth.ErrInvalidToken
	}
	if usedAt != nil {
		if successorID != nil && replayUntil != nil && !now.After(*replayUntil) && len(replayCiphertext) > 0 {
			wire, openErr := cipher.Open(proof.GenerationID, familyID, *replayUntil, replayCiphertext)
			if openErr != nil {
				if _, err := tx.Exec(ctx, `UPDATE refresh_session_families SET revoked_at = $2 WHERE id = $1`, familyID, now); err != nil {
					return auth.RefreshRotation{}, fmt.Errorf("revoke tampered replay family: %w", err)
				}
				if err := tx.Commit(ctx); err != nil {
					return auth.RefreshRotation{}, fmt.Errorf("commit tampered replay family: %w", err)
				}
				return auth.RefreshRotation{}, auth.ErrInvalidToken
			}
			var successorIdle time.Time
			if err := tx.QueryRow(ctx, `SELECT idle_expires_at FROM refresh_session_generations WHERE id = $1`, *successorID).Scan(&successorIdle); err != nil {
				return auth.RefreshRotation{}, fmt.Errorf("read replay successor: %w", err)
			}
			if err := tx.Commit(ctx); err != nil {
				return auth.RefreshRotation{}, fmt.Errorf("commit replay successor: %w", err)
			}
			return auth.RefreshRotation{
				AccountID: accountID, FamilyID: familyID, GenerationID: *successorID,
				WireToken: wire, IdleExpiresAt: successorIdle, AbsoluteExpiresAt: absoluteExpiresAt,
			}, nil
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_session_families SET revoked_at = $2 WHERE id = $1`, familyID, now); err != nil {
			return auth.RefreshRotation{}, fmt.Errorf("revoke reused refresh family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return auth.RefreshRotation{}, fmt.Errorf("commit reused refresh family: %w", err)
		}
		return auth.RefreshRotation{}, auth.ErrRefreshReuse
	}

	successor.FamilyID = familyID
	successor.AbsoluteExpiresAt = absoluteExpiresAt
	successor.IdleExpiresAt = minTime(successor.IdleExpiresAt, absoluteExpiresAt)
	replayDeadline := minTime(successor.ReplayUntil, absoluteExpiresAt)
	ciphertext, err := cipher.Seal(proof.GenerationID, familyID, replayDeadline, successor.WireToken)
	if err != nil {
		return auth.RefreshRotation{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE refresh_session_generations SET used_at = $2 WHERE id = $1`, proof.GenerationID, now); err != nil {
		return auth.RefreshRotation{}, fmt.Errorf("mark refresh generation used: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_session_generations (id, family_id, token_hash, idle_expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`, successor.GenerationID, familyID,
		successor.TokenHash, successor.IdleExpiresAt, successor.CreatedAt); err != nil {
		return auth.RefreshRotation{}, fmt.Errorf("insert refresh successor: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE refresh_session_generations
		SET successor_generation_id = $2, replay_until = $3, replay_ciphertext = $4
		WHERE id = $1`, proof.GenerationID, successor.GenerationID, replayDeadline, ciphertext); err != nil {
		return auth.RefreshRotation{}, fmt.Errorf("store refresh replay envelope: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.RefreshRotation{}, fmt.Errorf("commit rotate refresh: %w", err)
	}
	return auth.RefreshRotation{
		AccountID: accountID, FamilyID: familyID, GenerationID: successor.GenerationID,
		WireToken: successor.WireToken, IdleExpiresAt: successor.IdleExpiresAt,
		AbsoluteExpiresAt: absoluteExpiresAt,
	}, nil
}

func (s *Store) RevokeRefresh(ctx context.Context, proof auth.RefreshProof, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin revoke refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var familyID string
	var storedHash []byte
	err = tx.QueryRow(ctx, `SELECT family_id, token_hash FROM refresh_session_generations WHERE id = $1 FOR UPDATE`, proof.GenerationID).
		Scan(&familyID, &storedHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return fmt.Errorf("find refresh for logout: %w", err)
	}
	if subtle.ConstantTimeCompare(storedHash, proof.TokenHash) != 1 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE refresh_session_families SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, familyID, now); err != nil {
		return fmt.Errorf("revoke refresh family: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit revoke refresh family: %w", err)
	}
	return nil
}

func (s *Store) SweepExpiredReplayCiphertexts(ctx context.Context, now time.Time, batch int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		WITH expired AS (
			SELECT id FROM refresh_session_generations
			WHERE replay_ciphertext IS NOT NULL AND replay_until < $1
			ORDER BY replay_until, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		UPDATE refresh_session_generations g
		SET replay_ciphertext = NULL, replay_until = NULL
		FROM expired WHERE g.id = expired.id`, now, batch)
	if err != nil {
		return 0, fmt.Errorf("sweep refresh replay ciphertexts: %w", err)
	}
	return tag.RowsAffected(), nil
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
