// Package realtime pushes committed changes to browsers over SSE. It knows no
// business object: a Source says who may watch what and reads what happened
// from the database, a Notifier only wakes readers, and Stream moves bytes.
// The database stays the source of truth, so a lost or merged wake-up never
// loses data — every wake-up just means "read again from the cursor".
package realtime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	// ErrSnapshotRequired means the cursor is outside what the log still holds
	// (pruned or ahead of it). The client must read a fresh snapshot.
	ErrSnapshotRequired = errors.New("realtime cursor needs a fresh snapshot")
	// ErrUnavailable means the watched object is gone or no longer visible to
	// this account. A running stream says so once and ends.
	ErrUnavailable = errors.New("realtime object is no longer available")
	// ErrTooManyStreams is the per-account connection ceiling.
	ErrTooManyStreams = errors.New("too many realtime streams for this account")
	// ErrNotifierUnavailable means wake-ups cannot be delivered right now.
	ErrNotifierUnavailable = errors.New("realtime notifier unavailable")
)

// Notifier wakes subscribers of a topic after a committed write. It carries no
// data and may drop or merge wake-ups. The returned channel must already hold
// one wake-up once the subscription is live, and is closed when the notifier
// shuts down.
type Notifier interface {
	SubscribeRealtime(ctx context.Context, scope store.AccountScope, kind, id string) (<-chan struct{}, func(), error)
}

// Event is one SSE frame. ID is the cursor after this event; an empty ID means
// the source has no replayable log (an invalidation) and no id line is sent.
type Event struct {
	ID   string
	Type string
	Data []byte
}

// Source is one business's view of a stream. Every method runs with the
// caller's account scope; ReadAfter must itself confirm the reader may still see
// what it discloses, because it runs on every wake-up between heartbeats.
type Source interface {
	// Kind is the topic kind the source's writes notify, e.g. "canvas".
	Kind() string
	Authorize(ctx context.Context, scope store.AccountScope, id string) error
	// Cursor validates the client's resume point. It returns ErrSnapshotRequired
	// when the log can no longer serve it.
	Cursor(ctx context.Context, scope store.AccountScope, id, raw string) (string, error)
	// ReadAfter returns at most limit events after cursor. done means the object
	// will produce nothing more and every event has now been returned.
	ReadAfter(ctx context.Context, scope store.AccountScope, id, cursor string, limit int) (events []Event, done bool, err error)
}

// Options bound one process's streams. Zero values take the defaults below.
type Options struct {
	Heartbeat     time.Duration
	Lifetime      time.Duration
	WriteTimeout  time.Duration
	MaxPerAccount int
	BatchLimit    int
}

func (o Options) withDefaults() Options {
	if o.Heartbeat <= 0 {
		o.Heartbeat = 15 * time.Second
	}
	if o.Lifetime <= 0 {
		// Periodic reconnection bounds resource lifetime and reuses normal auth refresh.
		o.Lifetime = 5 * time.Minute
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = 10 * time.Second
	}
	if o.MaxPerAccount <= 0 {
		o.MaxPerAccount = 5
	}
	if o.BatchLimit <= 0 {
		o.BatchLimit = 100
	}
	return o
}

// Server admits streams and counts them per account.
type Server struct {
	notifier Notifier
	options  Options
	mu       sync.Mutex
	open     map[string]int
}

func NewServer(notifier Notifier, options Options) *Server {
	return &Server{notifier: notifier, options: options.withDefaults(), open: map[string]int{}}
}

// Stream is one admitted subscription. Nothing has been written to the client
// yet, so every refusal before Serve can still be an ordinary JSON error.
type Stream struct {
	server  *Server
	scope   store.AccountScope
	source  Source
	id      string
	cursor  string
	wake    <-chan struct{}
	stop    func()
	release sync.Once
}

// Open runs every check that must answer before the SSE headers: the account's
// connection ceiling, authorization, the cursor and the subscription. The
// subscription is live before Serve's first read, so an event committed between
// the client's snapshot and that read is never missed.
func (s *Server) Open(ctx context.Context, scope store.AccountScope, source Source, id, rawCursor string) (*Stream, error) {
	account := scope.AccountID()
	s.mu.Lock()
	if s.open[account] >= s.options.MaxPerAccount {
		s.mu.Unlock()
		return nil, ErrTooManyStreams
	}
	s.open[account]++
	s.mu.Unlock()
	stream := &Stream{server: s, scope: scope, source: source, id: id}
	ok := false
	defer func() {
		if !ok {
			stream.Close()
		}
	}()
	if err := source.Authorize(ctx, scope, id); err != nil {
		return nil, err
	}
	cursor, err := source.Cursor(ctx, scope, id, rawCursor)
	if err != nil {
		return nil, err
	}
	stream.cursor = cursor
	setup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	stream.wake, stream.stop, err = s.notifier.SubscribeRealtime(setup, scope, source.Kind(), id)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotifierUnavailable, err)
	}
	ok = true
	return stream, nil
}

