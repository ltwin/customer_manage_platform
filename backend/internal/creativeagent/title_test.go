package creativeagent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// The first answered run asks for a name; naming reads the opening dialogue and
// keeps only the title the model was asked for. A second run asks for nothing,
// and what it adds never changes the input a redelivered name replays.
func TestTheFirstAnsweredRunNamesItsConversation(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我想一组雪山婚纱的布光方案"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	task := f.titleTask()
	if task.RunID != run.ID || task.ConsentID != consent.ID || task.Through < 1 {
		t.Fatalf("queued title task: %+v", task)
	}

	before, err := f.service.ListConversations(t.Context(), f.alice, f.canvasID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	f.vendor.reply = "标题：「雪山婚纱布光」。\n其他说明"
	request := f.titleRequest(task)
	if err := f.service.workTitle(t.Context(), f.alice, request); err != nil {
		t.Fatal(err)
	}
	if err := f.service.workTitle(t.Context(), f.alice, request); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 2 {
		t.Fatalf("a redelivered title task must replay the paid call, provider calls: %d", f.vendor.calls())
	}
	if prompt := f.vendor.prompt(); !strings.Contains(prompt, "摄影师：帮我想一组雪山婚纱的布光方案") || !strings.Contains(prompt, "助手：") {
		t.Fatalf("title source:\n%s", prompt)
	}
	page, err := f.service.ListConversations(t.Context(), f.alice, f.canvasID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Items[0]; got.Title != "雪山婚纱布光" || got.Revision != before.Items[0].Revision+1 {
		t.Fatalf("conversation after naming: %+v", got)
	}

	next, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "再补一组夜景"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, next.ID); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_jobs.river_job", "args->'request'->>'kind'=$1", jobTitleKind); n != 1 {
		t.Fatalf("a later run must not rename the conversation, title tasks: %d", n)
	}
	// The conversation has grown since; a replay still sends nothing new.
	calls := f.vendor.calls()
	if err := f.service.nameConversation(t.Context(), f.alice, task); err != nil || f.vendor.calls() != calls {
		t.Fatalf("replay after the conversation grew: %v, provider calls %d -> %d", err, calls, f.vendor.calls())
	}
}

// Withdrawing the consent before the name is dispatched stops the words from
// leaving, and the abandoned call holds neither budget nor a retained result.
func TestARevokedConsentStopsNaming(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我想一组雪山婚纱的布光方案"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	task := f.titleTask()
	if _, err := f.service.RevokeConsent(t.Context(), f.alice,
		command(t, map[string]any{"consent_id": consent.ID, "expected_revision": consent.Revision})); err != nil {
		t.Fatal(err)
	}
	before, err := f.service.ListConversations(t.Context(), f.alice, f.canvasID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	calls := f.vendor.calls()
	if err := f.service.workTitle(t.Context(), f.alice, f.titleRequest(task)); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != calls {
		t.Fatalf("naming went out after the consent was withdrawn, provider calls %d -> %d", calls, f.vendor.calls())
	}
	page, err := f.service.ListConversations(t.Context(), f.alice, f.canvasID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0] != before.Items[0] {
		t.Fatalf("an unnamed conversation is left as it was: %+v", page.Items[0])
	}
	f.titleReleased()
}

// A provider that refuses the name ends the task; the finished request keeps no
// retention claim that nobody will ever consume.
func TestARefusedNameReleasesItsRetention(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我想一组雪山婚纱的布光方案"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	f.vendor.fail = &llmgateway.ProviderError{Outcome: llmgateway.OutcomeRejected, Class: "invalid_request", StatusCode: 400}
	if err := f.service.workTitle(t.Context(), f.alice, f.titleRequest(f.titleTask())); err != nil {
		t.Fatal(err)
	}
	if n := f.count("llm_requests", "caller_service=$1", titleCallerService); n != 1 {
		t.Fatalf("the refused name was never sent: %d requests", n)
	}
	f.titleReleased()
}

func (f *fixture) titleTask() titleTask {
	f.t.Helper()
	var raw []byte
	if err := f.db.QueryRow(`SELECT args->'request'->'payload' FROM creative_jobs.river_job WHERE args->'request'->>'kind'=$1`, jobTitleKind).Scan(&raw); err != nil {
		f.t.Fatalf("title task: %v", err)
	}
	var task titleTask
	if err := json.Unmarshal(raw, &task); err != nil {
		f.t.Fatal(err)
	}
	return task
}

func (f *fixture) titleRequest(task titleTask) jobs.Request {
	f.t.Helper()
	payload, err := json.Marshal(task)
	if err != nil {
		f.t.Fatal(err)
	}
	return jobs.Request{Kind: jobTitleKind, OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(), Payload: payload}
}

// titleReleased checks that an abandoned name left nothing held behind.
func (f *fixture) titleReleased() {
	f.t.Helper()
	if n := f.count("llm_result_consumers", "caller_service=$1 AND state='pending'", titleCallerService); n != 0 {
		f.t.Fatalf("pending title consumers: %d", n)
	}
	if n := f.count("llm_usage_reservations", "caller_service=$1 AND settlement_state NOT IN ('released','settled')", titleCallerService); n != 0 {
		f.t.Fatalf("title holds still reserved: %d", n)
	}
}

func TestCleanTitleKeepsOnlyTheName(t *testing.T) {
	for in, want := range map[string]string{
		"《人像布光》":                      "人像布光",
		"Title: \"Night Portraits\".": "Night Portraits",
		"  \n":                        "",
		"一二三四五六七八九十一二三四五六七八九十多出来": "一二三四五六七八九十一二三四五六七八九十",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
