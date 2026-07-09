package order

import (
	"errors"
	"time"

	"github.com/oapi-codegen/nullable"
)

const (
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
}

type CreateInput struct {
	CustomerID  string
	PackageID   *string
	Title       *string
	Price       *int
	Status      *string
	DepositPaid *bool
	BalancePaid *bool
	ShotAt      *time.Time
	DeliveredAt *time.Time
	Note        *string
}

type UpdateInput struct {
	Status      *string
	DepositPaid *bool
	BalancePaid *bool
	ShotAt      nullable.Nullable[time.Time]
	DeliveredAt nullable.Nullable[time.Time]
	Title       *string
	Price       nullable.Nullable[int]
	Note        *string
}

type ListFilter struct {
	CustomerID    string
	Status        string
	UnpaidBalance bool
	Page          int
	PageSize      int
}

type ListItem struct {
	Order
	CustomerDisplayName string
	PackageName         *string
}

type ListResult struct {
	Items []ListItem
	Total int64
}
