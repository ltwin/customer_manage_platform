package digest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	bindTokenBytes = 32
	bindTokenTTL   = 10 * time.Minute
	bindTxTimeout  = 5 * time.Second
)

type TokenMatchOutcome string

const (
	TokenMatched    TokenMatchOutcome = "matched"
	TokenNotMatched TokenMatchOutcome = "not_matched"
)

type BindOutcome string

const (
	BindApplied      BindOutcome = "applied"
	BindInvalid      BindOutcome = "invalid"
	BindChatConflict BindOutcome = "chat_conflict"
)

type ChatMatchOutcome string

const (
	ChatMatched    ChatMatchOutcome = "matched"
	ChatNotMatched ChatMatchOutcome = "not_matched"
)

type LocalTarget struct {
	LocalDate time.Time
	Timezone  string
}

type ClaimOutcome string

const (
	ClaimCreated      ClaimOutcome = "created"
	ClaimExisting     ClaimOutcome = "existing"
	ClaimUnauthorized ClaimOutcome = "unauthorized"
)

type BindingRepository interface {
	IssueBindToken(context.Context, store.AccountScope, []byte, time.Time) error
	MatchBindToken(context.Context, store.AccountScope, []byte, time.Time) (TokenMatchOutcome, error)
	ConsumeAndBind(context.Context, store.AccountScope, []byte, string, int64, time.Time) (BindOutcome, error)
	ClaimCommandIfCurrentChat(context.Context, store.AccountScope, string, int64, LocalTarget, MessageKind) (ClaimOutcome, error)
}

type ChatBindingMatcher interface {
	MatchCurrentChat(context.Context, store.AccountScope, string) (string, ChatMatchOutcome, error)
}

type AccountScopeEnumerator interface {
	AccountScopes(context.Context) ([]store.ScopedAccount, error)
}

type ScopeResolutionKind string

const (
	ScopeUnmatched ScopeResolutionKind = "unmatched"
	ScopeMatched   ScopeResolutionKind = "matched"
	ScopeIntegrity ScopeResolutionKind = "integrity"
)

type ScopeResolution struct {
	Kind    ScopeResolutionKind
	Account store.ScopedAccount
}

type BindTokenResolver struct {
	accounts AccountScopeEnumerator
	repo     BindingRepository
}

func NewBindTokenResolver(accounts AccountScopeEnumerator, repo BindingRepository) *BindTokenResolver {
	return &BindTokenResolver{accounts: accounts, repo: repo}
}

func (r *BindTokenResolver) Resolve(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) (ScopeResolution, error) {
	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		return ScopeResolution{}, err
	}
	matches := make([]store.ScopedAccount, 0, 1)
	for _, account := range accounts {
		outcome, err := r.repo.MatchBindToken(ctx, account.Scope, tokenHash, now)
		if err != nil {
			return ScopeResolution{}, err
		}
		if outcome == TokenMatched {
			matches = append(matches, account)
		}
	}
	switch len(matches) {
	case 0:
		return ScopeResolution{Kind: ScopeUnmatched}, nil
	case 1:
		return ScopeResolution{Kind: ScopeMatched, Account: matches[0]}, nil
	default:
		return ScopeResolution{Kind: ScopeIntegrity}, nil
	}
}

type ChatAccountResolver struct {
	accounts AccountScopeEnumerator
	matcher  ChatBindingMatcher
}

func NewChatAccountResolver(accounts AccountScopeEnumerator, matcher ChatBindingMatcher) *ChatAccountResolver {
	return &ChatAccountResolver{accounts: accounts, matcher: matcher}
}

