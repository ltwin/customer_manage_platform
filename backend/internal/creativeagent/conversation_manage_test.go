package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func (f *fixture) managed(receipt creativeops.Receipt, err error) (Conversation, error) {
	f.t.Helper()
	if err != nil {
		return Conversation{}, err
	}
	var c Conversation
	if err := json.Unmarshal(receipt.Outcome.Response, &c); err != nil {
		f.t.Fatal(err)
	}
	return c, nil
}

func (f *fixture) rename(scope store.AccountScope, id, title string) (Conversation, error) {
	return f.managed(f.service.RenameConversation(f.t.Context(), scope,
		command(f.t, map[string]string{"conversation_id": id, "title": title})))
}

func (f *fixture) archive(scope store.AccountScope, id string, archive bool) (Conversation, error) {
	set := f.service.RestoreConversation
	if archive {
		set = f.service.ArchiveConversation
	}
	return f.managed(set(f.t.Context(), scope, command(f.t, map[string]string{"conversation_id": id})))
}

func (f *fixture) listed(archived bool) []string {
	f.t.Helper()
	page, err := f.service.ListConversations(f.t.Context(), f.alice, f.canvasID, archived, 10, "")
	if err != nil {
		f.t.Fatal(err)
	}
	ids := []string{}
	for _, c := range page.Items {
		ids = append(ids, c.ID)
	}
	return ids
}

// A rename is the photographer's own words: trimmed, kept as typed, and it does
// not move the conversation up the list the way a new exchange does.
func TestARenameKeepsThePhotographersTitleAndPlace(t *testing.T) {
	f := setup(t)
	c := f.conversation(f.alice, f.canvasID)
	if c.TitleSource != titleDefault || c.Archived {
		t.Fatalf("a new conversation: %+v", c)
	}
	renamed, err := f.rename(f.alice, c.ID, "  雪山婚纱 · 二稿  ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Title != "雪山婚纱 · 二稿" || renamed.TitleSource != titleManual || renamed.Revision != c.Revision+1 ||
		!renamed.UpdatedAt.Equal(c.UpdatedAt) {
		t.Fatalf("renamed: %+v, was %+v", renamed, c)
	}
	for _, title := range []string{"", "   ", strings.Repeat("名", 121)} {
		if _, err := f.rename(f.alice, c.ID, title); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("title %q: %v", title, err)
		}
	}
	if _, err := f.rename(f.bob, c.ID, "越界"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another account renamed the conversation: %v", err)
	}
}

// Naming arrives after the first answer, which is exactly when a photographer
// may already have typed a name. Theirs stands, and none is paid for.
func TestNamingNeverOverwritesAChosenTitle(t *testing.T) {
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
	if _, err := f.rename(f.alice, c.ID, "我的标题"); err != nil {
		t.Fatal(err)
	}
	calls := f.vendor.calls()
	f.vendor.reply = "雪山婚纱布光"
	if err := f.service.workTitle(t.Context(), f.alice, f.titleRequest(f.titleTask())); err != nil {
		t.Fatal(err)
	}
	page, err := f.service.ListConversations(t.Context(), f.alice, f.canvasID, false, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Items[0]; got.Title != "我的标题" || got.TitleSource != titleManual || f.vendor.calls() != calls {
		t.Fatalf("after naming: %+v, provider calls %d -> %d", got, calls, f.vendor.calls())
	}
	// No call was made, so there is no caller group; handing back nothing is
	// still a clean end rather than an error.
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return f.service.abandonTitleInTx(t.Context(), tx, run.ID)
	}); err != nil {
		t.Fatalf("abandoning a name that was never called: %v", err)
	}
}

// The name may already be on its way when the photographer types one. The
// paid answer is then dropped rather than written over theirs.
func TestARenameDuringNamingWins(t *testing.T) {
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
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		if _, err := f.rename(f.alice, c.ID, "我的标题"); err != nil {
			t.Error(err)
		}
		input, cached, output := int64(60), int64(0), int64(8)
		return llmgateway.Result{
			Text: "雪山婚纱布光", FinishReason: llmgateway.FinishStop, Model: r.Snapshot.RequestModelID,
			Usage:             llmgateway.UsageEvidence{InputTotalTokens: &input, InputCachedTokens: &cached, OutputTokens: &output},
			ProviderRequestID: "stub-title",
		}, nil
	}
	if err := f.service.workTitle(t.Context(), f.alice, f.titleRequest(task)); err != nil {
		t.Fatal(err)
	}
	page, err := f.service.ListConversations(t.Context(), f.alice, f.canvasID, false, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := page.Items[0]; got.Title != "我的标题" || got.TitleSource != titleManual {
		t.Fatalf("the name overwrote the photographer's: %+v", got)
	}
}

