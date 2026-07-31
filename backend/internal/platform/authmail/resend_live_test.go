package authmail

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestResendLiveAcceptedReceipt(t *testing.T) {
	if os.Getenv("AUTH_MAIL_LIVE_TEST") != "1" {
		t.Skip("set AUTH_MAIL_LIVE_TEST=1 to run the controlled external receipt check")
	}
	values := readAcceptanceEnvironment(t)
	if values["AUTH_MAIL_DRIVER"] != "resend" {
		t.Fatal("AUTH_MAIL_DRIVER must select resend for the live receipt check")
	}
	apiKey := values["RESEND_API_KEY"]
	recipient := values["AUTH_MAIL_ACCEPTANCE_RECIPIENT"]
	publicBaseURL := strings.TrimRight(values["PUBLIC_BASE_URL"], "/")
	if apiKey == "" || recipient == "" || publicBaseURL == "" {
		t.Fatal("live receipt references are incomplete")
	}
	from := values["AUTH_MAIL_FROM"]
	if from == "" {
		from = "Photographer CRM <onboarding@resend.dev>"
	}
	actionSecret := make([]byte, 32)
	if _, err := rand.Read(actionSecret); err != nil {
		t.Fatal("create synthetic action secret")
	}
	actionURL := publicBaseURL + "/verify-email#token=live." + hex.EncodeToString(actionSecret)

	var logs bytes.Buffer
	sender := NewResend(apiKey, from, slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	receipt, err := sender.Send(ctx, auth.AuthMail{
		Purpose: auth.ActionEmailVerification, Recipient: recipient,
		ActionURL: actionURL, ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	})
	if err != nil {
		status := 0
		var providerError *resendHTTPError
		if errors.As(err, &providerError) {
			status = providerError.HTTPStatus()
		}
		t.Fatalf("live Resend request failed: http_status=%d class=%s", status, resendFailureClass(err))
	}
	if receipt.ProviderMessageID == "" || receipt.AcceptedAt.IsZero() {
		t.Fatal("live Resend response did not contain the accepted receipt allowlist")
	}
	logged := logs.String()
	if !strings.Contains(logged, `"event":"auth.mail_delivery"`) ||
		!strings.Contains(logged, `"status":"accepted"`) ||
		!strings.Contains(logged, receipt.ProviderMessageID) {
		t.Fatal("live accepted event is incomplete")
	}
	for _, forbidden := range []string{apiKey, recipient, actionURL, hex.EncodeToString(actionSecret)} {
		if strings.Contains(logged, forbidden) {
			t.Fatal("live accepted event leaked a protected value")
		}
	}
	t.Logf("provider_message_id=%s accepted_at=%s", receipt.ProviderMessageID, receipt.AcceptedAt.Format(time.RFC3339))
}

func readAcceptanceEnvironment(t *testing.T) map[string]string {
	t.Helper()
	rootEnv := filepath.Clean(filepath.Join("..", "..", "..", "..", ".env"))
	file, err := os.Open(rootEnv)
	if err != nil {
		t.Fatal("open owner-controlled environment reference")
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close owner-controlled environment reference: %v", err)
		}
	}()
	wanted := map[string]bool{
		"AUTH_MAIL_DRIVER": true, "RESEND_API_KEY": true,
		"AUTH_MAIL_ACCEPTANCE_RECIPIENT": true, "AUTH_MAIL_FROM": true,
		"PUBLIC_BASE_URL": true,
	}
	values := make(map[string]string, len(wanted))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !wanted[key] {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal("read owner-controlled environment reference")
	}
	return values
}
