package creativecanvas

import (
	"context"
	"errors"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ErrTargetChanged reports a classifiable binding conflict so the media
// coordinator can turn the publication into a candidate instead of failing.
var ErrTargetChanged = errors.New("creative node target changed")

// NodeTarget is the canvas-side publication target of an upload.
type NodeTarget struct {
	CanvasID             string               `json:"canvas_id"`
	NodeID               string               `json:"node_id"`
	ExpectedDataRevision creativeops.Revision `json:"expected_data_revision"`
}

// ValidateNodeTargetInTx locks the project/canvas and checks the node can
// still receive media of the given kind at the expected data revision.
// The caller must already hold library-side locks when it needs them.
func ValidateNodeTargetInTx(ctx context.Context, tx store.TxAccountScope, t NodeTarget, kind string) error {
	if t.CanvasID == "" || !validNodeID(t.NodeID) || t.ExpectedDataRevision < 1 || !creativecontent.IsMediaKind(kind) {
		return creativeops.ErrValidation
	}
	_, err := lockCanvas(ctx, tx, t.CanvasID, true)
	if errors.Is(err, ErrArchived) || errors.Is(err, ErrNotFound) {
		return ErrTargetChanged
	}
	if err != nil {
		return err
	}
	n, err := scanNode(tx.QueryRowForUpdate(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", t.NodeID, t.CanvasID))
	if errors.Is(err, ErrNotFound) {
		return ErrTargetChanged
	}
	if err != nil {
		return err
	}
	if n.TypeKey != MediaTypeKey(kind) || n.TypeVersion != 1 || n.DataRevision != t.ExpectedDataRevision || n.ActiveExecutionID != nil {
		return ErrTargetChanged
	}
	return nil
}

// BindNodeRevisionInTx installs an already retained media revision as the
// node's content, recording a reversible change under the caller's operation.
func BindNodeRevisionInTx(ctx context.Context, tx store.TxAccountScope, t NodeTarget, r creativecontent.Revision, operation string) (ChangeResult, error) {
	if err := ValidateNodeTargetInTx(ctx, tx, t, r.Kind); err != nil {
		return ChangeResult{}, err
	}
	c, err := lockCanvas(ctx, tx, t.CanvasID, true)
	if err != nil {
		return ChangeResult{}, err
	}
	before, ids, err := loadGraph(ctx, tx, c.ID)
	if err != nil {
		return ChangeResult{}, err
	}
	after := cloneGraph(before)
	n := after.Nodes[t.NodeID]
	n.ContentID = &r.ContentID
	n.ContentRevisionID = &r.ID
	n.SelectedVersionID = nil
	fitMediaSize(&n, r)
	after.Nodes[n.ID] = n
	changes, err := planChanges(before, after, ids)
	if err != nil {
		return ChangeResult{}, err
	}
	return persistGraph(ctx, tx, c, before, after, ids, changes, operation, newChangeID(), "", "", nil, nil)
}

// SaveNodeInput saves a node's current content into the personal library.
type SaveNodeInput struct {
	CanvasID             string                       `json:"canvas_id"`
	NodeID               string                       `json:"node_id"`
	ExpectedDataRevision creativeops.Revision         `json:"expected_data_revision"`
	Target               creativelibrary.ImportTarget `json:"target"`
}

// SaveNodeToLibrary fixes the node's current revision as a new asset. Library
// root, then project/canvas, then content: the shared lock order.
func SaveNodeToLibrary(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	return run(ctx, scope, "library.create_asset_from_node", c, func(v SaveNodeInput) error {
		if v.CanvasID == "" || !validNodeID(v.NodeID) || v.ExpectedDataRevision < 1 || strings.TrimSpace(v.Target.Title) == "" || utf8.RuneCountInString(v.Target.Title) > 200 {
			return creativeops.ErrValidation
		}
		return creativelibrary.ValidateImportTarget(v.Target)
	}, func(ctx context.Context, tx store.TxAccountScope, v SaveNodeInput) (creativeops.Outcome, error) {
		if err := creativelibrary.ValidateImportTargetInTx(ctx, tx, v.Target); err != nil {
			return creativeops.Outcome{}, err
		}
		if _, err := lockCanvas(ctx, tx, v.CanvasID, false); err != nil {
			return creativeops.Outcome{}, err
		}
		n, err := scanNode(tx.QueryRow(ctx, "creative_nodes", nodeColumns, "id=$2 AND canvas_id=$3", v.NodeID, v.CanvasID))
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if d, ok := definition(n.TypeKey); !ok || !strings.HasPrefix(n.TypeKey, "core.") || n.TypeKey == "core.group" || d.SchemaVersion != n.TypeVersion {
			return creativeops.Outcome{}, creativeops.ErrValidation
		}
		if n.DataRevision != v.ExpectedDataRevision {
			return creativeops.Outcome{}, ErrVersionConflict
		}
		if n.ContentRevisionID == nil {
			return creativeops.Outcome{}, creativeops.ErrValidation
		}
		r, err := creativecontent.RequireUsable(ctx, tx, *n.ContentRevisionID, "display")
		if err != nil {
			return creativeops.Outcome{}, err
		}
		if n.TypeKey != "core."+r.Kind {
			return creativeops.Outcome{}, creativecontent.ErrMissingRoot
		}
		result, err := creativelibrary.CreateFromRevisionInTx(ctx, tx, r, v.Target)
		if err != nil {
			return creativeops.Outcome{}, err
		}
		return outcome(201, "asset", result.ID, result.Revision, result)
	})
}

// Default node box and the chrome a media card keeps around its picture
// (41px header + 33px footer, per workspace.css). A node the photographer has
// never resized adopts the media's aspect ratio so the whole picture shows.
const (
	defaultNodeWidth  = 280.0
	defaultNodeHeight = 180.0
	mediaChromeHeight = 74.0
	minMediaHeight    = 140.0
	maxMediaHeight    = 640.0
)

func fitMediaSize(n *Node, r creativecontent.Revision) {
	if n.Width != defaultNodeWidth || n.Height != defaultNodeHeight || r.Kind == "audio" {
		return
	}
	for _, m := range r.Media {
		if m.Role != "original" || m.Width == nil || m.Height == nil || *m.Width <= 0 || *m.Height <= 0 {
			continue
		}
		n.Height = math.Round(math.Min(maxMediaHeight, math.Max(minMediaHeight, mediaChromeHeight+defaultNodeWidth*float64(*m.Height)/float64(*m.Width))))
		return
	}
}
