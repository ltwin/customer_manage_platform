package creativegeneration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs/versionedfstest"
)

// fakeRelay 按 2026-10-09 实测的中转形态应答：创建返回任务ID，查询先处理中再完成。
type fakeRelay struct {
	mu          sync.Mutex
	sent        []map[string]any
	polls       int
	results     []string
	fallback    bool
	backupCalls int
}

func (r *fakeRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch {
	case strings.HasPrefix(req.URL.Path, "/openapi/v2/"):
		r.backupCalls++
		_, _ = io.WriteString(w, `{"taskId":"rh-task","status":"SUCCESS","results":[{"url":"https://rh-images-1252422369.cos.ap-beijing.myqcloud.com/output/result.png","outputType":"png"}]}`)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/images/generations":
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		r.sent = append(r.sent, body)
		if r.fallback {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid key"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"task-unified-1","status":"processing","progress":1}`)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/tasks/task-unified-1":
		r.polls++
		if r.polls == 1 {
			_, _ = io.WriteString(w, `{"id":"task-unified-1","status":"processing","progress":40}`)
			return
		}
		raw, _ := json.Marshal(map[string]any{"id": "task-unified-1", "status": "completed", "progress": 100, "results": r.results,
			"usage": map[string]any{"text_input_tokens": 20, "image_input_tokens": 0, "image_output_tokens": 548}})
		_, _ = w.Write(raw)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type imageFixture struct {
	database *store.Store
	gateway  *gw.Service
	media    *creativemedia.Service
	db       *sql.DB
	scope    store.AccountScope
	svc      *creativegeneration.Service
	product  *creativegeneration.Product
	relay    *fakeRelay
	target   creativecanvas.ExecutionTarget
	command  func() []byte
}

func setupImages(t *testing.T, fallback ...bool) *imageFixture {
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('img','test','active')`)
	must(t, err)
	st, err := store.Open(t.Context(), url)
	must(t, err)
	t.Cleanup(st.Close)
	must(t, st.MigrateCreativeJobs(t.Context()))
	scope := st.ScopeFor(auth.AccountContext{AccountID: "img"})
	catalog, err := gw.DefaultCatalog(func(string) (string, bool) { return "", false })
	must(t, err)
	gateway, err := gw.New(gw.Config{Catalog: catalog, DefaultBudget: gw.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 1_000_000}})
	must(t, err)
	cfg := creativemedia.DefaultConfig()
	media, err := creativemedia.NewService(cfg, versionedfstest.NewMemory("local"), creativemedia.NewVerifier(cfg), bytes.Repeat([]byte{1}, 32), "/media")
	must(t, err)

	png, err := os.ReadFile("../creativemedia/testdata/sample.png")
	must(t, err)
	relay := &fakeRelay{results: []string{"https://files.example/i/file_aa11.png", "https://files.example/i/file_bb22.png"}}
	relay.fallback = len(fallback) > 0 && fallback[0]
	server := httptest.NewTLSServer(relay)
	t.Cleanup(server.Close)
	adapter, err := gw.NewAICodeWithImages(server.Client(), server.URL, "relay-key")
	must(t, err)
	policies, err := gw.AICodeWithImagePolicies(catalog.Currency())
	must(t, err)
	c := creativegeneration.Config{Gateway: gateway, Media: media, ProtectionKey: bytes.Repeat([]byte{3}, 32), Fetchers: map[string]creativemedia.GenerationFetcher{},
		References: func(s store.AccountScope) gw.GenerationReferenceOpener { return media.GenerationReferenceOpenerFor(s) }}
	for _, p := range policies {
		c.Adapters = append(c.Adapters, gw.GenerationAdapterRegistration{DeploymentID: p.Definition.DeploymentID, Version: p.Definition.AdapterVersion, Adapter: adapter})
		c.Fetchers[p.Definition.DeploymentID] = &fetcher{data: png}
	}
	if relay.fallback {
		rh, err := gw.NewRunningHubImages(server.Client(), server.URL, func() (string, bool) { return "rh-key", true })
		must(t, err)
		rhPolicies, err := gw.RunningHubImagePolicies(catalog.Currency())
		must(t, err)
		for _, p := range rhPolicies {
			c.Adapters = append(c.Adapters, gw.GenerationAdapterRegistration{DeploymentID: p.Definition.DeploymentID, Version: p.Definition.AdapterVersion, Adapter: rh})
			c.Fetchers[p.Definition.DeploymentID] = &fetcher{data: png}
		}
		policies = append(policies, rhPolicies...)
	}
	svc, err := creativegeneration.New(c)
	must(t, err)
	q, err := st.NewJobRuntime(svc.Handlers(), nil)
	must(t, err)
	svc.SetRuntime(q)
	product, err := creativegeneration.NewProduct(svc, creativegeneration.ImageRegistrations(svc, policies))
	must(t, err)

	r, err := creativecanvas.CreateProject(t.Context(), scope, cmd(creativecanvas.CreateProjectInput{Name: "生图"}))
	must(t, err)
	var project creativecanvas.ProjectResult
	must(t, json.Unmarshal(r.Outcome.Response, &project))
	node := "cwnode_" + uuid.NewString()
	_, err = creativecanvas.AddNode(t.Context(), scope, cmd(creativecanvas.AddNodeInput{CanvasID: project.CanvasID, NodeID: node, TypeKey: "core.image", ExpectedTopologyRevision: 1}))
	must(t, err)
	canvas, err := creativecanvas.GetCanvas(t.Context(), scope, project.CanvasID)
	must(t, err)
	model, parameters := "gpt-image-2.5-sunburst", json.RawMessage(`{"aspect_ratio":"2:3","count":2}`)
	if relay.fallback {
		model = "gpt-image-2"
		parameters = json.RawMessage(`{"aspect_ratio":"2:3","count":1}`)
	}
	_, err = creativecanvas.SaveNodePrompt(t.Context(), scope, cmd(creativecanvas.SavePromptInput{CanvasID: project.CanvasID, NodeID: node, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas), ActionKey: creativecanvas.GenerationAction,
		Text: "逆光人像，日出", ModelKey: model, Parameters: parameters}))
	must(t, err)
	canvas, err = creativecanvas.GetCanvas(t.Context(), scope, project.CanvasID)
	must(t, err)
	in := creativecanvas.RequestExecutionInput{CanvasID: project.CanvasID, NodeID: node, ExpectedDataRevision: canvas.Nodes[0].DataRevision, DraftRevision: canvas.Nodes[0].Prompt.Revision,
		ActionKey: creativecanvas.GenerationAction, ReadSet: creativecanvas.ReadSetFor(canvas), ConfigurationRevision: gw.AICodeWithImagePolicyVersion}
	return &imageFixture{database: st, gateway: gateway, media: media, db: db, scope: scope, svc: svc, product: product, relay: relay, target: creativecanvas.ExecutionTarget{CanvasID: project.CanvasID}, command: func() []byte { return creativegraph.Canonical(in) }}
}

func (f *imageFixture) advance(t *testing.T) {
	t.Helper()
	// 跳过轮询间隔，直接推进下一阶段。
	_, err := f.db.Exec(`UPDATE creative_generation_bindings SET next_step_at=clock_timestamp()-interval '1 second';UPDATE llm_generation_requests SET next_advance_at=clock_timestamp()-interval '1 second' WHERE next_advance_at IS NOT NULL`)
	must(t, err)
	err = f.svc.Work(t.Context(), f.scope, jobs.Request{Kind: "canvas.generation_handoff", Payload: creativegraph.Canonical(f.target)})
	var deferred *jobs.DeferredError
	if err != nil && !errors.As(err, &deferred) {
		t.Fatal(err)
	}
}

func TestImageGenerationRunsThroughTheRelayOnce(t *testing.T) {
	f := setupImages(t)
	catalog, err := f.product.Catalog(t.Context(), f.scope)
	must(t, err)
	if len(catalog.Models) != 3 || catalog.Models[0].Parameters[0].Default != "1:1" {
		t.Fatalf("catalog %+v", catalog)
	}
	command := cmd(nil)
	command.Payload = f.command()
	receipt, err := f.product.Request(t.Context(), f.scope, command)
	must(t, err)
	var e creativecanvas.NodeExecution
	must(t, json.Unmarshal(receipt.Outcome.Response, &e))
	f.target.ExecutionID = e.ID

	// 生成一经受理就会跑完扣费，摄影师入口不能取消。
	if _, err := creativecanvas.CancelPhotographerExecution(t.Context(), f.scope, cmd(f.target)); !errors.Is(err, creativecanvas.ErrActionUnavailable) {
		t.Fatalf("photographer cancelled a generation: %v", err)
	}
	for range 6 {
		f.advance(t)
	}
	if len(f.relay.sent) != 1 {
		t.Fatalf("submitted %d times", len(f.relay.sent))
	}
	sent := f.relay.sent[0]
	if sent["model"] != "gpt-image-2.5-sunburst" || sent["size"] != "2:3" || sent["resolution"] != "2K" || sent["quality"] != "medium" || sent["n"] != float64(2) || sent["prompt"] != "逆光人像，日出" {
		t.Fatalf("sent %v", sent)
	}
	if count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 2 {
		for _, q := range []string{
			"SELECT state||' '||coalesce(progress::text,'') FROM llm_generation_tasks",
			"SELECT state FROM llm_requests",
			"SELECT verification_required::text||' '||failures::text FROM creative_generation_bindings",
			"SELECT count(*)::text FROM llm_generation_outputs",
			"SELECT state||' '||coalesce(error,'') FROM creative_node_executions",
			"SELECT coalesce(string_agg(observation_state||':'||coalesce(error_code,''),','),'') FROM llm_generation_observations",
		} {
			var v sql.NullString
			_ = f.db.QueryRow(q).Scan(&v)
			t.Log(q, "=>", v.String)
		}
		t.Logf("polls %d", f.relay.polls)
		t.Fatal("two images were not kept as versions")
	}
	// 结算按实际token：20×5 + 548×27.2 微美元。
	var cost int64
	must(t, f.db.QueryRow(`SELECT COALESCE(sum(cost_micros),0) FROM llm_usage_measurements WHERE cost_component='generation_total'`).Scan(&cost))
	if cost != 100+14906 {
		t.Fatalf("settled %d", cost)
	}
	var state, applied string
	must(t, f.db.QueryRow(`SELECT state,apply_state FROM creative_node_executions WHERE id=$1`, e.ID).Scan(&state, &applied))
	if state != "succeeded" || applied != "applied" {
		t.Fatalf("execution %s/%s", state, applied)
	}
}

func mustNode(t *testing.T, f *imageFixture) string {
	t.Helper()
	var node string
	must(t, f.db.QueryRow(`SELECT id FROM creative_nodes WHERE canvas_id=$1`, f.target.CanvasID).Scan(&node))
	return node
}

// 助手的生成只做生成：按节点上已写好的设置提交一次，提示词被改过就拒绝，同一身份重放不再提交。
func TestAgentGenerateNodeSubmitsOnce(t *testing.T) {
	f := setupImages(t)
	node := mustNode(t, f)
	for _, bad := range []struct {
		model, nodeType string
		params          map[string]any
	}{{"sdxl", "core.image", nil}, {"gpt-image-2", "core.text", nil}, {"gpt-image-2", "core.image", map[string]any{"resolution": "8K"}}, {"gpt-image-2", "core.image", map[string]any{"seed": 1}}} {
		var refusal *creativegeneration.Refusal
		if _, err := f.product.DraftParameters(bad.model, bad.nodeType, bad.params); !errors.As(err, &refusal) {
			t.Fatalf("not refused: %+v %v", bad, err)
		}
	}
	params, err := f.product.DraftParameters("gpt-image-2", "core.image", map[string]any{"count": float64(4), "quality": "high"})
	if err != nil || string(params) != `{"count":4,"quality":"high"}` {
		t.Fatalf("parameters %s %v", params, err)
	}

	in := creativegeneration.AgentGeneration{Seed: "run/step", CanvasID: f.target.CanvasID, NodeID: node, DraftRevision: 2}
	var refusal *creativegeneration.Refusal
	if _, err = f.product.GenerateNode(t.Context(), f.scope, in); !errors.As(err, &refusal) || !strings.Contains(refusal.Message, "已被改动") {
		t.Fatalf("stale draft not refused: %v", err)
	}
	in.DraftRevision = 1
	first, err := f.product.GenerateNode(t.Context(), f.scope, in)
	must(t, err)
	if first.State != "running" || first.ApplyState != "pending" || !first.Adopted() || first.NodeID != node {
		t.Fatalf("first %+v", first)
	}
	f.target.ExecutionID = first.ExecutionID
	for range 6 {
		f.advance(t)
	}
	again, err := f.product.GenerateNode(t.Context(), f.scope, in)
	must(t, err)
	if again.State != "succeeded" || again.ApplyState != "applied" || again.ExecutionID != first.ExecutionID || len(again.ContentRevisionIDs) != 2 || len(f.relay.sent) != 1 {
		t.Fatalf("replay %+v, submitted %d times", again, len(f.relay.sent))
	}
	if sent := f.relay.sent[0]; sent["model"] != "gpt-image-2.5-sunburst" || sent["size"] != "2:3" || sent["n"] != float64(2) {
		t.Fatalf("sent %v", sent)
	}
}

func TestImageGenerationFallbackPublishesOnce(t *testing.T) {
	f := setupImages(t, true)
	catalog, err := f.product.Catalog(t.Context(), f.scope)
	must(t, err)
	if len(catalog.Models) != 6 {
		t.Fatalf("duplicated logical model: %+v", catalog.Models)
	}
	c := cmd(nil)
	c.Payload = f.command()
	receipt, err := f.product.Request(t.Context(), f.scope, c)
	must(t, err)
	var e creativecanvas.NodeExecution
	must(t, json.Unmarshal(receipt.Outcome.Response, &e))
	f.target.ExecutionID = e.ID
	for range 6 {
		_, err = f.db.Exec(`UPDATE llm_requests SET updated_at=clock_timestamp()-interval '10 seconds' WHERE state='failed';UPDATE llm_generation_routes SET next_advance_at=clock_timestamp()-interval '1 second' WHERE state='active'`)
		must(t, err)
		f.advance(t)
	}
	if len(f.relay.sent) != 1 || f.relay.backupCalls != 1 {
		t.Fatalf("primary=%d backup=%d", len(f.relay.sent), f.relay.backupCalls)
	}
	if count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 1 {
		t.Fatal("backup result was not published once")
	}
	var state, applied string
	must(t, f.db.QueryRow(`SELECT state,apply_state FROM creative_node_executions WHERE id=$1`, e.ID).Scan(&state, &applied))
	if state != "succeeded" || applied != "applied" {
		t.Fatalf("execution %s/%s", state, applied)
	}
	if count(t, f.db, "SELECT count(*) FROM llm_requests") != 2 {
		t.Fatal("attempt accounting was overwritten")
	}
	_, err = f.product.Request(t.Context(), f.scope, c)
	must(t, err)
	if len(f.relay.sent) != 1 || f.relay.backupCalls != 1 {
		t.Fatal("replay dispatched again")
	}
}

func TestRunningHubKeyOnlyRestartRetainsRecovery(t *testing.T) {
	f := setupImages(t, true)
	_, err := f.product.Catalog(t.Context(), f.scope)
	must(t, err)
	command := cmd(nil)
	command.Payload = f.command()
	receipt, err := f.product.Request(t.Context(), f.scope, command)
	must(t, err)
	var execution creativecanvas.NodeExecution
	must(t, json.Unmarshal(receipt.Outcome.Response, &execution))
	var root string
	must(t, f.db.QueryRow("SELECT request_id FROM creative_generation_bindings WHERE execution_id=$1", execution.ID).Scan(&root))
	// Obtain the result without running the media handoff yet.
	for range 3 {
		_, err = f.db.Exec("UPDATE llm_requests SET updated_at=clock_timestamp()-interval '10 seconds' WHERE state='failed'; UPDATE llm_generation_routes SET next_advance_at=clock_timestamp()-interval '1 second' WHERE state='active'")
		must(t, err)
		_, err = f.svc.GatewayRuntime().AdvanceGeneration(t.Context(), f.scope, root)
		must(t, err)
	}
	var current string
	must(t, f.scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		e := tx.QueryRow(t.Context(), "llm_generation_routes", "current_request_id", "root_request_id=$2", root).Scan(&current)
		return e
	}))
	imports, err := f.svc.GatewayRuntime().ImportableOutputs(t.Context(), f.scope, root, "creative_canvas", execution.ID)
	must(t, err)
	if len(imports) != 1 {
		t.Fatalf("imports=%d", len(imports))
	}
	// The production HTTP constructor clones this test-only transport.
	polls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openapi/v2/query" {
			t.Errorf("unexpected resubmission %s", r.URL.Path)
		}
		polls++
		_, _ = io.WriteString(w, `{"taskId":"rh-task","status":"SUCCESS","results":[{"url":"https://rh-images-1252422369.cos.ap-beijing.myqcloud.com/output/result.png","outputType":"png"}]}`)
	}))
	defer server.Close()
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig.ServerName = "127.0.0.1"
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	prior := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = prior; transport.CloseIdleConnections() })
	t.Setenv("CREATIVE_GENERATION_PROTECTION_KEY", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32)))
	composed, err := creativegeneration.Compose(f.gateway, "USD", f.media, func(k string) (string, bool) {
		switch k {
		case "RUNNINGHUB_API_KEY":
			return "key", true
		}
		return "", false
	})
	must(t, err)
	if composed.Service == nil {
		t.Fatal("missing RunningHub runtime")
	}
	queue, err := f.database.NewJobRuntime(composed.Service.Handlers(), nil)
	must(t, err)
	composed.Service.SetRuntime(queue)
	if len(composed.Product.Models()) != 4 {
		t.Fatal("key-only composition must enable models and retain recovery")
	}
	_, err = f.db.Exec("UPDATE llm_generation_requests SET next_advance_at=clock_timestamp()-interval '1 second' WHERE request_id=$1", current)
	must(t, err)
	progress, err := composed.Service.GatewayRuntime().AdvanceGeneration(t.Context(), f.scope, root)
	must(t, err)
	if progress.VerificationRequired || progress.ResultVerificationRequired || progress.BillingReconciliationRequired {
		t.Fatal("unbilled generation must not create billing reconciliation")
	}
	refreshed, err := composed.Service.GatewayRuntime().RefreshImportLocation(t.Context(), f.scope, imports[0])
	must(t, err)
	if refreshed.Output.Media.FetchURL == "" || polls != 1 {
		t.Fatalf("recovery polls=%d", polls)
	}
}
