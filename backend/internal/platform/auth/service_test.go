package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
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

func TestServiceDerivesLimiterDigestsFromCanonicalClientMeta(t *testing.T) {
	t.Parallel()
	service := NewService(nil, NewTokenIssuer("synthetic-root"))
	subjectDigest, sourceDigest, err := service.attemptDigests(
		AuthActionLogin,
		[]byte("owner@example.test"),
		ClientMeta{SourceIP: netip.MustParseAddr("2001:db8::1")},
	)
	if err != nil {
		t.Fatalf("derive limiter digests: %v", err)
	}
	if subjectDigest != "v1:A7_G3sRfkbn-OE_woV3IMCDA6GDF8zUndcpF8vcSCSU" ||
		sourceDigest != "v1:FaJOOyq0aYb9s5Lux4GHSYPaP_oywEedyOpr0gOfl_8" {
		t.Fatalf("limiter digests = %q/%q", subjectDigest, sourceDigest)
	}
	if _, _, err := service.attemptDigests(AuthActionLogin, []byte("owner@example.test"), ClientMeta{}); !IsAuthError(err, AuthErrorInternal) {
		t.Fatalf("invalid source must fail closed: %v", err)
	}
}

func TestLoginLimiterFailsClosedBeforeCredentialLookup(t *testing.T) {
	t.Parallel()
	lookupCalled := false
	repo := repositoryStub{findLogin: func(context.Context, string) (LoginRecord, bool, error) {
		lookupCalled = true
		return LoginRecord{}, false, nil
	}}
	limiter := &attemptLimiterStub{retryAfter: 1250 * time.Millisecond, allowed: false}
	service := NewService(repo, NewTokenIssuer("synthetic-root"), WithAttemptLimiter(limiter))
	_, err := service.Login(
		context.Background(), syntheticEmail(), "twelve-bytes",
		ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.10")},
	)
	if !IsAuthError(err, AuthErrorRateLimited) {
		t.Fatalf("limited login error = %v", err)
	}
	if retryAfter, ok := AuthRetryAfter(err); !ok || retryAfter != 1250*time.Millisecond {
		t.Fatalf("limited login retry = %s/%v", retryAfter, ok)
	}
	if lookupCalled || limiter.calls != 1 || limiter.action != AuthActionLogin {
		t.Fatalf("limited login reached credential lookup or missed limiter: lookup=%v limiter=%#v", lookupCalled, limiter)
	}
}

