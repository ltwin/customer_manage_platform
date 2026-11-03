package creativescene

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
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
			obj, _, err := savedObject(ctx, tx, in.DocumentID, in.ObjectID, in.ExpectedObjectRev, func(o Object) bool { return o.Kind == KindModelInstance && o.Model != nil }, "模型实例")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			src, err := usableModel(ctx, tx, obj.Model.ContentRevisionID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			p, _ := completeModel(src)
			source, materials := src.ID, assignedSlots(effectiveSlotIDs(slotIDs(p.Manifest), p.Faces), obj.MaterialSlots)
			if p.SourceRevisionID != "" {
				source = p.SourceRevisionID
			}
			// The scene's reference rows hold its textures, under no lock of
			// ours; each is checked again before the copy holds it.
			uses := slotTextures(materials, -1, "materials")
			if err := verifyImages(ctx, tx, uses, func(string) bool { return true }); err != nil {
				return creativeops.Outcome{}, err
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
			} else if result, err = writeLibraryCopy(ctx, tx, src, source, key, usedImages(uses), in); err != nil {
				return creativeops.Outcome{}, err
			}
			return assetOutcome(result)
		}}, c)
}

// assetOutcome is the receipt of saving into the library: 201 for a new
// asset, 200 when an existing one only gained the chosen groups.
func assetOutcome(result creativelibrary.AssetResult) (creativeops.Outcome, error) {
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
}

// savedObject reads the object a save into the library copies, from the
// scene's current revision, read and never locked: it must be of the kind
// fits names, and its content unit still at expected, the version the
// photographer saw.
func savedObject(ctx context.Context, tx store.TxAccountScope, documentID, objectID string, expected creativeops.Revision, fits func(Object) bool, kind string) (Object, creativedocument.Current, error) {
	head, err := creativedocument.ReadHeadInTx(ctx, tx, documentID, TypeKey)
	if err != nil {
		return Object{}, creativedocument.Current{}, err
	}
	if head.TrashedAt != nil {
		return Object{}, creativedocument.Current{}, creativedocument.ErrTrashed
	}
	cur, err := creativedocument.ReadAtInTx(ctx, tx, documentID, TypeKey, head.Revision)
	if err != nil {
		return Object{}, creativedocument.Current{}, err
	}
	scene, err := decodeScene(cur.Content.Payload)
	if err != nil {
		return Object{}, creativedocument.Current{}, err
	}
	i, ok := indexObjects(scene.Objects)[objectID]
	if !ok {
		return Object{}, creativedocument.Current{}, creativedocument.ErrNotFound
	}
	obj := scene.Objects[i]
	if !fits(obj) {
		return Object{}, creativedocument.Current{}, invalid("对象 %s 不是%s", objectID, kind)
	}
	unit := contentUnit(objectID)
	if got, exists := cur.UnitRevs[unit]; !exists || creativeops.Revision(got) != expected {
		c := creativedocument.Conflict{Unit: unit, ExpectedRev: &expected}
		if exists {
			current := creativeops.Revision(got)
			c.CurrentRev = &current
		}
		return Object{}, creativedocument.Current{}, &creativedocument.ConflictError{Conflicts: []creativedocument.Conflict{c}}
	}
	return obj, cur, nil
}

// assignedSlots are an instance's materials for the slots its model has, the
// GLB's and those its face assignment brings. A command checks slots by shape
// only, so a slot the model lost may linger.
func assignedSlots(ids []string, slots map[string]MaterialSlot) map[string]MaterialSlot {
	out := map[string]MaterialSlot{}
	for _, id := range ids {
		if v, ok := slots[id]; ok {
			out[id] = v
		}
	}
	return out
}

// writeLibraryCopy writes the library copy of src, its payload plus the
// materials and source, over src's GLB and the uploaded thumbnail, and creates
// the asset that holds it. The copy holds the textures its materials use.
func writeLibraryCopy(ctx context.Context, tx store.TxAccountScope, src creativecontent.Revision, source string, materials json.RawMessage, textures []string, in SaveModelInput) (creativelibrary.AssetResult, error) {
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
		if err := creativecontent.HoldTextures(ctx, tx, r.ID, textures); err != nil {
			return err
		}
		var err error
		result, err = creativelibrary.CreateFromRevisionInTx(ctx, tx, r, in.Target)
		return err
	}); err != nil {
		return creativelibrary.AssetResult{}, err
	}
	return result, creativestaging.ConsumeForAsset(ctx, tx, thumb, result.ID)
}

// saveMaterialOperationKey files one slot's material into the library.
const saveMaterialOperationKey = "library.create_asset_from_scene_material"

// ErrMaterialSlotEmpty is a slot with no material of its own: it renders a
// hint or the default, so there is nothing to save (400
// creative_material_slot_empty).
var ErrMaterialSlotEmpty = errors.New("creative scene: material slot empty")

// SlotEmptyError names the slot; errors.Is matches ErrMaterialSlotEmpty.
type SlotEmptyError struct{ ObjectID, SlotID string }

func (e *SlotEmptyError) Error() string {
	return fmt.Sprintf("%v: %s %s", ErrMaterialSlotEmpty, e.ObjectID, e.SlotID)
}
func (e *SlotEmptyError) Is(target error) bool  { return target == ErrMaterialSlotEmpty }
func (e *SlotEmptyError) PublicMessage() string { return "这个槽位还没有指定材质" }

