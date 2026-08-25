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
	DefaultDeliverySLADays   = 14

	DefaultHealthSleepingRatio       = 1.2
	DefaultHealthAtRiskRatio         = 2
	DefaultHealthLostRatio           = 3.5
	DefaultHealthFallbackCadenceDays = 120

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

// HealthTiers 是客户健康度分层参数组（roadmap §4.2）：ratio 阈值三档递增 +
// 仅 1 次拍摄客户的通用节奏基线。仅供 dashboard v2 customer_health 块消费，
// 与 reminder churn_thresholds 相互独立（churn=行动提醒固定天数，健康度=资产
// 盘点个人节奏倍数，owner 2026-08-24 拍板两套口径分离 + 前端交叉引用）。
type HealthTiers struct {
	SleepingRatio       float64 `json:"sleeping_ratio"`
	AtRiskRatio         float64 `json:"at_risk_ratio"`
	LostRatio           float64 `json:"lost_ratio"`
	FallbackCadenceDays int     `json:"fallback_cadence_days"`
}

// Settings 是账号级有效设置（读取时已叠加默认值）。
type Settings struct {
	Timezone                      string
	BirthdayLeadDays              int
	FollowUpAfterDays             int
	ChurnThresholds               []ChurnThreshold
	DigestHour                    int
	DeliverySLADays               int
	HealthTiers                   HealthTiers
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
	DeliverySLADays       *int
	HealthTiers           *HealthTiers
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
		DeliverySLADays:               DefaultDeliverySLADays,
		HealthTiers:                   DefaultHealthTiers(),
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

// DefaultHealthTiers 返回健康度分层默认参数（原型口径：1.2/2/3.5 倍 + 120 天基线）。
func DefaultHealthTiers() HealthTiers {
	return HealthTiers{
		SleepingRatio:       DefaultHealthSleepingRatio,
		AtRiskRatio:         DefaultHealthAtRiskRatio,
		LostRatio:           DefaultHealthLostRatio,
		FallbackCadenceDays: DefaultHealthFallbackCadenceDays,
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
