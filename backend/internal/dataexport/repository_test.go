package dataexport

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
)

func TestPostgresRepositoryLoadsCustomersInStableOrderAndDefaultsEmptySettings(t *testing.T) {
	ctx := context.Background()
	database := openDataExportStore(t)
	scope := createDataExportAccount(t, database, "acct-export")
	later := time.Date(2026, time.July, 20, 9, 0, 0, 0, time.UTC)
	earlier := later.Add(-time.Hour)
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "created_at", "display_name", "channel", "status"},
		"cus-later", later, "Later", "other", "archived"); err != nil {
		t.Fatalf("insert later customer: %v", err)
	}
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "created_at", "display_name", "channel", "status"},
		"cus-earlier", earlier, "Earlier", "other", "active"); err != nil {
		t.Fatalf("insert earlier customer: %v", err)
	}

	snapshot, err := NewPostgresRepository().LoadSnapshot(ctx, scope)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if len(snapshot.Customers) != 2 || snapshot.Customers[0].ID != "cus-earlier" || snapshot.Customers[1].ID != "cus-later" {
		t.Fatalf("customers are not in created_at/id order: %+v", snapshot.Customers)
	}
	if snapshot.SocialIdentities == nil || snapshot.CustomerNotes == nil || snapshot.Packages == nil ||
		snapshot.Orders == nil || snapshot.ScheduleSlots == nil || snapshot.Reminders == nil {
		t.Fatal("empty collections must be non-nil")
	}
	if got, want := canonicalSettingsTime(snapshot.Settings), canonicalSettingsTime(settings.DefaultSettings()); !reflect.DeepEqual(got, want) {
		t.Fatalf("empty settings row differs from complete effective defaults: got=%+v want=%+v", got, want)
	}
}

