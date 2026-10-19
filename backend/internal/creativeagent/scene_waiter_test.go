package creativeagent

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

// parkOnRender parks a run on one render job with the real waiter registered.
func parkOnRender(t *testing.T, f *fixture) (Run, claim, string) {
	t.Helper()
	creativescene.SetJobWaiterForTest(t, f.service.SceneJobWaiter())
	run, held, execution, _, err := startParkingRun(t, f, nil, readResultCall("a", "a"))
	if err = f.service.completeAttempt(t.Context(), f.alice, held, execution.outcome, err); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingClient {
		t.Fatalf("not parked: %s", got.State)
	}
	var step string
	for s := range f.runJobs(run.ID) {
		step = s
	}
	return run, held, step
}

// hostFails is a render host claiming the job and reporting that it could
// not render.
func (f *fixture) hostFails() (creativescene.CompleteResponse, error) {
	f.t.Helper()
	claimed, err := creativescene.Claim(f.t.Context(), f.alice, creativescene.ClaimInput{EditorSessionID: "tab_0123456789abcdef"})
	if err != nil || claimed == nil {
		f.t.Fatalf("claim %+v %v", claimed, err)
	}
	cmd := command(f.t, map[string]any{"job_id": claimed.ID, "claim_token": claimed.ClaimToken, "outcome": "failed",
		"error": map[string]any{"code": "render_error", "message": "WebGL 上下文丢失"}})
	cmd.OperationID = uuid.NewString()
	receipt, err := creativescene.Complete(f.t.Context(), f.alice, cmd)
	if err != nil {
		return creativescene.CompleteResponse{}, err
	}
	var out creativescene.CompleteResponse
	err = json.Unmarshal(receipt.Outcome.Response, &out)
	return out, err
}

func (f *fixture) revokeConsent(id string) {
	f.t.Helper()
	var revision int64
	if err := f.db.QueryRow(`SELECT revision FROM creative_egress_consents WHERE id=$1`, id).Scan(&revision); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.service.RevokeConsent(f.t.Context(), f.alice, command(f.t, map[string]any{"consent_id": id, "expected_revision": strconv.FormatInt(revision, 10)})); err != nil {
		f.t.Fatal(err)
	}
}

func lastToolResult(sent []llmgateway.ChatRequest) string {
	if len(sent) == 0 {
		return ""
	}
	messages := sent[len(sent)-1].Messages
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmgateway.RoleTool {
			for _, b := range messages[i].Blocks {
				if b.Kind == llmgateway.BlockText {
					return b.Text
				}
			}
		}
	}
	return ""
}

