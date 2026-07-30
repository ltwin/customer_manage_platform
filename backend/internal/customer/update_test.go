package customer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func createActiveCustomer(t *testing.T, svc *customer.Service, scope store.AccountScope, name string) customer.Customer {
	t.Helper()
	created, err := svc.Create(context.Background(), scope, customer.CreateInput{
		DisplayName: name,
		Channel:     customer.ChannelOther,
		Identities:  []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "wx-" + name}},
	})
	if err != nil {
		t.Fatalf("create customer %s: %v", name, err)
	}
	return created
}

func nullString() nullable.Nullable[string] {
	var v nullable.Nullable[string]
	v.SetNull()
	return v
}

func valueString(value string) nullable.Nullable[string] {
	var v nullable.Nullable[string]
	v.Set(value)
	return v
}

// A3：渐进字段更新、显式 null 清空、非法输入 400。
func TestUpdateProgressiveFields(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()
	created := createActiveCustomer(t, svc, scope, "阿芷")

	updated, err := svc.Update(ctx, scope, created.ID, customer.UpdateInput{
		DisplayName: strPtr("  赵芷  "),
		RealName:    valueString("赵芷"),
		Phone:       valueString("13800000001"),
		Birthday:    valueString("03-15"),
	})
	if err != nil {
		t.Fatalf("update fields: %v", err)
	}
	if updated.DisplayName != "赵芷" || deref(updated.RealName) != "赵芷" || deref(updated.Phone) != "13800000001" || deref(updated.Birthday) != "03-15" {
		t.Fatalf("updated fields mismatch: %+v", updated)
	}

	updated, err = svc.Update(ctx, scope, created.ID, customer.UpdateInput{Birthday: valueString("1999-02-28")})
	if err != nil || deref(updated.Birthday) != "1999-02-28" {
		t.Fatalf("full-date birthday: err=%v customer=%+v", err, updated)
	}

	updated, err = svc.Update(ctx, scope, created.ID, customer.UpdateInput{Phone: nullString()})
	if err != nil {
		t.Fatalf("clear phone with null: %v", err)
	}
	if updated.Phone != nil {
		t.Fatalf("phone should be cleared, got %q", *updated.Phone)
	}
	if deref(updated.RealName) != "赵芷" {
		t.Fatalf("real_name should stay untouched, got %+v", updated)
	}

	invalidCases := map[string]customer.UpdateInput{
		"invalid birthday": {Birthday: valueString("13-45")},
		"empty display":    {DisplayName: strPtr("  ")},
		"empty real_name":  {RealName: valueString(" ")},
		"invalid status":   {Status: strPtr(customer.StatusMerged)},
		"invalid channel":  {Channel: strPtr("bad")},
	}
	for name, input := range invalidCases {
		if _, err := svc.Update(ctx, scope, created.ID, input); !errors.Is(err, customer.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
}

// A4：channel↔referrer 联动矩阵。
func TestUpdateChannelReferrerLinkage(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := customerService()

	target := createActiveCustomer(t, svc, scopeA, "被介绍")
	activeRef := createActiveCustomer(t, svc, scopeA, "活跃介绍人")
	archivedRef := createActiveCustomer(t, svc, scopeA, "归档介绍人")
	if _, err := svc.Update(ctx, scopeA, archivedRef.ID, customer.UpdateInput{Status: strPtr(customer.StatusArchived)}); err != nil {
		t.Fatalf("archive referrer: %v", err)
	}
	crossRef := createActiveCustomer(t, svc, scopeB, "跨账号介绍人")

	if _, err := svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
		Channel: strPtr(customer.ChannelReferral),
	}); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("referral without referrer: want validation, got %v", err)
	}

	if _, err := svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
		Channel:            strPtr(customer.ChannelReferral),
		ReferrerCustomerID: strPtr(target.ID),
	}); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("self referrer: want validation, got %v", err)
	}

	if _, err := svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
		ReferrerCustomerID: strPtr(activeRef.ID),
	}); !errors.Is(err, customer.ErrValidation) {
		t.Fatalf("lone referrer while channel non-referral: want validation, got %v", err)
	}

	for name, refID := range map[string]string{
		"archived referrer":      archivedRef.ID,
		"cross-account referrer": crossRef.ID,
		"missing referrer":       "cus_missing",
	} {
		if _, err := svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
			Channel:            strPtr(customer.ChannelReferral),
			ReferrerCustomerID: strPtr(refID),
		}); !errors.Is(err, customer.ErrNotFound) {
			t.Fatalf("%s: want not found, got %v", name, err)
		}
	}

	updated, err := svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
		Channel:            strPtr(customer.ChannelReferral),
		ReferrerCustomerID: strPtr(activeRef.ID),
	})
	if err != nil || deref(updated.ReferrerCustomerID) != activeRef.ID || updated.Channel != customer.ChannelReferral {
		t.Fatalf("set referral with active referrer: err=%v customer=%+v", err, updated)
	}

	// 现渠道已是 referral：单独换介绍人是合法更新。
	otherRef := createActiveCustomer(t, svc, scopeA, "另一介绍人")
	updated, err = svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
		ReferrerCustomerID: strPtr(otherRef.ID),
	})
	if err != nil || deref(updated.ReferrerCustomerID) != otherRef.ID {
		t.Fatalf("replace referrer under referral: err=%v customer=%+v", err, updated)
	}

	updated, err = svc.Update(ctx, scopeA, target.ID, customer.UpdateInput{
		Channel: strPtr(customer.ChannelDouyin),
	})
	if err != nil {
		t.Fatalf("switch away from referral: %v", err)
	}
	if updated.Channel != customer.ChannelDouyin || updated.ReferrerCustomerID != nil {
		t.Fatalf("referrer should be auto-cleared after leaving referral, got %+v", updated)
	}
}

