package dataexport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Repository loads every allowlisted export collection from one account snapshot.
type Repository interface {
	LoadSnapshot(context.Context, store.AccountScope) (Snapshot, error)
}

// Clock supplies the single instant shared by exported_at and the attachment filename.
type Clock interface {
	Now() time.Time
}

type ClockFunc func() time.Time

func (fn ClockFunc) Now() time.Time { return fn() }

// Service constructs a versioned export document without depending on HTTP.
type Service struct {
	repo  Repository
	clock Clock
}

func NewService(repo Repository, clock Clock) *Service {
	return &Service{repo: repo, clock: clock}
}

func (s *Service) Build(ctx context.Context, scope store.AccountScope) (Document, error) {
	if s == nil || s.repo == nil {
		return Document{}, errors.New("data export repository is required")
	}
	if s.clock == nil {
		return Document{}, errors.New("data export clock is required")
	}
	exportedAt := s.clock.Now().UTC()
	snapshot, err := s.repo.LoadSnapshot(ctx, scope)
	if err != nil {
		return Document{}, fmt.Errorf("load data export snapshot: %w", err)
	}
	normalizeSnapshot(&snapshot)
	return Document{
		ExportedAt:       exportedAt,
		SchemaVersion:    4,
		Counts:           countsFor(snapshot),
		Customers:        snapshot.Customers,
		SocialIdentities: snapshot.SocialIdentities,
		CustomerNotes:    snapshot.CustomerNotes,
		Packages:         snapshot.Packages,
		Orders:           snapshot.Orders,
		ScheduleSlots:    snapshot.ScheduleSlots,
		Reminders:        snapshot.Reminders,
		Settings:         snapshot.Settings,
		AccountProfile:   snapshot.AccountProfile,
	}, nil
}

func normalizeSnapshot(snapshot *Snapshot) {
	empty := EmptySnapshot()
	if snapshot.Customers == nil {
		snapshot.Customers = empty.Customers
	}
	if snapshot.SocialIdentities == nil {
		snapshot.SocialIdentities = empty.SocialIdentities
	}
	if snapshot.CustomerNotes == nil {
		snapshot.CustomerNotes = empty.CustomerNotes
	}
	if snapshot.Packages == nil {
		snapshot.Packages = empty.Packages
	}
	if snapshot.Orders == nil {
		snapshot.Orders = empty.Orders
	}
	if snapshot.ScheduleSlots == nil {
		snapshot.ScheduleSlots = empty.ScheduleSlots
	}
	if snapshot.Reminders == nil {
		snapshot.Reminders = empty.Reminders
	}
}

func countsFor(snapshot Snapshot) Counts {
	return Counts{
		Customers:        len(snapshot.Customers),
		SocialIdentities: len(snapshot.SocialIdentities),
		CustomerNotes:    len(snapshot.CustomerNotes),
		Packages:         len(snapshot.Packages),
		Orders:           len(snapshot.Orders),
		ScheduleSlots:    len(snapshot.ScheduleSlots),
		Reminders:        len(snapshot.Reminders),
	}
}
