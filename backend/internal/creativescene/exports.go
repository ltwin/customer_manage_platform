package creativescene

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// Exports: the render host renders and stages the images; the photographer's
// export (POST scene-exports) and export_shots' completion both place them
// through placeExportsInTx (design「放置流程」).

const (
	exportOperationKey = "creative.scene_export"
	maxExportShots     = 32
	maxShotImages      = 3
	// maxExportImages keeps one export within a canvas change: each image is
	// a node and a version, and a change touches at most 100 objects.
	maxExportImages = 48
)

var passTitles = map[string]string{PassClay: "clay", PassLine: "线稿", PassLit: "材质光照"}

type ExportImage struct {
	Pass     string `json:"pass"`
	UploadID string `json:"upload_id"`
}

type ExportShot struct {
	CameraObjectID string        `json:"camera_object_id"`
	Images         []ExportImage `json:"images"`
}

// ExportInput is the export request's payload; DocumentID comes from the path.
type ExportInput struct {
	DocumentID    string               `json:"document_id"`
	CanvasID      string               `json:"canvas_id"`
	SceneNodeID   string               `json:"scene_node_id"`
	SceneRevision creativeops.Revision `json:"scene_revision"`
	Shots         []ExportShot         `json:"shots"`
}

type ExportNode struct {
	CameraObjectID    string `json:"camera_object_id"`
	Pass              string `json:"pass"`
	NodeID            string `json:"node_id"`
	ContentRevisionID string `json:"content_revision_id"`
}

// ExportResult is the export response and export_shots' job result.
type ExportResult struct {
	ChangeID      string               `json:"change_id"`
	SceneRevision creativeops.Revision `json:"scene_revision"`
	Nodes         []ExportNode         `json:"nodes"`
}

// validateShots checks the shape: 1–32 cameras without repeats, 1–3 images
// each without a repeated pass, at most 48 images in all.
func validateShots(shots []ExportShot) error {
	if len(shots) == 0 || len(shots) > maxExportShots {
		return invalid("shots 须为 1 到 %d 台摄像机", maxExportShots)
	}
	cameras := map[string]bool{}
	total := 0
	for _, s := range shots {
		if s.CameraObjectID == "" || cameras[s.CameraObjectID] {
			return invalid("摄像机 %q 缺失或重复", s.CameraObjectID)
		}
		cameras[s.CameraObjectID] = true
		if len(s.Images) == 0 || len(s.Images) > maxShotImages {
			return invalid("摄像机 %s 须有 1 到 %d 张图", s.CameraObjectID, maxShotImages)
		}
		passes := map[string]bool{}
		for _, img := range s.Images {
			if !slices.Contains(Passes, img.Pass) || passes[img.Pass] || img.UploadID == "" {
				return invalid("摄像机 %s 的导出方式 %q 不合法、重复或缺少上传", s.CameraObjectID, img.Pass)
			}
			passes[img.Pass] = true
		}
		total += len(s.Images)
	}
	if total > maxExportImages {
		return creativeops.Explain(creativedocument.ErrSizeLimit, "一次最多导出 %d 张图，现有 %d 张", maxExportImages, total)
	}
	return nil
}

// Export places a photographer's export images on the canvas.
func Export(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var in ExportInput
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: exportOperationKey, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if in.DocumentID == "" || in.CanvasID == "" || in.SceneNodeID == "" || in.SceneRevision < 1 {
				return invalid("缺少场景、画布、场景节点或 scene_revision")
			}
			return validateShots(in.Shots)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			res, err := placeExportsInTx(ctx, tx, in, creativedocument.Photographer, c.OperationID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			body, err := json.Marshal(res)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			id := res.ChangeID
			return creativeops.Outcome{HTTPStatus: 200, Response: body, ResultKind: "canvas_change", ResultID: &id}, nil
		}}, c)
}

