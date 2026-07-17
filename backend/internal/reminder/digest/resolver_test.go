package digest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type fixedAccounts struct{ accounts []store.ScopedAccount }

func (f fixedAccounts) AccountScopes(context.Context) ([]store.ScopedAccount, error) {
	return f.accounts, nil
}

type resolverBindingRepo struct {
	tokenOutcomes []TokenMatchOutcome
	chatOutcomes  []ChatMatchOutcome
	err           error
}

func (*resolverBindingRepo) IssueBindToken(context.Context, store.AccountScope, []byte, time.Time) error {
	return nil
}

func (r *resolverBindingRepo) MatchBindToken(context.Context, store.AccountScope, []byte, time.Time) (TokenMatchOutcome, error) {
	if r.err != nil {
		return TokenNotMatched, r.err
	}
	outcome := r.tokenOutcomes[0]
	r.tokenOutcomes = r.tokenOutcomes[1:]
	return outcome, nil
}

func (*resolverBindingRepo) ConsumeAndBind(context.Context, store.AccountScope, []byte, string, int64, time.Time) (BindOutcome, error) {
	return BindInvalid, nil
}

func (*resolverBindingRepo) ClaimCommandIfCurrentChat(context.Context, store.AccountScope, string, int64, LocalTarget, MessageKind) (ClaimOutcome, error) {
	return ClaimUnauthorized, nil
}

func (r *resolverBindingRepo) MatchCurrentChat(context.Context, store.AccountScope, string) (string, ChatMatchOutcome, error) {
	if r.err != nil {
		return "", ChatNotMatched, r.err
	}
	outcome := r.chatOutcomes[0]
	r.chatOutcomes = r.chatOutcomes[1:]
	return "chat", outcome, nil
}

func TestBindTokenResolverReturnsZeroOneManyAndDBError(t *testing.T) {
	accounts := fixedAccounts{accounts: []store.ScopedAccount{{AccountID: "a"}, {AccountID: "b"}}}
	tests := []struct {
		name     string
		outcomes []TokenMatchOutcome
		err      error
		want     ScopeResolutionKind
	}{
		{name: "zero", outcomes: []TokenMatchOutcome{TokenNotMatched, TokenNotMatched}, want: ScopeUnmatched},
		{name: "one", outcomes: []TokenMatchOutcome{TokenNotMatched, TokenMatched}, want: ScopeMatched},
		{name: "many", outcomes: []TokenMatchOutcome{TokenMatched, TokenMatched}, want: ScopeIntegrity},
		{name: "db error", err: errors.New("db down")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &resolverBindingRepo{tokenOutcomes: append([]TokenMatchOutcome(nil), tt.outcomes...), err: tt.err}
			resolution, err := NewBindTokenResolver(accounts, repo).Resolve(context.Background(), make([]byte, 32), time.Now())
			if tt.err != nil {
				if err == nil {
					t.Fatal("DB error was swallowed")
				}
				return
			}
			if err != nil || resolution.Kind != tt.want {
				t.Fatalf("resolution=%+v err=%v", resolution, err)
			}
		})
	}
}

func TestChatAccountResolverReturnsZeroOneManyAndDBError(t *testing.T) {
	accounts := fixedAccounts{accounts: []store.ScopedAccount{{AccountID: "a"}, {AccountID: "b"}}}
	tests := []struct {
		name     string
		outcomes []ChatMatchOutcome
		err      error
		want     ScopeResolutionKind
	}{
		{name: "zero", outcomes: []ChatMatchOutcome{ChatNotMatched, ChatNotMatched}, want: ScopeUnmatched},
		{name: "one", outcomes: []ChatMatchOutcome{ChatMatched, ChatNotMatched}, want: ScopeMatched},
		{name: "many", outcomes: []ChatMatchOutcome{ChatMatched, ChatMatched}, want: ScopeIntegrity},
		{name: "db error", err: errors.New("db down")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &resolverBindingRepo{chatOutcomes: append([]ChatMatchOutcome(nil), tt.outcomes...), err: tt.err}
			resolution, err := NewChatAccountResolver(accounts, repo).Resolve(context.Background(), "chat")
			if tt.err != nil {
				if err == nil {
					t.Fatal("DB error was swallowed")
				}
				return
			}
			if err != nil || resolution.Kind != tt.want {
				t.Fatalf("resolution=%+v err=%v", resolution, err)
			}
		})
	}
}
