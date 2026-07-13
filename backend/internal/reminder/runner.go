package reminder

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

const scanInterval = time.Hour

// AccountScopeEnumerator 枚举账号（与 avatar runner 同形）。
type AccountScopeEnumerator interface {
	AccountScopes(context.Context) ([]store.ScopedAccount, error)
}

// SettingsService 是 runner 取时区所需的最小面。
type SettingsService interface {
	Get(ctx context.Context, scope store.AccountScope) (settings.Settings, error)
}

// ScanRunner 进程内每日扫描调度。
type ScanRunner struct {
	accounts AccountScopeEnumerator
	svc      *Service
	settings SettingsService
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration
	running  atomic.Bool
}

func NewScanRunner(
	accounts AccountScopeEnumerator,
	svc *Service,
	settingsSvc SettingsService,
	logger *slog.Logger,
) *ScanRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &ScanRunner{
		accounts: accounts,
		svc:      svc,
		settings: settingsSvc,
		logger:   logger,
		now:      time.Now,
		interval: scanInterval,
	}
}

// WithClock 注入 now 与 interval（测试用）。
func (r *ScanRunner) WithClock(now func() time.Time, interval time.Duration) *ScanRunner {
	r.now = now
	if interval > 0 {
		r.interval = interval
	}
	return r
}

// Run 阻塞至 ctx 取消；首 tick 立即执行。
func (r *ScanRunner) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	r.RunOnce(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce single-flight 扫描所有账号。
func (r *ScanRunner) RunOnce(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)

	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		r.logError("reminder scan account enumeration failed", err)
		return
	}
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := r.scanAccount(ctx, account); err != nil {
			r.logError("reminder scan failed", err, slog.String("account_id", account.AccountID))
		}
	}
}

func (r *ScanRunner) scanAccount(ctx context.Context, account store.ScopedAccount) error {
	view, err := r.settings.Get(ctx, account.Scope)
	if err != nil {
		return err
	}
	clock, err := NewAccountClock(view.Timezone)
	if err != nil {
		return err
	}
	localToday := clock.LocalDate(r.now())
	needed, err := r.svc.NeedsScan(ctx, account.Scope, localToday)
	if err != nil {
		return err
	}
	if !needed {
		return nil
	}
	result, err := r.svc.ScanAndCheckpoint(ctx, account.Scope, localToday)
	if err != nil {
		return err
	}
	r.logger.Info("reminder scan completed",
		slog.String("account_id", account.AccountID),
		slog.String("date", FormatDate(localToday)),
		slog.Int("created", result.Created),
		slog.Int("skipped", result.Skipped),
		slog.Int("auto_dismissed", result.AutoDismissed),
	)
	return nil
}

func (r *ScanRunner) logError(message string, err error, attrs ...slog.Attr) {
	if r.logger == nil {
		return
	}
	args := make([]any, 0, len(attrs)+1)
	if err != nil {
		args = append(args, slog.Any("error", err))
	}
	for _, attr := range attrs {
		args = append(args, attr)
	}
	r.logger.Error(message, args...)
}
