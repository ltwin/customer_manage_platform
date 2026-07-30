package digest

import (
	"context"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

type Window struct {
	LocalDate time.Time
	Timezone  string
	Start     time.Time
	End       time.Time
}

func WindowFor(target LocalTarget) (Window, error) {
	location, err := time.LoadLocation(target.Timezone)
	if err != nil {
		return Window{}, fmt.Errorf("load digest timezone: %w", err)
	}
	year, month, day := target.LocalDate.Date()
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	return Window{
		LocalDate: time.Date(year, month, day, 0, 0, 0, 0, time.UTC),
		Timezone:  target.Timezone,
		Start:     start,
		End:       start.AddDate(0, 0, 1),
	}, nil
}

type ReminderItem struct {
	ID      string
	DueDate time.Time
	Content string
}

type ReminderDigest struct {
	Items []ReminderItem
	Total int
}

type ShootSlot struct {
	ID           string
	StartAt      time.Time
	EndAt        time.Time
	CustomerName string
	PackageName  string
}

type DigestSnapshot struct {
	LocalDate       time.Time
	Timezone        string
	Reminders       ReminderDigest
	TodayShootSlots []ShootSlot
	UnpaidCount     int
}

type SnapshotRepository interface {
	Load(context.Context, store.AccountScope, Window, int) (DigestSnapshot, error)
}

type PostgresSnapshotRepository struct{}

func NewPostgresSnapshotRepository() PostgresSnapshotRepository { return PostgresSnapshotRepository{} }

func (PostgresSnapshotRepository) Load(
	ctx context.Context,
	scope store.AccountScope,
	window Window,
	reminderLimit int,
) (DigestSnapshot, error) {
	reminders, err := loadDigestReminders(ctx, scope, window.LocalDate, reminderLimit)
	if err != nil {
		return DigestSnapshot{}, err
	}
	items, err := scheduledomain.AssembleListItems(ctx, scope, scheduledomain.ListFilter{From: window.Start, To: window.End})
	if err != nil {
		return DigestSnapshot{}, err
	}
	slots := make([]ShootSlot, 0, len(items))
	for _, item := range items {
		if item.Type != scheduledomain.TypeShoot {
			continue
		}
		packageName := ""
		if item.PackageName != nil {
			packageName = *item.PackageName
		}
		slots = append(slots, ShootSlot{
			ID: item.ID, StartAt: item.StartAt, EndAt: item.EndAt,
			CustomerName: item.CustomerDisplayName, PackageName: packageName,
		})
	}
	unpaid, err := scope.Count(ctx, "orders", "status = $2 AND balance_paid = false", "delivered")
	if err != nil {
		return DigestSnapshot{}, err
	}
	return DigestSnapshot{
		LocalDate: window.LocalDate, Timezone: window.Timezone,
		Reminders: reminders, TodayShootSlots: slots, UnpaidCount: int(unpaid),
	}, nil
}

func loadDigestReminders(
	ctx context.Context,
	scope store.AccountScope,
	localDate time.Time,
	limit int,
) (ReminderDigest, error) {
	if limit < 1 {
		return ReminderDigest{}, errorsNewReminderLimit()
	}
	date := reminderdomain.FormatDate(localDate)
	total, err := scope.Count(ctx, "reminders", "status = $2 AND due_date <= $3", reminderdomain.StatusPending, date)
	if err != nil {
		return ReminderDigest{}, err
	}
	rows, err := scope.QueryPage(ctx, "reminders", "id,due_date,content",
		"status = $2 AND due_date <= $3",
		[]store.OrderBy{{Column: "due_date"}, {Column: "id"}}, limit, 0,
		reminderdomain.StatusPending, date)
	if err != nil {
		return ReminderDigest{}, err
	}
	defer rows.Close()
	items := make([]ReminderItem, 0, min(limit, int(total)))
	for rows.Next() {
		var item ReminderItem
		if err := rows.Scan(&item.ID, &item.DueDate, &item.Content); err != nil {
			return ReminderDigest{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ReminderDigest{}, err
	}
	return ReminderDigest{Items: items, Total: int(total)}, nil
}

func errorsNewReminderLimit() error { return fmt.Errorf("reminder limit must be positive") }
