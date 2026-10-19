package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const testPillarCode = `export const params = { height: { type: 'number', default: 1, min: 0.5, max: 4 } }
export default function build({ params, THREE, lib }) { return new THREE.Group() }`

var testPillarModule = json.RawMessage(`{"params_schema":{"height":{"type":"number","default":1,"min":0.5,"max":4,"label":"高度"}},"params":{"height":2},"mount":"floor"}`)

func testManifest(t *testing.T) creativeglb.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "creativeglb", "testdata", "basic.glb"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := creativeglb.Inspect(raw, creativeglb.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// stageUpload leaves a staged upload as the media worker would: a model in its
// staged form, or an image.
func (f *fixture) stageUpload(kind string) string {
	f.t.Helper()
	blob, upload := "ccbl_"+uuid.NewString(), "ccup_"+uuid.NewString()
	mime := map[string]string{"model": "model/gltf-binary", "image": "image/png"}[kind]
	if _, err := f.db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,object_key,object_version,sha256,byte_size,mime,state,verified_at)
 VALUES($1,$2,'local','creative-v2/'||$2||'/blobs/'||$1,'v1','sha256-'||repeat('1',64),100,$3,'ready',clock_timestamp())`, blob, f.alice.AccountID(), mime); err != nil {
		f.t.Fatal(err)
	}
	draft := creativecontent.Draft{Kind: kind}
	if kind == "model" {
		payload, err := json.Marshal(map[string]any{"format": "glb", "manifest": testManifest(f.t)})
		if err != nil {
			f.t.Fatal(err)
		}
		draft.Payload.Model = payload
	}
	if err := f.alice.WithTxScope(f.t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.WriteMediaAndRetain(f.t.Context(), tx, draft, []creativecontent.ObjectBinding{{Role: "original", BlobID: blob}}, func(r creativecontent.Revision) error {
			return tx.Insert(f.t.Context(), "creative_upload_candidates", []string{"id", "upload_id", "content_id", "content_revision_id", "state", "reason", "target_snapshot", "original_name", "declared_kind", "expires_at"},
				"ccuc_"+uuid.NewString(), upload, r.ContentID, r.ID, "pending", "staged", json.RawMessage(`{"kind":"staged"}`), "m", kind, "2100-01-01T00:00:00Z")
		})
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return upload
}

// hostCompletes is a render host taking the next job and finishing it: a bake
// with staged uploads, a screenshot of the current revision, or the failure
// it is given.
func (f *fixture) hostCompletes(failure ...string) creativescene.CompleteResponse {
	f.t.Helper()
	claimed, err := creativescene.Claim(f.t.Context(), f.alice, creativescene.ClaimInput{EditorSessionID: "tab_0123456789abcdef"})
	if err != nil || claimed == nil {
		f.t.Fatalf("claim %+v %v", claimed, err)
	}
	in := map[string]any{"job_id": claimed.ID, "claim_token": claimed.ClaimToken, "outcome": "succeeded"}
	switch {
	case len(failure) == 2:
		in["outcome"], in["error"] = "failed", map[string]any{"code": failure[0], "message": failure[1]}
	case claimed.Kind == string(creativescene.KindBakeModel):
		in["bake"] = map[string]any{"glb_upload_id": f.stageUpload("model"), "thumbnail_upload_id": f.stageUpload("image"), "manifest": testManifest(f.t), "module": testPillarModule}
	default:
		var revision int64
		if err := f.db.QueryRow(`SELECT revision FROM creative_documents WHERE id=$1`, claimed.DocumentID).Scan(&revision); err != nil {
			f.t.Fatal(err)
		}
		in["render"] = map[string]any{"image_upload_id": f.stageUpload("image"), "scene_revision": strconv.FormatInt(revision, 10)}
	}
	cmd := command(f.t, in)
	receipt, err := creativescene.Complete(f.t.Context(), f.alice, cmd)
	if err != nil {
		f.t.Fatalf("complete %s: %v", claimed.Kind, err)
	}
	var out creativescene.CompleteResponse
	if err := json.Unmarshal(receipt.Outcome.Response, &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// visualScene is a fixture whose model sees images, with the scene waiter
// registered, a conversation whose consent lets the canvas and images leave,
// and an empty scene.
func visualScene(t *testing.T, imageInput bool) (*fixture, Conversation, Consent, string) {
	t.Helper()
	f := setup(t)
	model, err := f.service.models.Model(testModelKey)
	if err != nil {
		t.Fatal(err)
	}
	model.Capability.ImageInput = imageInput
	models, err := llmgateway.NewCatalog("scene-test", model.Price.Currency, []llmgateway.ModelConfig{model})
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.New(llmgateway.Config{Catalog: models, Providers: map[llmgateway.ProviderKey]llmgateway.Provider{llmgateway.ProviderOpenAICompatible: f.vendor},
		Credential: func(string) (string, bool) { return "test", true }, DefaultBudget: llmgateway.BudgetPolicy{MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if f.service, err = NewService(gateway, models, f.skills); err != nil {
		t.Fatal(err)
	}
	f.service.SetImageReader(testImageReader{})
	creativescene.SetJobWaiterForTest(t, f.service.SceneJobWaiter())
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	classes := []string{"text", "canvas_metadata"}
	if imageInput {
		classes = append(classes, "image_preview")
	}
	receipt, err := f.service.GrantConsent(t.Context(), f.alice, command(t, map[string]any{"conversation_id": c.ID, "purpose": "creative_assistance", "mode": ScopeAccountLibrary, "data_classes": classes}))
	if err != nil {
		t.Fatal(err)
	}
	var consent Consent
	if err := json.Unmarshal(receipt.Outcome.Response, &consent); err != nil {
		t.Fatal(err)
	}
	return f, c, consent, f.sceneDocument()
}

func sceneCall(id, name string, args map[string]any) llmgateway.ToolCall {
	raw, _ := json.Marshal(args)
	return llmgateway.ToolCall{ID: id, Name: name, Arguments: raw}
}

// lastTool is the newest tool result the request carries, decoded.
func lastTool(r llmgateway.ProviderRequest) map[string]any {
	messages := r.Chat.Messages
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != llmgateway.RoleTool {
			continue
		}
		for _, b := range messages[i].Blocks {
			if b.Kind == llmgateway.BlockText {
				var out map[string]any
				_ = json.Unmarshal([]byte(b.Text), &out)
				return out
			}
		}
	}
	return nil
}

func hasImage(r llmgateway.ProviderRequest) (bool, string) {
	for _, m := range r.Chat.Messages {
		for i, b := range m.Blocks {
			if b.Image != nil && i > 0 {
				return true, m.Blocks[i-1].Text
			}
		}
	}
	return false, ""
}

func toolNames(r llmgateway.ProviderRequest) []string {
	var out []string
	for _, t := range r.Chat.Tools {
		out = append(out, t.Name)
	}
	return out
}

func identityAt(x float64) map[string]any {
	return map[string]any{"position": []float64{x, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}}
}

// script answers model call n with the turn the test wants, and keeps what
// each request carried.
type script struct {
	mu    sync.Mutex
	seen  []llmgateway.ProviderRequest
	turns []func(r llmgateway.ProviderRequest) llmgateway.Result
}

func (s *script) answer(_ context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
	s.mu.Lock()
	s.seen = append(s.seen, r)
	s.mu.Unlock()
	if n > len(s.turns) {
		return scriptedResult(r), nil
	}
	return s.turns[n-1](r), nil
}

func (s *script) request(n int) llmgateway.ProviderRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[n-1]
}

func (f *fixture) objectsOf(doc string) []creativescene.Object {
	f.t.Helper()
	var raw []byte
	if err := f.db.QueryRow(`SELECT r.payload->'scene'->'objects' FROM creative_documents d JOIN creative_content_revisions r ON r.id=d.content_revision_id AND r.account_id=d.account_id WHERE d.id=$1`, doc).Scan(&raw); err != nil {
		f.t.Fatal(err)
	}
	var out []creativescene.Object
	if err := json.Unmarshal(raw, &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// AC-1 and AC-3: the assistant reads the scene, bakes a model on the page,
// moves it once written, and looks at a screenshot in its next request.
func TestTheAssistantBuildsASceneAndLooksAtIt(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	var placed string
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["revision"],
				"target": map[string]any{"add": map[string]any{"name": "石柱", "transform": identityAt(0)}}, "code": testPillarCode, "params": map[string]any{"height": 3}, "name_hint": "哥特式石柱"}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			placed = lastTool(r)["object_ids"].([]any)[0].(string)
			return scriptedResult(r, sceneCall("c", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": 1,
				"operations": []any{map[string]any{"op": "update_objects", "updates": []any{map[string]any{"object_id": placed, "transform": identityAt(4)}}}}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("d", renderViewTool, map[string]any{"document_id": doc, "preset": "overview"}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "搭一根石柱"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after write_model: %s %s", got.State, got.ErrorCode)
	}
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("bake %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after render_view: %s %s", got.State, got.ErrorCode)
	}
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("render %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("finished %s %s", got.State, got.ErrorCode)
	}

	baked := lastTool(s.request(3))
	if baked["status"] != "succeeded" || baked["triangles"] == nil || baked["size_m"] == nil || baked["written_revision"] == nil {
		t.Fatalf("write_model result %v", baked)
	}
	shot := lastTool(s.request(5))
	if shot["image_revision_id"] == nil || shot["scene_revision"] == nil {
		t.Fatalf("render_view result %v", shot)
	}
	if ok, label := hasImage(s.request(5)); !ok || !strings.Contains(label, "render_view 截图") {
		t.Fatalf("screenshot not in the next request (label %q)", label)
	}
	var moved bool
	for _, o := range f.objectsOf(doc) {
		if o.ID == placed {
			moved = o.Kind == creativescene.KindModelInstance && o.Transform.Position[0] == 4
		}
	}
	if !moved {
		t.Fatalf("model %s not placed and moved: %+v", placed, f.objectsOf(doc))
	}
	if n := f.count("creative_document_revisions", "document_id=$1 AND run_id=$2 AND actor_kind='agent'", doc, run.ID); n != 2 {
		t.Fatalf("revisions written by the run: %d", n)
	}

	// AC-3: a later run whose screenshot can no longer be read is told so in
	// words and carries on.
	f.service.SetImageReader(&unreadableImages{})
	f.vendor.script = s.answer
	next, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "再看看"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(next.ID); got.State != RunSucceeded {
		t.Fatalf("later run %s %s", got.State, got.ErrorCode)
	}
	last := s.request(len(s.seen))
	if ok, _ := hasImage(last); ok || !strings.Contains(f.vendor.sentText(len(s.seen)-1), "这张图片已无法读取") {
		t.Fatalf("reclaimed screenshot:\n%s", f.vendor.sentText(len(s.seen)-1))
	}
}

// AC-2: a compile error comes back as the call's failure with the page's
// message, nothing is written, and the assistant tries again.
func TestACompileErrorIsTheAssistantsToFix(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	add := func(id, code string) func(llmgateway.ProviderRequest) llmgateway.Result {
		return func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall(id, writeModelTool, map[string]any{"document_id": doc, "based_on_revision": 1,
				"target": map[string]any{"add": map[string]any{"name": "柱", "transform": identityAt(0)}}, "code": code}))
		}
	}
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{add("a", "export default 1 +"), add("b", testPillarCode)}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "搭一根柱子"})
	if err != nil {
		t.Fatal(err)
	}
	f.resume(run.ID)
	if resp := f.hostCompletes("compile_error", "第 1 行：Unexpected end of input"); resp.State != creativescene.JobFailed {
		t.Fatalf("failed bake %+v", resp)
	}
	if n := f.count("creative_document_revisions", "document_id=$1 AND run_id=$2", doc, run.ID); n != 0 {
		t.Fatalf("a failed bake wrote %d revisions", n)
	}
	f.resume(run.ID)
	failed := lastTool(s.request(2))
	if failed["status"] != "failed" || !strings.Contains(failed["error"].(map[string]any)["message"].(string), "第 1 行") || failed["error"].(map[string]any)["code"] != "compile_error" {
		t.Fatalf("compile error result %v", failed)
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM creative_agent_steps WHERE run_id=$1 AND kind='tool' ORDER BY ordinal LIMIT 1`, run.ID).Scan(&state); err != nil || state != "failed" {
		t.Fatalf("failed call's step %s %v", state, err)
	}
	f.hostCompletes()
	if got := f.resume(run.ID); got.State != RunSucceeded || lastTool(s.request(3))["status"] != "succeeded" {
		t.Fatalf("repaired run %s, result %v", got.State, lastTool(s.request(3)))
	}
}

