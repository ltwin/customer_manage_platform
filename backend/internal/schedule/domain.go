package schedule

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	TypeShoot = "shoot"
	TypeHold  = "hold"
	TypeBusy  = "busy"
)

var ErrValidation = errors.New("validation_failed")

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}

type Slot struct {
	ID        string
	AccountID string
	CreatedAt time.Time
	StartAt   time.Time
	EndAt     time.Time
	Type      string
	OrderID   *string
	Note      *string
}

type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (fn ClockFunc) Now() time.Time {
	return fn()
}

func ValidateSlot(slot Slot) error {
	if slot.StartAt.IsZero() || slot.EndAt.IsZero() || !slot.EndAt.After(slot.StartAt) {
		return ValidationError{Message: "end_at 必须晚于 start_at"}
	}
	slot.Type = strings.TrimSpace(slot.Type)
	if slot.Type != TypeShoot && slot.Type != TypeHold && slot.Type != TypeBusy {
		return ValidationError{Message: "type 非法"}
	}
	if slot.Type == TypeShoot && (slot.OrderID == nil || strings.TrimSpace(*slot.OrderID) == "") {
		return ValidationError{Message: "shoot 档期必须关联订单"}
	}
	if slot.Type != TypeShoot && slot.OrderID != nil {
		return ValidationError{Message: "hold/busy 档期不可关联订单"}
	}
	if slot.Note != nil && utf8.RuneCountInString(*slot.Note) > 500 {
		return ValidationError{Message: "note 不能超过 500 字"}
	}
	return nil
}

func ApplyUpdate(current Slot, input UpdateInput) (Slot, error) {
	next := current
	if input.StartAt != nil {
		next.StartAt = input.StartAt.UTC()
	}
	if input.EndAt != nil {
		next.EndAt = input.EndAt.UTC()
	}
	if input.Type != nil {
		next.Type = *input.Type
	}
	if input.OrderID.IsSpecified() {
		if input.OrderID.IsNull() {
			next.OrderID = nil
		} else {
			value := input.OrderID.MustGet()
			next.OrderID = &value
		}
	}
	if input.Note.IsSpecified() {
		if input.Note.IsNull() {
			next.Note = nil
		} else {
			value := input.Note.MustGet()
			next.Note = &value
		}
	}
	if err := ValidateSlot(next); err != nil {
		return Slot{}, err
	}
	return next, nil
}

func Overlaps(a, b Slot) bool {
	if a.ID != "" && a.ID == b.ID {
		return false
	}
	return a.StartAt.Before(b.EndAt) && b.StartAt.Before(a.EndAt)
}

func FindOrderScheduleConflict(existing []Slot, candidate Slot) *Slot {
	if candidate.Type != TypeShoot || candidate.OrderID == nil {
		return nil
	}
	for i := range existing {
		current := &existing[i]
		if current.ID == candidate.ID || current.Type != TypeShoot || current.OrderID == nil {
			continue
		}
		if *current.OrderID == *candidate.OrderID {
			return current
		}
	}
	return nil
}

func NeedsExternalReferenceValidation(current, next Slot) bool {
	return !current.StartAt.Equal(next.StartAt) ||
		!current.EndAt.Equal(next.EndAt) ||
		current.Type != next.Type ||
		!sameOptionalString(current.OrderID, next.OrderID)
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
