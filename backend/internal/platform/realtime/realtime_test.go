package realtime

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fakeNotifier struct {
	mu   sync.Mutex
	subs []chan struct{}
	err  error
}

func (n *fakeNotifier) SubscribeRealtime(context.Context, store.AccountScope, string, string) (<-chan struct{}, func(), error) {
	if n.err != nil {
		return nil, nil, n.err
	}
	ch := make(chan struct{}, 1)
	ch <- struct{}{}
	n.mu.Lock()
	n.subs = append(n.subs, ch)
	n.mu.Unlock()
	return ch, func() {}, nil
}

// wake behaves like the hub: a full buffer merges wake-ups.
func (n *fakeNotifier) wake() {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, ch := range n.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// fakeSource is a seq log: events are numbered from 1 and read after a cursor.
type fakeSource struct {
	mu        sync.Mutex
	events    []string
	done      bool
	readErr   error
	authErr   error
	cursorErr error
	reads     int
}

func (s *fakeSource) Kind() string { return "test" }
func (s *fakeSource) Authorize(context.Context, store.AccountScope, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authErr
}
func (s *fakeSource) Cursor(_ context.Context, _ store.AccountScope, _ string, raw string) (string, error) {
	if s.cursorErr != nil {
		return "", s.cursorErr
	}
	if raw == "" {
		return "0", nil
	}
	return raw, nil
}
func (s *fakeSource) ReadAfter(_ context.Context, _ store.AccountScope, _ string, cursor string, limit int) ([]Event, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.readErr != nil {
		return nil, false, s.readErr
	}
	after, _ := strconv.Atoi(cursor)
	var out []Event
	for i := after; i < len(s.events) && len(out) < limit; i++ {
		out = append(out, Event{ID: strconv.Itoa(i + 1), Type: "e", Data: []byte(`"` + s.events[i] + `"`)})
	}
	return out, s.done && after+len(out) == len(s.events), nil
}
func (s *fakeSource) add(values ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, values...)
}

// frameWriter hands each flushed chunk to the test, and can be told to fail
// like a client that stopped reading.
type frameWriter struct {
	header http.Header
	frames chan string
	mu     sync.Mutex
	buf    strings.Builder
	fail   bool
	// delay makes every flush slow, like a client that keeps reading but never
	// fast enough to hit the write timeout.
	delay time.Duration
}

func newFrameWriter() *frameWriter {
	return &frameWriter{header: http.Header{}, frames: make(chan string, 5000)}
}
func (w *frameWriter) Header() http.Header { return w.header }
func (w *frameWriter) WriteHeader(int)     {}
func (w *frameWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail {
		return 0, errors.New("client too slow")
	}
	return w.buf.Write(p)
}
func (w *frameWriter) Flush() {
	time.Sleep(w.delay)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() > 0 {
		w.frames <- w.buf.String()
		w.buf.Reset()
	}
}

func next(t *testing.T, w *frameWriter) string {
	t.Helper()
	select {
	case f := <-w.frames:
		return f
	case <-time.After(3 * time.Second):
		t.Fatal("no frame")
		return ""
	}
}

func quiet(t *testing.T, w *frameWriter) {
	t.Helper()
	select {
	case f := <-w.frames:
		if !strings.HasPrefix(f, ":") {
			t.Fatalf("unexpected frame %q", f)
		}
	case <-time.After(80 * time.Millisecond):
	}
}

func serve(t *testing.T, server *Server, source Source, cursor string) (*frameWriter, chan struct{}, context.CancelFunc) {
	t.Helper()
	stream, err := server.Open(t.Context(), store.AccountScope{}, source, "x", cursor)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	w := newFrameWriter()
	finished := make(chan struct{})
	go func() { stream.Serve(ctx, w, nil); close(finished) }()
	return w, finished, cancel
}

func TestWakeupsAreOnlyHintsTheLogDecides(t *testing.T) {
	n := &fakeNotifier{}
	source := &fakeSource{events: []string{"a"}}
	// A long heartbeat isolates the wake-up path.
	w, _, cancel := serve(t, NewServer(n, Options{Heartbeat: time.Hour}), source, "")
	defer cancel()
	if got := next(t, w); got != "id: 1\nevent: e\ndata: \"a\"\n\n" {
		t.Fatalf("initial read: %q", got)
	}
	// Duplicate and merged wake-ups read again and find nothing new.
	n.wake()
	n.wake()
	quiet(t, w)
	source.add("b", "c")
	n.wake()
	n.wake()
	if got := next(t, w) + next(t, w); !strings.Contains(got, "id: 2\n") || !strings.Contains(got, "id: 3\n") {
		t.Fatalf("after merge: %q", got)
	}
	quiet(t, w)
}

func TestHeartbeatCoversALostWakeup(t *testing.T) {
	n := &fakeNotifier{}
	source := &fakeSource{}
	w, _, cancel := serve(t, NewServer(n, Options{Heartbeat: 50 * time.Millisecond}), source, "")
	defer cancel()
	source.add("lost") // committed, but no wake-up ever arrives
	for range 5 {
		if got := next(t, w); strings.Contains(got, "id: 1\n") {
			return
		}
	}
	t.Fatal("the heartbeat never read the event behind a lost wake-up")
}

