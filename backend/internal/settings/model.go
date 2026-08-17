// Package settings 实现账号级业务配置域（时区、提醒参数、digest_hour）。
package settings

import (
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
)

const (
	DefaultTimezone          = "Asia/Shanghai"
	DefaultBirthdayLeadDays  = 3
	DefaultFollowUpAfterDays = 7
	DefaultChurnDays         = 180
	DefaultDigestHour        = 9

	ShootTypePortrait = "portrait"
	ShootTypeCosplay  = "cosplay"
	ShootTypeOther    = "other"
)

var (
	ErrValidation                   = errors.New("validation_failed")
	ErrNotFound                     = errors.New("not_found")
	ErrPlanningBusinessRuleRevision = errors.New("planning_business_rule_revision_conflict")
)

// ValidationError 是可回传给 HTTP 层的校验失败。
type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }

// ChurnThreshold 是按拍摄类型的流失天数阈值。
type ChurnThreshold struct {
	ShootType string `json:"shoot_type"`
	Days      int    `json:"days"`
}

// Settings 是账号级有效设置（读取时已叠加默认值）。
type Settings struct {
	Timezone                      string
	BirthdayLeadDays              int
	FollowUpAfterDays             int
	ChurnThresholds               []ChurnThreshold
	DigestHour                    int
	TelegramChatID                *string
	TelegramBindingRevision       int64
	Availability                  ScheduleAvailability
	PlanningBusinessRuleOverrides business.RuleOverrides
	PlanningBusinessRuleRevision  int64
	UpdatedAt                     time.Time
}

type PlanningBusinessRulesPatch struct {
	ExpectedRevision int64
	Overrides        business.RuleOverrides
}

// PatchInput 是 PATCH /settings 的域输入；指针表示"未传"。
type PatchInput struct {
	Timezone              *string
	BirthdayLeadDays      *int
	FollowUpAfterDays     *int
	ChurnThresholds       *[]ChurnThreshold
	DigestHour            *int
	Availability          *ScheduleAvailability
	PlanningBusinessRules *PlanningBusinessRulesPatch
}

// DefaultSettings 返回无存储行时的纯默认值（entry 级 churn 默认）。
func DefaultSettings() Settings {
	return Settings{
		Timezone:                      DefaultTimezone,
		BirthdayLeadDays:              DefaultBirthdayLeadDays,
		FollowUpAfterDays:             DefaultFollowUpAfterDays,
		ChurnThresholds:               defaultChurnThresholds(),
		DigestHour:                    DefaultDigestHour,
		TelegramBindingRevision:       1,
		Availability:                  DefaultScheduleAvailability(),
		PlanningBusinessRuleOverrides: make(business.RuleOverrides),
	}
}

func defaultChurnThresholds() []ChurnThreshold {
	return []ChurnThreshold{
		{ShootType: ShootTypePortrait, Days: DefaultChurnDays},
		{ShootType: ShootTypeCosplay, Days: DefaultChurnDays},
		{ShootType: ShootTypeOther, Days: DefaultChurnDays},
	}
}

// ChurnDaysFor 按 shoot_type 取有效阈值；未知类型回落 other。
func (s Settings) ChurnDaysFor(shootType string) int {
	if shootType != ShootTypePortrait && shootType != ShootTypeCosplay && shootType != ShootTypeOther {
		shootType = ShootTypeOther
	}
	for _, entry := range s.ChurnThresholds {
		if entry.ShootType == shootType {
			return entry.Days
		}
	}
	return DefaultChurnDays
}
