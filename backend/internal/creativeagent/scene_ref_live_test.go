package creativeagent

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Explicit opt-in only, like the other live acceptances: these spend chat
// tokens of a real model to show that a scene reference is acted on, not only
// carried. Each case checks the scene tools' arguments and the scene itself.
func TestLiveSceneReferencesAreActedOn(t *testing.T) {
	if os.Getenv("CREATIVE_AGENT_LIVE") != "1" || strings.TrimSpace(os.Getenv("AICODEWITH_API_KEY")) == "" {
		t.Skip("explicit live acceptance not enabled")
	}
	const model = "gpt-6.1-sol"
	f := setup(t)
	catalog, err := llmgateway.DefaultCatalog(nil)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := llmgateway.Build(llmgateway.Options{Catalog: catalog, MonthlyLimitMicros: 20_000_000, MonthlyTokenLimit: 5_000_000, Timeout: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if f.service, err = NewService(gateway, catalog, f.skills); err != nil {
		t.Fatal(err)
	}
	f.queue()
	f.accountConsent(f.alice, false)

	// run sends one message in a fresh conversation and returns the scene
	// writes and the assistant's reply.
	run := func(t *testing.T, editingScene string, segments ...InstructionSegment) ([]string, string) {
		t.Helper()
		c := f.conversation(f.alice, f.canvasID)
		payload := map[string]any{
			"conversation_id": c.ID, "model_key": model,
			"instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": segments},
		}
		if editingScene != "" {
			payload["scene_context"] = SceneContext{DocumentID: editingScene, SelectedObjectIDs: []string{}}
		}
		receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, payload))
		if err != nil {
			t.Fatal(err)
		}
		var started Run
		if err := json.Unmarshal(receipt.Outcome.Response, &started); err != nil {
			t.Fatal(err)
		}
		if err := f.work(f.alice, started.ID); err != nil {
			t.Fatal(err)
		}
		if got, err := f.service.ReadRun(t.Context(), f.alice, started.ID); err != nil || got.State != RunSucceeded {
			t.Fatalf("live run state=%s error=%s err=%v", got.State, got.ErrorCode, err)
		}
		page, err := f.service.ListMessages(t.Context(), f.alice, c.ID, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		var calls []string
		var reply []string
		for _, m := range page.Items {
			if m.RunID != started.ID || m.Role != "assistant" {
				continue
			}
			for _, call := range m.Body.ToolCalls {
				if sceneWriteTool(call.Name) {
					calls = append(calls, call.Name+":"+stringArgument(call.Arguments, "document_id"))
				}
			}
			for _, block := range m.Body.Blocks {
				if block.Type == "text" {
					reply = append(reply, block.Text)
				}
			}
		}
		t.Logf("scene writes %v", calls)
		return calls, strings.Join(reply, "\n")
	}
	read := func(t *testing.T, doc string) creativescene.SceneRead {
		t.Helper()
		var scene creativescene.SceneRead
		if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			scene, err = creativescene.AgentReadInTx(t.Context(), tx, creativescene.ReadInput{DocumentID: doc})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return scene
	}
	mainMaterial := func(t *testing.T, scene creativescene.SceneRead, objectID string) creativescene.MaterialSlot {
		t.Helper()
		for _, object := range scene.Objects {
			if object.ID == objectID {
				return object.MaterialSlots["main"]
			}
		}
		t.Fatalf("scene %s is missing object %s", scene.DocumentID, objectID)
		return creativescene.MaterialSlot{}
	}
	words := func(s string) InstructionSegment { return InstructionSegment{Type: "text", Text: s} }

	t.Run("同名物件跨场景", func(t *testing.T) {
		nave, aisle := f.sceneDocument(), f.sceneDocument()
		lampA, lampB := sceneObjectID("lampA"), sceneObjectID("lampB")
		f.addNamedObjects(nave, map[string]string{lampA: "台灯"})
		f.addNamedObjects(aisle, map[string]string{lampB: "台灯"})
		before := read(t, nave).Revision
		calls, _ := run(t, "", words("直接动手：把"), sceneRef(aisle, lampB),
			words("的材质改成正红色（#ff0000）。只提交材质修改，无需截图或导出。"))
		if len(calls) == 0 || slices.ContainsFunc(calls, func(c string) bool { return !strings.HasSuffix(c, ":"+aisle) }) {
			t.Fatalf("scene writes %v must all name %s", calls, aisle)
		}
		if got := mainMaterial(t, read(t, aisle), lampB).Params["color"]; got != "#ff0000" {
			t.Fatalf("referenced lamp color = %v, want #ff0000", got)
		}
		if got := read(t, nave).Revision; got != before {
			t.Fatalf("the other scene's revision changed from %d to %d", before, got)
		}
	})

	t.Run("同场景分批", func(t *testing.T) {
		doc := f.sceneDocument()
		lamp, key, sofa, table := sceneObjectID("lamp"), sceneObjectID("key"), sceneObjectID("sofa"), sceneObjectID("table")
		f.addNamedObjects(doc, map[string]string{lamp: "台灯", key: "主光", sofa: "沙发", table: "茶几"})
		run(t, doc, words("直接动手：把"), sceneRef(doc, lamp, key), words("改成做旧金属材质（metal@1）。把"),
			sceneRef(doc, sofa, table), words("改成胡桃木材质（wood@1，色调 #5c4033）。只提交材质修改，无需截图或导出。"))
		scene := read(t, doc)
		for _, id := range []string{lamp, key} {
			if got := mainMaterial(t, scene, id).Preset; got != "metal@1" {
				t.Fatalf("object %s has preset %q, want metal@1", id, got)
			}
		}
		for _, id := range []string{sofa, table} {
			if got := mainMaterial(t, scene, id); got.Preset != "wood@1" || got.Params["color"] != "#5c4033" {
				t.Fatalf("object %s has material %+v, want wood@1 with color #5c4033", id, got)
			}
		}
	})

	t.Run("已删除的物件先询问", func(t *testing.T) {
		doc := f.sceneDocument()
		lamp := sceneObjectID("lamp")
		f.addNamedObjects(doc, map[string]string{lamp: "台灯"})
		calls, reply := run(t, "", words("把"), sceneRef(doc, lamp, sceneObjectID("gone")), words("这两个物件都改成红色。"))
		if len(calls) != 0 {
			t.Fatalf("a reference to a deleted object must be asked about before any write, got %v", calls)
		}
		if strings.TrimSpace(reply) == "" {
			t.Fatal("the assistant did not reply about the deleted object")
		}
		// Check the reply during live acceptance since the question's phrasing can vary.
		t.Logf("reply for manual confirmation check: %s", reply)
	})
}
