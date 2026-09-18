package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops/einoadapter"
)

func TestCanvasHTTPAndAgentUseTheSameRegisteredQuery(t *testing.T) {
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec(`UPDATE accounts SET status='active' WHERE id=$1`, testAcctID); err != nil {
		t.Fatal(err)
	}
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	if err := scope.Insert(t.Context(), "creative_account_capabilities", []string{"read_enabled", "manual_write_enabled"}, true, true); err != nil {
		t.Fatal(err)
	}
	receipt, err := creativecanvas.CreateProject(t.Context(), scope, creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: json.RawMessage(`{"name":"统一工具测试"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var project creativecanvas.ProjectResult
	if err = json.Unmarshal(receipt.Outcome.Response, &project); err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	write := func(payload any) {
		t.Helper()
		operation := uuid.NewString()
		body, err := json.Marshal(map[string]any{"operation_id": operation, "client_created_at": time.Now().UTC(), "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		response := shootPlanningRequest(t, router, http.MethodPost, "/api/v1/creative/canvases/"+project.CanvasID+"/commands", token, operation, string(body))
		if response.Code != 200 {
			t.Fatalf("write status=%d body=%s", response.Code, response.Body.String())
		}
	}
	nodeID := "cwnode_" + uuid.NewString()
	write(map[string]any{"type": "add_node", "node_id": nodeID, "type_key": "core.text", "x": 0, "y": 0, "expected_topology_revision": "1"})
	canvas, err := creativecanvas.GetCanvas(t.Context(), scope, project.CanvasID)
	if err != nil {
		t.Fatal(err)
	}
	// This fits the existing HTTP input depth limit. The canvas response adds
	// nodes/prompt wrappers and must still return the accepted parameters intact.
	parameters := json.RawMessage(strings.Repeat(`{"x":`, 62) + `0` + strings.Repeat(`}`, 62))
	write(map[string]any{"type": "save_prompt", "node_id": nodeID, "expected_draft_revision": nil, "expected_topology_revision": canvas.TopologyRevision, "read_set": creativecanvas.ReadSetFor(canvas), "action_key": "generate", "text": "深层参数", "model_key": "", "parameters": parameters, "references": []any{}})
	response := shootPlanningRequest(t, router, http.MethodGet, "/api/v1/creative/canvases/"+project.CanvasID, token, "", "")
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var snapshot struct {
		Nodes []struct {
			Prompt *creativecanvas.NodePrompt `json:"prompt"`
		} `json:"nodes"`
	}
	if err = json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].Prompt == nil || !sameJSON(snapshot.Nodes[0].Prompt.Parameters, parameters) {
		t.Fatal("saved prompt parameters lost in the tool response")
	}
	runtime, err := creativecanvas.NewToolRuntime(nil)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := runtime.ForAgent(scope, []creativeops.ToolRef{{Key: creativecanvas.ReadCanvasTool, Version: 1}}, func(context.Context, string, json.RawMessage) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := einoadapter.Bind(agent, creativecanvas.ReadCanvasTool, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"id": project.CanvasID})
	result, err := adapter.InvokableRun(t.Context(), string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if result != response.Body.String() {
		t.Fatal("HTTP and Agent disagree on the application query result")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private tool result cached")
	}
	if _, err = db.Exec(`UPDATE accounts SET status='pending_verification' WHERE id=$1`, testAcctID); err != nil {
		t.Fatal(err)
	}
	response = shootPlanningRequest(t, router, http.MethodGet, "/api/v1/creative/canvases/"+project.CanvasID, token, "", "")
	if response.Code != 403 {
		t.Fatalf("revoked HTTP status=%d", response.Code)
	}
	if _, err = adapter.InvokableRun(t.Context(), string(raw)); err == nil {
		t.Fatal("bound Agent tool cached revoked access")
	}
}
