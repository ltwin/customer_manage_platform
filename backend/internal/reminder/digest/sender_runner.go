package digest

import (
	"context"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	deliverySenderInterval      = time.Second
	maxDeliveriesPerAccountTick = 20
)

type AccountDeliverySender interface {
	SendNext(context.Context, store.ScopedAccount) (bool, error)
}

type DeliverySenderRunner struct {
	accounts    AccountScopeEnumerator
	sender      AccountDeliverySender
	integration *IntegrationState
	interval    time.Duration
}

func NewDeliverySenderRunner(accounts AccountScopeEnumerator, sender AccountDeliverySender) *DeliverySenderRunner {
	return &DeliverySenderRunner{accounts: accounts, sender: sender, interval: deliverySenderInterval}
}

func (r *DeliverySenderRunner) WithIntegrationState(state *IntegrationState) *DeliverySenderRunner {
	r.integration = state
	return r
}

func (r *DeliverySenderRunner) RunOnce(ctx context.Context) {
	if r.integration != nil && r.integration.Suspended() {
		return
	}
	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		return
	}
	for _, account := range accounts {
		for range maxDeliveriesPerAccountTick {
			if ctx.Err() != nil || (r.integration != nil && r.integration.Suspended()) {
				return
			}
			processed, err := r.sender.SendNext(ctx, account)
			if err != nil || !processed {
				break
			}
		}
	}
}

func (r *DeliverySenderRunner) Run(ctx context.Context) {
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

var _ AccountDeliverySender = (*DeliverySender)(nil)
