package httpapi_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreativeCanvasSSEAuthorizationAndExternalWrite(t *testing.T) {
	router, database, tokens, databaseURL := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(t.Context(), "sse-other-account", "test-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", "sse-other-account"); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	key := uuid.NewString()
	body, err := json.Marshal(map[string]any{"operation_id": key, "client_created_at": time.Now().UTC(), "payload": map[string]string{"name": "SSE"}})
	if err != nil {
		t.Fatal(err)
	}
	result := shootPlanningRequest(t, router, "POST", "/api/v1/creative/projects", token, key, string(body))
	var project struct {
		Canvas string `json:"default_canvas_id"`
	}
	if result.Code != 201 {
		t.Fatalf("create: %d %s", result.Code, result.Body.String())
	}
	if err := json.Unmarshal(result.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/creative/canvases/" + project.Canvas + "/events"
	for _, scenario := range []struct {
		token  string
		status int
	}{{"", 401}, {issueToken(t, tokens, "sse-other-account"), 404}} {
		response := shootPlanningRequest(t, router, "GET", path, scenario.token, "", "")
		if response.Code != scenario.status {
			t.Fatalf("authorization: %d %s", response.Code, response.Body.String())
		}
	}
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("SSE headers: %d %v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	frame := func() string {
		t.Helper()
		var output strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			output.WriteString(line)
			if line == "\n" {
				return output.String()
			}
		}
	}
	if got := frame(); got != "event: invalidate\ndata: {}\n\n" {
		t.Fatalf("initial: %q", got)
	}
	// This SQL connection is outside the HTTP process's command application path.
	if _, err := db.Exec("UPDATE creative_canvases SET revision=revision+1 WHERE account_id=$1 AND id=$2", testAcctID, project.Canvas); err != nil {
		t.Fatal(err)
	}
	if got := frame(); got != "event: invalidate\ndata: {}\n\n" {
		t.Fatalf("change: %q", got)
	}
	if _, err := db.Exec("UPDATE accounts SET status='pending_verification' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	if got := frame(); got != "event: unavailable\ndata: {}\n\n" {
		t.Fatalf("revocation: %q", got)
	}
}

func TestCreativeCanvasSSEStopsWithStore(t *testing.T) {
	router, database, tokens, databaseURL := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO creative_projects(account_id,id,name) VALUES($1,'sse-p','P');", testAcctID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO creative_canvases(account_id,id,project_id,name) VALUES($1,'sse-c','sse-p','C');", testAcctID); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(router)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/api/v1/creative/canvases/sse-c/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+issueToken(t, tokens, testAcctID))
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if response.ProtoMajor != 2 {
		t.Fatal("expected HTTP/2 stream")
	}
	reader := bufio.NewReader(response.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("idle stream closed before heartbeat: %v", err)
		}
		if line == ": heartbeat\n" {
			break
		}
	}
	database.StopCanvasEvents()
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("stream must finish before shutdown timeout: %v", err)
	}
}