// placeExportsInTx checks the export against the revision it was rendered
// from, fingerprints every image and places them all in one canvas change
// written by actor. The scene is read, never locked.
func placeExportsInTx(ctx context.Context, tx store.TxAccountScope, in ExportInput, actor creativedocument.Actor, operation string) (ExportResult, error) {
	if err := validateShots(in.Shots); err != nil {
		return ExportResult{}, err
	}
	head, err := creativedocument.ReadHeadInTx(ctx, tx, in.DocumentID, TypeKey)
	if err != nil {
		return ExportResult{}, err
	}
	if head.TrashedAt != nil {
		return ExportResult{}, creativedocument.ErrTrashed
	}
	if int64(in.SceneRevision) > head.Revision {
		return ExportResult{}, invalid("scene_revision %d 晚于当前修订 %d", in.SceneRevision, head.Revision)
	}
	cur, err := creativedocument.ReadAtInTx(ctx, tx, in.DocumentID, TypeKey, int64(in.SceneRevision))
	if errors.Is(err, creativedocument.ErrRevisionNotFound) {
		return ExportResult{}, invalid("场景修订 %d 不存在", in.SceneRevision)
	}
	if err != nil {
		return ExportResult{}, err
	}
	scene, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return ExportResult{}, err
	}
	byID := indexObjects(scene.Objects)

	// Columns follow the passes this export uses, in clay, line, lit order.
	used := map[string]bool{}
	for _, s := range in.Shots {
		for _, img := range s.Images {
			used[img.Pass] = true
		}
	}
	var columns []string
	for _, p := range Passes {
		if used[p] {
			columns = append(columns, p)
		}
	}
	rows := make([]creativecanvas.SceneExportRow, 0, len(in.Shots))
	for _, s := range in.Shots {
		i, ok := byID[s.CameraObjectID]
		if !ok || scene.Objects[i].Kind != KindCamera {
			return ExportResult{}, invalid("摄像机 %s 在修订 %d 中不存在", s.CameraObjectID, in.SceneRevision)
		}
		cam := scene.Objects[i]
		name := *cam.Camera.Label
		if name == "" {
			name = cam.Name
		}
		if name == "" {
			name = "摄像机"
		}
		images := slices.Clone(s.Images)
		slices.SortFunc(images, func(a, b ExportImage) int {
			return slices.Index(Passes, a.Pass) - slices.Index(Passes, b.Pass)
		})
		row := creativecanvas.SceneExportRow{Aspect: cameraAspect(*cam.Camera)}
		for _, img := range images {
			fp, _ := Fingerprint(scene, s.CameraObjectID, img.Pass)
			row.Images = append(row.Images, creativecanvas.SceneExportImage{
				UploadID: img.UploadID,
				Title:    name + " · " + passTitles[img.Pass],
				Column:   slices.Index(columns, img.Pass),
				BasedOn: map[string]any{
					"scene_document_id": in.DocumentID,
					"scene_revision":    in.SceneRevision,
					"camera_object_id":  s.CameraObjectID,
					"pass":              img.Pass,
					"fingerprint":       fp,
				},
			})
		}
		rows = append(rows, row)
	}
	placed, err := creativecanvas.PlaceSceneExportsInTx(ctx, tx, creativecanvas.SceneExportPlacement{
		CanvasID: in.CanvasID, SceneNodeID: in.SceneNodeID, DocumentID: in.DocumentID, Rows: rows, Actor: actor, Operation: operation,
	})
	switch {
	case errors.Is(err, creativecanvas.ErrExportUploadUnavailable):
		return ExportResult{}, creativeops.Explain(ErrUploadUnavailable, "导出图的上传不可用，请重新导出")
	case errors.Is(err, creativecanvas.ErrLimit):
		return ExportResult{}, creativeops.Explain(creativedocument.ErrSizeLimit, "画布节点已到上限，放不下这次导出")
	case err != nil:
		return ExportResult{}, err
	}
	out := ExportResult{ChangeID: placed.ChangeID, SceneRevision: in.SceneRevision, Nodes: []ExportNode{}}
	for i, row := range rows {
		for j, img := range row.Images {
			p := placed.Rows[i][j]
			out.Nodes = append(out.Nodes, ExportNode{CameraObjectID: in.Shots[i].CameraObjectID, Pass: img.BasedOn["pass"].(string), NodeID: p.NodeID, ContentRevisionID: p.ContentRevisionID})
		}
	}
	return out, nil
}

// cameraAspect is the camera's frame width over height.
func cameraAspect(c Camera) float64 {
	w, h, ok := strings.Cut(*c.Aspect, ":")
	if !ok {
		return 1.5
	}
	var a, b float64
	if json.Unmarshal([]byte(w), &a) != nil || json.Unmarshal([]byte(h), &b) != nil || a <= 0 || b <= 0 {
		return 1.5
	}
	return a / b
}