// B1 at the tool: a write based on an old read conflicts with what the
// photographer did since, even after another write of the run succeeded.
func TestEditSceneConflictsWithAMoveItNeverSaw(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	p, q := sceneObjectID("pillarP"), sceneObjectID("pillarQ")
	f.addPillar(creativedocument.Photographer, doc, p, "柱 P", 10)
	move := func(id string, x float64, base any) map[string]any {
		return map[string]any{"document_id": doc, "based_on_revision": base,
			"operations": []any{map[string]any{"op": "update_objects", "updates": []any{map[string]any{"object_id": id, "transform": identityAt(x)}}}}}
	}
	var read any
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			read = lastTool(r)["revision"]
			f.movePillar(creativedocument.Photographer, doc, p, 3)
			return scriptedResult(r, sceneCall("b", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": read,
				"operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{"id": q, "kind": "primitive", "name": "柱 Q", "parent_id": nil, "order": 11,
					"transform": identityAt(1), "visible": true, "locked": false, "primitive": map[string]any{"builtin": "box@1"}}}}}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("c", editSceneTool, move(p, 9, read)))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("d", editSceneTool, move(p, 9, lastTool(r)["revision"])))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "把柱 P 挪到 9 米"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	if added := lastTool(s.request(3)); added["written_revision"] == nil {
		t.Fatalf("add Q %v", added)
	}
	conflict := lastTool(s.request(4))
	entries, _ := conflict["conflicts"].([]any)
	if conflict["status"] != "conflict" || len(entries) != 1 || entries[0].(map[string]any)["changed_by"] != "创作者" || conflict["changes_since"] == nil || conflict["revision"] == nil {
		t.Fatalf("conflict %v", conflict)
	}
	var states []string
	rows, err := f.db.Query(`SELECT state FROM creative_agent_steps WHERE run_id=$1 AND kind='tool' ORDER BY ordinal`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var st string
		if err := rows.Scan(&st); err != nil {
			t.Fatal(err)
		}
		states = append(states, st)
	}
	_ = rows.Close()
	if !slices.Equal(states, []string{"succeeded", "succeeded", "failed", "succeeded"}) {
		t.Fatalf("steps %v", states)
	}
	for _, o := range f.objectsOf(doc) {
		if o.ID == p && o.Transform.Position[0] != 9 {
			t.Fatalf("P at %v", o.Transform.Position)
		}
	}
}

