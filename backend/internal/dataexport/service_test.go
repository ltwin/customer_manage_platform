package dataexport

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fakeRepository struct {
	snapshot Snapshot
	err      error
}

func (r fakeRepository) LoadSnapshot(context.Context, store.AccountScope) (Snapshot, error) {
	return r.snapshot, r.err
}

type fakeClock struct{ now time.Time }

func (c fakeClock) Now() time.Time { return c.now }

type recordingRepository struct {
	events *[]string
}

func (r recordingRepository) LoadSnapshot(context.Context, store.AccountScope) (Snapshot, error) {
	*r.events = append(*r.events, "repository")
	return EmptySnapshot(), nil
}

type recordingClock struct {
	events *[]string
	now    time.Time
	calls  *int
}

func (c recordingClock) Now() time.Time {
	*c.events = append(*c.events, "clock")
	(*c.calls)++
	return c.now
}

func TestServiceBuildsVersionedDocumentWithLengthDerivedCounts(t *testing.T) {
	now := time.Date(2026, time.July, 21, 8, 30, 15, 0, time.FixedZone("CST", 8*60*60))
	svc := NewService(fakeRepository{snapshot: EmptySnapshot()}, fakeClock{now: now})

	doc, err := svc.Build(context.Background(), store.AccountScope{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if doc.SchemaVersion != 1 {
		t.Fatalf("schema version = %d, want 1", doc.SchemaVersion)
	}
	if !doc.ExportedAt.Equal(now.UTC()) {
		t.Fatalf("exported at = %s, want %s", doc.ExportedAt, now.UTC())
	}
	if doc.Counts != (Counts{}) {
		t.Fatalf("counts = %+v, want all zero", doc.Counts)
	}
	if doc.Customers == nil || doc.SocialIdentities == nil || doc.CustomerNotes == nil ||
		doc.Packages == nil || doc.Orders == nil || doc.ScheduleSlots == nil || doc.Reminders == nil {
		t.Fatal("all exported collections must be non-nil")
	}
}

func TestServiceCapturesExportTimeBeforeLoadingSnapshot(t *testing.T) {
	now := time.Date(2026, time.July, 21, 8, 30, 15, 0, time.FixedZone("CST", 8*60*60))
	events := make([]string, 0, 2)
	clockCalls := 0
	svc := NewService(
		recordingRepository{events: &events},
		recordingClock{events: &events, now: now, calls: &clockCalls},
	)

	doc, err := svc.Build(context.Background(), store.AccountScope{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got, want := strings.Join(events, ","), "clock,repository"; got != want {
		t.Fatalf("Build() event order = %q, want %q", got, want)
	}
	if clockCalls != 1 {
		t.Fatalf("Clock.Now() calls = %d, want 1", clockCalls)
	}
	if !doc.ExportedAt.Equal(now.UTC()) {
		t.Fatalf("exported at = %s, want %s", doc.ExportedAt, now.UTC())
	}
}

func TestServiceRejectsMissingDependencies(t *testing.T) {
	tests := []struct {
		name string
		svc  *Service
	}{
		{name: "nil service", svc: nil},
		{name: "repository", svc: NewService(nil, fakeClock{now: time.Now()})},
		{name: "clock", svc: NewService(fakeRepository{}, nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.svc.Build(context.Background(), store.AccountScope{}); err == nil {
				t.Fatal("Build() error = nil, want explicit dependency error")
			}
		})
	}
}

func TestPostgresRepositoryRejectsEmptyAccountScope(t *testing.T) {
	if _, err := NewPostgresRepository().LoadSnapshot(context.Background(), store.AccountScope{}); !errors.Is(err, store.ErrEmptyAccountScope) {
		t.Fatalf("LoadSnapshot(empty scope) error = %v, want ErrEmptyAccountScope", err)
	}
}
