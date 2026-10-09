package creativecanvas

import (
	"context"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/platform/realtime"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CheckReadAccess authorizes an event subscription without loading node content.
func CheckReadAccess(ctx context.Context, scope store.AccountScope, id string) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		_, err := lockCanvas(ctx, tx, id, false)
		return err
	})
}

// EventSource is the canvas stream: it has no event log, so every wake-up is
// one invalidation and the client re-reads the snapshot. A reconnect therefore
// needs no cursor — its first wake-up invalidates too.
type EventSource struct{}

var _ realtime.Source = EventSource{}

func (EventSource) Kind() string { return "canvas" }

func (EventSource) Authorize(ctx context.Context, scope store.AccountScope, id string) error {
	return lostAccess(CheckReadAccess(ctx, scope, id))
}

func (EventSource) Cursor(context.Context, store.AccountScope, string, string) (string, error) {
	return "", nil
}

// ReadAfter rechecks ownership and capability before disclosing that this
// canvas changed.
func (s EventSource) ReadAfter(ctx context.Context, scope store.AccountScope, id, _ string, _ int) ([]realtime.Event, bool, error) {
	if err := s.Authorize(ctx, scope, id); err != nil {
		return nil, false, err
	}
	return []realtime.Event{{Type: "invalidate"}}, false, nil
}

// lostAccess marks the refusals that mean "this canvas is gone for you", so a
// running stream says unavailable instead of silently retrying. The original
// stays wrapped for the JSON answer given before a stream opens.
func lostAccess(err error) error {
	if errors.Is(err, ErrNotFound) || errors.Is(err, store.ErrCreativeAccessDenied) || errors.Is(err, store.ErrNoRows) {
		return fmt.Errorf("%w: %w", realtime.ErrUnavailable, err)
	}
	return err
}
