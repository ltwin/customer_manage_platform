package schedule_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oapi-codegen/nullable"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

func TestScheduleCRUDRangeSummaryIsolationAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s, _ := openScheduleStore(t)
	scopeA := createScheduleAccount(t, s, "acct-a")
	scopeB := createScheduleAccount(t, s, "acct-b")
	seedScheduleCustomer(t, scopeA, "cus-a", "客户 A", "active")
	seedScheduleCustomer(t, scopeA, "cus-idempotent", "幂等客户", "active")
	seedScheduleCustomer(t, scopeB, "cus-b", "客户 B", "active")
	seedSchedulePackage(t, scopeA, "pkg-a", "婚礼跟拍")
	seedScheduleOrder(t, scopeA, "ord-a", "cus-a", "pkg-a", "婚礼订单", "consulting")
	seedScheduleOrder(t, scopeA, "ord-b", "cus-idempotent", "", "写真订单", "scheduled")

	now := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	svc := scheduledomain.NewService(
		scheduledomain.NewPostgresRepository(),
		scheduledomain.ClockFunc(func() time.Time { return now }),
	)

	shoot, err := svc.Create(ctx, scopeA, scheduledomain.CreateInput{
		StartAt: now.Add(2 * time.Hour),
		EndAt:   now.Add(4 * time.Hour),
		Type:    scheduledomain.TypeShoot,
		OrderID: strPtr("ord-a"),
	})
	if err != nil {
		t.Fatalf("create shoot: %v", err)
	}
	if shoot.Slot.ID == "" || len(shoot.Overlaps) != 0 {
		t.Fatalf("shoot result: %+v", shoot)
	}

	hold, err := svc.Create(ctx, scopeA, scheduledomain.CreateInput{
		StartAt: now.Add(3 * time.Hour),
		EndAt:   now.Add(5 * time.Hour),
		Type:    scheduledomain.TypeHold,
	})
	if err != nil {
		t.Fatalf("create hold: %v", err)
	}
	if len(hold.Overlaps) != 1 || hold.Overlaps[0] != shoot.Slot.ID {
		t.Fatalf("hold overlap snapshot: %+v", hold.Overlaps)
	}

	busy, err := svc.Create(ctx, scopeA, scheduledomain.CreateInput{
		StartAt: now.Add(5 * time.Hour),
		EndAt:   now.Add(6 * time.Hour),
		Type:    scheduledomain.TypeBusy,
	})
	if err != nil || len(busy.Overlaps) != 0 {
		t.Fatalf("touching busy should not overlap: result=%+v err=%v", busy, err)
	}

	items, err := svc.List(ctx, scopeA, scheduledomain.ListFilter{From: now.Add(time.Hour), To: now.Add(6 * time.Hour)})
	if err != nil {
		t.Fatalf("list slots: %v", err)
	}
	if len(items) != 3 || items[0].ID != shoot.Slot.ID || items[1].ID != hold.Slot.ID || items[2].ID != busy.Slot.ID {
		t.Fatalf("stable slot ordering: %+v", items)
	}
	if items[0].CustomerID != "cus-a" || items[0].CustomerDisplayName != "客户 A" ||
		items[0].CustomerStatus != "active" || items[0].OrderStatus != "consulting" ||
		items[0].OrderTitle == nil || *items[0].OrderTitle != "婚礼订单" ||
		items[0].PackageName == nil || *items[0].PackageName != "婚礼跟拍" {
		t.Fatalf("shoot list summary: %+v", items[0])
	}
	if items[1].CustomerID != "" || items[1].OrderStatus != "" {
		t.Fatalf("non-shoot must not carry reference summary: %+v", items[1])
	}
	crossing, err := svc.List(ctx, scopeA, scheduledomain.ListFilter{From: now.Add(3*time.Hour + 30*time.Minute), To: now.Add(4 * time.Hour)})
	if err != nil || len(crossing) != 2 || crossing[0].ID != shoot.Slot.ID || crossing[1].ID != hold.Slot.ID {
		t.Fatalf("range query must include slots crossing both boundaries: items=%+v err=%v", crossing, err)
	}

	if _, err := svc.Create(ctx, scopeA, scheduledomain.CreateInput{
		StartAt: now.Add(7 * time.Hour),
		EndAt:   now.Add(8 * time.Hour),
		Type:    scheduledomain.TypeShoot,
		OrderID: strPtr("ord-a"),
	}); !errors.Is(err, scheduledomain.ErrOrderAlreadyScheduled) {
		t.Fatalf("duplicate shoot: want order_already_scheduled, got %v", err)
	} else {
		var conflict scheduledomain.OrderAlreadyScheduledError
		if !errors.As(err, &conflict) || conflict.SlotID != shoot.Slot.ID || !conflict.StartAt.Equal(shoot.Slot.StartAt) {
			t.Fatalf("duplicate details: %+v", conflict)
		}
	}

	if _, err := scopeA.Update(ctx, "customers", "status = $2", "id = $3", "archived", "cus-a"); err != nil {
		t.Fatalf("archive customer: %v", err)
	}
	if _, err := scopeA.Update(ctx, "orders", "status = $2", "id = $3", "cancelled", "ord-a"); err != nil {
		t.Fatalf("cancel order: %v", err)
	}
	updated, err := svc.Update(ctx, scopeA, shoot.Slot.ID, scheduledomain.UpdateInput{Note: valueString("保留档期，修正备注")})
	if err != nil || updated.Note == nil || *updated.Note != "保留档期，修正备注" {
		t.Fatalf("note-only update after archive/cancel: slot=%+v err=%v", updated, err)
	}
	changedEnd := now.Add(4*time.Hour + time.Minute)
	if _, err := svc.Update(ctx, scopeA, shoot.Slot.ID, scheduledomain.UpdateInput{EndAt: &changedEnd}); !errors.Is(err, scheduledomain.ErrCustomerArchived) {
		t.Fatalf("time update must revalidate archived customer, got %v", err)
	}

	otherItems, err := svc.List(ctx, scopeB, scheduledomain.ListFilter{From: now, To: now.Add(24 * time.Hour)})
	if err != nil || len(otherItems) != 0 {
		t.Fatalf("cross-account list: items=%+v err=%v", otherItems, err)
	}
	if _, err := svc.Update(ctx, scopeB, shoot.Slot.ID, scheduledomain.UpdateInput{Note: valueString("越权")}); !errors.Is(err, scheduledomain.ErrNotFound) {
		t.Fatalf("cross-account update: want not found, got %v", err)
	}

	prepared, err := svc.PrepareCreate(scheduledomain.CreateInput{
		StartAt: now.Add(9 * time.Hour),
		EndAt:   now.Add(10 * time.Hour),
		Type:    scheduledomain.TypeShoot,
		OrderID: strPtr("ord-b"),
	})
	if err != nil {
		t.Fatalf("prepare idempotent shoot: %v", err)
	}
	expectedCustomerID, err := svc.LookupOrderCustomerID(ctx, scopeA, prepared)
	if err != nil {
		t.Fatalf("lookup expected customer: %v", err)
	}
	executor := idempotency.NewExecutor()
	callback := func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		result, err := svc.CreatePreparedInScope(ctx, tx, prepared, expectedCustomerID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		body, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 201, Body: body}, err
	}
	canonical, _ := json.Marshal(prepared.Input)
	first, err := executor.ExecuteCreate(ctx, scopeA, idempotency.OperationScheduleSlotCreate, "schedule-key", canonical, callback)
	if err != nil {
		t.Fatalf("idempotent create: %v", err)
	}
	second, err := executor.ExecuteCreate(ctx, scopeA, idempotency.OperationScheduleSlotCreate, "schedule-key", canonical, callback)
	if err != nil || string(first.Body) != string(second.Body) {
		t.Fatalf("idempotent replay: first=%s second=%s err=%v", first.Body, second.Body, err)
	}
	if count, err := scopeA.Count(ctx, "schedule_slots", ""); err != nil || count != 4 {
		t.Fatalf("slot count after replay = %d err=%v", count, err)
	}

	if err := svc.Delete(ctx, scopeA, hold.Slot.ID); err != nil {
		t.Fatalf("delete hold: %v", err)
	}
	if err := svc.Delete(ctx, scopeB, busy.Slot.ID); !errors.Is(err, scheduledomain.ErrNotFound) {
		t.Fatalf("cross-account delete: want not found, got %v", err)
	}
}

