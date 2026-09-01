package customer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

// A10：merge 全迁移面——身份 / 备注改挂 target、C 的 referrer 重定向、
// target 自指清空（channel 保持 referral）、source 置 merged + 指针 + 0 身份。
func TestMergeMigratesEverything(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()

	target := createActiveCustomer(t, svc, scope, "target")
	source := createActiveCustomer(t, svc, scope, "source")
	if _, err := svc.AddIdentity(ctx, scope, source.ID, customer.IdentityInput{Platform: customer.PlatformQQ, Handle: "src_qq"}); err != nil {
		t.Fatalf("add source identity: %v", err)
	}
	for _, content := range []string{"备注一", "备注二"} {
		if _, err := svc.AddNote(ctx, scope, source.ID, content); err != nil {
			t.Fatalf("add source note: %v", err)
		}
	}
	// target 的介绍人恰为 source（merge 后自指 → 清空）。
	if _, err := svc.Update(ctx, scope, target.ID, customer.UpdateInput{
		Channel:            strPtr(customer.ChannelReferral),
		ReferrerCustomerID: strPtr(source.ID),
	}); err != nil {
		t.Fatalf("point target referrer at source: %v", err)
	}
	// 客户 C 的 referrer 指向 source（merge 后重定向到 target）。
	c, err := svc.Create(ctx, scope, customer.CreateInput{
		DisplayName:        "客户C",
		Channel:            customer.ChannelReferral,
		ReferrerCustomerID: strPtr(source.ID),
		Identities:         []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "c-wx"}},
	})
	if err != nil {
		t.Fatalf("create customer C: %v", err)
	}

	mergedTarget, err := svc.Merge(ctx, scope, target.ID, source.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if mergedTarget.Status != customer.StatusActive {
		t.Fatalf("target should stay active, got %+v", mergedTarget)
	}
	if mergedTarget.Channel != customer.ChannelReferral || mergedTarget.ReferrerCustomerID != nil {
		t.Fatalf("target self-ref must be cleared with channel kept (referral+空 referrer 历史态), got %+v", mergedTarget)
	}

	targetDetail, err := svc.Detail(ctx, scope, target.ID)
	if err != nil {
		t.Fatalf("target detail: %v", err)
	}
	if len(targetDetail.Identities) != 3 {
		t.Fatalf("target should own 3 identities after merge, got %d", len(targetDetail.Identities))
	}
	if len(targetDetail.Notes) != 2 {
		t.Fatalf("target should own source notes, got %d", len(targetDetail.Notes))
	}

	sourceDetail, err := svc.Detail(ctx, scope, source.ID)
	if err != nil {
		t.Fatalf("source detail: %v", err)
	}
	if sourceDetail.Status != customer.StatusMerged || deref(sourceDetail.MergedIntoCustomerID) != target.ID {
		t.Fatalf("source should be merged with pointer, got %+v", sourceDetail.Customer)
	}
	if len(sourceDetail.Identities) != 0 || len(sourceDetail.Notes) != 0 {
		t.Fatalf("merged source shell should own nothing, got %+v", sourceDetail)
	}

	cDetail, err := svc.Detail(ctx, scope, c.ID)
	if err != nil {
		t.Fatalf("customer C detail: %v", err)
	}
	if deref(cDetail.ReferrerCustomerID) != target.ID {
		t.Fatalf("C referrer should be redirected to target, got %+v", cDetail.Customer)
	}
}

