package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The steer route is wired to the service: a run out of reach is absent.
// What the service refuses is covered in creativeagent's steer tests.
func TestSteerRouteIsWired(t *testing.T) {
	f := newMCPFixture(t, false)
	steer := func(runID, segments string) (int, string) {
		t.Helper()
		key := uuid.NewString()
		body := `{"operation_id":"` + key + `","client_created_at":"2026-11-11T08:00:00Z","payload":{"instruction_segments":` + segments + `}}`
		req, err := http.NewRequestWithContext(t.Context(), "POST", f.server.URL+"/api/v1/creative/agent-runs/"+runID+"/steers", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+f.web)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}
	if status, body := steer("ccrn_"+uuid.NewString(), `[{"type":"text","text":"换个方向"}]`); status != 404 || !strings.Contains(body, "not_found") {
		t.Fatalf("absent run: %d %s", status, body)
	}
}