func TestCompletedJobResumesTheParkedRunWithItsResult(t *testing.T) {
	f := setup(t)
	run, _, step := parkOnRender(t, f)
	resp, err := f.hostFails()
	if err != nil || resp.State != creativescene.JobFailed {
		t.Fatalf("complete %+v %v", resp, err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued || got.WaitReason != "" {
		t.Fatalf("after completion: %+v", got)
	}
	if holder := f.slotHolder(); holder != run.ID {
		t.Fatalf("slot held by %q", holder)
	}
	var state, output string
	if err = f.db.QueryRow(`SELECT state, output::text FROM creative_agent_steps WHERE id=$1`, step).Scan(&state, &output); err != nil {
		t.Fatal(err)
	}
	if state != "failed" || !strings.Contains(output, "render_error") {
		t.Fatalf("step %s %s", state, output)
	}
	if n := f.count("creative_agent_messages", "run_id=$1 AND role='tool' AND source_step_id=$2", run.ID, step); n != 1 {
		t.Fatalf("tool messages=%d", n)
	}
	done := f.resume(run.ID)
	f.vendor.mu.Lock()
	sent := append([]llmgateway.ChatRequest(nil), f.vendor.sent...)
	f.vendor.mu.Unlock()
	if done.State != RunSucceeded || len(sent) != 2 || !strings.Contains(lastToolResult(sent), "WebGL 上下文丢失") {
		t.Fatalf("resumed run %s after %d model calls, tool result %q", done.State, len(sent), lastToolResult(sent))
	}
}

// A run that may no longer write ends when its job completes: the page is
// told to discard what it made, and the run's jobs go with it.
func TestCompletionOfARunThatLostItsConsentEndsTheRun(t *testing.T) {
	f := setup(t)
	run, held, _ := parkOnRender(t, f)
	f.revokeConsent(held.ConsentID)
	if _, err := f.hostFails(); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("completion under a revoked consent: %v", err)
	}
	got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if got.State != RunFailed || got.ErrorCode != failureCode(ErrConsentRevoked) {
		t.Fatalf("run %s %s", got.State, got.ErrorCode)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1 AND state='cancelled'", run.ID); n != 1 {
		t.Fatalf("job not cancelled with the run")
	}
}

func TestExpiredJobTellsTheRunThePageNeverOpened(t *testing.T) {
	f := setup(t)
	run, _, step := parkOnRender(t, f)
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET deadline=now()-interval '1 second' WHERE run_id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if counts, err := creativescene.SweepJobs(t.Context(), f.alice, 10); err != nil || counts.Expired != 1 {
		t.Fatalf("sweep %+v %v", counts, err)
	}
	var output string
	if err := f.db.QueryRow(`SELECT output::text FROM creative_agent_steps WHERE id=$1`, step).Scan(&output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, `\"status\":\"expired\"`) {
		t.Fatalf("step output %s", output)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued {
		t.Fatalf("after expiry: %s", got.State)
	}
}

func TestExpiryOfARunThatLostItsConsentEndsTheRun(t *testing.T) {
	f := setup(t)
	run, held, _ := parkOnRender(t, f)
	f.revokeConsent(held.ConsentID)
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET deadline=now()-interval '1 second' WHERE run_id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if counts, err := creativescene.SweepJobs(t.Context(), f.alice, 10); err != nil || counts.Aborted != 1 {
		t.Fatalf("sweep %+v %v", counts, err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunFailed {
		t.Fatalf("run %s", got.State)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1 AND state='cancelled'", run.ID); n != 1 {
		t.Fatalf("job not cancelled with the run")
	}
}

// The page can finish while the attempt that parked is still running: the
// result is written under the running run, and the attempt then queues it.
func TestJobCompletedWhileTheRunIsStillRunning(t *testing.T) {
	f := setup(t)
	creativescene.SetJobWaiterForTest(t, f.service.SceneJobWaiter())
	var completed error
	finish := func(*runExecution) func(creativescene.Job, string) {
		return func(creativescene.Job, string) { _, completed = f.hostFails() }
	}
	run, held, execution, _, err := startParkingRun(t, f, finish, readResultCall("a", "a"))
	if completed != nil {
		t.Fatalf("completion during the attempt: %v", completed)
	}
	if n := f.count("creative_agent_steps", "run_id=$1 AND kind='tool' AND state='prepared'", run.ID); n != 0 {
		t.Fatalf("prepared tool steps=%d", n)
	}
	if err = f.service.completeAttempt(t.Context(), f.alice, held, execution.outcome, err); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued {
		t.Fatalf("after the attempt: %s", got.State)
	}
	if done := f.resume(run.ID); done.State != RunSucceeded {
		t.Fatalf("resumed run %s", done.State)
	}
}

// A run that ended while a page still held its job (left behind before the
// worker's own close cancelled jobs) must not keep that job claimed forever:
// the sweep cancels it once, instead of re-admitting the run every pass.
func TestSweepCancelsAJobWhoseRunAlreadyEnded(t *testing.T) {
	f := setup(t)
	run, _, _ := parkOnRender(t, f)
	if claimed, err := creativescene.Claim(t.Context(), f.alice, creativescene.ClaimInput{EditorSessionID: "tab_0123456789abcdef"}); err != nil || claimed == nil {
		t.Fatalf("claim %+v %v", claimed, err)
	}
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='failed', error_code='creative_context_limit', finished_at=now(), updated_at=now() WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET lease_until=now()-interval '1 second', deadline=now()-interval '1 second' WHERE run_id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := creativescene.SweepJobs(t.Context(), f.alice, 10); err != nil {
		t.Fatal(err)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1 AND state='cancelled'", run.ID); n != 1 {
		t.Fatalf("job of an ended run still open: %v", f.runJobs(run.ID))
	}
	if counts, err := creativescene.SweepJobs(t.Context(), f.alice, 10); err != nil || counts != (creativescene.SweepCounts{}) {
		t.Fatalf("second sweep %+v %v", counts, err)
	}
}

// A page that finishes a job of a run that already ended is told the job was
// cancelled, and it is.
func TestCompletionOfAJobWhoseRunAlreadyEndedCancelsIt(t *testing.T) {
	f := setup(t)
	run, _, _ := parkOnRender(t, f)
	if _, err := f.db.Exec(`UPDATE creative_agent_runs SET state='failed', error_code='creative_context_limit', finished_at=now(), updated_at=now() WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.hostFails(); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("completion for an ended run: %v", err)
	}
	if n := f.count("creative_scene_jobs", "run_id=$1 AND state='cancelled'", run.ID); n != 1 {
		t.Fatalf("job of an ended run still open: %v", f.runJobs(run.ID))
	}
}
