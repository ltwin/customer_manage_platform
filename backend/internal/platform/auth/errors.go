package auth

import (
	"errors"
	"fmt"
)

type AuthErrorKind string

const (
	AuthErrorValidation                AuthErrorKind = "validation"
	AuthErrorUnauthorized              AuthErrorKind = "unauthorized"
	AuthErrorEmailVerificationRequired AuthErrorKind = "email_verification_required"
	AuthErrorInvalidOrExpiredToken     AuthErrorKind = "invalid_or_expired_token"
	AuthErrorBootstrapNotEmpty         AuthErrorKind = "bootstrap_not_empty"
	AuthErrorInternal                  AuthErrorKind = "internal"
)

type ClassifiedAuthError struct {
	kind  AuthErrorKind
	cause error
}

func (e *ClassifiedAuthError) Error() string       { return string(e.kind) }
func (e *ClassifiedAuthError) Kind() AuthErrorKind { return e.kind }
func (e *ClassifiedAuthError) Unwrap() error       { return e.cause }

func newAuthError(kind AuthErrorKind) error { return &ClassifiedAuthError{kind: kind} }

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
