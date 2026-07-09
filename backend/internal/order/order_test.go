package order_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
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
	s, _ := openStoreWithURL(t)
	return s
}

func openStoreWithURL(t *testing.T) (*store.Store, string) {
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
	return s, url
}

func createAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func orderService() *orderdomain.Service {
	return orderdomain.NewService(orderdomain.NewPostgresRepository())
}

func packageService() *pkgcatalog.Service {
	return pkgcatalog.NewService(pkgcatalog.NewPostgresRepository())
}

func TestCreateBackfillReferencesAndValidation(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := orderService()

	seedCustomer(t, scopeA, "cus_active", "小美", "active")
	seedCustomer(t, scopeA, "cus_archived", "旧客", "archived")
	seedCustomer(t, scopeA, "cus_merged", "已合并", "merged")
	seedCustomer(t, scopeB, "cus_b", "B 客户", "active")
	seedPackage(t, scopeA, "pkg_active", "胶片写真", "active")
	seedPackage(t, scopeA, "pkg_archived", "下架套系", "archived")
	seedPackage(t, scopeB, "pkg_b", "B 套系", "active")

	created, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{
		CustomerID: "cus_active",
		PackageID:  strPtr("pkg_active"),
		Title:      strPtr("  婚纱拍摄  "),
		Price:      intPtr(68000),
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if created.ID == "" || created.AccountID != "acct-a" || created.Status != orderdomain.StatusConsulting ||
		created.DepositPaid || created.BalancePaid || created.ShotAt != nil || created.DeliveredAt != nil {
		t.Fatalf("created order defaults mismatch: %+v", created)
	}
	if created.Title == nil || *created.Title != "婚纱拍摄" {
		t.Fatalf("title should be trimmed, got %+v", created.Title)
	}

	for _, customerID := range []string{"cus_archived", "cus_merged"} {
		_, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{CustomerID: customerID})
		if !errors.Is(err, orderdomain.ErrCustomerArchived) {
			t.Fatalf("customer %s: want customer_archived, got %v", customerID, err)
		}
	}
	for name, input := range map[string]orderdomain.CreateInput{
		"missing customer":       {CustomerID: "cus_missing"},
		"cross-account customer": {CustomerID: "cus_b"},
		"missing package":        {CustomerID: "cus_active", PackageID: strPtr("pkg_missing")},
		"cross-account package":  {CustomerID: "cus_active", PackageID: strPtr("pkg_b")},
	} {
		if _, err := svc.Create(ctx, scopeA, input); !errors.Is(err, orderdomain.ErrNotFound) {
			t.Fatalf("%s: want not_found, got %v", name, err)
		}
	}
	if _, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{CustomerID: "cus_active", PackageID: strPtr("pkg_archived")}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("regular create with archived package: want validation, got %v", err)
	}
	if _, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{CustomerID: "cus_active", Price: intPtr(-1)}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("negative price: want validation, got %v", err)
	}

	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	deliveredAt := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	status := orderdomain.StatusDelivered
	paid := true
	backfilled, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{
		CustomerID:  "cus_active",
		PackageID:   strPtr("pkg_archived"),
		Status:      &status,
		ShotAt:      &shotAt,
		DeliveredAt: &deliveredAt,
		DepositPaid: &paid,
		BalancePaid: &paid,
	})
	if err != nil {
		t.Fatalf("backfill delivered with archived package: %v", err)
	}
	if backfilled.Status != orderdomain.StatusDelivered || backfilled.PackageID == nil || *backfilled.PackageID != "pkg_archived" {
		t.Fatalf("backfilled order mismatch: %+v", backfilled)
	}

	status = orderdomain.StatusShot
	if _, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{CustomerID: "cus_active", Status: &status}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("backfill shot without shot_at: want validation, got %v", err)
	}
	status = orderdomain.StatusClosed
	if _, err := svc.Create(ctx, scopeA, orderdomain.CreateInput{CustomerID: "cus_active", Status: &status, ShotAt: &shotAt, DeliveredAt: &deliveredAt}); !errors.Is(err, orderdomain.ErrUnpaidBalance) {
		t.Fatalf("backfill closed unpaid: want unpaid_balance, got %v", err)
	}
}

