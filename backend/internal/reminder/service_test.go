package reminder_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func startPostgres(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	url := storetest.NewURL(t)
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func createAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	activateLegacyTestAccount(t, s, id)
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func activateLegacyTestAccount(t *testing.T, s *store.Store, id string) {
	t.Helper()
	mail := &activationMailSender{}
	service := auth.NewService(
		s,
		auth.NewTokenIssuer("reminder-test-account-activation-root"),
		auth.WithAuthMailSender(mail),
		auth.WithAttemptLimiter(s),
		auth.WithPublicBaseURL("https://reminder.test"),
	)
	result, err := service.BeginLegacyClaim(context.Background(), id+"@reminder.test", false)
	if err != nil || result.State != auth.LegacyClaimReady || mail.wire == "" {
		t.Fatalf("begin legacy test-account activation: state=%s err=%v", result.State, err)
	}
	if _, err := service.VerifyEmail(context.Background(), mail.wire, auth.ClientMeta{SourceIP: netip.MustParseAddr("127.0.0.1")}); err != nil {
		t.Fatalf("verify legacy test-account activation: %v", err)
	}
}

type activationMailSender struct {
	wire string
}

func (s *activationMailSender) Send(_ context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	actionURL, err := url.Parse(mail.ActionURL)
	if err != nil {
		return auth.MailReceipt{}, fmt.Errorf("parse activation action URL: %w", err)
	}
	fragment, err := url.ParseQuery(actionURL.Fragment)
	if err != nil {
		return auth.MailReceipt{}, fmt.Errorf("parse activation action fragment: %w", err)
	}
	s.wire = fragment.Get("token")
	return auth.MailReceipt{ProviderMessageID: "reminder-test-activation", AcceptedAt: time.Now().UTC()}, nil
}

func newServices(s *store.Store) (*settings.Service, *reminder.Service) {
	settingsSvc := settings.NewService(settings.NewPostgresRepository()).WithScopeFactory(func(accountID string) store.AccountScope {
		return s.ScopeFor(auth.AccountContext{AccountID: accountID})
	})
	reminderSvc := reminder.NewService(
		reminder.NewPostgresRepository(),
		reminder.NewSettingsAdapter(settingsSvc),
		slog.New(slog.DiscardHandler),
	)
	return settingsSvc, reminderSvc
}

func seedCustomer(t *testing.T, scope store.AccountScope, id, name, birthday string) {
	t.Helper()
	ctx := context.Background()
	var b any
	if birthday != "" {
		b = birthday
	}
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "display_name", "channel", "birthday", "status"},
		id, name, "other", b, "active",
	); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
}

