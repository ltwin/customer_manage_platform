package creativegeneration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativegeneration"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs/versionedfstest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

type adapter struct {
	noUsage bool
	calls   atomic.Int64
	outputs []gw.GenerationOutput
}

func (a *adapter) Submit(_ context.Context, call gw.GenerationCall) (gw.GenerationEvent, error) {
	a.calls.Add(1)
	if a.noUsage {
		return gw.GenerationEvent{EventKey: "done", Observation: gw.GenerationObservation{State: gw.GenerationCompleted, TaskID: "task-" + call.RequestID, Outputs: a.outputs}}, nil
	}
	return gw.GenerationEvent{EventKey: "done", Observation: gw.GenerationObservation{State: gw.GenerationCompleted, TaskID: "task-" + call.RequestID, Outputs: a.outputs}, Usage: map[string]int64{"count": 2}}, nil
}
func (a *adapter) Observe(ctx context.Context, c gw.GenerationCall) (gw.GenerationEvent, error) {
	return gw.GenerationEvent{}, errors.New("unexpected observe")
}
func (a *adapter) Cancel(context.Context, gw.GenerationCall) (gw.GenerationEvent, error) {
	return gw.GenerationEvent{}, errors.New("unexpected cancel")
}

type fetcher struct {
	data  []byte
	fail  atomic.Bool
	calls atomic.Int64
}

func (f *fetcher) Open(_ context.Context, location string) (io.ReadCloser, error) {
	f.calls.Add(1)
	if f.fail.Load() && location == "https://assets.example/b" {
		return nil, errors.New("injected fetch failure")
	}
	return io.NopCloser(bytes.NewReader(f.data)), nil
}

type fixture struct {
	db        *sql.DB
	st        *store.Store
	scope     store.AccountScope
	svc       *creativegeneration.Service
	a         *adapter
	f         *fetcher
	p         gw.PreparedPolicyGeneration
	target    creativecanvas.ExecutionTarget
	node      string
	command   creativeops.Command
	admission gw.GenerationAdmission
}

