package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDeliveryOutcomeClassificationPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want DeliveryFailureClass
	}{
		{name: "cancel wins typed class", err: errors.Join(context.Canceled, NewDeliveryError(DeliveryProviderRejected)), want: DeliveryCancelled},
		{name: "deadline wins typed class", err: errors.Join(context.DeadlineExceeded, NewDeliveryError(DeliveryProviderRejected)), want: DeliveryTimeout},
		{name: "typed class", err: fmt.Errorf("adapter: %w", NewDeliveryError(DeliveryMisconfigured)), want: DeliveryMisconfigured},
		{name: "unclassified", err: errors.New("synthetic adapter failure"), want: DeliveryTemporarilyUnavailable},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			outcome := deliveryOutcome(test.err, MailReceipt{})
			if !outcome.Attempted || outcome.Accepted || outcome.FailureClass != test.want {
				t.Fatalf("outcome = %#v, want failure class %q", outcome, test.want)
			}
		})
	}
}

func TestCredentialValidationContract(t *testing.T) {
	t.Parallel()
	email := "Owner" + "@" + "Example.Invalid"
	normalized, err := normalizeCredentials("  "+email+"  ", "twelve-bytes")
	if err != nil || normalized != "owner"+"@"+"example.invalid" {
		t.Fatalf("normalize valid credentials: email=%q err=%v", normalized, err)
	}
	if _, err := normalizeCredentials(email, "  not-trimmed"); err != nil {
		t.Fatalf("password must not be trimmed before byte-length validation: %v", err)
	}
	for _, password := range []string{"short", string(make([]byte, 73))} {
		if _, err := normalizeCredentials(email, password); !IsAuthError(err, AuthErrorValidation) {
			t.Fatalf("password should fail validation: length=%d err=%v", len(password), err)
		}
	}
	if _, err := normalizeEmail("owner" + "@" + "例.invalid"); !IsAuthError(err, AuthErrorValidation) {
		t.Fatalf("non-ASCII email should fail validation: %v", err)
	}
}

