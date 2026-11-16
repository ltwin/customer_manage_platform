package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
func (f *fixture) sceneConsent(_ string) Consent {
	f.t.Helper()
	return f.accountConsent(f.alice, false)
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
	return f.startRefRun(conversationID, consentID, scene, InstructionSegment{Type: "text", Text: "把这根柱子加高"})
}

// startRefRun starts a run from the given segments, from the scene editor
// when scene is set.
func (f *fixture) startRefRun(conversationID, _ string, scene any, segments ...InstructionSegment) (Run, error) {
	f.t.Helper()
	payload := map[string]any{
		"conversation_id": conversationID,
		"instruction":     map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": segments},
		"model_key":       testModelKey,
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

func sceneRef(doc string, objects ...string) InstructionSegment {
	segment := InstructionSegment{Type: "scene_ref", DocumentID: doc}
	if len(objects) > 0 {
		segment.ObjectIDs = objects
	}
	return segment
}

// triggerBody is the photographer's message that started a run.
func (f *fixture) triggerBody(conversationID, runID string) Body {
	f.t.Helper()
	page, err := f.service.ListMessages(f.t.Context(), f.alice, conversationID, "", 50)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, m := range page.Items {
		if m.RunID == runID && m.Role == "user" {
			return m.Body
		}
	}
	f.t.Fatalf("no trigger message for %s", runID)
	return Body{}
}

// addNamedObjects adds objects named by id, in one command.
func (f *fixture) addNamedObjects(doc string, names map[string]string) {
	f.t.Helper()
	objects := make([]any, 0, len(names))
	order := 100
	for id, name := range names {
		order++
		objects = append(objects, map[string]any{
			"id": id, "kind": "primitive", "name": name, "parent_id": nil, "order": order, "visible": true, "locked": false,
			"transform": map[string]any{"position": []float64{0, 0, 0}, "rotation": []float64{0, 0, 0, 1}, "scale": []float64{1, 1, 1}},
			"primitive": map[string]any{"builtin": "box@1"}})
	}
	f.writeScene(creativedocument.Photographer, doc, []string{"objects"}, map[string]any{"op": "add_objects", "objects": objects})
}

func inOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(text[at:], p)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", p, text)
		}
		at += i + len(p)
	}
}

