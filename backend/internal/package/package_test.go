package pkgcatalog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func startPostgresContainer(t *testing.T) (string, *tcpostgres.PostgresContainer) {
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
	return url, ctr
}

func openStore(t *testing.T) (*store.Store, *tcpostgres.PostgresContainer) {
	t.Helper()
	url, ctr := startPostgresContainer(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(context.Background(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	return s, ctr
}

func packageService() *pkgcatalog.Service {
	return pkgcatalog.NewService(pkgcatalog.NewPostgresRepository())
}

func createAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func TestCreateListAndValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := packageService()

	created, err := svc.Create(ctx, scope, pkgcatalog.CreateInput{
		Name:             "  轻写真 90 分钟  ",
		ShootType:        pkgcatalog.ShootTypePortrait,
		PricingMode:      pkgcatalog.PricingModePerDuration,
		BasePrice:        0,
		DurationMinutes:  intPtr(90),
		ShotCountMin:     intPtr(60),
		ShotCountMax:     intPtr(80),
		RawDeliveryCount: intPtr(60),
		RetouchCount:     intPtr(8),
		Note:             strPtr("  含棚租  "),
	})
	if err != nil {
		t.Fatalf("create package: %v", err)
	}
	if created.ID == "" || created.AccountID != "acct-a" || created.Name != "轻写真 90 分钟" || created.BasePrice != 0 || created.Status != pkgcatalog.StatusActive {
		t.Fatalf("created package shape mismatch: %+v", created)
	}
	if got := derefInt(created.DurationMinutes); got != 90 {
		t.Fatalf("duration should be persisted, got %+v", created)
	}
	if got := derefString(created.Note); got != "含棚租" {
		t.Fatalf("note should be trimmed, got %+v", created)
	}

	list, err := svc.List(ctx, scope, pkgcatalog.ListFilter{})
	if err != nil {
		t.Fatalf("list packages: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].OrdersCount != 0 {
		t.Fatalf("list should return one zero-aggregate package, got %+v", list)
	}

	cases := map[string]pkgcatalog.CreateInput{
		"missing name": {
			ShootType: pkgcatalog.ShootTypePortrait, PricingMode: pkgcatalog.PricingModeFixed, BasePrice: 1,
		},
		"invalid shoot_type": {
			Name: "套系", ShootType: "bad", PricingMode: pkgcatalog.PricingModeFixed, BasePrice: 1,
		},
		"invalid pricing_mode": {
			Name: "套系", ShootType: pkgcatalog.ShootTypeOther, PricingMode: "bad", BasePrice: 1,
		},
		"negative base_price": {
			Name: "套系", ShootType: pkgcatalog.ShootTypeOther, PricingMode: pkgcatalog.PricingModeFixed, BasePrice: -1,
		},
		"oversized base_price": {
			Name: "套系", ShootType: pkgcatalog.ShootTypeOther, PricingMode: pkgcatalog.PricingModeFixed, BasePrice: overPostgresInteger,
		},
		"negative duration": {
			Name: "套系", ShootType: pkgcatalog.ShootTypeOther, PricingMode: pkgcatalog.PricingModeFixed, BasePrice: 1, DurationMinutes: intPtr(-1),
		},
		"oversized duration": {
			Name: "套系", ShootType: pkgcatalog.ShootTypeOther, PricingMode: pkgcatalog.PricingModeFixed, BasePrice: 1, DurationMinutes: intPtr(overPostgresInteger),
		},
		"invalid shot range": {
			Name: "套系", ShootType: pkgcatalog.ShootTypeOther, PricingMode: pkgcatalog.PricingModeFixed, BasePrice: 1, ShotCountMin: intPtr(9), ShotCountMax: intPtr(8),
		},
	}
	for name, input := range cases {
		before, err := scope.Count(ctx, "packages", "")
		if err != nil {
			t.Fatalf("%s: count before: %v", name, err)
		}
		_, err = svc.Create(ctx, scope, input)
		if !errors.Is(err, pkgcatalog.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
		after, err := scope.Count(ctx, "packages", "")
		if err != nil {
			t.Fatalf("%s: count after: %v", name, err)
		}
		if after != before {
			t.Fatalf("%s: validation failure wrote package, before=%d after=%d", name, before, after)
		}
	}
}

func TestUpdatePartialAndClearableFields(t *testing.T) {
	ctx := context.Background()
	s, _ := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := packageService()
	created := createPackage(t, svc, scope, "套系")

	updated, err := svc.Update(ctx, scope, created.ID, pkgcatalog.UpdateInput{
		BasePrice: intPtr(72000),
	})
	if err != nil {
		t.Fatalf("update base_price: %v", err)
	}
	if updated.BasePrice != 72000 || derefInt(updated.DurationMinutes) != 90 || derefInt(updated.RetouchCount) != 8 {
		t.Fatalf("partial update should keep untouched fields, got %+v", updated)
	}

	updated, err = svc.Update(ctx, scope, created.ID, pkgcatalog.UpdateInput{
		DurationMinutes:  nullInt(),
		RawDeliveryCount: nullInt(),
		RetouchCount:     valueInt(0),
		Note:             strPtr("  改价备注  "),
	})
	if err != nil {
		t.Fatalf("clear delivery fields: %v", err)
	}
	if updated.DurationMinutes != nil || updated.RawDeliveryCount != nil || derefInt(updated.RetouchCount) != 0 || derefString(updated.Note) != "改价备注" {
		t.Fatalf("clearable fields mismatch: %+v", updated)
	}
	if derefInt(updated.ShotCountMin) != 60 || derefInt(updated.ShotCountMax) != 80 {
		t.Fatalf("untouched shot range should stay, got %+v", updated)
	}

	updated, err = svc.Update(ctx, scope, created.ID, pkgcatalog.UpdateInput{Note: strPtr("  ")})
	if err != nil {
		t.Fatalf("clear note: %v", err)
	}
	if updated.Note != nil {
		t.Fatalf("blank note should clear existing note to null, got %+v", updated.Note)
	}

	invalidCases := map[string]pkgcatalog.UpdateInput{
		"empty name":           {Name: strPtr("  ")},
		"invalid status":       {Status: strPtr("merged")},
		"negative base_price":  {BasePrice: intPtr(-1)},
		"oversized base_price": {BasePrice: intPtr(overPostgresInteger)},
		"negative retouch":     {RetouchCount: valueInt(-1)},
		"oversized retouch":    {RetouchCount: valueInt(overPostgresInteger)},
		"invalid shot range":   {ShotCountMin: valueInt(99)},
	}
	for name, input := range invalidCases {
		if _, err := svc.Update(ctx, scope, created.ID, input); !errors.Is(err, pkgcatalog.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestListStatusPaginationAndIsolation(t *testing.T) {
	ctx := context.Background()
	s, _ := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := packageService()

	seedPackage(t, scopeA, seedPackageInput{ID: "pkg_old", Name: "旧套系", Status: pkgcatalog.StatusActive, CreatedAt: time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)})
	seedPackage(t, scopeA, seedPackageInput{ID: "pkg_new", Name: "新套系", Status: pkgcatalog.StatusActive, CreatedAt: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)})
	seedPackage(t, scopeA, seedPackageInput{ID: "pkg_archived", Name: "下架套系", Status: pkgcatalog.StatusArchived, CreatedAt: time.Date(2026, 7, 3, 9, 0, 0, 0, time.UTC)})
	seedPackage(t, scopeB, seedPackageInput{ID: "pkg_b", Name: "B 套系", Status: pkgcatalog.StatusActive, CreatedAt: time.Date(2026, 7, 4, 9, 0, 0, 0, time.UTC)})

	firstPage, err := svc.List(ctx, scopeA, pkgcatalog.ListFilter{Page: 1, PageSize: 1})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if firstPage.Total != 2 || len(firstPage.Items) != 1 || firstPage.Items[0].ID != "pkg_new" {
		t.Fatalf("default list should page active A packages by created_at desc, got %+v", firstPage)
	}

	archived, err := svc.List(ctx, scopeA, pkgcatalog.ListFilter{Status: pkgcatalog.StatusArchived})
	if err != nil || archived.Total != 1 || archived.Items[0].ID != "pkg_archived" {
		t.Fatalf("status=archived mismatch: err=%v list=%+v", err, archived)
	}

	all, err := svc.List(ctx, scopeA, pkgcatalog.ListFilter{Status: pkgcatalog.StatusAll})
	if err != nil || all.Total != 3 {
		t.Fatalf("status=all should include active+archived same-account packages: err=%v list=%+v", err, all)
	}

	for name, filter := range map[string]pkgcatalog.ListFilter{
		"bad status":    {Status: "merged"},
		"bad page":      {Page: -1},
		"overflow page": {Page: overPostgresInteger, PageSize: 100},
		"bad size":      {PageSize: -1},
		"huge size":     {PageSize: 101},
	} {
		if _, err := svc.List(ctx, scopeA, filter); !errors.Is(err, pkgcatalog.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestUpdateStatusAndDeleteIsolation(t *testing.T) {
	ctx := context.Background()
	s, _ := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := packageService()
	created := createPackage(t, svc, scopeA, "A 套系")

	archived, err := svc.Update(ctx, scopeA, created.ID, pkgcatalog.UpdateInput{Status: strPtr(pkgcatalog.StatusArchived)})
	if err != nil || archived.Status != pkgcatalog.StatusArchived {
		t.Fatalf("archive: err=%v package=%+v", err, archived)
	}
	restored, err := svc.Update(ctx, scopeA, created.ID, pkgcatalog.UpdateInput{Status: strPtr(pkgcatalog.StatusActive)})
	if err != nil || restored.Status != pkgcatalog.StatusActive {
		t.Fatalf("restore: err=%v package=%+v", err, restored)
	}

	if _, err := svc.Update(ctx, scopeB, created.ID, pkgcatalog.UpdateInput{Name: strPtr("越权")}); !errors.Is(err, pkgcatalog.ErrNotFound) {
		t.Fatalf("cross-account update: want not found, got %v", err)
	}
	if err := svc.Delete(ctx, scopeB, created.ID); !errors.Is(err, pkgcatalog.ErrNotFound) {
		t.Fatalf("cross-account delete: want not found, got %v", err)
	}
	if err := svc.Delete(ctx, scopeA, created.ID); err != nil {
		t.Fatalf("delete no-reference package: %v", err)
	}
	if err := svc.Delete(ctx, scopeA, created.ID); !errors.Is(err, pkgcatalog.ErrNotFound) {
		t.Fatalf("delete already removed package: want not found, got %v", err)
	}
}

func TestDeletePackageInUseWhenOrdersTableExists(t *testing.T) {
	ctx := context.Background()
	s, _ := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := packageService()
	seedCustomer(t, scope, "cus_order")

	for _, tc := range []struct {
		name          string
		status        string
		wantListCount int
	}{
		{name: "scheduled order", status: "scheduled", wantListCount: 1},
		{name: "cancelled order", status: "cancelled", wantListCount: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := createPackage(t, svc, scope, tc.name)
			seedOrder(t, scope, seedOrderInput{
				ID:         "ord_" + tc.status,
				CustomerID: "cus_order",
				PackageID:  created.ID,
				Status:     tc.status,
			})

			list, err := svc.List(ctx, scope, pkgcatalog.ListFilter{Status: pkgcatalog.StatusAll})
			if err != nil {
				t.Fatalf("list packages: %v", err)
			}
			item := findPackageItem(t, list, created.ID)
			if item.OrdersCount != tc.wantListCount {
				t.Fatalf("list orders_count = %d, want %d", item.OrdersCount, tc.wantListCount)
			}

			if err := svc.Delete(ctx, scope, created.ID); !errors.Is(err, pkgcatalog.ErrPackageInUse) {
				t.Fatalf("delete referenced package: want package_in_use, got %v", err)
			}
		})
	}
}

func findPackageItem(t *testing.T, result pkgcatalog.ListResult, id string) pkgcatalog.ListItem {
	t.Helper()
	for _, item := range result.Items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("package %s not found in %+v", id, result)
	return pkgcatalog.ListItem{}
}

func createPackage(t *testing.T, svc *pkgcatalog.Service, scope store.AccountScope, name string) pkgcatalog.Package {
	t.Helper()
	created, err := svc.Create(context.Background(), scope, pkgcatalog.CreateInput{
		Name:             name,
		ShootType:        pkgcatalog.ShootTypePortrait,
		PricingMode:      pkgcatalog.PricingModePerDuration,
		BasePrice:        68000,
		DurationMinutes:  intPtr(90),
		ShotCountMin:     intPtr(60),
		ShotCountMax:     intPtr(80),
		RawDeliveryCount: intPtr(60),
		RetouchCount:     intPtr(8),
	})
	if err != nil {
		t.Fatalf("create package %s: %v", name, err)
	}
	return created
}

type seedPackageInput struct {
	ID        string
	Name      string
	Status    string
	CreatedAt time.Time
}

func seedPackage(t *testing.T, scope store.AccountScope, input seedPackageInput) {
	t.Helper()
	if err := scope.Insert(context.Background(), "packages",
		[]string{"id", "name", "shoot_type", "pricing_mode", "base_price", "status", "created_at"},
		input.ID, input.Name, pkgcatalog.ShootTypePortrait, pkgcatalog.PricingModeFixed, 10000, input.Status, input.CreatedAt,
	); err != nil {
		t.Fatalf("seed package %s: %v", input.ID, err)
	}
}

func seedCustomer(t *testing.T, scope store.AccountScope, id string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "customers",
		[]string{"id", "display_name", "channel", "status"},
		id, "订单客户", "other", "active",
	); err != nil {
		t.Fatalf("seed customer %s: %v", id, err)
	}
}

type seedOrderInput struct {
	ID         string
	CustomerID string
	PackageID  string
	Status     string
}

func seedOrder(t *testing.T, scope store.AccountScope, input seedOrderInput) {
	t.Helper()
	if err := scope.Insert(context.Background(), "orders",
		[]string{"id", "customer_id", "package_id", "status"},
		input.ID, input.CustomerID, input.PackageID, input.Status,
	); err != nil {
		t.Fatalf("seed order %s: %v", input.ID, err)
	}
}

func intPtr(value int) *int {
	return &value
}

func strPtr(value string) *string {
	return &value
}

func nullInt() nullable.Nullable[int] {
	var v nullable.Nullable[int]
	v.SetNull()
	return v
}

func valueInt(value int) nullable.Nullable[int] {
	var v nullable.Nullable[int]
	v.Set(value)
	return v
}

func derefInt(value *int) int {
	if value == nil {
		return -1
	}
	return *value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

const overPostgresInteger = 1 << 31
