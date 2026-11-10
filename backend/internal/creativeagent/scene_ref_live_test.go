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

	// run sends one message in a fresh conversation and returns the scene
	// tools it called, by name, with the documents they named.
	run := func(t *testing.T, segments ...InstructionSegment) []string {
		t.Helper()
		c := f.conversation(f.alice, f.canvasID)
		consent := f.sceneConsent(c.ID)
		receipt, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{
			"conversation_id": c.ID, "model_key": model, "egress_consent_id": consent.ID,
			"instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": segments},
		}))
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
		for _, m := range page.Items {
			for _, call := range m.Body.ToolCalls {
				if m.RunID == started.ID && sceneWriteTool(call.Name) {
					calls = append(calls, call.Name+":"+stringArgument(call.Arguments, "document_id"))
				}
			}
		}
		t.Logf("scene writes %v", calls)
		return calls
	}
	names := func(t *testing.T, doc string) map[string]string {
		t.Helper()
		var ref creativescene.ReferenceContext
		if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			ref, err = creativescene.ReferenceContextInTx(t.Context(), tx, doc)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ref.Names
	}
	words := func(s string) InstructionSegment { return InstructionSegment{Type: "text", Text: s} }

	t.Run("同名物件跨场景", func(t *testing.T) {
		nave, aisle := f.sceneDocument(), f.sceneDocument()
		lampA, lampB := sceneObjectID("lampA"), sceneObjectID("lampB")
		f.addNamedObjects(nave, map[string]string{lampA: "台灯"})
		f.addNamedObjects(aisle, map[string]string{lampB: "台灯"})
		calls := run(t, words("直接动手，不用再问我：把"), sceneRef(aisle, lampB), words("改名为「红台灯」。"))
		if len(calls) == 0 || slices.ContainsFunc(calls, func(c string) bool { return !strings.HasSuffix(c, ":"+aisle) }) {
			t.Fatalf("scene writes %v must all name %s", calls, aisle)
		}
		if got := names(t, aisle)[lampB]; got != "红台灯" {
			t.Fatalf("referenced lamp named %q", got)
		}
		if got := names(t, nave)[lampA]; got != "台灯" {
			t.Fatalf("the other scene's lamp was renamed to %q", got)
		}
	})

	t.Run("同场景分批", func(t *testing.T) {
		doc := f.sceneDocument()
		lamp, key, sofa, table := sceneObjectID("lamp"), sceneObjectID("key"), sceneObjectID("sofa"), sceneObjectID("table")
		f.addNamedObjects(doc, map[string]string{lamp: "台灯", key: "主光", sofa: "沙发", table: "茶几"})
		run(t, words("直接动手，不用再问我：把"), sceneRef(doc, lamp, key), words("改名，在原名前加「金属-」；把"),
			sceneRef(doc, sofa, table), words("改名，在原名前加「胡桃木-」。"))
		got := names(t, doc)
		want := map[string]string{lamp: "金属-台灯", key: "金属-主光", sofa: "胡桃木-沙发", table: "胡桃木-茶几"}
		for id, name := range want {
			if got[id] != name {
				t.Fatalf("names %v, want %v", got, want)
			}
		}
	})

	t.Run("已删除的物件先询问", func(t *testing.T) {
		doc := f.sceneDocument()
		lamp := sceneObjectID("lamp")
		f.addNamedObjects(doc, map[string]string{lamp: "台灯"})
		calls := run(t, words("把"), sceneRef(doc, lamp, sceneObjectID("gone")), words("这两个物件都改成红色。"))
		if len(calls) != 0 {
			t.Fatalf("a reference to a deleted object must be asked about before any write, got %v", calls)
		}
	})
}
