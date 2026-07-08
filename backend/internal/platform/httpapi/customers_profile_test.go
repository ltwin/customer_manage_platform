package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
)

// createCustomerAPI 经 HTTP 建一个最简客户并返回契约体。
func createCustomerAPI(t *testing.T, h http.Handler, token, displayName string) httpapi.Customer {
	t.Helper()
	body := []byte(`{"display_name":"` + displayName + `","channel":"other","identities":[{"platform":"wechat","handle":"wx-` + displayName + `"}]}`)
	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s: want 201, got %d body=%s", displayName, rec.Code, rec.Body.String())
	}
	var created httpapi.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	return created
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env httpapi.ErrorEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error envelope: %v (body=%s)", err, body)
	}
	return env.Error.Code
}

// A3/A4/A5 HTTP 面：渐进字段、null 清空、联动、状态矩阵与 409 封套。
func TestCustomerProfileAPIUpdate(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	target := createCustomerAPI(t, h, token, "target")
	referrer := createCustomerAPI(t, h, token, "referrer")

	// A3：字段补全 + 显式 null 清空 phone。
	rec := authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"real_name":"赵芷","phone":"13800000001","birthday":"03-15"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("patch fields: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"phone":null}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("null-clear phone: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var afterClear httpapi.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &afterClear); err != nil {
		t.Fatalf("decode after clear: %v", err)
	}
	if afterClear.Phone != nil || afterClear.RealName == nil || *afterClear.RealName != "赵芷" {
		t.Fatalf("null must clear only phone: %+v", afterClear)
	}

	// A3 错误路径：非法 birthday。
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"birthday":"13-45"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid birthday: want 400, got %d", rec.Code)
	}

	// A4：channel 联动矩阵（缺介绍人 / 自指 / 单独传 / active 成功 / 改走清空）。
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"channel":"referral"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("referral without referrer: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"channel":"referral","referrer_customer_id":"`+*target.Id+`"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("self referrer: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"referrer_customer_id":"`+*referrer.Id+`"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("lone referrer on non-referral: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"channel":"referral","referrer_customer_id":"`+*referrer.Id+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set referral: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"channel":"douyin"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("leave referral: want 200, got %d", rec.Code)
	}
	var left httpapi.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &left); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if left.ReferrerCustomerId != nil {
		t.Fatalf("referrer should be cleared after leaving referral: %+v", left)
	}

	// A4：非 active 介绍人 404（先归档 referrer）。
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*referrer.Id, token,
		[]byte(`{"status":"archived"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive referrer: want 200, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"channel":"referral","referrer_customer_id":"`+*referrer.Id+`"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("archived referrer: want 404, got %d", rec.Code)
	}

	// A5：状态矩阵——merged 值 400；归档 / 恢复 200；merged 客户 409 customer_merged。
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"status":"merged"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status merged via PATCH: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"status":"archived"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: want 200, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*target.Id, token,
		[]byte(`{"status":"active"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("restore: want 200, got %d", rec.Code)
	}

	// merged 客户：先真实 merge 再 PATCH。
	source := createCustomerAPI(t, h, token, "source")
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/merge", token,
		[]byte(`{"source_customer_id":"`+*source.Id+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("merge: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*source.Id, token,
		[]byte(`{"real_name":"改不动"}`))
	if rec.Code != http.StatusConflict || errorCode(t, rec.Body.Bytes()) != "customer_merged" {
		t.Fatalf("patch merged: want 409 customer_merged, got %d %s", rec.Code, rec.Body.String())
	}
}

// A6 HTTP 面：归档客户缺省隐藏、?status=archived / all 可查回。
func TestCustomerProfileAPIArchiveListFilter(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	kept := createCustomerAPI(t, h, token, "kept")
	archived := createCustomerAPI(t, h, token, "tobearchived")
	_ = kept

	rec := authenticatedRequest(t, h, http.MethodPatch, "/api/v1/customers/"+*archived.Id, token,
		[]byte(`{"status":"archived"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: want 200, got %d", rec.Code)
	}

	assertTotal := func(path string, want int64) {
		t.Helper()
		rec := authenticatedRequest(t, h, http.MethodGet, path, token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d", path, rec.Code)
		}
		var list struct {
			Total int64 `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode list: %v", err)
		}
		if list.Total != want {
			t.Fatalf("GET %s: want total %d, got %d", path, want, list.Total)
		}
	}
	assertTotal("/api/v1/customers", 1)
	assertTotal("/api/v1/customers?status=archived", 1)
	assertTotal("/api/v1/customers?status=all", 2)
}

// A7/A8/A9 HTTP 面：身份增删（末位守护 409）与备注（400 边界、详情倒序）。
func TestCustomerProfileAPIIdentitiesAndNotes(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	target := createCustomerAPI(t, h, token, "target")

	// A7：合法 / 非法 platform / 空 handle。
	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/identities", token,
		[]byte(`{"platform":"telegram","handle":"azhi_tg"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add identity: want 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var identity httpapi.SocialIdentity
	if err := json.Unmarshal(rec.Body.Bytes(), &identity); err != nil {
		t.Fatalf("decode identity: %v", err)
	}
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/identities", token,
		[]byte(`{"platform":"bad","handle":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid platform: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/identities", token,
		[]byte(`{"platform":"wechat","handle":"  "}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty handle: want 400, got %d", rec.Code)
	}

	// A8：普通删除 204 / 不存在 404 / 删最后一个 409 last_identity。
	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/customers/"+*target.Id+"/identities/"+*identity.Id, token, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete identity: want 204, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/customers/"+*target.Id+"/identities/sid_missing", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing identity: want 404, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers/"+*target.Id, token, nil)
	var detail httpapi.CustomerDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Identities) != 1 {
		t.Fatalf("expect 1 identity left, got %d", len(detail.Identities))
	}
	rec = authenticatedRequest(t, h, http.MethodDelete, "/api/v1/customers/"+*target.Id+"/identities/"+*detail.Identities[0].Id, token, nil)
	if rec.Code != http.StatusConflict || errorCode(t, rec.Body.Bytes()) != "last_identity" {
		t.Fatalf("delete last identity: want 409 last_identity, got %d %s", rec.Code, rec.Body.String())
	}

	// A9：备注 201 / 空 400 / 详情倒序。
	for _, content := range []string{"第一条", "第二条"} {
		rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/notes", token,
			[]byte(`{"content":"`+content+`"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("add note %s: want 201, got %d body=%s", content, rec.Code, rec.Body.String())
		}
	}
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/notes", token,
		[]byte(`{"content":"  "}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty note: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers/"+*target.Id, token, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail with notes: %v", err)
	}
	if len(detail.Notes) != 2 || detail.Notes[0].Content != "第二条" || detail.Notes[1].Content != "第一条" {
		t.Fatalf("notes should be newest-first: %+v", detail.Notes)
	}
}

// A10/A11 HTTP 面：merge 成功与错误矩阵封套。
func TestCustomerProfileAPIMerge(t *testing.T) {
	h, _, issuer := newCustomerAPIRouter(t)
	token := issueToken(t, issuer, testAcctID)
	target := createCustomerAPI(t, h, token, "target")
	source := createCustomerAPI(t, h, token, "source")

	rec := authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/merge", token,
		[]byte(`{"source_customer_id":"`+*target.Id+`"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("merge self: want 400, got %d", rec.Code)
	}
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/merge", token,
		[]byte(`{"source_customer_id":"cus_missing"}`))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("merge missing source: want 404, got %d", rec.Code)
	}

	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/merge", token,
		[]byte(`{"source_customer_id":"`+*source.Id+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("merge: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var merged httpapi.Customer
	if err := json.Unmarshal(rec.Body.Bytes(), &merged); err != nil {
		t.Fatalf("decode merged target: %v", err)
	}
	if merged.Status != httpapi.CustomerStatusActive {
		t.Fatalf("target should stay active, got %+v", merged)
	}

	// 重放：source 已 merged → 409 merge_conflict。
	rec = authenticatedRequest(t, h, http.MethodPost, "/api/v1/customers/"+*target.Id+"/merge", token,
		[]byte(`{"source_customer_id":"`+*source.Id+`"}`))
	if rec.Code != http.StatusConflict || errorCode(t, rec.Body.Bytes()) != "merge_conflict" {
		t.Fatalf("replay merge: want 409 merge_conflict, got %d %s", rec.Code, rec.Body.String())
	}

	// source 详情：merged + 指针。
	rec = authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers/"+*source.Id, token, nil)
	var sourceDetail httpapi.CustomerDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &sourceDetail); err != nil {
		t.Fatalf("decode source detail: %v", err)
	}
	if sourceDetail.Status != httpapi.CustomerStatusMerged || sourceDetail.MergedIntoCustomerId == nil || *sourceDetail.MergedIntoCustomerId != *target.Id {
		t.Fatalf("source should be merged shell with pointer: %+v", sourceDetail)
	}
}

// A12 HTTP 面：账号 B 对 A 客户的全部写操作 404。
func TestCustomerProfileAPICrossAccountIsolation(t *testing.T) {
	h, s, issuer := newCustomerAPIRouter(t)
	tokenA := issueToken(t, issuer, testAcctID)
	if err := s.CreateAccount(context.Background(), "acct-b", "hash-b"); err != nil {
		t.Fatalf("create account B: %v", err)
	}
	tokenB := issueToken(t, issuer, "acct-b")

	target := createCustomerAPI(t, h, tokenA, "target")
	bCustomer := createCustomerAPI(t, h, tokenB, "b-customer")

	cases := []struct {
		method, path string
		body         []byte
	}{
		{http.MethodPatch, "/api/v1/customers/" + *target.Id, []byte(`{"real_name":"越权"}`)},
		{http.MethodPost, "/api/v1/customers/" + *target.Id + "/identities", []byte(`{"platform":"wechat","handle":"x"}`)},
		{http.MethodDelete, "/api/v1/customers/" + *target.Id + "/identities/sid_x", nil},
		{http.MethodPost, "/api/v1/customers/" + *target.Id + "/notes", []byte(`{"content":"越权备注"}`)},
		{http.MethodPost, "/api/v1/customers/" + *target.Id + "/merge", []byte(`{"source_customer_id":"` + *bCustomer.Id + `"}`)},
		{http.MethodPost, "/api/v1/customers/" + *bCustomer.Id + "/merge", []byte(`{"source_customer_id":"` + *target.Id + `"}`)},
	}
	for _, tc := range cases {
		rec := authenticatedRequest(t, h, tc.method, tc.path, tokenB, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s as B: want 404, got %d body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}

	// A 的客户完好无损。
	rec := authenticatedRequest(t, h, http.MethodGet, "/api/v1/customers/"+*target.Id, tokenA, nil)
	var detail httpapi.CustomerDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.Status != httpapi.CustomerStatusActive || detail.RealName != nil || len(detail.Notes) != 0 || len(detail.Identities) != 1 {
		t.Fatalf("A's customer must be untouched: %+v", detail)
	}
}