// MaterialPayload is payload.material of material content (data: 材质资产):
// a slot's whole assignment, and where it came from, for display only.
type MaterialPayload struct {
	Slot   MaterialSlot   `json:"slot"`
	Source MaterialSource `json:"source"`
}

// MaterialSource says which slot of which scene revision a material was
// saved from; it is no reference, and outlives the scene.
type MaterialSource struct {
	DocumentID string               `json:"document_id"`
	ObjectID   string               `json:"object_id"`
	SlotID     string               `json:"slot_id"`
	Revision   creativeops.Revision `json:"revision"`
}

// SaveMaterialInput is the payload of POST /creative/assets/from-scene-material.
type SaveMaterialInput struct {
	DocumentID        string                       `json:"document_id"`
	ObjectID          string                       `json:"object_id"`
	SlotID            string                       `json:"slot_id"`
	ExpectedObjectRev creativeops.Revision         `json:"expected_object_rev"`
	ThumbnailUploadID string                       `json:"thumbnail_upload_id"`
	Target            creativelibrary.ImportTarget `json:"target"`
}

// SaveMaterialToLibrary copies one slot's material, as the photographer saw
// it, into the asset library: new material content with one revision, which
// holds the slot's textures, and the asset that holds it. Placing it later
// copies the slot back; the two never stay linked (I7). A live material
// asset with exactly this slot only gains the chosen groups. The scene is
// read, never locked or written.
func SaveMaterialToLibrary(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var in SaveMaterialInput
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: saveMaterialOperationKey, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if in.DocumentID == "" || !objectIDPattern.MatchString(in.ObjectID) || !slotKeyPattern.MatchString(in.SlotID) || in.ExpectedObjectRev < 1 || in.ThumbnailUploadID == "" {
				return invalid("缺少场景、对象、槽位、expected_object_rev 或缩略图")
			}
			return creativelibrary.ValidateImportTarget(in.Target)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			// Lock order: library root, the scene (read only), the staged
			// thumbnail, then the material and its asset.
			if err := creativelibrary.ValidateImportTargetInTx(ctx, tx, in.Target); err != nil {
				return creativeops.Outcome{}, err
			}
			obj, cur, err := savedObject(ctx, tx, in.DocumentID, in.ObjectID, in.ExpectedObjectRev, func(o Object) bool { return o.Kind == KindPrimitive || o.Kind == KindModelInstance }, "基础体或模型实例")
			if err != nil {
				return creativeops.Outcome{}, err
			}
			slot, ok := obj.MaterialSlots[in.SlotID]
			if !ok {
				return creativeops.Outcome{}, &SlotEmptyError{ObjectID: in.ObjectID, SlotID: in.SlotID}
			}
			// The scene's reference rows hold the textures; each must still be
			// displayable before the material holds it too.
			uses := textureUses(slot, -1, "")
			if err := verifyImages(ctx, tx, uses, func(string) bool { return true }); err != nil {
				return creativeops.Outcome{}, err
			}
			key, err := json.Marshal(slot)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			// Before the upload is taken, as for models.
			hit, found, err := creativelibrary.FindMaterialAssetInTx(ctx, tx, key)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			var result creativelibrary.AssetResult
			if found {
				r, err := creativecontent.RequireUsable(ctx, tx, hit, "display")
				if err != nil {
					return creativeops.Outcome{}, err
				}
				if result, err = creativelibrary.SaveRevisionInTx(ctx, tx, r, in.Target); err != nil {
					return creativeops.Outcome{}, err
				}
			} else {
				source := MaterialSource{DocumentID: in.DocumentID, ObjectID: in.ObjectID, SlotID: in.SlotID, Revision: creativeops.Revision(cur.Revision)}
				if result, err = writeMaterial(ctx, tx, MaterialPayload{Slot: slot, Source: source}, usedImages(uses), in); err != nil {
					return creativeops.Outcome{}, err
				}
			}
			return assetOutcome(result)
		}}, c)
}

// writeMaterial writes the material over the uploaded thumbnail, holding its
// textures, and creates the asset that holds it.
func writeMaterial(ctx context.Context, tx store.TxAccountScope, p MaterialPayload, textures []string, in SaveMaterialInput) (creativelibrary.AssetResult, error) {
	thumb, thumbStaged, err := lockStaged(ctx, tx, in.ThumbnailUploadID, "image")
	if err != nil {
		return creativelibrary.AssetResult{}, err
	}
	thumbBlob, ok := original(thumbStaged)
	if !ok || thumbBlob.Mime != "image/png" {
		return creativelibrary.AssetResult{}, invalid("缩略图须为 PNG 图片")
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return creativelibrary.AssetResult{}, err
	}
	draft := creativecontent.Draft{Kind: materialKind, Payload: creativecontent.Payload{Material: payload}}
	var result creativelibrary.AssetResult
	if _, err := creativecontent.WriteMaterialAndRetain(ctx, tx, draft, []creativecontent.ObjectBinding{{Role: "thumbnail", BlobID: thumbBlob.BlobID}}, func(r creativecontent.Revision) error {
		if err := creativecontent.HoldTextures(ctx, tx, r.ID, textures); err != nil {
			return err
		}
		var err error
		result, err = creativelibrary.CreateFromRevisionInTx(ctx, tx, r, in.Target)
		return err
	}); err != nil {
		return creativelibrary.AssetResult{}, err
	}
	return result, creativestaging.ConsumeForAsset(ctx, tx, thumb, result.ID)
}
