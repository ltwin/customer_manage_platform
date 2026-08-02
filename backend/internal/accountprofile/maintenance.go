package accountprofile

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/avatarmedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	maintenanceInterval = time.Hour
	maxDueGCPerAccount  = 100
)

// AccountScopeEnumerator 枚举 active 账号。
type AccountScopeEnumerator interface {
	AccountScopes(context.Context) ([]store.ScopedAccount, error)
}

// MaintenanceRepository 是 GC runner 所需仓储面。
type MaintenanceRepository interface {
	Repository
}

// MaintenanceRunner 仅做 grace GC＋orphan 清理，不做 reconciliation／checkpoint。
type MaintenanceRunner struct {
	accounts AccountScopeEnumerator
	repo     MaintenanceRepository
	objects  ObjectStore
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration
	running  atomic.Bool
}

func NewMaintenanceRunner(
	accounts AccountScopeEnumerator,
	repo MaintenanceRepository,
	objects ObjectStore,
	logger *slog.Logger,
) *MaintenanceRunner {
	return &MaintenanceRunner{
		accounts: accounts, repo: repo, objects: objects, logger: logger,
		now: time.Now, interval: maintenanceInterval,
	}
}

func (r *MaintenanceRunner) Run(ctx context.Context) {
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

func (r *MaintenanceRunner) RunOnce(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)
	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		r.logError("account profile maintenance account enumeration failed", err)
		return
	}
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := r.collectAccount(ctx, account); err != nil {
			r.logError("account profile avatar gc failed", err, slog.String("account_id", account.AccountID))
		}
	}
}

func (r *MaintenanceRunner) collectAccount(ctx context.Context, account store.ScopedAccount) error {
	items, err := r.repo.ListDueGC(ctx, account.Scope, r.now().UTC(), maxDueGCPerAccount)
	if err != nil {
		return err
	}
	for _, item := range items {
		err := account.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			current, exists, err := r.repo.Lock(ctx, tx)
			if err != nil {
				return err
			}
			claimed, err := r.repo.LoadGCForUpdate(ctx, tx, item)
			if errors.Is(err, store.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if exists && current.AvatarObjectID != nil && *current.AvatarObjectID == claimed.Ref.AvatarObjectID {
				return r.repo.DeleteGC(ctx, tx, claimed)
			}
			key, err := avatarmedia.AccountProfileKey(account.AccountID, claimed.Ref)
			if err != nil {
				return err
			}
			if err := r.objects.Delete(ctx, key); err != nil {
				return err
			}
			return r.repo.DeleteGC(ctx, tx, claimed)
		})
		if err != nil {
			_ = account.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
				return r.repo.RecordGCFailure(ctx, tx, item, r.now().UTC())
			})
		}
	}
	return nil
}

func (r *MaintenanceRunner) logError(message string, err error, attrs ...slog.Attr) {
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
