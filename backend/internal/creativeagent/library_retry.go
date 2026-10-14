package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
)

// Only a proven rolled-back hierarchy conflict may get a new attempt. Object
// revisions and all requested values remain fixed; unknown outcomes never retry.
func (h *runtimeHandler) runLibraryPhase(ctx context.Context, c libraryCall, i, j int) (libraryCall, creativeops.Receipt, error) {
	for {
		state := c.Progress[i][j]
		if c.State != "executing" || state.Status != "pending" || state.Command == nil {
			return c, creativeops.Receipt{}, errLibraryFenced
		}
		command := *state.Command
		command.Guard = h.libraryGuard(c.ID, i, j)
		receipt, err := executeLibraryPhase(ctx, h.scope, c.Plan.Items[i].Phases[j].Name, command)
		if !errors.Is(err, creativelibrary.ErrVersionConflict) || c.Tool != manageLibraryTool || state.Attempt >= conflictRetries {
			return c, receipt, err
		}
		canRetry, checkErr := h.libraryHierarchyChanged(ctx, c, i, j)
		if checkErr != nil {
			return c, receipt, checkErr
		}
		if !canRetry {
			return c, receipt, err
		}
		c, checkErr = h.changeLibraryCall(ctx, c.ID, func(current *libraryCall) error {
			if current.State != "executing" {
				return errLibraryFenced
			}
			next := &current.Progress[i][j]
			if next.Status != "pending" || next.Command == nil || next.Command.OperationID != command.OperationID {
				return errLibraryFenced
			}
			next.Attempt++
			next.Command = nil
			return nil
		})
		if checkErr != nil {
			return c, receipt, checkErr
		}
		c, checkErr = h.freezeLibraryCommand(ctx, c, i, j)
		if checkErr != nil {
			return c, receipt, checkErr
		}
	}
}
func (h *runtimeHandler) libraryHierarchyChanged(ctx context.Context, c libraryCall, i, j int) (bool, error) {
	settings, err := creativelibrary.GetSettings(ctx, h.scope)
	if err != nil {
		return false, err
	}
	var input struct {
		Hierarchy creativeops.Revision `json:"hierarchy_revision"`
		Expected  creativeops.Revision `json:"expected_revision"`
	}
	if err = json.Unmarshal(c.Progress[i][j].Command.Payload, &input); err != nil {
		return false, err
	}
	if settings.HierarchyRevision == input.Hierarchy {
		return false, nil
	}
	item := c.Plan.Items[i]
	if strings.HasPrefix(item.Action, "create_") {
		return true, nil
	}
	table := "creative_tags"
	if strings.HasSuffix(item.Action, "group") {
		table = "creative_asset_groups"
	}
	if strings.HasSuffix(item.Action, "category") {
		table = "creative_tag_categories"
	}
	var revision creativeops.Revision
	err = h.scope.QueryRow(ctx, table, "revision", "id=$2", item.ID).Scan(&revision)
	if err != nil {
		return false, nil
	} // A missing/unreadable object is never grounds for retry.
	return revision == input.Expected, nil
}
