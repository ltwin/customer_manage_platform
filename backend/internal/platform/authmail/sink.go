// Package authmail 提供 account-auth 邮件 port 的基础设施 adapters。
package authmail

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
)

// Sink 是显式 localhost 开发 adapter：接受后立即丢弃邮件，不输出 recipient/action URL。
// production preflight 必须拒绝该 driver；真实邮件验收也不能以 sink receipt 代替。
type Sink struct {
	logger *slog.Logger
	now    func() time.Time
	next   atomic.Uint64
}

func NewSink(logger *slog.Logger, now func() time.Time) *Sink {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if now == nil {
		now = time.Now
	}
	return &Sink{logger: logger, now: now}
}

func (s *Sink) Send(ctx context.Context, mail auth.AuthMail) (auth.MailReceipt, error) {
	if err := ctx.Err(); err != nil {
		return auth.MailReceipt{}, err
	}
	messageID := fmt.Sprintf("dev-sink-%d", s.next.Add(1))
	acceptedAt := s.now().UTC()
	s.logger.InfoContext(ctx, "auth mail sink accepted",
		slog.String("purpose", string(mail.Purpose)),
		slog.String("provider_message_id", messageID),
	)
	return auth.MailReceipt{ProviderMessageID: messageID, AcceptedAt: acceptedAt}, nil
}

var _ auth.AuthMailSender = (*Sink)(nil)