// Each reference is frozen where it stood in the message, with the names the
// scene had then; an object already gone is kept and marked, so the run asks
// rather than guesses. Later runs read the same snapshot, minus the advice.
func TestASceneReferenceIsFrozenWhereItStood(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	nave, aisle := f.sceneDocument(), f.sceneDocument()
	pillar, plain, gone := sceneObjectID("pillarA"), sceneObjectID("plain"), sceneObjectID("gone")
	f.addPillar(creativedocument.Photographer, nave, pillar, "柱 A", 10)
	f.addPillar(creativedocument.Photographer, nave, plain, "", 11)

	run, err := f.startRefRun(c.ID, consent.ID, nil,
		InstructionSegment{Type: "text", Text: "把"}, sceneRef(nave, pillar, gone, plain),
		InstructionSegment{Type: "text", Text: "加高，"}, sceneRef(aisle),
		InstructionSegment{Type: "text", Text: "不动"})
	if err != nil {
		t.Fatal(err)
	}
	blocks := f.triggerBody(c.ID, run.ID).Blocks
	if len(blocks) != 5 || blocks[1].Type != "scene_ref" || blocks[3].Type != "scene_ref" {
		t.Fatalf("blocks %+v", blocks)
	}
	ref := blocks[1].Scene
	want := []BlockSceneObject{{ID: pillar, Name: "柱 A"}, {ID: gone, Missing: true}, {ID: plain}}
	if ref.DocumentID != nave || ref.Title != "中殿" || ref.Revision < 1 || !slices.Equal(ref.Objects, want) {
		t.Fatalf("snapshot %+v", ref)
	}
	if whole := blocks[3].Scene; whole.DocumentID != aisle || whole.Revision < 1 || len(whole.Objects) != 0 {
		t.Fatalf("whole-scene snapshot %+v", whole)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	line := "[引用场景「中殿」(" + nave + "，第 " + strconv.FormatInt(ref.Revision, 10) + " 版)中的物件：「柱 A」(" + pillar + ")、" + plain + "；" + gone + " 发送时已不在场景里"
	wholeLine := "[引用场景「中殿」(" + aisle + "，第 " + strconv.FormatInt(blocks[3].Scene.Revision, 10) + " 版)]"
	sent := f.vendor.sentText(f.vendor.calls() - 1)
	inOrder(t, sent, "把", sceneRefGuide, line+"，不要猜替代对象，先向创作者确认]", "加高，", wholeLine, "不动")
	if strings.Count(sent, sceneRefGuide) != 1 {
		t.Fatalf("the guide must be said once:\n%s", sent)
	}

	// A rename later does not rewrite what the message showed.
	name := "柱 甲"
	f.writeScene(creativedocument.Photographer, nave, []string{"object:" + pillar}, map[string]any{"op": "update_objects",
		"updates": []any{map[string]any{"object_id": pillar, "name": name}}})
	if got := f.triggerBody(c.ID, run.ID).Blocks[1].Scene.Objects[0].Name; got != "柱 A" {
		t.Fatalf("snapshot renamed to %q", got)
	}
	next, err := f.startRefRun(c.ID, consent.ID, nil, InstructionSegment{Type: "text", Text: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, next.ID); err != nil {
		t.Fatal(err)
	}
	history := f.vendor.sentText(f.vendor.calls() - 1)
	if strings.Count(history, line+"]") != 1 || strings.Contains(history, "先向创作者确认") || strings.Contains(history, name) {
		t.Fatalf("history line:\n%s", history)
	}

	// A past scene reference cannot authorize a new run after the account withdraws consent.
	f.withdraw(f.alice)
	calls := f.vendor.calls()
	if _, err := f.startRefRun(c.ID, "", nil, InstructionSegment{Type: "text", Text: "继续讨论灯光"}); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("follow-up without account consent: %v", err)
	}
	if f.vendor.calls() != calls {
		t.Fatal("a follow-up exposed history after consent was withdrawn")
	}
}

func TestSceneReferencesSurviveHistoryToolsAndCompaction(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	lamp := sceneObjectID("lamp")
	f.addPillar(creativedocument.Photographer, doc, lamp, "台灯", 10)
	first, err := f.startRefRun(c.ID, consent.ID, nil,
		InstructionSegment{Type: "text", Text: "修改"}, sceneRef(doc, lamp), InstructionSegment{Type: "text", Text: "为蓝色"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, first.ID); err != nil {
		t.Fatal(err)
	}
	f.writeScene(creativedocument.Photographer, doc, []string{"object:" + lamp}, map[string]any{"op": "update_objects",
		"updates": []any{map[string]any{"object_id": lamp, "name": "新的灯名"}}})
	next, err := f.startRefRun(c.ID, consent.ID, nil, InstructionSegment{Type: "text", Text: "继续"})
	if err != nil {
		t.Fatal(err)
	}
	var ordinal int64
	if err := f.db.QueryRow("SELECT ordinal FROM creative_agent_messages WHERE id=$1", first.TriggerMessageID).Scan(&ordinal); err != nil {
		t.Fatal(err)
	}
	check := func(allowed bool) {
		t.Helper()
		h := &runtimeHandler{service: f.service, scope: f.alice, held: claim{RunID: next.ID, ConversationID: c.ID}}
		history, err := h.readHistory(t.Context(), `{"before_ordinal":0,"limit":20}`)
		if err != nil {
			t.Fatal(err)
		}
		var transcript string
		if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			chat, err := f.service.compactionRequestInTx(t.Context(), tx, c.ID, compactionPlan{CoversThrough: ordinal}, 32000)
			if err == nil {
				transcript = chat.Messages[1].Blocks[0].Text
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		for _, text := range []string{history, transcript} {
			if allowed {
				inOrder(t, text, "修改", doc, "台灯", lamp, "为蓝色")
			} else if strings.Contains(text, doc) || strings.Contains(text, lamp) {
				t.Fatalf("scene metadata left after consent was withdrawn: %s", text)
			}
			if strings.Contains(text, "新的灯名") {
				t.Fatalf("historical name was rewritten: %s", text)
			}
		}
	}
	check(true)
	f.withdraw(f.alice)
	check(false)
}

func TestEditingContextAndChangesUseTheReferenceSnapshot(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	pillar := sceneObjectID("pillarA")
	earlier := f.finishedRun(c.ID, consent.ID)
	f.addPillar(creativedocument.Actor{Kind: "agent", RunID: &earlier}, doc, pillar, "柱 A", 10)
	f.movePillar(creativedocument.Photographer, doc, pillar, 2)
	body := Body{Blocks: []Block{{Type: "scene_ref", Scene: &BlockScene{
		DocumentID: doc, Objects: []BlockSceneObject{{ID: pillar}},
	}}}}
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		snapshots, err := fillSceneRefsInTx(t.Context(), tx, body)
		if err != nil {
			return err
		}
		// Another transaction commits after the explicit reference is frozen.
		f.writeScene(creativedocument.Photographer, doc, []string{"object:" + pillar}, map[string]any{"op": "update_objects",
			"updates": []any{map[string]any{"object_id": pillar, "name": "后来改名"}}})
		inputs, err := sceneInputsInTx(t.Context(), tx, c.ID, &SceneContext{DocumentID: doc, SelectedObjectIDs: []string{pillar}}, snapshots)
		if err != nil {
			return err
		}
		ref := body.Blocks[0].Scene
		if len(inputs) != 2 || inputs[0].Snapshot["revision"] != ref.Revision || inputs[1].Snapshot["revision"] != ref.Revision ||
			!strings.Contains(inputs[0].Text, ref.Objects[0].Name) || strings.Contains(inputs[1].Text, "后来改名") {
			t.Fatalf("scene inputs do not match the reference snapshot: %+v, ref %+v", inputs, ref)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// The scene open in the editor is named once, by its own line; a reference to
// it points at it.
func TestAReferenceToTheEditedSceneIsNotNamedTwice(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	pillar := sceneObjectID("pillarA")
	f.addPillar(creativedocument.Photographer, doc, pillar, "柱 A", 10)
	run, err := f.startRefRun(c.ID, consent.ID, map[string]any{"document_id": doc, "selected_object_ids": []string{}},
		InstructionSegment{Type: "text", Text: "把"}, sceneRef(doc, pillar), InstructionSegment{Type: "text", Text: "改断"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	sent := f.vendor.sentText(f.vendor.calls() - 1)
	inOrder(t, sent, "把", "[引用当前编辑的场景中的物件：「柱 A」("+pillar+")]", "改断")
	if strings.Count(sent, "「中殿」("+doc) != 1 || strings.Contains(sent, "没有选中物件") {
		t.Fatalf("the edited scene must be named once:\n%s", sent)
	}
	if blocks := f.triggerBody(c.ID, run.ID).Blocks; len(blocks) != 3 {
		t.Fatalf("an empty selection must add no selection block: %+v", blocks)
	}
}

// What a reference says is frozen with the run: a retried attempt sends the
// same words although the object has since been deleted.
func TestSceneReferencesAreFrozenWithTheRun(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	pillar := sceneObjectID("pillarA")
	f.addPillar(creativedocument.Photographer, doc, pillar, "柱 A", 10)
	run, err := f.startRefRun(c.ID, consent.ID, nil, InstructionSegment{Type: "text", Text: "加高"}, sceneRef(doc, pillar))
	if err != nil {
		t.Fatal(err)
	}
	var kind string
	if err := f.db.QueryRow(`SELECT source_revision_snapshot->>'kind' FROM creative_run_inputs
		WHERE run_id=$1 AND input_role='instruction' AND read_level='metadata_only'`, run.ID).Scan(&kind); err != nil || kind != "scene_ref" {
		t.Fatalf("frozen reference row kind=%q err=%v", kind, err)
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
			b.WriteString(m.Content + "\n")
		}
		return b.String()
	}
	first := assembled()
	if !strings.Contains(first, "「柱 A」("+pillar+")") {
		t.Fatalf("request lacks the reference:\n%s", first)
	}
	f.writeScene(creativedocument.Photographer, doc, []string{"objects", "object:" + pillar, "object:" + pillar + ":content"}, map[string]any{"op": "remove_objects", "object_ids": []string{pillar}})
	if again := assembled(); again != first {
		t.Fatalf("second assembly differs:\n%s\n---\n%s", first, again)
	}
}

// A reference that cannot be honoured refuses the whole submission: nothing
// is created, no slot is taken and no message is written.
func TestASceneReferenceIsChecked(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	words := InstructionSegment{Type: "text", Text: "改一下"}
	if _, err := f.startRefRun(c.ID, "", nil, words, sceneRef(f.sceneDocument())); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("scene reference without account consent: %v", err)
	}
	consent := f.sceneConsent(c.ID)
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
	six := []InstructionSegment{{Type: "text", Text: "比较"}}
	for range maxSceneRefs + 1 {
		six = append(six, sceneRef(f.sceneDocument()))
	}
	for _, tc := range []struct {
		name     string
		consent  string
		segments []InstructionSegment
		want     error
	}{
		{"unknown scene", consent.ID, []InstructionSegment{words, sceneRef("cdoc_missing")}, ErrNotFound},
		{"another account's scene", consent.ID, []InstructionSegment{words, sceneRef(theirs.ID)}, ErrNotFound},
		{"scene in the trash", consent.ID, []InstructionSegment{words, sceneRef(trashed)}, ErrNotFound},
		{"six scenes", consent.ID, six, ErrLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.startRefRun(c.ID, tc.consent, nil, tc.segments...); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	// An omitted object list references the whole scene. On the wire, a
	// present field has to belong to the segment and carry a valid value.
	doc := f.sceneDocument()
	for _, segment := range []map[string]any{
		{"type": "scene_ref", "document_id": doc, "object_ids": []string{}},
		{"type": "scene_ref", "document_id": doc, "object_ids": nil},
		{"type": "scene_ref", "document_id": doc, "text": ""},
		{"type": "scene_ref", "document_id": doc, "command": ""},
		{"type": "scene_ref", "document_id": doc, "skill_id": ""},
		{"type": "scene_ref", "document_id": doc, "skill_version_id": nil},
		{"type": "scene_ref", "document_id": doc, "content_revision_id": ""},
		{"type": "text", "text": "原文", "document_id": ""},
		{"type": "text", "text": "原文", "object_ids": nil},
		{"type": "scene_ref", "document_id": doc, "unknown": ""},
	} {
		_, err := f.service.CreateRun(t.Context(), f.alice, command(t, map[string]any{
			"conversation_id": c.ID, "model_key": testModelKey,
			"instruction": map[string]any{"schema_version": instructionSchemaVersion, "instruction_segments": []any{segment}},
		}))
		if !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("malformed segment %+v: %v", segment, err)
		}
	}
	if n := f.count("creative_agent_runs", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("refused runs left %d behind", n)
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("refused runs left %d messages", n)
	}
}

// The fifty is per reference: one scene split over two references of forty
// objects each is one accepted message.
func TestOneSceneMayBeReferencedTwiceWithFortyObjectsEach(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	names := map[string]string{}
	batch := func(prefix string) []string {
		ids := make([]string, 40)
		for i := range ids {
			ids[i] = sceneObjectID(fmt.Sprintf("%s%02d", prefix, i))
			names[ids[i]] = fmt.Sprintf("%s%02d", prefix, i)
		}
		return ids
	}
	metal, walnut := batch("m"), batch("w")
	f.addNamedObjects(doc, names)
	run, err := f.startRefRun(c.ID, consent.ID, nil, sceneRef(doc, metal...), InstructionSegment{Type: "text", Text: "改成金属，"},
		sceneRef(doc, walnut...), InstructionSegment{Type: "text", Text: "改成胡桃木"})
	if err != nil {
		t.Fatal(err)
	}
	blocks := f.triggerBody(c.ID, run.ID).Blocks
	if len(blocks[0].Scene.Objects) != 40 || len(blocks[2].Scene.Objects) != 40 || blocks[2].Scene.Objects[0].Name != "w00" {
		t.Fatalf("references %+v / %+v", blocks[0].Scene, blocks[2].Scene)
	}
}

// longNames is a scene of fifty objects whose names are as long as a name
// may be, so a reference to all of them costs far more than its ids.
func (f *fixture) longNames(doc string) []string {
	f.t.Helper()
	names := map[string]string{}
	ids := make([]string, maxSceneSelection)
	for i := range ids {
		ids[i] = sceneObjectID(fmt.Sprintf("long%02d", i))
		names[ids[i]] = strconv.Itoa(i) + strings.Repeat("柱", maxSceneNameRunes-len(strconv.Itoa(i)))
	}
	f.addNamedObjects(doc, names)
	return ids
}

// The turn's budget is charged with the references as they are worded once
// their names are known, not with the bare ids the request carried.
func TestAReferenceIsChargedAsWorded(t *testing.T) {
	f := setup(t)
	f.queue()
	f.service.limits.AutoCompactPercent = 0
	f.vendor.script = compactionScript(testSummary)
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	for _, words := range []string{"第一轮：秋日人像", "第二轮：加入逆光", "第三轮：换成胶片质感"} {
		run, err := f.startRefRun(c.ID, consent.ID, nil, InstructionSegment{Type: "text", Text: words})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
	}
	doc := f.sceneDocument()
	ids := f.longNames(doc)
	segments := []InstructionSegment{{Type: "text", Text: "把"}, sceneRef(doc, ids...), {Type: "text", Text: "改成金属"}}
	placeholder, err := f.service.ResolveInstruction(t.Context(), f.alice, segments)
	if err != nil {
		t.Fatal(err)
	}
	f.service.limits.AutoCompactPercent = 1
	run, err := f.startRefRun(c.ID, consent.ID, nil, segments...)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := f.db.QueryRow(`SELECT compaction_plan FROM creative_agent_runs WHERE id=$1`, run.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var plan compactionPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	worded := bodyBytes(f.triggerBody(c.ID, run.ID))
	if plan.RequestBytes != worded || worded < placeholder.Bytes+maxSceneSelection*maxSceneNameRunes*3 {
		t.Fatalf("planned %d bytes, worded %d, placeholder %d", plan.RequestBytes, worded, placeholder.Bytes)
	}
}

// Names can make a message that fit as ids too long once worded; that is a
// limit, refused with nothing written.
func TestAReferenceThatOutgrowsTheMessageIsRefused(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.sceneConsent(c.ID)
	doc := f.sceneDocument()
	ids := f.longNames(doc)
	segments := []InstructionSegment{sceneRef(doc, ids...), sceneRef(doc, ids...), sceneRef(doc, ids...)}
	if _, err := f.service.ResolveInstruction(t.Context(), f.alice, segments); err != nil {
		t.Fatalf("as ids the message fits: %v", err)
	}
	if _, err := f.startRefRun(c.ID, consent.ID, nil, segments...); !errors.Is(err, ErrLimit) {
		t.Fatalf("got %v, want a limit", err)
	}
	if n := f.count("creative_agent_runs", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("refused run left %d behind", n)
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("refused run left %d messages", n)
	}
}