func cmd(v any) creativeops.Command {
	return creativeops.Command{OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: creativegraph.Canonical(v)}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func setup(t *testing.T, kind gw.GenerationKind) *fixture {
	t.Helper()
	url := storetest.NewURL(t)
	db, err := sql.Open("pgx", url)
	must(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES('g4','test','active'),('g4-other','test','active'); INSERT INTO creative_account_capabilities(account_id,node_generate_enabled,generation_apply_enabled)VALUES('g4',true,true)`)
	must(t, err)
	st, err := store.Open(t.Context(), url)
	must(t, err)
	t.Cleanup(st.Close)
	must(t, st.MigrateCreativeJobs(t.Context()))
	scope := st.ScopeFor(auth.AccountContext{AccountID: "g4"})
	catalog, err := gw.DefaultCatalog(func(string) (string, bool) { return "", false })
	must(t, err)
	gateway, err := gw.New(gw.Config{Catalog: catalog, DefaultBudget: gw.BudgetPolicy{MonthlyLimitMicros: 10000000, MonthlyTokenLimit: 1000000}})
	must(t, err)
	cfg := creativemedia.DefaultConfig()
	media, err := creativemedia.NewService(cfg, versionedfstest.NewMemory("local"), creativemedia.NewVerifier(cfg), bytes.Repeat([]byte{1}, 32), "/media")
	must(t, err)
	f := &fixture{db: db, st: st, scope: scope, a: &adapter{}, f: &fetcher{}}
	if kind == gw.GenerationImage {
		f.f.data, err = os.ReadFile("../creativemedia/testdata/sample.png")
		must(t, err)
	}
	for i := range 2 {
		out := gw.GenerationOutput{ID: fmt.Sprint("output-", i), Index: i, Kind: kind, Text: fmt.Sprint("作品", i)}
		if kind != gw.GenerationText {
			out.Text = ""
			out.Media = &gw.GeneratedMedia{MIME: "image/png", ByteSize: int64(len(f.f.data)), FetchURL: []string{"https://assets.example/a", "https://assets.example/b"}[i], ExpiresAt: time.Now().Add(time.Hour)}
		}
		f.a.outputs = append(f.a.outputs, out)
	}
	s, err := creativegeneration.New(creativegeneration.Config{Authorization: func(context.Context, store.TxAccountScope, gw.RequestView, gw.PreparedPolicyGeneration) error {
		return nil
	}, Gateway: gateway, Media: media, ProtectionKey: bytes.Repeat([]byte{2}, 32), Adapters: []gw.GenerationAdapterRegistration{{DeploymentID: "test-deployment", Version: "1", LimitKey: "g4-limit", Concurrency: 2, RateLimit: 100, Adapter: f.a}}, Fetchers: map[string]creativemedia.GenerationFetcher{"test-deployment": f.f}})
	must(t, err)
	f.svc = s
	q, err := st.NewJobRuntime(s.Handlers(), nil)
	must(t, err)
	s.SetRuntime(q)
	schema := json.RawMessage(`{"type":"object","additionalProperties":false,"required":["prompt","parameters"],"properties":{"prompt":{"type":"string"},"parameters":{"type":"object","additionalProperties":false,"required":["count"],"properties":{"count":{"type":"integer","minimum":1,"maximum":2}}}}}`)
	output := gw.GenerationOutputPolicy{Kind: kind, MinOutputs: 1, MaxOutputs: 2, MaxBytes: 1000000}
	if kind != gw.GenerationText {
		output.MIMEs = []string{"image/png"}
	}
	d := gw.GenerationPolicyDefinition{EngineVersion: gw.GenerationPolicyEngineVersion, Version: "1", Profile: gw.GenerationProfileDefinition{ModelKey: "test-model", ModelRevision: "1", Version: "1"}, DeploymentID: "test-deployment", AdapterVersion: "1", Currency: catalog.Currency(), Modes: []gw.GenerationPolicyMode{{Definition: gw.GenerationModeDefinition{Key: "generate", Version: "1", Kind: kind, InputSchema: schema}, Output: output, Parameters: map[string]gw.GenerationFactType{"count": {Type: "number", Unit: "count"}}, Pricing: []gw.GenerationPriceComponent{{ID: "output", Estimate: gw.GenerationValueRef{Source: "parameter", Field: "count", Unit: "count"}, Usage: "count", Unit: "count", Divisor: 1, QuantityRound: "ceil", Tiers: []gw.GenerationPriceTier{{ID: "base", Fallback: true, Rate: gw.GenerationRatio{Numerator: 1000, Denominator: 1}}}}}}}}
	upper := int64(2)
	policy, err := gw.CompileGenerationPolicy(creativegraph.Canonical(d), gw.GenerationPolicyTarget{ModelKey: "test-model", ModelRevision: "1", DeploymentID: "test-deployment", AdapterVersion: "1", Modes: []string{"generate"}, Usage: map[string]gw.GenerationUsageDefinition{"count": {Unit: "count", UpperBound: &upper}}})
	must(t, err)
	request := gw.GenerationRequest{Version: gw.GenerationContractVersion, ModelKey: "test-model", Kind: kind, Mode: "generate", Input: gw.GenerationInput{Prompt: "生成作品", Parameters: json.RawMessage(`{"count":2}`)}}
	facts, err := gw.ReadGenerationFacts(t.Context(), request, nil)
	must(t, err)
	prepared, err := policy.Prepare(request, facts)
	must(t, err)
	if prepared.Verdict != gw.GenerationAllow {
		t.Fatal(prepared)
	}
	f.p = prepared.Prepared
	must(t, scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := s.GatewayRuntime().ConfigureDeploymentInTx(t.Context(), tx, d, true, time.Now().Add(time.Hour), 0)
		return err
	}))
	r, err := creativecanvas.CreateProject(t.Context(), scope, cmd(creativecanvas.CreateProjectInput{Name: "G4"}))
	must(t, err)
	var project creativecanvas.ProjectResult
	must(t, json.Unmarshal(r.Outcome.Response, &project))
	f.node = "cwnode_" + uuid.NewString()
	_, err = creativecanvas.AddNode(t.Context(), scope, cmd(creativecanvas.AddNodeInput{CanvasID: project.CanvasID, NodeID: f.node, TypeKey: "core." + string(kind), ExpectedTopologyRevision: 1}))
	must(t, err)
	canvas, err := creativecanvas.GetCanvas(t.Context(), scope, project.CanvasID)
	must(t, err)
	_, err = creativecanvas.SaveNodePrompt(t.Context(), scope, cmd(creativecanvas.SavePromptInput{CanvasID: project.CanvasID, NodeID: f.node, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas), ActionKey: creativecanvas.GenerationAction, Text: "生成作品", ModelKey: "test-model", Parameters: json.RawMessage(`{"count":2}`)}))
	must(t, err)
	canvas, err = creativecanvas.GetCanvas(t.Context(), scope, project.CanvasID)
	must(t, err)
	f.command = cmd(creativecanvas.RequestExecutionInput{CanvasID: project.CanvasID, NodeID: f.node, ExpectedDataRevision: canvas.Nodes[0].DataRevision, DraftRevision: canvas.Nodes[0].Prompt.Revision, ActionKey: creativecanvas.GenerationAction, ReadSet: creativecanvas.ReadSetFor(canvas)})
	deadline := time.Now().Add(time.Hour)
	f.admission = gw.GenerationAdmission{Prepared: f.p, Deadline: deadline, GroupDeadline: deadline, GroupLimitMicros: 1000000, GroupTokenLimit: 1000}
	f.target.CanvasID = project.CanvasID
	return f
}
func (f *fixture) admit(t *testing.T) {
	t.Helper()
	r, err := f.svc.Request(t.Context(), f.scope, f.command, f.admission, 1)
	must(t, err)
	var e creativecanvas.NodeExecution
	must(t, json.Unmarshal(r.Outcome.Response, &e))
	f.target.ExecutionID = e.ID
}
func (f *fixture) step(t *testing.T) {
	t.Helper()
	_, err := f.db.Exec(`UPDATE creative_generation_bindings SET next_step_at=clock_timestamp()-interval '1 second' WHERE execution_id=$1`, f.target.ExecutionID)
	must(t, err)
	err = f.svc.Work(t.Context(), f.scope, jobs.Request{Kind: "canvas.generation_handoff", Payload: creativegraph.Canonical(f.target)})
	if err != nil {
		var deferred *jobs.DeferredError
		if !errors.As(err, &deferred) {
			t.Fatal(err)
		}
	}
}
func count(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	must(t, db.QueryRow(q).Scan(&n))
	return n
}
func TestGenerationTextHandoffVersionsAndReplay(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	_, err := f.svc.Request(t.Context(), f.scope, f.command, f.admission, 1)
	must(t, err)
	for range 4 {
		f.step(t)
	}
	if f.a.calls.Load() != 1 || count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 2 || count(t, f.db, "SELECT count(*) FROM llm_result_consumers WHERE state='consumed'") != 1 {
		t.Fatal("duplicate or missing handoff")
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), f.scope, f.target.CanvasID, f.node)
	must(t, err)
	if versions.SelectedVersionID == nil {
		t.Fatal("not selected")
	}
}
func TestGenerationMediaPartialFailureNeverResubmits(t *testing.T) {
	f := setup(t, gw.GenerationImage)
	f.f.fail.Store(true)
	f.admit(t)
	f.step(t)
	f.step(t)
	if count(t, f.db, "SELECT count(*) FROM llm_result_consumers WHERE state='pending'") != 1 || count(t, f.db, "SELECT count(*) FROM creative_execution_refs WHERE direction='output'") != 1 {
		t.Fatal("partial output released consumer")
	}
	f.f.fail.Store(false)
	for range 3 {
		f.step(t)
	}
	if f.a.calls.Load() != 1 || count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 2 {
		t.Fatal("import retried generation")
	}
}

func TestGenerationAdmissionRollbackAndSameKeyConcurrency(t *testing.T) {
	f := setup(t, gw.GenerationText)
	_, err := f.db.Exec(`CREATE FUNCTION g4_fail_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.operation_type='canvas.request_generation' THEN RAISE EXCEPTION 'receipt injected'; END IF; RETURN NEW; END $$;CREATE TRIGGER g4_receipt BEFORE INSERT ON creative_operation_receipts FOR EACH ROW EXECUTE FUNCTION g4_fail_receipt()`)
	must(t, err)
	if _, err = f.svc.Request(t.Context(), f.scope, f.command, f.admission, 1); err == nil {
		t.Fatal("missing rollback failure")
	}
	for _, table := range []string{"llm_requests", "llm_usage_reservations", "llm_result_consumers", "creative_node_executions", "creative_generation_bindings"} {
		if count(t, f.db, "SELECT count(*) FROM "+table) != 0 {
			t.Fatal("partial acceptance", table)
		}
	}
	_, err = f.db.Exec(`DROP TRIGGER g4_receipt ON creative_operation_receipts`)
	must(t, err)
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := f.svc.Request(t.Context(), f.scope, f.command, f.admission, 1); results <- err }()
	}
	for range 2 {
		must(t, <-results)
	}
	if count(t, f.db, "SELECT count(*) FROM llm_requests") != 1 || count(t, f.db, "SELECT count(*) FROM creative_node_executions") != 1 {
		t.Fatal("duplicate admission")
	}
}
func TestGenerationCancelKeepsCandidates(t *testing.T) {
	f := setup(t, gw.GenerationImage)
	f.admit(t)
	f.step(t)
	_, err := creativecanvas.CancelNodeExecution(t.Context(), f.scope, cmd(f.target))
	must(t, err)
	for range 3 {
		f.step(t)
	}
	if count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 0 || count(t, f.db, "SELECT count(*) FROM creative_execution_refs WHERE direction='output'") != 2 || count(t, f.db, "SELECT count(*) FROM llm_result_consumers WHERE state='consumed'") != 1 {
		t.Fatal("cancel lost candidates or published")
	}
}
func TestGenerationImportCommitFailureReusesStoredObjects(t *testing.T) {
	f := setup(t, gw.GenerationImage)
	f.admit(t)
	_, err := f.db.Exec(`CREATE FUNCTION g4_fail_handoff() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='ready' THEN RAISE EXCEPTION 'handoff injected'; END IF;RETURN NEW;END $$;CREATE TRIGGER g4_handoff BEFORE UPDATE ON creative_generation_imports FOR EACH ROW EXECUTE FUNCTION g4_fail_handoff()`)
	must(t, err)
	f.step(t)
	if count(t, f.db, "SELECT count(*) FROM creative_generation_imports WHERE state='stored'") != 1 || count(t, f.db, "SELECT count(*) FROM creative_execution_refs WHERE direction='output'") != 0 {
		t.Fatal("storage checkpoint missing or partial DB handoff")
	}
	_, err = f.db.Exec(`DROP TRIGGER g4_handoff ON creative_generation_imports;UPDATE llm_generation_outputs SET projection=jsonb_set(projection,'{media,expires_at}','"2000-01-01T00:00:00Z"') WHERE ordinal=0`)
	must(t, err)
	for range 4 {
		f.step(t)
	}
	if f.f.calls.Load() != 2 || f.a.calls.Load() != 1 || count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 2 {
		t.Fatal("stored output refetched or resubmitted", f.f.calls.Load())
	}
}
func TestGenerationPartialSuccessAndExplicitImportResume(t *testing.T) {
	f := setup(t, gw.GenerationImage)
	f.admit(t)
	f.f.fail.Store(true)
	for range 6 {
		f.step(t)
	}
	if count(t, f.db, "SELECT count(*) FROM creative_generation_bindings WHERE verification_required") != 1 || count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 1 || count(t, f.db, "SELECT count(*) FROM llm_result_consumers WHERE state='pending'") != 1 {
		t.Fatal("partial failure not retained")
	}
	f.f.fail.Store(false)
	must(t, f.svc.Resume(t.Context(), f.scope, f.target))
	for range 3 {
		f.step(t)
	}
	if count(t, f.db, "SELECT count(*) FROM creative_node_versions") != 1 || count(t, f.db, "SELECT count(*) FROM creative_execution_refs WHERE direction='output'") != 2 || f.a.calls.Load() != 1 {
		t.Fatal("late result overwrote partial publication")
	}
}
func TestGenerationCapabilityRevocationAndCrossAccount(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	_, err := f.db.Exec(`UPDATE creative_account_capabilities SET node_generate_enabled=false,generation_apply_enabled=false WHERE account_id='g4'`)
	must(t, err)
	f.step(t)
	if f.a.calls.Load() != 0 {
		t.Fatal("revoked generation dispatched")
	}
	other := f.st.ScopeFor(auth.AccountContext{AccountID: "g4-other"})
	if err = f.svc.Work(t.Context(), other, jobs.Request{Payload: creativegraph.Canonical(f.target)}); err == nil {
		t.Fatal("cross-account execution accessed")
	}
}

func TestGenerationEditedTargetRegistersUnselectedVersions(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	f.step(t)
	_, err := f.db.Exec(`UPDATE creative_nodes SET data_revision=data_revision+1 WHERE id=$1`, f.node)
	must(t, err)
	for range 3 {
		f.step(t)
	}
	versions, err := creativecanvas.ListNodeVersions(t.Context(), f.scope, f.target.CanvasID, f.node)
	must(t, err)
	if len(versions.Items) != 2 || versions.SelectedVersionID != nil {
		t.Fatal("edited node was overwritten", versions)
	}
	e, err := creativecanvas.GetNodeExecution(t.Context(), f.scope, f.target)
	must(t, err)
	if e.ApplyState != "conflicted" {
		t.Fatal(e.ApplyState)
	}
}
func TestGenerationConcurrentCancellationAndPublication(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	f.step(t)
	f.step(t)
	_, err := f.db.Exec(`UPDATE creative_generation_bindings SET next_step_at=clock_timestamp()-interval '1 second'`)
	must(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	hold, err := f.db.BeginTx(ctx, nil)
	must(t, err)
	_, err = hold.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('creative-write:g4',0))`)
	must(t, err)
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		<-start
		done <- f.svc.Work(ctx, f.scope, jobs.Request{Payload: creativegraph.Canonical(f.target)})
	}()
	go func() {
		<-start
		_, err := creativecanvas.CancelNodeExecution(ctx, f.scope, cmd(f.target))
		done <- err
	}()
	close(start)
	must(t, hold.Commit())
	for range 2 {
		err := <-done
		var deferred *jobs.DeferredError
		if err != nil && !errors.As(err, &deferred) {
			t.Fatal(err)
		}
	}
	e, err := creativecanvas.GetNodeExecution(t.Context(), f.scope, f.target)
	must(t, err)
	n := count(t, f.db, "SELECT count(*) FROM creative_node_versions")
	if e.State == "cancelled" && n != 0 || e.ApplyState == "applied" && n != 2 {
		t.Fatal("publication/cancellation not atomic", e.State, e.ApplyState, n)
	}
}
func TestGenerationQueueRepairAfterDiscard(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	_, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='discarded',finalized_at=now()`)
	must(t, err)
	_, err = f.svc.SweepDue(t.Context(), f.st, "", 100)
	must(t, err)
	if count(t, f.db, "SELECT count(*) FROM creative_jobs.river_job WHERE state='available'") == 0 {
		t.Fatal("due scanner did not replace discarded job")
	}
	for range 3 {
		f.step(t)
	}
	if f.a.calls.Load() != 1 {
		t.Fatal("repair reissued generation")
	}
}

func TestGenerationReviewRevokedBeforeDispatchReleasesHold(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	_, err := f.db.Exec(`UPDATE creative_account_capabilities SET node_generate_enabled=false WHERE account_id='g4'`)
	must(t, err)
	for range 6 {
		_, err = f.db.Exec(`UPDATE creative_generation_bindings SET next_step_at=clock_timestamp()-interval '1 second',lease_until=NULL`)
		must(t, err)
		err = f.svc.Work(t.Context(), f.scope, jobs.Request{Payload: creativegraph.Canonical(f.target)})
		t.Logf("work result: %v", err)
	}
	var state string
	var hold int64
	must(t, f.db.QueryRow(`SELECT state FROM llm_requests`).Scan(&state))
	must(t, f.db.QueryRow(`SELECT remaining_hold_micros FROM llm_usage_reservations`).Scan(&hold))
	t.Logf("state=%s hold=%d", state, hold)
	if state == "prepared" || hold != 0 {
		t.Fatal("undispatched revoked generation retains reservation")
	}
}

func TestGenerationReviewUnknownBillingDoesNotBlockKnownOutputs(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.a.noUsage = true
	f.admit(t)
	f.step(t)
	var request string
	must(t, f.db.QueryRow(`SELECT request_id FROM creative_generation_bindings`).Scan(&request))
	for range 6 {
		_, err := f.db.Exec(`UPDATE llm_generation_requests SET next_advance_at=clock_timestamp()-interval '1 second' WHERE NOT verification_required`)
		must(t, err)
		_, err = f.svc.GatewayRuntime().AdvanceGeneration(t.Context(), f.scope, request)
		must(t, err)
	}
	for range 6 {
		f.step(t)
	}
	t.Logf("imported=%d versions=%d pending_consumers=%d", count(t, f.db, `SELECT count(*) FROM llm_generation_outputs WHERE import_state='imported'`), count(t, f.db, `SELECT count(*) FROM creative_node_versions`), count(t, f.db, `SELECT count(*) FROM llm_result_consumers WHERE state='pending'`))
	if count(t, f.db, `SELECT count(*) FROM creative_node_versions`) != 2 {
		t.Fatal("billing observation errors blocked known completed outputs")
	}
	var hold, spent int64
	must(t, f.db.QueryRow(`SELECT reserved_micros,spent_micros FROM llm_budgets WHERE account_id='g4'`).Scan(&hold, &spent))
	if hold != 2000 || spent != 0 || count(t, f.db, `SELECT count(*) FROM llm_result_consumers WHERE state='consumed'`) != 1 {
		t.Fatal("result consumption changed unknown billing", hold, spent)
	}

}

func TestGenerationReviewSaturatedProviderParksAfterBound(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	_, err := f.db.Exec(`INSERT INTO platform_llm_limits(limit_key,capacity,active_count,rate_limit,window_seconds,window_start) VALUES('g4-limit',2,2,100,60,now())`)
	must(t, err)
	for range 6 {
		_, err = f.db.Exec(`UPDATE creative_generation_bindings SET next_step_at=clock_timestamp()-interval '1 second',lease_until=NULL`)
		must(t, err)
		err = f.svc.Work(t.Context(), f.scope, jobs.Request{Payload: creativegraph.Canonical(f.target)})
		t.Logf("work result: %v", err)
	}
	var failures int
	var parked bool
	must(t, f.db.QueryRow(`SELECT failures,verification_required FROM creative_generation_bindings`).Scan(&failures, &parked))
	t.Logf("failures=%d parked=%v calls=%d", failures, parked, f.a.calls.Load())
	if !parked {
		t.Fatal("driver cannot durably park nonterminal request")
	}
	_, err = f.db.Exec(`UPDATE platform_llm_limits SET active_count=0 WHERE limit_key='g4-limit'`)
	must(t, err)
	var request string
	must(t, f.db.QueryRow(`SELECT request_id FROM creative_generation_bindings`).Scan(&request))
	_, err = f.svc.GatewayRuntime().AdvanceGeneration(t.Context(), f.scope, request)
	must(t, err)
	var hold int64
	must(t, f.db.QueryRow(`SELECT remaining_hold_micros FROM llm_usage_reservations`).Scan(&hold))
	if hold != 0 || f.a.calls.Load() != 0 {
		t.Fatal("parked undispatched generation retained permit", hold, f.a.calls.Load())
	}

}

func TestMainVersionPinnedSurvivesNewGenerationAndCanFollowLatest(t *testing.T) {
	f := setup(t, gw.GenerationText)
	f.admit(t)
	for range 4 {
		f.step(t)
	}
	readVersions := func() creativecanvas.VersionPage {
		v, err := creativecanvas.ListNodeVersions(t.Context(), f.scope, f.target.CanvasID, f.node)
		must(t, err)
		return v
	}
	versions := readVersions()
	if versions.SelectedVersionID == nil || *versions.SelectedVersionID != versions.Items[0].ID {
		t.Fatal("automatic main did not select last output")
	}
	selectMain := func(id string, follow bool) error {
		canvas, err := creativecanvas.GetCanvas(t.Context(), f.scope, f.target.CanvasID)
		if err != nil {
			return err
		}
		_, err = creativecanvas.ApplyCommands(t.Context(), f.scope, cmd(creativecanvas.BatchInput{CanvasID: canvas.ID, ExpectedTopologyRevision: canvas.TopologyRevision, ReadSet: creativecanvas.ReadSetFor(canvas), Actions: []creativecanvas.Action{{Type: "select_version", NodeID: f.node, VersionID: id, FollowLatest: follow}}}))
		return err
	}
	pinned := versions.Items[len(versions.Items)-1].ID
	must(t, selectMain(pinned, false))
	runAgain := func() {
		c, err := creativecanvas.GetCanvas(t.Context(), f.scope, f.target.CanvasID)
		must(t, err)
		for _, n := range c.Nodes {
			if n.ID == f.node {
				f.command = cmd(creativecanvas.RequestExecutionInput{CanvasID: c.ID, NodeID: n.ID, ExpectedDataRevision: n.DataRevision, DraftRevision: n.Prompt.Revision, ActionKey: creativecanvas.GenerationAction, ReadSet: creativecanvas.ReadSetFor(c)})
			}
		}
		f.admit(t)
		for range 4 {
			f.step(t)
		}
	}
	runAgain()
	versions = readVersions()
	if len(versions.Items) != 4 || versions.SelectedVersionID == nil || *versions.SelectedVersionID != pinned {
		t.Fatalf("new generation overwrote pinned main: %+v", versions)
	}
	if err := selectMain(pinned, true); !errors.Is(err, creativecanvas.ErrVersionConflict) {
		t.Fatalf("stale follow target: %v", err)
	}
	must(t, selectMain(versions.Items[0].ID, true))
	runAgain()
	versions = readVersions()
	if len(versions.Items) != 6 || versions.SelectedVersionID == nil || *versions.SelectedVersionID != versions.Items[0].ID {
		t.Fatal("follow latest did not advance")
	}
}