func seedOrder(t *testing.T, scope store.AccountScope, id, customerID, status string, shotAt, deliveredAt *time.Time, packageID *string) {
	t.Helper()
	ctx := context.Background()
	var shot, delivered, pkg any
	if shotAt != nil {
		shot = *shotAt
	}
	if deliveredAt != nil {
		delivered = *deliveredAt
	}
	if packageID != nil {
		pkg = *packageID
	}
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "customer_id", "package_id", "title", "status", "shot_at", "delivered_at"},
		id, customerID, pkg, "测试单", status, shot, delivered,
	); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func TestScanIdempotentAndBirthdayFollowUpChurn(t *testing.T) {
	ctx := context.Background()
	s := startPostgres(t)
	scope := createAccount(t, s, "acct-a")
	_, svc := newServices(s)

	// fixed clock: 2026-07-12 Asia/Shanghai
	now := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	svc.WithClock(func() time.Time { return now })

	seedCustomer(t, scope, "cus_bday", "小明", "07-14") // within lead 3
	seedCustomer(t, scope, "cus_fu", "小红", "")
	seedCustomer(t, scope, "cus_churn", "小刚", "")
	seedCustomer(t, scope, "cus_zero", "零成交", "")

	deliveredAt := time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC) // local +7 days = 07-08 ≤ 07-12
	seedOrder(t, scope, "ord_fu", "cus_fu", "delivered", nil, &deliveredAt, nil)

	shotAt := time.Date(2025, 12, 1, 8, 0, 0, 0, time.UTC) // >180 days before 07-12
	seedOrder(t, scope, "ord_churn", "cus_churn", "closed", &shotAt, nil, nil)

	// only cancelled → zero completed
	seedOrder(t, scope, "ord_cancel", "cus_zero", "cancelled", &shotAt, nil, nil)

	first, err := svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if first.Created < 3 {
		t.Fatalf("want at least birthday+follow_up+churn created, got %+v", first)
	}

	second, err := svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if second.Created != 0 {
		t.Fatalf("double scan must create 0, got %+v", second)
	}

	list, err := svc.List(ctx, scope, reminder.ListFilter{Status: reminder.StatusPending, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	types := map[string]int{}
	for _, item := range list.Items {
		types[item.Type]++
	}
	if types[reminder.TypeBirthday] < 1 || types[reminder.TypeFollowUp] < 1 || types[reminder.TypeChurn] < 1 {
		t.Fatalf("missing rule types: %v items=%+v", types, list.Items)
	}
	// zero-deal should not churn
	for _, item := range list.Items {
		if item.CustomerID != nil && *item.CustomerID == "cus_zero" {
			t.Fatalf("zero-deal customer must not get reminder: %+v", item)
		}
	}
}

func TestChurnRequiresNoNonTerminalOrders(t *testing.T) {
	// REV-001: delivered 是非终态，仅 delivered 老单不得生成 churn。
	ctx := context.Background()
	s := startPostgres(t)
	scope := createAccount(t, s, "acct-a")
	_, svc := newServices(s)
	svc.WithClock(func() time.Time {
		return time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	})

	seedCustomer(t, scope, "cus_del", "交付未完", "")
	shotAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	deliveredAt := time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC)
	seedOrder(t, scope, "ord_del", "cus_del", "delivered", &shotAt, &deliveredAt, nil)

	result, err := svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	list, err := svc.List(ctx, scope, reminder.ListFilter{Status: reminder.StatusPending, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, item := range list.Items {
		if item.Type == reminder.TypeChurn {
			t.Fatalf("delivered-only customer must not get churn: scan=%+v item=%+v", result, item)
		}
	}

	// close the order → churn should appear
	if _, err := scope.Update(ctx, "orders", "status = $2", "id = $3", "closed", "ord_del"); err != nil {
		t.Fatalf("close order: %v", err)
	}
	result, err = svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("scan after close: %v", err)
	}
	if result.Created < 1 {
		t.Fatalf("want churn after close, got %+v", result)
	}

	// re-open via new delivered → auto-dismiss old churn
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "customer_id", "status", "shot_at", "delivered_at"},
		"ord_new_del", "cus_del", "delivered", shotAt, deliveredAt,
	); err != nil {
		t.Fatalf("seed delivered: %v", err)
	}
	result, err = svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("scan after redeliver: %v", err)
	}
	if result.AutoDismissed < 1 {
		t.Fatalf("want auto-dismiss when delivered non-terminal exists, got %+v", result)
	}
}

func TestAutoDismissOrphanOrderAndRepurchase(t *testing.T) {
	ctx := context.Background()
	s := startPostgres(t)
	scope := createAccount(t, s, "acct-a")
	_, svc := newServices(s)
	svc.WithClock(func() time.Time {
		return time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	})

	seedCustomer(t, scope, "cus_1", "客户", "")
	shotAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	seedOrder(t, scope, "ord_old", "cus_1", "closed", &shotAt, nil, nil)

	// first scan creates churn
	if _, err := svc.Scan(ctx, scope, nil); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// delete order → orphan
	if _, err := scope.Delete(ctx, "orders", "id = $2", "ord_old"); err != nil {
		t.Fatalf("delete order: %v", err)
	}
	// seed another closed so churn rule still has completed history after re-order path
	// (for orphan path we only care about auto-dismiss of order_id)
	result, err := svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("scan after delete: %v", err)
	}
	if result.AutoDismissed < 1 {
		t.Fatalf("want orphan auto-dismiss, got %+v", result)
	}

	// repurchase path: create churn again then open order
	seedOrder(t, scope, "ord_closed2", "cus_1", "closed", &shotAt, nil, nil)
	if _, err := svc.Scan(ctx, scope, nil); err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if err := scope.Insert(ctx, "orders",
		[]string{"id", "customer_id", "status"},
		"ord_open", "cus_1", "consulting",
	); err != nil {
		t.Fatalf("seed open: %v", err)
	}
	result, err = svc.Scan(ctx, scope, nil)
	if err != nil {
		t.Fatalf("scan repurchase: %v", err)
	}
	if result.AutoDismissed < 1 {
		t.Fatalf("want repurchase auto-dismiss, got %+v", result)
	}
}

