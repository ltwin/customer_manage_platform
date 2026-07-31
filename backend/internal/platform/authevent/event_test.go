package authevent

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestEventProtocolCoversSevenNamesWithOnlyAllowlistedFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	dryRun := false
	events := []Event{
		{Timestamp: now, Name: Login, Result: ResultSuccess, AccountRef: "account:abc123", SessionRef: "session:def456"},
		{Timestamp: now, Name: EmailVerified, Result: ResultSuccess, AccountRef: "account:abc123", SessionRef: "session:def456"},
		{Timestamp: now, Name: RateLimited, Result: ResultFailure, FailureClass: "rate_limited", Action: "login", SourceDigest: "v1:" + strings.Repeat("a", 43)},
		{Timestamp: now, Name: RefreshReuse, Result: ResultFailure, FailureClass: "reuse", FamilyRef: "family:abc123"},
		{Timestamp: now, Name: MailDelivery, Result: ResultSuccess, Action: "email_verification", ProviderMessageID: "provider-123"},
		{Timestamp: now, Name: PasswordChanged, Result: ResultSuccess, Action: "change_password", AccountRef: "account:abc123"},
		{Timestamp: now, Name: LegacyClaim, Result: ResultFailure, FailureClass: "not_ready", AccountRef: "account:abc123", DryRun: &dryRun},
	}
	allowed := map[string]bool{
		"time": true, "level": true, "msg": true, "event": true, "result": true,
		"failure_class": true, "account_ref": true, "session_ref": true, "family_ref": true,
		"action": true, "source_digest": true, "provider_message_id": true, "dry_run": true,
	}
	for _, event := range events {
		if err := event.Validate(); err != nil {
			t.Fatalf("validate %s: %v", event.Name, err)
		}
		var output bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&output, nil))
		logger.LogAttrs(t.Context(), slog.LevelInfo, "auth event", event.Attrs()...)
		var record map[string]any
		if err := json.Unmarshal(output.Bytes(), &record); err != nil {
			t.Fatalf("decode %s: %v", event.Name, err)
		}
		for key := range record {
			if !allowed[key] {
				t.Fatalf("event %s emitted non-allowlisted key %q", event.Name, key)
			}
		}
	}
}

func TestEventProtocolRejectsUnknownFailureAndRawSource(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	for _, event := range []Event{
		{Timestamp: now, Name: RefreshReuse, Result: ResultFailure, FailureClass: "provider_body_detail"},
		{Timestamp: now, Name: RateLimited, Result: ResultFailure, FailureClass: "rate_limited", Action: "login", SourceDigest: "192.0.2.1"},
	} {
		if err := event.Validate(); err == nil {
			t.Fatalf("invalid event passed validation: %#v", event)
		}
	}
}
