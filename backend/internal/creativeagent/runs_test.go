package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// stubVendor is the company at the far end of the wire. Everything between the
// assistant and it is the real gateway: admission, the hold, the dispatch
// intent, the persisted complete result and the accounting all run for real, so
// a test that observes "the vendor was never called" is observing the actual
// dispatch path rather than a mock of it.
type stubVendor struct {
	mu     sync.Mutex
	sent   []llmgateway.ChatRequest
	reply  string
	fail   error
	script func(context.Context, llmgateway.ProviderRequest, int) (llmgateway.Result, error)
}

func (v *stubVendor) Key() llmgateway.ProviderKey { return llmgateway.ProviderOpenAICompatible }

func (v *stubVendor) Invoke(ctx context.Context, r llmgateway.ProviderRequest) (llmgateway.Result, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sent = append(v.sent, r.Chat)
	if v.script != nil {
		return v.script(ctx, r, len(v.sent))
	}
	if v.fail != nil {
		return llmgateway.Result{}, v.fail
	}
	text := v.reply
	if text == "" {
		text = "好的，我来整理这些参考。"
	}
	input, cached, output := int64(120), int64(0), int64(40)
	return llmgateway.Result{
		Text: text, FinishReason: llmgateway.FinishStop, Model: r.Snapshot.RequestModelID,
		Usage:             llmgateway.UsageEvidence{InputTotalTokens: &input, InputCachedTokens: &cached, OutputTokens: &output},
		ProviderRequestID: "stub-" + r.Snapshot.RequestModelID,
	}, nil
}

func (v *stubVendor) calls() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.sent)
}