func TestCreateSerializesWithConcurrentCustomerMerge(t *testing.T) {
	ctx := context.Background()
	s, databaseURL := openStoreWithURL(t)
	scope := createAccount(t, s, "acct-a")
	svc := orderService()

	seedCustomer(t, scope, "cus_target", "目标客户", "active")
	seedCustomer(t, scope, "cus_source", "源客户", "active")

	ready := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus_source").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus_target", "cus_source"); err != nil {
				return err
			}
			close(ready)
			<-release
			return nil
		})
	}()
	<-ready
	released := false
	defer func() {
		if !released {
			close(release)
			<-txDone
		}
	}()

	createDone := make(chan error, 1)
	go func() {
		_, err := svc.Create(ctx, scope, orderdomain.CreateInput{CustomerID: "cus_source"})
		createDone <- err
	}()
	waitForBlockedForUpdate(t, databaseURL, "customers")

	close(release)
	released = true
	if err := <-txDone; err != nil {
		t.Fatalf("finish merge transaction: %v", err)
	}
	if err := <-createDone; !errors.Is(err, orderdomain.ErrCustomerArchived) {
		t.Fatalf("racing create after merge commit: want customer_archived, got %v", err)
	}
	if _, err := svc.Create(ctx, scope, orderdomain.CreateInput{CustomerID: "cus_source"}); !errors.Is(err, orderdomain.ErrCustomerArchived) {
		t.Fatalf("create after merge: want customer_archived, got %v", err)
	}
	list, err := svc.List(ctx, scope, orderdomain.ListFilter{CustomerID: "cus_source"})
	if err != nil {
		t.Fatalf("list source orders: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("source customer should not receive racing order, got %+v", list)
	}
}

func TestCreateSerializesWithConcurrentPackageArchive(t *testing.T) {
	ctx := context.Background()
	s, databaseURL := openStoreWithURL(t)
	scope := createAccount(t, s, "acct-a")
	svc := orderService()

	seedCustomer(t, scope, "cus_active", "小美", "active")
	seedPackage(t, scope, "pkg_race", "将下架套系", "active")

	ready := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "packages", "id", "id = $2", "pkg_race").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "packages", "status = $2", "id = $3", "archived", "pkg_race"); err != nil {
				return err
			}
			close(ready)
			<-release
			return nil
		})
	}()
	<-ready
	released := false
	defer func() {
		if !released {
			close(release)
			<-txDone
		}
	}()

	createDone := make(chan error, 1)
	go func() {
		_, err := svc.Create(ctx, scope, orderdomain.CreateInput{CustomerID: "cus_active", PackageID: strPtr("pkg_race")})
		createDone <- err
	}()
	waitForBlockedForUpdate(t, databaseURL, "packages")

	close(release)
	released = true
	if err := <-txDone; err != nil {
		t.Fatalf("finish package archive transaction: %v", err)
	}
	if err := <-createDone; !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("racing create after package archive commit: want validation, got %v", err)
	}
	if _, err := svc.Create(ctx, scope, orderdomain.CreateInput{CustomerID: "cus_active", PackageID: strPtr("pkg_race")}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("create after package archive: want validation, got %v", err)
	}
}

func TestCreateSerializesWithConcurrentPackageDelete(t *testing.T) {
	ctx := context.Background()
	s, databaseURL := openStoreWithURL(t)
	scope := createAccount(t, s, "acct-a")
	svc := orderService()

	seedCustomer(t, scope, "cus_active", "小美", "active")
	seedPackage(t, scope, "pkg_delete", "待删套系", "active")

	ready := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "packages", "id", "id = $2", "pkg_delete").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Delete(ctx, "packages", "id = $2", "pkg_delete"); err != nil {
				return err
			}
			close(ready)
			<-release
			return nil
		})
	}()
	<-ready
	released := false
	defer func() {
		if !released {
			close(release)
			<-txDone
		}
	}()

	createDone := make(chan error, 1)
	go func() {
		_, err := svc.Create(ctx, scope, orderdomain.CreateInput{CustomerID: "cus_active", PackageID: strPtr("pkg_delete")})
		createDone <- err
	}()
	waitForBlockedForUpdate(t, databaseURL, "packages")

	close(release)
	released = true
	if err := <-txDone; err != nil {
		t.Fatalf("finish package delete transaction: %v", err)
	}
	if err := <-createDone; !errors.Is(err, orderdomain.ErrNotFound) {
		t.Fatalf("racing create after package delete commit: want not_found, got %v", err)
	}
	list, err := svc.List(ctx, scope, orderdomain.ListFilter{PageSize: 100})
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("deleted package should not receive racing order, got %+v", list)
	}
}

