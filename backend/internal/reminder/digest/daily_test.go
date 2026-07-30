package digest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}

type scanProbe struct {
	calls int
	err   error
}

func (s *scanProbe) EnsureScan(context.Context, store.ScopedAccount, LocalTarget) error {
	s.calls++
	return s.err
}

func TestDailySchedulerScanBeforeSendOnceAndRestartCatchup(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 2, 30, 0, 0, time.UTC) // 10:30 Asia/Shanghai
	if err := account.Scope.Upsert(context.Background(), "settings",
		[]string{"timezone", "digest_hour", "telegram_chat_id", "updated_at"},
		[]string{"account_id"},
		[]string{"timezone", "digest_hour", "telegram_chat_id", "updated_at"},
		"Asia/Shanghai", 9, "chat-daily", now); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	settingsSvc := settings.NewService(settings.NewPostgresRepository())
	provider := NewSettingsTargetProvider(settingsSvc)
	repo := NewPostgresDailyRepository()
	scan := &scanProbe{}
	scheduler := NewDailyScheduler(accountEnumerator{db: db}, provider, scan, repo, discardLogger()).WithClock(func() time.Time { return now })

	scheduler.RunOnce(context.Background())
	scheduler.RunOnce(context.Background())
	if scan.calls != 1 {
		t.Fatalf("already-enqueued local date must not rescan, calls=%d", scan.calls)
	}
	assertDailyCount(t, account.Scope, 1)

	restarted := NewDailyScheduler(accountEnumerator{db: db}, provider, scan, repo, discardLogger()).WithClock(func() time.Time { return now })
	restarted.RunOnce(context.Background())
	assertDailyCount(t, account.Scope, 1)
	if scan.calls != 1 {
		t.Fatalf("restart must observe existing daily before scan, calls=%d", scan.calls)
	}
}

func TestDailySchedulerScanFailureDoesNotEnqueueIncompleteDigest(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 2, 30, 0, 0, time.UTC)
	if err := account.Scope.Upsert(context.Background(), "settings",
		[]string{"timezone", "digest_hour", "telegram_chat_id", "updated_at"},
		[]string{"account_id"},
		[]string{"timezone", "digest_hour", "telegram_chat_id", "updated_at"},
		"Asia/Shanghai", 9, "chat-daily", now); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	provider := NewSettingsTargetProvider(settings.NewService(settings.NewPostgresRepository()))
	repo := NewPostgresDailyRepository()
	scan := &scanProbe{err: errors.New("scan failed")}
	scheduler := NewDailyScheduler(accountEnumerator{db: db}, provider, scan, repo, discardLogger()).WithClock(func() time.Time { return now })

	scheduler.RunOnce(context.Background())
	assertDailyCount(t, account.Scope, 0)
	scan.err = nil
	scheduler.RunOnce(context.Background())
	assertDailyCount(t, account.Scope, 1)
}

func assertDailyCount(t *testing.T, scope store.AccountScope, want int64) {
	t.Helper()
	got, err := scope.Count(context.Background(), "telegram_deliveries", "source = $2", string(DeliverySourceDaily))
	if err != nil {
		t.Fatalf("count daily: %v", err)
	}
	if got != want {
		t.Fatalf("daily deliveries: want %d, got %d", want, got)
	}
}

type stubDailyEnumerator struct{ accounts []store.ScopedAccount }

func (e stubDailyEnumerator) AccountScopes(context.Context) ([]store.ScopedAccount, error) {
	return e.accounts, nil
}

type stubDailyTargets struct {
	target LocalTarget
	due    bool
}

func (p stubDailyTargets) DailyDue(context.Context, store.ScopedAccount, time.Time) (LocalTarget, bool, error) {
	return p.target, p.due, nil
}

type stubDailyRepo struct{ exists bool }

func (r stubDailyRepo) DailyExists(context.Context, store.AccountScope, LocalTarget) (bool, error) {
	return r.exists, nil
}

func (stubDailyRepo) EnqueueDaily(context.Context, store.AccountScope, LocalTarget, time.Time) (bool, error) {
	return true, nil
}

func TestDailySchedulerLogsSanitizedScanFailure(t *testing.T) {
	now := time.Date(2026, 7, 15, 2, 30, 0, 0, time.UTC)
	target := LocalTarget{LocalDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), Timezone: "Asia/Shanghai"}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	scheduler := NewDailyScheduler(
		stubDailyEnumerator{accounts: []store.ScopedAccount{{AccountID: "acc-log"}}},
		stubDailyTargets{target: target, due: true},
		&scanProbe{err: errors.New("scan boom")},
		stubDailyRepo{},
		logger,
	).WithClock(func() time.Time { return now })

	scheduler.RunOnce(context.Background())

	out := buf.String()
	for _, want := range []string{"account_id=acc-log", "target_local_date=2026-07-15", "scan boom"} {
		if !strings.Contains(out, want) {
			t.Fatalf("scan failure log missing %q: %s", want, out)
		}
	}
	for _, forbidden := range []string{"chat", "token"} {
		if strings.Contains(out, forbidden) {
			t.Fatalf("scan failure log leaked %q: %s", forbidden, out)
		}
	}
}
