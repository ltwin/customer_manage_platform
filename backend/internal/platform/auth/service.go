package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	actionTokenTTL      = 24 * time.Hour
	refreshIdleTTL      = 14 * 24 * time.Hour
	refreshAbsoluteTTL  = 30 * 24 * time.Hour
	refreshReplayWindow = 10 * time.Second
	maxReplaySweepBatch = 1000
)

// Repository 是 account-auth 深模块消费的事务面；HTTP/CLI 不直接访问它。
type Repository interface {
	AccountCount(context.Context) (int64, error)
	RegisterAccount(context.Context, RegistrationAdmissionMode, RegistrationRecord) (bool, error)
	ReplaceVerificationToken(context.Context, string, string, []byte, time.Time, time.Time) (bool, error)
	ConsumeActionAndActivate(context.Context, ActionProof, RefreshSeed, time.Time) (Account, error)
	FindLoginRecord(context.Context, string) (LoginRecord, bool, error)
	CreateRefreshSession(context.Context, string, RefreshSeed) error
	RotateRefresh(context.Context, RefreshProof, RefreshSeed, *ReplayCipher, time.Time) (RefreshRotation, error)
	RevokeRefresh(context.Context, RefreshProof, time.Time) error
	CurrentAccount(context.Context, string) (Account, error)
	PlanLegacyClaim(context.Context, string) (LegacyClaimRecord, error)
	BeginLegacyClaim(context.Context, LegacyClaimCommand) (LegacyClaimRecord, error)
	InspectLegacyState(context.Context) (LegacyAuthState, error)
	SweepExpiredReplayCiphertexts(context.Context, time.Time, int) (int64, error)
}

type Service struct {
	repo          Repository
	tokens        *TokenIssuer
	replay        *ReplayCipher
	mail          AuthMailSender
	mode          RegistrationAdmissionMode
	now           func() time.Time
	random        io.Reader
	publicBaseURL string
}

type ServiceOption func(*Service)

func WithRegistrationAdmissionMode(mode RegistrationAdmissionMode) ServiceOption {
	return func(service *Service) { service.mode = mode }
}

func WithAuthClock(now func() time.Time) ServiceOption {
	return func(service *Service) {
		if now != nil {
			service.now = now
			service.tokens.WithClock(now)
		}
	}
}

func WithAuthRandom(random io.Reader) ServiceOption {
	return func(service *Service) {
		if random == nil {
			return
		}
		service.random = random
		service.tokens.WithRandom(random)
		service.replay = requiredReplayCipher(service.tokens, random)
	}
}

func WithAuthMailSender(sender AuthMailSender) ServiceOption {
	return func(service *Service) {
		if sender != nil {
			service.mail = sender
		}
	}
}

func WithPublicBaseURL(publicBaseURL string) ServiceOption {
	return func(service *Service) {
		service.publicBaseURL = strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	}
}

