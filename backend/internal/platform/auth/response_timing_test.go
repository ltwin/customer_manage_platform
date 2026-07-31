package auth

import (
	"context"
	"io"
	"net/netip"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestLoginUsesOneCostEquivalentCompareAndResponseBudget(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		found bool
	}{
		{name: "existing account wrong password", found: true},
		{name: "missing account", found: false},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := newAuthTimingClock(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
			repo := repositoryStub{findLogin: func(context.Context, string) (LoginRecord, bool, error) {
				clock.Advance(20 * time.Millisecond)
				return LoginRecord{
					Account:      Account{ID: "account-1", Status: AccountActive},
					PasswordHash: dummyPasswordHash,
				}, test.found, nil
			}}
			service := NewService(
				repo,
				NewTokenIssuer("synthetic-root"),
				WithAttemptLimiter(&attemptLimiterStub{allowed: true}),
				WithAuthClock(clock.Now),
				WithAuthResponseDelay(clock.Delay),
				WithAuthTimingRandom(zeroReader{}),
			)
			compareCalls := 0
			service.passwordMatches = func(hash, password string) bool {
				compareCalls++
				return false
			}

			started := clock.Now()
			_, err := service.Login(context.Background(), syntheticEmail(), "wrong-password", ClientMeta{
				SourceIP: netip.MustParseAddr("198.51.100.10"),
			})
			if !IsAuthError(err, AuthErrorUnauthorized) {
				t.Fatalf("login error = %v, want unauthorized", err)
			}
			if compareCalls != 1 {
				t.Fatalf("password compares = %d, want exactly one", compareCalls)
			}
			if elapsed := clock.Now().Sub(started); elapsed != 300*time.Millisecond {
				t.Fatalf("response budget elapsed = %s, want 300ms", elapsed)
			}
		})
	}
}

func TestDummyPasswordHashMatchesProductionCost(t *testing.T) {
	t.Parallel()
	if err := validateDummyPasswordHash(); err != nil {
		t.Fatal(err)
	}
}

