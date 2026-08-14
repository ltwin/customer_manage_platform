// Package reminder 实现提醒引擎：扫描生成、列表、custom、done/dismiss、每日调度。
package reminder

import (
	"errors"
	"time"
)

const (
	TypeBirthday                = "birthday"
	TypeFollowUp                = "follow_up"
	TypeChurn                   = "churn"
	TypeCustom                  = "custom"
	TypePlanAssignmentChecklist = "plan_assignment_checklist"

	StatusPending   = "pending"
	StatusDone      = "done"
	StatusDismissed = "dismissed"
)

var (
	ErrValidation = errors.New("validation_failed")
	ErrNotFound   = errors.New("not_found")
)

// ValidationError 是可回传 HTTP 层的校验失败。
type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string { return e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }

// Reminder 是提醒资源。
type Reminder struct {
	ID         string
	AccountID  string
	CreatedAt  time.Time
	Type       string
	CustomerID *string
	OrderID    *string
	PlanID     *string   // plan_assignment_checklist 只读引用；legacy 类型恒为空
	DueDate    time.Time // date-only，UTC 午夜存储语义
	Content    string
	Status     string
	DedupKey   string
}

// ListFilter 是 GET /reminders 过滤。
type ListFilter struct {
	Status     string
	CustomerID string
	DueBefore  *time.Time // date-only
	Page       int
	PageSize   int
}

// ListResult 是分页列表。
type ListResult struct {
	Items []Reminder
	Total int64
}

// CreateCustomInput 是手工创建 custom 提醒的输入。
type CreateCustomInput struct {
	CustomerID *string
	DueDate    time.Time
	Content    string
}

// ScanResult 是扫描计数。
type ScanResult struct {
	Created       int
	Skipped       int
	AutoDismissed int
}

// SettingsView 是扫描所需的设置只读投影，避免 reminder 依赖 settings 包循环。
type SettingsView struct {
	Timezone          string
	BirthdayLeadDays  int
	FollowUpAfterDays int
	ChurnDaysFor      func(shootType string) int
}
