package creativeagent

import (
	"context"
	"log/slog"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Watch timing is variable only so tests need not wait it out.
var (
	// cancelCheckInterval bounds how often notifications make the watcher read
	// the run. A streaming turn writes events of its own, and each one wakes it.
	cancelCheckInterval = 500 * time.Millisecond
	// cancelCheckTimeout bounds one read, so a slow database delays the next
	// check rather than holding the watcher.
	cancelCheckTimeout = 2 * time.Second
)

// watchCancel ends the attempt as soon as the run is stopped or taken over
// elsewhere, instead of at the next lease renewal. Notifications only make the
// stop arrive sooner: nothing waits for the subscription, and while it is not
// live — or never becomes live — the renewal still finds the stop. The returned
// function ends the watch and waits for it.
func (s *Service) watchCancel(ctx context.Context, scope store.AccountScope, held claim, cancelAttempt context.CancelFunc) func() {
	if s.notifier == nil {
		return func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		// Subscribing waits for the listener; ctx ends that wait with the attempt.
		wake, unsubscribe, err := s.notifier.SubscribeRealtime(ctx, scope, RunEventSource{}.Kind(), held.RunID)
		if err != nil {
			if ctx.Err() == nil {
				s.logger.WarnContext(ctx, "creative agent cannot watch its run for a stop; the lease renewal will find it",
					slog.String("run_id", held.RunID), slog.String("error", err.Error()))
			}
			return
		}
		defer unsubscribe()
		var checked time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-wake:
				if !ok {
					return
				}
			}
			if wait := cancelCheckInterval - time.Since(checked); wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			checked = time.Now()
			if s.runStopped(ctx, scope, held) {
				cancelAttempt()
				return
			}
		}
	}()
	return func() { cancel(); <-exited }
}

// runStopped reports whether the claim no longer covers the run. A failed read
// answers no: the next wake-up, or the lease renewal, asks again.
func (s *Service) runStopped(ctx context.Context, scope store.AccountScope, held claim) bool {
	ctx, cancel := context.WithTimeout(ctx, cancelCheckTimeout)
	defer cancel()
	var state string
	var epoch int64
	var cancelled *time.Time
	if err := scope.QueryRow(ctx, "creative_agent_runs", "state,execution_epoch,cancel_requested_at", "id=$2", held.RunID).
		Scan(&state, &epoch, &cancelled); err != nil {
		s.logger.DebugContext(ctx, "creative agent could not check its run for a stop",
			slog.String("run_id", held.RunID), slog.String("error", err.Error()))
		return false
	}
	return cancelled != nil || epoch != held.Epoch || state != RunRunning
}
