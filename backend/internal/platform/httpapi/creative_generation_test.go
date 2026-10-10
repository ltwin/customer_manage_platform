package httpapi_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestCreativeGenerationHTTPEmptyCatalogAndTrustedInput(t *testing.T) {
	router, st, tokens, url := newCustomerAPIRouterWithURL(t)
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err = db.Exec("UPDATE accounts SET status='active' WHERE id=$1", testAcctID); err != nil {
		t.Fatal(err)
	}
	scope := st.ScopeFor(auth.AccountContext{AccountID: testAcctID})
	command := func(v any) creativeops.Command {
		raw, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: raw}
	}
	r, err := creativecanvas.CreateProject(t.Context(), scope, command(creativecanvas.CreateProjectInput{Name: "生成入口"}))
	if err != nil {
		t.Fatal(err)
	}
	var project creativecanvas.ProjectResult
	if err = json.Unmarshal(r.Outcome.Response, &project); err != nil {
		t.Fatal(err)
	}
	node := "cwnode_" + uuid.NewString()
	_, err = creativecanvas.AddNode(t.Context(), scope, command(creativecanvas.AddNodeInput{CanvasID: project.CanvasID, NodeID: node, TypeKey: "core.text", ExpectedTopologyRevision: 1}))
	if err != nil {
		t.Fatal(err)
	}
	token := issueToken(t, tokens, testAcctID)
	base := "/api/v1/creative/canvases/" + project.CanvasID
	response := shootPlanningRequest(t, router, "GET", base+"/nodes/"+node+"/generation-catalog", token, "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"models":[]`) || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("catalog: %d %s", response.Code, response.Body.String())
	}
	for _, field := range []string{"account_id", "price", "deadline", "prepared"} {
		payload := map[string]any{"node_id": node, "action_key": "gateway.generation", "draft_revision": "1", "expected_data_revision": "1", "configuration_revision": "1", "read_set": []any{}, field: "untrusted"}
		c := command(payload)
		body, _ := json.Marshal(map[string]any{"operation_id": c.OperationID, "client_created_at": c.CreatedAt, "payload": payload})
		response = shootPlanningRequest(t, router, "POST", base+"/executions", token, c.OperationID, string(body))
		if response.Code != 400 {
			t.Fatalf("untrusted %s: %d %s", field, response.Code, response.Body.String())
		}
	}
}
