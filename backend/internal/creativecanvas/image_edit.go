package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"

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

var (
	ErrImageEditSourceChanged = errors.New("creative image edit source changed")
	ErrImageEditNodeBusy      = errors.New("creative image edit active execution")
)

// ImageEditTarget is a checked target tied to the caller's transaction. Its
// fields cannot be supplied by clients. The account write barrier and canvas
// lock remain held from PrepareImageEditInTx through ApplyInTx.
type ImageEditTarget struct {
	tx     store.TxAccountScope
	canvas Canvas
	nodeID string
	source string
}

// PrepareImageEditInTx is the single adoption guard. Source changes take
// precedence over data conflicts, so a cleared/replaced image is not mistaken
// for a title edit. A pure metadata conflict can retry the same ready render.
func PrepareImageEditInTx(ctx context.Context, tx store.TxAccountScope, target NodeTarget, source string) (*ImageEditTarget, error) {
	if target.CanvasID == "" || !validNodeID(target.NodeID) || target.ExpectedDataRevision < 1 || source == "" {
		return nil, creativeops.ErrValidation
	}
	c, err := lockCanvas(ctx, tx, target.CanvasID, true)
	if err != nil {
		return nil, err
	}
	n, err := scanNode(tx.QueryRowForUpdate(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", target.NodeID, target.CanvasID))
	if err != nil {
		return nil, err
	}
	if n.TypeKey != MediaTypeKey("image") || n.TypeVersion != 1 || n.ContentRevisionID == nil || *n.ContentRevisionID != source {
		return nil, ErrImageEditSourceChanged
	}
	if n.ActiveExecutionID != nil {
		return nil, ErrImageEditNodeBusy
	}
	if n.DataRevision != target.ExpectedDataRevision {
		return nil, ErrVersionConflict
	}
	return &ImageEditTarget{tx: tx, canvas: c, nodeID: n.ID, source: source}, nil
}

// ApplyInTx persists a retained revision using the already locked target. No
// target locks or validation reads are repeated. Graph loading supplies the
// before/after state needed for undo and graph heads.
func (t *ImageEditTarget) ApplyInTx(ctx context.Context, revision creativecontent.Revision, provenance json.RawMessage, operation string) (string, ChangeResult, error) {
	if revision.Kind != "image" {
		return "", ChangeResult{}, creativeops.ErrValidation
	}
	tx, c := t.tx, t.canvas
	before, ids, err := loadGraph(ctx, tx, c.ID)
	if err != nil {
		return "", ChangeResult{}, err
	}
	after := cloneGraph(before)
	n := after.Nodes[t.nodeID]
	version, err := makeVersion(ctx, tx, n.ID, revision.ID, "edit", provenance, []VersionInput{{RevisionID: t.source, Role: "source"}})
	if err != nil {
		return "", ChangeResult{}, err
	}
	after.Versions[version.ID] = version
	n.ContentID = &revision.ContentID
	n.ContentRevisionID = &revision.ID
	n.SelectedVersionID = &version.ID
	fitMediaSize(&n, revision)
	after.Nodes[n.ID] = n
	changes, err := planChanges(before, after, ids)
	if err != nil {
		return "", ChangeResult{}, err
	}
	result, err := persistGraph(ctx, tx, c, before, after, ids, changes, operation, newChangeID(), "", "", nil, nil)
	return version.ID, result, err
}
