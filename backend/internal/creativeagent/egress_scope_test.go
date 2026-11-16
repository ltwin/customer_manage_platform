package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestTheReadScopeWithoutTheApprovalList pins what a run may read now that
// the per-message approval list is gone: the current canvas, the
// conversation's references before this run or by it, and the library when
// the consent allows it. Nothing the list used to reach falls outside, and
// nothing outside these comes in.
func TestTheReadScopeWithoutTheApprovalList(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)

	onCanvas := f.revision(f.alice, "画布上的正文")
	if _, err := f.db.Exec(`INSERT INTO creative_nodes(id,account_id,canvas_id,type_key,x,y,title,content_id,content_revision_id)
		SELECT 'ccnd_scope','agent-a','cccv_a','core.text',0,0,'主画面',content_id,id FROM creative_content_revisions WHERE id=$1`, onCanvas); err != nil {
		t.Fatal(err)
	}
	earlier := f.revision(f.alice, "之前对话里引用过的")
	appendLookupMessage(t, f, c.ID, []ContentRef{{RevisionID: earlier, Role: "input"}}, "早先的消息")
	// Once listed by a per-conversation grant that was later withdrawn. The
	// old rule refused it in this conversation for good; the owner accepted
	// dropping that rule (2026-11-13), so a reference makes it readable.
	formerlyWithdrawn := f.revision(f.alice, "曾在撤回的授权清单里")
	if _, err := f.db.Exec(`INSERT INTO creative_egress_consents(id,account_id,vendor_key,purpose,conversation_id,scope,policy_version,revoked_at)
		VALUES ('ccec_00000000-0000-4000-8000-0000000000aa','agent-a','platform','creative_assistance',$1,'{"mode":"selected_revisions","data_classes":["text"]}','egress-2026-10-2',now())`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO creative_egress_consent_contents(account_id,consent_id,content_revision_id)
		VALUES ('agent-a','ccec_00000000-0000-4000-8000-0000000000aa',$1)`, formerlyWithdrawn); err != nil {
		t.Fatal(err)
	}
	appendLookupMessage(t, f, c.ID, []ContentRef{{RevisionID: formerlyWithdrawn, Role: "input"}}, "再次引用")
	_, inLibrary := f.modelAsset()
	trashedAsset, trashed := f.modelAsset()
	if _, err := f.db.Exec(`UPDATE creative_assets SET deleted_at=now() WHERE id=$1`, trashedAsset); err != nil {
		t.Fatal(err)
	}
	attached := f.revision(f.alice, "这条消息附带的")
	steered := f.revision(f.alice, "运行中插队附带的")
	loose := f.unlistedImage() // own, in no library entry
	theirs := f.revision(f.bob, "别人的")

	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"},
		InstructionSegment{Type: "content_ref", ContentRevisionID: attached})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.service.appendMessageInTx(t.Context(), tx, c.ID, newMessage{Role: "user", Status: "complete", RunID: run.ID,
			Body: Body{Blocks: []Block{{Type: "text", Text: "插队"}}}, ContentRefs: []ContentRef{{RevisionID: steered, Role: "input"}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	readable := func(consent Consent, id string) bool {
		t.Helper()
		var ok bool
		if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			var err error
			ok, err = readableInTx(t.Context(), tx, run.ID, consent, id)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	libraryOff := consent
	libraryOn := f.accountConsent(f.alice, true)
	for _, tt := range []struct {
		name    string
		id      string
		off, on bool
	}{
		{"on the current canvas", onCanvas, true, true},
		{"referenced earlier in the conversation", earlier, true, true},
		{"attached to this message", attached, true, true},
		{"steered into this run", steered, true, true},
		{"listed by a withdrawn grant, referenced again", formerlyWithdrawn, true, true},
		{"only in the library", inLibrary, false, true},
		{"in the library's trash", trashed, false, false},
		{"own, never referenced, not in the library", loose, false, false},
		{"own, never referenced, filed in the library", f.revision(f.alice, "资产库里的正文"), false, true},
		{"another account's", theirs, false, false},
	} {
		if got := readable(libraryOff, tt.id); got != tt.off {
			t.Errorf("%s, library off: readable=%v want %v", tt.name, got, tt.off)
		}
		if got := readable(libraryOn, tt.id); got != tt.on {
			t.Errorf("%s, library on: readable=%v want %v", tt.name, got, tt.on)
		}
	}
}

func TestNoRunStartsWithoutAConsentInForce(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	if _, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "帮我理一理。"}); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("an account that never agreed started a run: %v", err)
	}
	f.agree(f.alice, true)
	f.withdraw(f.alice)
	if _, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "帮我理一理。"}); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("a withdrawn consent started a run: %v", err)
	}
	for _, table := range []string{"creative_agent_runs", "creative_agent_messages", "llm_usage_reservations"} {
		if n := f.count(table, "true"); n != 0 {
			t.Fatalf("a refused run left %d rows in %s", n, table)
		}
	}
	// Agreeing again is all it takes.
	f.agree(f.alice, true)
	run, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.service.ReadAccountConsent(t.Context(), f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if run.EgressConsentID != state.Consent.ID {
		t.Fatalf("the run recorded consent %s, the account's is %s", run.EgressConsentID, state.Consent.ID)
	}
}

// A new notice stops a run in progress exactly as a withdrawal does, only
// with the other reason.
func TestANewNoticeStopsTheDispatch(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	// The server's notice moves on: the stored consent now reads as older.
	f.ageConsent(consent.ID)
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 0 {
		t.Fatal("content left the account under a notice the photographer has not seen")
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_egress_required" {
		t.Fatalf("run %+v", after)
	}
}

// A run started before a withdrawal carries on if the photographer agrees
// again before its next dispatch: the rule is judged at dispatch.
func TestAgreeingAgainBeforeTheDispatchLetsTheRunGoOn(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	f.withdraw(f.alice)
	f.agree(f.alice, true)
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || after.State != RunSucceeded {
		t.Fatalf("run %+v %v", after, err)
	}
}

func TestTheLibraryChoice(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	var tools [][]string
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		tools = append(tools, toolNames(r))
		return scriptedResult(r), nil
	}
	say := func() Run {
		t.Helper()
		run, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "找素材"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.work(f.alice, run.ID); err != nil {
			t.Fatal(err)
		}
		return run
	}

	f.agree(f.alice, false)
	say()
	if slices.Contains(tools[len(tools)-1], saveLibraryTool) || slices.Contains(tools[len(tools)-1], readLibraryTool) {
		t.Fatalf("library tools offered with library search off: %v", tools[len(tools)-1])
	}

	f.agree(f.alice, true)
	say()
	if !slices.Contains(tools[len(tools)-1], saveLibraryTool) {
		t.Fatalf("library tools missing after turning library search on: %v", tools[len(tools)-1])
	}

	// Turned off while a run that has the tools is in progress: the call is
	// refused, the run is not ended.
	run, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "找素材"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	h := &runtimeHandler{service: f.service, scope: f.alice, held: held, libraryEnabled: true}
	if err := h.requireLibrary(t.Context(), true); err != nil {
		t.Fatalf("library search on: %v", err)
	}
	f.agree(f.alice, false)
	var refusal *toolRefusal
	if err := h.requireLibrary(t.Context(), true); !errors.As(err, &refusal) {
		t.Fatalf("library search off mid-run must refuse the call, not end the run: %v", err)
	}
	f.withdraw(f.alice)
	if err := h.requireLibrary(t.Context(), true); !errors.Is(err, ErrConsentRevoked) {
		t.Fatalf("a withdrawal mid-run ends it: %v", err)
	}
}

// What library tools returned stops leaving once library search is closed:
// the request after the switch carries a placeholder, in the run that searched
// and in a later run reading it from the conversation's history.
func TestClosingTheLibraryWithholdsWhatItsToolsReturned(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	f.agree(f.alice, true)
	const title = "只在素材库里的灯塔"
	id := f.revision(f.alice, "正文")
	if _, err := f.db.Exec(`UPDATE creative_assets SET title=$2, normalized_title=$2 WHERE content_revision_id=$1`, id, title); err != nil {
		t.Fatal(err)
	}
	var carried, withheld []bool
	turn := 0
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		if len(r.Chat.Tools) == 0 {
			return scriptedResult(r), nil // the conversation's title
		}
		raw, err := json.Marshal(r.Chat.Messages)
		if err != nil {
			t.Fatal(err)
		}
		carried = append(carried, strings.Contains(string(raw), title))
		withheld = append(withheld, strings.Contains(string(raw), "素材库检索已关闭"))
		turn++
		switch turn {
		case 1:
			return scriptedResult(r, call("find", 0, searchAssetsTool, `{"limit":20}`)), nil
		case 2:
			// This request left with library search open; the next is bound
			// after it closes.
			f.agree(f.alice, false)
			return scriptedResult(r, call("again", 0, readResultTool, `{"item_id":"missing","offset":0,"limit":100}`)), nil
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "找灯塔"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if after, err := f.service.ReadRun(t.Context(), f.alice, run.ID); err != nil || after.State != RunSucceeded {
		t.Fatalf("run %+v %v", after, err)
	}
	if !slices.Equal(carried, []bool{false, true, false}) || !withheld[2] {
		t.Fatalf("library title per request %v, placeholder %v", carried, withheld)
	}

	next, err := f.startRun(f.alice, c.ID, "", InstructionSegment{Type: "text", Text: "接着说"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, next.ID); err != nil {
		t.Fatal(err)
	}
	if carried[len(carried)-1] || !withheld[len(withheld)-1] {
		t.Fatalf("history carried the library title into a later run: %v %v", carried, withheld)
	}

	// read_history and the summarizer's transcript withhold them too.
	h := &runtimeHandler{service: f.service, scope: f.alice, held: claim{RunID: next.ID, ConversationID: c.ID}}
	history, err := h.readHistory(t.Context(), `{"before_ordinal":0,"limit":20}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(history, title) || !strings.Contains(history, "素材库检索已关闭") {
		t.Fatalf("read_history: %s", history)
	}
	line := transcriptLine("tool", Body{ToolName: searchAssetsTool, Blocks: []Block{{Type: "text", Text: title}}}, false)
	if strings.Contains(line, title) {
		t.Fatalf("transcript: %s", line)
	}
	if line := transcriptLine("tool", Body{ToolName: searchAssetsTool, Blocks: []Block{{Type: "text", Text: title}}}, true); !strings.Contains(line, title) {
		t.Fatalf("transcript with library search open: %s", line)
	}
}

// Library search closed between binding a request and dispatching it: the gate
// refuses it like any missing consent, and the photographer is told it was
// library search that closed, not that the notice changed.
func TestAClosedLibraryDispatchSaysWhy(t *testing.T) {
	if !errors.Is(ErrLibraryClosed, ErrEgressRequired) {
		t.Fatal("a closed library must still be refused as a missing consent")
	}
	if got := failureCode(ErrLibraryClosed); got != "creative_egress_library_closed" {
		t.Fatalf("closed library ends as %s", got)
	}
	if got := failureCode(ErrEgressRequired); got != "creative_egress_required" {
		t.Fatalf("missing consent ends as %s", got)
	}
}
