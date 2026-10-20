package creativescene

import (
	"context"
	"encoding/json"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// saveModelOperationKey files a model instance's revision into the library.
const saveModelOperationKey = "library.create_asset_from_scene_model"

// SaveModelInput is the payload of POST /creative/assets/from-scene-model.
type SaveModelInput struct {
	DocumentID        string                       `json:"document_id"`
	ObjectID          string                       `json:"object_id"`
	ExpectedObjectRev creativeops.Revision         `json:"expected_object_rev"`
	Target            creativelibrary.ImportTarget `json:"target"`
}

// SaveModelToLibrary files the model revision an instance references into the
// asset library, the same revision the photographer saw: the instance's content
// unit must still be at expected_object_rev. The scene is read, never locked or
// written; a revision already in the library only gains the chosen groups.
func SaveModelToLibrary(ctx context.Context, scope store.AccountScope, c creativeops.Command) (creativeops.Receipt, error) {
	var in SaveModelInput
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: saveModelOperationKey, Capability: "manual_write",
		Validate: func(raw json.RawMessage) error {
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if in.DocumentID == "" || !objectIDPattern.MatchString(in.ObjectID) || in.ExpectedObjectRev < 1 {
				return invalid("缺少场景、对象或 expected_object_rev")
			}
			return creativelibrary.ValidateImportTarget(in.Target)
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			// Lock order: library root, then the scene (read only), then content.
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
			r, err := usableModel(ctx, tx, obj.Model.ContentRevisionID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			result, err := creativelibrary.SaveRevisionInTx(ctx, tx, r, in.Target)
			if err != nil {
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
