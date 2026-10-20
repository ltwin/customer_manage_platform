package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func sceneObjectID(tag string) string { return "sobj_" + (tag + strings.Repeat("0", 21))[:21] }

// sceneConsent lets the canvas leave the account, as the panel's grant does.
func (f *fixture) sceneConsent(conversationID string) Consent {
	f.t.Helper()
	receipt, err := f.service.GrantConsent(f.t.Context(), f.alice, command(f.t, map[string]any{"conversation_id": conversationID,
		"purpose": "creative_assistance", "mode": ScopeAccountLibrary, "data_classes": []string{"text", "canvas_metadata"}}))
	if err != nil {
		f.t.Fatal(err)
	}
	var c Consent
	if err := json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		f.t.Fatal(err)
	}
	return c
}

// writeScene sends one scene command as actor, expecting the current
// revisions of the given units.
func (f *fixture) writeScene(actor creativedocument.Actor, doc string, units []string, ops ...map[string]any) {
	f.t.Helper()
	var raw []byte
	if err := f.db.QueryRow(`SELECT unit_revs FROM creative_documents WHERE id=$1`, doc).Scan(&raw); err != nil {
		f.t.Fatal(err)
	}
	var revs map[string]int64
	if err := json.Unmarshal(raw, &revs); err != nil {
		f.t.Fatal(err)
	}
	expected := map[string]any{}
	for _, u := range units {
		if r, ok := revs[u]; ok {
			expected[u] = strconv.FormatInt(r, 10)
		} else {
			expected[u] = nil
		}
	}
	if _, err := creativedocument.CommandAs(f.t.Context(), f.alice, actor, command(f.t, map[string]any{"document_id": doc, "expected": expected, "operations": ops})); err != nil {
		f.t.Fatalf("%s writes %v: %v", actor.Kind, ops, err)
	}
}

func (f *fixture) addPillar(actor creativedocument.Actor, doc, id, name string, order int) {
	f.writeScene(actor, doc, []string{"objects"}, map[string]any{"op": "add_objects", "objects": []any{map[string]any{
		"id": id, "kind": "primitive", "name": name, "parent_id": nil, "order": order, "visible": true, "locked": false,
		"transform": map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
		"primitive": map[string]any{"builtin": "box@1"}}}})
}

func (f *fixture) movePillar(actor creativedocument.Actor, doc, id string, x float64) {
	f.writeScene(actor, doc, []string{"object:" + id}, map[string]any{"op": "update_objects", "updates": []any{map[string]any{"object_id": id,
		"transform": map[string]any{"position": []float64{x, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}}}}})
}

func (f *fixture) startSceneRun(conversationID, consentID string, scene any) (Run, error) {
	f.t.Helper()
	payload := map[string]any{
		"conversation_id":   conversationID,
		"instruction":       map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []InstructionSegment{{Type: "text", Text: "把这根柱子加高"}}},
		"model_key":         testModelKey,
		"egress_consent_id": consentID,
	}
	if scene != nil {
		payload["scene_context"] = scene
	}
	receipt, err := f.service.CreateRun(f.t.Context(), f.alice, command(f.t, payload))
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
		f.t.Fatal(err)
	}
	return run, nil
}

// finishedRun is an earlier turn of the conversation, done, so its writes
// are the conversation's own.
func (f *fixture) finishedRun(conversationID, consentID string) string {
	f.t.Helper()
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, conversationID, consentID, InstructionSegment{Type: "text", Text: "搭一个中殿"})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		f.t.Fatal(err)
	}
	return run.ID
}

// sceneInputs is what a run froze about scenes, by kind and document.
func (f *fixture) sceneInputs(runID string) []string {
	f.t.Helper()
	rows, err := f.db.Query(`SELECT source_revision_snapshot->>'kind', source_revision_snapshot->>'document_id' FROM creative_run_inputs
		WHERE run_id=$1 AND input_role='selection' AND source_revision_snapshot IS NOT NULL ORDER BY ordinal`, runID)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var kind, doc string
		if err := rows.Scan(&kind, &doc); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, kind+":"+doc)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// What a run is told about scenes is worded when it is created and frozen
