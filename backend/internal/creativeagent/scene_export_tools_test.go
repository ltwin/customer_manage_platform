package creativeagent

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// placeOnCanvas puts an existing scene on the fixture's canvas.
func (f *fixture) placeOnCanvas(doc string) string {
	f.t.Helper()
	c, err := creativecanvas.GetCanvas(f.t.Context(), f.alice, f.canvasID)
	if err != nil {
		f.t.Fatal(err)
	}
	node := "cwnode_" + uuid.NewString()
	if _, err := creativecanvas.ApplyCommands(f.t.Context(), f.alice, command(f.t, creativecanvas.BatchInput{CanvasID: f.canvasID, ExpectedTopologyRevision: c.TopologyRevision,
		ReadSet: creativecanvas.ReadSetFor(c), Actions: []creativecanvas.Action{{Type: "add_node", NodeID: node, TypeKey: creativescene.NodeTypeKey, DocumentID: doc}}})); err != nil {
		f.t.Fatal(err)
	}
	return node
}

// AC-6, AC-7, AC-13, AC-14: the assistant aims a camera with look_at, sets
// its depth of field on an empty, looks through it with depth of field, and
// exports; the images land beside the scene node and the result names them.
func TestTheAssistantAimsLooksAndExports(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	f.placeOnCanvas(doc)
	cam, empty := "sobj_CAM000000000000000001", "sobj_EMP000000000000000001"
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": 1, "operations": []any{
				map[string]any{"op": "add_objects", "objects": []any{
					map[string]any{"id": empty, "kind": "group", "name": "空物体 1", "parent_id": nil, "order": 1, "visible": true, "locked": false,
						"transform": map[string]any{"position": []float64{0, 1, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}}},
					map[string]any{"id": cam, "kind": "camera", "name": "摄像机 1", "parent_id": nil, "order": 2, "visible": true, "locked": false,
						"transform": map[string]any{"position": []float64{0, 1, 5}}, "look_at": []float64{0, 1, 0},
						"camera": map[string]any{"focal_mm": 50, "depth_of_field": true, "f_stop": 1.4, "focus_object_id": empty}},
				}}}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", renderViewTool, map[string]any{"document_id": doc, "camera_object_id": cam, "depth_of_field": true}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("c", exportShotsTool, map[string]any{"document_id": doc}))
		},
		// Before repainting, the canvas tells which image was shot how.
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, llmgateway.ToolCall{ID: "d", Name: readCanvasTool, Arguments: json.RawMessage(`{"offset":0,"limit":50}`)})
		},
	}}
	f.vendor.script = s.answer
	creativecanvas.SetExportFreshness(creativescene.Freshness{})
	t.Cleanup(func() { creativecanvas.SetExportFreshness(nil) })
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "摆一台机位，确认虚化后导出"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after render_view: %s %s", got.State, got.ErrorCode)
	}
	if edited := lastTool(s.request(2)); edited["written_revision"] == nil {
		t.Fatalf("edit_scene result %v", edited)
	}
	var render creativescene.RenderRequest
	var raw []byte
	if err := f.db.QueryRow(`SELECT request FROM creative_scene_jobs WHERE run_id=$1 AND kind='render_view'`, run.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &render); err != nil || !render.DepthOfField || render.CameraObjectID != cam {
		t.Fatalf("render request %s", raw)
	}
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("render %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after export_shots: %s %s", got.State, got.ErrorCode)
	}
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("export %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("finished %s %s", got.State, got.ErrorCode)
	}

	// look_at turned the camera's −Z towards the empty: a camera straight
	// ahead of its target needs no rotation.
	for _, o := range f.objectsOf(doc) {
		if o.ID == cam {
			q := o.Transform.Rotation
			if math.Abs(q[3]) < 0.999999 || *o.Camera.FocusObjectID != empty || !*o.Camera.DepthOfField || *o.Camera.FStop != 1.4 {
				t.Fatalf("camera %+v %+v", o.Transform, *o.Camera)
			}
		}
	}
	exported := lastTool(s.request(4))
	nodes, _ := exported["nodes"].([]any)
	if exported["status"] != "succeeded" || exported["change_id"] == nil || len(nodes) != 2 {
		t.Fatalf("export_shots result %v", exported)
	}
	// The camera stands 5 m in front of the empty it focuses on.
	lens := map[string]any{"focal_mm": 50.0, "depth_of_field": true, "f_stop": 1.4, "focus_m": 5.0, "focus_object_name": "空物体 1"}
	for _, n := range nodes {
		if got := n.(map[string]any)["lens"]; !reflect.DeepEqual(got, lens) {
			t.Fatalf("export_shots lens %v", got)
		}
	}
	read := lastTool(s.request(5))
	sources := 0
	for _, n := range read["nodes"].([]any) {
		src, ok := n.(map[string]any)["export_source"].(map[string]any)
		if !ok {
			continue
		}
		sources++
		if src["camera_object_id"] != cam || src["scene_document_id"] != doc || src["state"] != "current" || !reflect.DeepEqual(src["lens"], lens) {
			t.Fatalf("export_source %v", src)
		}
	}
	if sources != 2 {
		t.Fatalf("read_canvas shows %d export sources: %v", sources, read)
	}
	if ok, _ := hasImage(s.request(4)); !ok {
		t.Fatal("the depth of field screenshot is not in the request after render_view")
	}
	canvas, err := creativecanvas.GetCanvas(t.Context(), f.alice, f.canvasID)
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	for _, n := range canvas.Nodes {
		if n.TypeKey == "core.image" {
			images++
		}
	}
	if images != 2 {
		t.Fatalf("canvas has %d images", images)
	}
}