func TestScheduleConcurrentCreatesKeepOneShootWithTypedConflict(t *testing.T) {
	ctx := context.Background()
	s, _ := openScheduleStore(t)
	scope := createScheduleAccount(t, s, "acct-unique")
	seedScheduleCustomer(t, scope, "cus-unique", "唯一客户", "active")
	seedScheduleOrder(t, scope, "ord-unique", "cus-unique", "", "唯一订单", "scheduled")
	now := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	svc := scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(func() time.Time { return now }))
	input := scheduledomain.CreateInput{
		StartAt: now.Add(time.Hour), EndAt: now.Add(2 * time.Hour), Type: scheduledomain.TypeShoot, OrderID: strPtr("ord-unique"),
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.Create(ctx, scope, input)
			results <- err
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, scheduledomain.ErrOrderAlreadyScheduled):
			var details scheduledomain.OrderAlreadyScheduledError
			if !errors.As(err, &details) || details.SlotID == "" || details.StartAt.IsZero() {
				t.Fatalf("typed conflict details missing: %+v", details)
			}
			conflicts++
		default:
			t.Fatalf("unexpected concurrent create error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	if count, err := scope.Count(ctx, "schedule_slots", ""); err != nil || count != 1 {
		t.Fatalf("unique shoot count=%d err=%v", count, err)
	}
}

