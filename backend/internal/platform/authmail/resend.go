package authmail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/authevent"
)

const (
	resendEndpoint       = "https://api.resend.com/emails"
	resendUserAgent      = "photographer-crm/1.0"
	resendAttemptTimeout = 400 * time.Millisecond
	resendTotalTimeout   = 850 * time.Millisecond
	resendRetryDelay     = 25 * time.Millisecond
	resendMaxAttempts    = 2
	resendResponseLimit  = 64 << 10
)

// Resend 通过 Resend HTTPS API 投递认证邮件。请求总时限和尝试次数都有固定上界。
type Resend struct {
	apiKey       string
	from         string
	endpoint     string
	client       *http.Client
	logger       *slog.Logger
	now          func() time.Time
	totalTimeout time.Duration
	retryDelay   time.Duration
	maxAttempts  int
}

func NewResend(apiKey, from string, logger *slog.Logger) *Resend {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &Resend{
		apiKey: apiKey, from: from, endpoint: resendEndpoint,
		client: &http.Client{Timeout: resendAttemptTimeout}, logger: logger, now: time.Now,
		totalTimeout: resendTotalTimeout, retryDelay: resendRetryDelay, maxAttempts: resendMaxAttempts,
	}
}

func (s *Resend) Send(ctx context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	if err := ctx.Err(); err != nil {
		return auth.MailReceipt{}, err
	}
	if strings.TrimSpace(s.apiKey) == "" || strings.TrimSpace(s.from) == "" || s.client == nil || s.now == nil {
		return auth.MailReceipt{}, auth.NewDeliveryError(auth.DeliveryMisconfigured)
	}
	subject, htmlBody, textBody, err := renderAuthMail(mail)
	if err != nil {
		return auth.MailReceipt{}, err
	}
	payload, err := json.Marshal(resendSendRequest{
		From: s.from, To: []string{mail.Recipient}, Subject: subject, HTML: htmlBody, Text: textBody,
	})
	if err != nil {
		return auth.MailReceipt{}, auth.NewDeliveryError(auth.DeliveryMisconfigured)
	}

	totalTimeout := s.totalTimeout
	if totalTimeout <= 0 {
		totalTimeout = resendTotalTimeout
	}
	operationCtx, cancel := context.WithTimeout(ctx, totalTimeout)
	defer cancel()
	maxAttempts := s.maxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		receipt, retry, attemptErr := s.sendAttempt(operationCtx, mail, payload)
		if attemptErr == nil {
			s.logAccepted(operationCtx, mail.Purpose, receipt)
			return receipt, nil
		}
		lastErr = attemptErr
		if !retry || attempt == maxAttempts {
			break
		}
		if err := waitForRetry(operationCtx, s.retryDelay); err != nil {
			lastErr = err
			break
		}
	}
	if err := operationCtx.Err(); err != nil {
		lastErr = err
	}
	s.logFailure(ctx, mail.Purpose, lastErr)
	return auth.MailReceipt{}, lastErr
}

type resendSendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

type resendSendResponse struct {
	ID string `json:"id"`
}

type resendHTTPError struct {
	status int
	class  auth.DeliveryFailureClass
}

func (e *resendHTTPError) Error() string { return string(e.class) }

func (e *resendHTTPError) FailureClass() auth.DeliveryFailureClass { return e.class }

func (e *resendHTTPError) HTTPStatus() int { return e.status }

