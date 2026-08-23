package settings

import (
	"errors"
	"reflect"
	"testing"
)

func TestDefaultSettingsIncludesCompleteAvailability(t *testing.T) {
	got := DefaultSettings().Availability
	want := ScheduleAvailability{
		Weekly: ScheduleAvailabilityWeekly{
			Monday:    availabilityWindow("10:00", "19:00"),
			Tuesday:   availabilityWindow("10:00", "19:00"),
			Wednesday: availabilityWindow("10:00", "19:00"),
			Thursday:  availabilityWindow("10:00", "19:00"),
			Friday:    availabilityWindow("10:00", "19:00"),
			Saturday:  availabilityWindow("09:00", "20:00"),
			Sunday:    availabilityWindow("09:00", "20:00"),
		},
		MinOpeningMinutes: 120,
		TurnaroundMinutes: 60,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("default availability = %#v, want %#v", got, want)
	}
}

func TestServicePatchReplacesWholeAvailability(t *testing.T) {
	availability := DefaultScheduleAvailability()
	availability.Weekly.Monday = availabilityWindow("08:30", "17:30")
	availability.Weekly.Sunday = nil
	availability.MinOpeningMinutes = 90
	availability.TurnaroundMinutes = 30

	got, _, err := applyPatch(DefaultSettings(), PatchInput{Availability: &availability})
	if err != nil {
		t.Fatalf("Patch() error = %v", err)
	}
	if !reflect.DeepEqual(got.Availability, availability) {
		t.Fatalf("saved availability = %#v, want %#v", got.Availability, availability)
	}
}

func TestServicePatchDeliverySLADays(t *testing.T) {
	days := func(value int) *int { return &value }

	for _, valid := range []int{1, 30, 180} {
		got, _, err := applyPatch(DefaultSettings(), PatchInput{DeliverySLADays: days(valid)})
		if err != nil {
			t.Fatalf("Patch(%d) error = %v", valid, err)
		}
		if got.DeliverySLADays != valid {
			t.Fatalf("saved delivery_sla_days = %d, want %d", got.DeliverySLADays, valid)
		}
	}
	for _, invalid := range []int{0, -1, 181} {
		if _, _, err := applyPatch(DefaultSettings(), PatchInput{DeliverySLADays: days(invalid)}); !errors.Is(err, ErrValidation) {
			t.Fatalf("Patch(%d) error = %v, want validation error", invalid, err)
		}
	}
}

func TestServicePatchRejectsInvalidAvailabilityWithoutWriting(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ScheduleAvailability)
	}{
		{name: "bad start", mutate: func(value *ScheduleAvailability) {
			value.Weekly.Monday.Start = "8:00"
		}},
		{name: "end not after start", mutate: func(value *ScheduleAvailability) {
			value.Weekly.Monday.End = value.Weekly.Monday.Start
		}},
		{name: "minimum opening below range", mutate: func(value *ScheduleAvailability) {
			value.MinOpeningMinutes = 14
		}},
		{name: "minimum opening above range", mutate: func(value *ScheduleAvailability) {
			value.MinOpeningMinutes = 481
		}},
		{name: "turnaround below range", mutate: func(value *ScheduleAvailability) {
			value.TurnaroundMinutes = -1
		}},
		{name: "turnaround above range", mutate: func(value *ScheduleAvailability) {
			value.TurnaroundMinutes = 241
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			availability := DefaultScheduleAvailability()
			tt.mutate(&availability)
			_, _, err := applyPatch(DefaultSettings(), PatchInput{Availability: &availability})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Patch() error = %v, want validation error", err)
			}
		})
	}
}

func availabilityWindow(start, end string) *ScheduleAvailabilityWindow {
	return &ScheduleAvailabilityWindow{Start: start, End: end}
}
