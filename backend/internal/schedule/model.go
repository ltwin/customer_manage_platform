package schedule

import (
	"errors"
	"fmt"
	"time"

	"github.com/oapi-codegen/nullable"
)

var (
	ErrNotFound              = errors.New("not_found")
	ErrOrderAlreadyScheduled = errors.New("order_already_scheduled")
	ErrCustomerArchived      = errors.New("customer_archived")
	ErrCustomerChanged       = errors.New("customer_changed")
)

type OrderAlreadyScheduledError struct {
	SlotID  string
	StartAt time.Time
}

func (e OrderAlreadyScheduledError) Error() string {
	return fmt.Sprintf("订单已有 shoot 档期 %s", e.SlotID)
}

func (e OrderAlreadyScheduledError) Unwrap() error {
	return ErrOrderAlreadyScheduled
}

type CreateInput struct {
	StartAt time.Time
	EndAt   time.Time
	Type    string
	OrderID *string
	Note    *string
}

type PreparedCreate struct {
	Input CreateInput
	slot  Slot
}

type CreateResult struct {
	Slot     Slot     `json:"slot"`
	Overlaps []string `json:"overlaps"`
}

type UpdateInput struct {
	StartAt *time.Time
	EndAt   *time.Time
	Type    *string
	OrderID nullable.Nullable[string]
	Note    nullable.Nullable[string]
}

type ListFilter struct {
	From time.Time
	To   time.Time
}

type ListItem struct {
	Slot
	CustomerID          string
	CustomerDisplayName string
	CustomerStatus      string
	OrderStatus         string
	OrderTitle          *string
	PackageName         *string
}

type CustomerChangedError struct {
	CustomerID string
}

func (e CustomerChangedError) Error() string {
	return "订单客户已变更为 " + e.CustomerID
}
