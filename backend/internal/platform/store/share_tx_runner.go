package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ShareCommitmentRow is the unscoped selector lookup result used by planshare resolve.
type ShareCommitmentRow struct {
	Selector    string
	Commitment  []byte
	Fingerprint string
}

// LookupShareCommitmentBySelector is the unscoped selector narrow-read used by
// anonymous token resolve. It never constructs AccountScope.
func (s *Store) LookupShareCommitmentBySelector(
	ctx context.Context,
	selector string,
) (ShareCommitmentRow, bool, error) {
	if s == nil || s.pool == nil {
		return ShareCommitmentRow{}, false, errors.New("store is not open")
	}
	var row ShareCommitmentRow
	err := s.pool.QueryRow(ctx, `
		SELECT selector, secret_commitment, fingerprint
		FROM share_generations
		WHERE selector = $1`, selector).Scan(&row.Selector, &row.Commitment, &row.Fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return ShareCommitmentRow{}, false, nil
	}
	if err != nil {
		return ShareCommitmentRow{}, false, fmt.Errorf("lookup share commitment: %w", err)
	}
	return row, true, nil
}

// RunShareTransaction opens a repeatable-read account-filtered transaction for
// a validated share fingerprint. Callers must not expose AccountScope to HTTP.
func (s *Store) RunShareTransaction(
	ctx context.Context,
	fingerprint string,
	fn func(TxAccountScope) error,
) error {
	if s == nil || s.pool == nil {
		return errors.New("store is not open")
	}
	if fingerprint == "" {
		return errors.New("share fingerprint required")
	}
	if fn == nil {
		return errors.New("share transaction callback required")
	}
	var accountID string
	err := s.pool.QueryRow(ctx, `
		SELECT account_id
		FROM share_generations
		WHERE fingerprint = $1`, fingerprint).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoRows
	}
	if err != nil {
		return fmt.Errorf("resolve share capability account: %w", err)
	}
	if accountID == "" {
		return ErrNoRows
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadWrite,
	})
	if err != nil {
		return fmt.Errorf("begin share tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()
	scoped := AccountScope{pool: s.pool, runner: tx, accountID: accountID}
	if err := fn(TxAccountScope{scope: scoped}); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("share tx rollback after %w: %w", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit share tx: %w", err)
	}
	return nil
}