// Close releases the subscription and the account's connection slot. It is
// idempotent and Serve calls it on return.
func (st *Stream) Close() {
	st.release.Do(func() {
		if st.stop != nil {
			st.stop()
		}
		s := st.server
		s.mu.Lock()
		account := st.scope.AccountID()
		if s.open[account]--; s.open[account] <= 0 {
			delete(s.open, account)
		}
		s.mu.Unlock()
	})
}

// Serve writes the stream until the client leaves, the lifetime ends, the
// object finishes or access is lost. reauth re-validates the caller's own
// credential and the source re-validates the object once per heartbeat
// interval — on schedule even while a long backlog is being replayed.
func (st *Stream) Serve(ctx context.Context, w http.ResponseWriter, reauth func() error) {
	defer st.Close()
	options := st.server.options
	// The lifetime bounds everything the connection does, including a read or
	// a write that is in flight when it ends.
	deadline := time.Now().Add(options.Lifetime)
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store, no-transform")
	header.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	control := http.NewResponseController(w)
	session := &session{stream: st, reauth: reauth, interval: options.Heartbeat, nextCheck: time.Now().Add(options.Heartbeat)}
	session.write = func(frame string) bool {
		// A slow reader must not keep a handler blocked: it is disconnected and
		// catches up from its cursor when it reconnects.
		limit := time.Now().Add(options.WriteTimeout)
		if deadline.Before(limit) {
			limit = deadline
		}
		_ = control.SetWriteDeadline(limit)
		defer func() { _ = control.SetWriteDeadline(time.Time{}) }()
		if _, err := w.Write([]byte(frame)); err != nil {
			return false
		}
		return control.Flush() == nil
	}
	// Commit the headers now; the first wake-up is already pending.
	if control.Flush() != nil {
		return
	}
	heartbeat := time.NewTicker(options.Heartbeat)
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if !session.recheck(ctx) || !session.write(": heartbeat\n\n") {
				return
			}
			// A lost wake-up is covered here: the heartbeat also reads.
			if !session.drain(ctx) {
				return
			}
		case _, open := <-st.wake:
			if !open {
				return
			}
			if !session.drain(ctx) {
				return
			}
		}
	}
}

// session is one Serve call's moving state: when access was last confirmed
// and how to reach the client.
type session struct {
	stream    *Stream
	write     func(string) bool
	reauth    func() error
	interval  time.Duration
	nextCheck time.Time
}

// recheck confirms the caller may still watch, and says so on the stream when
// the object itself is gone. It reports whether the stream should continue.
func (s *session) recheck(ctx context.Context) bool {
	if s.reauth != nil && s.reauth() != nil {
		return false
	}
	st := s.stream
	if err := st.source.Authorize(ctx, st.scope, st.id); err != nil {
		if errors.Is(err, ErrUnavailable) {
			_ = s.write("event: unavailable\ndata: {}\n\n")
		}
		return false
	}
	s.nextCheck = time.Now().Add(s.interval)
	return true
}

// proceed is asked before every frame and every read: it ends the stream at
// its lifetime and runs the access check as soon as it is due, so neither
// waits for a backlog to finish.
func (s *session) proceed(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if time.Now().Before(s.nextCheck) {
		return true
	}
	return s.recheck(ctx)
}

// drain reads from the cursor until the log is exhausted. It reports whether
// the stream should continue.
func (s *session) drain(ctx context.Context) bool {
	st := s.stream
	limit := st.server.options.BatchLimit
	for {
		if !s.proceed(ctx) {
			return false
		}
		events, done, err := st.source.ReadAfter(ctx, st.scope, st.id, st.cursor, limit)
		switch {
		case errors.Is(err, ErrSnapshotRequired):
			_ = s.write("event: control.snapshot_required\ndata: {}\n\n")
			return false
		case errors.Is(err, ErrUnavailable):
			_ = s.write("event: unavailable\ndata: {}\n\n")
			return false
		case err != nil:
			// Transient failures close the stream so the client retries.
			return false
		}
		for _, event := range events {
			if !s.proceed(ctx) || !s.write(frame(event)) {
				return false
			}
			if event.ID != "" {
				st.cursor = event.ID
			}
		}
		if done {
			return false
		}
		if len(events) < limit {
			return true
		}
	}
}

func frame(event Event) string {
	var b strings.Builder
	if event.ID != "" {
		b.WriteString("id: " + event.ID + "\n")
	}
	b.WriteString("event: " + event.Type + "\n")
	data := event.Data
	if len(data) == 0 {
		data = []byte("{}")
	}
	// JSON never contains a raw newline, so one data line carries it whole.
	b.WriteString("data: ")
	b.Write(data)
	b.WriteString("\n\n")
	return b.String()
}
