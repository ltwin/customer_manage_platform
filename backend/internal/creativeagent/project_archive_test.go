package creativeagent

import (
	"errors"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
)

func (f *fixture) setProjectArchived(archived bool) {
	f.t.Helper()
	value := "NULL"
	if archived {
		value = "clock_timestamp()"
	}
	if _, err := f.db.Exec(`UPDATE creative_projects SET archived_at=` + value + `,revision=revision+1 WHERE id='ccpj_a'`); err != nil {
		f.t.Fatal(err)
	}
}

// Every entry that puts a run back to work checks the project under its row
// lock (design 2026-10-11-creative-home §3.4).
func TestArchivedProjectRefusesNewRuns(t *testing.T) {
	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	consent := f.consent(f.alice, c.ID)
	f.setProjectArchived(true)
	if _, err := f.startRun(f.alice, c.ID, consent.ID, InstructionSegment{Type: "text", Text: "整理"}); !errors.Is(err, creativecanvas.ErrArchived) {
		t.Fatalf("run started in an archived project: %v", err)
	}
	if n := f.count("creative_agent_runs", "TRUE"); n != 0 {
		t.Fatalf("runs=%d", n)
	}
}

// A waiting run may sit in an archived project, but it continues only after
// the project is restored (owner 2026-10-11, D6).
func TestWaitingRunInArchivedProjectContinuesOnlyAfterRestore(t *testing.T) {
	f := setup(t)
	run, held, paused := pauseCheckpointRun(t, f, false)
	if err := f.service.completeAttempt(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	waiting, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || waiting.State != RunWaitingInput {
		t.Fatalf("waiting=%+v %v", waiting, err)
	}
	f.setProjectArchived(true)
	input := map[string]any{"run_id": run.ID, "expected_revision": waiting.Revision, "waiting_token": waiting.WaitingToken, "text": "补充要求"}
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, input)); !errors.Is(err, creativecanvas.ErrArchived) {
		t.Fatalf("supplement resumed a run in an archived project: %v", err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingInput {
		t.Fatalf("state=%s", got.State)
	}
	f.setProjectArchived(false)
	if _, err = f.service.SupplementRun(t.Context(), f.alice, command(t, input)); err != nil {
		t.Fatal(err)
	}
}

// Archiving and resuming take the same project row: a supplement that arrives
// while an archive is in flight waits for it and then sees the archive.
func TestSupplementWaitsForAnArchiveInFlight(t *testing.T) {
	f := setup(t)
	run, held, paused := pauseCheckpointRun(t, f, false)
	if err := f.service.completeAttempt(t.Context(), f.alice, held, paused.outcome, errRunInterrupted); err != nil {
		t.Fatal(err)
	}
	waiting, err := f.service.ReadRun(t.Context(), f.alice, run.ID)
	if err != nil || waiting.State != RunWaitingInput {
		t.Fatalf("waiting=%+v %v", waiting, err)
	}
	archive, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Rollback() }()
	if _, err = archive.Exec(`SELECT 1 FROM creative_projects WHERE id='ccpj_a' FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		input := map[string]any{"run_id": run.ID, "expected_revision": waiting.Revision, "waiting_token": waiting.WaitingToken, "text": "补充要求"}
		_, err := f.service.SupplementRun(t.Context(), f.alice, command(t, input))
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("supplement did not wait for the project lock: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err = archive.Exec(`UPDATE creative_projects SET archived_at=clock_timestamp(),revision=revision+1 WHERE id='ccpj_a'`); err != nil {
		t.Fatal(err)
	}
	if err = archive.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, creativecanvas.ErrArchived) {
		t.Fatalf("supplement after archive: %v", err)
	}
	if got, _ := f.service.ReadRun(t.Context(), f.alice, run.ID); got.State != RunWaitingInput {
		t.Fatalf("state=%s", got.State)
	}
}
