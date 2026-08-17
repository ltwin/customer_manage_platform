package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const settingsColumns = "timezone, birthday_lead_days, follow_up_after_days, churn_thresholds, digest_hour, telegram_chat_id, telegram_binding_revision, availability, planning_business_rule_overrides, planning_business_rule_revision, updated_at"

// PostgresRepository 实现 settings.Repository。
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (PostgresRepository) Get(ctx context.Context, scope store.AccountScope) (Settings, bool, error) {
	var (
		s             Settings
		thresholds    []byte
		availability  []byte
		businessRules []byte
		chatID        sql.NullString
		bindingRev    int64
		updatedAt     time.Time
	)
	err := scope.QueryRow(ctx, "settings", settingsColumns, "").Scan(
		&s.Timezone,
		&s.BirthdayLeadDays,
		&s.FollowUpAfterDays,
		&thresholds,
		&s.DigestHour,
		&chatID,
		&bindingRev,
		&availability,
		&businessRules,
		&s.PlanningBusinessRuleRevision,
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
	decodedAvailability, err := DecodeScheduleAvailabilityJSON(availability)
	if err != nil {
		return Settings{}, false, fmt.Errorf("decode stored settings availability: %v", err)
	}
	s.Availability = decodedAvailability
	if err := json.Unmarshal(businessRules, &s.PlanningBusinessRuleOverrides); err != nil {
		return Settings{}, false, fmt.Errorf("decode planning business rule overrides: %w", err)
	}
	if chatID.Valid {
		v := chatID.String
		s.TelegramChatID = &v
	}
	s.TelegramBindingRevision = bindingRev
	s.UpdatedAt = updatedAt
	return s, true, nil
}

func (PostgresRepository) Upsert(ctx context.Context, scope store.AccountScope, settings Settings) (Settings, error) {
	thresholds, err := json.Marshal(settings.ChurnThresholds)
	if err != nil {
		return Settings{}, err
	}
	availability, err := encodeScheduleAvailabilityJSON(settings.Availability)
	if err != nil {
		return Settings{}, err
	}
	businessRules, err := json.Marshal(settings.PlanningBusinessRuleOverrides)
	if err != nil {
		return Settings{}, err
	}
	now := time.Now().UTC()
	ownedColumns := []string{
		"timezone", "birthday_lead_days", "follow_up_after_days", "churn_thresholds", "digest_hour", "availability",
		"planning_business_rule_overrides", "planning_business_rule_revision", "updated_at",
	}
	if err := scope.Upsert(ctx, "settings",
		ownedColumns,
		[]string{"account_id"},
		ownedColumns,
		settings.Timezone,
		settings.BirthdayLeadDays,
		settings.FollowUpAfterDays,
		thresholds,
		settings.DigestHour,
		availability,
		businessRules,
		settings.PlanningBusinessRuleRevision,
		now,
	); err != nil {
		return Settings{}, err
	}
	saved, _, err := PostgresRepository{}.Get(ctx, scope)
	if err != nil {
		return Settings{}, err
	}
	return saved, nil
}
