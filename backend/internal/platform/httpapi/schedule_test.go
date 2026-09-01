package httpapi_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	customerdomain "github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestScheduleAPIEndpointsIdempotencyNullableAndIsolation(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	ctx := context.Background()
	tokenA := issueToken(t, issuer, testAcctID)
	scopeA := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	customers := customerdomain.NewService(customerdomain.NewPostgresRepository())
	orders := orderdomain.NewService(orderdomain.NewPostgresRepository())

	customerA, err := customers.Create(ctx, scopeA, customerdomain.CreateInput{
		DisplayName: "档期客户",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformWechat, Handle: "schedule-a"}},
	})
	if err != nil {
		t.Fatalf("create customer A: %v", err)
	}
	scheduledStatus := orderdomain.StatusScheduled
	orderA, err := orders.Create(ctx, scopeA, orderdomain.CreateInput{
		CreationMode: orderdomain.CreationModeNew,
		CustomerID:   customerA.ID,
		Status:       &scheduledStatus,
	})
	if err != nil {
		t.Fatalf("create order A: %v", err)
	}

	if err := s.CreateAccount(ctx, "acct-schedule-b", "test-hash"); err != nil {
		t.Fatalf("create account B: %v", err)
	}
	scopeB := s.ScopeFor(auth.AccountContext{AccountID: "acct-schedule-b"})
	foreignSlotID := "slot-foreign"
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	end := start.Add(2 * time.Hour)
	if err := scopeB.Insert(ctx, "schedule_slots",
		[]string{"id", "start_at", "end_at", "type"},
		foreignSlotID, start, end, "hold",
	); err != nil {
		t.Fatalf("seed foreign slot: %v", err)
	}

	rec := scheduleRequest(t, h, http.MethodGet, "/api/v1/schedule/slots?from="+start.Format(time.RFC3339)+"&to="+end.Format(time.RFC3339), "", nil, "")
	if rec.Code != http.StatusUnauthorized || decodeEnvelope(t, rec).Error.Code != "unauthorized" {
		t.Fatalf("unauthorized schedule list: %d %s", rec.Code, rec.Body.String())
	}

	holdBody := []byte(`{"start_at":"` + start.Format(time.RFC3339) + `","end_at":"` + end.Format(time.RFC3339) + `","type":"hold","note":"待确认"}`)
	first := scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", tokenA, holdBody, "schedule-attempt-key")
	replay := scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", tokenA, holdBody, "schedule-attempt-key")
	if first.Code != http.StatusCreated || replay.Code != http.StatusCreated || first.Body.String() != replay.Body.String() {
		t.Fatalf("idempotent hold create: first=%d %s replay=%d %s", first.Code, first.Body.String(), replay.Code, replay.Body.String())
	}
	changed := scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", tokenA,
		[]byte(`{"start_at":"`+start.Format(time.RFC3339)+`","end_at":"`+end.Add(time.Hour).Format(time.RFC3339)+`","type":"hold"}`),
		"schedule-attempt-key")
	if changed.Code != http.StatusConflict || decodeEnvelope(t, changed).Error.Code != "idempotency_conflict" {
		t.Fatalf("idempotency conflict: %d %s", changed.Code, changed.Body.String())
	}
	var holdResult struct {
		Slot     httpapi.ScheduleSlot `json:"slot"`
		Overlaps []string             `json:"overlaps"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &holdResult); err != nil || holdResult.Slot.Id == nil || len(holdResult.Overlaps) != 0 {
		t.Fatalf("decode hold result: %+v err=%v", holdResult, err)
	}

	shootStart := start.Add(30 * time.Minute)
	shootEnd := end.Add(30 * time.Minute)
	shootBody := []byte(`{"start_at":"` + shootStart.Format(time.RFC3339) + `","end_at":"` + shootEnd.Format(time.RFC3339) + `","type":"shoot","order_id":"` + orderA.ID + `"}`)
	rec = scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", tokenA, shootBody, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create shoot: %d %s", rec.Code, rec.Body.String())
	}
	var shootResult struct {
		Slot     httpapi.ScheduleSlot `json:"slot"`
		Overlaps []string             `json:"overlaps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &shootResult); err != nil || shootResult.Slot.Id == nil || len(shootResult.Overlaps) != 1 || shootResult.Overlaps[0] != *holdResult.Slot.Id {
		t.Fatalf("shoot overlap result: %+v err=%v", shootResult, err)
	}
	rec = scheduleRequest(t, h, http.MethodGet, "/api/v1/schedule/slots/"+*shootResult.Slot.Id, tokenA, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get shoot by id: %d %s", rec.Code, rec.Body.String())
	}
	var shootItem map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &shootItem); err != nil ||
		shootItem["id"] != *shootResult.Slot.Id ||
		shootItem["type"] != "shoot" ||
		shootItem["customer_id"] != customerA.ID ||
		shootItem["order_id"] != orderA.ID {
		t.Fatalf("get shoot summary: %+v err=%v", shootItem, err)
	}

	rec = scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", tokenA,
		[]byte(`{"start_at":"`+end.Add(time.Hour).Format(time.RFC3339)+`","end_at":"`+end.Add(2*time.Hour).Format(time.RFC3339)+`","type":"shoot","order_id":"`+orderA.ID+`"}`), "")
	env := decodeEnvelope(t, rec)
	var details httpapi.ScheduleConflictDetails
	if env.Error.Details != nil {
		details, err = env.Error.Details.AsScheduleConflictDetails()
	}
	if rec.Code != http.StatusConflict || env.Error.Code != "order_already_scheduled" || env.Error.Details == nil || err != nil || details.ScheduleSlotId != *shootResult.Slot.Id {
		t.Fatalf("duplicate shoot details: status=%d env=%+v", rec.Code, env)
	}
	archivedCustomer, err := customers.Create(ctx, scopeA, customerdomain.CreateInput{
		DisplayName: "并发归档客户",
		Channel:     customerdomain.ChannelOther,
		Identities:  []customerdomain.IdentityInput{{Platform: customerdomain.PlatformWechat, Handle: "schedule-archived"}},
	})
	if err != nil {
		t.Fatalf("create archived candidate customer: %v", err)
	}
	archivedOrder, err := orders.Create(ctx, scopeA, orderdomain.CreateInput{
		CreationMode: orderdomain.CreationModeNew,
		CustomerID:   archivedCustomer.ID,
		Status:       &scheduledStatus,
	})
	if err != nil {
		t.Fatalf("create archived candidate order: %v", err)
	}
	archived := customerdomain.StatusArchived
	if _, err := customers.Update(ctx, scopeA, archivedCustomer.ID, customerdomain.UpdateInput{Status: &archived}); err != nil {
		t.Fatalf("archive candidate customer: %v", err)
	}
	rec = scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", tokenA,
		[]byte(`{"start_at":"`+end.Add(3*time.Hour).Format(time.RFC3339)+`","end_at":"`+end.Add(4*time.Hour).Format(time.RFC3339)+`","type":"shoot","order_id":"`+archivedOrder.ID+`"}`), "")
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "customer_archived" {
		t.Fatalf("future shoot for archived customer: %d %s", rec.Code, rec.Body.String())
	}

	rec = scheduleRequest(t, h, http.MethodPatch, "/api/v1/schedule/slots/"+*shootResult.Slot.Id, tokenA, []byte(`{"type":"hold"}`), "")
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("shoot to hold without null order: %d %s", rec.Code, rec.Body.String())
	}
	rec = scheduleRequest(t, h, http.MethodPatch, "/api/v1/schedule/slots/"+*shootResult.Slot.Id, tokenA, []byte(`{"type":"hold","order_id":null,"note":"临时保留"}`), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("shoot to hold with explicit null: %d %s", rec.Code, rec.Body.String())
	}
	var updated httpapi.ScheduleSlot
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil || updated.OrderId != nil || updated.Note == nil {
		t.Fatalf("updated hold shape: %+v err=%v", updated, err)
	}
	rec = scheduleRequest(t, h, http.MethodPatch, "/api/v1/schedule/slots/"+*shootResult.Slot.Id, tokenA, []byte(`{"note":null}`), "")
	updated = httpapi.ScheduleSlot{}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &updated) != nil || updated.Note != nil {
		t.Fatalf("explicit note null: %d %s", rec.Code, rec.Body.String())
	}
	rec = scheduleRequest(t, h, http.MethodPatch, "/api/v1/schedule/slots/"+*shootResult.Slot.Id, tokenA,
		[]byte(`{"type":"shoot","order_id":"`+orderA.ID+`"}`), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("hold back to shoot: %d %s", rec.Code, rec.Body.String())
	}

	rec = scheduleRequest(t, h, http.MethodGet,
		"/api/v1/schedule/slots?from="+start.Add(-time.Hour).Format(time.RFC3339)+"&to="+shootEnd.Add(time.Hour).Format(time.RFC3339), tokenA, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list schedule: %d %s", rec.Code, rec.Body.String())
	}
	var items []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil || len(items) != 2 {
		t.Fatalf("schedule list shape: %s err=%v", rec.Body.String(), err)
	}
	for _, item := range items {
		switch item["type"] {
		case "shoot":
			for _, required := range []string{
				"order_id", "customer_id", "customer_display_name", "customer_status", "order_status",
				"order_deposit_paid", "order_balance_paid",
			} {
				if _, ok := item[required]; !ok {
					t.Fatalf("shoot summary missing %s: %+v", required, item)
				}
			}
			if _, ok := item["order_price"]; ok {
				t.Fatalf("shoot without a price must omit order_price: %+v", item)
			}
			if _, ok := item["package_shoot_type"]; ok {
				t.Fatalf("shoot without a package must omit package_shoot_type: %+v", item)
			}
		case "hold", "busy":
			for _, forbidden := range []string{
				"order_id", "customer_id", "customer_display_name", "customer_status", "order_status",
				"order_price", "order_deposit_paid", "order_balance_paid", "package_shoot_type",
			} {
				if _, ok := item[forbidden]; ok {
					t.Fatalf("non-shoot list item leaked %s: %+v", forbidden, item)
				}
			}
		default:
			t.Fatalf("unexpected list item: %+v", item)
		}
	}

	for _, path := range []string{
		"/api/v1/schedule/slots",
		"/api/v1/schedule/slots?from=" + end.Format(time.RFC3339) + "&to=" + start.Format(time.RFC3339),
	} {
		rec = scheduleRequest(t, h, http.MethodGet, path, tokenA, nil, "")
		if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
			t.Fatalf("invalid range %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}

	rec = scheduleRequest(t, h, http.MethodPatch, "/api/v1/schedule/slots/"+foreignSlotID, tokenA, []byte(`{"note":"越权"}`), "")
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account patch: %d %s", rec.Code, rec.Body.String())
	}
	rec = scheduleRequest(t, h, http.MethodGet, "/api/v1/schedule/slots/"+foreignSlotID, tokenA, nil, "")
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account get: %d %s", rec.Code, rec.Body.String())
	}
	rec = scheduleRequest(t, h, http.MethodDelete, "/api/v1/schedule/slots/"+foreignSlotID, tokenA, nil, "")
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = scheduleRequest(t, h, http.MethodDelete, "/api/v1/schedule/slots/"+*holdResult.Slot.Id, tokenA, nil, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete hold: %d %s", rec.Code, rec.Body.String())
	}
	rec = scheduleRequest(t, h, http.MethodDelete, "/api/v1/schedule/slots/"+*holdResult.Slot.Id, tokenA, nil, "")
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("delete missing hold: %d %s", rec.Code, rec.Body.String())
	}
}

