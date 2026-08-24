package order

import (
	"errors"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

const (
	CreationModeNew      = "new"
	CreationModeBackfill = "backfill"

	StatusConsulting = "consulting"
	StatusScheduled  = "scheduled"
	StatusShot       = "shot"
	StatusSelected   = "selected"
	StatusRetouching = "retouching"
	StatusDelivered  = "delivered"
	StatusClosed     = "closed"
	StatusCancelled  = "cancelled"
)

var (
	ErrValidation              = errors.New("validation_failed")
	ErrNotFound                = errors.New("not_found")
	ErrCustomerArchived        = errors.New("customer_archived")
	ErrInvalidStatusTransition = errors.New("invalid_status_transition")
	ErrUnpaidBalance           = errors.New("unpaid_balance")
	ErrOrderNotTerminal        = errors.New("order_not_terminal")
	ErrOrderInUse              = errors.New("order_in_use")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}

type Order struct {
	ID          string
	AccountID   string
	CreatedAt   time.Time
	CustomerID  string
	PackageID   *string
	Title       *string
	Status      string
	Price       *int
	DepositPaid bool
	BalancePaid bool
	ShotAt      *time.Time
	DeliveredAt *time.Time
	Note        *string
	// DeliveryDueAt 是 date-only 的应交付日（UTC 午夜表示），非时刻。
	DeliveryDueAt *time.Time
	// DeliveryDueIsOverride 为 true 时该应交付日是订单级覆盖，不随 shot_at 变更重算。
	DeliveryDueIsOverride bool
	// AmountPaid 是已收现金（分），恒有值；OutstandingAmount 是待收余额（分），
	// NULL 表示 price 未定价、不计入待收合计；PaidAt 是收款时刻（NULL=未知，含存量 backfill）。
	AmountPaid        int
	OutstandingAmount *int
	PaidAt            *time.Time
}

type OrderInUseError struct {
	SlotID  string
	StartAt time.Time
}

func (e OrderInUseError) Error() string {
	return "订单仍被拍摄档期引用"
}

func (e OrderInUseError) Unwrap() error {
	return ErrOrderInUse
}

type CreateInput struct {
	CreationMode  string
	CustomerID    string
	PackageID     *string
	Title         *string
	Price         *int
	Status        *string
	DepositPaid   *bool
	BalancePaid   *bool
	ShotAt        *time.Time
	DeliveredAt   *time.Time
	Note          *string
	DeliveryDueAt *time.Time
	AmountPaid    *int
	// OutstandingAmount 与 PaidAt 为支付事实录入；显式值优先于 DEC-10 推定。
	OutstandingAmount *int
	PaidAt            *time.Time

	// deliveryPolicy 由 service 从账号 settings 解析后填入（与 ListFilter.schedulableNow 同惯例）。
	deliveryPolicy DeliveryPolicy
}

type PreparedCreate struct {
	Input   CreateInput
	initial Order
}

type UpdateInput struct {
	Status        *string
	DepositPaid   *bool
	BalancePaid   *bool
	ShotAt        nullable.Nullable[time.Time]
	DeliveredAt   nullable.Nullable[time.Time]
	Title         *string
	Price         nullable.Nullable[int]
	Note          *string
	DeliveryDueAt nullable.Nullable[time.Time]
	AmountPaid    nullable.Nullable[int]
	// OutstandingAmount 与 PaidAt 三态仅用于区分「未传」与「显式 null」；
	// 金额字段无 null 语义，显式 null 一律 400。
	OutstandingAmount nullable.Nullable[int]
	PaidAt            nullable.Nullable[time.Time]

	// deliveryPolicy 由 service 从账号 settings 解析后填入。
	deliveryPolicy DeliveryPolicy
}

type ListFilter struct {
	ID             string
	CustomerID     string
	Status         string
	UnpaidBalance  bool
	SchedulableAt  *time.Time
	Page           int
	PageSize       int
	schedulableNow time.Time
}

type ListItem struct {
	Order
	CustomerDisplayName string
	PackageName         *string
	PlanningSummary     *crm.Summary
}

type ListResult struct {
	Items []ListItem
	Total int64
}