// prompt is everything the vendor was actually shown, flattened. Assertions on
// what did or did not leave the account read this rather than the assembler's
// own return value.
func (v *stubVendor) prompt() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var b strings.Builder
	for _, chat := range v.sent {
		for _, m := range chat.Messages {
			for _, block := range m.Blocks {
				b.WriteString(block.Text)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

// queue creates the real River schema and binds it. Enqueue is the last thing
// creating a run does, so a fixture that stubbed it would never exercise the
// only failure that has to take the whole creation down with it.
func (f *fixture) queue() {
	f.t.Helper()
	if err := f.st.MigrateCreativeJobs(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	runtime, err := f.st.NewJobRuntime(f.service.Handlers(), slog.New(slog.DiscardHandler))
	if err != nil {
		f.t.Fatal(err)
	}
	f.service.SetRuntime(runtime)
}

const testModelKey = "deepseek-flash"

func (f *fixture) consent(scope store.AccountScope, conversationID string, revisionIDs ...string) Consent {
	f.t.Helper()
	payload := map[string]any{
		"conversation_id": conversationID,
		"vendor_key":      "deepseek",
		"purpose":         "creative_assistance",
		"mode":            ScopeAccountLibrary,
		"data_classes":    []string{"text"},
	}
	if len(revisionIDs) > 0 {
		payload["mode"] = ScopeSelectedRevisions
		payload["selected_revision_ids"] = revisionIDs
	}
	receipt, err := f.service.GrantConsent(f.t.Context(), scope, command(f.t, payload))
	if err != nil {
		f.t.Fatalf("grant consent: %v", err)
	}
	var c Consent
	if err := json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		f.t.Fatal(err)
	}
	return c
}

func (f *fixture) startRun(scope store.AccountScope, conversationID, consentID string, segments ...InstructionSegment) (Run, error) {
	f.t.Helper()
	receipt, err := f.service.CreateRun(f.t.Context(), scope, command(f.t, map[string]any{
		"conversation_id": conversationID,
		"instruction": map[string]any{
			"schema_version":       instructionSchemaVersion,
			"instruction_segments": segments,
		},
		"model_key":         testModelKey,
		"egress_consent_id": consentID,
	}))
	if err != nil {
		return Run{}, err
	}
	var run Run
	if err := json.Unmarshal(receipt.Outcome.Response, &run); err != nil {
		f.t.Fatal(err)
	}
	return run, nil
}

// work drives one queued run to its terminal state, exactly as the worker
// process does when River delivers the task.
func (f *fixture) work(scope store.AccountScope, runID string) error {
	f.t.Helper()
	return f.service.workRun(f.t.Context(), scope, jobs.Request{
		Kind: jobRunKind, OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(),
		Payload: json.RawMessage(`{"run_id":"` + runID + `"}`),
	})
}

// Creating a run is one fact. The photographer's message, the frozen inputs,
// the write slot, the budget hold, the first event and the queued task all
// exist together or none of them does — a visible run nobody will execute and a
// queued task with no run are each a way for work to disappear.
func TestCreatingARunIsOneFactOrNone(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)

	// No queue yet: the enqueue fails inside the transaction, and everything
	// written before it must go with it.
	if _, err := f.startRun(f.alice, c.ID, consent.ID,
		InstructionSegment{Type: "text", Text: "帮我理一理。"}); err == nil {
		t.Fatal("a run must not be created when it cannot be queued")
	}
	for table, cond := range map[string]string{
		"creative_agent_runs":    "true",
		"creative_run_inputs":    "true",
		"creative_run_events":    "true",
		"creative_agent_slots":   "run_id IS NOT NULL",
		"llm_usage_reservations": "true",
	} {
		if n := f.count(table, cond); n != 0 {
			t.Fatalf("%s kept %d rows after a creation that failed", table, n)
		}
	}
	// The trigger message is written before the enqueue, so its absence is what
	// proves the rollback reached the part a photographer would have seen.
	if n := f.count("creative_agent_messages", "true"); n != 0 {
		t.Fatalf("a message survived a creation that failed: %d", n)
	}

	f.queue()
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	if run.State != RunQueued || run.CanvasID != f.canvasID || run.LimitsVersion != limitsVersion {
		t.Fatalf("run %+v", run)
	}
	for table, cond := range map[string]string{
		"creative_agent_runs":     "id=$1",
		"creative_run_inputs":     "run_id=$1",
		"creative_run_events":     "run_id=$1 AND seq=1 AND event_type='run.accepted'",
		"creative_agent_messages": "id=$1",
	} {
		id := run.ID
		if table == "creative_agent_messages" {
			id = run.TriggerMessageID
		}
		if n := f.count(table, cond, id); n != 1 {
			t.Fatalf("%s has %d rows for the created run", table, n)
		}
	}
	if n := f.count("creative_agent_slots", "run_id=$1", run.ID); n != 1 {
		t.Fatal("the run did not take the account's write slot")
	}
	if n := f.count("llm_usage_reservations", "caller_group_id=$1", run.ID); n != 1 {
		t.Fatal("the run took no budget hold")
	}
}

// The account has one write slot, so a second submission is refused while the
// first is still working — and it is told which run to wait for rather than
// just "busy".
func TestTheAccountHasOneWriteSlot(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	first, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "第一条。"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "第二条。"})
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("a second run must wait for the slot, got %v", err)
	}
	var busy BusyError
	if !errors.As(err, &busy) || busy.ActiveRunID != first.ID {
		t.Fatalf("the refusal must name the run holding the slot: %+v", err)
	}
	// The other account is unaffected: the slot is per account, not per
	// deployment.
	other := f.conversation(f.bob, "cccv_b")
	if _, err := f.startRun(f.bob, other.ID, f.consent(f.bob, other.ID).ID,
		InstructionSegment{Type: "text", Text: "我的。"}); err != nil {
		t.Fatalf("another account was blocked by this account's slot: %v", err)
	}
}