func TestScheduleAPICreateOverlapsExcludeCancelledShoot(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	ctx := context.Background()
	token := issueToken(t, issuer, testAcctID)
	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	if err := scope.Insert(ctx, "customers",
		[]string{"id", "display_name", "channel", "status"},
		"cus-http-overlap", "HTTP 重叠客户", "other", "active",
	); err != nil {
		t.Fatalf("seed overlap customer: %v", err)
	}
	for _, order := range []struct{ id, status string }{
		{id: "ord-http-overlap-active", status: "scheduled"},
		{id: "ord-http-overlap-cancelled", status: "cancelled"},
	} {
		if err := scope.Insert(ctx, "orders",
			[]string{"id", "customer_id", "title", "status"},
			order.id, "cus-http-overlap", order.id, order.status,
		); err != nil {
			t.Fatalf("seed %s: %v", order.id, err)
		}
	}

	start := time.Date(2026, 8, 2, 2, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	for _, fixture := range []struct {
		id, slotType string
		orderID      any
	}{
		{id: "slot-http-active-shoot", slotType: "shoot", orderID: "ord-http-overlap-active"},
		{id: "slot-http-cancelled-shoot", slotType: "shoot", orderID: "ord-http-overlap-cancelled"},
		{id: "slot-http-hold", slotType: "hold", orderID: nil},
		{id: "slot-http-busy", slotType: "busy", orderID: nil},
	} {
		if err := scope.Insert(ctx, "schedule_slots",
			[]string{"id", "start_at", "end_at", "type", "order_id"},
			fixture.id, start, end, fixture.slotType, fixture.orderID,
		); err != nil {
			t.Fatalf("seed %s: %v", fixture.id, err)
		}
	}

	body := []byte(`{"start_at":"` + start.Add(30*time.Minute).Format(time.RFC3339) + `","end_at":"` + end.Add(-30*time.Minute).Format(time.RFC3339) + `","type":"hold"}`)
	rec := scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", token, body, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create overlapping hold: %d %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Overlaps []string `json:"overlaps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := []string{"slot-http-active-shoot", "slot-http-busy", "slot-http-hold"}
	if !slices.Equal(response.Overlaps, want) {
		t.Fatalf("overlaps=%v want=%v", response.Overlaps, want)
	}
}

func TestScheduleAPIReturnsCustomerChangedAfterTwoMoves(t *testing.T) {
	h, s, issuer, databaseURL := newCustomerAPIRouterWithURL(t)
	ctx := context.Background()
	token := issueToken(t, issuer, testAcctID)
	scope := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	for _, customer := range []struct{ id, name string }{
		{id: "cus-http-source", name: "HTTP 源客户"},
		{id: "cus-http-middle", name: "HTTP 中间客户"},
		{id: "cus-http-target", name: "HTTP 目标客户"},
	} {
		if err := scope.Insert(ctx, "customers", []string{"id", "display_name", "channel", "status"}, customer.id, customer.name, "other", "active"); err != nil {
			t.Fatalf("seed %s: %v", customer.id, err)
		}
	}
	if err := scope.Insert(ctx, "orders", []string{"id", "customer_id", "status"}, "ord-http-move", "cus-http-source", "scheduled"); err != nil {
		t.Fatalf("seed moving order: %v", err)
	}
	firstReady := make(chan struct{})
	firstRelease := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- scope.WithinTx(ctx, func(tx store.AccountScope) error {
			var id string
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-http-source").Scan(&id); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "orders", "customer_id = $2", "id = $3", "cus-http-middle", "ord-http-move"); err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus-http-middle", "cus-http-source"); err != nil {
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
			if err := tx.QueryRowForUpdate(ctx, "customers", "id", "id = $2", "cus-http-middle").Scan(&id); err != nil {
				return err
			}
			close(secondReady)
			<-secondRelease
			if _, err := tx.Update(ctx, "orders", "customer_id = $2", "id = $3", "cus-http-target", "ord-http-move"); err != nil {
				return err
			}
			_, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", "merged", "cus-http-target", "cus-http-middle")
			return err
		})
	}()
	<-secondStarted
	waitForScheduleHTTPBlockedForUpdate(t, databaseURL, "customers", 1)

	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	requestDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		requestDone <- scheduleRequest(t, h, http.MethodPost, "/api/v1/schedule/slots", token,
			[]byte(`{"start_at":"`+start.Format(time.RFC3339)+`","end_at":"`+start.Add(time.Hour).Format(time.RFC3339)+`","type":"shoot","order_id":"ord-http-move"}`),
			"schedule-double-move-key")
	}()
	waitForScheduleHTTPBlockedForUpdate(t, databaseURL, "customers", 2)
	close(firstRelease)
	if err := <-firstDone; err != nil {
		t.Fatalf("finish first customer move: %v", err)
	}
	<-secondReady
	waitForScheduleHTTPBlockedForUpdate(t, databaseURL, "customers", 1)
	close(secondRelease)
	if err := <-secondDone; err != nil {
		t.Fatalf("finish second customer move: %v", err)
	}
	rec := <-requestDone
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "customer_changed" {
		t.Fatalf("double move response: %d %s", rec.Code, rec.Body.String())
	}
	if count, err := scope.Count(ctx, "schedule_slots", "order_id = $2", "ord-http-move"); err != nil || count != 0 {
		t.Fatalf("double move must leave no slot: count=%d err=%v", count, err)
	}
	if count, err := scope.Count(ctx, "idempotency_records", "operation = $2 AND key = $3", "schedule-slot.create.v1", "schedule-double-move-key"); err != nil || count != 0 {
		t.Fatalf("double move must leave no idempotency claim: count=%d err=%v", count, err)
	}
}

func scheduleRequest(
	t *testing.T,
	h http.Handler,
	method, path, token string,
	body []byte,
	idempotencyKey string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func waitForScheduleHTTPBlockedForUpdate(t *testing.T, databaseURL, table string, minimum int) {
	t.Helper()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open schedule HTTP observer: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close schedule HTTP observer: %v", err)
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
			t.Fatalf("observe schedule HTTP lock wait: %v", err)
		}
		if blocked >= minimum {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %d schedule HTTP writes to block on %s", minimum, table)
		case <-ticker.C:
		}
	}
}
