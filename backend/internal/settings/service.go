package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

// Repository 是 settings 持久化面。
type Repository interface {
	Get(ctx context.Context, scope store.AccountScope) (Settings, bool, error)
	Upsert(ctx context.Context, scope store.AccountScope, settings Settings) (Settings, error)
}

// ScopeFactory 由 composition root 注入，供 TimezoneForAccount 构造 AccountScope。
type ScopeFactory func(accountID string) store.AccountScope

// TimezoneChangePlanningParticipant is caller-owned by settings; reminder supplies the real adapter.
type TimezoneChangePlanningParticipant interface {
	ListAffectedPlanIDsInScope(ctx context.Context, tx store.TxAccountScope, oldTimezone, newTimezone string) ([]string, error)
}

// Service 封装默认值叠加、校验与时区提供。
type Service struct {
	repo                Repository
	scopeFor            ScopeFactory
	reminderTZEnabled   bool
	timezoneParticipant TimezoneChangePlanningParticipant
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// WithScopeFactory 注入账号 scope 工厂，供 AccountTimezoneProvider 使用。
func (s *Service) WithScopeFactory(factory ScopeFactory) *Service {
	s.scopeFor = factory
	return s
}

// WithPlanningReminderTimezone enables the fence-aware timezone patch path.
// When enabled, a real participant is required; missing wiring fails closed.
func (s *Service) WithPlanningReminderTimezone(enabled bool, participant TimezoneChangePlanningParticipant) (*Service, error) {
	if enabled && participant == nil {
		return nil, errors.New("settings timezone participant required when reminder timezone enabled")
	}
	if !enabled && participant != nil {
		return nil, errors.New("settings timezone participant must be nil when reminder timezone disabled")
	}
	s.reminderTZEnabled = enabled
	s.timezoneParticipant = participant
	return s, nil
}

// Get 返回有效设置：无行时返回纯默认，不隐式建行。
func (s *Service) Get(ctx context.Context, scope store.AccountScope) (Settings, error) {
	stored, found, err := s.repo.Get(ctx, scope)
	if err != nil {
		return Settings{}, err
	}
	if !found {
		return DefaultSettings(), nil
	}
	return EffectiveSettings(stored), nil
}

// Patch 校验后 upsert；reminder 启用且 timezone 变更时走 fence + generation reserve。
func (s *Service) Patch(ctx context.Context, scope store.AccountScope, input PatchInput) (Settings, error) {
	current, err := s.Get(ctx, scope)
	if err != nil {
		return Settings{}, err
	}
	next := current
	timezoneTouched := false
	if input.Timezone != nil {
		tz := strings.TrimSpace(*input.Timezone)
		if err := validateTimezone(tz); err != nil {
			return Settings{}, err
		}
		timezoneTouched = true
		next.Timezone = tz
	}
	if input.BirthdayLeadDays != nil {
		if *input.BirthdayLeadDays < 1 {
			return Settings{}, ValidationError{Message: "birthday_lead_days 须 ≥ 1"}
		}
		next.BirthdayLeadDays = *input.BirthdayLeadDays
	}
	if input.FollowUpAfterDays != nil {
		if *input.FollowUpAfterDays < 1 {
			return Settings{}, ValidationError{Message: "follow_up_after_days 须 ≥ 1"}
		}
		next.FollowUpAfterDays = *input.FollowUpAfterDays
	}
	if input.DigestHour != nil {
		if *input.DigestHour < 0 || *input.DigestHour > 23 {
			return Settings{}, ValidationError{Message: "digest_hour 须在 0-23"}
		}
		next.DigestHour = *input.DigestHour
	}
	if input.ChurnThresholds != nil {
		normalized, err := normalizeChurnThresholds(*input.ChurnThresholds)
		if err != nil {
			return Settings{}, err
		}
		next.ChurnThresholds = overlayChurnThresholds(defaultChurnThresholds(), normalized)
	}
	if input.Availability != nil {
		if err := ValidateScheduleAvailability(*input.Availability); err != nil {
			return Settings{}, err
		}
		next.Availability = *input.Availability
	}

	if s.reminderTZEnabled && timezoneTouched {
		return s.patchWithReminderTimezone(ctx, scope, current, next)
	}
	saved, err := s.repo.Upsert(ctx, scope, next)
	if err != nil {
		return Settings{}, err
	}
	return EffectiveSettings(saved), nil
}

func (s *Service) patchWithReminderTimezone(
	ctx context.Context,
	scope store.AccountScope,
	current, next Settings,
) (Settings, error) {
	if s.timezoneParticipant == nil {
		return Settings{}, errors.New("settings timezone participant missing")
	}
	var saved Settings
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		locked, err := tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return err
		}
		if err := lockSettingsRow(ctx, tx); err != nil {
			return err
		}
		effectiveOld := EffectiveSettings(current).Timezone
		effectiveNew := EffectiveSettings(next).Timezone
		if effectiveOld == effectiveNew {
			upserted, err := upsertSettingsInScope(ctx, tx, next)
			if err != nil {
				return err
			}
			saved = upserted
			return nil
		}
		planIDs, err := s.timezoneParticipant.ListAffectedPlanIDsInScope(ctx, tx, effectiveOld, effectiveNew)
		if err != nil {
			return err
		}
		upserted, err := upsertSettingsInScope(ctx, tx, next)
		if err != nil {
			return err
		}
		for _, planID := range planIDs {
			if _, err := locked.ReserveGeneration(ctx, planningreminder.MutationFact{
				PlanID: planID, MutationKind: planningreminder.MutationSettingsTimezoneChanged,
			}); err != nil {
				return err
			}
		}
		saved = upserted
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return EffectiveSettings(saved), nil
}

