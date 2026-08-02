// Package dataexport builds the account-scoped, reference-only JSON export read model.
package dataexport

import (
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// Snapshot is the complete account-scoped data observed within one read transaction.
type Snapshot struct {
	Customers        []customer.Customer
	SocialIdentities []customer.SocialIdentity
	CustomerNotes    []customer.CustomerNote
	Packages         []pkgcatalog.Package
	Orders           []order.Order
	ScheduleSlots    []schedule.Slot
	Reminders        []reminder.Reminder
	Settings         settings.Settings
	AccountProfile   AccountProfileExport
}

// AccountProfileAvatarExport 是导出用头像引用（无 bytes／object_id／url）。
type AccountProfileAvatarExport struct {
	Version   string
	MediaType string
	Size      int64
	UpdatedAt time.Time
}

// AccountProfileExport 是 schema v3 顶层 account_profile 投影。
type AccountProfileExport struct {
	DisplayName     *string
	ProfileRevision int64
	AvatarRevision  int64
	Avatar          *AccountProfileAvatarExport
	UpdatedAt       *time.Time
}

// VirtualAccountProfile 返回无行时的虚拟默认投影。
func VirtualAccountProfile() AccountProfileExport {
	return AccountProfileExport{ProfileRevision: 0, AvatarRevision: 0}
}

// EmptySnapshot returns non-nil collections and effective default settings.
func EmptySnapshot() Snapshot {
	return Snapshot{
		Customers:        make([]customer.Customer, 0),
		SocialIdentities: make([]customer.SocialIdentity, 0),
		CustomerNotes:    make([]customer.CustomerNote, 0),
		Packages:         make([]pkgcatalog.Package, 0),
		Orders:           make([]order.Order, 0),
		ScheduleSlots:    make([]schedule.Slot, 0),
		Reminders:        make([]reminder.Reminder, 0),
		Settings:         settings.DefaultSettings(),
		AccountProfile:   VirtualAccountProfile(),
	}
}

// Counts mirrors the seven exported collections and is derived from their final lengths.
type Counts struct {
	Customers        int
	SocialIdentities int
	CustomerNotes    int
	Packages         int
	Orders           int
	ScheduleSlots    int
	Reminders        int
}

// Document is the application-layer export result before OpenAPI projection.
type Document struct {
	ExportedAt       time.Time
	SchemaVersion    int
	Counts           Counts
	Customers        []customer.Customer
	SocialIdentities []customer.SocialIdentity
	CustomerNotes    []customer.CustomerNote
	Packages         []pkgcatalog.Package
	Orders           []order.Order
	ScheduleSlots    []schedule.Slot
	Reminders        []reminder.Reminder
	Settings         settings.Settings
	AccountProfile   AccountProfileExport
}
