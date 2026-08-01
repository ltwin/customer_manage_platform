package settings

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type settingsRepositoryStub struct {
	stored  Settings
	found   bool
	upserts int
}

func (r *settingsRepositoryStub) Get(context.Context, store.AccountScope) (Settings, bool, error) {
	return r.stored, r.found, nil
}

func (r *settingsRepositoryStub) Upsert(_ context.Context, _ store.AccountScope, value Settings) (Settings, error) {
	r.stored = value
	r.found = true
	r.upserts++
	return value, nil
}

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
	repo := &settingsRepositoryStub{}
	svc := NewService(repo)
	availability := DefaultScheduleAvailability()
	availability.Weekly.Monday = availabilityWindow("08:30", "17:30")
	availability.Weekly.Sunday = nil
	availability.MinOpeningMinutes = 90
	availability.TurnaroundMinutes = 30

	got, err := svc.Patch(context.Background(), store.AccountScope{}, PatchInput{Availability: &availability})
	if err != nil {
		t.Fatalf("Patch() error = %v", err)
	}
	if repo.upserts != 1 {
		t.Fatalf("upserts = %d, want 1", repo.upserts)
	}
	if !reflect.DeepEqual(got.Availability, availability) {
		t.Fatalf("saved availability = %#v, want %#v", got.Availability, availability)
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
			repo := &settingsRepositoryStub{}
			svc := NewService(repo)
			availability := DefaultScheduleAvailability()
			tt.mutate(&availability)
			_, err := svc.Patch(context.Background(), store.AccountScope{}, PatchInput{Availability: &availability})
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("Patch() error = %v, want validation error", err)
			}
			if repo.upserts != 0 {
				t.Fatalf("upserts = %d, want 0", repo.upserts)
			}
		})
	}
}

func availabilityWindow(start, end string) *ScheduleAvailabilityWindow {
	return &ScheduleAvailabilityWindow{Start: start, End: end}
}