// AC-7: a bake that conflicted is written by retry_job_id without baking
// again; two retries of it in one turn write once.
func TestRetryJobIDWritesTheConflictedBakeOnce(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	var placed string
	var retryJob, head any
	retry := func(id string) llmgateway.ToolCall {
		return sceneCall(id, writeModelTool, map[string]any{"document_id": doc, "based_on_revision": head, "retry_job_id": retryJob,
			"target": map[string]any{"replace": map[string]any{"object_id": placed}}})
	}
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": 1,
				"target": map[string]any{"add": map[string]any{"name": "柱", "transform": identityAt(0)}}, "code": testPillarCode}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			placed = lastTool(r)["object_ids"].([]any)[0].(string)
			return scriptedResult(r, sceneCall("b", writeModelTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["written_revision"],
				"target": map[string]any{"replace": map[string]any{"object_id": placed}}, "code": testPillarCode, "params": map[string]any{"height": 4}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			conflict := lastTool(r)
			retryJob, head = conflict["retry_job_id"], conflict["revision"]
			return scriptedResult(r, retry("c"), retry("d"))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "搭一根柱子再加高"})
	if err != nil {
		t.Fatal(err)
	}
	f.resume(run.ID)
	f.hostCompletes()
	f.resume(run.ID)
	// While it bakes, the photographer gives the instance a material.
	f.writeScene(creativedocument.Photographer, doc, []string{"object:" + placed + ":content"}, map[string]any{"op": "update_objects",
		"updates": []any{map[string]any{"object_id": placed, "material_slots": map[string]any{"body": map[string]any{"preset": "stone@1", "params": map[string]any{}}}}}})
	if resp := f.hostCompletes(); resp.State != creativescene.JobFailed {
		t.Fatalf("conflicting bake %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	conflict := lastTool(s.request(3))
	if conflict["status"] != "conflict" || conflict["retry_job_id"] == nil || conflict["changes_since"] == nil {
		t.Fatalf("bake conflict %v", conflict)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1", run.ID); n != 2 {
		t.Fatalf("a retry made a job: %d jobs", n)
	}
	var results []string
	rows, err := f.db.Query(`SELECT state, output->>'result' FROM creative_agent_steps WHERE run_id=$1 AND kind='tool' ORDER BY ordinal OFFSET 2`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var st, out string
		if err := rows.Scan(&st, &out); err != nil {
			t.Fatal(err)
		}
		results = append(results, st+" "+out)
	}
	_ = rows.Close()
	slices.Sort(results)
	if len(results) != 2 || !strings.HasPrefix(results[0], "failed") || !strings.Contains(results[0], "已被使用") || !strings.HasPrefix(results[1], "succeeded") {
		t.Fatalf("two retries: %v", results)
	}
}

// render_view is offered only to a model that can see images.
func TestRenderViewNeedsAModelThatSeesImages(t *testing.T) {
	for _, imageInput := range []bool{false, true} {
		f, c, consent, _ := visualScene(t, imageInput)
		s := &script{}
		f.vendor.script = s.answer
		run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "看看场景"})
		if err != nil {
			t.Fatal(err)
		}
		f.resume(run.ID)
		names := toolNames(s.request(1))
		if !slices.Contains(names, readSceneTool) || !slices.Contains(names, editSceneTool) || !slices.Contains(names, writeModelTool) || slices.Contains(names, renderViewTool) != imageInput {
			t.Fatalf("image input %v: tools %v", imageInput, names)
		}
	}
}

