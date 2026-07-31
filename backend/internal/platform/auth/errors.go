package auth

import (
	"errors"
	"fmt"
	"time"
)

type AuthErrorKind string

const (
	AuthErrorValidation                AuthErrorKind = "validation"
	AuthErrorUnauthorized              AuthErrorKind = "unauthorized"
	AuthErrorEmailVerificationRequired AuthErrorKind = "email_verification_required"
	AuthErrorInvalidOrExpiredToken     AuthErrorKind = "invalid_or_expired_token"
	AuthErrorBootstrapNotEmpty         AuthErrorKind = "bootstrap_not_empty"
	AuthErrorRateLimited               AuthErrorKind = "rate_limited"
	AuthErrorInternal                  AuthErrorKind = "internal"
)

type ClassifiedAuthError struct {
	kind         AuthErrorKind
	cause        error
	retryAfter   time.Duration
	action       AuthAction
	sourceDigest string
}

func (e *ClassifiedAuthError) Error() string       { return string(e.kind) }
func (e *ClassifiedAuthError) Kind() AuthErrorKind { return e.kind }
func (e *ClassifiedAuthError) Unwrap() error       { return e.cause }

func (e *ClassifiedAuthError) RetryAfter() time.Duration { return e.retryAfter }

func newAuthError(kind AuthErrorKind) error { return &ClassifiedAuthError{kind: kind} }

func newRateLimitedError(action AuthAction, sourceDigest string, retryAfter time.Duration) error {
	return &ClassifiedAuthError{
		kind: AuthErrorRateLimited, retryAfter: retryAfter, action: action, sourceDigest: sourceDigest,
	}
}

func AuthRetryAfter(err error) (time.Duration, bool) {
	var classified *ClassifiedAuthError
	if !errors.As(err, &classified) || classified.Kind() != AuthErrorRateLimited {
		return 0, false
	}
	return classified.RetryAfter(), true
}

func AuthRateLimitDetails(err error) (AuthAction, string, bool) {
	var classified *ClassifiedAuthError
	if !errors.As(err, &classified) || classified.Kind() != AuthErrorRateLimited ||
		classified.action == "" || classified.sourceDigest == "" {
		return "", "", false
	}
	return classified.action, classified.sourceDigest, true
}

func classifyAuthFailure(operation string, err error) error {
	var classified *ClassifiedAuthError
	if errors.As(err, &classified) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	return &ClassifiedAuthError{
		kind:  AuthErrorInternal,
		cause: fmt.Errorf("%s: %w", operation, err),
	}
}

func IsAuthError(err error, kind AuthErrorKind) bool {
	var classified *ClassifiedAuthError
	return errors.As(err, &classified) && classified.Kind() == kind
}

var (
	ErrInvalidPassword             = newAuthError(AuthErrorUnauthorized)
	ErrInvalidToken                = newAuthError(AuthErrorUnauthorized)
	ErrEmailVerificationRequired   = newAuthError(AuthErrorEmailVerificationRequired)
	ErrInvalidOrExpiredActionToken = newAuthError(AuthErrorInvalidOrExpiredToken)
	ErrBootstrapNotEmpty           = newAuthError(AuthErrorBootstrapNotEmpty)
	ErrRefreshReuse                = newAuthError(AuthErrorUnauthorized)
)

type ClassifiedDeliveryError struct {
	class DeliveryFailureClass
}

func NewDeliveryError(class DeliveryFailureClass) error {
	return &ClassifiedDeliveryError{class: class}
}

func (e *ClassifiedDeliveryError) Error() string { return string(e.class) }
func (e *ClassifiedDeliveryError) FailureClass() DeliveryFailureClass {
	return e.class
}
