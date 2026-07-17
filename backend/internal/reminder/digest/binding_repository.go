package digest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type PostgresBindingRepository struct{}

func NewPostgresBindingRepository() PostgresBindingRepository { return PostgresBindingRepository{} }

func (PostgresBindingRepository) IssueBindToken(
	ctx context.Context,
	scope store.AccountScope,
	tokenHash []byte,
	expiresAt time.Time,
) error {
	return scope.Upsert(ctx, "telegram_bind_tokens",
		[]string{"token_hash", "expires_at", "consumed_at", "updated_at"},
		[]string{"account_id"},
		[]string{"token_hash", "expires_at", "consumed_at", "updated_at"},
		tokenHash, expiresAt, nil, time.Now().UTC())
}

func (PostgresBindingRepository) MatchBindToken(
	ctx context.Context,
	scope store.AccountScope,
	tokenHash []byte,
	now time.Time,
) (TokenMatchOutcome, error) {
	exists, err := scope.Exists(ctx, "telegram_bind_tokens",
		"token_hash = $2 AND expires_at > $3 AND consumed_at IS NULL", tokenHash, now)
	if err != nil {
		return TokenNotMatched, err
	}
	if !exists {
		return TokenNotMatched, nil
	}
	return TokenMatched, nil
}

func (PostgresBindingRepository) ConsumeAndBind(
	ctx context.Context,
	scope store.AccountScope,
	tokenHash []byte,
	chatID string,
	updateID int64,
	now time.Time,
) (BindOutcome, error) {
	outcome := BindInvalid
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var storedHash []byte
		err := tx.QueryRowForUpdate(ctx, "telegram_bind_tokens", "token_hash",
			"token_hash = $2 AND expires_at > $3 AND consumed_at IS NULL", tokenHash, now).Scan(&storedHash)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "telegram_bind_tokens",
			"consumed_at = $2, updated_at = $2",
			"token_hash = $3 AND consumed_at IS NULL", now, tokenHash); err != nil {
			return err
		}
		if err := tx.Upsert(ctx, "settings",
			[]string{"telegram_chat_id", "updated_at"},
			[]string{"account_id"},
			[]string{"telegram_chat_id", "updated_at"},
			chatID, now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "telegram_deliveries",
			"status = $2, claim_id = NULL, lease_until = NULL, updated_at = $3",
			"source = $4 AND status = $5",
			string(DeliveryStatusSuperseded), now,
			string(DeliverySourceBindingAck), string(DeliveryStatusPending)); err != nil {
			return err
		}
		id, err := randomDeliveryID()
		if err != nil {
			return err
		}
		if err := tx.Insert(ctx, "telegram_deliveries", []string{
			"id", "source", "source_key", "message_kind", "status", "next_attempt_at", "updated_at",
		}, id, string(DeliverySourceBindingAck), strconv.FormatInt(updateID, 10),
			string(MessageKindBindingAck), string(DeliveryStatusPending), now, now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "telegram_deliveries",
			"next_attempt_at = $2, last_error_code = NULL, updated_at = $2",
			"status = $3 AND last_error_code IN ($4, $5)",
			now, string(DeliveryStatusPending), "recipient_missing", "recipient_integrity"); err != nil {
			return err
		}
		outcome = BindApplied
		return nil
	})
	if err != nil {
		if store.IsUniqueViolation(err, "settings_telegram_chat_id_unique") {
			return BindChatConflict, nil
		}
		return BindInvalid, err
	}
	return outcome, nil
}

func (PostgresBindingRepository) MatchCurrentChat(
	ctx context.Context,
	scope store.AccountScope,
	chatID string,
) (string, ChatMatchOutcome, error) {
	var current sql.NullString
	err := scope.QueryRow(ctx, "settings", "telegram_chat_id", "telegram_chat_id = $2", chatID).Scan(&current)
	if errors.Is(err, store.ErrNoRows) {
		return "", ChatNotMatched, nil
	}
	if err != nil {
		return "", ChatNotMatched, err
	}
	if !current.Valid {
		return "", ChatNotMatched, nil
	}
	return current.String, ChatMatched, nil
}

func (PostgresBindingRepository) ClaimCommandIfCurrentChat(
	ctx context.Context,
	scope store.AccountScope,
	chatID string,
	updateID int64,
	target LocalTarget,
	messageKind MessageKind,
) (ClaimOutcome, error) {
	outcome := ClaimUnauthorized
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var current sql.NullString
		err := tx.QueryRowForUpdate(ctx, "settings", "telegram_chat_id", "").Scan(&current)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !current.Valid || current.String != chatID {
			return nil
		}
		id, err := randomDeliveryID()
		if err != nil {
			return err
		}
		row := tx.InsertOnConflictDoNothingReturning(ctx, "telegram_deliveries",
			[]string{
				"id", "source", "source_key", "message_kind", "target_local_date",
				"timezone_at_enqueue", "status", "next_attempt_at", "updated_at",
			},
			[]string{"account_id", "source", "source_key"},
			[]string{"id"},
			id, string(DeliverySourceCommand), strconv.FormatInt(updateID, 10), string(messageKind),
			target.LocalDate, target.Timezone, string(DeliveryStatusPending), time.Now().UTC(), time.Now().UTC())
		var returnedID string
		if err := row.Scan(&returnedID); errors.Is(err, store.ErrNoRows) {
			outcome = ClaimExisting
			return nil
		} else if err != nil {
			return err
		}
		outcome = ClaimCreated
		return nil
	})
	return outcome, err
}

func randomDeliveryID() (string, error) {
	id, err := randomClaimID()
	if err != nil {
		return "", fmt.Errorf("generate delivery id: %w", err)
	}
	return "tgd_" + id, nil
}

var (
	_ BindingRepository  = PostgresBindingRepository{}
	_ ChatBindingMatcher = PostgresBindingRepository{}
)
