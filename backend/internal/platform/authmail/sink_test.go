package authmail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestSinkAcceptsWithoutLoggingRecipientOrActionURL(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	now := time.Date(2026, 7, 31, 9, 10, 11, 0, time.UTC)
	sink := NewSink(logger, func() time.Time { return now })
	recipient := "owner" + "@" + "example.invalid"
	actionURL := "https://app.example.invalid/verify-email#token=synthetic-secret"
	receipt, err := sink.Send(context.Background(), auth.AuthMail{
		Purpose: auth.ActionEmailVerification, Recipient: recipient,
		ActionURL: actionURL, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil || receipt.ProviderMessageID == "" || !receipt.AcceptedAt.Equal(now) {
		t.Fatalf("sink receipt: has_id=%t accepted_at=%s err=%v",
			receipt.ProviderMessageID != "", receipt.AcceptedAt, err)
	}
	logged := output.String()
	if strings.Contains(logged, recipient) || strings.Contains(logged, actionURL) || strings.Contains(logged, "synthetic-secret") {
		t.Fatal("sink log leaked recipient or action bearer")
	}
}
