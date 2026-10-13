package creativecanvas

import (
	"context"

	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ImageEditSourceInTx locks the project and canvas for writing and returns the
// image node's current revision, the only source an edit may start from. A
// node with an active execution is refused: its content is about to change.
func ImageEditSourceInTx(ctx context.Context, tx store.TxAccountScope, canvasID, nodeID string) (string, error) {
	if canvasID == "" || !validNodeID(nodeID) {
		return "", creativeops.ErrValidation
	}
	if _, err := lockCanvas(ctx, tx, canvasID, true); err != nil {
		return "", err
	}
	n, err := scanNode(tx.QueryRowForUpdate(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", nodeID, canvasID))
	if err != nil {
		return "", err
	}
	if n.TypeKey != MediaTypeKey("image") || n.TypeVersion != 1 || n.ContentRevisionID == nil {
		return "", creativeops.ErrValidation
	}
	if n.ActiveExecutionID != nil {
		return "", ErrVersionConflict
	}
	return *n.ContentRevisionID, nil
}
