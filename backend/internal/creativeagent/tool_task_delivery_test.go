package creativeagent

import (
	"context"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// requestText is everything a request's messages say, in order.
func requestText(r llmgateway.ProviderRequest) string {
	var out strings.Builder
	for _, m := range r.Chat.Messages {
		for _, b := range m.Blocks {
			out.WriteString(b.Text)
			out.WriteString("\n")
		}
	}
	return out.String()
}

func (f *fixture) delivery(handle string) (state, step string) {
	f.t.Helper()
	var s *string
	if err := f.db.QueryRow(`SELECT notification_state,delivery_model_step_id FROM creative_agent_tool_tasks WHERE id=$1`, handle).Scan(&state, &s); err != nil {
		f.t.Fatal(err)
	}
	if s != nil {
		step = *s
	}
	return state, step
}

// finishedInBackground leaves a pending notice in a conversation whose run
// already ended.
func finishedInBackground(t *testing.T, f *fixture) (Run, string) {
	t.Helper()
	run, generator, handle := detachedGeneration(t, f)
	generator.mu.Lock()
	generator.pending = false
	generator.mu.Unlock()
	f.markDue(handle)
	if _, err := f.service.FinishDueToolTasks(t.Context(), f.st, 10); err != nil {
		t.Fatal(err)
	}
	if state, _ := f.delivery(handle); state != "pending" {
		t.Fatalf("notice %s", state)
	}
	return run, handle
}

// The next run of the conversation carries a pending notice as tool data;
// the dispatch that sends it marks it delivered, the model step keeps it
// frozen, and a later run reads it once, from history.
func TestNextRunCarriesThePendingNotice(t *testing.T) {
	f := setup(t)
	run, handle := finishedInBackground(t, f)
	consent := contextConsent(t, f, run.ConversationID)
	var seen []llmgateway.ProviderRequest
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		seen = append(seen, r)
		return scriptedResult(r), nil
	}
	next, err := f.startRun(f.alice, run.ConversationID, consent.ID, InstructionSegment{Type: "text", Text: "好了吗"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, next.ID); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || !strings.Contains(requestText(seen[0]), noticePrefix) || !strings.Contains(requestText(seen[0]), handle) {
		t.Fatalf("notice not carried: %q", requestText(seen[0]))
	}
	state, step := f.delivery(handle)
	var frozen string
	if err := f.db.QueryRow(`SELECT coalesce(background_notification_ids::text,'') FROM creative_agent_steps WHERE id=$1 AND run_id=$2`, step, next.ID).Scan(&frozen); err != nil ||
		state != "delivered" || !strings.Contains(frozen, handle) {
		t.Fatalf("delivery %s %s frozen=%q %v", state, step, frozen, err)
	}

	later, err := f.startRun(f.alice, run.ConversationID, consent.ID, InstructionSegment{Type: "text", Text: "再说一遍"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, later.ID); err != nil {
		t.Fatal(err)
	}
	if text := requestText(seen[len(seen)-1]); strings.Count(text, noticePrefix) != 1 {
		t.Fatalf("history carries the notice %d times", strings.Count(text, noticePrefix))
	}
}

// A request refused before it goes out leaves the notice pending for the
// next one: only a committed dispatch delivers.
func TestRefusedDispatchLeavesTheNoticePending(t *testing.T) {
	f := setup(t)
	run, handle := finishedInBackground(t, f)
	consent := contextConsent(t, f, run.ConversationID)
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		return scriptedResult(r), nil
	}
	next, err := f.startRun(f.alice, run.ConversationID, consent.ID, InstructionSegment{Type: "text", Text: "好了吗"})
	if err != nil {
		t.Fatal(err)
	}
	f.withdraw(f.alice)
	_ = f.work(f.alice, next.ID)
	if got, _ := f.service.ReadRun(t.Context(), f.alice, next.ID); got.State == RunSucceeded {
		t.Fatal("dispatch went out without consent")
	}
	if state, step := f.delivery(handle); state != "pending" || step != "" {
		t.Fatalf("refused request delivered the notice: %s %s", state, step)
	}
}

// A background result that lands while the run is still working is
// answered by that run: it arrives after the request in flight, before the
// final answer is recorded, so the run makes one more request carrying it.
func TestNoticeArrivingMidRunIsAnsweredByTheRun(t *testing.T) {
	f := setup(t)
	f.queue()
	generator := &stubGenerator{}
	f.service.SetGenerator(generator)
	c := f.conversation(f.alice, f.canvasID)
	consent := contextConsent(t, f, c.ID)
	var seen []llmgateway.ProviderRequest
	f.vendor.script = func(ctx context.Context, r llmgateway.ProviderRequest, n int) (llmgateway.Result, error) {
		seen = append(seen, r)
		switch n {
		case 1:
			return scriptedResult(r, call("bg", 0, generateNodeTool, `{"node_id":"ccnd_img","draft_revision":1,"run_in_background":true}`)), nil
		case 2:
			// The generation finishes while this request is out.
			if _, err := f.service.FinishDueToolTasks(context.WithoutCancel(ctx), f.st, 10); err != nil {
				t.Error(err)
			}
		}
		return scriptedResult(r), nil
	}
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "后台出一张图"})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	handle := f.sceneTasks(run.ID)[0].ID
	if len(seen) != 3 || strings.Contains(requestText(seen[1]), noticePrefix) || !strings.Contains(requestText(seen[2]), handle) {
		t.Fatalf("requests=%d", len(seen))
	}
	if state, _ := f.delivery(handle); state != "delivered" {
		t.Fatalf("notice %s", state)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunSucceeded {
		t.Fatalf("run %s", got.State)
	}
}
