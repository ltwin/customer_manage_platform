package creativeagent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativeskill"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// holds reports how much budget this account still has tied up, and in how many
// reservations. One run must leave exactly one, and a run that is over must
// leave nothing held: a hold that outlives its run spends a month's allowance
// without a single model call having been made.
func (f *fixture) holds() (count int, tokens int64) {
	f.t.Helper()
	if err := f.db.QueryRow(
		`SELECT count(*), coalesce(sum(remaining_hold_tokens),0) FROM llm_usage_reservations`).
		Scan(&count, &tokens); err != nil {
		f.t.Fatal(err)
	}
	return count, tokens
}

// A run takes one hold, and the turn it was taken for claims that same one.
// Taking a second beside it would be invisible in every other assertion — the
// run succeeds, the answer arrives — right up to the point where an account
// that has run a few times is refused for a budget it never spent.
func TestOneRunHoldsBudgetOnceAndGivesItBackWhenItEnds(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)

	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	// The premise: creating the run really did take a hold, so what the next
	// assertion measures is the turn joining it rather than nothing happening.
	if count, tokens := f.holds(); count != 1 || tokens == 0 {
		t.Fatalf("creating the run left %d reservations holding %d tokens", count, tokens)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	count, tokens := f.holds()
	if count != 1 {
		t.Fatalf("one run and one turn produced %d reservations; the turn took a second hold "+
			"instead of claiming the one the run was admitted under", count)
	}
	if tokens != 0 {
		t.Fatalf("a finished run still holds %d tokens", tokens)
	}
	// Claimed, not abandoned: the hold belongs to the request that was actually
	// paid for, which is what lets accounting correct it later.
	var claimed int
	if err := f.db.QueryRow(
		`SELECT count(*) FROM llm_usage_reservations WHERE request_id IS NOT NULL`).Scan(&claimed); err != nil {
		t.Fatal(err)
	}
	if claimed != 1 {
		t.Fatalf("%d reservations were claimed by a request", claimed)
	}
}

// A run that ends before anything is dispatched still has to give its hold
// back. Nothing was spent, so nothing may stay reserved.
//
// This path gets there through the request: admission commits before the
// dispatch intent is refused, so the hold is already claimed and cancelling the
// request returns it. The expiry test below is the one that isolates the
// release itself, on a run that never reached admission at all.
func TestARunThatNeverDispatchesReleasesItsHold(t *testing.T) {
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
		t.Fatal("the premise is wrong: something was dispatched")
	}
	if count, tokens := f.holds(); tokens != 0 {
		t.Fatalf("%d reservations still hold %d tokens after a run that never dispatched", count, tokens)
	}
}

