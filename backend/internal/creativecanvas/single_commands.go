package creativecanvas

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func singleOutcome(ctx context.Context, tx store.TxAccountScope, v BatchInput, operation, nodeID string) (creativeops.Outcome, error) {
	result, err := applyBatch(ctx, tx, v, operation)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	n, err := scanNode(tx.QueryRow(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", nodeID, v.CanvasID))
	if err != nil {
		return creativeops.Outcome{}, err
	}
	body := struct {
		NodeResult
		ChangeID      string         `json:"change_id"`
		ObjectResults []ObjectResult `json:"object_results"`
	}{NodeResult: NodeResult{NodeID: n.ID, CanvasRevision: result.ResultRevision, BeforeTopologyRevision: result.BeforeTopologyRevision, TopologyRevision: result.ResultTopologyRevision, PlacementRevision: n.PlacementRevision, DataRevision: n.DataRevision, ContentRevisionID: n.ContentRevisionID}, ChangeID: result.ChangeID, ObjectResults: result.ObjectResults}
	return outcome(200, "node", n.ID, result.ResultRevision, body)
}
func AddNode(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.add_node", c, func(v AddNodeInput) error {
		if v.CanvasID == "" || !validNodeID(v.NodeID) || !validPoint(v.X, v.Y) || v.ExpectedTopologyRevision < 1 || utf8.RuneCountInString(v.Title) > 200 || !knownType(v.TypeKey) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v AddNodeInput) (creativeops.Outcome, error) {
		return singleOutcome(ctx, tx, BatchInput{CanvasID: v.CanvasID, ExpectedTopologyRevision: v.ExpectedTopologyRevision, Actions: []Action{{Type: "add_node", NodeID: v.NodeID, TypeKey: v.TypeKey, Title: v.Title, X: v.X, Y: v.Y, Asset: v.Asset, Content: v.Content}}}, c.OperationID, v.NodeID)
	}, requirePosition)
}
func MoveNode(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.move_node", c, func(v MoveNodeInput) error {
		if v.CanvasID == "" || v.NodeID == "" || v.ExpectedPlacementRevision < 1 || !validPoint(v.X, v.Y) {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v MoveNodeInput) (creativeops.Outcome, error) {
		return singleOutcome(ctx, tx, BatchInput{CanvasID: v.CanvasID, ReadSet: []ObjectRead{{Kind: "node", ID: v.NodeID, PlacementRevision: v.ExpectedPlacementRevision}}, Actions: []Action{{Type: "move_node", NodeID: v.NodeID, X: v.X, Y: v.Y}}}, c.OperationID, v.NodeID)
	}, requirePosition)
}
func ReplaceContent(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "canvas.replace_content", c, func(v ReplaceContentInput) error {
		if v.CanvasID == "" || v.NodeID == "" || v.ExpectedDataRevision < 1 {
			return creativeops.ErrValidation
		}
		return nil
	}, func(ctx context.Context, tx store.TxAccountScope, v ReplaceContentInput) (creativeops.Outcome, error) {
		if _, err := lockCanvas(ctx, tx, v.CanvasID, true); err != nil {
			return creativeops.Outcome{}, err
		}
		n, err := scanNode(tx.QueryRow(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", v.NodeID, v.CanvasID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if (n.ContentRevisionID == nil) != (v.ExpectedContentRevisionID == nil) || n.ContentRevisionID != nil && *n.ContentRevisionID != *v.ExpectedContentRevisionID {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		return singleOutcome(ctx, tx, BatchInput{CanvasID: v.CanvasID, ReadSet: []ObjectRead{{Kind: "node", ID: v.NodeID, DataRevision: v.ExpectedDataRevision}}, Actions: []Action{{Type: "replace_content", NodeID: v.NodeID, Payload: &v.Payload, Rights: v.ContentRights}}}, c.OperationID, v.NodeID)
	}, func(raw json.RawMessage) error { return requireContentPointer(raw) })
}