// AC-R2-7: 「换成实时 GI，按 4K 导出」 — the assistant writes engine and
// resolution with set_render, looks at the real-time GI with render_view and
// exports; the page draws both from the revision that carries them
// (frontend/scripts/creative-scene-agent.e2e.mjs checks the pictures).
func TestTheAssistantSwitchesToRealtimeGIAndExportsIn4K(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	f.placeOnCanvas(doc)
	cam := "sobj_CAM000000000000000001"
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": 1, "operations": []any{
				map[string]any{"op": "add_objects", "objects": []any{
					map[string]any{"id": cam, "kind": "camera", "name": "摄像机 1", "parent_id": nil, "order": 1, "visible": true, "locked": false,
						"transform": map[string]any{"position": []float64{0, 1, 5}}, "look_at": []float64{0, 1, 0}, "camera": map[string]any{}},
				}},
				map[string]any{"op": "set_render", "render": map[string]any{"engine": "realtime_gi", "resolution": "4k"}},
			}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", renderViewTool, map[string]any{"document_id": doc, "camera_object_id": cam, "mode": "lit"}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("c", exportShotsTool, map[string]any{"document_id": doc, "passes": []string{"clay", "line", "lit"}}))
		},
	}}
	f.vendor.script = s.answer
	creativecanvas.SetExportFreshness(creativescene.Freshness{})
	t.Cleanup(func() { creativecanvas.SetExportFreshness(nil) })
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "换成实时 GI，按 4K 导出"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after render_view: %s %s", got.State, got.ErrorCode)
	}
	edited := lastTool(s.request(2))
	if edited["written_revision"] == nil {
		t.Fatalf("edit_scene result %v", edited)
	}
	var raw []byte
	if err := f.db.QueryRow(`SELECT r.payload->'scene'->'render' FROM creative_documents d JOIN creative_content_revisions r ON r.id=d.content_revision_id AND r.account_id=d.account_id WHERE d.id=$1`, doc).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var render creativescene.Render
	if err := json.Unmarshal(raw, &render); err != nil {
		t.Fatal(err)
	}
	// set_render merges by key: the mode and the exposure stay as they were.
	if render != (creativescene.Render{Mode: "clay", ExposureEV: 0, Engine: creativescene.EngineRealtimeGI, Resolution: "4k", ExposureEV100: 15, ExposureMode: creativescene.ExposureAmbient}) {
		t.Fatalf("render %+v", render)
	}
	var request creativescene.RenderRequest
	if err := f.db.QueryRow(`SELECT request FROM creative_scene_jobs WHERE run_id=$1 AND kind='render_view'`, run.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.Mode != "lit" || request.CameraObjectID != cam {
		t.Fatalf("render request %s", raw)
	}
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("render %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunWaitingClient {
		t.Fatalf("after export_shots: %s %s", got.State, got.ErrorCode)
	}
	if ok, _ := hasImage(s.request(3)); !ok {
		t.Fatal("the real-time GI screenshot is not in the request after render_view")
	}
	if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
		t.Fatalf("export %+v", resp)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("finished %s %s", got.State, got.ErrorCode)
	}
	exported := lastTool(s.request(4))
	if nodes, _ := exported["nodes"].([]any); exported["status"] != "succeeded" || len(nodes) != 3 {
		t.Fatalf("export_shots result %v", exported)
	}
}

