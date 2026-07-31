package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestRunBootstrapDryRunUsesPlanAndRedactsSecrets(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	application := &fakeAuthApplication{
		plan: auth.BootstrapPlan{Eligible: true, RecipientRef: "email:abc123"},
	}
	code := run(context.Background(), []string{"auth", "bootstrap", "--email", syntheticCLIEmail(), "--dry-run"}, commandDeps{
		stdout: &stdout,
		stderr: &stderr,
		getenv: func(name string) string {
			if name == accountctlPasswordEnv {
				return "twelve-bytes"
			}
			return ""
		},
		newApplication: fakeApplicationFactory(application),
	})
	if code != exitOK || application.planCalls != 1 || application.registerCalls != 0 {
		t.Fatalf("dry-run: exit=%d plan=%d register=%d stdout=%q stderr=%q",
			code, application.planCalls, application.registerCalls, stdout.String(), stderr.String())
	}
	assertCLIOutputRedacted(t, stdout.String()+stderr.String())
	if !strings.Contains(stdout.String(), `"operation":"bootstrap"`) ||
		!strings.Contains(stdout.String(), `"dry_run":true`) ||
		!strings.Contains(stdout.String(), `"eligible":true`) {
		t.Fatalf("bootstrap dry-run output = %q", stdout.String())
	}
}

func TestRunBootstrapExitMappingAndRemediation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		application *fakeAuthApplication
		wantExit    int
		wantText    string
	}{
		{
			name: "accepted",
			application: &fakeAuthApplication{register: auth.DispatchResult{
				Attempted: true,
				Delivery:  auth.DeliveryOutcome{Attempted: true, Accepted: true},
			}},
			wantExit: exitOK,
			wantText: `"status":"accepted"`,
		},
		{
			name: "delivery failure",
			application: &fakeAuthApplication{register: auth.DispatchResult{
				Attempted: true,
				Delivery: auth.DeliveryOutcome{
					Attempted: true, FailureClass: auth.DeliveryTemporarilyUnavailable,
				},
			}},
			wantExit: exitDelivery,
			wantText: "检查邮件 provider 后调用 /auth/email/resend；不要重跑 bootstrap",
		},
		{
			name:        "database no longer empty",
			application: &fakeAuthApplication{registerErr: auth.ErrBootstrapNotEmpty},
			wantExit:    exitNotReady,
			wantText:    "改走公开注册或 legacy claim",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"auth", "bootstrap", "--email", syntheticCLIEmail()}, commandDeps{
				stdout: &stdout, stderr: &stderr,
				getenv:         func(string) string { return "twelve-bytes" },
				newApplication: fakeApplicationFactory(test.application),
			})
			if code != test.wantExit || !strings.Contains(stdout.String()+stderr.String(), test.wantText) {
				t.Fatalf("exit=%d output=%q", code, stdout.String()+stderr.String())
			}
			assertCLIOutputRedacted(t, stdout.String()+stderr.String())
		})
	}
}

func TestRunLegacyClaimExitMappingAndDryRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		args     []string
		result   auth.LegacyClaimResult
		wantExit int
		wantText string
	}{
		{
			name: "dry-run ready",
			args: []string{"auth", "claim-legacy", "--email", syntheticCLIEmail(), "--dry-run"},
			result: auth.LegacyClaimResult{
				DryRun: true, State: auth.LegacyClaimReady,
				AccountIDRedacted: "account:def456", EmailRedacted: "email:abc123", LegacyCount: 1,
			},
			wantExit: exitOK,
			wantText: `"event":"auth.legacy_claim"`,
		},
		{
			name:     "no target",
			args:     []string{"auth", "claim-legacy", "--email", syntheticCLIEmail()},
			result:   auth.LegacyClaimResult{State: auth.LegacyClaimNoTarget, EmailRedacted: "email:abc123"},
			wantExit: exitNotReady,
			wantText: `"state":"no_target"`,
		},
		{
			name: "delivery failure",
			args: []string{"auth", "claim-legacy", "--email", syntheticCLIEmail()},
			result: auth.LegacyClaimResult{
				State:         auth.LegacyClaimPendingSameEmail,
				EmailRedacted: "email:abc123",
				Delivery:      auth.DeliveryOutcome{Attempted: true, FailureClass: auth.DeliveryProviderRejected},
			},
			wantExit: exitDelivery,
			wantText: "检查邮件 provider 后重跑相同 claim",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			application := &fakeAuthApplication{claim: test.result}
			code := run(context.Background(), test.args, commandDeps{
				stdout: &stdout, stderr: &stderr, getenv: func(string) string { return "" },
				newApplication: fakeApplicationFactory(application),
			})
			if code != test.wantExit || !strings.Contains(stdout.String()+stderr.String(), test.wantText) {
				t.Fatalf("exit=%d output=%q", code, stdout.String()+stderr.String())
			}
			assertCLIOutputRedacted(t, stdout.String()+stderr.String())
		})
	}
}