func NewService(repo Repository, tokens *TokenIssuer, options ...ServiceOption) *Service {
	service := &Service{
		repo: repo, tokens: tokens, replay: requiredReplayCipher(tokens, rand.Reader), mail: unavailableMailSender{},
		mode: RegistrationPublic, now: time.Now, random: rand.Reader,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func requiredReplayCipher(tokens *TokenIssuer, random io.Reader) *ReplayCipher {
	if tokens == nil {
		panic("auth: nil token issuer")
	}
	replay, err := tokens.ReplayCipher(random)
	if err != nil {
		// TokenIssuer 始终持有 32-byte 派生 key；失败代表构造期不变量已破坏。
		panic(fmt.Sprintf("auth: create replay cipher: %v", err))
	}
	return replay
}

func (s *Service) PlanBootstrap(ctx context.Context, email, password string) (BootstrapPlan, error) {
	normalized, err := normalizeCredentials(email, password)
	if err != nil {
		return BootstrapPlan{}, err
	}
	count, err := s.repo.AccountCount(ctx)
	if err != nil {
		return BootstrapPlan{}, classifyAuthFailure("plan bootstrap", err)
	}
	return BootstrapPlan{Eligible: count == 0, RecipientRef: recipientReference(normalized)}, nil
}

func (s *Service) Register(ctx context.Context, email, password string) (DispatchResult, error) {
	if s.mode != RegistrationPublic && s.mode != RegistrationBootstrapFirstAccount {
		return DispatchResult{}, classifyAuthFailure("register admission mode", fmt.Errorf("unsupported configuration"))
	}
	normalized, err := normalizeCredentials(email, password)
	if err != nil {
		return DispatchResult{}, err
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("register password", err)
	}
	wire, selector, tokenHash, err := newBearerToken(s.random)
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("register action token", err)
	}
	accountID, err := randomID(s.random)
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("register account id", err)
	}
	identityID, err := randomID(s.random)
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("register identity id", err)
	}
	now := s.now().UTC()
	created, err := s.repo.RegisterAccount(ctx, s.mode, RegistrationRecord{
		AccountID: accountID, IdentityID: identityID, NormalizedEmail: normalized,
		PasswordHash: passwordHash, ActionSelector: selector, ActionSecretHash: tokenHash,
		ActionExpiresAt: now.Add(actionTokenTTL), CreatedAt: now,
	})
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("register", err)
	}
	if !created {
		return DispatchResult{}, nil
	}
	return DispatchResult{Attempted: true, Delivery: s.deliverAction(
		ctx, ActionEmailVerification, normalized, wire, now.Add(actionTokenTTL),
	)}, nil
}

func (s *Service) ResendVerification(ctx context.Context, email string) (DispatchResult, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("resend verification token", err)
	}
	wire, selector, tokenHash, err := newBearerToken(s.random)
	if err != nil {
		return DispatchResult{}, err
	}
	now := s.now().UTC()
	replaced, err := s.repo.ReplaceVerificationToken(ctx, normalized, selector, tokenHash, now.Add(actionTokenTTL), now)
	if err != nil {
		return DispatchResult{}, classifyAuthFailure("resend verification", err)
	}
	if !replaced {
		return DispatchResult{}, nil
	}
	return DispatchResult{Attempted: true, Delivery: s.deliverAction(
		ctx, ActionEmailVerification, normalized, wire, now.Add(actionTokenTTL),
	)}, nil
}

func (s *Service) VerifyEmail(ctx context.Context, wire string) (Session, error) {
	selector, tokenHash, err := parseBearerToken(wire)
	if err != nil {
		return Session{}, ErrInvalidOrExpiredActionToken
	}
	now := s.now().UTC()
	seed, err := s.newRefreshSeed(now)
	if err != nil {
		return Session{}, classifyAuthFailure("verify email refresh seed", err)
	}
	account, err := s.repo.ConsumeActionAndActivate(ctx, ActionProof{
		Selector: selector, SecretHash: tokenHash,
	}, seed, now)
	if err != nil {
		return Session{}, classifyAuthFailure("verify email", err)
	}
	return s.sessionFor(account.ID, seed)
}

func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	normalized, err := normalizeEmail(email)
	if err != nil || !validPassword(password) {
		return Session{}, newAuthError(AuthErrorValidation)
	}
	record, found, err := s.repo.FindLoginRecord(ctx, normalized)
	if err != nil {
		return Session{}, classifyAuthFailure("login", err)
	}
	hash := dummyPasswordHash
	if found {
		hash = record.PasswordHash
	}
	passwordOK := VerifyPassword(hash, password)
	if !found || !passwordOK {
		return Session{}, ErrInvalidPassword
	}
	if record.Account.Status != AccountActive {
		return Session{}, ErrEmailVerificationRequired
	}
	now := s.now().UTC()
	seed, err := s.newRefreshSeed(now)
	if err != nil {
		return Session{}, classifyAuthFailure("login refresh seed", err)
	}
	if err := s.repo.CreateRefreshSession(ctx, record.Account.ID, seed); err != nil {
		return Session{}, classifyAuthFailure("login create refresh session", err)
	}
	return s.sessionFor(record.Account.ID, seed)
}

