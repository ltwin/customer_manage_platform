package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// realtimeChannel is the one PostgreSQL channel every realtime stream shares.
// Notifications only wake readers; they carry a topic, never content, and may
// be lost or merged without losing data because readers re-read the database.
const realtimeChannel = "creative_realtime"

// realtimeTopic names what changed. An empty ID in a notification reaches every
// subscriber of that kind in the account (e.g. a project rename invalidates all
// of its canvases).
type realtimeTopic struct {
	Kind    string `json:"Kind"`
	Account string `json:"Account"`
	ID      string `json:"ID"`
}

// A single dedicated LISTEN connection serves every subscription in this
// process. PostgreSQL delivers notifications only after the writing transaction
// commits, including writes made by other API instances and creative workers.
type realtimeHub struct {
	mu          sync.Mutex
	subscribers map[realtimeTopic]map[chan struct{}]struct{}
	ready       bool
	stopped     bool
	cancel      context.CancelFunc
	done        chan struct{}
}

var errRealtimeStopped = errors.New("realtime listener stopped")

// SubscribeRealtime wakes the caller whenever a committed write touches the
// topic. It is not an event log. The caller must authorize the object first;
// the account always comes from the scope. The first wake-up is sent only AFTER
// LISTEN has committed, and every listener reconnect wakes everyone to cover
// notifications missed meanwhile.
func (s *Store) SubscribeRealtime(ctx context.Context, scope AccountScope, kind, id string) (<-chan struct{}, func(), error) {
	if scope.accountID == "" || scope.pool != s.pool {
		return nil, nil, ErrEmptyAccountScope
	}
	if kind == "" || id == "" {
		return nil, nil, errors.New("realtime topic needs kind and id")
	}
	s.realtimeOnce.Do(func() {
		runCtx, cancel := context.WithCancel(context.Background())
		h := &realtimeHub{subscribers: make(map[realtimeTopic]map[chan struct{}]struct{}), cancel: cancel, done: make(chan struct{})}
		s.realtime = h
		config := s.pool.Config().ConnConfig.Copy()
		go h.run(runCtx, config)
	})
	h := s.realtime
	if h == nil {
		return nil, nil, errRealtimeStopped
	}
	key := realtimeTopic{Kind: kind, Account: scope.accountID, ID: id}
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.stopped {
		h.mu.Unlock()
		return nil, nil, errRealtimeStopped
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
			return nil, nil, errRealtimeStopped
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
		return nil, nil, errRealtimeStopped
	}
}

// StopRealtime closes live streams before HTTP shutdown waits for handlers.
// It is idempotent and permanently prevents new subscriptions on this Store.
func (s *Store) StopRealtime() {
	s.realtimeOnce.Do(func() {})
	if s.realtime != nil {
		s.realtime.close()
	}
}

func (h *realtimeHub) close() { h.cancel(); <-h.done }

func (h *realtimeHub) invalidate(key realtimeTopic) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for target, subscribers := range h.subscribers {
		// The zero topic is the reconnect broadcast; otherwise account and kind
		// must match and an empty ID covers every object of that kind.
		if key.Account != "" && (target.Account != key.Account || target.Kind != key.Kind || (key.ID != "" && target.ID != key.ID)) {
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

func (h *realtimeHub) run(ctx context.Context, config *pgx.ConnConfig) {
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
			_, err = conn.Exec(connectCtx, "LISTEN "+realtimeChannel)
		}
		cancel()
		if err == nil {
			h.mu.Lock()
			h.ready = true
			h.mu.Unlock()
			h.invalidate(realtimeTopic{})
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
				var key realtimeTopic
				if json.Unmarshal([]byte(notification.Payload), &key) == nil && key.Account != "" && key.Kind != "" {
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