func TestRunRejectsPasswordArgumentAndMapsValidationOrInternal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		args        []string
		application *fakeAuthApplication
		wantExit    int
	}{
		{
			name:        "password argv forbidden",
			args:        []string{"auth", "bootstrap", "--email", syntheticCLIEmail(), "--password", "argv-secret"},
			application: &fakeAuthApplication{},
			wantExit:    exitInvalidInput,
		},
		{
			name:        "application validation",
			args:        []string{"auth", "bootstrap", "--email", syntheticCLIEmail()},
			application: &fakeAuthApplication{registerErr: fakeAuthError{kind: auth.AuthErrorValidation}},
			wantExit:    exitInvalidInput,
		},
		{
			name:        "internal",
			args:        []string{"auth", "claim-legacy", "--email", syntheticCLIEmail()},
			application: &fakeAuthApplication{claimErr: errors.New("synthetic internal")},
			wantExit:    exitInternal,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), test.args, commandDeps{
				stdout: &stdout, stderr: &stderr,
				getenv:         func(string) string { return "twelve-bytes" },
				newApplication: fakeApplicationFactory(test.application),
			})
			if code != test.wantExit {
				t.Fatalf("exit=%d want=%d output=%q", code, test.wantExit, stdout.String()+stderr.String())
			}
			assertCLIOutputRedacted(t, stdout.String()+stderr.String())
		})
	}
}

type fakeAuthApplication struct {
	plan          auth.BootstrapPlan
	planErr       error
	register      auth.DispatchResult
	registerErr   error
	claim         auth.LegacyClaimResult
	claimErr      error
	planCalls     int
	registerCalls int
}

func (a *fakeAuthApplication) PlanBootstrap(context.Context, string, string) (auth.BootstrapPlan, error) {
	a.planCalls++
	return a.plan, a.planErr
}

func (a *fakeAuthApplication) Register(context.Context, string, string) (auth.DispatchResult, error) {
	a.registerCalls++
	return a.register, a.registerErr
}

func (a *fakeAuthApplication) BeginLegacyClaim(context.Context, string, bool) (auth.LegacyClaimResult, error) {
	return a.claim, a.claimErr
}

func fakeApplicationFactory(application authApplication) applicationFactory {
	return func(context.Context, auth.RegistrationAdmissionMode, io.Writer) (authApplication, func(), error) {
		return application, func() {}, nil
	}
}

type fakeAuthError struct{ kind auth.AuthErrorKind }

func (e fakeAuthError) Error() string            { return string(e.kind) }
func (e fakeAuthError) Kind() auth.AuthErrorKind { return e.kind }

func assertCLIOutputRedacted(t *testing.T, output string) {
	t.Helper()
	for _, forbidden := range []string{
		syntheticCLIEmail(), "twelve-bytes", "argv-secret", "DATABASE_URL", "postgres://",
		"Authorization", "Bearer ", "Set-Cookie", "token=", "#token",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("CLI output contains forbidden value %q: %q", forbidden, output)
		}
	}
}

func syntheticCLIEmail() string { return "owner" + "@" + "example.invalid" }