func TestPasswordResetMailBranchesMeetStatisticalResponseBudget(t *testing.T) {
	t.Parallel()
	type branch struct {
		name      string
		replaced  bool
		mailError error
	}
	branches := []branch{
		{name: "missing account"},
		{name: "accepted delivery", replaced: true},
		{name: "provider failure", replaced: true, mailError: NewDeliveryError(DeliveryTemporarilyUnavailable)},
	}
	samples := make(map[string][]time.Duration, len(branches))
	for _, test := range branches {
		clock := newAuthTimingClock(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
		repo := repositoryStub{replacePasswordReset: func(
			context.Context, string, string, []byte, time.Time, time.Time,
		) (bool, error) {
			clock.Advance(20 * time.Millisecond)
			return test.replaced, nil
		}}
		sender := timingMailSender{clock: clock, latency: 800 * time.Millisecond, err: test.mailError}
		service := NewService(
			repo,
			NewTokenIssuer("synthetic-root"),
			WithAttemptLimiter(&attemptLimiterStub{allowed: true}),
			WithAuthClock(clock.Now),
			WithAuthResponseDelay(clock.Delay),
			WithAuthTimingRandom(zeroReader{}),
			WithAuthMailSender(sender),
			WithPublicBaseURL("https://app.example.invalid"),
		)
		for index := 0; index < 220; index++ {
			started := clock.Now()
			if _, err := service.BeginPasswordReset(context.Background(), syntheticEmail(), ClientMeta{
				SourceIP: netip.MustParseAddr("198.51.100.20"),
			}); err != nil {
				t.Fatalf("%s sample %d: %v", test.name, index, err)
			}
			elapsed := clock.Now().Sub(started)
			if elapsed < time.Second || elapsed > 1050*time.Millisecond {
				t.Fatalf("%s sample %d elapsed = %s, want [1s,1.05s]", test.name, index, elapsed)
			}
			if index >= 20 {
				samples[test.name] = append(samples[test.name], elapsed)
			}
		}
	}

	for left := 0; left < len(branches); left++ {
		for right := left + 1; right < len(branches); right++ {
			assertTimingEquivalent(t, branches[left].name, samples[branches[left].name], branches[right].name, samples[branches[right].name])
		}
	}
}

func TestEveryLimitedPublicAuthActionUsesItsResponseBudget(t *testing.T) {
	t.Parallel()
	wire, _, _, err := newBearerToken(zeroReader{})
	if err != nil {
		t.Fatalf("create synthetic action token: %v", err)
	}
	meta := ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.30")}
	tests := []struct {
		name string
		want time.Duration
		run  func(context.Context, *Service) error
	}{
		{name: "register", want: time.Second, run: func(ctx context.Context, service *Service) error {
			_, err := service.Register(ctx, syntheticEmail(), "twelve-bytes", meta)
			return err
		}},
		{name: "resend", want: time.Second, run: func(ctx context.Context, service *Service) error {
			_, err := service.ResendVerification(ctx, syntheticEmail(), meta)
			return err
		}},
		{name: "verify", want: 300 * time.Millisecond, run: func(ctx context.Context, service *Service) error {
			_, err := service.VerifyEmail(ctx, wire, meta)
			return err
		}},
		{name: "reset", want: 300 * time.Millisecond, run: func(ctx context.Context, service *Service) error {
			return service.ResetPassword(ctx, wire, "new-password-1", meta)
		}},
		{name: "change", want: 300 * time.Millisecond, run: func(ctx context.Context, service *Service) error {
			return service.ChangePassword(ctx, AccountContext{AccountID: "account-1"}, "current-pass-1", "new-password-1", meta)
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := newAuthTimingClock(time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
			service := NewService(
				repositoryStub{},
				NewTokenIssuer("synthetic-root"),
				WithAttemptLimiter(&attemptLimiterStub{allowed: true}),
				WithAuthClock(clock.Now),
				WithAuthResponseDelay(clock.Delay),
				WithAuthTimingRandom(zeroReader{}),
				WithPublicBaseURL("https://app.example.invalid"),
			)
			service.passwordMatches = func(string, string) bool { return false }
			started := clock.Now()
			_ = test.run(context.Background(), service)
			if elapsed := clock.Now().Sub(started); elapsed != test.want {
				t.Fatalf("elapsed = %s, want %s", elapsed, test.want)
			}
		})
	}
}

func assertTimingEquivalent(t *testing.T, leftName string, left []time.Duration, rightName string, right []time.Duration) {
	t.Helper()
	leftMedian, leftP95 := timingPercentiles(left)
	rightMedian, rightP95 := timingPercentiles(right)
	medianDelta := absoluteDuration(leftMedian - rightMedian)
	p95Delta := absoluteDuration(leftP95 - rightP95)
	ratio := float64(leftP95) / float64(rightP95)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	if medianDelta > 15*time.Millisecond || p95Delta > 30*time.Millisecond || ratio > 1.25 {
		t.Fatalf(
			"timing mismatch %s/%s: median_delta=%s p95_delta=%s p95_ratio=%.3f",
			leftName, rightName, medianDelta, p95Delta, ratio,
		)
	}
}

func timingPercentiles(samples []time.Duration) (time.Duration, time.Duration) {
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left] < ordered[right] })
	return ordered[len(ordered)/2], ordered[(95*len(ordered)+99)/100-1]
}

func absoluteDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return -duration
	}
	return duration
}

type authTimingClock struct {
	mu  sync.Mutex
	now time.Time
}

func newAuthTimingClock(now time.Time) *authTimingClock {
	return &authTimingClock{now: now}
}

func (c *authTimingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *authTimingClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func (c *authTimingClock) Delay(ctx context.Context, duration time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.Advance(duration)
	return nil
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}

var _ io.Reader = zeroReader{}

type timingMailSender struct {
	clock   *authTimingClock
	latency time.Duration
	err     error
}

func (s timingMailSender) Send(context.Context, AuthMail) (MailReceipt, error) {
	s.clock.Advance(s.latency)
	return MailReceipt{ProviderMessageID: "synthetic-provider-id", AcceptedAt: s.clock.Now()}, s.err
}
