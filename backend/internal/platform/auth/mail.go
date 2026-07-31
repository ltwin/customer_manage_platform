package auth

import (
	"context"
	"errors"
	"time"
)

type AuthMail struct {
	Purpose   ActionPurpose
	Recipient string
	ActionURL string
	ExpiresAt time.Time
}

type MailReceipt struct {
	ProviderMessageID string
	AcceptedAt        time.Time
}

type AuthMailSender interface {
	Send(context.Context, AuthMail) (MailReceipt, error)
}

type unavailableMailSender struct{}

func (unavailableMailSender) Send(context.Context, AuthMail) (MailReceipt, error) {
	return MailReceipt{}, NewDeliveryError(DeliveryMisconfigured)
}

func deliveryOutcome(err error, receipt MailReceipt) DeliveryOutcome {
	if err == nil {
		return DeliveryOutcome{
			Attempted: true, Accepted: true,
			ProviderMessageID: receipt.ProviderMessageID, AcceptedAt: receipt.AcceptedAt,
		}
	}
	class := DeliveryTemporarilyUnavailable
	switch {
	case errors.Is(err, context.Canceled):
		class = DeliveryCancelled
	case errors.Is(err, context.DeadlineExceeded):
		class = DeliveryTimeout
	default:
		var classified interface {
			FailureClass() DeliveryFailureClass
		}
		if errors.As(err, &classified) {
			class = classified.FailureClass()
		}
	}
	return DeliveryOutcome{Attempted: true, FailureClass: class}
}
