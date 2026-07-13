package settings

import (
	"context"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Repository 是 settings 持久化面。
type Repository interface {
	Get(ctx context.Context, scope store.AccountScope) (Settings, bool, error)
	Upsert(ctx context.Context, scope store.AccountScope, settings Settings) (Settings, error)
}

// ScopeFactory 由 composition root 注入，供 TimezoneForAccount 构造 AccountScope。
type ScopeFactory func(accountID string) store.AccountScope

// Service 封装默认值叠加、校验与时区提供。
type Service struct {
	repo     Repository
	scopeFor ScopeFactory
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// WithScopeFactory 注入账号 scope 工厂，供 AccountTimezoneProvider 使用。
func (s *Service) WithScopeFactory(factory ScopeFactory) *Service {
	s.scopeFor = factory
	return s
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
	return mergeWithDefaults(stored), nil
}

// Patch 校验后 upsert；返回叠加默认后的有效设置。
func (s *Service) Patch(ctx context.Context, scope store.AccountScope, input PatchInput) (Settings, error) {
	current, err := s.Get(ctx, scope)
	if err != nil {
		return Settings{}, err
	}
	next := current
	if input.Timezone != nil {
		tz := strings.TrimSpace(*input.Timezone)
		if err := validateTimezone(tz); err != nil {
			return Settings{}, err
		}
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
		// entry 级叠加：以默认全类型为底，再被 PATCH 条目覆盖。
		next.ChurnThresholds = overlayChurnThresholds(defaultChurnThresholds(), normalized)
	}
	saved, err := s.repo.Upsert(ctx, scope, next)
	if err != nil {
		return Settings{}, err
	}
	return mergeWithDefaults(saved), nil
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

// mergeWithDefaults 保证读取时 churn 数组 entry 级完整。
func mergeWithDefaults(stored Settings) Settings {
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
	return stored
}
