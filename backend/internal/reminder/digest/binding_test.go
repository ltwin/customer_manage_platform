package digest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

func TestBindingServiceIssuesOpaqueSingleUseTokenAndReplacesPrevious(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := NewPostgresBindingRepository()
	accounts := accountEnumerator{db: db}
	service := NewBindingService(repo, NewBindTokenResolver(accounts, repo), NewRecipientGate(), "studio_digest_bot").
		WithClock(func() time.Time { return now }).
		WithRandom(bytes.NewReader(append(bytes.Repeat([]byte{0x41}, 32), bytes.Repeat([]byte{0x44}, 32)...)))

	first, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue first token: %v", err)
	}
	if len(first.Token) != 43 || strings.Contains(first.Token, "acc_digest") {
		t.Fatalf("token is not 43-char opaque base64url: %q", first.Token)
	}
	if first.DeepLink != "https://t.me/studio_digest_bot?start="+first.Token || !first.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("bind link mismatch: %+v", first)
	}
	firstHash := sha256.Sum256([]byte(first.Token))
	match, err := repo.MatchBindToken(context.Background(), account.Scope, firstHash[:], now)
	if err != nil || match != TokenMatched {
		t.Fatalf("first token did not match: outcome=%s err=%v", match, err)
	}

	second, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue second token: %v", err)
	}
	if second.Token == first.Token {
		t.Fatal("two issues reused the same token")
	}
	match, err = repo.MatchBindToken(context.Background(), account.Scope, firstHash[:], now)
	if err != nil || match != TokenNotMatched {
		t.Fatalf("old token remained valid: outcome=%s err=%v", match, err)
	}
}

func TestBindingServicePrivateStartConsumesBindsAndSupersedesOldAck(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := NewPostgresBindingRepository()
	service := NewBindingService(repo, NewBindTokenResolver(accountEnumerator{db: db}, repo), NewRecipientGate(), "studio_digest_bot").
		WithClock(func() time.Time { return now }).
		WithRandom(bytes.NewReader(append(bytes.Repeat([]byte{0x42}, 32), bytes.Repeat([]byte{0x45}, 64)...)))

	first, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue first: %v", err)
	}
	outcome, err := service.HandleStart(context.Background(), Update{
		ID: 10, ChatID: "chat-old", ChatType: "private", Text: "/start " + first.Token,
	})
	if err != nil || outcome != BindApplied {
		t.Fatalf("consume first: outcome=%s err=%v", outcome, err)
	}
	outcome, err = service.HandleStart(context.Background(), Update{
		ID: 10, ChatID: "chat-old", ChatType: "private", Text: "/start " + first.Token,
	})
	if err != nil || outcome != BindInvalid {
		t.Fatalf("replay must be invalid: outcome=%s err=%v", outcome, err)
	}

	second, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue second: %v", err)
	}
	outcome, err = service.HandleStart(context.Background(), Update{
		ID: 11, ChatID: "chat-new", ChatType: "private", Text: "/start " + second.Token,
	})
	if err != nil || outcome != BindApplied {
		t.Fatalf("rebind: outcome=%s err=%v", outcome, err)
	}
	chat, match, err := repo.MatchCurrentChat(context.Background(), account.Scope, "chat-new")
	if err != nil || match != ChatMatched || chat != "chat-new" {
		t.Fatalf("new chat not current: chat=%q outcome=%s err=%v", chat, match, err)
	}
	_, match, err = repo.MatchCurrentChat(context.Background(), account.Scope, "chat-old")
	if err != nil || match != ChatNotMatched {
		t.Fatalf("old chat retained authority: outcome=%s err=%v", match, err)
	}
	rows, err := account.Scope.Query(context.Background(), "telegram_deliveries", "status", "source = $2", string(DeliverySourceBindingAck))
	if err != nil {
		t.Fatalf("query acks: %v", err)
	}
	defer rows.Close()
	statuses := make(map[DeliveryStatus]int)
	for rows.Next() {
		var status DeliveryStatus
		if err := rows.Scan(&status); err != nil {
			t.Fatalf("scan ack: %v", err)
		}
		statuses[status]++
	}
	if statuses[DeliveryStatusPending] != 1 || statuses[DeliveryStatusSuperseded] != 1 {
		t.Fatalf("ack statuses: %+v", statuses)
	}
}

