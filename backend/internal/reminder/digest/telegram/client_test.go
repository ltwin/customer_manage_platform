package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
)

func TestClientSendMessageAndGetUpdatesUseBotAPIEnvelope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/botsecret/sendMessage", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("sendMessage method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
	})
	mux.HandleFunc("/botsecret/getUpdates", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "8" || r.URL.Query().Get("timeout") != "30" {
			t.Fatalf("getUpdates query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":8,"message":{"chat":{"id":123,"type":"private"},"text":"/today"}}]}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := NewClient("secret", server.URL, server.Client())

	ref, err := client.SendMessage(context.Background(), "123", "safe text")
	if err != nil || ref.MessageID != 42 {
		t.Fatalf("SendMessage: ref=%+v err=%v", ref, err)
	}
	updates, err := client.GetUpdates(context.Background(), 8, 30*time.Second)
	if err != nil || len(updates) != 1 || updates[0].ChatType != "private" || updates[0].Text != "/today" {
		t.Fatalf("GetUpdates: updates=%+v err=%v", updates, err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return fn(request) }

func TestClientClassifiesRateLimitAndRedactsToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"retry","parameters":{"retry_after":17}}`))
	}))
	defer server.Close()
	client := NewClient("super-secret-token", server.URL, server.Client())

	_, err := client.SendMessage(context.Background(), "123", "private body")
	tgErr, ok := err.(*digest.TelegramError)
	if !ok || tgErr.Kind != digest.TelegramErrorRateLimited || tgErr.RetryAfter != 17*time.Second {
		t.Fatalf("classification: %#v", err)
	}
	if strings.Contains(err.Error(), "super-secret-token") || strings.Contains(err.Error(), "private body") {
		t.Fatalf("error leaked request material: %q", err.Error())
	}
}

func TestClientClassifiesGetUpdatesWebhookConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"}`))
	}))
	defer server.Close()
	client := NewClient("secret", server.URL, server.Client())

	_, err := client.GetUpdates(context.Background(), 0, 30*time.Second)
	tgErr, ok := err.(*digest.TelegramError)
	if !ok || tgErr.Kind != digest.TelegramErrorWebhookConflict {
		t.Fatalf("classification: %#v", err)
	}
}

func TestClientClassifiesBotAPIResponseMatrix(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		status    int
		body      string
		want      digest.TelegramErrorKind
	}{
		{name: "poll server", operation: "poll", status: 500, body: `{"ok":false,"error_code":500}`, want: digest.TelegramErrorServer},
		{name: "poll auth", operation: "poll", status: 401, body: `{"ok":false,"error_code":401}`, want: digest.TelegramErrorInvalidAuth},
		{name: "poll client", operation: "poll", status: 400, body: `{"ok":false,"error_code":400}`, want: digest.TelegramErrorClient},
		{name: "poll protocol", operation: "poll", status: 200, body: `{broken`, want: digest.TelegramErrorProtocol},
		{name: "send conflict is client not webhook", operation: "send", status: 409, body: `{"ok":false,"error_code":409}`, want: digest.TelegramErrorClient},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			client := NewClient("secret", server.URL, server.Client())
			var err error
			if tt.operation == "poll" {
				_, err = client.GetUpdates(context.Background(), 0, 30*time.Second)
			} else {
				_, err = client.SendMessage(context.Background(), "1", "text")
			}
			telegramErr, ok := err.(*digest.TelegramError)
			if !ok || telegramErr.Kind != tt.want {
				t.Fatalf("kind: want=%s err=%#v", tt.want, err)
			}
		})
	}
}

func TestClientPreservesParentCancellationInsteadOfWrappingAsNetwork(t *testing.T) {
	// A canceled parent context must surface as context.Canceled, not a
	// TelegramError(network): the sender relies on errors.Is to keep the claim
	// and burn no budget during shutdown/cancel windows (REV-001).
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})}
	client := NewClient("secret", "https://example.invalid", httpClient)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, sendErr := client.SendMessage(ctx, "123", "private body")
	if !errors.Is(sendErr, context.Canceled) {
		t.Fatalf("SendMessage cancel not preserved: %#v", sendErr)
	}
	var tgErr *digest.TelegramError
	if errors.As(sendErr, &tgErr) {
		t.Fatalf("SendMessage cancel misclassified as TelegramError: %#v", tgErr)
	}

	_, pollErr := client.GetUpdates(ctx, 0, 30*time.Second)
	if !errors.Is(pollErr, context.Canceled) || errors.As(pollErr, &tgErr) {
		t.Fatalf("GetUpdates cancel not preserved: %#v", pollErr)
	}
}

func TestClientClassifiesTransportNetworkAndTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want digest.TelegramErrorKind
	}{
		{name: "network", err: errors.New("connection reset"), want: digest.TelegramErrorNetwork},
		{name: "timeout", err: context.DeadlineExceeded, want: digest.TelegramErrorTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, tt.err
			})}
			client := NewClient("secret", "https://example.invalid", httpClient)
			_, err := client.GetUpdates(context.Background(), 0, 30*time.Second)
			telegramErr, ok := err.(*digest.TelegramError)
			if !ok || telegramErr.Kind != tt.want {
				t.Fatalf("kind: want=%s err=%#v", tt.want, err)
			}
		})
	}
}