// One submission produces one answer, and the whole path is real: the step is
// fixed before anything is sent, the result the vendor returned is what the
// photographer reads, and the slot is free again afterwards.
func TestOneSubmissionProducesOneAnswer(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.vendor.reply = "先按光线把参考分成三组。"
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理这些参考。"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunSucceeded || after.FinishedAt == nil || after.ErrorCode != "" {
		t.Fatalf("run %+v", after)
	}
	if n := f.count("creative_agent_slots", "run_id IS NOT NULL"); n != 0 {
		t.Fatal("a finished run kept the write slot")
	}
	// The step exists and succeeded, and it names the gateway request so a later
	// reader can reconcile the cost without parsing anything.
	if n := f.count("creative_agent_steps",
		"run_id=$1 AND kind='model' AND state='succeeded' AND llm_request_id IS NOT NULL", run.ID); n != 1 {
		t.Fatalf("the model step is missing or unfinished")
	}
	page, err := f.service.ListMessages(t.Context(), f.alice, c.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected the submission and one answer, got %d", len(page.Items))
	}
	answer := page.Items[1]
	if answer.Role != "assistant" || answer.Body.Blocks[0].Text != "先按光线把参考分成三组。" {
		t.Fatalf("answer %+v", answer)
	}
	// The answer is the projection of a step, which is what makes consuming the
	// same result twice return this message instead of appending a second one.
	if n := f.count("creative_agent_messages", "id=$1 AND source_step_id IS NOT NULL AND run_id=$2",
		answer.ID, run.ID); n != 1 {
		t.Fatal("the answer is not registered as this step's projection")
	}
	if f.vendor.calls() != 1 {
		t.Fatalf("the vendor was called %d times for one turn", f.vendor.calls())
	}
}

// An authorisation withdrawn in a transaction that commits before the dispatch
// intent must stop the bytes. This is the whole reason the last check runs
// inside the dispatch transaction rather than in one of its own.
func TestAWithdrawnAuthorisationStopsTheDispatch(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{
		"consent_id": consent.ID, "expected_revision": "1",
	})); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 0 {
		t.Fatal("content left the account after the authorisation was withdrawn")
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_egress_revoked" {
		t.Fatalf("run %+v", after)
	}
	if n := f.count("creative_agent_slots", "run_id IS NOT NULL"); n != 0 {
		t.Fatal("a run stopped by a withdrawal kept the write slot")
	}
}

// A skill withdrawn between submitting and dispatching stops the dispatch. The
// run carries the frozen instructions, so only a fresh read of the directory
// can answer this — a cached snapshot would happily run withdrawn work.
func TestASkillWithdrawnAfterSubmissionStopsTheDispatch(t *testing.T) {
	f := setup(t)
	f.queue()
	published := f.publishSkill("reference-direction", "参考整理与创作方向")
	f.registerSkillTools()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID,
		InstructionSegment{Type: "text", Text: "按这个来。"},
		InstructionSegment{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID})
	if err != nil {
		t.Fatal(err)
	}
	// The premise: the run really did fix this version, and that fact is a row
	// rather than something hidden in a document.
	if n := f.count("creative_run_skill_refs", "run_id=$1 AND skill_version_id=$2", run.ID, published.ID); n != 1 {
		t.Fatal("the run did not register the skill it fixed")
	}
	if err := f.skills.DisableVersion(t.Context(), f.platform, published.ID, "首版描述有误"); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 0 {
		t.Fatal("a withdrawn skill was dispatched anyway")
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_skill_unavailable" {
		t.Fatalf("run %+v", after)
	}
}

// The inputs are fixed when the run is created. A message appended by another
// window a moment later belongs to the next run, not to this one: what the
// photographer saw when they pressed send is what the model is told.
func TestHistoryIsFrozenWhenTheRunIsCreated(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	first, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "第一次提交。"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "第二次提交。"})
	if err != nil {
		t.Fatal(err)
	}
	// Appended after the run was created, so it must not reach the model.
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.service.appendMessageInTx(t.Context(), tx, c.ID, newMessage{
			Role: "user", Status: "complete",
			Body: Body{Blocks: []Block{{Type: "text", Text: "另一个窗口后写的。"}}},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, second.ID); err != nil {
		t.Fatal(err)
	}
	prompt := f.vendor.prompt()
	if strings.Contains(prompt, "另一个窗口后写的") {
		t.Fatal("a message written after the run was created reached the model")
	}
	if !strings.Contains(prompt, "第一次提交") || !strings.Contains(prompt, "第二次提交") {
		t.Fatalf("the frozen history did not reach the model:\n%s", prompt)
	}
}

