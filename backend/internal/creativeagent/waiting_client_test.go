package creativeagent

import (
	"errors"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// parkOnClient pauses a run after its tool step, marks that step as still
// waiting on a page, and parks the run the way a client tool does.
func parkOnClient(t *testing.T, f *fixture) Run {
	t.Helper()
	run, _, _ := pauseCheckpointRun(t, f, true)
	if _, err := f.db.Exec(`UPDATE creative_agent_steps SET state='prepared', output=NULL, finished_at=NULL WHERE run_id=$1 AND kind='tool'`, run.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		c, err := f.service.lockControlledRun(t.Context(), tx, run.ID)
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(t.Context())
		if err != nil {
			return err
		}
		return f.service.waitForClient(t.Context(), tx, c, now)
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *fixture) sweepRuns() {
	f.t.Helper()
	if _, err := f.service.SweepRuns(f.t.Context(), f.alice, 10); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) slotHolder() string {
	f.t.Helper()
	var run *string
	if err := f.db.QueryRow(`SELECT run_id FROM creative_agent_slots WHERE account_id='agent-a'`).Scan(&run); err != nil {
		f.t.Fatal(err)
	}
	if run == nil {
		return ""
	}
	return *run
}

func TestWaitingClientParksAndResumesWhenItsStepsSettle(t *testing.T) {
	f := setup(t)
	run := parkOnClient(t, f)
	parked, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || parked.State != RunWaitingClient || parked.WaitReason != waitClientJob || parked.WaitingToken == "" {
		t.Fatalf("parked=%+v %v", parked, err)
	}
	if holder := f.slotHolder(); holder != "" {
		t.Fatalf("a parked run kept the slot: %s", holder)
	}
	// A photographer's message cannot wake a run that waits on a page.
	input := map[string]any{"run_id": run.ID, "expected_revision": parked.Revision, "waiting_token": parked.WaitingToken, "text": "补充要求"}
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, input)); !errors.Is(err, ErrRunState) {
		t.Fatalf("supplement woke a waiting_client run: %v", err)
	}
	f.sweepRuns()
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingClient || got.Revision != parked.Revision {
		t.Fatalf("the sweep moved a run whose step still waits: %+v", got)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_steps SET state='succeeded', output='{}' WHERE run_id=$1 AND kind='tool'`, run.ID); err != nil {
		t.Fatal(err)
	}
	f.sweepRuns()
	resumed, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || resumed.State != RunQueued || resumed.WaitReason != "" || resumed.WaitingToken != "" {
		t.Fatalf("resumed=%+v %v", resumed, err)
	}
	if holder := f.slotHolder(); holder != run.ID {
		t.Fatalf("slot held by %q", holder)
	}
}

func TestWaitingClientWaitsForABusySlot(t *testing.T) {
	f := setup(t)
	run := parkOnClient(t, f)
	if _, err := f.db.Exec(`UPDATE creative_agent_steps SET state='succeeded', output='{}' WHERE run_id=$1 AND kind='tool'`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_agent_slots SET run_id='ccrn_other', claim_token='other', acquired_at=now() WHERE account_id='agent-a'`); err != nil {
		t.Fatal(err)
	}
	f.sweepRuns()
	busy, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || busy.State != RunWaitingClient || busy.WaitReason != waitSlotBusy {
		t.Fatalf("busy=%+v %v", busy, err)
	}
	// Still busy: the reason is already told, nothing changes.
	f.sweepRuns()
	if again, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); again.Revision != busy.Revision {
		t.Fatalf("a repeated busy sweep bumped the run: %d -> %d", busy.Revision, again.Revision)
	}
	if _, err = f.db.Exec(`UPDATE creative_agent_slots SET run_id=NULL, claim_token=NULL, acquired_at=NULL WHERE account_id='agent-a'`); err != nil {
		t.Fatal(err)
	}
	f.sweepRuns()
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunQueued || got.WaitReason != "" {
		t.Fatalf("after the slot freed: %+v", got)
	}
}

func TestCancellingAWaitingClientRunCancelsItsJobs(t *testing.T) {
	f := setup(t)
	run := parkOnClient(t, f)
	typ, ok := creativedocument.Lookup(creativescene.TypeKey)
	if !ok {
		t.Fatal("scene type not registered")
	}
	var job creativescene.Job
	if err := f.alice.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		doc, err := creativedocument.CreateInTx(t.Context(), tx, typ, "中殿", nil, creativedocument.Photographer)
		if err != nil {
			return err
		}
		job, err = creativescene.CreateJob(t.Context(), tx, creativescene.NewJob{DocumentID: doc.ID, Kind: creativescene.KindRenderView, RunID: run.ID, ToolCallID: "reused-id",
			Request: creativescene.RenderRequest{Preset: "overview", Mode: "clay"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": run.ID})); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunCancelled {
		t.Fatalf("state=%s", got.State)
	}
	if n := f.count("creative_scene_jobs", "id=$1 AND state='cancelled'", job.ID); n != 1 {
		t.Fatalf("job not cancelled with its run")
	}
}
