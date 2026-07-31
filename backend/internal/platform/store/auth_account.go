package store

import (
	"context"
	"crypto/subtle"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

const accountAdmissionLockSQL = `SELECT pg_advisory_xact_lock(hashtext('crm-auth/account-admission/v1'))`

func (s *Store) RegisterAccount(
	ctx context.Context,
	mode auth.RegistrationAdmissionMode,
	record auth.RegistrationRecord,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin register account: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, accountAdmissionLockSQL); err != nil {
		return false, fmt.Errorf("lock account admission: %w", err)
	}
	switch mode {
	case auth.RegistrationBootstrapFirstAccount:
		var count int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM accounts`).Scan(&count); err != nil {
			return false, fmt.Errorf("count bootstrap accounts: %w", err)
		}
		if count != 0 {
			return false, auth.ErrBootstrapNotEmpty
		}
	case auth.RegistrationPublic:
	default:
		return false, fmt.Errorf("unsupported registration admission mode")
	}
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM account_identities WHERE kind = 'email' AND normalized_value = $1
		)`, record.NormalizedEmail).Scan(&exists); err != nil {
		return false, fmt.Errorf("check email identity: %w", err)
	}
	if exists {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO accounts (id, status, password_hash, created_at)
		VALUES ($1, 'pending_verification', NULL, $2)`, record.AccountID, record.CreatedAt); err != nil {
		return false, fmt.Errorf("insert pending account: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO account_identities (id, account_id, kind, normalized_value, created_at)
		VALUES ($1, $2, 'email', $3, $4)`,
		record.IdentityID, record.AccountID, record.NormalizedEmail, record.CreatedAt); err != nil {
		return false, fmt.Errorf("insert email identity: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO password_credentials (account_id, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $3)`, record.AccountID, record.PasswordHash, record.CreatedAt); err != nil {
		return false, fmt.Errorf("insert password credential: %w", err)
	}
	if err := insertActionToken(ctx, tx, record.AccountID, auth.ActionEmailVerification,
		record.ActionSelector, record.ActionSecretHash, record.ActionExpiresAt, record.CreatedAt); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit register account: %w", err)
	}
	return true, nil
}

func (s *Store) ReplaceVerificationToken(
	ctx context.Context,
	email, selector string,
	secretHash []byte,
	expiresAt, now time.Time,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin replace verification token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accountID string
	err = tx.QueryRow(ctx, `
		SELECT a.id
		FROM account_identities i
		JOIN accounts a ON a.id = i.account_id
		WHERE i.kind = 'email' AND i.normalized_value = $1
		  AND a.status = 'pending_verification'
		FOR UPDATE OF a`, email).Scan(&accountID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("find pending email identity: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM auth_action_tokens WHERE account_id = $1 AND purpose = 'email_verification'`, accountID); err != nil {
		return false, fmt.Errorf("delete prior verification token: %w", err)
	}
	if err := insertActionToken(ctx, tx, accountID, auth.ActionEmailVerification, selector, secretHash, expiresAt, now); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit replace verification token: %w", err)
	}
	return true, nil
}

