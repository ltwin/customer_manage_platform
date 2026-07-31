package authmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

func TestResendAcceptsMailWithIdempotencyAndRedactedEvent(t *testing.T) {
	t.Parallel()
	var requestOK atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var payload struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			HTML    string   `json:"html"`
			Text    string   `json:"text"`
		}
		if json.Unmarshal(body, &payload) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requestOK.Store(
			r.Method == http.MethodPost && r.URL.Path == "/emails" &&
				r.Header.Get("Authorization") == "Bearer test-api-key" &&
				r.Header.Get("Content-Type") == "application/json" &&
				r.Header.Get("User-Agent") == "photographer-crm/1.0" &&
				r.Header.Get("Idempotency-Key") != "" &&
				!strings.Contains(r.Header.Get("Idempotency-Key"), "synthetic-secret") &&
				payload.From == "Photographer CRM <onboarding@resend.dev>" &&
				len(payload.To) == 1 && payload.To[0] == "owner@example.invalid" &&
				payload.Subject != "" && strings.Contains(payload.HTML, "synthetic-secret") &&
				strings.Contains(payload.Text, "synthetic-secret"),
		)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"provider-message-123"}`)
	}))
	t.Cleanup(server.Close)

	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	var logs bytes.Buffer
	sender := testResendSender(server, &logs, now)
	receipt, err := sender.Send(context.Background(), auth.AuthMail{
		Purpose:   auth.ActionEmailVerification,
		Recipient: "owner@example.invalid",
		ActionURL: "https://app.example.invalid/verify-email#token=synthetic-secret",
		ExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil || receipt.ProviderMessageID != "provider-message-123" || !receipt.AcceptedAt.Equal(now) {
		t.Fatalf("receipt mismatch: has_id=%t accepted_at=%s err=%v",
			receipt.ProviderMessageID != "", receipt.AcceptedAt, err)
	}
	if !requestOK.Load() {
		t.Fatal("Resend request contract mismatch")
	}
	logged := logs.String()
	if !strings.Contains(logged, `"event":"auth.mail_delivery"`) ||
		!strings.Contains(logged, `"provider_message_id":"provider-message-123"`) ||
		!strings.Contains(logged, `"status":"accepted"`) {
		t.Fatal("accepted delivery event is incomplete")
	}
	for _, forbidden := range []string{"owner@example.invalid", "synthetic-secret", "test-api-key", "verify-email"} {
		if strings.Contains(logged, forbidden) {
			t.Fatal("delivery event leaked recipient, action URL, or credential")
		}
	}
}

func TestResendRetriesTemporaryFailureAtMostOnce(t *testing.T) {
	t.Parallel()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"provider-message-456"}`)
	}))
	t.Cleanup(server.Close)

	sender := testResendSender(server, io.Discard, time.Now().UTC())
	sender.retryDelay = 0
	_, err := sender.Send(context.Background(), syntheticAuthMail())
	if err != nil || attempts.Load() != 2 {
		t.Fatalf("temporary retry mismatch: attempts=%d err=%v", attempts.Load(), err)
	}
}

func TestResendClassifiesProviderFailuresWithoutResponseBody(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		status    int
		want      auth.DeliveryFailureClass
		wantCalls int32
	}{
		{name: "invalid credential", status: http.StatusUnauthorized, want: auth.DeliveryMisconfigured, wantCalls: 1},
		{name: "provider rejection", status: http.StatusUnprocessableEntity, want: auth.DeliveryProviderRejected, wantCalls: 1},
		{name: "rate limited", status: http.StatusTooManyRequests, want: auth.DeliveryTemporarilyUnavailable, wantCalls: 2},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"name":"provider_error","message":"must-not-leak"}`)
			}))
			t.Cleanup(server.Close)
			var logs bytes.Buffer
			sender := testResendSender(server, &logs, time.Now().UTC())
			sender.retryDelay = 0
			_, err := sender.Send(context.Background(), syntheticAuthMail())
			if deliveryFailureClass(err) != test.want || attempts.Load() != test.wantCalls {
				t.Fatalf("failure mismatch: class=%q attempts=%d", deliveryFailureClass(err), attempts.Load())
			}
			if strings.Contains(logs.String(), "must-not-leak") {
				t.Fatal("provider response body leaked into event")
			}
		})
	}
}

func TestResendHonorsCallerCancellation(t *testing.T) {
	t.Parallel()
	sender := testResendSender(nil, io.Discard, time.Now().UTC())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := sender.Send(ctx, syntheticAuthMail())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation must remain detectable: %v", err)
	}
}

func TestResendBoundsTheWholeOperationByDeadline(t *testing.T) {
	t.Parallel()
	sender := testResendSender(nil, io.Discard, time.Now().UTC())
	sender.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	sender.totalTimeout = 20 * time.Millisecond
	sender.retryDelay = 0
	started := time.Now()
	_, err := sender.Send(context.Background(), syntheticAuthMail())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("whole-operation deadline must remain detectable: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("whole-operation deadline was not bounded: %s", elapsed)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func testResendSender(server *httptest.Server, output io.Writer, now time.Time) *Resend {
	client := &http.Client{Timeout: 100 * time.Millisecond}
	endpoint := "http://127.0.0.1:1/emails"
	if server != nil {
		client = server.Client()
		endpoint = server.URL + "/emails"
	}
	return &Resend{
		apiKey: "test-api-key", from: "Photographer CRM <onboarding@resend.dev>", endpoint: endpoint,
		client: client, logger: slog.New(slog.NewJSONHandler(output, nil)), now: func() time.Time { return now },
		totalTimeout: time.Second, retryDelay: time.Millisecond, maxAttempts: 2,
	}
}

func syntheticAuthMail() auth.AuthMail {
	return auth.AuthMail{
		Purpose: auth.ActionEmailVerification, Recipient: "owner@example.invalid",
		ActionURL: "https://app.example.invalid/verify-email#token=synthetic-secret",
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}
}

func deliveryFailureClass(err error) auth.DeliveryFailureClass {
	var classified interface {
		FailureClass() auth.DeliveryFailureClass
	}
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}
