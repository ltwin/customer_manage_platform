package digest

import (
	"context"
	"errors"
	"sync"
)

type RecipientGate interface {
	WithSend(context.Context, string, func(context.Context) error) error
	WithRebind(context.Context, string, func(context.Context) error) error
}

type recipientGate struct {
	mu       sync.Mutex
	accounts map[string]*recipientGateState
}

type recipientGateState struct {
	active        bool
	rebindWaiters int
	changed       chan struct{}
}

func NewRecipientGate() RecipientGate {
	return &recipientGate{accounts: make(map[string]*recipientGateState)}
}

func (g *recipientGate) WithSend(ctx context.Context, accountID string, fn func(context.Context) error) error {
	return g.with(ctx, accountID, false, fn)
}

func (g *recipientGate) WithRebind(ctx context.Context, accountID string, fn func(context.Context) error) error {
	return g.with(ctx, accountID, true, fn)
}

func (g *recipientGate) with(ctx context.Context, accountID string, rebind bool, fn func(context.Context) error) error {
	if accountID == "" || fn == nil {
		return errors.New("recipient gate requires account and callback")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state := g.state(accountID)
	registered := false
	if rebind {
		g.mu.Lock()
		state.rebindWaiters++
		registered = true
		g.signalLocked(state)
		g.mu.Unlock()
	}

	for {
		g.mu.Lock()
		if !state.active && (rebind || state.rebindWaiters == 0) {
			state.active = true
			if registered {
				state.rebindWaiters--
			}
			g.mu.Unlock()
			break
		}
		changed := state.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			if registered {
				g.mu.Lock()
				state.rebindWaiters--
				g.signalLocked(state)
				g.mu.Unlock()
			}
			return ctx.Err()
		case <-changed:
		}
	}

	defer func() {
		g.mu.Lock()
		state.active = false
		g.signalLocked(state)
		g.mu.Unlock()
	}()
	return fn(ctx)
}

func (g *recipientGate) state(accountID string) *recipientGateState {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := g.accounts[accountID]
	if state == nil {
		state = &recipientGateState{changed: make(chan struct{})}
		g.accounts[accountID] = state
	}
	return state
}

func (g *recipientGate) signalLocked(state *recipientGateState) {
	close(state.changed)
	state.changed = make(chan struct{})
}
