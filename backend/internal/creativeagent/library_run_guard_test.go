package creativeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
)

func TestLibraryPhaseRechecksRunAdmission(t *testing.T) {
	for _, mode := range []string{"cancelled", "lease_expired", "epoch_changed", "claim_changed", "deadline_expired", "consent_revoked"} {
		t.Run(mode, func(t *testing.T) {
			f, h, a := organizeFixture(t)
			raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"late write"}]}`, a.ID)
			c, err := h.startLibraryCall(withToolStep(t.Context(), organizeStep), organizeAssetsTool, raw)
			if err != nil {
				t.Fatal(err)
			}
			c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			want := ErrRunState
			switch mode {
			case "cancelled":
				_, err = f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": h.held.RunID}))
			case "lease_expired":
				_, err = f.db.Exec(`UPDATE creative_agent_runs SET lease_until=now()-interval '1 minute' WHERE account_id='agent-a' AND id=$1`, h.held.RunID)
			case "epoch_changed":
				_, err = f.db.Exec(`UPDATE creative_agent_runs SET execution_epoch=execution_epoch+1 WHERE account_id='agent-a' AND id=$1`, h.held.RunID)
			case "claim_changed":
				h.held.Token = "obsolete-claim"
			case "deadline_expired":
				h.held.Deadline = time.Now().Add(-time.Minute)
				want = llmgateway.ErrDeadline
			case "consent_revoked":
				_, err = f.service.RevokeConsent(t.Context(), f.alice, command(t, map[string]any{"consent_id": h.held.ConsentID, "expected_revision": "1"}))
				want = ErrConsentRevoked
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := h.runLibraryPhase(t.Context(), c, 0, 0); !errors.Is(err, want) {
				t.Errorf("invalidated run executed a new phase: got %v, want %v", err, want)
			}
			actual, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
			if err != nil || actual.Title != "原图" || actual.Revision != 1 {
				t.Fatalf("late effect committed: asset=%+v err=%v", actual, err)
			}
			if _, err := (creativeops.Executor{}).Lookup(t.Context(), f.alice, c.Progress[0][0].Command.OperationID); !errors.Is(err, creativeops.ErrNotFound) {
				t.Fatalf("rejected phase has a receipt: %v", err)
			}
		})
	}
}

func TestLibraryCancellationKeepsCommittedPhaseAndStopsRemainingWrites(t *testing.T) {
	f, h, a := organizeFixture(t)
	r, err := creativelibrary.CreateTag(t.Context(), f.alice, command(t, creativelibrary.TagInput{Name: "参考", Color: "#a1a1a7", HierarchyRevision: 1}))
	if err != nil {
		t.Fatal(err)
	}
	var tag creativelibrary.LibraryResult
	if err := json.Unmarshal(r.Outcome.Response, &tag); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"已提交标题","add_tag_ids":[%q]}]}`, a.ID, tag.ID)
	c, err := h.startLibraryCall(withToolStep(t.Context(), organizeStep), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The metadata commits, but its journal update has not happened yet.
	_, first, err := h.runLibraryPhase(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CancelRun(t.Context(), f.alice, command(t, map[string]any{"run_id": h.held.RunID})); err != nil {
		t.Fatal(err)
	}
	_, replay, err := h.runLibraryPhase(t.Context(), c, 0, 0)
	if err != nil || replay.OperationID != first.OperationID || !sameLibraryPayload(replay.Outcome.Response, first.Outcome.Response) {
		t.Fatalf("cancel hid committed receipt: %+v %v", replay, err)
	}
	// This is the still-running tool resuming after CancelRun, before its local
	// context observes the lease-renewal cancellation. New phases must stop.
	if _, err := h.runLibraryWrite(withToolStep(t.Context(), organizeStep), organizeAssetsTool, raw); err != nil {
		t.Fatal(err)
	}
	out, err := h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q}`, c.ID))
	if err != nil || !strings.Contains(out, `"status":"partial"`) {
		t.Fatalf("committed prefix not reconciled: %s %v", out, err)
	}
	stored, err := h.loadLibraryCall(t.Context(), c.ID)
	if err != nil || stored.State != "complete" || stored.Progress[0][0].Status != "applied" || stored.Progress[0][1].Status != "not_run" {
		t.Fatalf("wrong settled phases: %+v %v", stored.Progress, err)
	}
	actual, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
	if err != nil || actual.Title != "已提交标题" || actual.Revision != 2 || len(actual.TagIDs) != 0 {
		t.Fatalf("cancelled continuation wrote: %+v %v", actual, err)
	}
}