func TestPackageDeleteSeesConcurrentOrderCreate(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := orderService()
	pkgSvc := packageService()

	seedCustomer(t, scope, "cus_active", "小美", "active")
	seedPackage(t, scope, "pkg_used", "已用套系", "active")

	created := createOrder(t, svc, scope, orderdomain.CreateInput{CustomerID: "cus_active", PackageID: strPtr("pkg_used")})
	if created.PackageID == nil || *created.PackageID != "pkg_used" {
		t.Fatalf("created package link mismatch: %+v", created)
	}
	if err := pkgSvc.Delete(ctx, scope, "pkg_used"); !errors.Is(err, pkgcatalog.ErrPackageInUse) {
		t.Fatalf("delete package after order create: want package_in_use, got %v", err)
	}
}

func TestUpdateQueryAndDelete(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scopeA := createAccount(t, s, "acct-a")
	scopeB := createAccount(t, s, "acct-b")
	svc := orderService()

	seedCustomer(t, scopeA, "cus_active", "小美", "active")
	seedCustomer(t, scopeB, "cus_b", "B 客户", "active")
	seedPackage(t, scopeA, "pkg_active", "胶片写真", "active")
	created := createOrder(t, svc, scopeA, orderdomain.CreateInput{CustomerID: "cus_active", PackageID: strPtr("pkg_active"), Price: intPtr(68000)})

	status := orderdomain.StatusScheduled
	updated, err := svc.Update(ctx, scopeA, created.ID, orderdomain.UpdateInput{Status: &status})
	if err != nil || updated.Status != orderdomain.StatusScheduled {
		t.Fatalf("scheduled update: err=%v order=%+v", err, updated)
	}
	status = orderdomain.StatusShot
	updated, err = svc.Update(ctx, scopeA, created.ID, orderdomain.UpdateInput{Status: &status})
	if err != nil || updated.Status != orderdomain.StatusShot || updated.ShotAt == nil {
		t.Fatalf("shot update should write shot_at: err=%v order=%+v", err, updated)
	}
	status = orderdomain.StatusDelivered
	updated, err = svc.Update(ctx, scopeA, created.ID, orderdomain.UpdateInput{Status: &status})
	if err != nil || updated.Status != orderdomain.StatusDelivered || updated.DeliveredAt == nil {
		t.Fatalf("front-jump delivered should write delivered_at: err=%v order=%+v", err, updated)
	}

	unpaid, err := svc.List(ctx, scopeA, orderdomain.ListFilter{UnpaidBalance: true})
	if err != nil {
		t.Fatalf("list unpaid: %v", err)
	}
	if len(unpaid.Items) != 1 || unpaid.Items[0].ID != created.ID || unpaid.Items[0].CustomerDisplayName != "小美" || unpaid.Items[0].PackageName == nil || *unpaid.Items[0].PackageName != "胶片写真" {
		t.Fatalf("unpaid list should include delivered unpaid order with summaries, got %+v", unpaid)
	}

	status = orderdomain.StatusClosed
	note := "should rollback"
	if _, err := svc.Update(ctx, scopeA, created.ID, orderdomain.UpdateInput{
		Status: &status,
		Note:   &note,
		Price:  nullable.NewNullableWithValue(99000),
	}); !errors.Is(err, orderdomain.ErrUnpaidBalance) {
		t.Fatalf("close unpaid: want unpaid_balance, got %v", err)
	}
	afterFailed := mustFindOrder(t, svc, scopeA, created.ID)
	if afterFailed.Note != nil || afterFailed.Price == nil || *afterFailed.Price != 68000 {
		t.Fatalf("failed mixed patch should not persist fields, got %+v", afterFailed)
	}

	paid := true
	updated, err = svc.Update(ctx, scopeA, created.ID, orderdomain.UpdateInput{Status: &status, BalancePaid: &paid})
	if err != nil || updated.Status != orderdomain.StatusClosed || !updated.BalancePaid {
		t.Fatalf("paid close: err=%v order=%+v", err, updated)
	}
	if err := svc.Delete(ctx, scopeA, created.ID); err != nil {
		t.Fatalf("delete closed order: %v", err)
	}
	if _, err := svc.List(ctx, scopeA, orderdomain.ListFilter{CustomerID: "cus_active"}); err != nil {
		t.Fatalf("list after delete: %v", err)
	}

	nonTerminal := createOrder(t, svc, scopeA, orderdomain.CreateInput{CustomerID: "cus_active"})
	if err := svc.Delete(ctx, scopeA, nonTerminal.ID); !errors.Is(err, orderdomain.ErrOrderNotTerminal) {
		t.Fatalf("delete non-terminal: want order_not_terminal, got %v", err)
	}
	if _, err := svc.Update(ctx, scopeB, nonTerminal.ID, orderdomain.UpdateInput{Status: &status}); !errors.Is(err, orderdomain.ErrNotFound) {
		t.Fatalf("cross-account update: want not_found, got %v", err)
	}
}

