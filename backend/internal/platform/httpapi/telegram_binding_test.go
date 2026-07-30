package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestCreateTelegramBindTokenRequiresAuthAndReturnsOpaqueDeepLink(t *testing.T) {
	router, _, issuer := newCustomerAPIRouter(t)

	rec := authenticatedRequest(t, router, http.MethodPost, "/api/v1/settings/telegram/bind-token", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated bind-token: want 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	token, err := issuer.Issue(testAcctID)
	if err != nil {
		t.Fatalf("issue auth token: %v", err)
	}
	rec = authenticatedRequest(t, router, http.MethodPost, "/api/v1/settings/telegram/bind-token", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("authenticated bind-token: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Token    string `json:"token"`
		DeepLink string `json:"deep_link"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bind-token: %v", err)
	}
	if len(body.Token) != 43 || body.DeepLink != "https://t.me/studio_digest_bot?start="+body.Token {
		t.Fatalf("bind-token response mismatch: %+v", body)
	}
	if strings.Contains(rec.Body.String(), testAcctID) {
		t.Fatalf("bind-token response leaked account route: %s", rec.Body.String())
	}
}
