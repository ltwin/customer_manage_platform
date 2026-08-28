package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/config"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
)

func TestNewHTTPServerUsesProductionConnectionTimeouts(t *testing.T) {
	server := newHTTPServer(":0", http.NewServeMux())

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout: got %v, want 5s", server.ReadHeaderTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout: got %v, want 60s", server.IdleTimeout)
	}
}

func TestServerStartupDoesNotReferenceLegacySeed(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read server composition root: %v", err)
	}
	for _, forbidden := range []string{"EnsureDefaultAccount", "SEED_ADMIN_PASSWORD", "account-seed"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("server startup still references retired seed path %q", forbidden)
		}
	}
}

func TestServerComposesAuthReplaySweepRunner(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read server composition root: %v", err)
	}
	if !strings.Contains(string(source), "newAuthReplaySweepRunner(authService") {
		t.Fatal("server startup does not compose the auth replay ciphertext sweep runner")
	}
}

func TestAuthReplaySweepRunnerStartsImmediatelyRepeatsAndStopsOnCancel(t *testing.T) {
	sweeper := &replaySweepStub{calls: make(chan int, 3)}
	runner := newAuthReplaySweepRunner(sweeper, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.run(ctx, ticks)
	}()

	assertSweepBatch(t, sweeper.calls)
	ticks <- time.Now()
	assertSweepBatch(t, sweeper.calls)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("auth replay sweep runner did not stop after cancellation")
	}
}

func TestAuthReplaySweepRunnerLogsOnlyStableFailureFields(t *testing.T) {
	const sensitive = "ciphertext-or-database-value"
	var output bytes.Buffer
	runner := newAuthReplaySweepRunner(
		&replaySweepStub{err: errors.New(sensitive)},
		slog.New(slog.NewJSONHandler(&output, nil)),
	)

	runner.sweep(context.Background())

	logged := output.String()
	for _, expected := range []string{
		`"event":"auth.replay_ciphertext_sweep"`,
		`"result":"failed"`,
		`"failure_class":"internal"`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("auth replay sweep log missing %q: %s", expected, logged)
		}
	}
	if strings.Contains(logged, sensitive) {
		t.Fatalf("auth replay sweep log leaked raw error: %s", logged)
	}
}

type replaySweepStub struct {
	calls chan int
	err   error
}

func (s *replaySweepStub) SweepExpiredReplayCiphertexts(context.Context, int) (int64, error) {
	if s.calls != nil {
		s.calls <- authReplaySweepBatch
	}
	return 0, s.err
}

func assertSweepBatch(t *testing.T, calls <-chan int) {
	t.Helper()
	select {
	case batch := <-calls:
		if batch != authReplaySweepBatch {
			t.Fatalf("sweep batch: got %d, want %d", batch, authReplaySweepBatch)
		}
	case <-time.After(time.Second):
		t.Fatal("auth replay sweep was not called")
	}
}

func TestStartupFailureLogUsesStableClassificationWithoutRawValues(t *testing.T) {
	const (
		secretValue = "postgres://crm:secret-value@db.example/crm?sslmode=disable"
		localPath   = "/private/deployment/customer-avatar-root"
	)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	failure := newStartupFailure(
		"avatar-store-init",
		"AVATAR_LOCAL_ROOT",
		"filesystem",
		fmt.Errorf("open %s using %s", localPath, secretValue),
	)

	logStartupFailure(logger, failure)

	logged := output.String()
	for _, expected := range []string{
		`"operation":"avatar-store-init"`,
		`"config_key":"AVATAR_LOCAL_ROOT"`,
		`"error_class":"filesystem"`,
	} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("startup log missing %q: %s", expected, logged)
		}
	}
	for _, forbidden := range []string{secretValue, localPath, "secret-value", "DATABASE_URL"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("startup log leaked forbidden value %q: %s", forbidden, logged)
		}
	}
}

func TestClassifyConfigStartupFailureUsesConfigKeyAndStableClass(t *testing.T) {
	failure := classifyConfigStartupFailure(fmt.Errorf(
		"%w: %s",
		config.ErrAvatarLocalRootUnavailable,
		"/private/deployment/customer-avatar-root",
	))

	var classified *startupFailure
	if !errors.As(failure, &classified) {
		t.Fatalf("failure is not classified: %T", failure)
	}
	if classified.operation != "config-load" || classified.configKey != "AVATAR_LOCAL_ROOT" || classified.errorClass != "filesystem" {
		t.Fatalf("unexpected classification: %+v", classified)
	}
}