func TestScheduleUpdateRetriesAfterCustomerMerge(t *testing.T) {
	ctx := context.Background()
	s, databaseURL := openScheduleStore(t)
	scope := createScheduleAccount(t, s, "acct-update-race")
	seedScheduleCustomer(t, scope, "cus-update-source", "更新源客户", "active")
	seedScheduleCustomer(t, scope, "cus-update-target", "更新目标客户", "active")
	seedScheduleOrder(t, scope, "ord-update", "cus-update-source", "", "更新订单", "scheduled")
	now := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	svc := scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(func() time.Time { return now }))
	created, err := svc.Create(ctx, scope, scheduledomain.CreateInput{
		StartAt: now.Add(time.Hour), EndAt: now.Add(2 * time.Hour), Type: scheduledomain.TypeShoot, OrderID: strPtr("ord-update"),
	})
	if err != nil {
		t.Fatalf("seed shoot: %v", err)
	}

	ready := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-update-source").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "orders", "customer_id = $2", "id = $3", "cus-update-target", "ord-update"); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus-update-target", "cus-update-source"); err != nil {
				return err
			}
			close(ready)
			<-release
			return nil
		})
	}()
	<-ready
	updateDone := make(chan struct {
		slot scheduledomain.Slot
		err  error
	}, 1)
	changedEnd := now.Add(2*time.Hour + 30*time.Minute)
	go func() {
		slot, err := svc.Update(ctx, scope, created.Slot.ID, scheduledomain.UpdateInput{EndAt: &changedEnd})
		updateDone <- struct {
			slot scheduledomain.Slot
			err  error
		}{slot: slot, err: err}
	}()
	waitForScheduleBlockedForUpdate(t, databaseURL, "customers", 1)
	close(release)
	if err := <-txDone; err != nil {
		t.Fatalf("finish update merge transaction: %v", err)
	}
	updated := <-updateDone
	if updated.err != nil || !updated.slot.EndAt.Equal(changedEnd) {
		t.Fatalf("update should retry after merge: slot=%+v err=%v", updated.slot, updated.err)
	}
	items, err := svc.List(ctx, scope, scheduledomain.ListFilter{From: now, To: now.Add(3 * time.Hour)})
	if err != nil || len(items) != 1 || items[0].CustomerID != "cus-update-target" {
		t.Fatalf("updated merge summary: items=%+v err=%v", items, err)
	}
}