func TestFieldCorrectionInvariants(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := orderService()
	seedCustomer(t, scope, "cus_active", "小美", "active")

	order := createOrder(t, svc, scope, orderdomain.CreateInput{CustomerID: "cus_active"})
	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{ShotAt: nullable.NewNullableWithValue(shotAt)}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("prewrite shot_at: want validation, got %v", err)
	}

	status := orderdomain.StatusScheduled
	order, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{Status: &status})
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	status = orderdomain.StatusShot
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{
		Status: &status,
		ShotAt: nullable.NewNullNullable[time.Time](),
	}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("scheduled->shot with shot_at null: want validation, got %v", err)
	}
	status = orderdomain.StatusCancelled
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{
		Status: &status,
		ShotAt: nullable.NewNullableWithValue(shotAt),
	}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("scheduled->cancelled with shot_at prewrite: want validation, got %v", err)
	}
	status = orderdomain.StatusShot
	order, err = svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{Status: &status, ShotAt: nullable.NewNullableWithValue(shotAt)})
	if err != nil {
		t.Fatalf("shot: %v", err)
	}
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{ShotAt: nullable.NewNullNullable[time.Time]()}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("clear shot_at: want validation, got %v", err)
	}
	status = orderdomain.StatusDelivered
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{
		Status:      &status,
		DeliveredAt: nullable.NewNullNullable[time.Time](),
	}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("shot->delivered with delivered_at null: want validation, got %v", err)
	}

	status = orderdomain.StatusCancelled
	order, err = svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{Status: &status})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if order.ShotAt == nil || !order.ShotAt.Equal(shotAt) {
		t.Fatalf("cancel should preserve reached shot_at, got %+v", order)
	}
	paid := true
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{DepositPaid: &paid}); !errors.Is(err, orderdomain.ErrValidation) {
		t.Fatalf("terminal payment correction: want validation, got %v", err)
	}
	note := "坏账原因"
	if _, err := svc.Update(ctx, scope, order.ID, orderdomain.UpdateInput{Note: &note}); err != nil {
		t.Fatalf("terminal note correction should pass: %v", err)
	}
}

