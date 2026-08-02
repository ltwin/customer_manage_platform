package customer

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
	maintenanceInterval    = time.Hour
	reconciliationPageSize = 100
	maxDueGCPerAccount     = 100
	pointerAuditAttempts   = 3
)

type AccountScopeEnumerator interface {
	AccountScopes(context.Context) ([]store.ScopedAccount, error)
}

type AvatarMaintenanceRepository interface {
	AvatarRepository
	LoadCheckpoint(context.Context, store.AccountScope) (AvatarReconciliationCheckpoint, error)
	SaveCheckpoint(context.Context, store.TxAccountScope, AvatarReconciliationCheckpoint, time.Time) error
	ListCurrentPointers(context.Context, store.AccountScope, string, int) ([]Customer, error)
	ListDueGC(context.Context, store.AccountScope, time.Time, int) ([]AvatarGCItem, error)
	LoadGCForUpdate(context.Context, store.TxAccountScope, AvatarGCItem) (AvatarGCItem, error)
	DeleteGC(context.Context, store.TxAccountScope, AvatarGCItem) error
	RecordGCFailure(context.Context, store.TxAccountScope, AvatarGCItem, time.Time) error
}

type AvatarMaintenanceRunner struct {
	accounts AccountScopeEnumerator
	repo     AvatarMaintenanceRepository
	objects  AvatarObjectStore
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration
	running  atomic.Bool
}

func NewAvatarMaintenanceRunner(
	accounts AccountScopeEnumerator,
	repo AvatarMaintenanceRepository,
	objects AvatarObjectStore,
	logger *slog.Logger,
) *AvatarMaintenanceRunner {
	return &AvatarMaintenanceRunner{
		accounts: accounts, repo: repo, objects: objects, logger: logger,
		now: time.Now, interval: maintenanceInterval,
	}
}

func (r *AvatarMaintenanceRunner) Run(ctx context.Context) {
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

func (r *AvatarMaintenanceRunner) RunOnce(ctx context.Context) {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	defer r.running.Store(false)
	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		r.logError("avatar maintenance account enumeration failed", err)
		return
	}
	for _, account := range accounts {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := r.reconcileAccount(ctx, account); err != nil {
			r.logError("avatar reconciliation failed", err, slog.String("account_id", account.AccountID))
		}
		if err := r.collectAccount(ctx, account); err != nil {
			r.logError("avatar gc failed", err, slog.String("account_id", account.AccountID))
		}
	}
}

func (r *AvatarMaintenanceRunner) reconcileAccount(ctx context.Context, account store.ScopedAccount) error {
	checkpoint, err := r.repo.LoadCheckpoint(ctx, account.Scope)
	if err != nil {
		return err
	}
	prefix, err := avatarmedia.AccountPrefix(account.AccountID)
	if err != nil {
		return err
	}
	page, err := r.objects.List(ctx, prefix, avatarmedia.CursorFromRaw(checkpoint.ObjectCursor), reconciliationPageSize)
	if err != nil {
		return err
	}
	err = account.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		for _, item := range page.Items {
			objectAccountID, customerID, ref, err := ParseAvatarObjectKey(item.Key.String())
			if err != nil || objectAccountID != account.AccountID {
				continue
			}
			current, err := r.repo.Lock(ctx, tx, customerID)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			currentRef, hasCurrent := optionalObjectRefFromCustomer(current)
			if hasCurrent && currentRef == ref {
				continue
			}
			if err := r.repo.EnqueueGC(ctx, tx, customerID, ref, r.now().UTC().Add(avatarGCGrace)); err != nil {
				return err
			}
		}
		if page.Done {
			checkpoint.ObjectCursor = ""
			checkpoint.ObjectCycle++
		} else {
			checkpoint.ObjectCursor = page.NextCursor.String()
		}
		return r.repo.SaveCheckpoint(ctx, tx, checkpoint, r.now().UTC())
	})
	if err != nil {
		return err
	}

	pointers, err := r.repo.ListCurrentPointers(ctx, account.Scope, checkpoint.PointerCursor, reconciliationPageSize)
	if err != nil {
		return err
	}
	validator := AvatarApplication{objects: r.objects}
	for _, current := range pointers {
		valid, verifyErr := auditCurrentPointer(ctx, &validator, current)
		if verifyErr != nil {
			if errors.Is(verifyErr, context.Canceled) || errors.Is(verifyErr, context.DeadlineExceeded) {
				return verifyErr
			}
			r.logError("avatar current pointer audit temporary", verifyErr,
				slog.String("account_id", account.AccountID), slog.String("customer_id", current.ID),
				slog.Int("attempts", pointerAuditAttempts))
		} else if !valid {
			r.logError("avatar current pointer integrity failed", nil,
				slog.String("account_id", account.AccountID), slog.String("customer_id", current.ID))
		}
		checkpoint.PointerCursor = current.ID
	}
	if len(pointers) < reconciliationPageSize {
		checkpoint.PointerCursor = ""
		checkpoint.PointerCycle++
	}
	return account.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return r.repo.SaveCheckpoint(ctx, tx, checkpoint, r.now().UTC())
	})
}

func auditCurrentPointer(ctx context.Context, validator *AvatarApplication, current Customer) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < pointerAuditAttempts; attempt++ {
		valid, err := validator.verifyCurrent(ctx, current)
		if err == nil {
			return valid, nil
		}
		lastErr = err
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return false, lastErr
}

func (r *AvatarMaintenanceRunner) collectAccount(ctx context.Context, account store.ScopedAccount) error {
	items, err := r.repo.ListDueGC(ctx, account.Scope, r.now().UTC(), maxDueGCPerAccount)
	if err != nil {
		return err
	}
	for _, item := range items {
		err := account.Scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			current, err := r.repo.Lock(ctx, tx, item.CustomerID)
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
			if current.AvatarObjectID != nil && *current.AvatarObjectID == claimed.Ref.AvatarObjectID {
				return r.repo.DeleteGC(ctx, tx, claimed)
			}
			key, err := AvatarObjectKey(account.AccountID, item.CustomerID, claimed.Ref)
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

func (r *AvatarMaintenanceRunner) logError(message string, err error, attrs ...slog.Attr) {
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
