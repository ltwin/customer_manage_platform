package creativeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
)

func TestLibrarySameAttemptFailureCannotHideCommittedReceipt(t *testing.T) {
	testLibraryFailureCommitOrder(t, true)
}
func TestLibraryRecordedFailureFencesSameAttempt(t *testing.T) {
	testLibraryFailureCommitOrder(t, false)
}
func testLibraryFailureCommitOrder(t *testing.T, committedFirst bool) {
	t.Helper()
	f, h, a := organizeFixture(t)
	r, err := creativelibrary.CreateGroup(t.Context(), f.alice, command(t, creativelibrary.GroupInput{Name: "待归入", HierarchyRevision: 1}))
	if err != nil {
		t.Fatal(err)
	}
	var group creativelibrary.LibraryResult
	if err = json.Unmarshal(r.Outcome.Response, &group); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","add_group_ids":[%q]}]}`, a.ID, group.ID)
	c, err := h.startLibraryCall(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmd := *c.Progress[0][0].Command
	cmd.Guard = h.libraryGuard(c.ID, 0, 0)
	r, err = creativelibrary.DeleteGroup(t.Context(), f.alice, command(t, creativelibrary.GroupInput{ID: group.ID, ExpectedRevision: group.Revision, HierarchyRevision: group.HierarchyRevision}))
	if err != nil {
		t.Fatal(err)
	}
	var deletion creativelibrary.LibraryResult
	if err = json.Unmarshal(r.Outcome.Response, &deletion); err != nil {
		t.Fatal(err)
	}
	// A fails after the group was deleted, then pauses before persisting failure.
	_, failure := executeLibraryPhase(t.Context(), f.alice, "organize", cmd)
	if !errors.Is(failure, creativelibrary.ErrNotFound) {
		t.Fatalf("expected missing group, got %v", failure)
	}
	if !committedFirst {
		if _, err = h.setLibraryPhase(t.Context(), c, 0, 0, libraryFailure(failure), nil, failure.Error(), 1); err != nil {
			t.Fatal(err)
		}
	}
	// The photographer undoes deletion. The target asset's revision has not changed.
	if _, err = creativelibrary.RestoreGroup(t.Context(), f.alice, command(t, creativelibrary.RestoreGroupInput{DeletionID: deletion.DeletionID})); err != nil {
		t.Fatal(err)
	}
	// B resumes the same frozen attempt and commits, then pauses before its journal update.
	receipt, err := executeLibraryPhase(t.Context(), f.alice, "organize", cmd)
	if !committedFirst {
		if !errors.Is(err, errLibraryFenced) {
			t.Fatalf("late same-attempt execution not fenced: %v", err)
		}
		actual, readErr := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
		if readErr != nil || len(actual.GroupIDs) != 0 {
			t.Fatal("late effect committed", readErr)
		}
		out, readErr := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
		if readErr != nil || !strings.Contains(out, `"status":"not_found"`) {
			t.Fatal(out, readErr)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	// A records its earlier business failure. B then records its actual receipt.
	current, err := h.setLibraryPhase(t.Context(), c, 0, 0, libraryFailure(failure), nil, failure.Error(), 1)
	if err != nil {
		t.Fatal(err)
	}
	current, err = h.setLibraryPhase(t.Context(), c, 0, 0, "applied", receipt.Outcome.Response, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if current.Progress[0][0].Status != "applied" {
		t.Errorf("committed same-attempt receipt hidden by %s", current.Progress[0][0].Status)
	}
	out, err := h.runLibraryWrite(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := creativelibrary.GetAsset(t.Context(), f.alice, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual.GroupIDs) != 1 || actual.GroupIDs[0] != group.ID {
		t.Fatal("expected actual committed group relation")
	}
	if !strings.Contains(out, `"status":"applied"`) {
		t.Fatalf("asset has committed relation, final summary=%s", out)
	}
}