func lockSettingsRow(ctx context.Context, tx store.TxAccountScope) error {
	var tz string
	err := tx.QueryRowForUpdate(ctx, "settings", "timezone", "TRUE").Scan(&tz)
	if errors.Is(err, store.ErrNoRows) {
		// No row yet: Upsert will create; still OK for accounts without planning sources.
		return nil
	}
	return err
}

func upsertSettingsInScope(ctx context.Context, tx store.TxAccountScope, settings Settings) (Settings, error) {
	thresholds, err := json.Marshal(settings.ChurnThresholds)
	if err != nil {
		return Settings{}, err
	}
	availability, err := encodeScheduleAvailabilityJSON(settings.Availability)
	if err != nil {
		return Settings{}, err
	}
	now := time.Now().UTC()
	ownedColumns := []string{
		"timezone", "birthday_lead_days", "follow_up_after_days", "churn_thresholds", "digest_hour", "availability", "updated_at",
	}
	if err := tx.Upsert(ctx, "settings",
		ownedColumns,
		[]string{"account_id"},
		ownedColumns,
		settings.Timezone,
		settings.BirthdayLeadDays,
		settings.FollowUpAfterDays,
		thresholds,
		settings.DigestHour,
		availability,
		now,
	); err != nil {
		return Settings{}, fmt.Errorf("upsert settings in tx: %w", err)
	}
	var (
		s           Settings
		threshBytes []byte
		availBytes  []byte
		telegram    sql.NullString
		bindingRev  int64
		updatedAt   time.Time
	)
	if err := tx.QueryRow(ctx, "settings", settingsColumns, "TRUE").Scan(
		&s.Timezone, &s.BirthdayLeadDays, &s.FollowUpAfterDays, &threshBytes,
		&s.DigestHour, &telegram, &bindingRev, &availBytes, &updatedAt,
	); err != nil {
		return Settings{}, err
	}
	if telegram.Valid {
		v := telegram.String
		s.TelegramChatID = &v
	}
	s.TelegramBindingRevision = bindingRev
	if len(threshBytes) > 0 {
		_ = json.Unmarshal(threshBytes, &s.ChurnThresholds)
	}
	decoded, err := DecodeScheduleAvailabilityJSON(availBytes)
	if err != nil {
		return Settings{}, err
	}
	s.Availability = decoded
	s.UpdatedAt = updatedAt
	return s, nil
}

// TimezoneForAccount 实现 httpapi.AccountTimezoneProvider。
func (s *Service) TimezoneForAccount(ctx context.Context, accountID string) (string, error) {
	if s.scopeFor == nil {
		return DefaultTimezone, nil
	}
	settings, err := s.Get(ctx, s.scopeFor(accountID))
	if err != nil {
		return "", err
	}
	return settings.Timezone, nil
}

func validateTimezone(tz string) error {
	if tz == "" {
		return ValidationError{Message: "timezone 必填"}
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return ValidationError{Message: "timezone 非法 IANA 时区"}
	}
	return nil
}

func normalizeChurnThresholds(entries []ChurnThreshold) ([]ChurnThreshold, error) {
	seen := make(map[string]struct{}, len(entries))
	out := make([]ChurnThreshold, 0, len(entries))
	for _, entry := range entries {
		st := strings.TrimSpace(entry.ShootType)
		if st != ShootTypePortrait && st != ShootTypeCosplay && st != ShootTypeOther {
			return nil, ValidationError{Message: "churn_thresholds.shoot_type 非法"}
		}
		if entry.Days < 1 {
			return nil, ValidationError{Message: "churn_thresholds.days 须 ≥ 1"}
		}
		if _, ok := seen[st]; ok {
			return nil, ValidationError{Message: "churn_thresholds.shoot_type 重复"}
		}
		seen[st] = struct{}{}
		out = append(out, ChurnThreshold{ShootType: st, Days: entry.Days})
	}
	return out, nil
}

// overlayChurnThresholds：base 全类型默认，overrides 逐条覆盖。
func overlayChurnThresholds(base, overrides []ChurnThreshold) []ChurnThreshold {
	byType := make(map[string]int, len(base))
	order := make([]string, 0, len(base))
	for _, e := range base {
		byType[e.ShootType] = e.Days
		order = append(order, e.ShootType)
	}
	for _, e := range overrides {
		if _, ok := byType[e.ShootType]; !ok {
			order = append(order, e.ShootType)
		}
		byType[e.ShootType] = e.Days
	}
	out := make([]ChurnThreshold, 0, len(order))
	for _, st := range order {
		out = append(out, ChurnThreshold{ShootType: st, Days: byType[st]})
	}
	return out
}

// EffectiveSettings returns the stored row overlaid with all read defaults.
func EffectiveSettings(stored Settings) Settings {
	def := DefaultSettings()
	if stored.Timezone == "" {
		stored.Timezone = def.Timezone
	}
	if stored.BirthdayLeadDays < 1 {
		stored.BirthdayLeadDays = def.BirthdayLeadDays
	}
	if stored.FollowUpAfterDays < 1 {
		stored.FollowUpAfterDays = def.FollowUpAfterDays
	}
	if stored.DigestHour < 0 || stored.DigestHour > 23 {
		stored.DigestHour = def.DigestHour
	}
	stored.ChurnThresholds = overlayChurnThresholds(def.ChurnThresholds, stored.ChurnThresholds)
	if isZeroScheduleAvailability(stored.Availability) {
		stored.Availability = def.Availability
	}
	if stored.TelegramBindingRevision < 1 {
		stored.TelegramBindingRevision = def.TelegramBindingRevision
	}
	return stored
}
