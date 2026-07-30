package digest

import (
	"context"
	"testing"
	"time"
)

type blockingDigestLoop struct {
	started chan struct{}
	stopped chan struct{}
}

func newBlockingDigestLoop() *blockingDigestLoop {
	return &blockingDigestLoop{started: make(chan struct{}), stopped: make(chan struct{})}
}

func (l *blockingDigestLoop) Run(ctx context.Context) {
	close(l.started)
	<-ctx.Done()
	close(l.stopped)
}

func TestTelegramRunnerStartsAllLoopsAndStopsOnCancellation(t *testing.T) {
	poller := newBlockingDigestLoop()
	daily := newBlockingDigestLoop()
	sender := newBlockingDigestLoop()
	runner := NewTelegramRunner(poller, daily, sender, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.Run(ctx)
		close(done)
	}()

	for name, started := range map[string]<-chan struct{}{
		"poller": poller.started,
		"daily":  daily.started,
		"sender": sender.started,
	} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("%s did not start", name)
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("TelegramRunner did not stop after cancellation")
	}
	for name, stopped := range map[string]<-chan struct{}{
		"poller": poller.stopped,
		"daily":  daily.stopped,
		"sender": sender.stopped,
	} {
		select {
		case <-stopped:
		default:
			t.Fatalf("%s did not observe cancellation", name)
		}
	}
}
