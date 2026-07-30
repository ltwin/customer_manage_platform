// Package telegram adapts the raw Telegram Bot API to digest ports.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/reminder/digest"
)

const maxResponseBytes = 4 << 20

type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

func NewClient(token, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{token: token, baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient}
}

func (c *Client) SendMessage(ctx context.Context, chatID, text string) (digest.MessageRef, error) {
	body, err := json.Marshal(struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}{ChatID: chatID, Text: text})
	if err != nil {
		return digest.MessageRef{}, &digest.TelegramError{Kind: digest.TelegramErrorProtocol, Code: "encode_request"}
	}
	var result struct {
		MessageID int64 `json:"message_id"`
	}
	if err := c.call(ctx, http.MethodPost, "sendMessage", nil, body, &result); err != nil {
		return digest.MessageRef{}, err
	}
	return digest.MessageRef{MessageID: result.MessageID}, nil
}

func (c *Client) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]digest.Update, error) {
	query := make(url.Values)
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("timeout", strconv.Itoa(int(timeout/time.Second)))
	query.Set("allowed_updates", `["message"]`)
	var result []struct {
		ID      int64 `json:"update_id"`
		Message *struct {
			Chat struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"chat"`
			Text string `json:"text"`
		} `json:"message"`
	}
	if err := c.call(ctx, http.MethodGet, "getUpdates", query, nil, &result); err != nil {
		return nil, err
	}
	updates := make([]digest.Update, 0, len(result))
	for _, raw := range result {
		update := digest.Update{ID: raw.ID}
		if raw.Message != nil {
			update.ChatID = strconv.FormatInt(raw.Message.Chat.ID, 10)
			update.ChatType = raw.Message.Chat.Type
			update.Text = raw.Message.Text
		}
		updates = append(updates, update)
	}
	return updates, nil
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	ErrorCode   int             `json:"error_code"`
	Description string          `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) call(
	ctx context.Context,
	method string,
	operation string,
	query url.Values,
	body []byte,
	result any,
) error {
	endpoint := c.baseURL + "/bot" + c.token + "/" + operation
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return &digest.TelegramError{Kind: digest.TelegramErrorProtocol, Code: "build_request"}
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient.Do(req)
	if err != nil {
		return classifyTransport(err)
	}
	defer func() { _ = response.Body.Close() }()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return &digest.TelegramError{Kind: digest.TelegramErrorNetwork, Code: "read_response"}
	}
	var envelope apiEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return &digest.TelegramError{Kind: digest.TelegramErrorProtocol, Code: "decode_response"}
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices || !envelope.OK {
		return classifyAPI(operation, response.StatusCode, envelope)
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return &digest.TelegramError{Kind: digest.TelegramErrorProtocol, Code: "missing_result"}
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return &digest.TelegramError{Kind: digest.TelegramErrorProtocol, Code: "decode_result"}
	}
	return nil
}

func classifyTransport(err error) error {
	// A parent cancel is not a Telegram transport failure: the sender must see
	// context.Canceled unwrapped so it keeps the claim and burns no budget.
	// Wrapping it as network here would misclassify shutdown/cancel windows.
	if errors.Is(err, context.Canceled) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &digest.TelegramError{Kind: digest.TelegramErrorTimeout, Code: "deadline"}
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return &digest.TelegramError{Kind: digest.TelegramErrorTimeout, Code: "network_timeout"}
	}
	return &digest.TelegramError{Kind: digest.TelegramErrorNetwork, Code: "transport"}
}

func classifyAPI(operation string, status int, envelope apiEnvelope) error {
	code := envelope.ErrorCode
	if code == 0 {
		code = status
	}
	telegramErr := &digest.TelegramError{Code: strconv.Itoa(code)}
	switch {
	case status == http.StatusTooManyRequests || code == http.StatusTooManyRequests:
		telegramErr.Kind = digest.TelegramErrorRateLimited
		telegramErr.RetryAfter = time.Duration(envelope.Parameters.RetryAfter) * time.Second
	case status == http.StatusUnauthorized || status == http.StatusForbidden || code == http.StatusUnauthorized || code == http.StatusForbidden:
		telegramErr.Kind = digest.TelegramErrorInvalidAuth
	case operation == "getUpdates" && (status == http.StatusConflict || code == http.StatusConflict):
		telegramErr.Kind = digest.TelegramErrorWebhookConflict
	case status >= http.StatusInternalServerError || code >= http.StatusInternalServerError:
		telegramErr.Kind = digest.TelegramErrorServer
	case status >= http.StatusBadRequest || code >= http.StatusBadRequest:
		telegramErr.Kind = digest.TelegramErrorClient
	default:
		telegramErr.Kind = digest.TelegramErrorProtocol
	}
	return telegramErr
}

var (
	_ digest.TelegramSender       = (*Client)(nil)
	_ digest.TelegramUpdateSource = (*Client)(nil)
)

func (c *Client) String() string {
	return fmt.Sprintf("TelegramClient(base=%s)", c.baseURL)
}
