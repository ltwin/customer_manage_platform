package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

func TestDataExportRouteReturnsRealAccountData(t *testing.T) {
	router, database, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	create := authenticatedRequest(t, router, http.MethodPost, "/api/v1/customers", token, []byte(`{
		"display_name":"Fixture Customer",
		"channel":"other",
		"identities":[{"platform":"wechat","handle":"fixture-handle"}]
	}`))
	if create.Code != http.StatusCreated {
		t.Fatalf("create fixture customer: status=%d body=%s", create.Code, create.Body.String())
	}
	var created httpapi.Customer
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil || created.Id == nil {
		t.Fatalf("decode fixture customer: %v body=%s", err, create.Body.String())
	}
	avatarVersion := "sha256-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	objectID := "fedcba9876543210fedcba9876543210"
	scope := database.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	fixtureTime := time.Date(2026, time.July, 20, 8, 0, 0, 0, time.UTC)
	if _, err := scope.Update(context.Background(), "customers", `
		created_at = $2,
		real_name = $3,
		phone = $4,
		birthday = $5,
		avatar_revision = 1,
		avatar_version = $6,
		avatar_object_id = $7,
		avatar_media_type = 'image/jpeg',
		avatar_size = 42,
		avatar_updated_at = $8`, "id = $9",
		fixtureTime, "Fixture Legal Name", "13000000000", "03-15", avatarVersion, objectID, fixtureTime, *created.Id); err != nil {
		t.Fatalf("attach internal avatar pointer fixture: %v", err)
	}
	if _, err := scope.Update(context.Background(), "social_identities", "created_at = $2, remark = $3", "customer_id = $4",
		fixtureTime, "fixture identity remark", *created.Id); err != nil {
		t.Fatalf("complete identity fixture: %v", err)
	}
	var identityID string
	if err := scope.QueryRow(context.Background(), "social_identities", "id", "customer_id = $2", *created.Id).Scan(&identityID); err != nil {
		t.Fatalf("read fixture identity id: %v", err)
	}
	if err := scope.Insert(context.Background(), "customer_notes",
		[]string{"id", "created_at", "customer_id", "content"},
		"note-route", fixtureTime, *created.Id, "fixture route note"); err != nil {
		t.Fatalf("insert customer note fixture: %v", err)
	}
	if err := scope.Insert(context.Background(), "packages",
		[]string{
			"id", "created_at", "name", "shoot_type", "pricing_mode", "base_price", "duration_minutes",
			"shot_count_min", "shot_count_max", "raw_delivery_count", "retouch_count", "note", "status",
		},
		"pkg-route", fixtureTime, "Fixture Route Package", "portrait", "fixed", 12800, 90, 30, 60, 45, 12, "fixture package note", "active"); err != nil {
		t.Fatalf("insert package fixture: %v", err)
	}
	shotAt := fixtureTime.Add(24 * time.Hour)
	deliveredAt := fixtureTime.Add(48 * time.Hour)
	if err := scope.Insert(context.Background(), "orders",
		[]string{
			"id", "created_at", "customer_id", "package_id", "title", "status", "price", "deposit_paid",
			"balance_paid", "shot_at", "delivered_at", "delivery_due_at", "delivery_due_is_override",
			"channel_snapshot", "shoot_type_snapshot", "note",
		},
		"order-route", fixtureTime, *created.Id, "pkg-route", "Fixture Route Order", "delivered", 12800, true, true,
		shotAt, deliveredAt, "2026-07-25", false, "other", "portrait", "fixture order note"); err != nil {
		t.Fatalf("insert order fixture: %v", err)
	}
	if err := scope.Insert(context.Background(), "schedule_slots",
		[]string{"id", "created_at", "start_at", "end_at", "type", "order_id", "note"},
		"slot-route", fixtureTime, shotAt, shotAt.Add(2*time.Hour), "shoot", "order-route", "fixture slot note"); err != nil {
		t.Fatalf("insert schedule slot fixture: %v", err)
	}
	if err := scope.Insert(context.Background(), "reminders",
		[]string{"id", "created_at", "type", "customer_id", "order_id", "due_date", "content", "status", "dedup_key"},
		"reminder-route", fixtureTime, "custom", *created.Id, "order-route", "2026-07-22", "Fixture route reminder", "pending", "dedup-route"); err != nil {
		t.Fatalf("insert reminder fixture: %v", err)
	}
	if err := scope.Insert(context.Background(), "settings",
		[]string{"timezone", "birthday_lead_days", "follow_up_after_days", "churn_thresholds", "digest_hour", "delivery_sla_days", "health_tiers", "telegram_chat_id", "availability", "updated_at"},
		"Asia/Tokyo", 5, 9, []byte(`[{
			"shoot_type":"portrait","days":90
		}]`), 7, 30,
		[]byte(`{"sleeping_ratio":1.5,"at_risk_ratio":2.5,"lost_ratio":4,"fallback_cadence_days":90}`), "fixture-chat",
		[]byte(`{"weekly":{"1":{"start":"08:30","end":"17:30"},"2":null,"3":{"start":"10:00","end":"19:00"},"4":{"start":"10:00","end":"19:00"},"5":{"start":"10:00","end":"19:00"},"6":{"start":"09:00","end":"20:00"},"7":null},"min_opening_minutes":90,"turnaround_minutes":30}`),
		fixtureTime); err != nil {
		t.Fatalf("insert settings fixture: %v", err)
	}
	if err := scope.Insert(context.Background(), "avatar_object_gc",
		[]string{"customer_id", "avatar_version", "avatar_object_id", "not_before", "next_attempt_at", "last_error"},
		*created.Id, avatarVersion, objectID, fixtureTime, fixtureTime, "GC-STATE-SENTINEL"); err != nil {
		t.Fatalf("insert avatar GC fixture: %v", err)
	}

	rec := authenticatedRequest(t, router, http.MethodGet, "/api/v1/export", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /export: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var document httpapi.ExportDocument
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if document.SchemaVersion != 5 || len(document.Customers) != 1 || len(document.SocialIdentities) != 1 ||
		len(document.CustomerNotes) != 1 || len(document.Packages) != 1 || len(document.Orders) != 1 ||
		len(document.ScheduleSlots) != 1 || len(document.Reminders) != 1 {
		t.Fatalf("export did not include real account data: %+v", document)
	}
	if document.AccountProfile.ProfileRevision != "pr-0" || document.AccountProfile.AvatarRevision != "ar-0" {
		t.Fatalf("export account_profile virtual default mismatch: %+v", document.AccountProfile)
	}
	if document.Counts.Customers != len(document.Customers) ||
		document.Counts.SocialIdentities != len(document.SocialIdentities) ||
		document.Counts.CustomerNotes != len(document.CustomerNotes) ||
		document.Counts.Packages != len(document.Packages) ||
		document.Counts.Orders != len(document.Orders) ||
		document.Counts.ScheduleSlots != len(document.ScheduleSlots) ||
		document.Counts.Reminders != len(document.Reminders) {
		t.Fatalf("counts do not match arrays: %+v", document.Counts)
	}
	if document.Customers[0].AvatarRevision == nil || string(*document.Customers[0].AvatarRevision) != "ar-1" ||
		document.Customers[0].AvatarVersion == nil || string(*document.Customers[0].AvatarVersion) != avatarVersion ||
		document.Customers[0].AvatarUrl == nil {
		t.Fatalf("public avatar reference is incomplete: %+v", document.Customers[0])
	}
	body := rec.Body.String()
	for _, sentinel := range []string{objectID, "GC-STATE-SENTINEL"} {
		if strings.Contains(body, sentinel) {
			t.Fatalf("export leaked internal sentinel %q", sentinel)
		}
	}
	var raw any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode structural export scan: %v", err)
	}
	keys := make(map[string]bool)
	collectJSONKeys(raw, keys)
	for _, forbidden := range []string{
		"avatar_object_id", "avatar_media_type", "avatar_size", "avatar_updated_at",
		"object_inventory_cursor", "pointer_customer_id_cursor", "last_error",
		"password_hash", "jwt", "bot_token", "bind_token", "idempotency_key",
		"checkpoint", "delivery", "update_state", "log",
	} {
		if keys[forbidden] {
			t.Fatalf("export leaked forbidden JSON key %q", forbidden)
		}
	}
	assertCompleteRouteExportJSON(t, raw, *created.Id, identityID, avatarVersion, fixtureTime, shotAt, deliveredAt)

	second := authenticatedRequest(t, router, http.MethodGet, "/api/v1/export", token, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("second GET /export: status=%d body=%s", second.Code, second.Body.String())
	}
	var secondDocument httpapi.ExportDocument
	if err := json.Unmarshal(second.Body.Bytes(), &secondDocument); err != nil {
		t.Fatalf("decode second export: %v", err)
	}
	document.ExportedAt = time.Time{}
	secondDocument.ExportedAt = time.Time{}
	if !reflect.DeepEqual(document, secondDocument) {
		t.Fatalf("static data export changed beyond exported_at:\nfirst=%+v\nsecond=%+v", document, secondDocument)
	}
}