func TestListFiltersSummariesAndStableSorting(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	scope := createAccount(t, s, "acct-a")
	svc := orderService()
	seedCustomer(t, scope, "cus_one", "小美", "active")
	seedCustomer(t, scope, "cus_two", "阿泽", "active")
	seedPackage(t, scope, "pkg_one", "胶片写真", "active")

	sameMoment := time.Date(2026, 7, 9, 8, 0, 0, 0, time.UTC)
	shotAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	deliveredAt := time.Date(2026, 6, 9, 8, 0, 0, 0, time.UTC)
	seedOrder(t, scope, seedOrderInput{ID: "ord_a", CustomerID: "cus_one", Status: orderdomain.StatusScheduled, CreatedAt: sameMoment})
	seedOrder(t, scope, seedOrderInput{ID: "ord_b", CustomerID: "cus_one", Status: orderdomain.StatusShot, BalancePaid: false, ShotAt: &shotAt, CreatedAt: sameMoment})
	seedOrder(t, scope, seedOrderInput{ID: "ord_c", CustomerID: "cus_two", PackageID: strPtr("pkg_one"), Status: orderdomain.StatusDelivered, BalancePaid: true, ShotAt: &shotAt, DeliveredAt: &deliveredAt, CreatedAt: sameMoment})

	firstPage, err := svc.List(ctx, scope, orderdomain.ListFilter{Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if firstPage.Total != 3 || len(firstPage.Items) != 2 || firstPage.Items[0].ID != "ord_c" || firstPage.Items[1].ID != "ord_b" {
		t.Fatalf("stable sort page mismatch: %+v", firstPage)
	}
	if firstPage.Items[0].CustomerDisplayName != "阿泽" || firstPage.Items[0].PackageName == nil || *firstPage.Items[0].PackageName != "胶片写真" {
		t.Fatalf("summary mismatch: %+v", firstPage.Items[0])
	}

	unpaid, err := svc.List(ctx, scope, orderdomain.ListFilter{UnpaidBalance: true})
	if err != nil {
		t.Fatalf("list unpaid: %v", err)
	}
	if unpaid.Total != 1 || len(unpaid.Items) != 1 || unpaid.Items[0].ID != "ord_b" {
		t.Fatalf("unpaid filter should only return shot..delivered unpaid, got %+v", unpaid)
	}
	byCustomer, err := svc.List(ctx, scope, orderdomain.ListFilter{CustomerID: "cus_one", Status: orderdomain.StatusShot})
	if err != nil || byCustomer.Total != 1 || byCustomer.Items[0].ID != "ord_b" {
		t.Fatalf("customer/status filter mismatch: err=%v list=%+v", err, byCustomer)
	}
}

func createOrder(t *testing.T, svc *orderdomain.Service, scope store.AccountScope, input orderdomain.CreateInput) orderdomain.Order {
	t.Helper()
	created, err := svc.Create(context.Background(), scope, input)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	return created
}

func mustFindOrder(t *testing.T, svc *orderdomain.Service, scope store.AccountScope, id string) orderdomain.Order {
	t.Helper()
	list, err := svc.List(context.Background(), scope, orderdomain.ListFilter{PageSize: 100})
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	for _, item := range list.Items {
		if item.ID == id {
			return item.Order
		}
	}
	t.Fatalf("order %s not found in %+v", id, list)
	return orderdomain.Order{}
}

func seedCustomer(t *testing.T, scope store.AccountScope, id, displayName, status string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "customers",
		[]string{"id", "display_name", "channel", "status"},
		id, displayName, "other", status,
	); err != nil {
		t.Fatalf("seed customer %s: %v", id, err)
	}
}

func seedPackage(t *testing.T, scope store.AccountScope, id, name, status string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "packages",
		[]string{"id", "name", "shoot_type", "pricing_mode", "base_price", "status"},
		id, name, "portrait", "fixed", 68000, status,
	); err != nil {
		t.Fatalf("seed package %s: %v", id, err)
	}
}

type seedOrderInput struct {
	ID          string
	CustomerID  string
	PackageID   *string
	Status      string
	Price       *int
	BalancePaid bool
	ShotAt      *time.Time
	DeliveredAt *time.Time
	CreatedAt   time.Time
}

func seedOrder(t *testing.T, scope store.AccountScope, input seedOrderInput) {
	t.Helper()
	if err := scope.Insert(context.Background(), "orders",
		[]string{"id", "customer_id", "package_id", "status", "price", "balance_paid", "shot_at", "delivered_at", "created_at"},
		input.ID, input.CustomerID, nullableString(input.PackageID), input.Status, nullableInt(input.Price), input.BalancePaid, nullableTime(input.ShotAt), nullableTime(input.DeliveredAt), input.CreatedAt,
	); err != nil {
		t.Fatalf("seed order %s: %v", input.ID, err)
	}
}

func strPtr(value string) *string {
	return &value
}

func intPtr(value int) *int {
	return &value
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func waitForBlockedForUpdate(t *testing.T, databaseURL, table string) {
	t.Helper()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open observer db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("close observer db: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := db.QueryRowContext(ctx, `
SELECT EXISTS (
	SELECT 1
	FROM pg_stat_activity
	WHERE datname = current_database()
	  AND wait_event_type = 'Lock'
	  AND query LIKE '%' || $1 || '%'
	  AND query LIKE '%FOR UPDATE%'
)`, table).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe blocked FOR UPDATE: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for order create to block on %s FOR UPDATE", table)
		case <-ticker.C:
		}
	}
}