func TestBindingServiceRejectsGroupTamperExpiredAndCrossAccountChat(t *testing.T) {
	db, account := startDigestPostgres(t)
	if err := db.CreateAccount(context.Background(), "acc-other", "hash"); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	activateLegacyTestAccount(t, db, "acc-other")
	accounts, err := db.AccountScopes(context.Background())
	if err != nil || len(accounts) != 2 {
		t.Fatalf("account scopes: len=%d err=%v", len(accounts), err)
	}
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := NewPostgresBindingRepository()
	service := NewBindingService(repo, NewBindTokenResolver(accountEnumerator{db: db}, repo), NewRecipientGate(), "studio_digest_bot").
		WithClock(func() time.Time { return now }).
		WithRandom(bytes.NewReader(append(
			append(bytes.Repeat([]byte{0x43}, 32), bytes.Repeat([]byte{0x46}, 32)...),
			append(bytes.Repeat([]byte{0x47}, 32), append(bytes.Repeat([]byte{0x48}, 32), bytes.Repeat([]byte{0x49}, 32)...)...)...,
		)))

	link, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	for _, update := range []Update{
		{ID: 20, ChatID: "chat-shared", ChatType: "group", Text: "/start " + link.Token},
		{ID: 21, ChatID: "chat-shared", ChatType: "private", Text: "/start " + link.Token + "x"},
	} {
		outcome, err := service.HandleStart(context.Background(), update)
		if err != nil || outcome != BindInvalid {
			t.Fatalf("unsafe start accepted: update=%+v outcome=%s err=%v", update, outcome, err)
		}
	}

	other := accounts[0]
	if other.AccountID == account.AccountID {
		other = accounts[1]
	}
	otherLink, err := service.IssueBindToken(context.Background(), other.Scope)
	if err != nil {
		t.Fatalf("issue other: %v", err)
	}
	if outcome, err := service.HandleStart(context.Background(), Update{
		ID: 22, ChatID: "chat-shared", ChatType: "private", Text: "/start " + otherLink.Token,
	}); err != nil || outcome != BindApplied {
		t.Fatalf("bind other: outcome=%s err=%v", outcome, err)
	}
	accountLink, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue account: %v", err)
	}
	if outcome, err := service.HandleStart(context.Background(), Update{
		ID: 23, ChatID: "chat-shared", ChatType: "private", Text: "/start " + accountLink.Token,
	}); err != nil || outcome != BindChatConflict {
		t.Fatalf("cross-account chat conflict: outcome=%s err=%v", outcome, err)
	}

	expired, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue expired: %v", err)
	}
	service.WithClock(func() time.Time { return now.Add(11 * time.Minute) })
	if outcome, err := service.HandleStart(context.Background(), Update{
		ID: 24, ChatID: "chat-expired", ChatType: "private", Text: "/start " + expired.Token,
	}); err != nil || outcome != BindInvalid {
		t.Fatalf("expired token accepted: outcome=%s err=%v", outcome, err)
	}
}

func TestParseCommandStripsOptionalBotMention(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		command string
		rest    []string
	}{
		{name: "plain start", text: "/start abc", command: "/start", rest: []string{"abc"}},
		{name: "start with bot mention", text: "/start@studio_digest_bot abc", command: "/start", rest: []string{"abc"}},
		{name: "today with bot mention", text: "/today@studio_digest_bot", command: "/today", rest: nil},
		{name: "leading spaces", text: "   /start@bot tok", command: "/start", rest: []string{"tok"}},
		{name: "non-command", text: "hello world", command: "", rest: nil},
		{name: "empty", text: "", command: "", rest: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, rest := parseCommand(tt.text)
			if command != tt.command {
				t.Fatalf("command: want %q got %q", tt.command, command)
			}
			if len(rest) != len(tt.rest) {
				t.Fatalf("rest: want %v got %v", tt.rest, rest)
			}
			for i := range rest {
				if rest[i] != tt.rest[i] {
					t.Fatalf("rest[%d]: want %q got %q", i, tt.rest[i], rest[i])
				}
			}
		})
	}
}