// A4b：建档面 referrer 校验收紧为 active（D8）。
func TestCreateReferrerMustBeActive(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()

	archivedRef := createActiveCustomer(t, svc, scope, "归档介绍人")
	if _, err := svc.Update(ctx, scope, archivedRef.ID, customer.UpdateInput{Status: strPtr(customer.StatusArchived)}); err != nil {
		t.Fatalf("archive referrer: %v", err)
	}
	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_merged_ref", DisplayName: "已合并介绍人",
		Channel: customer.ChannelOther, Status: customer.StatusMerged, CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		Handle: "merged-ref",
	})

	for name, refID := range map[string]string{
		"archived referrer": archivedRef.ID,
		"merged referrer":   "cus_merged_ref",
	} {
		_, err := svc.Create(ctx, scope, customer.CreateInput{
			DisplayName:        "新客户",
			Channel:            customer.ChannelReferral,
			ReferrerCustomerID: strPtr(refID),
			Identities:         []customer.IdentityInput{{Platform: customer.PlatformWechat, Handle: "new-" + name}},
		})
		if !errors.Is(err, customer.ErrNotFound) {
			t.Fatalf("%s: want not found, got %v", name, err)
		}
	}
}

// A5 + A6：状态矩阵与归档列表语义。
func TestUpdateStatusMatrixAndArchivedList(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := customerService()
	created := createActiveCustomer(t, svc, scope, "阿芷")

	archived, err := svc.Update(ctx, scope, created.ID, customer.UpdateInput{Status: strPtr(customer.StatusArchived)})
	if err != nil || archived.Status != customer.StatusArchived {
		t.Fatalf("archive: err=%v customer=%+v", err, archived)
	}

	// A6：缺省列表隐藏 archived；显式 status 可查回。
	defaultList, err := svc.List(ctx, scope, customer.ListFilter{})
	if err != nil || defaultList.Total != 0 {
		t.Fatalf("default list should hide archived: err=%v list=%+v", err, defaultList)
	}
	archivedList, err := svc.List(ctx, scope, customer.ListFilter{Status: customer.StatusArchived})
	if err != nil || archivedList.Total != 1 {
		t.Fatalf("status=archived should return archived: err=%v list=%+v", err, archivedList)
	}
	allList, err := svc.List(ctx, scope, customer.ListFilter{Status: customer.StatusAll})
	if err != nil || allList.Total != 1 {
		t.Fatalf("status=all should return archived: err=%v list=%+v", err, allList)
	}

	// D4：archived 档案可继续编辑。
	edited, err := svc.Update(ctx, scope, created.ID, customer.UpdateInput{RealName: valueString("赵芷")})
	if err != nil || deref(edited.RealName) != "赵芷" {
		t.Fatalf("archived customer should stay editable: err=%v customer=%+v", err, edited)
	}

	restored, err := svc.Update(ctx, scope, created.ID, customer.UpdateInput{Status: strPtr(customer.StatusActive)})
	if err != nil || restored.Status != customer.StatusActive {
		t.Fatalf("restore: err=%v customer=%+v", err, restored)
	}

	seedCustomer(t, scope, seedCustomerInput{
		ID: "cus_merged", DisplayName: "已合并客户",
		Channel: customer.ChannelOther, Status: customer.StatusMerged, CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC),
		Handle: "merged-handle",
	})
	if _, err := svc.Update(ctx, scope, "cus_merged", customer.UpdateInput{RealName: valueString("任意")}); !errors.Is(err, customer.ErrCustomerMerged) {
		t.Fatalf("patch merged customer: want ErrCustomerMerged, got %v", err)
	}
}

// A12（PATCH 面）：跨账号 PATCH 一律 404，无跨账号写入。
func TestUpdateCrossAccountIsolation(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := customerService()
	created := createActiveCustomer(t, svc, scopeA, "A 客户")

	if _, err := svc.Update(ctx, scopeB, created.ID, customer.UpdateInput{RealName: valueString("越权")}); !errors.Is(err, customer.ErrNotFound) {
		t.Fatalf("cross-account patch: want not found, got %v", err)
	}
	detail, err := svc.Detail(ctx, scopeA, created.ID)
	if err != nil || detail.RealName != nil {
		t.Fatalf("cross-account patch must not write: err=%v detail=%+v", err, detail)
	}
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