func TestScheduleCreateReturnsCustomerChangedAfterTwoMoves(t *testing.T) {
	ctx := context.Background()
	s, databaseURL := openScheduleStore(t)
	scope := createScheduleAccount(t, s, "acct-double-move")
	seedScheduleCustomer(t, scope, "cus-move-source", "移动源", "active")
	seedScheduleCustomer(t, scope, "cus-move-middle", "移动中间", "active")
	seedScheduleCustomer(t, scope, "cus-move-target", "移动目标", "active")
	seedScheduleOrder(t, scope, "ord-double-move", "cus-move-source", "", "连续迁移订单", "scheduled")
	now := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	svc := scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(func() time.Time { return now }))

	firstReady := make(chan struct{})
	firstRelease := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-move-source").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "orders", "customer_id = $2", "id = $3", "cus-move-middle", "ord-double-move"); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus-move-middle", "cus-move-source"); err != nil {
				return err
			}
			close(firstReady)
			<-firstRelease
			return nil
		})
	}()
	<-firstReady

	secondStarted := make(chan struct{})
	secondReady := make(chan struct{})
	secondRelease := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			close(secondStarted)
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-move-middle").Scan(&id); err != nil {
				return err
			}
			close(secondReady)
			<-secondRelease
			if _, err := tx.Update(ctx, "orders", "customer_id = $2", "id = $3", "cus-move-target", "ord-double-move"); err != nil {
				return err
			}
			_, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus-move-target", "cus-move-middle")
			return err
		})
	}()
	<-secondStarted
	waitForScheduleBlockedForUpdate(t, databaseURL, "customers", 1)

	createDone := make(chan error, 1)
	go func() {
		_, err := svc.Create(ctx, scope, scheduledomain.CreateInput{
			StartAt: now.Add(time.Hour), EndAt: now.Add(2 * time.Hour), Type: scheduledomain.TypeShoot, OrderID: strPtr("ord-double-move"),
		})
		createDone <- err
	}()
	waitForScheduleBlockedForUpdate(t, databaseURL, "customers", 2)
	close(firstRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("finish first move: %v", err)
	}
	<-secondReady
	waitForScheduleBlockedForUpdate(t, databaseURL, "customers", 1)
	close(secondRelease)
	if err := <-secondDone; err != nil {
		t.Fatalf("finish second move: %v", err)
	}
	if err := <-createDone; !errors.Is(err, scheduledomain.ErrCustomerChanged) {
		t.Fatalf("two moves: want customer_changed, got %v", err)
	}
	if count, err := scope.Count(ctx, "schedule_slots", ""); err != nil || count != 0 {
		t.Fatalf("double-move must not leave slot: count=%d err=%v", count, err)
	}
}

func TestScheduleCreateRetriesAfterCustomerMergeAndRejectsArchive(t *testing.T) {
	ctx := context.Background()
	s, databaseURL := openScheduleStore(t)
	scope := createScheduleAccount(t, s, "acct-race")
	seedScheduleCustomer(t, scope, "cus-source", "源客户", "active")
	seedScheduleCustomer(t, scope, "cus-target", "目标客户", "active")
	seedScheduleCustomer(t, scope, "cus-archive", "待归档客户", "active")
	seedScheduleOrder(t, scope, "ord-merge", "cus-source", "", "迁移订单", "scheduled")
	seedScheduleOrder(t, scope, "ord-archive", "cus-archive", "", "归档订单", "scheduled")
	now := time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC)
	svc := scheduledomain.NewService(scheduledomain.NewPostgresRepository(), scheduledomain.ClockFunc(func() time.Time { return now }))

	ready := make(chan struct{})
	release := make(chan struct{})
	txDone := make(chan error, 1)
	go func() {
		txDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-source").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "orders", "customer_id = $2", "id = $3", "cus-target", "ord-merge"); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus-target", "cus-source"); err != nil {
				return err
			}
			close(ready)
			<-release
			return nil
		})
	}()
	<-ready
	createDone := make(chan struct {
		result scheduledomain.CreateResult
		err    error
	}, 1)
	go func() {
		result, err := svc.Create(ctx, scope, scheduledomain.CreateInput{
			StartAt: now.Add(time.Hour), EndAt: now.Add(2 * time.Hour), Type: scheduledomain.TypeShoot, OrderID: strPtr("ord-merge"),
		})
		createDone <- struct {
			result scheduledomain.CreateResult
			err    error
		}{result: result, err: err}
	}()
	waitForScheduleBlockedForUpdate(t, databaseURL, "customers", 1)
	close(release)
	if err := <-txDone; err != nil {
		t.Fatalf("finish merge transaction: %v", err)
	}
	created := <-createDone
	if created.err != nil || created.result.Slot.ID == "" {
		t.Fatalf("create should retry on moved customer: result=%+v err=%v", created.result, created.err)
	}
	items, err := svc.List(ctx, scope, scheduledomain.ListFilter{From: now, To: now.Add(3 * time.Hour)})
	if err != nil || len(items) != 1 || items[0].CustomerID != "cus-target" {
		t.Fatalf("merged shoot summary: items=%+v err=%v", items, err)
	}

	archiveReady := make(chan struct{})
	archiveRelease := make(chan struct{})
	archiveDone := make(chan error, 1)
	go func() {
		archiveDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-archive").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "customers", "status = $2", "id = $3", "archived", "cus-archive"); err != nil {
				return err
			}
			close(archiveReady)
			<-archiveRelease
			return nil
		})
	}()
	<-archiveReady
	archiveCreateDone := make(chan error, 1)
	go func() {
		_, err := svc.Create(ctx, scope, scheduledomain.CreateInput{
			StartAt: now.Add(4 * time.Hour), EndAt: now.Add(5 * time.Hour), Type: scheduledomain.TypeShoot, OrderID: strPtr("ord-archive"),
		})
		archiveCreateDone <- err
	}()
	waitForScheduleBlockedForUpdate(t, databaseURL, "customers", 1)
	close(archiveRelease)
	if err := <-archiveDone; err != nil {
		t.Fatalf("finish archive transaction: %v", err)
	}
	if err := <-archiveCreateDone; !errors.Is(err, scheduledomain.ErrCustomerArchived) {
		t.Fatalf("archive-first future shoot: want customer_archived, got %v", err)
	}
}

func openScheduleStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithDatabase("crm_test"),
		tcpostgres.WithUsername("crm_test"),
		tcpostgres.WithPassword("crm_test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(ctr) })
	url, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	s, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(s.Close)
	return s, url
}

func createScheduleAccount(t *testing.T, s *store.Store, id string) store.AccountScope {
	t.Helper()
	if err := s.CreateAccount(context.Background(), id, "test-hash"); err != nil {
		t.Fatalf("create account %s: %v", id, err)
	}
	return s.ScopeFor(auth.AccountContext{AccountID: id})
}

func seedScheduleCustomer(t *testing.T, scope store.AccountScope, id, name, status string) {
	t.Helper()
	if err := scope.Insert(context.Background(), "customers", []string{"id", "display_name", "channel", "status"}, id, name, "other", status); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
}

func seedSchedulePackage(t *testing.T, scope store.AccountScope, id, name string) {
	t.Helper()
	if err := scope.Insert(
		context.Background(),
		"packages",
		[]string{"id", "name", "shoot_type", "pricing_mode", "base_price"},
		id,
		name,
		"portrait",
		"fixed",
		10000,
	); err != nil {
		t.Fatalf("seed package: %v", err)
	}
}

func seedScheduleOrder(t *testing.T, scope store.AccountScope, id, customerID, packageID, title, status string) {
	t.Helper()
	cols := []string{"id", "customer_id", "title", "status"}
	args := []any{id, customerID, title, status}
	if packageID != "" {
		cols = append(cols, "package_id")
		args = append(args, packageID)
	}
	if err := scope.Insert(context.Background(), "orders", cols, args...); err != nil {
		t.Fatalf("seed order: %v", err)
	}
}

func valueString(value string) nullable.Nullable[string] {
	var result nullable.Nullable[string]
	result.Set(value)
	return result
}

func strPtr(value string) *string { return &value }

func waitForScheduleBlockedForUpdate(t *testing.T, databaseURL, table string, minimum int) {
	t.Helper()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open observer db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close observer db: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		err := db.QueryRowContext(ctx, `
	SELECT count(*) FROM pg_stat_activity
	WHERE datname = current_database()
	  AND wait_event_type = 'Lock'
	  AND query LIKE '%' || $1 || '%'
	  AND query LIKE '%FOR UPDATE%'
`, table).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe blocked FOR UPDATE: %v", err)
		}
		if blocked >= minimum {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %d schedule writes to block on %s", minimum, table)
		case <-ticker.C:
		}
	}
}