// A write recovered after it committed, before its result was recorded,
// answers from its receipt instead of writing twice.
func TestSceneWritesReplayTheirReceipt(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	f.vendor.script = (&script{}).answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "加一根柱子"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"document_id": doc, "based_on_revision": 1, "operations": []any{map[string]any{"op": "add_objects", "objects": []any{map[string]any{
		"id": sceneObjectID("replayed"), "kind": "primitive", "name": "柱", "parent_id": nil, "order": 11, "transform": identityAt(0), "visible": true, "locked": false,
		"primitive": map[string]any{"builtin": "box@1"}}}}}})
	first, err := execution.runtime.editScene(t.Context(), "step-replayed", string(args))
	if err != nil {
		t.Fatal(err)
	}
	again, err := execution.runtime.editScene(t.Context(), "step-replayed", string(args))
	if err != nil {
		t.Fatal(err)
	}
	var a, b map[string]any
	if json.Unmarshal(first.(json.RawMessage), &a) != nil || json.Unmarshal(again.(json.RawMessage), &b) != nil || fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("replay answered %s, first %s", again, first)
	}
	if n := f.count("creative_document_revisions", "document_id=$1 AND run_id=$2", doc, run.ID); n != 1 {
		t.Fatalf("revisions after a replay: %d", n)
	}
}

