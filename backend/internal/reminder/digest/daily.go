package digest

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

const dailyInterval = time.Minute

type SettingsReader interface {
	Get(context.Context, store.AccountScope) (settings.Settings, error)
}

type SettingsTargetProvider struct{ settings SettingsReader }

func NewSettingsTargetProvider(settings SettingsReader) SettingsTargetProvider {
	return SettingsTargetProvider{settings: settings}
}

func (p SettingsTargetProvider) TargetFor(
	ctx context.Context,
	account store.ScopedAccount,
	now time.Time,
) (LocalTarget, error) {
	view, err := p.settings.Get(ctx, account.Scope)
	if err != nil {
		return LocalTarget{}, err
	}
	location, err := time.LoadLocation(view.Timezone)
	if err != nil {
		return LocalTarget{}, err
	}
	local := now.In(location)
	return LocalTarget{
		LocalDate: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC),
		Timezone:  view.Timezone,
	}, nil
}

func (p SettingsTargetProvider) DailyDue(
	ctx context.Context,
	account store.ScopedAccount,
	now time.Time,
) (LocalTarget, bool, error) {
	view, err := p.settings.Get(ctx, account.Scope)
	if err != nil {
		return LocalTarget{}, false, err
	}
	if view.TelegramChatID == nil {
		return LocalTarget{}, false, nil
	}
	location, err := time.LoadLocation(view.Timezone)
	if err != nil {
		return LocalTarget{}, false, err
	}
	local := now.In(location)
	target := LocalTarget{
		LocalDate: time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC),
		Timezone:  view.Timezone,
	}
	return target, local.Hour() >= view.DigestHour, nil
}

type DailyTargetProvider interface {
	DailyDue(context.Context, store.ScopedAccount, time.Time) (LocalTarget, bool, error)
}

type DailyRepository interface {
	DailyExists(context.Context, store.AccountScope, LocalTarget) (bool, error)
	EnqueueDaily(context.Context, store.AccountScope, LocalTarget, time.Time) (bool, error)
}

type PostgresDailyRepository struct{}

func NewPostgresDailyRepository() PostgresDailyRepository { return PostgresDailyRepository{} }

func (PostgresDailyRepository) DailyExists(
	ctx context.Context,
	scope store.AccountScope,
	target LocalTarget,
) (bool, error) {
	return scope.Exists(ctx, "telegram_deliveries", "source = $2 AND source_key = $3",
		string(DeliverySourceDaily), target.LocalDate.Format("2006-01-02"))
}

func (PostgresDailyRepository) EnqueueDaily(
	ctx context.Context,
	scope store.AccountScope,
	target LocalTarget,
	now time.Time,
) (bool, error) {
	id, err := randomDeliveryID()
	if err != nil {
		return false, err
	}
	row := scope.InsertOnConflictDoNothingReturning(ctx, "telegram_deliveries",
		[]string{
			"id", "source", "source_key", "message_kind", "target_local_date",
			"timezone_at_enqueue", "status", "next_attempt_at", "updated_at",
		},
		[]string{"account_id", "source", "source_key"},
		[]string{"id"},
		id, string(DeliverySourceDaily), target.LocalDate.Format("2006-01-02"), string(MessageKindDigest),
		target.LocalDate, target.Timezone, string(DeliveryStatusPending), now, now)
	var returned string
	if err := row.Scan(&returned); errors.Is(err, store.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

type DailyScheduler struct {
	accounts AccountScopeEnumerator
	targets  DailyTargetProvider
	scan     ScanEnsurer
	repo     DailyRepository
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration
	running  atomic.Bool
}

func NewDailyScheduler(
	accounts AccountScopeEnumerator,
	targets DailyTargetProvider,
	scan ScanEnsurer,
	repo DailyRepository,
	logger *slog.Logger,
) *DailyScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &DailyScheduler{
		accounts: accounts, targets: targets, scan: scan, repo: repo, logger: logger,
		now: time.Now, interval: dailyInterval,
	}
}

func (s *DailyScheduler) WithClock(now func() time.Time) *DailyScheduler {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *DailyScheduler) RunOnce(ctx context.Context) {
	if !s.running.CompareAndSwap(false, true) {
		return
	}
	defer s.running.Store(false)
	accounts, err := s.accounts.AccountScopes(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "telegram daily enumerate accounts failed", "error", err)
		return
	}
	for _, account := range accounts {
		if ctx.Err() != nil {
			return
		}
		target, due, err := s.targets.DailyDue(ctx, account, s.now().UTC())
		if err != nil {
			s.logger.ErrorContext(ctx, "telegram daily due check failed",
				"account_id", account.AccountID, "error", err)
			continue
		}
		if !due {
			continue
		}
		exists, err := s.repo.DailyExists(ctx, account.Scope, target)
		if err != nil {
			s.logger.ErrorContext(ctx, "telegram daily existence check failed",
				"account_id", account.AccountID, "target_local_date", target.LocalDate.Format("2006-01-02"), "error", err)
			continue
		}
		if exists {
			continue
		}
		if err := s.scan.EnsureScan(ctx, account, target); err != nil {
			s.logger.ErrorContext(ctx, "telegram daily scan failed; will retry next tick",
				"account_id", account.AccountID, "target_local_date", target.LocalDate.Format("2006-01-02"), "error", err)
			continue
		}
		if _, err := s.repo.EnqueueDaily(ctx, account.Scope, target, s.now().UTC()); err != nil {
			s.logger.ErrorContext(ctx, "telegram daily enqueue failed",
				"account_id", account.AccountID, "target_local_date", target.LocalDate.Format("2006-01-02"), "error", err)
		}
	}
}

func (s *DailyScheduler) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	s.RunOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RunOnce(ctx)
		}
	}
}

var _ TargetProvider = SettingsTargetProvider{}