func (s *Resend) sendAttempt(
	ctx context.Context,
	mail auth.AuthMail,
	payload []byte,
) (auth.MailReceipt, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(payload))
	if err != nil {
		return auth.MailReceipt{}, false, auth.NewDeliveryError(auth.DeliveryMisconfigured)
	}
	request.Header.Set("Authorization", "Bearer "+s.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", resendUserAgent)
	request.Header.Set("Idempotency-Key", resendIdempotencyKey(mail))

	response, err := s.client.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return auth.MailReceipt{}, false, ctxErr
		}
		return auth.MailReceipt{}, true, auth.NewDeliveryError(auth.DeliveryTemporarilyUnavailable)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		var accepted resendSendResponse
		decoder := json.NewDecoder(io.LimitReader(response.Body, resendResponseLimit))
		if err := decoder.Decode(&accepted); err != nil || strings.TrimSpace(accepted.ID) == "" {
			return auth.MailReceipt{}, true, auth.NewDeliveryError(auth.DeliveryTemporarilyUnavailable)
		}
		return auth.MailReceipt{ProviderMessageID: accepted.ID, AcceptedAt: s.now().UTC()}, false, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, resendResponseLimit))
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return auth.MailReceipt{}, false, &resendHTTPError{
			status: response.StatusCode,
			class:  auth.DeliveryMisconfigured,
		}
	case response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError:
		return auth.MailReceipt{}, true, &resendHTTPError{
			status: response.StatusCode,
			class:  auth.DeliveryTemporarilyUnavailable,
		}
	default:
		return auth.MailReceipt{}, false, &resendHTTPError{
			status: response.StatusCode,
			class:  auth.DeliveryProviderRejected,
		}
	}
}

func renderAuthMail(mail auth.AuthMail) (string, string, string, error) {
	if strings.TrimSpace(mail.Recipient) == "" || strings.TrimSpace(mail.ActionURL) == "" {
		return "", "", "", auth.NewDeliveryError(auth.DeliveryProviderRejected)
	}
	var subject, action string
	switch mail.Purpose {
	case auth.ActionEmailVerification:
		subject, action = "验证你的 Photographer CRM 邮箱", "验证邮箱"
	case auth.ActionLegacyClaim:
		subject, action = "认领你的 Photographer CRM 账号", "认领账号"
	case auth.ActionPasswordReset:
		subject, action = "重置你的 Photographer CRM 密码", "重置密码"
	default:
		return "", "", "", auth.NewDeliveryError(auth.DeliveryMisconfigured)
	}
	escapedURL := html.EscapeString(mail.ActionURL)
	htmlBody := fmt.Sprintf("<p>请点击下方链接%s：</p><p><a href=\"%s\">%s</a></p><p>该链接将在 %s 失效。</p>",
		action, escapedURL, action, mail.ExpiresAt.UTC().Format(time.RFC3339))
	textBody := fmt.Sprintf("请使用以下链接%s：\n%s\n该链接将在 %s 失效。",
		action, mail.ActionURL, mail.ExpiresAt.UTC().Format(time.RFC3339))
	return subject, htmlBody, textBody, nil
}

func resendIdempotencyKey(mail auth.AuthMail) string {
	digest := sha256.Sum256([]byte(string(mail.Purpose) + "\x00" + mail.ActionURL))
	return "auth-mail-v1-" + hex.EncodeToString(digest[:])
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Resend) logAccepted(ctx context.Context, purpose auth.ActionPurpose, receipt auth.MailReceipt) {
	record := authevent.Event{
		Name: authevent.MailDelivery, Result: authevent.ResultSuccess,
		Action: string(purpose), ProviderMessageID: receipt.ProviderMessageID,
	}
	s.logger.LogAttrs(ctx, slog.LevelInfo, "auth mail delivery", record.Attrs()...)
}

func (s *Resend) logFailure(ctx context.Context, purpose auth.ActionPurpose, err error) {
	record := authevent.Event{
		Name: authevent.MailDelivery, Result: authevent.ResultFailure,
		Action: string(purpose), FailureClass: resendFailureClass(err),
	}
	s.logger.LogAttrs(ctx, slog.LevelWarn, "auth mail delivery", record.Attrs()...)
}

func resendFailureClass(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return string(auth.DeliveryCancelled)
	case errors.Is(err, context.DeadlineExceeded):
		return string(auth.DeliveryTimeout)
	}
	var classified interface {
		FailureClass() auth.DeliveryFailureClass
	}
	if errors.As(err, &classified) {
		return string(classified.FailureClass())
	}
	return string(auth.DeliveryTemporarilyUnavailable)
}

var _ auth.AuthMailSender = (*Resend)(nil)