// A11：merge 错误矩阵——source / target 非 active、source==target、不存在或跨账号。
func TestMergeErrorMatrix(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := customerService()

	target := createActiveCustomer(t, svc, scopeA, "target")
	archived := createActiveCustomer(t, svc, scopeA, "archived")
	if _, err := svc.Update(ctx, scopeA, archived.ID, customer.UpdateInput{Status: strPtr(customer.StatusArchived)}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	if _, err := svc.Merge(ctx, scopeA, target.ID, archived.ID); !errors.Is(err, customer.ErrMergeConflict) {
		t.Fatalf("source non-active: want merge_conflict, got %v", err)
	}
	if _, err := svc.Merge(ctx, scopeA, archived.ID, target.ID); !errors.Is(err, customer.ErrMergeConflict) {
		t.Fatalf("target non-active: want merge_conflict, got %v", err)
	}
	if _, err := svc.Merge(ctx, scopeA, target.ID, target.ID); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("source==target: want validation, got %v", err)
	}
	if _, err := svc.Merge(ctx, scopeA, target.ID, "cus_missing"); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("missing source: want not found, got %v", err)
	}

	// 已 merged 的 source 重复提交：source 非 active → merge_conflict（天然防重放）。
	source := createActiveCustomer(t, svc, scopeA, "source")
	if _, err := svc.Merge(ctx, scopeA, target.ID, source.ID); err != nil {
		t.Fatalf("first merge: %v", err)
	}
	if _, err := svc.Merge(ctx, scopeA, target.ID, source.ID); !errors.Is(err, customer.ErrMergeConflict) {
		t.Fatalf("replayed merge: want merge_conflict, got %v", err)
	}

	// A12（merge 面）：账号 B 拿不到 A 的客户。
	bTarget := createActiveCustomer(t, svc, scopeB, "b-target")
	if _, err := svc.Merge(ctx, scopeB, bTarget.ID, target.ID); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("cross-account source: want not found, got %v", err)
	}
	if aDetail, err := svc.Detail(ctx, scopeA, target.ID); err != nil || aDetail.Status != customer.StatusActive {
		t.Fatalf("cross-account merge must not touch A's customer: err=%v detail=%+v", err, aDetail)
	}
}

// 事务原子性：中途失败（备注迁移被注入的 CHECK 约束拦截）时，
// 已执行的身份迁移整体回滚，双方与指针全部保持原状。
func TestMergeRollbackOnMidTransactionFailure(t *testing.T) {
	ctx := context.Background()
	url, database := startPostgresDatabase(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.CreateAccount(ctx, "acct-a", "test-hash"); err != nil {
		t.Fatalf("create account: %v", err)
	}
	scope := s.ScopeFor(auth.AccountContext{AccountID: "acct-a"})
	svc := customerService()

	target := createActiveCustomer(t, svc, scope, "target")
	source := createActiveCustomer(t, svc, scope, "source")
	if _, err := svc.AddNote(ctx, scope, source.ID, "会被拦的备注"); err != nil {
		t.Fatalf("add source note: %v", err)
	}

	// 注入（容器内 psql 执行 DDL，pgx 直连被 depguard 限制在 store 系包）：
	// 禁止任何备注挂到 target → merge 的备注迁移步必然失败。
	execSQL := func(statement string) { storetest.ExecSQL(t, database, statement) }
	execSQL("ALTER TABLE customer_notes ADD CONSTRAINT tmp_block_target CHECK (customer_id <> '" + target.ID + "')")

	if _, err := svc.Merge(ctx, scope, target.ID, source.ID); err == nil {
		t.Fatal("merge should fail on injected constraint")
	}

	sourceDetail, err := svc.Detail(ctx, scope, source.ID)
	if err != nil {
		t.Fatalf("source detail after failed merge: %v", err)
	}
	if sourceDetail.Status != customer.StatusActive || len(sourceDetail.Identities) != 1 || len(sourceDetail.Notes) != 1 {
		t.Fatalf("failed merge must roll back everything, got %+v", sourceDetail)
	}
	targetDetail, err := svc.Detail(ctx, scope, target.ID)
	if err != nil {
		t.Fatalf("target detail after failed merge: %v", err)
	}
	if len(targetDetail.Identities) != 1 || len(targetDetail.Notes) != 0 {
		t.Fatalf("target must stay untouched after rollback, got %+v", targetDetail)
	}

	execSQL("ALTER TABLE customer_notes DROP CONSTRAINT tmp_block_target")
	if _, err := svc.Merge(ctx, scope, target.ID, source.ID); err != nil {
		t.Fatalf("merge should succeed after constraint removed: %v", err)
	}
}