func (s *Service) Refresh(ctx context.Context, wire string) (Session, error) {
	if s.replay == nil {
		return Session{}, newAuthError(AuthErrorInternal)
	}
	generationID, tokenHash, err := parseBearerToken(wire)
	if err != nil {
		return Session{}, ErrInvalidToken
	}
	now := s.now().UTC()
	successor, err := s.newRefreshGeneration(now)
	if err != nil {
		return Session{}, classifyAuthFailure("refresh successor", err)
	}
	rotation, err := s.repo.RotateRefresh(ctx, RefreshProof{GenerationID: generationID, TokenHash: tokenHash}, successor, s.replay, now)
	if err != nil {
		return Session{}, classifyAuthFailure("refresh", err)
	}
	successor.FamilyID = rotation.FamilyID
	successor.GenerationID = rotation.GenerationID
	successor.WireToken = rotation.WireToken
	successor.IdleExpiresAt = rotation.IdleExpiresAt
	successor.AbsoluteExpiresAt = rotation.AbsoluteExpiresAt
	return s.sessionFor(rotation.AccountID, successor)
}

func (s *Service) Logout(ctx context.Context, wire string) error {
	if strings.TrimSpace(wire) == "" {
		return nil
	}
	generationID, tokenHash, err := parseBearerToken(wire)
	if err != nil {
		return nil
	}
	if err := s.repo.RevokeRefresh(ctx, RefreshProof{GenerationID: generationID, TokenHash: tokenHash}, s.now().UTC()); err != nil {
		return classifyAuthFailure("logout", err)
	}
	return nil
}

func (s *Service) CurrentAccount(ctx context.Context, accountID string) (Account, error) {
	account, err := s.repo.CurrentAccount(ctx, accountID)
	if err != nil {
		return Account{}, classifyAuthFailure("current account", err)
	}
	return account, nil
}

func (s *Service) AccountByID(ctx context.Context, accountID string) (Account, error) {
	return s.CurrentAccount(ctx, accountID)
}

func (s *Service) ParseToken(token string) (string, error) { return s.tokens.Parse(token) }

func (s *Service) InspectLegacyState(ctx context.Context) (LegacyAuthState, error) {
	state, err := s.repo.InspectLegacyState(ctx)
	if err != nil {
		return LegacyAuthState{}, classifyAuthFailure("inspect legacy state", err)
	}
	return state, nil
}

func (s *Service) BeginLegacyClaim(ctx context.Context, email string, dryRun bool) (LegacyClaimResult, error) {
	normalized, err := normalizeEmail(email)
	if err != nil {
		return LegacyClaimResult{}, classifyAuthFailure("legacy claim email", err)
	}
	if dryRun {
		record, err := s.repo.PlanLegacyClaim(ctx, normalized)
		if err != nil {
			return LegacyClaimResult{}, classifyAuthFailure("plan legacy claim", err)
		}
		return legacyClaimResult(true, normalized, record), nil
	}
	wire, selector, tokenHash, err := newBearerToken(s.random)
	if err != nil {
		return LegacyClaimResult{}, classifyAuthFailure("legacy claim action token", err)
	}
	identityID, err := randomID(s.random)
	if err != nil {
		return LegacyClaimResult{}, classifyAuthFailure("legacy claim identity id", err)
	}
	now := s.now().UTC()
	record, err := s.repo.BeginLegacyClaim(ctx, LegacyClaimCommand{
		NormalizedEmail:  normalized,
		IdentityID:       identityID,
		ActionSelector:   selector,
		ActionSecretHash: tokenHash,
		ActionExpiresAt:  now.Add(actionTokenTTL),
		CreatedAt:        now,
	})
	if err != nil {
		return LegacyClaimResult{}, classifyAuthFailure("begin legacy claim", err)
	}
	result := legacyClaimResult(false, normalized, record)
	if record.State != LegacyClaimReady && record.State != LegacyClaimPendingSameEmail {
		return result, nil
	}
	result.Delivery = s.deliverAction(ctx, ActionLegacyClaim, normalized, wire, now.Add(actionTokenTTL))
	return result, nil
}

func legacyClaimResult(dryRun bool, email string, record LegacyClaimRecord) LegacyClaimResult {
	return LegacyClaimResult{
		DryRun:            dryRun,
		State:             record.State,
		AccountIDRedacted: identifierReference("account", record.AccountID),
		EmailRedacted:     recipientReference(email),
		LegacyCount:       record.LegacyCount,
		PendingClaimCount: record.PendingClaimCount,
	}
}