// with it: every attempt assembles the same request although the scene moves
// on, so a resumed run's requests hash as the recorded ones.
func TestSceneInputsAreFrozenWithTheRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	pillar := sceneObjectID("pillarA")
	earlier := f.finishedRun(c.ID, consent.ID)
	f.addPillar(creativedocument.Actor{Kind: "agent", RunID: &earlier}, doc, pillar, "柱 A", 10)
	f.movePillar(creativedocument.Photographer, doc, pillar, 2)

	run, err := f.startSceneRun(c.ID, consent.ID, map[string]any{"document_id": doc, "selected_object_ids": []string{pillar, sceneObjectID("gone"), pillar}})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.sceneInputs(run.ID); !slices.Equal(got, []string{"scene_context:" + doc, "scene_changes:" + doc}) {
		t.Fatalf("frozen scene inputs %v", got)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	assembled := func() string {
		t.Helper()
		messages, _, _, err := f.service.assemble(t.Context(), f.alice, held)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, m := range messages {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
		return b.String()
	}

	// Read afresh, both lines would now say something else.
	f.addPillar(creativedocument.Photographer, doc, sceneObjectID("pillarB"), "柱 B", 11)
	first := assembled()
	for _, want := range []string{
		"创作者正在编辑场景「中殿」(" + doc + "，第 3 版)，选中了：「柱 A」(" + pillar + ")。",
		"在本会话上次写入（第 2 版）之后被改过，现在是第 3 版",
		"创作者改了「柱 A」(" + pillar + ")的摆放",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("request lacks %q:\n%s", want, first)
		}
	}
	if strings.Contains(first, "柱 B") || strings.Contains(first, sceneObjectID("gone")) || strings.Count(first, "「柱 A」("+pillar+")。") != 1 {
		t.Fatalf("scene lines not as frozen:\n%s", first)
	}
	f.movePillar(creativedocument.Photographer, doc, pillar, 5)
	if again := assembled(); again != first {
		t.Fatalf("second assembly differs:\n%s", again)
	}

	execution, err := f.service.newRunExecution(t.Context(), f.alice, held)
	if err != nil {
		t.Fatal(err)
	}
	out, err := execution.run(t.Context())
	if err = f.service.completeAttempt(t.Context(), f.alice, held, out, err); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s", got.State)
	}
	if sent := f.vendor.sentText(f.vendor.calls() - 1); !strings.Contains(sent, "创作者正在编辑场景「中殿」") || strings.Contains(sent, "柱 B") {
		t.Fatalf("sent request:\n%s", sent)
	}
}