func TestStreamEndsWhenSourceIsDoneOrNeedsSnapshot(t *testing.T) {
	n := &fakeNotifier{}
	done := &fakeSource{events: []string{"a", "b"}, done: true}
	w, finished, cancel := serve(t, NewServer(n, Options{Heartbeat: time.Hour, BatchLimit: 1}), done, "")
	defer cancel()
	// Batches continue until the log is exhausted, then the stream closes.
	if got := next(t, w) + next(t, w); !strings.Contains(got, "id: 2\n") {
		t.Fatalf("batches: %q", got)
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("a finished source must close the stream")
	}
	for _, tc := range []struct {
		err   error
		frame string
	}{
		{ErrSnapshotRequired, "event: control.snapshot_required\ndata: {}\n\n"},
		{ErrUnavailable, "event: unavailable\ndata: {}\n\n"},
	} {
		w, finished, cancel := serve(t, NewServer(n, Options{Heartbeat: time.Hour}), &fakeSource{readErr: tc.err}, "")
		if got := next(t, w); got != tc.frame {
			t.Fatalf("%v: %q", tc.err, got)
		}
		<-finished
		cancel()
	}
}

func TestOpenRefusesBeforeAnyByte(t *testing.T) {
	n := &fakeNotifier{}
	server := NewServer(n, Options{MaxPerAccount: 2})
	if _, err := server.Open(t.Context(), store.AccountScope{}, &fakeSource{cursorErr: ErrSnapshotRequired}, "x", "9"); !errors.Is(err, ErrSnapshotRequired) {
		t.Fatal(err)
	}
	if _, err := server.Open(t.Context(), store.AccountScope{}, &fakeSource{authErr: ErrUnavailable}, "x", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err := NewServer(&fakeNotifier{err: errors.New("down")}, Options{}).Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", ""); !errors.Is(err, ErrNotifierUnavailable) {
		t.Fatal(err)
	}
	// Refused opens gave their slots back.
	first, err := server.Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", ""); !errors.Is(err, ErrTooManyStreams) {
		t.Fatal("connection ceiling not enforced", err)
	}
	first.Close()
	first.Close() // idempotent: one slot back, not two
	if _, err = server.Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = server.Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", ""); !errors.Is(err, ErrTooManyStreams) {
		t.Fatal("double close released two slots", err)
	}
}

func TestSlowConsumerIsDisconnectedAndHeartbeatReauthenticates(t *testing.T) {
	n := &fakeNotifier{}
	source := &fakeSource{}
	w, finished, cancel := serve(t, NewServer(n, Options{Heartbeat: time.Hour}), source, "")
	defer cancel()
	w.mu.Lock()
	w.fail = true
	w.mu.Unlock()
	source.add("a")
	n.wake()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("a failing writer must end the stream")
	}

	server := NewServer(n, Options{Heartbeat: 30 * time.Millisecond})
	stream, err := server.Open(t.Context(), store.AccountScope{}, &fakeSource{}, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() {
		stream.Serve(t.Context(), newFrameWriter(), func() error { return errors.New("token expired") })
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("an expired credential must end the stream at the next heartbeat")
	}
}

// A long backlog must not postpone the checks that bound a connection: while
// it replays, the credential is still re-validated on schedule and the stream
// still ends at its lifetime, leaving the rest to the reconnect.
func TestBacklogReplayStillReauthenticatesAndEndsOnTime(t *testing.T) {
	source := &fakeSource{}
	for i := range 2000 {
		source.add(strconv.Itoa(i))
	}
	server := NewServer(&fakeNotifier{}, Options{Heartbeat: 10 * time.Millisecond, Lifetime: 60 * time.Millisecond, BatchLimit: 50})
	stream, err := server.Open(t.Context(), store.AccountScope{}, source, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	w := newFrameWriter()
	w.delay = time.Millisecond
	var mu sync.Mutex
	reauths := 0
	started := time.Now()
	ended := make(chan struct{})
	go func() {
		stream.Serve(t.Context(), w, func() error {
			mu.Lock()
			defer mu.Unlock()
			reauths++
			return nil
		})
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("backlog replay never ended")
	}
	mu.Lock()
	defer mu.Unlock()
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond || len(w.frames) >= 2000 {
		t.Fatalf("the stream outlived its lifetime: %v, %d frames sent", elapsed, len(w.frames))
	}
	if reauths == 0 {
		t.Fatal("the credential was never re-validated during the replay")
	}

	// A credential that expires mid-replay ends the stream at the next check.
	stream, err = server.Open(t.Context(), store.AccountScope{}, source, "x", "")
	if err != nil {
		t.Fatal(err)
	}
	slow := newFrameWriter()
	slow.delay = time.Millisecond
	server.options.Lifetime = time.Hour
	stopped := make(chan struct{})
	go func() {
		stream.Serve(t.Context(), slow, func() error { return errors.New("token expired") })
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("an expired credential did not stop the replay")
	}
	if len(slow.frames) >= 2000 {
		t.Fatal("the whole backlog was disclosed after the credential expired")
	}
}
