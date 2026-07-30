package schedule

import (
	"strings"
	"testing"
	"time"
)

func TestOverlapsUsesHalfOpenIntervalsAndExcludesSelf(t *testing.T) {
	base := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		a    Slot
		b    Slot
		want bool
	}{
		{name: "touching endpoint", a: Slot{StartAt: base, EndAt: base.Add(time.Hour)}, b: Slot{StartAt: base.Add(time.Hour), EndAt: base.Add(2 * time.Hour)}},
		{name: "partial overlap", a: Slot{StartAt: base, EndAt: base.Add(2 * time.Hour)}, b: Slot{StartAt: base.Add(time.Hour), EndAt: base.Add(3 * time.Hour)}, want: true},
		{name: "containment", a: Slot{StartAt: base, EndAt: base.Add(3 * time.Hour)}, b: Slot{StartAt: base.Add(time.Hour), EndAt: base.Add(2 * time.Hour)}, want: true},
		{name: "cross type", a: Slot{Type: TypeHold, StartAt: base, EndAt: base.Add(2 * time.Hour)}, b: Slot{Type: TypeBusy, StartAt: base.Add(time.Hour), EndAt: base.Add(3 * time.Hour)}, want: true},
		{name: "same persisted slot", a: Slot{ID: "slot-1", StartAt: base, EndAt: base.Add(2 * time.Hour)}, b: Slot{ID: "slot-1", StartAt: base.Add(time.Hour), EndAt: base.Add(3 * time.Hour)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Overlaps(tc.a, tc.b); got != tc.want {
				t.Fatalf("Overlaps() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateSlotAndOrderUniqueness(t *testing.T) {
	start := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	orderID := "ord-1"

	if err := ValidateSlot(Slot{Type: TypeShoot, StartAt: start, EndAt: end, OrderID: &orderID}); err != nil {
		t.Fatalf("valid shoot: %v", err)
	}
	if err := ValidateSlot(Slot{Type: TypeShoot, StartAt: start, EndAt: end}); err == nil {
		t.Fatal("shoot without order must fail")
	}
	if err := ValidateSlot(Slot{Type: TypeHold, StartAt: start, EndAt: end, OrderID: &orderID}); err == nil {
		t.Fatal("non-shoot with order must fail")
	}
	if err := ValidateSlot(Slot{Type: TypeBusy, StartAt: start, EndAt: start}); err == nil {
		t.Fatal("end equal start must fail")
	}
	longNote := strings.Repeat("档", 501)
	if err := ValidateSlot(Slot{Type: TypeBusy, StartAt: start, EndAt: end, Note: &longNote}); err == nil {
		t.Fatal("note over 500 runes must fail")
	}

	existing := []Slot{
		{ID: "slot-1", Type: TypeShoot, OrderID: &orderID, StartAt: start, EndAt: end},
		{ID: "slot-2", Type: TypeHold, StartAt: start, EndAt: end},
	}
	if conflict := FindOrderScheduleConflict(existing, Slot{ID: "slot-new", Type: TypeShoot, OrderID: &orderID}); conflict == nil || conflict.ID != "slot-1" {
		t.Fatalf("expected order conflict, got %+v", conflict)
	}
	if conflict := FindOrderScheduleConflict(existing, existing[0]); conflict != nil {
		t.Fatalf("PATCH self must not conflict, got %+v", conflict)
	}
}

func TestClockFuncProvidesDeterministicNow(t *testing.T) {
	want := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	clock := ClockFunc(func() time.Time { return want })
	if got := clock.Now(); !got.Equal(want) {
		t.Fatalf("clock.Now() = %s, want %s", got, want)
	}
}

func TestNeedsExternalReferenceValidationIgnoresNoteOnlyUpdate(t *testing.T) {
	start := time.Date(2026, 7, 10, 10, 0, 0, 0, time.UTC)
	orderA := "ord-a"
	base := Slot{Type: TypeShoot, OrderID: &orderA, StartAt: start, EndAt: start.Add(time.Hour)}
	note := "修正备注"
	noteOnly := base
	noteOnly.Note = &note
	if NeedsExternalReferenceValidation(base, noteOnly) {
		t.Fatal("note-only update must not revalidate external order/customer state")
	}

	orderB := "ord-b"
	tests := []Slot{
		{Type: TypeShoot, OrderID: &orderA, StartAt: start.Add(time.Minute), EndAt: base.EndAt},
		{Type: TypeShoot, OrderID: &orderA, StartAt: base.StartAt, EndAt: base.EndAt.Add(time.Minute)},
		{Type: TypeHold, StartAt: base.StartAt, EndAt: base.EndAt},
		{Type: TypeShoot, OrderID: &orderB, StartAt: base.StartAt, EndAt: base.EndAt},
	}
	for i, next := range tests {
		if !NeedsExternalReferenceValidation(base, next) {
			t.Fatalf("case %d must revalidate final entity", i)
		}
	}
}