// The photographer's message shows the selection the run was told, as it was
// named then; one sent with nothing selected shows none.
func TestTheMessageShowsTheSelectionItAskedAbout(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	pillar, plain := sceneObjectID("pillarA"), sceneObjectID("plain")
	f.addPillar(creativedocument.Photographer, doc, pillar, "柱 A", 10)
	f.addPillar(creativedocument.Photographer, doc, plain, "", 11)

	run, err := f.startSceneRun(c.ID, consent.ID, map[string]any{"document_id": doc, "selected_object_ids": []string{pillar, sceneObjectID("gone"), plain}})
	if err != nil {
		t.Fatal(err)
	}
	trigger := func(runID string) Body {
		t.Helper()
		page, err := f.service.ListMessages(t.Context(), f.alice, c.ID, "", 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range page.Items {
			if m.RunID == runID && m.Role == "user" {
				return m.Body
			}
		}
		t.Fatalf("no trigger message for %s", runID)
		return Body{}
	}
	blocks := trigger(run.ID).Blocks
	if len(blocks) != 2 || blocks[0].Type != "text" || blocks[1].Type != "scene_selection" {
		t.Fatalf("blocks %+v", blocks)
	}
	want := BlockScene{DocumentID: doc, Title: "中殿", Objects: []BlockSceneObject{{ID: pillar, Name: "柱 A"}, {ID: plain}}}
	if got := blocks[1].Scene; got == nil || got.DocumentID != want.DocumentID || got.Title != want.Title || !slices.Equal(got.Objects, want.Objects) {
		t.Fatalf("selection %+v", blocks[1].Scene)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}

	next, err := f.startSceneRun(c.ID, consent.ID, map[string]any{"document_id": doc, "selected_object_ids": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if blocks := trigger(next.ID).Blocks; len(blocks) != 1 || blocks[0].Type != "text" {
		t.Fatalf("message without selection %+v", blocks)
	}
	// The next run hears the earlier selection as part of that message, once.
	if err := f.work(f.alice, next.ID); err != nil {
		t.Fatal(err)
	}
	line := "[发送这条消息时，创作者在场景「中殿」(" + doc + ")里选中了：「柱 A」(" + pillar + ")、" + plain + "]"
	if sent := f.vendor.sentText(f.vendor.calls() - 1); strings.Count(sent, line) != 1 {
		t.Fatalf("history lacks the selection once:\n%s", sent)
	}
}

func TestARunFromTheCanvasIsToldNothingAboutScenes(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	f.sceneDocument()
	run, err := f.startSceneRun(c.ID, consent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.sceneInputs(run.ID); len(got) != 0 {
		t.Fatalf("scene inputs %v", got)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if sent := f.vendor.sentText(0); strings.Contains(sent, "创作者正在编辑场景") || strings.Contains(sent, "被改过") {
		t.Fatalf("canvas run told about scenes:\n%s", sent)
	}
}

// The scenes are canvas metadata, told only under a consent that lets the
// canvas leave the account.
func TestSceneInputsNeedCanvasMetadataConsent(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	doc := f.sceneDocument()
	run, err := f.startSceneRun(c.ID, consent.ID, map[string]any{"document_id": doc, "selected_object_ids": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.sceneInputs(run.ID); len(got) != 0 {
		t.Fatalf("scene inputs without canvas consent %v", got)
	}
}

func TestSceneContextIsChecked(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	trashed := f.sceneDocument()
	if _, err := f.db.Exec(`UPDATE creative_documents SET trashed_at=now() WHERE id=$1`, trashed); err != nil {
		t.Fatal(err)
	}
	var theirs creativedocument.Document
	typ, _ := creativedocument.Lookup(creativescene.TypeKey)
	if err := f.bob.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		theirs, err = creativedocument.CreateInTx(t.Context(), tx, typ, "别人的场景", nil, creativedocument.Photographer)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, maxSceneSelection+1)
	for i := range tooMany {
		tooMany[i] = sceneObjectID(strconv.Itoa(i))
	}
	for _, tc := range []struct {
		name  string
		scene map[string]any
		want  error
	}{
		{"no document", map[string]any{"document_id": "", "selected_object_ids": []string{}}, creativeops.ErrValidation},
		{"too many selected", map[string]any{"document_id": doc, "selected_object_ids": tooMany}, creativeops.ErrValidation},
		{"empty object id", map[string]any{"document_id": doc, "selected_object_ids": []string{""}}, creativeops.ErrValidation},
		{"unknown document", map[string]any{"document_id": "cdoc_missing", "selected_object_ids": []string{}}, ErrNotFound},
		{"another account's scene", map[string]any{"document_id": theirs.ID, "selected_object_ids": []string{}}, ErrNotFound},
		{"scene in the trash", map[string]any{"document_id": trashed, "selected_object_ids": []string{}}, ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.startSceneRun(c.ID, consent.ID, tc.scene); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	if n := f.count("creative_agent_runs", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("refused runs left %d behind", n)
	}
}

// A run hears what changed in the scene being edited and in the three scenes
// the conversation wrote last; older ones it can still read.
func TestOnlyTheScenesWrittenLastAreSummarized(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	earlier := f.finishedRun(c.ID, consent.ID)
	agent := creativedocument.Actor{Kind: "agent", RunID: &earlier}
	docs := make([]string, 4)
	for i := range docs {
		docs[i] = f.sceneDocument()
		pillar := sceneObjectID("p" + strconv.Itoa(i))
		f.addPillar(agent, docs[i], pillar, "柱", 10)
	}
	untouched := f.sceneDocument()
	for i := range docs {
		f.movePillar(creativedocument.Photographer, docs[i], sceneObjectID("p"+strconv.Itoa(i)), 2)
	}

	run, err := f.startSceneRun(c.ID, consent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scene_changes:" + docs[3], "scene_changes:" + docs[2], "scene_changes:" + docs[1]}
	if got := f.sceneInputs(run.ID); !slices.Equal(got, want) {
		t.Fatalf("summarized %v, want %v", got, want)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}

	// Editing the oldest one (or one never written) adds it in front.
	for _, editing := range []string{docs[0], untouched} {
		run, err = f.startSceneRun(c.ID, consent.ID, map[string]any{"document_id": editing, "selected_object_ids": []string{}})
		if err != nil {
			t.Fatal(err)
		}
		want = []string{"scene_context:" + editing}
		if editing == docs[0] {
			want = append(want, "scene_changes:"+docs[0])
		}
		want = append(want, "scene_changes:"+docs[3], "scene_changes:"+docs[2], "scene_changes:"+docs[1])
		if got := f.sceneInputs(run.ID); !slices.Equal(got, want) {
			t.Fatalf("editing %s summarized %v, want %v", editing, got, want)
		}
		if err := f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
	}
}
