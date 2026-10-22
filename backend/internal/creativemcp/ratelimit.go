package creativemcp

import (
	"sync"
	"time"
)

const (
	defaultCallsPerMinute = 120
	limiterWindow         = time.Minute
)

// limiter is a sliding window of tool calls per connection. It lives in
// process memory: the deployment is one process, and a restart only forgets a
// minute of history.
type limiter struct {
	mu    sync.Mutex
	limit int
	now   func() time.Time
	calls map[string][]time.Time
}

func newLimiter(limit int, now func() time.Time) *limiter {
	if limit <= 0 {
		limit = defaultCallsPerMinute
	}
	if now == nil {
		now = time.Now
	}
	return &limiter{limit: limit, now: now, calls: map[string][]time.Time{}}
}

// allow records a call, or says how many seconds until the next one fits.
func (l *limiter) allow(connection string) (int, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	kept := l.calls[connection][:0]
	for _, at := range l.calls[connection] {
		if now.Sub(at) < limiterWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) >= l.limit {
		l.calls[connection] = kept
		wait := limiterWindow - now.Sub(kept[0])
		return max(1, int((wait+time.Second-1)/time.Second)), false
	}
	l.calls[connection] = append(kept, now)
	if len(l.calls) > 10000 {
		for key, times := range l.calls {
			if len(times) == 0 || now.Sub(times[len(times)-1]) >= limiterWindow {
				delete(l.calls, key)
			}
		}
	}
	return 0, true
}