// The run's own deadline path leaves nothing held either. It is a separate
// terminal path from the one above — it ends the run without ever claiming it —
// so the release has to be at the point both of them pass through.
func TestARunThatExpiresInTheQueueReleasesItsHold(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
		InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(
		`UPDATE creative_agent_runs
		 SET deadline_at=created_at-interval '59 minutes', created_at=created_at-interval '1 hour'
		 WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if count, tokens := f.holds(); tokens != 0 {
		t.Fatalf("%d reservations still hold %d tokens after an expired run", count, tokens)
	}
}

// A model the worker cannot serve is discovered after the run was created — the
// deployment's catalog is read where the work happens, not where it was asked
// for. The takeover has already committed by then, so the failure has to reach
// a terminal state: a run left in running holds the account's only write slot
// and the redelivered task, finding nothing queued to claim, stops.
func TestAModelTheWorkerCannotServeEndsTheRunRatherThanStranding(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	run, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "帮我理一理。"})
	if err != nil {
		t.Fatal(err)
	}
	// The worker's own view of the deployment: same catalog, no credential, so
	// the model is listed and disabled exactly as it would be on a host that was
	// never given the key.
	working := f.service.models
	blind, err := llmgateway.DefaultCatalog(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	f.service.models = blind

	// Whatever the attempt reports is not the invariant. What matters is that
	// the run cannot be left mid-flight: River would redeliver, the redelivered
	// task would find nothing queued to claim, and the slot would stay taken
	// with nobody coming back for it.
	_ = f.work(f.alice, run.ID)
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_model_capability_missing" {
		t.Fatalf("run %+v", after)
	}
	if n := f.count("creative_agent_slots", "run_id IS NOT NULL"); n != 0 {
		t.Fatal("a run stranded by an unavailable model kept the account's write slot")
	}
	if count, tokens := f.holds(); tokens != 0 {
		t.Fatalf("%d reservations still hold %d tokens", count, tokens)
	}
	// The account is not locked out. Restoring the credential is what makes the
	// model servable again; the slot being free is what makes the next
	// submission possible at all, and that is the half this run had to get
	// right. Submitting while the model is still unavailable is refused on its
	// own merits, which is a different answer from "busy".
	f.service.models = working
	if _, err := f.startRun(f.alice, c.ID, consent.ID,
		InstructionSegment{Type: "text", Text: "再试一次。"}); err != nil {
		t.Fatalf("the account stayed blocked after a stranded run: %v", err)
	}
}

// What a submission costs against the input ceiling is measured on the request
// that is actually sent. A content reference costs its identifier when the run
// is created and its whole text when the prompt is built, and one legitimate
// reference can be larger than the entire budget — so the ceiling has to be
// checked again, after expansion and before anything leaves.
func TestAnExpandedReferenceIsMeasuredAgainstTheInputCeiling(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	// 90,000 Chinese runes is 270,000 UTF-8 bytes: a revision the content store
	// accepts, and more than the 256 KiB one request may carry.
	huge := f.revision(f.alice, strings.Repeat("参", 90_000))
	consent := f.consent(f.alice, c.ID, huge)

	run, err := f.startRun(f.alice, c.ID, consent.ID,
		InstructionSegment{Type: "text", Text: "参考这段。"},
		InstructionSegment{Type: "content_ref", ContentRevisionID: huge})
	// Creating the run is accepted: what it counted was an identifier, and at
	// that point that is genuinely all the submission contains.
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 0 {
		t.Fatalf("a prompt of %d bytes was sent past the %d byte ceiling",
			len(f.vendor.prompt()), DefaultLimits().InputTextBytes)
	}
	// The error code is what separates enforcing this contract from getting the
	// same outcome by accident. The hold a run takes is sized to this very
	// ceiling, so the gateway would refuse an oversized request too — but it
	// would refuse it as a budget that ran out, which is not what happened and
	// not something the photographer can act on. It would also stop being true
	// the moment a hold is sized any other way, and a limit that holds only
	// because of an unrelated number is not being enforced.
	after, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != RunFailed || after.ErrorCode != "creative_context_limit" {
		t.Fatalf("run %+v", after)
	}
	// The other side: a reference that fits still resolves and is really sent,
	// so the check is about the size and not about references.
	small := f.revision(f.alice, "按自然光分组。")
	fits := f.consent(f.alice, c.ID, small)
	next, err := f.startRun(f.alice, c.ID, fits.ID,
		InstructionSegment{Type: "content_ref", ContentRevisionID: small})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, next.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.vendor.prompt(), "按自然光分组。") {
		t.Fatalf("a reference within the ceiling never reached the model:\n%s", f.vendor.prompt())
	}
}

// countingDirectory records that the dispatch path really consulted the control
// port. Without this the run could pass every other assertion while checking
// availability only in a transaction of its own, which is the arrangement the
// invariant is about.
type countingDirectory struct {
	SkillDirectory
	mu      sync.Mutex
	inTxRan int
}

func (d *countingDirectory) RequireRunnableInTx(ctx context.Context, tx store.TxAccountScope, skillID, versionID string) error {
	d.mu.Lock()
	d.inTxRan++
	d.mu.Unlock()
	return d.SkillDirectory.RequireRunnableInTx(ctx, tx, skillID, versionID)
}

func (d *countingDirectory) calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.inTxRan
}

// The availability check runs inside the transaction that records the dispatch
// intent, not in one of its own. A check in its own transaction answers a
// question that has already expired: a withdrawal committing between it and the
// intent still lets the bytes go out.
func TestSkillAvailabilityIsCheckedInsideTheDispatchTransaction(t *testing.T) {
	f := setup(t)
	f.queue()
	published := f.publishSkill("reference-direction", "参考整理与创作方向")
	f.registerSkillTools()
	counting := &countingDirectory{SkillDirectory: f.service.skills}
	f.service.skills = counting

	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID,
		InstructionSegment{Type: "text", Text: "按这个来。"},
		InstructionSegment{Type: "skill_ref", SkillID: published.SkillID, SkillVersionID: published.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.work(f.alice, run.ID); err != nil {
		t.Fatal(err)
	}
	if f.vendor.calls() != 1 {
		t.Fatalf("the premise is wrong: the run did not dispatch (%d calls)", f.vendor.calls())
	}
	if counting.calls() == 0 {
		t.Fatal("the dispatch never consulted the skill control port, so a withdrawal " +
			"committed after the early read would not have stopped it")
	}
}

// The publisher's withdrawal and a reader's dispatch intent take the same
// version control lock, which is what makes "the disable committed first, so
// the dispatch must not go" a fact rather than a hope. They are in different
// accounts by construction — a platform skill exists so that one account can run
// what another published — so nothing account-scoped could serialise them.
func TestAWithdrawalAndADispatchIntentTakeTheSameVersionLock(t *testing.T) {
	f := setup(t)
	published := f.publishSkill("reference-direction", "参考整理与创作方向")

	// The reader's transaction passes the check and keeps its transaction open,
	// exactly as a dispatch does between checking and recording its intent.
	checked := make(chan struct{})
	release := make(chan struct{})
	readerDone := make(chan error, 1)
	go func() {
		readerDone <- f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
			if err := f.skills.RequireRunnableInTx(t.Context(), tx, published.SkillID, published.ID); err != nil {
				return err
			}
			close(checked)
			<-release
			return nil
		})
	}()
	<-checked

	withdrawn := make(chan error, 1)
	go func() {
		withdrawn <- f.skills.DisableVersion(t.Context(), f.platform, published.ID, "首版描述有误")
	}()
	// The withdrawal must wait: the dispatch that passed the check is allowed to
	// finish, and this is the half of the invariant that says an already
	// committed dispatch is not recalled.
	select {
	case err := <-withdrawn:
		t.Fatalf("the withdrawal overtook an open dispatch intent: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-readerDone; err != nil {
		t.Fatal(err)
	}
	if err := <-withdrawn; err != nil {
		t.Fatal(err)
	}

	// And the other order: once the withdrawal has committed, every later check
	// refuses. A dispatch is one of those checks.
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return f.skills.RequireRunnableInTx(t.Context(), tx, published.SkillID, published.ID)
	}); !errors.Is(err, creativeskill.ErrDisabled) {
		t.Fatalf("a dispatch after the withdrawal was not refused: %v", err)
	}
}

// The control port answers about visibility with the same vocabulary the read
// ports use, so a guessed pairing learns nothing here either.
func TestTheControlPortReportsWhatIsNotVisibleAsAbsent(t *testing.T) {
	f := setup(t)
	direction := f.publishSkill("reference-direction", "参考整理与创作方向")
	retouch := f.publishSkill("retouch-notes", "修图要点")
	private := f.publishOwnSkill("private-line", "内部草稿")

	for _, tc := range []struct {
		name               string
		skillID, versionID string
	}{
		{"版本属于另一个 Skill", direction.SkillID, retouch.ID},
		{"另一个账号的私有 Skill", private.SkillID, private.ID},
		{"根本不存在", direction.SkillID, "ccsv_00000000-0000-4000-8000-0000000000ff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
				return f.skills.RequireRunnableInTx(t.Context(), tx, tc.skillID, tc.versionID)
			})
			if !errors.Is(err, creativeskill.ErrNotFound) {
				t.Fatalf("expected absence, got %v", err)
			}
		})
	}
}
