package main

import (
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/authevent"
)

func writeLegacyClaimEvent(
	output io.Writer,
	now time.Time,
	dryRun bool,
	result auth.LegacyClaimResult,
	err error,
) error {
	event := authevent.Event{
		Timestamp: now.UTC(), Name: authevent.LegacyClaim,
		Result: authevent.ResultSuccess, AccountRef: result.AccountIDRedacted, DryRun: &dryRun,
	}
	switch {
	case err != nil:
		event.Result = authevent.ResultFailure
		event.FailureClass = legacyEventFailureClass(err)
	case result.State != auth.LegacyClaimReady && result.State != auth.LegacyClaimPendingSameEmail:
		event.Result = authevent.ResultFailure
		event.FailureClass = "not_ready"
	case !dryRun && !result.Delivery.Accepted:
		event.Result = authevent.ResultFailure
		event.FailureClass = string(result.Delivery.FailureClass)
		if event.FailureClass == "" {
			event.FailureClass = "delivery_failed"
		}
	}
	return json.NewEncoder(output).Encode(event)
}

func legacyEventFailureClass(err error) string {
	var classified interface{ Kind() auth.AuthErrorKind }
	if errors.As(err, &classified) {
		switch classified.Kind() {
		case auth.AuthErrorValidation:
			return "validation"
		case auth.AuthErrorUnauthorized:
			return "unauthorized"
		}
	}
	return "internal"
}