func (s *Store) ConsumeActionAndActivate(
	ctx context.Context,
	proof auth.ActionProof,
	seed auth.RefreshSeed,
	now time.Time,
) (auth.Account, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.Account{}, fmt.Errorf("begin consume action token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accountID string
	var purpose auth.ActionPurpose
	var storedHash []byte
	var expiresAt time.Time
	var consumedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT account_id, purpose, secret_hash, expires_at, consumed_at
		FROM auth_action_tokens WHERE selector = $1 FOR UPDATE`, proof.Selector).
		Scan(&accountID, &purpose, &storedHash, &expiresAt, &consumedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.Account{}, auth.ErrInvalidOrExpiredActionToken
		}
		return auth.Account{}, fmt.Errorf("lock action token: %w", err)
	}
	if consumedAt != nil || !now.Before(expiresAt) ||
		subtle.ConstantTimeCompare(storedHash, proof.SecretHash) != 1 ||
		(proof.Purpose != "" && proof.Purpose != purpose) {
		return auth.Account{}, auth.ErrInvalidOrExpiredActionToken
	}
	var account auth.Account
	var verifiedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.status, a.created_at, i.normalized_value, i.verified_at
		FROM accounts a
		JOIN account_identities i ON i.account_id = a.id AND i.kind = 'email'
		WHERE a.id = $1 FOR UPDATE OF a, i`, accountID).
		Scan(&account.ID, &account.Status, &account.CreatedAt, &account.Email, &verifiedAt)
	if err != nil {
		return auth.Account{}, fmt.Errorf("lock action account: %w", err)
	}
	validState := (purpose == auth.ActionEmailVerification && account.Status == auth.AccountPendingVerification) ||
		(purpose == auth.ActionLegacyClaim && account.Status == auth.AccountLegacyUnclaimed)
	if !validState || verifiedAt != nil {
		return auth.Account{}, auth.ErrInvalidOrExpiredActionToken
	}
	if _, err := tx.Exec(ctx, `UPDATE auth_action_tokens SET consumed_at = $2 WHERE selector = $1`, proof.Selector, now); err != nil {
		return auth.Account{}, fmt.Errorf("consume action token: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE account_identities SET verified_at = $2 WHERE account_id = $1 AND kind = 'email'`, accountID, now); err != nil {
		return auth.Account{}, fmt.Errorf("verify email identity: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET status = 'active' WHERE id = $1`, accountID); err != nil {
		return auth.Account{}, fmt.Errorf("activate account: %w", err)
	}
	if err := insertRefreshSession(ctx, tx, accountID, seed); err != nil {
		return auth.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.Account{}, fmt.Errorf("commit consume action token: %w", err)
	}
	account.Status = auth.AccountActive
	return account, nil
}

func (s *Store) FindLoginRecord(ctx context.Context, email string) (auth.LoginRecord, bool, error) {
	var record auth.LoginRecord
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.status, a.created_at, i.normalized_value, pc.password_hash
		FROM account_identities i
		JOIN accounts a ON a.id = i.account_id
		JOIN password_credentials pc ON pc.account_id = a.id
		WHERE i.kind = 'email' AND i.normalized_value = $1`, email).
		Scan(&record.Account.ID, &record.Account.Status, &record.Account.CreatedAt,
			&record.Account.Email, &record.PasswordHash)
	if err != nil {
		if err == pgx.ErrNoRows {
			return auth.LoginRecord{}, false, nil
		}
		return auth.LoginRecord{}, false, fmt.Errorf("find login record: %w", err)
	}
	return record, true, nil
}

func (s *Store) CurrentAccount(ctx context.Context, accountID string) (auth.Account, error) {
	var account auth.Account
	err := s.pool.QueryRow(ctx, `
		SELECT a.id, a.status, a.created_at, i.normalized_value
		FROM accounts a
		JOIN account_identities i ON i.account_id = a.id AND i.kind = 'email'
		WHERE a.id = $1 AND a.status = 'active' AND i.verified_at IS NOT NULL`, accountID).
		Scan(&account.ID, &account.Status, &account.CreatedAt, &account.Email)
	if err != nil {
		return auth.Account{}, fmt.Errorf("current account: %w", err)
	}
	return account, nil
}

func (s *Store) PlanLegacyClaim(ctx context.Context, email string) (auth.LegacyClaimRecord, error) {
	record, err := inspectLegacyClaim(ctx, s.pool, email, false)
	if err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("plan legacy claim: %w", err)
	}
	return record, nil
}

func (s *Store) BeginLegacyClaim(ctx context.Context, command auth.LegacyClaimCommand) (auth.LegacyClaimRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("begin legacy claim: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := inspectLegacyClaim(ctx, tx, command.NormalizedEmail, true)
	if err != nil {
		return auth.LegacyClaimRecord{}, err
	}
	switch record.State {
	case auth.LegacyClaimReady:
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_identities (id, account_id, kind, normalized_value, created_at)
			VALUES ($1, $2, 'email', $3, $4)`, command.IdentityID, record.AccountID,
			command.NormalizedEmail, command.CreatedAt); err != nil {
			return auth.LegacyClaimRecord{}, fmt.Errorf("insert legacy email identity: %w", err)
		}
	case auth.LegacyClaimPendingSameEmail:
		if _, err := tx.Exec(ctx, `
			DELETE FROM auth_action_tokens
			WHERE account_id = $1 AND purpose = 'legacy_claim'`, record.AccountID); err != nil {
			return auth.LegacyClaimRecord{}, fmt.Errorf("delete prior legacy claim token: %w", err)
		}
	default:
		return record, nil
	}
	if err := insertActionToken(ctx, tx, record.AccountID, auth.ActionLegacyClaim,
		command.ActionSelector, command.ActionSecretHash, command.ActionExpiresAt, command.CreatedAt); err != nil {
		return auth.LegacyClaimRecord{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("commit legacy claim: %w", err)
	}
	record.PendingClaimCount = 1
	return record, nil
}

func (s *Store) InspectLegacyState(ctx context.Context) (auth.LegacyAuthState, error) {
	var state auth.LegacyAuthState
	if err := s.pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM accounts WHERE status = 'legacy_unclaimed'),
		(SELECT count(*) FROM accounts a
		 WHERE a.status = 'legacy_unclaimed'
		   AND EXISTS (SELECT 1 FROM account_identities i
		               WHERE i.account_id = a.id AND i.kind = 'email' AND i.verified_at IS NULL)
		   AND EXISTS (SELECT 1 FROM auth_action_tokens t
		               WHERE t.account_id = a.id AND t.purpose = 'legacy_claim' AND t.consumed_at IS NULL)),
		(SELECT count(*) FROM accounts a
		 WHERE a.status = 'active'
		   AND EXISTS (SELECT 1 FROM account_identities i
		               WHERE i.account_id = a.id AND i.kind = 'email' AND i.verified_at IS NOT NULL)
		   AND EXISTS (SELECT 1 FROM auth_action_tokens t
		               WHERE t.account_id = a.id AND t.purpose = 'legacy_claim' AND t.consumed_at IS NOT NULL))`).
		Scan(&state.LegacyUnclaimedCount, &state.PendingClaimCount, &state.ActiveClaimedCount); err != nil {
		return auth.LegacyAuthState{}, fmt.Errorf("inspect legacy state: %w", err)
	}
	return state, nil
}

type legacyClaimQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func inspectLegacyClaim(
	ctx context.Context,
	query legacyClaimQuerier,
	email string,
	lock bool,
) (auth.LegacyClaimRecord, error) {
	lockClause := ""
	if lock {
		lockClause = " FOR UPDATE"
	}
	rows, err := query.Query(ctx, `SELECT id FROM accounts WHERE status = 'legacy_unclaimed' ORDER BY id`+lockClause)
	if err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("list legacy accounts: %w", err)
	}
	legacyIDs := make([]string, 0, 2)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return auth.LegacyClaimRecord{}, fmt.Errorf("scan legacy account: %w", err)
		}
		legacyIDs = append(legacyIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("list legacy accounts: %w", err)
	}

	record := auth.LegacyClaimRecord{LegacyCount: len(legacyIDs)}
	if err := query.QueryRow(ctx, `SELECT count(*) FROM accounts a
		WHERE a.status = 'legacy_unclaimed'
		  AND EXISTS (SELECT 1 FROM account_identities i
		              WHERE i.account_id = a.id AND i.kind = 'email' AND i.verified_at IS NULL)
		  AND EXISTS (SELECT 1 FROM auth_action_tokens t
		              WHERE t.account_id = a.id AND t.purpose = 'legacy_claim' AND t.consumed_at IS NULL)`).
		Scan(&record.PendingClaimCount); err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("count pending legacy claims: %w", err)
	}
	if len(legacyIDs) == 0 {
		var accountID string
		err := query.QueryRow(ctx, `SELECT a.id
			FROM account_identities i
			JOIN accounts a ON a.id = i.account_id
			WHERE i.kind = 'email' AND i.normalized_value = $1 AND a.status = 'active'
			  AND EXISTS (SELECT 1 FROM auth_action_tokens t
			              WHERE t.account_id = a.id AND t.purpose = 'legacy_claim' AND t.consumed_at IS NOT NULL)`, email).
			Scan(&accountID)
		switch err {
		case nil:
			record.State = auth.LegacyClaimAlreadyClaimed
			record.AccountID = accountID
		case pgx.ErrNoRows:
			record.State = auth.LegacyClaimNoTarget
		default:
			return auth.LegacyClaimRecord{}, fmt.Errorf("find completed legacy claim: %w", err)
		}
		return record, nil
	}
	if len(legacyIDs) != 1 {
		record.State = auth.LegacyClaimConflict
		return record, nil
	}

	record.AccountID = legacyIDs[0]
	emailLockClause := ""
	if lock {
		emailLockClause = " FOR UPDATE"
	}
	var existingAccountID string
	var verifiedAt *time.Time
	err = query.QueryRow(ctx, `SELECT account_id, verified_at
		FROM account_identities
		WHERE kind = 'email' AND normalized_value = $1`+emailLockClause, email).
		Scan(&existingAccountID, &verifiedAt)
	switch {
	case err == nil && existingAccountID == record.AccountID && verifiedAt == nil:
		record.State = auth.LegacyClaimPendingSameEmail
		return record, nil
	case err == nil:
		record.State = auth.LegacyClaimConflict
		return record, nil
	case err != pgx.ErrNoRows:
		return auth.LegacyClaimRecord{}, fmt.Errorf("find legacy email identity: %w", err)
	}

	var accountHasIdentity bool
	if err := query.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM account_identities WHERE account_id = $1 AND kind = 'email'
	)`, record.AccountID).Scan(&accountHasIdentity); err != nil {
		return auth.LegacyClaimRecord{}, fmt.Errorf("check legacy account identity: %w", err)
	}
	if accountHasIdentity {
		record.State = auth.LegacyClaimConflict
		return record, nil
	}
	record.State = auth.LegacyClaimReady
	return record, nil
}

func insertActionToken(
	ctx context.Context,
	tx pgx.Tx,
	accountID string,
	purpose auth.ActionPurpose,
	selector string,
	secretHash []byte,
	expiresAt, now time.Time,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO auth_action_tokens (selector, account_id, purpose, secret_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, selector, accountID, purpose, secretHash, expiresAt, now)
	if err != nil {
		return fmt.Errorf("insert action token: %w", err)
	}
	return nil
}

func insertRefreshSession(ctx context.Context, tx pgx.Tx, accountID string, seed auth.RefreshSeed) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_session_families (id, account_id, absolute_expires_at, created_at)
		VALUES ($1, $2, $3, $4)`, seed.FamilyID, accountID, seed.AbsoluteExpiresAt, seed.CreatedAt); err != nil {
		return fmt.Errorf("insert refresh family: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_session_generations (id, family_id, token_hash, idle_expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)`, seed.GenerationID, seed.FamilyID,
		seed.TokenHash, seed.IdleExpiresAt, seed.CreatedAt); err != nil {
		return fmt.Errorf("insert refresh generation: %w", err)
	}
	return nil
}