// A worker that stopped after the name was paid for but before it was written
// leaves a result waiting. If the photographer renames meanwhile, the
// redelivered task writes nothing and still hands that result back.
func TestARedeliveredNameAfterARenameReleasesWhatTheCallLeft(t *testing.T) {
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
	// The worker's context ends as the answer arrives: the gateway still
	// records the result, the consuming transaction never commits.
	ctx, stop := context.WithCancel(t.Context())
	f.vendor.script = func(_ context.Context, r llmgateway.ProviderRequest, _ int) (llmgateway.Result, error) {
		stop()
		input, cached, output := int64(60), int64(0), int64(8)
		return llmgateway.Result{
			Text: "雪山婚纱布光", FinishReason: llmgateway.FinishStop, Model: r.Snapshot.RequestModelID,
			Usage:             llmgateway.UsageEvidence{InputTotalTokens: &input, InputCachedTokens: &cached, OutputTokens: &output},
			ProviderRequestID: "stub-title",
		}, nil
	}
	if err := f.service.nameConversation(ctx, f.alice, task); err == nil {
		t.Fatal("the interrupted name was written")
	}
	if n := f.count("llm_result_consumers", "caller_service=$1 AND state='pending'", titleCallerService); n != 1 {
		t.Fatalf("the interruption left %d pending title consumers, want 1", n)
	}
	f.vendor.script = nil
	if _, err := f.rename(f.alice, c.ID, "我的标题"); err != nil {
		t.Fatal(err)
	}
	if err := f.service.workTitle(t.Context(), f.alice, f.titleRequest(task)); err != nil {
		t.Fatal(err)
	}
	f.titleReleased()
}

// Archiving moves a conversation between the two lists and closes it to new
// runs; restoring opens it again. Repeating either changes nothing.
func TestAnArchivedConversationLeavesTheListAndTakesNoRuns(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	archived, err := f.archive(f.alice, c.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.Revision != c.Revision+1 || !archived.UpdatedAt.Equal(c.UpdatedAt) {
		t.Fatalf("archived: %+v", archived)
	}
	if open, closed := f.listed(false), f.listed(true); len(open) != 0 || len(closed) != 1 || closed[0] != c.ID {
		t.Fatalf("open %v, archived %v", open, closed)
	}
	again, err := f.archive(f.alice, c.ID, true)
	if err != nil || again.Revision != archived.Revision {
		t.Fatalf("archiving twice: %+v, %v", again, err)
	}
	if _, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "继续"}); !errors.Is(err, ErrConversationArchived) {
		t.Fatalf("a run started in an archived conversation: %v", err)
	}
	if n := f.count("creative_agent_messages", "conversation_id=$1", c.ID); n != 0 {
		t.Fatalf("the refused run left %d messages behind", n)
	}
	if _, err := f.rename(f.alice, c.ID, "改名"); !errors.Is(err, ErrConversationArchived) {
		t.Fatalf("an archived conversation was renamed: %v", err)
	}

	restored, err := f.archive(f.alice, c.ID, false)
	if err != nil || restored.Archived {
		t.Fatalf("restored: %+v, %v", restored, err)
	}
	if open := f.listed(false); len(open) != 1 {
		t.Fatalf("restored conversation is not listed: %v", open)
	}
	if _, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "继续"}); err != nil {
		t.Fatalf("a restored conversation takes runs again: %v", err)
	}
}

// A run still going would disappear from the panel mid-answer.
func TestAConversationWithAnUnfinishedRunCannotBeArchived(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.archive(f.alice, c.ID, true); !errors.Is(err, ErrRunState) {
		t.Fatalf("archived with a queued run: %v", err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.archive(f.alice, c.ID, true); err != nil {
		t.Fatalf("archiving after the run finished: %v", err)
	}
}
