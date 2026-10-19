package creativecanvas

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Scene exports land as image nodes beside the scene node. The scene package
// decides what to place and writes based_on; this file owns where the nodes
// go and the single canvas change that creates them.

const (
	exportCellWidth    = 300
	exportGap          = 40
	exportOffset       = 80
	exportMaxNudges    = 50
	sceneNodeTypeKey   = "core.scene"
	exportBasedOnKey   = "based_on"
	exportImageTypeKey = "core.image"
)

// ErrExportUploadUnavailable is a staged export image that is missing,
// expired, already used or not an image. Nothing is placed.
var ErrExportUploadUnavailable = errors.New("creative canvas: staged export image unavailable")

// SceneExportImage is one export image: its staged upload, title, column
// (the pass's position among the export's passes) and based_on without
// content_revision_id, which placement adds from the staged upload.
type SceneExportImage struct {
	UploadID string
	Title    string
	Column   int
	BasedOn  map[string]any
}

// SceneExportRow is one camera: its images and frame aspect (width / height).
type SceneExportRow struct {
	Aspect float64
	Images []SceneExportImage
}

type SceneExportPlacement struct {
	CanvasID    string
	SceneNodeID string
	DocumentID  string
	Rows        []SceneExportRow
	Actor       creativedocument.Actor
	// Operation is recorded as the canvas change's operation.
	Operation string
}

type PlacedExportImage struct {
	NodeID            string
	ContentRevisionID string
}

type SceneExportPlaced struct {
	ChangeID string
	// Rows mirrors SceneExportPlacement.Rows.
	Rows [][]PlacedExportImage
}

// PlaceSceneExportsInTx creates every export image node in one canvas change
// under the canvas lock, so two exports never land on the same spot. The
// scene node must be on the canvas and hold the scene document.
func PlaceSceneExportsInTx(ctx context.Context, tx store.TxAccountScope, in SceneExportPlacement) (SceneExportPlaced, error) {
	c, err := lockCanvas(ctx, tx, in.CanvasID, true)
	if err != nil {
		return SceneExportPlaced{}, err
	}
	g, ids, err := loadGraph(ctx, tx, c.ID)
	if err != nil {
		return SceneExportPlaced{}, err
	}
	scene, ok := g.Nodes[in.SceneNodeID]
	if !ok || scene.TypeKey != sceneNodeTypeKey || scene.DocumentID == nil || *scene.DocumentID != in.DocumentID {
		return SceneExportPlaced{}, ErrNotFound
	}
	// Lock the uploads first: canvas edits report an unavailable upload as a
	// version conflict, and export callers need to tell it apart.
	revisions := map[string]string{}
	for _, row := range in.Rows {
		for _, img := range row.Images {
			s, err := creativestaging.Lock(ctx, tx, img.UploadID)
			if errors.Is(err, creativestaging.ErrUnavailable) || err == nil && s.Kind != "image" {
				return SceneExportPlaced{}, ErrExportUploadUnavailable
			}
			if err != nil {
				return SceneExportPlaced{}, err
			}
			revisions[img.UploadID] = s.ContentRevisionID
		}
	}

	x, y := exportOrigin(g, scene, in.Rows)
	var actions []Action
	out := SceneExportPlaced{Rows: make([][]PlacedExportImage, len(in.Rows))}
	rowY := y
	for i, row := range in.Rows {
		for _, img := range row.Images {
			id := "cwnode_" + uuid.NewString()
			basedOn := make(map[string]any, len(img.BasedOn)+1)
			for k, v := range img.BasedOn {
				basedOn[k] = v
			}
			basedOn["content_revision_id"] = revisions[img.UploadID]
			actions = append(actions,
				Action{Type: "add_node", NodeID: id, TypeKey: exportImageTypeKey, Title: img.Title, ParentID: scene.ParentID,
					X: x + float64(img.Column)*(exportCellWidth+exportGap), Y: rowY, Config: map[string]any{exportBasedOnKey: basedOn}},
				Action{Type: "fill_from_upload", NodeID: id, UploadID: img.UploadID})
			out.Rows[i] = append(out.Rows[i], PlacedExportImage{NodeID: id, ContentRevisionID: revisions[img.UploadID]})
		}
		rowY += exportCellWidth/row.Aspect + exportGap
	}
	// The new nodes' parent chain may grow to fit them, so it is read here.
	var reads []ObjectRead
	for p := scene.ParentID; p != nil; p = g.Nodes[*p].ParentID {
		ident := ids[*p]
		reads = append(reads, ObjectRead{Kind: "node", ID: *p, PlacementRevision: ident.P, DataRevision: ident.D})
	}
	actor := in.Actor
	if actor.Kind == "" {
		actor = creativedocument.Photographer
	}
	r, err := applyBatchAs(ctx, tx, BatchInput{CanvasID: c.ID, ExpectedTopologyRevision: c.TopologyRevision, ReadSet: reads, Actions: actions}, in.Operation, actor)
	if err != nil {
		return SceneExportPlaced{}, err
	}
	out.ChangeID = r.ChangeID
	return out, nil
}

