package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const settingsColumns = "timezone, birthday_lead_days, follow_up_after_days, churn_thresholds, digest_hour, telegram_chat_id, updated_at"

// PostgresRepository 实现 settings.Repository。
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (PostgresRepository) Get(ctx context.Context, scope store.AccountScope) (Settings, bool, error) {
	var (
		s          Settings
		thresholds []byte
		chatID     sql.NullString
		updatedAt  time.Time
	)
	err := scope.QueryRow(ctx, "settings", settingsColumns, "").Scan(
		&s.Timezone,
		&s.BirthdayLeadDays,
		&s.FollowUpAfterDays,
		&thresholds,
		&s.DigestHour,
		&chatID,
		&updatedAt,
	)
	if errors.Is(err, store.ErrNoRows) {
		return Settings{}, false, nil
	}
	if err != nil {
		return Settings{}, false, err
	}
	if len(thresholds) > 0 {
		if err := json.Unmarshal(thresholds, &s.ChurnThresholds); err != nil {
			return Settings{}, false, err
		}
	}
	if chatID.Valid {
		v := chatID.String
		s.TelegramChatID = &v
	}
	s.UpdatedAt = updatedAt
	return s, true, nil
}

func (PostgresRepository) Upsert(ctx context.Context, scope store.AccountScope, settings Settings) (Settings, error) {
	thresholds, err := json.Marshal(settings.ChurnThresholds)
	if err != nil {
		return Settings{}, err
	}
	// 先查后写：有行则 Update，无行则 Insert（主键 = account_id）。
	_, found, err := PostgresRepository{}.Get(ctx, scope)
	if err != nil {
		return Settings{}, err
	}
	now := time.Now().UTC()
	var chat any
	if settings.TelegramChatID != nil {
		chat = *settings.TelegramChatID
	}
	if found {
		if _, err := scope.Update(ctx, "settings",
			"timezone = $2, birthday_lead_days = $3, follow_up_after_days = $4, churn_thresholds = $5, digest_hour = $6, telegram_chat_id = $7, updated_at = $8",
			"",
			settings.Timezone,
			settings.BirthdayLeadDays,
			settings.FollowUpAfterDays,
			thresholds,
			settings.DigestHour,
			chat,
			now,
		); err != nil {
			return Settings{}, err
		}
	} else {
		if err := scope.Insert(ctx, "settings",
			[]string{
				"timezone",
				"birthday_lead_days",
				"follow_up_after_days",
				"churn_thresholds",
				"digest_hour",
				"telegram_chat_id",
				"updated_at",
			},
			settings.Timezone,
			settings.BirthdayLeadDays,
			settings.FollowUpAfterDays,
			thresholds,
			settings.DigestHour,
			chat,
			now,
		); err != nil {
			return Settings{}, err
		}
	}
	saved, _, err := PostgresRepository{}.Get(ctx, scope)
	if err != nil {
		return Settings{}, err
	}
	return saved, nil
}
