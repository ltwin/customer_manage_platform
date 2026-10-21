package creativescene

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativeglb"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativestaging"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// saveModelOperationKey files a model instance's model into the library.
const saveModelOperationKey = "library.create_asset_from_scene_model"

// SaveModelInput is the payload of POST /creative/assets/from-scene-model.
type SaveModelInput struct {
	DocumentID        string                       `json:"document_id"`
	ObjectID          string                       `json:"object_id"`
	ExpectedObjectRev creativeops.Revision         `json:"expected_object_rev"`
	ThumbnailUploadID string                       `json:"thumbnail_upload_id"`
	Target            creativelibrary.ImportTarget `json:"target"`
}

// SaveModelToLibrary copies the model an instance references, with the
// instance's materials, into the asset library: the version the photographer
// saw, so the instance's content unit must still be at expected_object_rev.
// The copy is new content the scene never writes to; its transform stays in
// the scene. The scene is read, never locked or written. A live asset copied
// from the same revision with the same materials only gains the chosen groups.
func SaveModelToLibrary(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var in SaveModelInput
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: saveModelOperationKey, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if in.DocumentID == "" || !objectIDPattern.MatchString(in.ObjectID) || in.ExpectedObjectRev < 1 || in.ThumbnailUploadID == "" {
				return invalid("缺少场景、对象、expected_object_rev 或缩略图")
			}
			return creativelibrary.ValidateImportTarget(in.Target)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			// Lock order: library root, the scene (read only), the source model,
			// the staged thumbnail, then the copy and its asset.
			if err := creativelibrary.ValidateImportTargetInTx(ctx, tx, in.Target); err != nil {
				return creativeops.Outcome{}, err
			}
			head, err := creativedocument.ReadHeadInTx(ctx, tx, in.DocumentID, TypeKey)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			if head.TrashedAt != nil {
				return creativeops.Outcome{}, creativedocument.ErrTrashed
			}
			cur, err := creativedocument.ReadAtInTx(ctx, tx, in.DocumentID, TypeKey, head.Revision)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			scene, err := decodeScene(cur.Content.Payload)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			i, ok := indexObjects(scene.Objects)[in.ObjectID]
			if !ok {
				return creativeops.Outcome{}, creativedocument.ErrNotFound
			}
			obj := scene.Objects[i]
			if obj.Kind != KindModelInstance || obj.Model == nil {
				return creativeops.Outcome{}, invalid("对象 %s 不是模型实例", in.ObjectID)
			}
			unit := contentUnit(in.ObjectID)
			if got, exists := cur.UnitRevs[unit]; !exists || creativeops.Revision(got) != in.ExpectedObjectRev {
				expected := in.ExpectedObjectRev
				c := creativedocument.Conflict{Unit: unit, ExpectedRev: &expected}
				if exists {
					current := creativeops.Revision(got)
					c.CurrentRev = &current
				}
				return creativeops.Outcome{}, &creativedocument.ConflictError{Conflicts: []creativedocument.Conflict{c}}
			}
			src, err := usableModel(ctx, tx, obj.Model.ContentRevisionID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			p, _ := completeModel(src)
			source, materials := src.ID, assignedSlots(p.Manifest, obj.MaterialSlots)
			if p.SourceRevisionID != "" {
				source = p.SourceRevisionID
			}
			key, err := json.Marshal(materials)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			// Before the upload is taken: an asset already in the library needs
			// no new thumbnail, so saving it again works after the upload expired.
			hit, found, err := creativelibrary.FindModelAssetInTx(ctx, tx, source, key)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			var result creativelibrary.AssetResult
			if found {
				r, err := usableModel(ctx, tx, hit)
				if err != nil {
					return creativeops.Outcome{}, err
				}
				result, err = creativelibrary.SaveRevisionInTx(ctx, tx, r, in.Target)
				if err != nil {
					return creativeops.Outcome{}, err
				}
			} else if result, err = writeLibraryCopy(ctx, tx, src, source, key, in); err != nil {
				return creativeops.Outcome{}, err
			}
			status := 201
			if result.Reused {
				status = 200
			}
			body, err := json.Marshal(result)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			id, revision := result.ID, result.Revision
			return creativeops.Outcome{HTTPStatus: status, ResultKind: "asset", ResultID: &id, ResultRevision: &revision, Response: body}, nil
		}}, c)
}

// assignedSlots are an instance's materials for the slots its model has. A
// command checks slots by shape only, so a slot the model lost may linger.
func assignedSlots(m creativeglb.Manifest, slots map[string]MaterialSlot) map[string]MaterialSlot {
	out := map[string]MaterialSlot{}
	for _, s := range m.Slots {
		if v, ok := slots[s.ID]; ok {
			out[s.ID] = v
		}
	}
	return out
}

// writeLibraryCopy writes the library copy of src, its payload plus the
// materials and source, over src's GLB and the uploaded thumbnail, and creates
// the asset that holds it.
func writeLibraryCopy(ctx context.Context, tx store.TxAccountScope, src creativecontent.Revision, source string, materials json.RawMessage, in SaveModelInput) (creativelibrary.AssetResult, error) {
	thumb, thumbStaged, err := lockStaged(ctx, tx, in.ThumbnailUploadID, "image")
	if err != nil {
		return creativelibrary.AssetResult{}, err
	}
	glbBlob, glbOK := original(src)
	thumbBlob, thumbOK := original(thumbStaged)
	if !thumbOK || thumbBlob.Mime != "image/png" {
		return creativelibrary.AssetResult{}, invalid("缩略图须为 PNG 图片")
	}
	if !glbOK {
		return creativelibrary.AssetResult{}, modelUnavailable(src.ID)
	}
	// The rest of the payload is copied as stored, code included.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(src.Payload.Model, &fields); err != nil {
		return creativelibrary.AssetResult{}, err
	}
	if fields["source_revision_id"], err = json.Marshal(source); err != nil {
		return creativelibrary.AssetResult{}, err
	}
	fields["materials"] = materials
	payload, err := json.Marshal(fields)
	if err != nil {
		return creativelibrary.AssetResult{}, err
	}
	draft := creativecontent.Draft{Kind: modelKind, DeclarationID: src.DeclarationID, Payload: creativecontent.Payload{Model: payload}}
	objects := []creativecontent.ObjectBinding{{Role: "original", BlobID: glbBlob.BlobID}, {Role: "thumbnail", BlobID: thumbBlob.BlobID}}
	var result creativelibrary.AssetResult
	if _, err := creativecontent.WriteModelAndRetain(ctx, tx, draft, "", objects, func(r creativecontent.Revision) error {
		var err error
		result, err = creativelibrary.CreateFromRevisionInTx(ctx, tx, r, in.Target)
		return err
	}); err != nil {
		return creativelibrary.AssetResult{}, err
	}
	return result, creativestaging.ConsumeForAsset(ctx, tx, thumb, result.ID)
}
