package digest

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const telegramLongPollTimeout = 30 * time.Second

var errUpdateNotDurable = errors.New("telegram update did not reach durable terminal outcome")

type UpdateProcessor interface {
	HandleUpdate(context.Context, Update) (bool, error)
}

type Poller struct {
	source      TelegramUpdateSource
	processor   UpdateProcessor
	integration *IntegrationState
	mu          sync.Mutex
	nextOffset  int64
}

func NewPoller(source TelegramUpdateSource, processor UpdateProcessor) *Poller {
	return &Poller{source: source, processor: processor}
}

func (p *Poller) WithIntegrationState(state *IntegrationState) *Poller {
	p.integration = state
	return p
}

func (p *Poller) PollOnce(ctx context.Context) error {
	updates, err := p.source.GetUpdates(ctx, p.NextOffset(), telegramLongPollTimeout)
	if err != nil {
		return err
	}
	sort.SliceStable(updates, func(i, j int) bool { return updates[i].ID < updates[j].ID })
	for _, update := range updates {
		terminal, err := p.processor.HandleUpdate(ctx, update)
		if err != nil {
			return err
		}
		if !terminal {
			return errUpdateNotDurable
		}
		p.mu.Lock()
		if update.ID+1 > p.nextOffset {
			p.nextOffset = update.ID + 1
		}
		p.mu.Unlock()
	}
	return nil
}

func (p *Poller) NextOffset() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nextOffset
}

func (p *Poller) Run(ctx context.Context) {
	failures := 0
	for ctx.Err() == nil {
		if p.integration != nil {
			if delay := p.integration.RetryAfter(); delay > 0 {
				if !waitForPollDelay(ctx, delay) {
					return
				}
				continue
			}
		}
		err := p.PollOnce(ctx)
		if err == nil {
			failures = 0
			continue
		}
		if ctx.Err() != nil {
			return
		}
		failures++
		delay := pollBackoff(failures)
		var telegramErr *TelegramError
		if errors.As(err, &telegramErr) {
			if policy, ok := TelegramPolicyFor(TelegramOperationPoll, telegramErr.Kind, failures, telegramErr.RetryAfter); ok {
				delay = policy.Delay
				if policy.Action == TelegramPolicySuspendIntegration && p.integration != nil {
					p.integration.Suspend(telegramErr.Kind, policy.Delay)
				}
			}
		}
		if !waitForPollDelay(ctx, delay) {
			return
		}
	}
}

func waitForPollDelay(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