// exportOrigin is the top-left corner of the export block: right of the scene
// node, moved below whatever sibling it would overlap.
func exportOrigin(g graphState, scene Node, rows []SceneExportRow) (float64, float64) {
	cols := 0
	height := 0.0
	for i, row := range rows {
		for _, img := range row.Images {
			cols = max(cols, img.Column+1)
		}
		height += exportCellWidth / row.Aspect
		if i > 0 {
			height += exportGap
		}
	}
	width := float64(cols)*exportCellWidth + float64(max(cols-1, 0))*exportGap
	x, y := scene.X+scene.Width+exportOffset, scene.Y
	for range exportMaxNudges {
		bottom, hit := math.Inf(-1), false
		for _, n := range g.Nodes {
			if !sameParent(n.ParentID, scene.ParentID) {
				continue
			}
			if n.X < x+width && x < n.X+n.Width && n.Y < y+height && y < n.Y+n.Height {
				hit = true
				bottom = max(bottom, n.Y+n.Height)
			}
		}
		if !hit {
			break
		}
		y = bottom + exportGap
	}
	return x, y
}

func sameParent(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ExportSource is the based_on of one export image node whose current content
// is still the exported image.
type ExportSource struct {
	NodeID         string
	DocumentID     string
	SceneRevision  string
	CameraObjectID string
	Pass           string
	Fingerprint    string
}

// ExportFreshness tells whether export images still match their scene. The
// scene package implements it; cmd/server registers it at start-up so this
// package never imports the scene package. current holds the current
// snapshot of every usable source document. Check returns current or stale
// for each node.
type ExportFreshness interface {
	Check(ctx context.Context, tx store.ReadTxAccountScope, sources []ExportSource, current map[string]creativecontent.Revision) (map[string]string, error)
}

var exportFreshness ExportFreshness

// SetExportFreshness registers the freshness check; call it once at start-up.
func SetExportFreshness(f ExportFreshness) { exportFreshness = f }

const (
	BasedOnCurrent     = "current"
	BasedOnStale       = "stale"
	BasedOnUnavailable = "unavailable"
)

// exportSource reads n's based_on; ok is false when the node has none or its
// content is no longer the exported image.
func exportSource(n Node) (ExportSource, bool) {
	if n.TypeKey != exportImageTypeKey || n.ContentRevisionID == nil || len(n.Config) == 0 {
		return ExportSource{}, false
	}
	var cfg struct {
		BasedOn *struct {
			SceneDocumentID   string `json:"scene_document_id"`
			SceneRevision     string `json:"scene_revision"`
			CameraObjectID    string `json:"camera_object_id"`
			Pass              string `json:"pass"`
			ContentRevisionID string `json:"content_revision_id"`
			Fingerprint       string `json:"fingerprint"`
		} `json:"based_on"`
	}
	if json.Unmarshal(n.Config, &cfg) != nil || cfg.BasedOn == nil || cfg.BasedOn.ContentRevisionID != *n.ContentRevisionID || cfg.BasedOn.SceneDocumentID == "" {
		return ExportSource{}, false
	}
	b := cfg.BasedOn
	return ExportSource{NodeID: n.ID, DocumentID: b.SceneDocumentID, SceneRevision: b.SceneRevision, CameraObjectID: b.CameraObjectID, Pass: b.Pass, Fingerprint: b.Fingerprint}, true
}

// fillBasedOn sets BasedOnState on export image nodes. documents already
// holds every source document (fillNodeViews reads them with the others).
func fillBasedOn(ctx context.Context, tx store.ReadTxAccountScope, nodes map[string]*Node, sources []ExportSource, documents map[string]creativedocument.NodeSummary) error {
	if len(sources) == 0 || exportFreshness == nil {
		return nil
	}
	current := map[string]creativecontent.Revision{}
	var usable []ExportSource
	for _, s := range sources {
		d := documents[s.DocumentID]
		if !d.Exists || d.Trashed || !d.Usable || d.TypeKey != "scene" {
			nodes[s.NodeID].BasedOnState = BasedOnUnavailable
			continue
		}
		current[s.DocumentID] = d.Content
		usable = append(usable, s)
	}
	if len(usable) == 0 {
		return nil
	}
	states, err := exportFreshness.Check(ctx, tx, usable, current)
	if err != nil {
		return err
	}
	for _, s := range usable {
		nodes[s.NodeID].BasedOnState = states[s.NodeID]
	}
	return nil
}

// DocumentNodeInTx is the node on the canvas holding the document, the
// earliest created when there are several; ErrNotFound when there is none.
func DocumentNodeInTx(ctx context.Context, tx store.TxAccountScope, canvasID, documentID string) (string, error) {
	rows, err := tx.QueryPage(ctx, "creative_nodes", "id", "canvas_id=$2 AND document_id=$3", []store.OrderBy{{Column: "created_at"}, {Column: "id"}}, 1, 0, canvasID, documentID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", err
		}
		return "", ErrNotFound
	}
	var id string
	if err := rows.Scan(&id); err != nil {
		return "", err
	}
	return id, rows.Err()
}