func assertCompleteRouteExportJSON(
	t *testing.T,
	raw any,
	customerID string,
	identityID string,
	avatarVersion string,
	fixtureTime time.Time,
	shotAt time.Time,
	deliveredAt time.Time,
) {
	t.Helper()
	document, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("export JSON root type = %T, want object", raw)
	}
	canonicalizeExportJSONTimes(t, document)
	exportedAt, ok := document["exported_at"]
	if !ok {
		t.Fatal("export JSON missing exported_at")
	}
	expected := map[string]any{
		"exported_at":    exportedAt,
		"schema_version": float64(5),
		"counts": map[string]any{
			"customers": float64(1), "social_identities": float64(1), "customer_notes": float64(1),
			"packages": float64(1), "orders": float64(1), "schedule_slots": float64(1), "reminders": float64(1),
		},
		"customers": []any{map[string]any{
			"id": customerID, "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"display_name": "Fixture Customer", "real_name": "Fixture Legal Name", "phone": "13000000000",
			"birthday": "03-15", "channel": "other", "status": "active", "avatar_revision": "ar-1",
			"avatar_version": avatarVersion,
			"avatar_url":     "/api/v1/customers/" + customerID + "/avatar/content?v=" + avatarVersion,
		}},
		"social_identities": []any{map[string]any{
			"id": identityID, "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"customer_id": customerID, "platform": "wechat", "handle": "fixture-handle", "remark": "fixture identity remark",
		}},
		"customer_notes": []any{map[string]any{
			"id": "note-route", "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"customer_id": customerID, "content": "fixture route note",
		}},
		"packages": []any{map[string]any{
			"id": "pkg-route", "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"name": "Fixture Route Package", "shoot_type": "portrait", "pricing_mode": "fixed", "base_price": float64(12800),
			"duration_minutes": float64(90), "shot_count_min": float64(30), "shot_count_max": float64(60),
			"raw_delivery_count": float64(45), "retouch_count": float64(12), "note": "fixture package note", "status": "active",
		}},
		"orders": []any{map[string]any{
			"id": "order-route", "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"customer_id": customerID, "package_id": "pkg-route", "title": "Fixture Route Order", "status": "delivered",
			"price": float64(12800), "deposit_paid": true, "balance_paid": true, "shot_at": shotAt.Format(time.RFC3339),
			"delivered_at": deliveredAt.Format(time.RFC3339), "delivery_due_at": "2026-07-25",
			"delivery_due_is_override": false, "note": "fixture order note",
			// 支付事实（ITEM-2）：直插 fixture 未录金额 → amount_paid 默认 0 恒在；outstanding/paid_at 可缺省。
			"amount_paid": float64(0),
			// 归因快照（ITEM-3）：channel_snapshot 恒输出；shoot_type_snapshot 随套系输出。
			"channel_snapshot": "other", "shoot_type_snapshot": "portrait",
		}},
		"schedule_slots": []any{map[string]any{
			"id": "slot-route", "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"start_at": shotAt.Format(time.RFC3339), "end_at": shotAt.Add(2 * time.Hour).Format(time.RFC3339),
			"type": "shoot", "order_id": "order-route", "note": "fixture slot note",
		}},
		"reminders": []any{map[string]any{
			"id": "reminder-route", "account_id": testAcctID, "created_at": fixtureTime.Format(time.RFC3339),
			"type": "custom", "customer_id": customerID, "order_id": "order-route", "due_date": "2026-07-22",
			"content": "Fixture route reminder", "status": "pending", "dedup_key": "dedup-route",
		}},
		"settings": map[string]any{
			"timezone": "Asia/Tokyo", "birthday_lead_days": float64(5), "follow_up_after_days": float64(9),
			"planning_business_rule_overrides": map[string]any{}, "planning_business_rule_revision": float64(0),
			"churn_thresholds": []any{
				map[string]any{"shoot_type": "portrait", "days": float64(90)},
				map[string]any{"shoot_type": "cosplay", "days": float64(180)},
				map[string]any{"shoot_type": "other", "days": float64(180)},
			},
			"digest_hour": float64(7), "delivery_sla_days": float64(30), "telegram_chat_id": "fixture-chat",
			"health_tiers": map[string]any{"sleeping_ratio": 1.5, "at_risk_ratio": 2.5, "lost_ratio": float64(4), "fallback_cadence_days": float64(90)},
			"availability": map[string]any{
				"weekly": map[string]any{
					"1": map[string]any{"start": "08:30", "end": "17:30"},
					"2": nil,
					"3": map[string]any{"start": "10:00", "end": "19:00"},
					"4": map[string]any{"start": "10:00", "end": "19:00"},
					"5": map[string]any{"start": "10:00", "end": "19:00"},
					"6": map[string]any{"start": "09:00", "end": "20:00"},
					"7": nil,
				},
				"min_opening_minutes": float64(90),
				"turnaround_minutes":  float64(30),
			},
		},
		"account_profile": map[string]any{
			"display_name":     nil,
			"profile_revision": "pr-0",
			"avatar_revision":  "ar-0",
			"avatar":           nil,
			"updated_at":       nil,
		},
	}
	if !reflect.DeepEqual(document, expected) {
		t.Fatalf("route export JSON differs from complete expected document:\n got: %#v\nwant: %#v", document, expected)
	}
}

func canonicalizeExportJSONTimes(t *testing.T, value any) {
	t.Helper()
	timestampKeys := map[string]bool{
		"created_at": true, "shot_at": true, "delivered_at": true,
		"start_at": true, "end_at": true, "exported_at": true,
	}
	var visit func(any)
	visit = func(current any) {
		switch typed := current.(type) {
		case map[string]any:
			for key, child := range typed {
				if timestampKeys[key] {
					rawTimestamp, ok := child.(string)
					if !ok {
						t.Fatalf("export timestamp %s type = %T, want string", key, child)
					}
					parsed, err := time.Parse(time.RFC3339Nano, rawTimestamp)
					if err != nil {
						t.Fatalf("parse export timestamp %s=%q: %v", key, rawTimestamp, err)
					}
					typed[key] = parsed.UTC().Format(time.RFC3339Nano)
					continue
				}
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(value)
}

func collectJSONKeys(value any, keys map[string]bool) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			keys[key] = true
			collectJSONKeys(child, keys)
		}
	case []any:
		for _, child := range typed {
			collectJSONKeys(child, keys)
		}
	}
}
