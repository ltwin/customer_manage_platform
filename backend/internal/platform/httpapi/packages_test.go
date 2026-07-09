package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

func TestPackageAPIRoundtrip(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)

	createBody := []byte(`{
		"name":"轻写真 90 分钟",
		"shoot_type":"portrait",
		"pricing_mode":"per_duration",
		"base_price":68000,
		"duration_minutes":90,
		"shot_count_min":60,
		"shot_count_max":80,
		"raw_delivery_count":60,
		"retouch_count":8,
		"note":"含棚租"
	}`)
	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/packages", token, createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create package: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var created httpapi.Package
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created package: %v", err)
	}
	if created.Id == nil || created.AccountId == nil || *created.AccountId != testAcctID || created.Status != httpapi.PackageStatusActive {
		t.Fatalf("created package shape mismatch: %+v", created)
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list packages: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []httpapi.PackageListItem `json:"items"`
		Total int64                     `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].OrdersCount != 0 {
		t.Fatalf("list zero aggregate shape mismatch: %+v", list)
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+*created.Id, token, []byte(`{
		"base_price":72000,
		"duration_minutes":null
	}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch package: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var patched httpapi.Package
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patched package: %v", err)
	}
	if patched.BasePrice != 72000 || patched.DurationMinutes != nil || patched.ShotCountMin == nil || *patched.ShotCountMin != 60 {
		t.Fatalf("patch should update one field and clear duration only, got %+v", patched)
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+*created.Id, token, []byte(`{"note":""}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear package note: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var noteCleared httpapi.Package
	if err := json.Unmarshal(rec.Body.Bytes(), &noteCleared); err != nil {
		t.Fatalf("decode note-cleared package: %v", err)
	}
	if noteCleared.Note != nil {
		t.Fatalf("empty note should clear existing note to null, got %+v", noteCleared.Note)
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+*created.Id, token, []byte(`{"status":"archived"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive package: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var archived httpapi.Package
	if err := json.Unmarshal(rec.Body.Bytes(), &archived); err != nil || archived.Status != httpapi.PackageStatusArchived {
		t.Fatalf("archive response mismatch: err=%v package=%+v", err, archived)
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list active after archive: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode active list: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("default active list should hide archived package, got %+v", list)
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages?status=archived", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list archived: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode archived list: %v", err)
	}
	if list.Total != 1 || list.Items[0].Status != httpapi.PackageStatusArchived {
		t.Fatalf("archived list mismatch: %+v", list)
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+*created.Id, token, []byte(`{"status":"active"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("restore package: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/packages/"+*created.Id, token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete package: want 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages?status=all", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after delete: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode all list: %v", err)
	}
	if list.Total != 0 {
		t.Fatalf("deleted package should disappear, got %+v", list)
	}
}

func TestPackageAPIErrorPathsAndScope(t *testing.T) {
	h, s, issuer, ctr := newCustomerAPIRouterWithContainer(t)
	tokenA := issueToken(t, issuer, testAcctID)
	ctx := context.Background()
	execSQL := func(statement string) {
		code, output, err := ctr.Exec(ctx, []string{"psql", "-U", "crm_test", "-d", "crm_test", "-c", statement})
		if err != nil || code != 0 {
			t.Fatalf("exec %q: code=%d err=%v output=%v", statement, code, err, output)
		}
	}
	hash, err := auth.HashPassword("second-password")
	if err != nil {
		t.Fatalf("hash second account password: %v", err)
	}
	if err := s.CreateAccount(ctx, "acct-b", hash); err != nil {
		t.Fatalf("create second account: %v", err)
	}
	scopeB := s.ScopeFor(auth.AccountContext{AccountID: "acct-b"})
	otherPackage, err := pkgcatalog.NewService(pkgcatalog.NewPostgresRepository()).Create(ctx, scopeB, pkgcatalog.CreateInput{
		Name:        "B 套系",
		ShootType:   pkgcatalog.ShootTypePortrait,
		PricingMode: pkgcatalog.PricingModeFixed,
		BasePrice:   10000,
	})
	if err != nil {
		t.Fatalf("create B package: %v", err)
	}

	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/packages", tokenA, []byte(`{
		"name":"免费体验",
		"shoot_type":"portrait",
		"pricing_mode":"fixed",
		"base_price":0
	}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("zero price should be accepted, got %d %s", rec.Code, rec.Body.String())
	}
	var created httpapi.Package
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode zero-price package: %v", err)
	}
	if created.Id == nil {
		t.Fatalf("zero-price package should include id: %+v", created)
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/packages", tokenA, []byte(`{
		"name":"缺价格",
		"shoot_type":"portrait",
		"pricing_mode":"fixed"
	}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("missing base_price should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/packages", tokenA, []byte(`{
		"name":"坏数据",
		"shoot_type":"portrait",
		"pricing_mode":"fixed",
		"base_price":-1
	}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("negative price should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/packages", tokenA, []byte(`{
		"name":"超界价格",
		"shoot_type":"portrait",
		"pricing_mode":"fixed",
		"base_price":2147483648
	}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("oversized price should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/packages", tokenA, []byte(`{
		"name":"`+strings.Repeat("x", 2<<20)+`",
		"shoot_type":"portrait",
		"pricing_mode":"fixed",
		"base_price":1
	}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("oversized create body should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+*created.Id, tokenA, []byte(`{"base_price":2147483648}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("oversized patch price should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+*created.Id, tokenA, []byte(`{"note":"`+strings.Repeat("x", 2<<20)+`"}`))
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("oversized patch body should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages?page=0", tokenA, nil)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("invalid page should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages?page=2147483648&page_size=100", tokenA, nil)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("overflow page should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages?page_size=101", tokenA, nil)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("oversized page_size should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages?status=merged", tokenA, nil)
	if rec.Code != http.StatusBadRequest || decodeEnvelope(t, rec).Error.Code != "validation_failed" {
		t.Fatalf("invalid status should be 400 validation_failed, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/packages/"+otherPackage.ID, tokenA, []byte(`{"name":"越权"}`))
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account patch should be 404, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/packages/"+otherPackage.ID, tokenA, nil)
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("cross-account delete should be 404, got %d %s", rec.Code, rec.Body.String())
	}

	execSQL(`CREATE TABLE orders (
		id TEXT PRIMARY KEY,
		account_id TEXT NOT NULL REFERENCES accounts (id),
		package_id TEXT NOT NULL,
		status TEXT NOT NULL,
		FOREIGN KEY (account_id, package_id) REFERENCES packages (account_id, id)
	)`)
	scopeA := s.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	if err := scopeA.Insert(ctx, "orders", []string{"id", "package_id", "status"}, "ord_package_in_use", *created.Id, "scheduled"); err != nil {
		t.Fatalf("seed package reference: %v", err)
	}
	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/packages/"+*created.Id, tokenA, nil)
	if rec.Code != http.StatusConflict || decodeEnvelope(t, rec).Error.Code != "package_in_use" {
		t.Fatalf("referenced package delete should be 409 package_in_use, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/packages", "", nil)
	if rec.Code != http.StatusUnauthorized || decodeEnvelope(t, rec).Error.Code != "unauthorized" {
		t.Fatalf("missing auth should be 401, got %d %s", rec.Code, rec.Body.String())
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/orders", tokenA, []byte(`{}`))
	if rec.Code != http.StatusNotFound || decodeEnvelope(t, rec).Error.Code != "not_found" {
		t.Fatalf("orders endpoint should stay 404, got %d %s", rec.Code, rec.Body.String())
	}
}