func TestClassifyOSSConfigStartupFailureUsesExactKey(t *testing.T) {
	tests := []struct {
		name string
		err  error
		key  string
	}{
		{name: "region", err: config.ErrOSSRegionMissing, key: "OSS_REGION"},
		{name: "bucket", err: config.ErrOSSBucketMissing, key: "OSS_BUCKET"},
		{name: "cname switch", err: config.ErrOSSUseCNameInvalid, key: "OSS_USE_CNAME"},
		{name: "cname endpoint", err: config.ErrOSSCNameEndpointMissing, key: "OSS_ENDPOINT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var classified *startupFailure
			if !errors.As(classifyConfigStartupFailure(tt.err), &classified) {
				t.Fatal("failure is not classified")
			}
			if classified.configKey != tt.key || classified.errorClass != "invalid_config" {
				t.Fatalf("unexpected classification: %+v", classified)
			}
		})
	}
}

func TestComposeObjectStoresRejectsUnknownDriver(t *testing.T) {
	_, _, err := composeObjectStores(context.Background(), config.Config{AvatarStorageDriver: "future"})
	if !errors.Is(err, config.ErrAvatarStorageDriverInvalid) {
		t.Fatalf("unknown storage driver must fail closed: %v", err)
	}
}

func TestBuildTelegramIntegrationHonorsOptionalConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		cfg         config.Config
		wantEnabled bool
		wantErr     bool
	}{
		{name: "disabled", cfg: config.Config{}},
		{name: "partial", cfg: config.Config{TelegramBotToken: "synthetic-token"}, wantErr: true},
		{
			name:        "active",
			cfg:         config.Config{TelegramBotToken: "synthetic-token", TelegramBotUsername: "synthetic_bot"},
			wantEnabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var freshness *reminder.AssignmentReminderFreshness
			if tt.wantEnabled {
				freshness = reminder.NewAssignmentReminderFreshness()
			}
			binding, runner, err := buildTelegramIntegration(tt.cfg, nil, nil, nil, freshness, nil)
			if tt.wantEnabled && freshness == nil {
				t.Fatal("active telegram requires freshness wiring")
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("error: got %v, wantErr=%v", err, tt.wantErr)
			}
			if got := binding != nil && runner != nil; got != tt.wantEnabled {
				t.Fatalf("enabled: got %v, want %v", got, tt.wantEnabled)
			}
			if !tt.wantEnabled && (binding != nil || runner != nil) {
				t.Fatal("disabled or invalid config must not expose a partial Telegram integration")
			}
		})
	}

	t.Run("active_missing_freshness_fail_closed", func(t *testing.T) {
		_, _, err := buildTelegramIntegration(
			config.Config{TelegramBotToken: "synthetic-token", TelegramBotUsername: "synthetic_bot"},
			nil, nil, nil, nil, nil,
		)
		if err == nil {
			t.Fatal("missing freshness must fail closed")
		}
	})
}

func TestWaitForRunnerUsesCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := waitForRunner(ctx, make(chan struct{}))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waitForRunner: want deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("waitForRunner exceeded bounded wait: %v", elapsed)
	}
}

func TestWaitForRunnerReturnsWhenRunnerStops(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if err := waitForRunner(context.Background(), done); err != nil {
		t.Fatalf("waitForRunner: %v", err)
	}
}

func TestServerLifecycleBoundsListenerErrorWhenRunnerDoesNotStop(t *testing.T) {
	listenerErr := errors.New("listener failed")
	serverResult := make(chan error, 1)
	serverResult <- listenerErr
	cancelCalled := false
	shutdownCalled := false
	lifecycle := serverLifecycle{
		timeout:      10 * time.Millisecond,
		cancel:       func() { cancelCalled = true },
		shutdown:     func(context.Context) error { shutdownCalled = true; return nil },
		serverResult: serverResult,
		runnerDone:   make(chan struct{}),
	}

	started := time.Now()
	err := lifecycle.wait(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait: want deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("listener error path exceeded bounded wait: %v", elapsed)
	}
	if !cancelCalled {
		t.Fatal("listener error path must cancel the runner context")
	}
	if shutdownCalled {
		t.Fatal("listener error path must not shut down an already stopped HTTP server")
	}
}

func TestServerLifecycleBoundsSignalShutdownWhenRunnerDoesNotStop(t *testing.T) {
	rootCtx, cancelRoot := context.WithCancel(context.Background())
	cancelRoot()
	cancelCalled := false
	shutdownCalled := false
	lifecycle := serverLifecycle{
		timeout:      10 * time.Millisecond,
		cancel:       func() { cancelCalled = true },
		shutdown:     func(context.Context) error { shutdownCalled = true; return nil },
		serverResult: make(chan error),
		runnerDone:   make(chan struct{}),
	}

	started := time.Now()
	err := lifecycle.wait(rootCtx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait: want deadline exceeded, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("signal path exceeded bounded wait: %v", elapsed)
	}
	if !cancelCalled {
		t.Fatal("signal path must cancel the runner context")
	}
	if !shutdownCalled {
		t.Fatal("signal path must call HTTP Shutdown")
	}
}