// A run whose window closed while the task waited in the queue is not
// dispatched. Waiting never extends the deadline, so the only honest outcome is
// to end it and give the slot back.
func TestAWindowThatClosedInTheQueueIsNotDispatched(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	// Both timestamps move into the past together, because the column refuses a
	// deadline that precedes the creation: the run is one whose window closed
	// while its task sat in the queue, not one created with an invalid bound.
	if _, err := f.db.Exec(
		`UPDATE creative_agent_runs
		 SET deadline_at=created_at-interval '59 minutes', created_at=created_at-interval '1 hour'
		 WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 0 {
		t.Fatal("a run past its deadline was dispatched")
	}
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_run_deadline_exceeded" {
		t.Fatalf("run %+v", after)
	}
	if n := f.count("creative_agent_slots", "run_id IS NOT NULL"); n != 0 {
		t.Fatal("an expired run kept the write slot")
	}
}

// A redelivered task finds nothing to claim. River promises at-least-once
// delivery; the run's own state is what makes the effect at-most-once.
func TestARedeliveredTaskDoesNothingTwice(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatalf("a redelivered task must be a no-op, not a failure: %v", err)
	}
	if f.vendor.calls() != 1 {
		t.Fatalf("the vendor was called %d times across two deliveries", f.vendor.calls())
	}
	page, err := f.service.ListMessages(t.Context(), f.alice, c.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("a second delivery appended %d extra messages", len(page.Items)-2)
	}
}

// An authorisation granted for another conversation does not cover this one:
// the photographer's own earlier turns are part of what leaves the account.
func TestTheAuthorisationMustBeForThisConversation(t *testing.T) {
	f := setup(t)
	f.queue()
	mine := f.conversation(f.alice, f.canvasID)
	other := f.conversation(f.alice, f.canvasID)
	elsewhere := f.consent(f.alice, other.ID)
	if _, err := f.startRun(f.alice, mine.ID, elsewhere.ID,
		InstructionSegment{Type: "text", Text: "帮我理一理。"}); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("a consent for another conversation must not start this run, got %v", err)
	}
	// Named content needs to have been listed: a library-wide grant authorises
	// what a search finds, not a revision chosen by hand.
	revision := f.revision(f.alice, "我的参考文字")
	library := f.consent(f.alice, mine.ID)
	if _, err := f.startRun(f.alice, mine.ID, library.ID,
		InstructionSegment{Type: "content_ref", ContentRevisionID: revision}); !errors.Is(err, ErrEgressRequired) {
		t.Fatalf("named content under a library-wide grant must be refused, got %v", err)
	}
	listed := f.consent(f.alice, mine.ID, revision)
	run, err := f.startRun(f.alice, mine.ID, listed.ID,
		InstructionSegment{Type: "content_ref", ContentRevisionID: revision})
	if err != nil {
		t.Fatalf("a listed revision must be accepted: %v", err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.vendor.prompt(), "我的参考文字") {
		t.Fatalf("the authorised content never reached the model:\n%s", f.vendor.prompt())
	}
}

// Another account's run is absent, not refused: the scope decides what exists
// before the identifier does.
func TestAnotherAccountsRunIsAbsent(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
		InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ReadRun(t.Context(), f.bob, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account's run must be absent, got %v", err)
	}
	if _, err := f.service.ReadRun(t.Context(), f.alice, ""); !errors.Is(err, creativeops.ErrValidation) {
		t.Fatalf("an empty id is malformed, got %v", err)
	}
}
