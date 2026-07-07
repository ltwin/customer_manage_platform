package customer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(ctr); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}
	return url
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func customerService() *customer.Service {
	return customer.NewService(customer.NewPostgresRepository())
}

func createAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func TestCreateListAndDetail(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()

	created, err := svc.Create(ctx, scope, customer.CreateInput{
		DisplayName: "  阿芷  ",
		Channel:     customer.ChannelXiaohongshu,
		Identities: []customer.IdentityInput{
			{Platform: customer.PlatformWechat, Handle: "azhi-photo"},
			{Platform: customer.PlatformXiaohongshu, Handle: "阿芷写真", Remark: strPtr("主页私信")},
		},
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if created.ID == "" || created.AccountID != "acct-a" || created.DisplayName != "阿芷" || created.Status != customer.StatusActive {
		t.Fatalf("created customer shape mismatch: %+v", created)
	}

	list, err := svc.List(ctx, scope, customer.ListFilter{})
	if err != nil {
		t.Fatalf("list customers: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("list should return one customer, got %+v", list)
	}
	if list.Items[0].OrdersCount != 0 || list.Items[0].LastShotAt != nil {
		t.Fatalf("list aggregate zero shape mismatch: %+v", list.Items[0])
	}

	detail, err := svc.Detail(ctx, scope, created.ID)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(detail.Identities) != 2 || len(detail.Notes) != 0 || detail.Referrer != nil {
		t.Fatalf("detail relation shape mismatch: %+v", detail)
	}
	if detail.Stats.OrdersCount != 0 || detail.Stats.TotalOrderAmount != 0 || detail.Stats.LastShotAt != nil {
		t.Fatalf("detail stats must be zero shape, got %+v", detail.Stats)
	}
}

func TestCreateValidationRollback(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()

	cases := map[string]customer.CreateInput{
		"missing display": {
			Channel:    customer.ChannelOther,
			Identities: []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "ok"}},
		},
		"empty identities": {
			DisplayName: "阿芷",
			Channel:     customer.ChannelOther,
		},
		"invalid platform": {
			DisplayName: "阿芷",
			Channel:     customer.ChannelOther,
			Identities:  []customer.IdentityInput{{Platform: "bad", Handle: "ok"}},
		},
		"empty handle": {
			DisplayName: "阿芷",
			Channel:     customer.ChannelOther,
			Identities:  []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "  "}},
		},
	}
	for name, input := range cases {
		before, err := scope.Count(ctx, "customers", "")
		if err != nil {
			t.Fatalf("%s: count before: %v", name, err)
		}
		_, err = svc.Create(ctx, scope, input)
		if !errors.Is(err, customer.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
		after, err := scope.Count(ctx, "customers", "")
		if err != nil {
			t.Fatalf("%s: count after: %v", name, err)
		}
		if after != before {
			t.Fatalf("%s: validation failure wrote partial customer, before=%d after=%d", name, before, after)
		}
	}
}

func TestReferralVisibility(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := customerService()

	referrer, err := svc.Create(ctx, scopeA, customer.CreateInput{
		DisplayName: "介绍人",
		Channel:     customer.ChannelOther,
		Identities:  []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "referrer"}},
	})
	if err != nil {
		t.Fatalf("create referrer: %v", err)
	}
	child, err := svc.Create(ctx, scopeA, customer.CreateInput{
		DisplayName:        "被介绍客户",
		Channel:            customer.ChannelReferral,
		ReferrerCustomerID: strPtr(referrer.ID),
		Identities:         []customer.IdentityInput{{Platform: customer.PlatformQQ, Handle: "child"}},
	})
	if err != nil {
		t.Fatalf("create referred customer: %v", err)
	}
	detail, err := svc.Detail(ctx, scopeA, child.ID)
	if err != nil {
		t.Fatalf("detail referred customer: %v", err)
	}
	if detail.Referrer == nil || detail.Referrer.ID != referrer.ID {
		t.Fatalf("detail should include referrer summary, got %+v", detail.Referrer)
	}

	_, err = svc.Create(ctx, scopeA, customer.CreateInput{
		DisplayName: "缺介绍人",
		Channel:     customer.ChannelReferral,
		Identities:  []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "missing-ref"}},
	})
	if !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("missing referrer: want validation, got %v", err)
	}

	_, err = svc.Create(ctx, scopeA, customer.CreateInput{
		DisplayName:        "不存在介绍人",
		Channel:            customer.ChannelReferral,
		ReferrerCustomerID: strPtr("cus_missing"),
		Identities:         []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "not-found-ref"}},
	})
	if !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("missing referrer id: want not found, got %v", err)
	}

	otherAccountCustomer, err := svc.Create(ctx, scopeB, customer.CreateInput{
		DisplayName: "B 账号客户",
		Channel:     customer.ChannelOther,
		Identities:  []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "b-ref"}},
	})
	if err != nil {
		t.Fatalf("create B customer: %v", err)
	}
	_, err = svc.Create(ctx, scopeA, customer.CreateInput{
		DisplayName:        "跨账号介绍",
		Channel:            customer.ChannelReferral,
		ReferrerCustomerID: strPtr(otherAccountCustomer.ID),
		Identities:         []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "cross-ref"}},
	})
	if !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("cross-account referrer: want not found, got %v", err)
	}
}