func TestBindingServiceAcceptsStartWithBotMention(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := NewPostgresBindingRepository()
	service := NewBindingService(repo, NewBindTokenResolver(accountEnumerator{db: db}, repo), NewRecipientGate(), "studio_digest_bot").
		WithClock(func() time.Time { return now }).
		WithRandom(bytes.NewReader(bytes.Repeat([]byte{0x4A}, 32)))

	link, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	outcome, err := service.HandleStart(context.Background(), Update{
		ID: 30, ChatID: "chat-mention", ChatType: "private", Text: "/start@studio_digest_bot " + link.Token,
	})
	if err != nil || outcome != BindApplied {
		t.Fatalf("/start@bot deep-link must bind: outcome=%s err=%v", outcome, err)
	}
}

type accountEnumerator struct{ db *store.Store }

func (e accountEnumerator) AccountScopes(ctx context.Context) ([]store.ScopedAccount, error) {
	return e.db.AccountScopes(ctx)
}

func TestSettingsUpsertDoesNotOverwriteCurrentTelegramChatWithStaleRead(t *testing.T) {
	_, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	if err := account.Scope.Upsert(context.Background(), "settings",
		[]string{"telegram_chat_id", "updated_at"},
		[]string{"account_id"},
		[]string{"telegram_chat_id", "updated_at"},
		"chat-old", now); err != nil {
		t.Fatalf("seed old chat: %v", err)
	}
	repo := settings.NewPostgresRepository()
	stale, found, err := repo.Get(context.Background(), account.Scope)
	if err != nil || !found {
		t.Fatalf("read stale settings: found=%v err=%v", found, err)
	}
	if err := account.Scope.Upsert(context.Background(), "settings",
		[]string{"telegram_chat_id", "updated_at"},
		[]string{"account_id"},
		[]string{"telegram_chat_id", "updated_at"},
		"chat-new", now.Add(time.Second)); err != nil {
		t.Fatalf("concurrent rebind: %v", err)
	}
	stale.Timezone = "UTC"
	if _, err := repo.Upsert(context.Background(), account.Scope, stale); err != nil {
		t.Fatalf("settings upsert: %v", err)
	}
	current, found, err := repo.Get(context.Background(), account.Scope)
	if err != nil || !found || current.TelegramChatID == nil {
		t.Fatalf("read current settings: %+v found=%v err=%v", current, found, err)
	}
	if *current.TelegramChatID != "chat-new" {
		t.Fatalf("stale Settings PATCH silently reverted rebind to %q", *current.TelegramChatID)
	}
}

func TestPostgresBindingRepositoryConcurrentIssueLeavesExactlyOneCurrentToken(t *testing.T) {
	_, account := startDigestPostgres(t)
	repo := NewPostgresBindingRepository()
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	hashes := [][32]byte{sha256.Sum256([]byte("token-a")), sha256.Sum256([]byte("token-b"))}
	start := make(chan struct{})
	errs := make(chan error, len(hashes))
	var wg sync.WaitGroup
	for i := range hashes {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			errs <- repo.IssueBindToken(context.Background(), account.Scope, hashes[index][:], now.Add(10*time.Minute))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent issue failed: %v", err)
		}
	}
	matches := 0
	for i := range hashes {
		outcome, err := repo.MatchBindToken(context.Background(), account.Scope, hashes[i][:], now)
		if err != nil {
			t.Fatalf("match token %d: %v", i, err)
		}
		if outcome == TokenMatched {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("concurrent issue left %d valid tokens, want exactly 1", matches)
	}
}

func TestBindingServiceConcurrentConsumeAppliesOnce(t *testing.T) {
	db, account := startDigestPostgres(t)
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	repo := NewPostgresBindingRepository()
	service := NewBindingService(repo, NewBindTokenResolver(accountEnumerator{db: db}, repo), NewRecipientGate(), "studio_digest_bot").
		WithClock(func() time.Time { return now }).
		WithRandom(bytes.NewReader(bytes.Repeat([]byte{0x51}, 32)))
	link, err := service.IssueBindToken(context.Background(), account.Scope)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	start := make(chan struct{})
	outcomes := make(chan BindOutcome, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			outcome, err := service.HandleStart(context.Background(), Update{
				ID: int64(100 + index), ChatID: "chat-race", ChatType: "private", Text: "/start " + link.Token,
			})
			outcomes <- outcome
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(outcomes)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent consume: %v", err)
		}
	}
	counts := make(map[BindOutcome]int)
	for outcome := range outcomes {
		counts[outcome]++
	}
	if counts[BindApplied] != 1 || counts[BindInvalid] != 1 {
		t.Fatalf("concurrent consume outcomes: %+v", counts)
	}
}
