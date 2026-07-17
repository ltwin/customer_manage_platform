package digest

import (
	"testing"
	"time"
)

func TestTelegramPolicyIsTotalForOperationAndKind(t *testing.T) {
	for _, operation := range []TelegramOperation{TelegramOperationPoll, TelegramOperationSend} {
		for _, kind := range TelegramErrorKinds() {
			policy, ok := TelegramPolicyFor(operation, kind, 1, 17*time.Second)
			if !ok {
				t.Fatalf("undefined policy for operation=%s kind=%s", operation, kind)
			}
			if policy.Action == "" {
				t.Fatalf("empty action for operation=%s kind=%s", operation, kind)
			}
		}
	}
}

func TestPollPolicyBackoffSuspendAndRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		kind       TelegramErrorKind
		failures   int
		retryAfter time.Duration
		wantAction TelegramPolicyAction
		wantDelay  time.Duration
	}{
		{name: "network first", kind: TelegramErrorNetwork, failures: 1, wantAction: TelegramPolicyBackoff, wantDelay: time.Second},
		{name: "timeout second", kind: TelegramErrorTimeout, failures: 2, wantAction: TelegramPolicyBackoff, wantDelay: 5 * time.Second},
		{name: "server third", kind: TelegramErrorServer, failures: 3, wantAction: TelegramPolicyBackoff, wantDelay: 30 * time.Second},
		{name: "network capped", kind: TelegramErrorNetwork, failures: 9, wantAction: TelegramPolicyBackoff, wantDelay: time.Minute},
		{name: "rate respects retry after", kind: TelegramErrorRateLimited, failures: 1, retryAfter: 17 * time.Second, wantAction: TelegramPolicyBackoff, wantDelay: 17 * time.Second},
		{name: "webhook poll only", kind: TelegramErrorWebhookConflict, wantAction: TelegramPolicySuspendPoll, wantDelay: 5 * time.Minute},
		{name: "invalid auth global", kind: TelegramErrorInvalidAuth, wantAction: TelegramPolicySuspendIntegration, wantDelay: 5 * time.Minute},
		{name: "client fail visible", kind: TelegramErrorClient, wantAction: TelegramPolicySuspendPoll, wantDelay: 5 * time.Minute},
		{name: "protocol fail visible", kind: TelegramErrorProtocol, wantAction: TelegramPolicySuspendPoll, wantDelay: 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := TelegramPolicyFor(TelegramOperationPoll, tt.kind, tt.failures, tt.retryAfter)
			if !ok || got.Action != tt.wantAction || got.Delay != tt.wantDelay {
				t.Fatalf("policy=%+v ok=%v", got, ok)
			}
		})
	}
}