// Scenes are canvas metadata: without the canvas in the consent the scene
// tools say so instead of reading or writing.
func TestSceneToolsNeedTheCanvasInTheConsent(t *testing.T) {
	f, c, _, doc := visualScene(t, false)
	textOnly := f.consent(f.alice, c.ID)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, textOnly.ID, InstructionSegment{Type: "text", Text: "看看场景"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	if got := lastTool(s.request(2)); got["code"] != "creative_tool_not_authorized" {
		t.Fatalf("read without canvas consent: %v", got)
	}
}

// A scene call that does not decode is answered with the field to fix, never
// with the generation tool's words about image references (a light written
// with fields the format lacked was told it had too many images). Names from
// before v7 are such fields now.
func TestABadSceneCallSaysWhatToFix(t *testing.T) {
	f, c, consent, doc := visualScene(t, false)
	lantern := map[string]any{"id": sceneObjectID("lantern"), "kind": "light", "name": "灯笼暖光", "parent_id": nil, "order": 2, "visible": true, "locked": false,
		"transform": identityAt(0), "light": map[string]any{"type": "point", "color": "#ffaa55", "intensity": 35, "range_m": 20}}
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", readSceneTool, map[string]any{"document_id": doc}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": lastTool(r)["revision"],
				"operations": []any{map[string]any{"op": "add_objects", "objects": []any{lantern}}}}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "加一盏灯笼的暖光"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	refused := lastTool(s.request(3))
	if message, _ := refused["message"].(string); refused["code"] != "creative_tool_refused" || !strings.Contains(message, "objects[0].light 没有字段 range_m") || strings.Contains(message, "图片") {
		t.Fatalf("refusal %v", refused)
	}
}