// A scene that is not on the run's canvas is refused before any job.
func TestExportShotsNeedsTheSceneOnTheCanvas(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", exportShotsTool, map[string]any{"document_id": doc}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "导出"})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("finished %s %s", got.State, got.ErrorCode)
	}
	if refused := lastTool(s.request(2)); refused["code"] != "creative_tool_refused" {
		t.Fatalf("result %v", refused)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1", run.ID); n != 0 {
		t.Fatalf("%d jobs", n)
	}
}

// A lit screenshot under a physical sky says how it was exposed, so the
// assistant can tell a viewpoint's brightness from a camera's (owner
// 2026-10-23): here a manual camera at f/8, 1/125 s, ISO 100.
func TestRenderViewReportsTheExposure(t *testing.T) {
	f, c, consent, doc := visualScene(t, true)
	cam := "sobj_CAM000000000000000001"
	s := &script{turns: []func(llmgateway.ProviderRequest) llmgateway.Result{
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("a", editSceneTool, map[string]any{"document_id": doc, "based_on_revision": 1, "operations": []any{
				map[string]any{"op": "set_environment", "environment": map[string]any{"sky": map[string]any{"kind": "physical", "date": "2026-10-01"}, "ambient": map[string]any{"color": "#ffffff", "intensity": 0.4}, "fog": nil}},
				map[string]any{"op": "add_objects", "objects": []any{
					map[string]any{"id": cam, "kind": "camera", "name": "摄像机 1", "parent_id": nil, "order": 1, "visible": true, "locked": false,
						"transform": map[string]any{"position": []float64{0, 1, 5}}, "look_at": []float64{0, 1, 0},
						"camera": map[string]any{"exposure_mode": "manual", "f_stop": 8, "shutter_s": 1.0 / 125, "iso": 100}},
				}},
			}}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("b", renderViewTool, map[string]any{"document_id": doc, "camera_object_id": cam, "mode": "lit"}))
		},
		func(r llmgateway.ProviderRequest) llmgateway.Result {
			return scriptedResult(r, sceneCall("c", renderViewTool, map[string]any{"document_id": doc, "preset": "overview", "mode": "lit"}))
		},
	}}
	f.vendor.script = s.answer
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "用摄像机看看傍晚的效果"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"camera", "overview"} {
		if got := f.resume(run.ID); got.State != RunWaitingClient {
			t.Fatalf("before the %s shot: %s %s", step, got.State, got.ErrorCode)
		}
		if resp := f.hostCompletes(); resp.State != creativescene.JobSucceeded {
			t.Fatalf("%s shot %+v", step, resp)
		}
	}
	if got := f.resume(run.ID); got.State != RunSucceeded {
		t.Fatalf("finished %s %s", got.State, got.ErrorCode)
	}
	if got := lastTool(s.request(3))["exposure"]; !reflect.DeepEqual(got, map[string]any{"by": "camera", "mode": "manual", "ev100": 12.97}) {
		t.Fatalf("the camera's shot: %v", got)
	}
	if got := lastTool(s.request(4))["exposure"]; !reflect.DeepEqual(got, map[string]any{"by": "default_camera", "mode": "ambient", "compensation": 0.0}) {
		t.Fatalf("the overview: %v", got)
	}
}
