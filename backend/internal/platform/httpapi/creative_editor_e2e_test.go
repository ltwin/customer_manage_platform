package httpapi_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/httpapi"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Opt-in browser test; the ordinary Go gate never launches a browser or a dev server.
func TestCreativeEditorBrowser(t *testing.T) {
	frontendURL := os.Getenv("CREATIVE_EDITOR_URL")
	if frontendURL == "" {
		t.Skip("set CREATIVE_EDITOR_URL to the isolated Vite server")
	}
	db, err := store.Open(t.Context(), startCustomerPostgres(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mail := &testMailSender{}
	tokens := auth.NewTokenIssuer("synthetic-creative-browser-secret")
	service := auth.NewService(db, tokens, auth.WithAttemptLimiter(db), auth.WithAuthMailSender(mail), auth.WithRegistrationAdmissionMode(auth.RegistrationPublic), auth.WithPublicBaseURL(frontendURL))
	handler := httpapi.NewRouter(httpapi.RouterDeps{Logger: slog.New(slog.DiscardHandler), DB: db, ScopeFactory: db, Auth: service, PublicBaseURL: frontendURL, PublicRegistrationEnabled: true})
	email := "creative-browser@example.invalid"
	registered := authRequest(t, handler, http.MethodPost, "/api/v1/auth/register", `{"email":"`+email+`","password":"`+testPassword+`"}`, frontendURL, "")
	if registered.Code != 202 {
		t.Fatalf("register %d %s", registered.Code, registered.Body.String())
	}
	action, err := url.Parse(mail.Last().ActionURL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(action.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"token": fragment.Get("token")})
	if err != nil {
		t.Fatal(err)
	}
	verified := authRequest(t, handler, http.MethodPost, "/api/v1/auth/email/verify", string(body), frontendURL, "")
	if verified.Code != 200 {
		t.Fatalf("verify %d %s", verified.Code, verified.Body.String())
	}
	var access httpapi.AccessTokenResponse
	if err := json.Unmarshal(verified.Body.Bytes(), &access); err != nil {
		t.Fatal(err)
	}
	me := shootPlanningRequest(t, handler, "GET", "/api/v1/me", access.AccessToken, "", "")
	var account struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(me.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "frontend/scripts/creative-text-canvas.e2e.mjs")
	cmd := exec.CommandContext(t.Context(), "node", script)
	cmd.Env = append(os.Environ(), "CREATIVE_EDITOR_API="+server.URL, "CREATIVE_EDITOR_EMAIL="+email, "CREATIVE_EDITOR_PASSWORD="+testPassword)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("browser: %v\n%s", err, out)
	}
	t.Log(string(out))
}
