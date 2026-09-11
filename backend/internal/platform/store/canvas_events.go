package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// A single dedicated LISTEN connection serves every canvas subscription in this
// process. PostgreSQL delivers notifications only after the writing transaction
// commits, including writes made by other API instances and creative workers.
type canvasEventKey struct {
	Account string `json:"Account"`
	Canvas  string `json:"Canvas"`
}
type canvasEventHub struct {
	mu          sync.Mutex
	subscribers map[canvasEventKey]map[chan struct{}]struct{}
	ready       bool
	stopped     bool
	cancel      context.CancelFunc
	done        chan struct{}
}

// SubscribeCanvas invalidates snapshots; it is not an event log. The caller must
// authorize the canvas first. The initial signal is sent only AFTER LISTEN has
// committed; every listener reconnect invalidates all snapshots to cover gaps.
func (s *Store) SubscribeCanvas(ctx context.Context, scope AccountScope, id string) (<-chan struct{}, func(), error) {
	if scope.accountID == "" || scope.pool != s.pool {
		return nil, nil, ErrEmptyAccountScope
	}
	s.canvasEventsOnce.Do(func() {
		runCtx, cancel := context.WithCancel(context.Background())
		h := &canvasEventHub{subscribers: make(map[canvasEventKey]map[chan struct{}]struct{}), cancel: cancel, done: make(chan struct{})}
		s.canvasEvents = h
		config := s.pool.Config().ConnConfig.Copy()
		go h.run(runCtx, config)
	})
	h := s.canvasEvents
	if h == nil {
		return nil, nil, errors.New("canvas event listener stopped")
	}
	key := canvasEventKey{Account: scope.accountID, Canvas: id}
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil, nil, errors.New("canvas event listener stopped")
	}
	if h.subscribers[key] == nil {
		h.subscribers[key] = make(map[chan struct{}]struct{})
	}
	h.subscribers[key][ch] = struct{}{}
	if h.ready {
		ch <- struct{}{}
	}
	h.mu.Unlock()
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers[key], ch)
			if len(h.subscribers[key]) == 0 {
				delete(h.subscribers, key)
			}
			h.mu.Unlock()
		})
	}
	select {
	case <-ch:
		h.mu.Lock()
		if h.stopped {
			h.mu.Unlock()
			unsubscribe()
			return nil, nil, errors.New("canvas event listener stopped")
		}
		select {
		case ch <- struct{}{}:
		default:
		}
		h.mu.Unlock()
		return ch, unsubscribe, nil
	case <-ctx.Done():
		unsubscribe()
		return nil, nil, ctx.Err()
	case <-h.done:
		unsubscribe()
		return nil, nil, errors.New("canvas event listener stopped")
	}
}

// StopCanvasEvents closes live streams before HTTP shutdown waits for handlers.
// It is idempotent and permanently prevents new subscriptions on this Store.
func (s *Store) StopCanvasEvents() {
	s.canvasEventsOnce.Do(func() {})
	if s.canvasEvents != nil {
		s.canvasEvents.close()
	}
}

func (h *canvasEventHub) close() { h.cancel(); <-h.done }

func (h *canvasEventHub) invalidate(key canvasEventKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for target, subscribers := range h.subscribers {
		if key.Account != "" && (target.Account != key.Account || (key.Canvas != "" && target.Canvas != key.Canvas)) {
			continue
		}
		for ch := range subscribers {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

func (h *canvasEventHub) run(ctx context.Context, config *pgx.ConnConfig) {
	defer func() {
		h.mu.Lock()
		h.stopped = true
		h.ready = false
		for _, subscribers := range h.subscribers {
			for ch := range subscribers {
				close(ch)
			}
		}
		h.mu.Unlock()
		close(h.done)
	}()
	delay := time.Second
	for ctx.Err() == nil {
		connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		conn, err := pgx.ConnectConfig(connectCtx, config)
		if err == nil {
			_, err = conn.Exec(connectCtx, "LISTEN creative_canvas_changed")
		}
		cancel()
		if err == nil {
			h.mu.Lock()
			h.ready = true
			h.mu.Unlock()
			h.invalidate(canvasEventKey{})
			delay = time.Second
			for ctx.Err() == nil {
				waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
				notification, waitErr := conn.WaitForNotification(waitCtx)
				waitCancel()
				if errors.Is(waitErr, context.DeadlineExceeded) && ctx.Err() == nil {
					pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
					pingErr := conn.Ping(pingCtx)
					pingCancel()
					if pingErr == nil {
						continue
					}
				}
				if waitErr != nil {
					break
				}
				var key canvasEventKey
				if json.Unmarshal([]byte(notification.Payload), &key) == nil && key.Account != "" {
					h.invalidate(key)
				}
			}
		}
		h.mu.Lock()
		h.ready = false
		h.mu.Unlock()
		if conn != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
			_ = conn.Close(closeCtx)
			closeCancel()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
