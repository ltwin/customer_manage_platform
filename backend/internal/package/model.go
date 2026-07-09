// Package pkgcatalog implements the package-catalog domain. The directory is
// named "package" for the domain term; the Go package name avoids the keyword.
package pkgcatalog

import (
	"errors"
	"time"

	"github.com/oapi-codegen/nullable"
)

const (
	ShootTypePortrait = "portrait"
	ShootTypeCosplay  = "cosplay"
	ShootTypeOther    = "other"

	PricingModePerDuration = "per_duration"
	PricingModePerPhoto    = "per_photo"
	PricingModeFixed       = "fixed"

	StatusActive   = "active"
	StatusArchived = "archived"
	StatusAll      = "all"
)

var (
	ErrValidation   = errors.New("validation_failed")
	ErrNotFound     = errors.New("not_found")
	ErrPackageInUse = errors.New("package_in_use")
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

type Package struct {
	ID               string
	AccountID        string
	CreatedAt        time.Time
	Name             string
	ShootType        string
	PricingMode      string
	BasePrice        int
	DurationMinutes  *int
	ShotCountMin     *int
	ShotCountMax     *int
	RawDeliveryCount *int
	RetouchCount     *int
	Note             *string
	Status           string
}

type CreateInput struct {
	Name             string
	ShootType        string
	PricingMode      string
	BasePrice        int
	DurationMinutes  *int
	ShotCountMin     *int
	ShotCountMax     *int
	RawDeliveryCount *int
	RetouchCount     *int
	Note             *string
}

// UpdateInput mirrors PATCH /packages/{id}: pointer fields mean
// "not sent/sent value"; nullable ints mean "not sent/sent value/sent null".
type UpdateInput struct {
	Name             *string
	ShootType        *string
	PricingMode      *string
	BasePrice        *int
	DurationMinutes  nullable.Nullable[int]
	ShotCountMin     nullable.Nullable[int]
	ShotCountMax     nullable.Nullable[int]
	RawDeliveryCount nullable.Nullable[int]
	RetouchCount     nullable.Nullable[int]
	Note             *string
	Status           *string
}

type ListFilter struct {
	Status   string
	Page     int
	PageSize int
}

type ListItem struct {
	Package
	OrdersCount int
}

type ListResult struct {
	Items []ListItem
	Total int64
}