func TestPostgresRepositoryLoadsEveryEntityAndTerminalStatus(t *testing.T) {
	ctx := context.Background()
	database := openDataExportStore(t)
	scope := createDataExportAccount(t, database, "acct-full")
	createdAt := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)

	if err := scope.Insert(ctx, "customers",
		[]string{"id", "created_at", "display_name", "real_name", "phone", "birthday", "channel", "status"},
		"cus-a", createdAt, "Fixture cus-a", "Legal Name", "13000000000", "03-15", "other", "active"); err != nil {
		t.Fatalf("insert customer cus-a: %v", err)
	}
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "created_at", "display_name", "real_name", "channel", "status", "merged_into_customer_id"},
		"cus-c", createdAt, "Fixture cus-c", "Merged Name", "other", "merged", "cus-a"); err != nil {
		t.Fatalf("insert customer cus-c: %v", err)
	}
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "created_at", "display_name", "channel", "referrer_customer_id", "status"},
		"cus-b", createdAt, "Fixture cus-b", "referral", "cus-a", "archived"); err != nil {
		t.Fatalf("insert customer cus-b: %v", err)
	}
	for _, item := range []struct {
		id, customerID, platform, handle string
		remark                           any
	}{
		{id: "identity-b", customerID: "cus-b", platform: "telegram", handle: "fixture-telegram"},
		{id: "identity-a", customerID: "cus-a", platform: "wechat", handle: "fixture-handle", remark: "fixture-remark"},
	} {
		if err := scope.Insert(ctx, "social_identities",
			[]string{"id", "created_at", "customer_id", "platform", "handle", "remark"},
			item.id, createdAt, item.customerID, item.platform, item.handle, item.remark); err != nil {
			t.Fatalf("insert identity %s: %v", item.id, err)
		}
	}
	for _, item := range []struct{ id, customerID, content string }{
		{id: "note-b", customerID: "cus-b", content: "fixture note b"},
		{id: "note-a", customerID: "cus-a", content: "fixture note a"},
	} {
		if err := scope.Insert(ctx, "customer_notes",
			[]string{"id", "created_at", "customer_id", "content"},
			item.id, createdAt, item.customerID, item.content); err != nil {
			t.Fatalf("insert note %s: %v", item.id, err)
		}
	}
	if err := scope.Insert(ctx, "packages",
		[]string{"id", "created_at", "name", "shoot_type", "pricing_mode", "base_price", "status"},
		"pkg-b", createdAt, "Fixture pkg-b", "cosplay", "per_photo", 8000, "archived"); err != nil {
		t.Fatalf("insert package pkg-b: %v", err)
	}
	if err := scope.Insert(ctx, "packages",
		[]string{
			"id", "created_at", "name", "shoot_type", "pricing_mode", "base_price", "duration_minutes",
			"shot_count_min", "shot_count_max", "raw_delivery_count", "retouch_count", "note", "status",
		},
		"pkg-a", createdAt, "Fixture pkg-a", "portrait", "fixed", 12000, 90, 30, 60, 45, 12, "fixture package", "active"); err != nil {
		t.Fatalf("insert package pkg-a: %v", err)
	}
	orderStatuses := []string{
		"consulting", "scheduled", "shot", "selected", "retouching", "delivered", "closed", "cancelled",
	}
	for index := len(orderStatuses) - 1; index >= 0; index-- {
		status := orderStatuses[index]
		id := "order-" + string(rune('a'+index))
		var packageID, title, price, shotAt, deliveredAt, deliveryDueAt, note any
		var depositPaid, balancePaid, deliveryDueIsOverride bool
		var amountPaid int
		var outstanding, paidAt, shootTypeSnapshot any
		channelSnapshot := "other"
		switch id {
		case "order-a":
			packageID, title, price, note = "pkg-a", "Fixture consulting", 12000, "fixture order"
			shootTypeSnapshot = "portrait"
			depositPaid = true
			// 部分收款：导出必须原样带出金额事实，而非回落推定。
			amountPaid, outstanding = 300, 700
		case "order-f":
			packageID, title, price, note = "pkg-b", "Fixture delivered", 8000, "delivered order"
			shootTypeSnapshot = "cosplay"
			shotAt, deliveredAt = createdAt.Add(-48*time.Hour), createdAt.Add(-24*time.Hour)
			depositPaid, balancePaid = true, true
			// 显式覆盖的应交付日：导出必须原样带出，而不是回落默认派生。
			deliveryDueAt, deliveryDueIsOverride = time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC), true
			amountPaid, outstanding, paidAt = 8000, 0, createdAt.Add(-20*time.Hour)
		}
		if err := scope.Insert(ctx, "orders",
			[]string{
				"id", "created_at", "customer_id", "package_id", "title", "status", "price", "deposit_paid",
				"balance_paid", "shot_at", "delivered_at", "delivery_due_at", "delivery_due_is_override",
				"amount_paid", "outstanding_amount", "paid_at", "channel_snapshot", "shoot_type_snapshot", "note",
			},
			id, createdAt, "cus-a", packageID, title, status, price, depositPaid, balancePaid, shotAt, deliveredAt,
			deliveryDueAt, deliveryDueIsOverride, amountPaid, outstanding, paidAt, channelSnapshot, shootTypeSnapshot, note); err != nil {
			t.Fatalf("insert order %s: %v", id, err)
		}
	}
	for index, slotType := range []string{"shoot", "hold", "busy"} {
		var orderID any
		var note any
		switch slotType {
		case "shoot":
			orderID = "order-a"
			note = "fixture shoot slot"
		case "hold":
			note = "fixture hold slot"
		}
		startAt := createdAt.Add(time.Duration(3-index) * time.Hour)
		if err := scope.Insert(ctx, "schedule_slots",
			[]string{"id", "created_at", "start_at", "end_at", "type", "order_id", "note"},
			"slot-"+slotType, createdAt, startAt, startAt.Add(time.Hour), slotType, orderID, note); err != nil {
			t.Fatalf("insert slot %s: %v", slotType, err)
		}
	}
	for _, status := range []string{"pending", "done", "dismissed"} {
		var customerID, orderID any
		switch status {
		case "pending":
			customerID, orderID = "cus-a", "order-a"
		case "dismissed":
			customerID = "cus-b"
		}
		if err := scope.Insert(ctx, "reminders",
			[]string{"id", "created_at", "type", "customer_id", "order_id", "due_date", "content", "status", "dedup_key"},
			"reminder-"+status, createdAt, "custom", customerID, orderID, "2026-07-22", "Fixture "+status, status, "dedup-"+status); err != nil {
			t.Fatalf("insert reminder %s: %v", status, err)
		}
	}
	if err := scope.Insert(ctx, "settings",
		[]string{"timezone", "birthday_lead_days", "follow_up_after_days", "churn_thresholds", "digest_hour", "delivery_sla_days", "telegram_chat_id", "availability", "updated_at"},
		"Asia/Tokyo", 5, 9, []byte(`[{"shoot_type":"portrait","days":90}]`), 7, 30, "fixture-chat",
		[]byte(`{"weekly":{"1":{"start":"08:30","end":"17:30"},"2":null,"3":{"start":"10:00","end":"19:00"},"4":{"start":"10:00","end":"19:00"},"5":{"start":"10:00","end":"19:00"},"6":{"start":"09:00","end":"20:00"},"7":null},"min_opening_minutes":90,"turnaround_minutes":30}`),
		createdAt); err != nil {
		t.Fatalf("insert settings: %v", err)
	}

	snapshot, err := NewPostgresRepository().LoadSnapshot(ctx, scope)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	dueDate := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	expected := Snapshot{
		Customers: []customer.Customer{
			{ID: "cus-a", AccountID: "acct-full", CreatedAt: createdAt, DisplayName: "Fixture cus-a", RealName: testPointer("Legal Name"), Phone: testPointer("13000000000"), Birthday: testPointer("03-15"), Channel: "other", Status: "active"},
			{ID: "cus-b", AccountID: "acct-full", CreatedAt: createdAt, DisplayName: "Fixture cus-b", Channel: "referral", ReferrerCustomerID: testPointer("cus-a"), Status: "archived"},
			{ID: "cus-c", AccountID: "acct-full", CreatedAt: createdAt, DisplayName: "Fixture cus-c", RealName: testPointer("Merged Name"), Channel: "other", Status: "merged", MergedIntoCustomerID: testPointer("cus-a")},
		},
		SocialIdentities: []customer.SocialIdentity{
			{ID: "identity-a", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Platform: "wechat", Handle: "fixture-handle", Remark: testPointer("fixture-remark")},
			{ID: "identity-b", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-b", Platform: "telegram", Handle: "fixture-telegram"},
		},
		CustomerNotes: []customer.CustomerNote{
			{ID: "note-a", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Content: "fixture note a"},
			{ID: "note-b", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-b", Content: "fixture note b"},
		},
		Packages: []pkgcatalog.Package{
			{ID: "pkg-a", AccountID: "acct-full", CreatedAt: createdAt, Name: "Fixture pkg-a", ShootType: "portrait", PricingMode: "fixed", BasePrice: 12000, DurationMinutes: testPointer(90), ShotCountMin: testPointer(30), ShotCountMax: testPointer(60), RawDeliveryCount: testPointer(45), RetouchCount: testPointer(12), Note: testPointer("fixture package"), Status: "active"},
			{ID: "pkg-b", AccountID: "acct-full", CreatedAt: createdAt, Name: "Fixture pkg-b", ShootType: "cosplay", PricingMode: "per_photo", BasePrice: 8000, Status: "archived"},
		},
		Orders: []orderdomain.Order{
			{ID: "order-a", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", PackageID: testPointer("pkg-a"), Title: testPointer("Fixture consulting"), Status: "consulting", Price: testPointer(12000), DepositPaid: true, AmountPaid: 300, OutstandingAmount: testPointer(700), ChannelSnapshot: "other", ShootTypeSnapshot: testPointer("portrait"), Note: testPointer("fixture order")},
			{ID: "order-b", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Status: "scheduled", ChannelSnapshot: "other"},
			{ID: "order-c", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Status: "shot", ChannelSnapshot: "other"},
			{ID: "order-d", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Status: "selected", ChannelSnapshot: "other"},
			{ID: "order-e", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Status: "retouching", ChannelSnapshot: "other"},
			{ID: "order-f", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", PackageID: testPointer("pkg-b"), Title: testPointer("Fixture delivered"), Status: "delivered", Price: testPointer(8000), DepositPaid: true, BalancePaid: true, ShotAt: testPointer(createdAt.Add(-48 * time.Hour)), DeliveredAt: testPointer(createdAt.Add(-24 * time.Hour)), DeliveryDueAt: testPointer(time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC)), DeliveryDueIsOverride: true, AmountPaid: 8000, OutstandingAmount: testPointer(0), PaidAt: testPointer(createdAt.Add(-20 * time.Hour)), ChannelSnapshot: "other", ShootTypeSnapshot: testPointer("cosplay"), Note: testPointer("delivered order")},
			{ID: "order-g", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Status: "closed", ChannelSnapshot: "other"},
			{ID: "order-h", AccountID: "acct-full", CreatedAt: createdAt, CustomerID: "cus-a", Status: "cancelled", ChannelSnapshot: "other"},
		},
		ScheduleSlots: []schedule.Slot{
			{ID: "slot-busy", AccountID: "acct-full", CreatedAt: createdAt, StartAt: createdAt.Add(time.Hour), EndAt: createdAt.Add(2 * time.Hour), Type: "busy"},
			{ID: "slot-hold", AccountID: "acct-full", CreatedAt: createdAt, StartAt: createdAt.Add(2 * time.Hour), EndAt: createdAt.Add(3 * time.Hour), Type: "hold", Note: testPointer("fixture hold slot")},
			{ID: "slot-shoot", AccountID: "acct-full", CreatedAt: createdAt, StartAt: createdAt.Add(3 * time.Hour), EndAt: createdAt.Add(4 * time.Hour), Type: "shoot", OrderID: testPointer("order-a"), Note: testPointer("fixture shoot slot")},
		},
		Reminders: []reminder.Reminder{
			{ID: "reminder-dismissed", AccountID: "acct-full", CreatedAt: createdAt, Type: "custom", CustomerID: testPointer("cus-b"), DueDate: dueDate, Content: "Fixture dismissed", Status: "dismissed", DedupKey: "dedup-dismissed"},
			{ID: "reminder-done", AccountID: "acct-full", CreatedAt: createdAt, Type: "custom", DueDate: dueDate, Content: "Fixture done", Status: "done", DedupKey: "dedup-done"},
			{ID: "reminder-pending", AccountID: "acct-full", CreatedAt: createdAt, Type: "custom", CustomerID: testPointer("cus-a"), OrderID: testPointer("order-a"), DueDate: dueDate, Content: "Fixture pending", Status: "pending", DedupKey: "dedup-pending"},
		},
		Settings: settings.Settings{
			Timezone:                      "Asia/Tokyo",
			BirthdayLeadDays:              5,
			FollowUpAfterDays:             9,
			PlanningBusinessRuleOverrides: business.RuleOverrides{},
			ChurnThresholds: []settings.ChurnThreshold{
				{ShootType: "portrait", Days: 90},
				{ShootType: "cosplay", Days: 180},
				{ShootType: "other", Days: 180},
			},
			DigestHour:              7,
			DeliverySLADays:         30,
			TelegramChatID:          testPointer("fixture-chat"),
			TelegramBindingRevision: 1,
			Availability: settings.ScheduleAvailability{
				Weekly: settings.ScheduleAvailabilityWeekly{
					Monday:    testAvailabilityWindow("08:30", "17:30"),
					Tuesday:   nil,
					Wednesday: testAvailabilityWindow("10:00", "19:00"),
					Thursday:  testAvailabilityWindow("10:00", "19:00"),
					Friday:    testAvailabilityWindow("10:00", "19:00"),
					Saturday:  testAvailabilityWindow("09:00", "20:00"),
					Sunday:    nil,
				},
				MinOpeningMinutes: 90,
				TurnaroundMinutes: 30,
			},
			UpdatedAt: createdAt,
		},
	}
	if got, want := canonicalSnapshotTimes(snapshot), canonicalSnapshotTimes(expected); !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot differs from complete allowlist fixture:\n got: %#v\nwant: %#v", got, want)
	}
	settingsView, err := settings.NewService(settings.NewPostgresRepository()).Get(ctx, scope)
	if err != nil {
		t.Fatalf("GET settings parity read: %v", err)
	}
	if got, want := canonicalSettingsTime(snapshot.Settings), canonicalSettingsTime(settingsView); !reflect.DeepEqual(got, want) {
		t.Fatalf("export settings differ from settings service: export=%+v service=%+v", snapshot.Settings, settingsView)
	}
}

func testAvailabilityWindow(start, end string) *settings.ScheduleAvailabilityWindow {
	return &settings.ScheduleAvailabilityWindow{Start: start, End: end}
}

func TestPostgresRepositoryUsesOneBatchReadPerExportTableAndNoCountQuery(t *testing.T) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithCmd("postgres", "-c", "log_statement=all"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start statement-logging postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(database.Close)
	scope := createDataExportAccount(t, database, "acct-query-proof")
	if _, err := NewPostgresRepository().LoadSnapshot(ctx, scope); err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	reader, err := ctr.Logs(ctx)
	if err != nil {
		t.Fatalf("read postgres logs: %v", err)
	}
	defer func() { _ = reader.Close() }()
	logBytes, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("consume postgres logs: %v", err)
	}
	logs := string(logBytes)
	tables := []string{
		"customers", "social_identities", "customer_notes", "packages", "orders",
		"schedule_slots", "reminders", "settings",
	}
	for _, table := range tables {
		needle := "FROM " + table + " WHERE account_id = $1"
		if count := strings.Count(logs, needle); count != 1 {
			t.Fatalf("%s batch read count = %d, want 1", table, count)
		}
	}
	lowerLogs := strings.ToLower(logs)
	for _, table := range tables {
		if strings.Contains(lowerLogs, "select count(*) from "+table+" where account_id") {
			t.Fatalf("export snapshot issued a separate count query for %s", table)
		}
	}
}

func TestPostgresRepositoryKeepsAccountRowsIsolated(t *testing.T) {
	ctx := context.Background()
	database := openDataExportStore(t)
	scopeA := createDataExportAccount(t, database, "acct-a")
	scopeB := createDataExportAccount(t, database, "acct-b")
	if err := scopeA.Insert(ctx, "customers", []string{"id", "display_name", "channel"},
		"customer-a", "Account A", "other"); err != nil {
		t.Fatalf("insert account A customer: %v", err)
	}
	if err := scopeB.Insert(ctx, "customers", []string{"id", "display_name", "channel"},
		"customer-b", "Account B", "other"); err != nil {
		t.Fatalf("insert account B customer: %v", err)
	}

	snapshot, err := NewPostgresRepository().LoadSnapshot(ctx, scopeB)
	if err != nil {
		t.Fatalf("LoadSnapshot(account B): %v", err)
	}
	if len(snapshot.Customers) != 1 || snapshot.Customers[0].ID != "customer-b" || snapshot.Customers[0].AccountID != "acct-b" {
		t.Fatalf("account B snapshot crossed account boundary: %+v", snapshot.Customers)
	}
}

func TestPostgresRepositoryReportsQueryAndSettingsDecodeStages(t *testing.T) {
	t.Run("settings decode", func(t *testing.T) {
		ctx := context.Background()
		database := openDataExportStore(t)
		scope := createDataExportAccount(t, database, "acct-settings-failure")
		if err := scope.Insert(ctx, "settings",
			[]string{"timezone", "birthday_lead_days", "follow_up_after_days", "churn_thresholds", "digest_hour", "updated_at"},
			"Asia/Shanghai", 3, 7, []byte(`{"unexpected":"object"}`), 9, time.Now().UTC()); err != nil {
			t.Fatalf("insert malformed settings shape: %v", err)
		}
		if _, err := NewPostgresRepository().LoadSnapshot(ctx, scope); err == nil ||
			!strings.Contains(err.Error(), "load settings") {
			t.Fatalf("settings decode error = %v, want settings stage", err)
		}
	})
}

func testPointer[T any](value T) *T {
	return &value
}

func canonicalSnapshotTimes(snapshot Snapshot) Snapshot {
	for index := range snapshot.Customers {
		snapshot.Customers[index].CreatedAt = snapshot.Customers[index].CreatedAt.UTC()
	}
	for index := range snapshot.SocialIdentities {
		snapshot.SocialIdentities[index].CreatedAt = snapshot.SocialIdentities[index].CreatedAt.UTC()
	}
	for index := range snapshot.CustomerNotes {
		snapshot.CustomerNotes[index].CreatedAt = snapshot.CustomerNotes[index].CreatedAt.UTC()
	}
	for index := range snapshot.Packages {
		snapshot.Packages[index].CreatedAt = snapshot.Packages[index].CreatedAt.UTC()
	}
	for index := range snapshot.Orders {
		item := &snapshot.Orders[index]
		item.CreatedAt = item.CreatedAt.UTC()
		item.ShotAt = canonicalTimePointer(item.ShotAt)
		item.DeliveredAt = canonicalTimePointer(item.DeliveredAt)
		item.PaidAt = canonicalTimePointer(item.PaidAt)
	}
	for index := range snapshot.ScheduleSlots {
		item := &snapshot.ScheduleSlots[index]
		item.CreatedAt = item.CreatedAt.UTC()
		item.StartAt = item.StartAt.UTC()
		item.EndAt = item.EndAt.UTC()
	}
	for index := range snapshot.Reminders {
		item := &snapshot.Reminders[index]
		item.CreatedAt = item.CreatedAt.UTC()
		item.DueDate = item.DueDate.UTC()
	}
	snapshot.Settings = canonicalSettingsTime(snapshot.Settings)
	return snapshot
}

func canonicalSettingsTime(value settings.Settings) settings.Settings {
	value.UpdatedAt = value.UpdatedAt.UTC()
	return value
}

func canonicalTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	canonical := value.UTC()
	return &canonical
}

func openDataExportStore(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	database, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(database.Close)
	return database
}

func createDataExportAccount(t *testing.T, database *store.Store, accountID string) store.AccountScope {
	t.Helper()
	if err := database.CreateAccount(context.Background(), accountID, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", accountID, err)
	}
	return database.ScopeFor(auth.AccountContext{AccountID: accountID})
}
