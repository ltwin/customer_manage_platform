package digest

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTelegramErrorIsTypedAndRedacted(t *testing.T) {
	err := &TelegramError{
		Kind:       TelegramErrorRateLimited,
		Code:       "429",
		RetryAfter: 45 * time.Second,
	}
	wrapped := errors.New(err.Error())
	if strings.Contains(err.Error(), "token") || strings.Contains(err.Error(), "payload") {
		t.Fatalf("error string contains sensitive material: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "rate_limited") || !strings.Contains(err.Error(), "429") {
		t.Fatalf("error string lacks safe classification: %q", err.Error())
	}
	var target *TelegramError
	if !errors.As(err, &target) || target.RetryAfter != 45*time.Second {
		t.Fatalf("typed error was not preserved: %#v", target)
	}
	if errors.As(wrapped, &target) {
		t.Fatal("plain string wrapping must not recreate typed TelegramError")
	}
}

func TestTelegramErrorKindsAreExhaustive(t *testing.T) {
	got := TelegramErrorKinds()
	want := []TelegramErrorKind{
		TelegramErrorNetwork,
		TelegramErrorTimeout,
		TelegramErrorServer,
		TelegramErrorRateLimited,
		TelegramErrorWebhookConflict,
		TelegramErrorInvalidAuth,
		TelegramErrorClient,
		TelegramErrorProtocol,
	}
	if len(got) != len(want) {
		t.Fatalf("want %d kinds, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("kind[%d]: want %q, got %q", i, want[i], got[i])
		}
	}
}