func TestListFiltersAndPagination(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := customerService()

	seedCustomer(t, scopeA, seedCustomerInput{
		ID: "cus_old", DisplayName: "旧客户", RealName: "阿芷实名", Phone: "13800000001",
		Channel: customer.ChannelOther, Status: customer.StatusActive, CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		Handle: "wx-azhi",
	})
	seedCustomer(t, scopeA, seedCustomerInput{
		ID: "cus_new", DisplayName: "小红书客户", Phone: "13900000001",
		Channel: customer.ChannelXiaohongshu, Status: customer.StatusActive, CreatedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
		Handle: "needle-xhs",
	})
	seedCustomer(t, scopeA, seedCustomerInput{
		ID: "cus_archived", DisplayName: "归档客户",
		Channel: customer.ChannelXiaohongshu, Status: customer.StatusArchived, CreatedAt: time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC),
		Handle: "archived-needle",
	})
	seedCustomer(t, scopeB, seedCustomerInput{
		ID: "cus_b", DisplayName: "B 账号客户",
		Channel: customer.ChannelXiaohongshu, Status: customer.StatusActive, CreatedAt: time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC),
		Handle: "needle-xhs",
	})

	firstPage, err := svc.List(ctx, scopeA, customer.ListFilter{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if firstPage.Total != 2 || len(firstPage.Items) != 1 || firstPage.Items[0].ID != "cus_new" {
		t.Fatalf("default list should page active A customers by created_at desc, got %+v", firstPage)
	}

	byPhone, err := svc.List(ctx, scopeA, customer.ListFilter{Q: "13800000001"})
	if err != nil {
		t.Fatalf("list by phone: %v", err)
	}
	if byPhone.Total != 1 || byPhone.Items[0].ID != "cus_old" {
		t.Fatalf("q should match phone, got %+v", byPhone)
	}

	byHandleAndChannel, err := svc.List(ctx, scopeA, customer.ListFilter{Q: "needle", Channel: customer.ChannelXiaohongshu})
	if err != nil {
		t.Fatalf("list by handle/channel: %v", err)
	}
	if byHandleAndChannel.Total != 1 || byHandleAndChannel.Items[0].ID != "cus_new" {
		t.Fatalf("q/channel should stay account-scoped and active by default, got %+v", byHandleAndChannel)
	}

	allXHS, err := svc.List(ctx, scopeA, customer.ListFilter{Channel: customer.ChannelXiaohongshu, Status: customer.StatusAll})
	if err != nil {
		t.Fatalf("list all xhs: %v", err)
	}
	if allXHS.Total != 2 {
		t.Fatalf("status=all should include archived same-account customers, got %+v", allXHS)
	}

	if _, err := svc.List(ctx, scopeA, customer.ListFilter{Page: -1}); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("invalid page should validate, got %v", err)
	}
}

// 跨页一致性：同 created_at 的客户在连续翻页中不重复、不丢行（REV-001 / R1）。
func TestListPaginationStableAcrossPages(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()

	sameMoment := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	total := 5
	for i := 0; i < total; i++ {
		id := fmt.Sprintf("cus_same_%d", i)
		seedCustomer(t, scope, seedCustomerInput{
			ID: id, DisplayName: "同刻客户" + id,
			Channel: customer.ChannelOther, Status: customer.StatusActive, CreatedAt: sameMoment,
			Handle: "h-" + id,
		})
	}

	seen := make(map[string]bool, total)
	for page := 1; page <= 3; page++ {
		result, err := svc.List(ctx, scope, customer.ListFilter{Page: page, PageSize: 2})
		if err != nil {
			t.Fatalf("list page %d: %v", page, err)
		}
		if result.Total != int64(total) {
			t.Fatalf("page %d total = %d, want %d", page, result.Total, total)
		}
		for _, item := range result.Items {
			if seen[item.ID] {
				t.Fatalf("customer %s duplicated across pages", item.ID)
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != total {
		t.Fatalf("pages united %d customers, want %d (lost rows)", len(seen), total)
	}
}

type seedCustomerInput struct {
	ID          string
	DisplayName string
	RealName    string
	Phone       string
	Channel     string
	Status      string
	CreatedAt   time.Time
	Handle      string
}

func seedCustomer(t *testing.T, scope store.AccountScope, input seedCustomerInput) {
	t.Helper()
	ctx := context.Background()
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "display_name", "real_name", "phone", "channel", "status", "created_at"},
		input.ID, input.DisplayName, nullableString(input.RealName), nullableString(input.Phone), input.Channel, input.Status, input.CreatedAt,
	); err != nil {
		t.Fatalf("seed customer %s: %v", input.ID, err)
	}
	if err := scope.Insert(ctx, "social_identities",
		[]string{"id", "customer_id", "platform", "handle"},
		"sid_"+input.ID, input.ID, customer.PlatformWechat, input.Handle,
	); err != nil {
		t.Fatalf("seed identity %s: %v", input.ID, err)
	}
}

func strPtr(value string) *string {
	return &value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
