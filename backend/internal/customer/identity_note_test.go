package customer_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
)

// A7：追加身份——合法 / 非法 platform / 空 handle / merged 客户。
func TestAddIdentity(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()
	created := createActiveCustomer(t, svc, scope, "阿芷")

	identity, err := svc.AddIdentity(ctx, scope, created.ID, customer.IdentityInput{
		Platform: customer.PlatformTelegram, Handle: "azhi_tg", Remark: strPtr("常用"),
	})
	if err != nil {
		t.Fatalf("add identity: %v", err)
	}
	if identity.CustomerID != created.ID || identity.Platform != customer.PlatformTelegram || identity.AccountID != "acct-a" {
		t.Fatalf("identity shape mismatch: %+v", identity)
	}

	if _, err := svc.AddIdentity(ctx, scope, created.ID, customer.IdentityInput{Platform: "bad", Handle: "x"}); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("invalid platform: want validation, got %v", err)
	}
	if _, err := svc.AddIdentity(ctx, scope, created.ID, customer.IdentityInput{Platform: customer.PlatformWechat, Handle: "  "}); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("empty handle: want validation, got %v", err)
	}

	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_merged", DisplayName: "已合并",
		Channel: customer.ChannelOther, Status: customer.StatusMerged, CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		Handle: "merged-h",
	})
	if _, err := svc.AddIdentity(ctx, scope, "cus_merged", customer.IdentityInput{Platform: customer.PlatformWechat, Handle: "x"}); !errors.Is(err, customer.ErrCustomerMerged) {
		t.Fatalf("merged customer: want ErrCustomerMerged, got %v", err)
	}
}

// A8：删除身份——普通删除 / 删最后一个 / 不存在或跨账号。
func TestDeleteIdentityAndLastGuard(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := customerService()
	created := createActiveCustomer(t, svc, scopeA, "阿芷")

	second, err := svc.AddIdentity(ctx, scopeA, created.ID, customer.IdentityInput{Platform: customer.PlatformQQ, Handle: "azhi_qq"})
	if err != nil {
		t.Fatalf("add second identity: %v", err)
	}

	if err := svc.DeleteIdentity(ctx, scopeB, created.ID, second.ID); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("cross-account delete: want not found, got %v", err)
	}
	if err := svc.DeleteIdentity(ctx, scopeA, created.ID, "sid_missing"); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("missing identity: want not found, got %v", err)
	}

	if err := svc.DeleteIdentity(ctx, scopeA, created.ID, second.ID); err != nil {
		t.Fatalf("delete second identity: %v", err)
	}
	detail, err := svc.Detail(ctx, scopeA, created.ID)
	if err != nil || len(detail.Identities) != 1 {
		t.Fatalf("one identity should remain: err=%v identities=%+v", err, detail.Identities)
	}

	if err := svc.DeleteIdentity(ctx, scopeA, created.ID, detail.Identities[0].ID); !errors.Is(err, customer.ErrLastIdentity) {
		t.Fatalf("delete last identity: want ErrLastIdentity, got %v", err)
	}
}

// D3：并发删除同客户两个身份，至少一个失败（末位守护不被删穿）。
func TestDeleteIdentityConcurrentGuard(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()
	created := createActiveCustomer(t, svc, scope, "阿芷")
	second, err := svc.AddIdentity(ctx, scope, created.ID, customer.IdentityInput{Platform: customer.PlatformQQ, Handle: "azhi_qq"})
	if err != nil {
		t.Fatalf("add second identity: %v", err)
	}
	detail, err := svc.Detail(ctx, scope, created.ID)
	if err != nil || len(detail.Identities) != 2 {
		t.Fatalf("expect 2 identities: err=%v detail=%+v", err, detail)
	}
	first := detail.Identities[0]
	if first.ID == second.ID {
		first = detail.Identities[1]
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{first.ID, second.ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = svc.DeleteIdentity(ctx, scope, created.ID, id)
		}()
	}
	wg.Wait()

	failures := 0
	for _, err := range errs {
		if errors.Is(err, customer.ErrLastIdentity) {
			failures++
		} else if err != nil {
			t.Fatalf("unexpected concurrent delete error: %v", err)
		}
	}
	if failures != 1 {
		t.Fatalf("exactly one delete should hit last-identity guard, got %d (errs=%v)", failures, errs)
	}
	after, err := svc.Detail(ctx, scope, created.ID)
	if err != nil || len(after.Identities) != 1 {
		t.Fatalf("one identity must survive: err=%v identities=%+v", err, after.Identities)
	}
}

// A9：备注——合法 / 空 content / >500 字 / merged 客户；详情倒序（含并列时间戳）。
func TestAddNoteAndDetailOrdering(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()
	created := createActiveCustomer(t, svc, scope, "阿芷")

	note, err := svc.AddNote(ctx, scope, created.ID, "喜欢胶片感，修图别过度液化")
	if err != nil {
		t.Fatalf("add note: %v", err)
	}
	if note.CustomerID != created.ID || note.AccountID != "acct-a" || note.Content == "" {
		t.Fatalf("note shape mismatch: %+v", note)
	}

	if _, err := svc.AddNote(ctx, scope, created.ID, "   "); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("empty content: want validation, got %v", err)
	}
	if _, err := svc.AddNote(ctx, scope, created.ID, strings.Repeat("字", 501)); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("content >500: want validation, got %v", err)
	}
	if _, err := svc.AddNote(ctx, scope, created.ID, strings.Repeat("字", 500)); err != nil {
		t.Fatalf("content =500 should pass: %v", err)
	}

	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_merged", DisplayName: "已合并",
		Channel: customer.ChannelOther, Status: customer.StatusMerged, CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		Handle: "merged-h",
	})
	if _, err := svc.AddNote(ctx, scope, "cus_merged", "写不进去"); !errors.Is(err, customer.ErrCustomerMerged) {
		t.Fatalf("merged customer note: want ErrCustomerMerged, got %v", err)
	}

	// 并列时间戳倒序：直接落两条同 created_at 的备注，断言 id DESC 决序。
	moment := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"note_a", "note_b"} {
		if err := scope.Insert(ctx, "customer_notes",
			[]string{"id", "customer_id", "content", "created_at"},
			id, created.ID, "备注 "+id, moment,
		); err != nil {
			t.Fatalf("seed note %s: %v", id, err)
		}
	}
	detail, err := svc.Detail(ctx, scope, created.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.Notes) != 4 {
		t.Fatalf("expect 4 notes, got %d", len(detail.Notes))
	}
	for i := 1; i < len(detail.Notes); i++ {
		prev, curr := detail.Notes[i-1], detail.Notes[i]
		if prev.CreatedAt.Before(curr.CreatedAt) {
			t.Fatalf("notes not in created_at DESC order: %+v", detail.Notes)
		}
		if prev.CreatedAt.Equal(curr.CreatedAt) && prev.ID < curr.ID {
			t.Fatalf("tie must break by id DESC: %+v", detail.Notes)
		}
	}
}