func TestNormalizeEmailAcceptsOnlyASCIIAddrSpec(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		"  Owner@Example.Invalid  ": "owner@example.invalid",
		"owner+crm@example.invalid": "owner+crm@example.invalid",
		"a.b-c_d@example.invalid":   "a.b-c_d@example.invalid",
	}
	for raw, want := range valid {
		got, err := normalizeEmail(raw)
		if err != nil || got != want {
			t.Errorf("normalizeEmail(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}

	invalid := []string{
		"ab@",
		"@ab",
		"a b@example.invalid",
		"a..b@example.invalid",
		".ab@example.invalid",
		"ab.@example.invalid",
		"Owner <owner@example.invalid>",
		"owner@example.invalid (comment)",
		"owner@例.invalid",
	}
	for _, raw := range invalid {
		if _, err := normalizeEmail(raw); !IsAuthError(err, AuthErrorValidation) {
			t.Errorf("normalizeEmail(%q) error = %v; want validation", raw, err)
		}
	}
}

func TestDeliveryErrorExposesTypedFailureClass(t *testing.T) {
	t.Parallel()
	var classified interface {
		FailureClass() DeliveryFailureClass
	}
	err := fmt.Errorf("adapter: %w", NewDeliveryError(DeliveryProviderRejected))
	if !errors.As(err, &classified) {
		t.Fatal("delivery error must expose the provider-neutral typed interface")
	}
	if classified.FailureClass() != DeliveryProviderRejected {
		t.Fatalf("failure class = %q", classified.FailureClass())
	}
}

func TestServiceClassifiesRepositoryFailureAsInternal(t *testing.T) {
	t.Parallel()
	repositoryFailure := errors.New("synthetic repository failure")
	repo := repositoryStub{accountCount: func(context.Context) (int64, error) {
		return 0, repositoryFailure
	}}
	service := NewService(repo, NewTokenIssuer("synthetic-root-secret"))
	_, err := service.PlanBootstrap(context.Background(), syntheticEmail(), "twelve-bytes")
	if !IsAuthError(err, AuthErrorInternal) {
		t.Fatalf("repository failure should be classified internal: %v", err)
	}
	if !errors.Is(err, repositoryFailure) {
		t.Fatalf("repository cause should remain in the error chain: %v", err)
	}
}

func TestServiceRejectsInvalidConstructionModeAndUnboundedSweep(t *testing.T) {
	t.Parallel()
	registerCalled := false
	sweepCalled := false
	repo := repositoryStub{
		register: func(context.Context, RegistrationAdmissionMode, RegistrationRecord) (bool, error) {
			registerCalled = true
			return true, nil
		},
		sweep: func(context.Context, time.Time, int) (int64, error) {
			sweepCalled = true
			return 0, nil
		},
	}
	service := NewService(repo, NewTokenIssuer("synthetic-root-secret"),
		WithRegistrationAdmissionMode(RegistrationAdmissionMode("unsupported")))
	if _, err := service.Register(context.Background(), syntheticEmail(), "twelve-bytes"); !IsAuthError(err, AuthErrorInternal) {
		t.Fatalf("invalid construction mode should fail closed: %v", err)
	}
	if registerCalled {
		t.Fatal("invalid construction mode reached the repository")
	}
	if _, err := service.SweepExpiredReplayCiphertexts(context.Background(), 1001); !IsAuthError(err, AuthErrorValidation) {
		t.Fatalf("unbounded replay sweep should fail validation: %v", err)
	}
	if sweepCalled {
		t.Fatal("unbounded replay sweep reached the repository")
	}
}

func TestLegacyClaimDryRunDoesNotGenerateBearerOrWrite(t *testing.T) {
	t.Parallel()
	beginCalled := false
	repo := repositoryStub{
		legacyPlan: func(context.Context, string) (LegacyClaimRecord, error) {
			return LegacyClaimRecord{
				State:             LegacyClaimReady,
				AccountID:         "legacy-stable-id",
				LegacyCount:       1,
				PendingClaimCount: 0,
			}, nil
		},
		legacyBegin: func(context.Context, LegacyClaimCommand) (LegacyClaimRecord, error) {
			beginCalled = true
			return LegacyClaimRecord{}, nil
		},
	}
	service := NewService(repo, NewTokenIssuer("synthetic-root-secret"),
		WithAuthRandom(alwaysFailReader{}))

	result, err := service.BeginLegacyClaim(context.Background(), syntheticEmail(), true)
	if err != nil {
		t.Fatalf("dry-run legacy claim: %v", err)
	}
	if !result.DryRun || result.State != LegacyClaimReady || result.AccountIDRedacted == "" ||
		result.AccountIDRedacted == "legacy-stable-id" || result.EmailRedacted == "" ||
		result.EmailRedacted == syntheticEmail() || result.LegacyCount != 1 || result.PendingClaimCount != 0 {
		t.Fatalf("dry-run result = %#v", result)
	}
	if beginCalled {
		t.Fatal("dry-run reached the mutating legacy claim repository method")
	}
}

func syntheticEmail() string {
	return "owner" + "@" + "example.invalid"
}

type repositoryStub struct {
	accountCount func(context.Context) (int64, error)
	register     func(context.Context, RegistrationAdmissionMode, RegistrationRecord) (bool, error)
	legacyPlan   func(context.Context, string) (LegacyClaimRecord, error)
	legacyBegin  func(context.Context, LegacyClaimCommand) (LegacyClaimRecord, error)
	sweep        func(context.Context, time.Time, int) (int64, error)
}

func (r repositoryStub) AccountCount(ctx context.Context) (int64, error) {
	if r.accountCount != nil {
		return r.accountCount(ctx)
	}
	return 0, nil
}

func (r repositoryStub) RegisterAccount(ctx context.Context, mode RegistrationAdmissionMode, record RegistrationRecord) (bool, error) {
	if r.register != nil {
		return r.register(ctx, mode, record)
	}
	return false, nil
}

func (repositoryStub) ReplaceVerificationToken(context.Context, string, string, []byte, time.Time, time.Time) (bool, error) {
	return false, nil
}

func (repositoryStub) ConsumeActionAndActivate(context.Context, ActionProof, RefreshSeed, time.Time) (Account, error) {
	return Account{}, nil
}

func (repositoryStub) FindLoginRecord(context.Context, string) (LoginRecord, bool, error) {
	return LoginRecord{}, false, nil
}

func (repositoryStub) CreateRefreshSession(context.Context, string, RefreshSeed) error {
	return nil
}

func (repositoryStub) RotateRefresh(context.Context, RefreshProof, RefreshSeed, *ReplayCipher, time.Time) (RefreshRotation, error) {
	return RefreshRotation{}, nil
}

func (repositoryStub) RevokeRefresh(context.Context, RefreshProof, time.Time) error {
	return nil
}

func (repositoryStub) CurrentAccount(context.Context, string) (Account, error) {
	return Account{}, nil
}

func (r repositoryStub) PlanLegacyClaim(ctx context.Context, email string) (LegacyClaimRecord, error) {
	if r.legacyPlan != nil {
		return r.legacyPlan(ctx, email)
	}
	return LegacyClaimRecord{State: LegacyClaimNoTarget}, nil
}

func (r repositoryStub) BeginLegacyClaim(ctx context.Context, command LegacyClaimCommand) (LegacyClaimRecord, error) {
	if r.legacyBegin != nil {
		return r.legacyBegin(ctx, command)
	}
	return LegacyClaimRecord{State: LegacyClaimNoTarget}, nil
}

func (repositoryStub) InspectLegacyState(context.Context) (LegacyAuthState, error) {
	return LegacyAuthState{}, nil
}

func (r repositoryStub) SweepExpiredReplayCiphertexts(ctx context.Context, now time.Time, batch int) (int64, error) {
	if r.sweep != nil {
		return r.sweep(ctx, now, batch)
	}
	return 0, nil
}

type alwaysFailReader struct{}

func (alwaysFailReader) Read([]byte) (int, error) {
	return 0, errors.New("randomness must not be read")
}
