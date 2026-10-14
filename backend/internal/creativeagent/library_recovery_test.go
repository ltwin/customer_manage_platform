package creativeagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

func TestLibrarySettledFirstPageCursorContinues(t *testing.T) {
	f, h, a := organizeFixture(t)
	raw := fmt.Sprintf(`{"items":[{"asset_id":%q,"expected_revision":"1","title":"已提交标题"}]}`, a.ID)
	c, err := h.startLibraryCall(t.Context(), organizeAssetsTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	command := *c.Progress[0][0].Command
	command.Guard = h.libraryGuard(c.ID, 0, 0)
	if _, err = executeLibraryPhase(t.Context(), f.alice, "metadata", command); err != nil {
		t.Fatal(err)
	}
	if _, err = h.unknownLibraryCall(t.Context(), c, fmt.Errorf("lost journal update")); err != nil {
		t.Fatal(err)
	}
	first, err := h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q,"limit":1}`, c.ID))
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Next  string `json:"next_cursor"`
		Total int    `json:"total_count"`
	}
	if err = json.Unmarshal([]byte(first), &page); err != nil {
		t.Fatal(err)
	}
	if page.Next == "" || page.Total != 2 {
		t.Fatalf("expected two details: %s", first)
	}
	_, err = h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q,"limit":1,"cursor":%q}`, c.ID, page.Next))
	if err != nil {
		t.Fatalf("first page completes reconciliation but next cursor rejects: %v; first=%s", err, first)
	}
}

func TestLibraryRetryObservesConcurrentSettlement(t *testing.T) {
	_, h, _ := organizeFixture(t)
	c, err := h.startLibraryCall(t.Context(), manageLibraryTool, `{"actions":[{"action":"create_group","name":"待创建"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	// This is the persisted retry boundary: Attempt advanced, command not frozen.
	c, err = h.changeLibraryCall(t.Context(), c.ID, func(current *libraryCall) error {
		current.Progress[0][0].Attempt++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.runLibraryPhase(t.Context(), c, 0, 0); !errors.Is(err, errLibraryFenced) {
		t.Fatal(err)
	}
	if _, err = h.unknownLibraryCall(t.Context(), c, errors.New("another executor stopped")); err != nil {
		t.Fatal(err)
	}
	if _, err = h.readLibraryResult(t.Context(), fmt.Sprintf(`{"result_id":%q}`, c.ID)); err != nil {
		t.Fatal(err)
	}
	c, err = h.freezeLibraryCommand(t.Context(), c, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != "complete" || c.Progress[0][0].Command != nil {
		t.Fatal("not at closed retry boundary")
	}
	if _, _, err = h.runLibraryPhase(t.Context(), c, 0, 0); !errors.Is(err, errLibraryFenced) {
		t.Fatalf("expected fenced exit: %v", err)
	}
}

func TestLibraryOldAttemptCannotOverwriteCommittedRetry(t *testing.T) {
	f, h, _ := organizeFixture(t)
	receipt, err := creativelibrary.CreateGroup(t.Context(), f.alice, command(t, creativelibrary.GroupInput{Name: "原组", HierarchyRevision: 1}))
	if err != nil {
		t.Fatal(err)
	}
	var group creativelibrary.LibraryResult
	if err = json.Unmarshal(receipt.Outcome.Response, &group); err != nil {
		t.Fatal(err)
	}
	raw := fmt.Sprintf(`{"actions":[{"action":"rename_group","group_id":%q,"expected_revision":"1","name":"新组"}]}`, group.ID)
	old, err := h.startLibraryCall(t.Context(), manageLibraryTool, raw)
	if err != nil {
		t.Fatal(err)
	}
	old, err = h.freezeLibraryCommand(t.Context(), old, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = creativelibrary.CreateTag(t.Context(), f.alice, command(t, creativelibrary.TagInput{Name: "并发结构", Color: "#a1a1a7", HierarchyRevision: group.HierarchyRevision})); err != nil {
		t.Fatal(err)
	}
	// A observes the old attempt's rollback, then B retries and commits without
	// yet journaling the result. A must not apply its obsolete conflict to B.
	cmd := *old.Progress[0][0].Command
	cmd.Guard = h.libraryGuard(old.ID, 0, 0)
	if _, err = executeLibraryPhase(t.Context(), f.alice, "rename_group", cmd); !errors.Is(err, creativelibrary.ErrVersionConflict) {
		t.Fatal(err)
	}
	newer, r, err := h.runLibraryPhase(t.Context(), old, 0, 0)
	if err != nil || newer.Progress[0][0].Attempt != 1 || r.OperationID == "" {
		t.Fatal("retry did not commit", err)
	}
	current, err := h.setLibraryPhase(t.Context(), old, 0, 0, "conflict", nil, "old failure", creativeops.Revision(1))
	if !errors.Is(err, errLibraryFenced) {
		t.Fatalf("old attempt changed new phase: %v", err)
	}
	output, err := h.unknownLibraryCall(t.Context(), current, err)
	if err != nil || !strings.Contains(output, `"status":"applied"`) {
		t.Fatal(output, err)
	}
	var name string
	if err = f.db.QueryRow(`SELECT name FROM creative_asset_groups WHERE account_id='agent-a' AND id=$1`, group.ID).Scan(&name); err != nil || name != "新组" {
		t.Fatal(name, err)
	}
	stored, err := h.loadLibraryCall(t.Context(), old.ID)
	if err != nil || stored.Progress[0][0].Message != "" || stored.Progress[0][0].CurrentRevision != 0 {
		t.Fatal("stale failure metadata persisted", err)
	}
}