func (s *Service) deliverAction(
	ctx context.Context,
	purpose ActionPurpose,
	recipient, wire string,
	expiresAt time.Time,
) DeliveryOutcome {
	actionURL, err := s.actionURL(wire)
	if err != nil {
		return DeliveryOutcome{Attempted: true, FailureClass: DeliveryMisconfigured}
	}
	receipt, sendErr := s.mail.Send(ctx, AuthMail{
		Purpose: purpose, Recipient: recipient, ActionURL: actionURL, ExpiresAt: expiresAt,
	})
	return deliveryOutcome(sendErr, receipt)
}

func (s *Service) actionURL(wire string) (string, error) {
	base, err := url.Parse(s.publicBaseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("public base URL is unavailable")
	}
	target := base.ResolveReference(&url.URL{Path: "/verify-email"})
	target.Fragment = url.Values{"token": []string{wire}}.Encode()
	return target.String(), nil
}

func (s *Service) SweepExpiredReplayCiphertexts(ctx context.Context, batch int) (int64, error) {
	if batch < 1 || batch > maxReplaySweepBatch {
		return 0, newAuthError(AuthErrorValidation)
	}
	cleared, err := s.repo.SweepExpiredReplayCiphertexts(ctx, s.now().UTC(), batch)
	if err != nil {
		return 0, classifyAuthFailure("sweep expired replay ciphertexts", err)
	}
	return cleared, nil
}

func (s *Service) newRefreshSeed(now time.Time) (RefreshSeed, error) {
	seed, err := s.newRefreshGeneration(now)
	if err != nil {
		return RefreshSeed{}, err
	}
	seed.FamilyID, err = randomID(s.random)
	if err != nil {
		return RefreshSeed{}, err
	}
	seed.IdleExpiresAt = now.Add(refreshIdleTTL)
	seed.AbsoluteExpiresAt = now.Add(refreshAbsoluteTTL)
	return seed, nil
}

func (s *Service) newRefreshGeneration(now time.Time) (RefreshSeed, error) {
	wire, generationID, tokenHash, err := newBearerToken(s.random)
	if err != nil {
		return RefreshSeed{}, err
	}
	return RefreshSeed{
		GenerationID: generationID, WireToken: wire, TokenHash: tokenHash,
		IdleExpiresAt: now.Add(refreshIdleTTL), ReplayUntil: now.Add(refreshReplayWindow),
		CreatedAt: now,
	}, nil
}

func (s *Service) sessionFor(accountID string, seed RefreshSeed) (Session, error) {
	access, accessExpires, err := s.tokens.IssueForSession(accountID, seed.FamilyID)
	if err != nil {
		return Session{}, classifyAuthFailure("issue access token", err)
	}
	return Session{
		AccountID:   accountID,
		AccessToken: access, AccessExpiresAt: accessExpires,
		RefreshToken: seed.WireToken, RefreshExpiresAt: seed.IdleExpiresAt,
		RefreshAbsoluteAt: seed.AbsoluteExpiresAt,
		RefreshSessionID:  seed.FamilyID, RefreshGeneration: seed.GenerationID,
	}, nil
}

func normalizeCredentials(email, password string) (string, error) {
	normalized, err := normalizeEmail(email)
	if err != nil || !validPassword(password) {
		return "", newAuthError(AuthErrorValidation)
	}
	return normalized, nil
}

func normalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if len(normalized) < 3 || len(normalized) > 254 || strings.Count(normalized, "@") != 1 {
		return "", newAuthError(AuthErrorValidation)
	}
	for _, char := range normalized {
		if char > 0x7f {
			return "", newAuthError(AuthErrorValidation)
		}
	}
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Name != "" || parsed.Address != normalized {
		return "", newAuthError(AuthErrorValidation)
	}
	return normalized, nil
}

func validPassword(password string) bool {
	return utf8.ValidString(password) && len([]byte(password)) >= 12 && len([]byte(password)) <= 72
}

func recipientReference(email string) string {
	return identifierReference("email", email)
}

func identifierReference(kind, value string) string {
	if value == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s:%x", kind, digest[:6])
}