// AC-7: the assistant reads the three.js fields, their defaults, the local
// −Z facing and merge-by-key in edit_scene, and none of the old names.
func TestEditSceneDescribesThreejsFields(t *testing.T) {
	info, err := (&sceneTool{name: editSceneTool}).Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"light 字段同 three.js", "distance", "decay（默认 2）", "angle（弧度，光锥半角，≤π/2，默认 π/3）", "penumbra", "只有 type 必填",
		"坎德拉", "局部 −Z", "near, far, label}，均可省略", "fog:{color, near, far}", "0 朝 +Z、90 朝 +X", "light、camera 按键合并", "灯光物件、lit 模式和 hdri 天空暂不显示"} {
		if !strings.Contains(info.Desc, want) {
			t.Errorf("edit_scene does not say %q", want)
		}
	}
	for _, old := range []string{"range_m", "angle_deg", "near_m", "far_m", "整体替换", "七个字段"} {
		if strings.Contains(info.Desc, old) {
			t.Errorf("edit_scene still says %q", old)
		}
	}
}

func TestSceneRefusalNeverSpeaksOfGeneration(t *testing.T) {
	var refused *toolRefusal
	if err := sceneRefusal(fmt.Errorf("%w: trailing data", creativeops.ErrValidation)); !errors.As(err, &refused) || strings.Contains(refused.message, "图片") {
		t.Fatalf("unexplained validation: %v", err)
	}
	explained := creativeops.Explain(creativeops.ErrValidation, "缺少字段 order")
	if err := sceneRefusal(explained); err != explained {
		t.Fatalf("explained validation: %v", err)
	}
	if err := sceneRefusal(errToolArguments); !errors.As(err, &refused) || !strings.Contains(refused.message, "参数不符合工具说明") {
		t.Fatalf("arguments: %v", err)
	}
}

// The assistant can start a scene on the canvas: the node's document is
// written by the run and comes back for the scene tools to use.
func TestTheAssistantCreatesASceneOnTheCanvas(t *testing.T) {
	f, c, consent, _ := visualScene(t, false)
	var doc string
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", editCanvasTool, map[string]any{"actions": []any{map[string]any{"type": "add_node", "key": "s", "node_type": "scene", "title": "哥特教堂"}}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			documents, _ := lastTool(r)["documents"].(map[string]any)
			doc, _ = documents["s"].(string)
			return scriptedResult(r, sceneCall("b", readSceneTool, map[string]any{"document_id": doc}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "新建一个场景"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	created := lastTool(s.request(2))
	if created["success"] != true || created["created"].(map[string]any)["s"] == nil || doc == "" {
		t.Fatalf("edit_canvas result %v", created)
	}
	if read := lastTool(s.request(3)); read["title"] != "哥特教堂" || read["revision"] != 1.0 {
		t.Fatalf("read_scene of the new scene %v", read)
	}
	var actor, runID string
	if err := f.db.QueryRow(`SELECT actor_kind, run_id FROM creative_document_revisions WHERE document_id=$1 AND revision=1`, doc).Scan(&actor, &runID); err != nil || actor != "agent" || runID != run.ID {
		t.Fatalf("first revision written by %s %s %v", actor, runID, err)
	}
	if n := f.count("creative_nodes", "canvas_id=$1 AND type_key='core.scene' AND document_id=$2", f.canvasID, doc); n != 1 {
		t.Fatalf("scene nodes %d", n)
	}
}