func (r *ChatAccountResolver) Resolve(ctx context.Context, chatID string) (ScopeResolution, error) {
	accounts, err := r.accounts.AccountScopes(ctx)
	if err != nil {
		return ScopeResolution{}, err
	}
	matches := make([]store.ScopedAccount, 0, 1)
	for _, account := range accounts {
		_, outcome, err := r.matcher.MatchCurrentChat(ctx, account.Scope, chatID)
		if err != nil {
			return ScopeResolution{}, err
		}
		if outcome == ChatMatched {
			matches = append(matches, account)
		}
	}
	switch len(matches) {
	case 0:
		return ScopeResolution{Kind: ScopeUnmatched}, nil
	case 1:
		return ScopeResolution{Kind: ScopeMatched, Account: matches[0]}, nil
	default:
		return ScopeResolution{Kind: ScopeIntegrity}, nil
	}
}

type BindLink struct {
	Token     string
	DeepLink  string
	ExpiresAt time.Time
}

type BindingService struct {
	repo        BindingRepository
	resolver    *BindTokenResolver
	gate        RecipientGate
	botUsername string
	now         func() time.Time
	random      io.Reader
}

func NewBindingService(
	repo BindingRepository,
	resolver *BindTokenResolver,
	gate RecipientGate,
	botUsername string,
) *BindingService {
	return &BindingService{
		repo: repo, resolver: resolver, gate: gate, botUsername: botUsername,
		now: time.Now, random: rand.Reader,
	}
}

func (s *BindingService) WithClock(now func() time.Time) *BindingService {
	if now != nil {
		s.now = now
	}
	return s
}

func (s *BindingService) WithRandom(random io.Reader) *BindingService {
	if random != nil {
		s.random = random
	}
	return s
}

func (s *BindingService) IssueBindToken(ctx context.Context, scope store.AccountScope) (BindLink, error) {
	raw := make([]byte, bindTokenBytes)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return BindLink{}, fmt.Errorf("generate bind token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	expiresAt := now.Add(bindTokenTTL)
	if err := s.repo.IssueBindToken(ctx, scope, hash[:], expiresAt); err != nil {
		return BindLink{}, err
	}
	deepLink := "https://t.me/" + url.PathEscape(s.botUsername) + "?start=" + url.QueryEscape(token)
	return BindLink{Token: token, DeepLink: deepLink, ExpiresAt: expiresAt}, nil
}

func (s *BindingService) HandleStart(ctx context.Context, update Update) (BindOutcome, error) {
	if update.ChatType != "private" {
		return BindInvalid, nil
	}
	command, rest := parseCommand(update.Text)
	if command != "/start" || len(rest) != 1 || len(rest[0]) != 43 {
		return BindInvalid, nil
	}
	token := rest[0]
	if raw, err := base64.RawURLEncoding.DecodeString(token); err != nil || len(raw) != bindTokenBytes {
		return BindInvalid, nil
	}
	hash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	resolution, err := s.resolver.Resolve(ctx, hash[:], now)
	if err != nil {
		return BindInvalid, err
	}
	if resolution.Kind != ScopeMatched {
		return BindInvalid, nil
	}
	var outcome BindOutcome
	err = s.gate.WithRebind(ctx, resolution.Account.AccountID, func(gateCtx context.Context) error {
		txCtx, cancel := context.WithTimeout(gateCtx, bindTxTimeout)
		defer cancel()
		var consumeErr error
		outcome, consumeErr = s.repo.ConsumeAndBind(
			txCtx,
			resolution.Account.Scope,
			hash[:],
			update.ChatID,
			update.ID,
			now,
		)
		return consumeErr
	})
	if err != nil {
		return BindInvalid, err
	}
	return outcome, nil
}

var ErrBindingIntegrity = errors.New("binding integrity conflict")

// parseCommand splits a Telegram message into its leading command word and the
// remaining whitespace-separated arguments. The command word may carry an
// optional "@botusername" suffix (used when the same command is issued in
// shared contexts); it is stripped so "/start@bot" normalizes to "/start"
// (REV-005). Non-command text returns an empty command.
func parseCommand(text string) (string, []string) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", nil
	}
	command := fields[0]
	if !strings.HasPrefix(command, "/") {
		return "", nil
	}
	if at := strings.IndexByte(command, '@'); at >= 0 {
		command = command[:at]
	}
	return command, fields[1:]
}
