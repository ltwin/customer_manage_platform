// Package authevent defines the only structured authentication event payload
// accepted by producers and accountctl monitoring.
package authevent

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type Name string

const (
	Login           Name = "auth.login"
	EmailVerified   Name = "auth.email_verified"
	RateLimited     Name = "auth.rate_limited"
	RefreshReuse    Name = "auth.refresh_reuse"
	MailDelivery    Name = "auth.mail_delivery"
	PasswordChanged Name = "auth.password_changed"
	LegacyClaim     Name = "auth.legacy_claim"
)

type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
)

type Event struct {
	Timestamp         time.Time `json:"timestamp"`
	Name              Name      `json:"event"`
	Result            Result    `json:"result"`
	FailureClass      string    `json:"failure_class,omitempty"`
	AccountRef        string    `json:"account_ref,omitempty"`
	SessionRef        string    `json:"session_ref,omitempty"`
	FamilyRef         string    `json:"family_ref,omitempty"`
	Action            string    `json:"action,omitempty"`
	SourceDigest      string    `json:"source_digest,omitempty"`
	ProviderMessageID string    `json:"provider_message_id,omitempty"`
	DryRun            *bool     `json:"dry_run,omitempty"`
}

func (event Event) Validate() error {
	if event.Timestamp.IsZero() {
		return fmt.Errorf("missing timestamp")
	}
	if !Supported(event.Name) {
		return fmt.Errorf("unsupported event")
	}
	if event.Result != ResultSuccess && event.Result != ResultFailure {
		return fmt.Errorf("unsupported result")
	}
	if event.Result == ResultFailure {
		if !fixedFailureClass(event.FailureClass) {
			return fmt.Errorf("unsupported failure class")
		}
	} else if event.FailureClass != "" {
		return fmt.Errorf("success contains failure class")
	}
	if event.Name == LegacyClaim {
		if event.DryRun == nil {
			return fmt.Errorf("legacy event missing dry_run")
		}
	} else if event.DryRun != nil {
		return fmt.Errorf("dry_run on non-legacy event")
	}
	if event.SourceDigest != "" && !validDigest(event.SourceDigest) {
		return fmt.Errorf("invalid source digest")
	}
	if event.Name == RateLimited && (event.Action == "" || event.SourceDigest == "") {
		return fmt.Errorf("rate limit event missing action or source")
	}
	if event.Name == MailDelivery && event.Action == "" {
		return fmt.Errorf("mail event missing action")
	}
	if event.Name == PasswordChanged && event.Action == "" {
		return fmt.Errorf("password event missing action")
	}
	if !validReference(event.AccountRef, "account:") ||
		!validReference(event.SessionRef, "session:") ||
		!validReference(event.FamilyRef, "family:") {
		return fmt.Errorf("invalid redacted reference")
	}
	return nil
}

func (event Event) Attrs() []slog.Attr {
	attrs := []slog.Attr{
		slog.String("event", string(event.Name)),
		slog.String("result", string(event.Result)),
	}
	attrs = appendStringAttr(attrs, "failure_class", event.FailureClass)
	attrs = appendStringAttr(attrs, "account_ref", event.AccountRef)
	attrs = appendStringAttr(attrs, "session_ref", event.SessionRef)
	attrs = appendStringAttr(attrs, "family_ref", event.FamilyRef)
	attrs = appendStringAttr(attrs, "action", event.Action)
	attrs = appendStringAttr(attrs, "source_digest", event.SourceDigest)
	attrs = appendStringAttr(attrs, "provider_message_id", event.ProviderMessageID)
	if event.DryRun != nil {
		attrs = append(attrs, slog.Bool("dry_run", *event.DryRun))
	}
	return attrs
}

func Supported(name Name) bool {
	switch name {
	case Login, EmailVerified, RateLimited, RefreshReuse, MailDelivery, PasswordChanged, LegacyClaim:
		return true
	default:
		return false
	}
}

func appendStringAttr(attrs []slog.Attr, key, value string) []slog.Attr {
	if value == "" {
		return attrs
	}
	return append(attrs, slog.String(key, value))
}

func fixedFailureClass(class string) bool {
	switch class {
	case "validation", "unauthorized", "email_verification_required", "invalid_or_expired_token",
		"rate_limited", "internal", "reuse", "cancelled", "timeout", "provider_rejected",
		"temporarily_unavailable", "misconfigured", "not_ready", "delivery_failed":
		return true
	default:
		return false
	}
}

func validDigest(value string) bool {
	if len(value) != 46 || !strings.HasPrefix(value, "v1:") {
		return false
	}
	for _, char := range value[3:] {
		if (char < 'A' || char > 'Z') && (char < 'a' || char > 'z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func validReference(value, prefix string) bool {
	return value == "" || (strings.HasPrefix(value, prefix) && len(value) > len(prefix))
}