func TestLimiterResetPolicyPreservesCommittedAuthSemantics(t *testing.T) {
	t.Parallel()
	meta := ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.40")}
	newService := func(repo Repository, limiter *attemptLimiterStub) *Service {
		return NewService(
			repo,
			NewTokenIssuer("synthetic-root"),
			WithAttemptLimiter(limiter),
			WithAuthResponseDelay(func(context.Context, time.Duration) error { return nil }),
			WithAuthTimingRandom(zeroReader{}),
		)
	}

	t.Run("consume error fails closed before credential lookup", func(t *testing.T) {
		lookupCalled := false
		repo := repositoryStub{findLogin: func(context.Context, string) (LoginRecord, bool, error) {
			lookupCalled = true
			return LoginRecord{}, false, nil
		}}
		service := newService(repo, &attemptLimiterStub{err: errors.New("limiter unavailable")})
		_, err := service.Login(context.Background(), syntheticEmail(), "twelve-bytes", meta)
		if !IsAuthError(err, AuthErrorInternal) || lookupCalled {
			t.Fatalf("limiter failure crossed credential boundary: err=%v lookup=%t", err, lookupCalled)
		}
	})

	t.Run("login success resets login subject only", func(t *testing.T) {
		limiter := &attemptLimiterStub{allowed: true}
		repo := repositoryStub{findLogin: func(context.Context, string) (LoginRecord, bool, error) {
			return LoginRecord{Account: Account{ID: "account-1", Status: AccountActive}, PasswordHash: dummyPasswordHash}, true, nil
		}}
		service := newService(repo, limiter)
		service.passwordMatches = func(string, string) bool { return true }
		if _, err := service.Login(context.Background(), syntheticEmail(), "twelve-bytes", meta); err != nil {
			t.Fatalf("login: %v", err)
		}
		if limiter.resetCalls != 1 || limiter.resetActions[0] != AuthActionLogin {
			t.Fatalf("login reset policy = %#v", limiter.resetActions)
		}
	})

	t.Run("change reset failure prevents password commit", func(t *testing.T) {
		changeCalled := false
		limiter := &attemptLimiterStub{allowed: true, resetErr: errors.New("limiter reset unavailable")}
		repo := repositoryStub{changePassword: func(context.Context, PasswordChangeCommand) error {
			changeCalled = true
			return nil
		}}
		service := newService(repo, limiter)
		service.passwordMatches = func(string, string) bool { return true }
		err := service.ChangePassword(
			context.Background(), AccountContext{AccountID: "account-1"}, "current-pass-1", "new-password-1", meta,
		)
		if !IsAuthError(err, AuthErrorInternal) || changeCalled {
			t.Fatalf("change committed before limiter reset: err=%v committed=%t", err, changeCalled)
		}
	})

	t.Run("change success resets before password commit", func(t *testing.T) {
		sequence := make([]string, 0, 2)
		limiter := &attemptLimiterStub{allowed: true, onReset: func() { sequence = append(sequence, "reset") }}
		repo := repositoryStub{changePassword: func(context.Context, PasswordChangeCommand) error {
			sequence = append(sequence, "commit")
			return nil
		}}
		service := newService(repo, limiter)
		service.passwordMatches = func(string, string) bool { return true }
		if err := service.ChangePassword(
			context.Background(), AccountContext{AccountID: "account-1"}, "current-pass-1", "new-password-1", meta,
		); err != nil {
			t.Fatalf("change password: %v", err)
		}
		if len(sequence) != 2 || sequence[0] != "reset" || sequence[1] != "commit" ||
			limiter.resetCalls != 1 || limiter.resetActions[0] != AuthActionChangePassword {
			t.Fatalf("change order/reset policy = %#v/%#v", sequence, limiter.resetActions)
		}
	})

	t.Run("verify and reset never reset source or subject", func(t *testing.T) {
		wire, _, _, err := newBearerToken(zeroReader{})
		if err != nil {
			t.Fatalf("create action token: %v", err)
		}
		for _, action := range []struct {
			name string
			run  func(*Service) error
		}{
			{name: "verify", run: func(service *Service) error {
				_, err := service.VerifyEmail(context.Background(), wire, meta)
				return err
			}},
			{name: "reset", run: func(service *Service) error {
				return service.ResetPassword(context.Background(), wire, "new-password-1", meta)
			}},
		} {
			limiter := &attemptLimiterStub{allowed: true}
			if err := action.run(newService(repositoryStub{}, limiter)); err != nil {
				t.Fatalf("%s: %v", action.name, err)
			}
			if limiter.resetCalls != 0 {
				t.Fatalf("%s reset limiter buckets: %#v", action.name, limiter.resetActions)
			}
		}
	})
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
	if _, err := service.Register(context.Background(), syntheticEmail(), "twelve-bytes", ClientMeta{SourceIP: netip.MustParseAddr("198.51.100.11")}); !IsAuthError(err, AuthErrorInternal) {
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
	accountCount         func(context.Context) (int64, error)
	register             func(context.Context, RegistrationAdmissionMode, RegistrationRecord) (bool, error)
	replacePasswordReset func(context.Context, string, string, []byte, time.Time, time.Time) (bool, error)
	passwordCredential   func(context.Context, string) (string, error)
	changePassword       func(context.Context, PasswordChangeCommand) error
	legacyPlan           func(context.Context, string) (LegacyClaimRecord, error)
	legacyBegin          func(context.Context, LegacyClaimCommand) (LegacyClaimRecord, error)
	sweep                func(context.Context, time.Time, int) (int64, error)
	findLogin            func(context.Context, string) (LoginRecord, bool, error)
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

func (r repositoryStub) ReplacePasswordResetToken(ctx context.Context, email, selector string, hash []byte, expiresAt, now time.Time) (bool, error) {
	if r.replacePasswordReset != nil {
		return r.replacePasswordReset(ctx, email, selector, hash, expiresAt, now)
	}
	return false, nil
}

func (repositoryStub) ConsumeActionAndActivate(context.Context, ActionProof, RefreshSeed, time.Time) (Account, error) {
	return Account{}, nil
}

func (repositoryStub) ResetPassword(context.Context, ActionProof, string, time.Time) (string, error) {
	return "", nil
}

func (r repositoryStub) PasswordCredential(ctx context.Context, accountID string) (string, error) {
	if r.passwordCredential != nil {
		return r.passwordCredential(ctx, accountID)
	}
	return dummyPasswordHash, nil
}

func (r repositoryStub) ChangePassword(ctx context.Context, command PasswordChangeCommand) error {
	if r.changePassword != nil {
		return r.changePassword(ctx, command)
	}
	return nil
}

func (r repositoryStub) FindLoginRecord(ctx context.Context, email string) (LoginRecord, bool, error) {
	if r.findLogin != nil {
		return r.findLogin(ctx, email)
	}
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

type attemptLimiterStub struct {
	retryAfter   time.Duration
	allowed      bool
	err          error
	calls        int
	action       AuthAction
	resetCalls   int
	resetActions []AuthAction
	resetErr     error
	onReset      func()
}

func (l *attemptLimiterStub) Consume(_ context.Context, action AuthAction, _, _ string, _ time.Time) (time.Duration, bool, error) {
	l.calls++
	l.action = action
	return l.retryAfter, l.allowed, l.err
}

func (l *attemptLimiterStub) ResetSubject(_ context.Context, action AuthAction, _ string) error {
	l.resetCalls++
	l.resetActions = append(l.resetActions, action)
	if l.onReset != nil {
		l.onReset()
	}
	return l.resetErr
}
