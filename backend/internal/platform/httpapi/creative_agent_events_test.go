package httpapi_test

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCreativeAgentRunSSEFollowsTheLog(t *testing.T) {
	router, database, tokens, databaseURL := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	if err := database.CreateAccount(t.Context(), "sse-run-other", "test-hash"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE accounts SET status='active' WHERE id=$1", "sse-run-other"); err != nil {
		t.Fatal(err)
	}
	runID := "ccrn_" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO creative_agent_runs(account_id,id,conversation_id,canvas_id,trigger_message_id,egress_consent_id,
		model_key,model_snapshot,state,claim_token,limits_version,limits_snapshot,deadline_at)
		VALUES($1,$2,'c','v','m','e','k','{}','running','t','v1','{}',now()+interval '1 hour')`, testAcctID, runID); err != nil {
		t.Fatal(err)
	}
	// append commits one event the way the run's own writers do: the seq and
	// the run's watermark move together, outside the HTTP process.
	seq := 0
	appendEvent := func(kind string, finish bool) {
		t.Helper()
		seq++
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO creative_run_events(account_id,run_id,seq,schema_version,event_type,payload) VALUES($1,$2,$3,1,$4,'{}')`, testAcctID, runID, seq, kind); err == nil {
			_, err = tx.Exec(`UPDATE creative_agent_runs SET last_event_seq=$3, finished_at=CASE WHEN $4 THEN now() ELSE finished_at END, state=CASE WHEN $4 THEN 'succeeded' ELSE state END WHERE account_id=$1 AND id=$2`, testAcctID, runID, seq, finish)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	appendEvent("run.accepted", false)
	appendEvent("run.state_changed", false)
	token := issueToken(t, tokens, testAcctID)
	path := "/api/v1/creative/agent-runs/" + runID + "/events"
	for _, scenario := range []struct {
		token, query string
		status       int
		code         string
	}{
		{"", "", 401, ""},
		{issueToken(t, tokens, "sse-run-other"), "", 404, ""},
		{token, "?after_seq=9", 409, "creative_snapshot_required"},
		{token, "?after_seq=01", 400, ""},
	} {
		response := shootPlanningRequest(t, router, "GET", path+scenario.query, scenario.token, "", "")
		if response.Code != scenario.status || !strings.Contains(response.Body.String(), scenario.code) {
			t.Fatalf("%s: %d %s", scenario.query, response.Code, response.Body.String())
		}
	}

	// The snapshot was read at seq 2; this event commits before the client
	// subscribes and must still arrive.
	appendEvent("block.created", false)
	server := httptest.NewServer(router)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL+path+"?after_seq=2", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	// after_seq wins over a stale Last-Event-ID.
	request.Header.Set("Last-Event-ID", "0")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != 200 || response.Header.Get("Content-Type") != "text/event-stream" || response.Header.Get("Cache-Control") != "no-store, no-transform" {
		t.Fatalf("SSE headers: %d %v", response.StatusCode, response.Header)
	}
	reader := bufio.NewReader(response.Body)
	frame := func() (id, event string, data map[string]any) {
		t.Helper()
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			line = strings.TrimSuffix(line, "\n")
			switch {
			case line == "":
				if event != "" {
					return id, event, data
				}
			case strings.HasPrefix(line, "id: "):
				id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &data); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if id, event, data := frame(); id != "3" || event != "block.created" || data["seq"] != "3" || data["run_id"] != runID {
		t.Fatalf("first frame %s %s %v", id, event, data)
	}
	// A write from outside the HTTP process wakes the stream after commit.
	appendEvent("block.updated", false)
	if id, event, _ := frame(); id != "4" || event != "block.updated" {
		t.Fatalf("live frame %s %s", id, event)
	}
	appendEvent("run.finished", true)
	if id, event, _ := frame(); id != strconv.Itoa(seq) || event != "run.finished" {
		t.Fatalf("final frame %s %s", id, event)
	}
	// Once the run is over and every event is out, the server closes.
	if rest, err := io.ReadAll(reader); err != nil || strings.Contains(string(rest), "event:") {
		t.Fatalf("stream did not end cleanly: %q %v", rest, err)
	}
}