func TestSettingsDefaultsAndTimezoneProvider(t *testing.T) {
	ctx := context.Background()
	s := startPostgres(t)
	scope := createAccount(t, s, "acct-a")
	settingsSvc, _ := newServices(s)

	got, err := settingsSvc.Get(ctx, scope)
	if err != nil {
		t.Fatalf("get defaults: %v", err)
	}
	if got.Timezone != settings.DefaultTimezone || got.BirthdayLeadDays != 3 || len(got.ChurnThresholds) != 3 {
		t.Fatalf("defaults mismatch: %+v", got)
	}

	tz := "America/Los_Angeles"
	portraitOnly := []settings.ChurnThreshold{{ShootType: "portrait", Days: 90}}
	patched, err := settingsSvc.Patch(ctx, scope, settings.PatchInput{
		Timezone:        &tz,
		ChurnThresholds: &portraitOnly,
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if patched.Timezone != tz {
		t.Fatalf("timezone: %s", patched.Timezone)
	}
	if patched.ChurnDaysFor("portrait") != 90 {
		t.Fatalf("portrait threshold: %d", patched.ChurnDaysFor("portrait"))
	}
	if patched.ChurnDaysFor("cosplay") != 180 {
		t.Fatalf("cosplay should keep default 180, got %d", patched.ChurnDaysFor("cosplay"))
	}

	providerTZ, err := settingsSvc.TimezoneForAccount(ctx, "acct-a")
	if err != nil || providerTZ != tz {
		t.Fatalf("provider: %s err=%v", providerTZ, err)
	}
}

func TestCustomDoneDismissAndMergeReassign(t *testing.T) {
	ctx := context.Background()
	s := startPostgres(t)
	scope := createAccount(t, s, "acct-a")
	_, svc := newServices(s)

	seedCustomer(t, scope, "cus_src", "源", "")
	seedCustomer(t, scope, "cus_tgt", "目标", "")
	cid := "cus_src"
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	created, err := svc.CreateCustom(ctx, scope, reminder.CreateCustomInput{
		CustomerID: &cid,
		DueDate:    due,
		Content:    "选片",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.DedupKey != "custom:"+created.ID {
		t.Fatalf("dedup: %s", created.DedupKey)
	}

	done, err := svc.MarkDone(ctx, scope, created.ID)
	if err != nil || done.Status != reminder.StatusDone {
		t.Fatalf("done: %+v err=%v", done, err)
	}
	// idempotent
	if _, err := svc.MarkDone(ctx, scope, created.ID); err != nil {
		t.Fatalf("done again: %v", err)
	}

	// merge reassign via repository (same as customer merge path)
	repo := reminder.NewPostgresRepository()
	if err := repo.ReassignCustomer(ctx, scope, "cus_tgt", "cus_src"); err != nil {
		t.Fatalf("reassign: %v", err)
	}
	found, err := repo.Find(ctx, scope, created.ID)
	if err != nil || found.CustomerID == nil || *found.CustomerID != "cus_tgt" {
		t.Fatalf("after reassign: %+v err=%v", found, err)
	}
}

func TestScanRunnerOncePerLocalDay(t *testing.T) {
	ctx := context.Background()
	s := startPostgres(t)
	_ = createAccount(t, s, "acct-a")
	settingsSvc, svc := newServices(s)

	now := time.Date(2026, 7, 12, 1, 0, 0, 0, time.UTC)
	svc.WithClock(func() time.Time { return now })
	runner := reminder.NewScanRunner(s, svc, settingsSvc, slog.New(slog.DiscardHandler)).
		WithClock(func() time.Time { return now }, time.Hour)

	runner.RunOnce(ctx)
	runner.RunOnce(ctx) // same day, should no-op via checkpoint

	scope := s.ScopeFor(auth.AccountContext{AccountID: "acct-a"})
	last, found, err := reminder.NewPostgresRepository().LoadScanState(ctx, scope)
	if err != nil || !found {
		t.Fatalf("checkpoint: found=%v err=%v", found, err)
	}
	if reminder.FormatDate(last) != "2026-07-12" {
		t.Fatalf("last_scan_date: %v", last)
	}

	// cross day
	now = time.Date(2026, 7, 13, 1, 0, 0, 0, time.UTC)
	svc.WithClock(func() time.Time { return now })
	runner.WithClock(func() time.Time { return now }, time.Hour)
	runner.RunOnce(ctx)
	last, _, err = reminder.NewPostgresRepository().LoadScanState(ctx, scope)
	if err != nil || reminder.FormatDate(last) != "2026-07-13" {
		t.Fatalf("cross-day checkpoint: %v err=%v", last, err)
	}
}
