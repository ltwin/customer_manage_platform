package creativeimageedit

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrRenderNotReady = errors.New("creative image edit render not ready")
	ErrExpired        = errors.New("creative image edit render expired or discarded")
)

type ApplyInput struct {
	CanvasID                 string               `json:"canvas_id"`
	NodeID                   string               `json:"node_id"`
	RenderID                 string               `json:"render_id"`
	ExpectedSourceRevisionID string               `json:"expected_source_revision_id"`
	ExpectedDataRevision     creativeops.Revision `json:"expected_data_revision"`
}

type ApplyResult struct {
	creativecanvas.ChangeResult
	VersionID         string `json:"version_id"`
	ContentRevisionID string `json:"content_revision_id"`
}

// Apply is synchronous: the receipt, quota, rights, node version and render
// transition are one transaction. Receipt replay precedes mutable source and
// candidate checks, including after undo, expiry or later edits.
func (s *Service) Apply(ctx context.Context, scope store.AccountScope, command creativeops.Command) (creativeops.Receipt, error) {
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{
		Key: "canvas.apply_image_edit", Capability: "media_write",
		Validate: func(raw json.RawMessage) error {
			var in ApplyInput
			if err := creativeops.Decode(raw, &in); err != nil {
				return err
			}
			if in.CanvasID == "" || in.NodeID == "" || in.RenderID == "" || in.ExpectedSourceRevisionID == "" || in.ExpectedDataRevision < 1 {
				return creativeops.ErrValidation
			}
			return nil
		},
		Apply: func(ctx context.Context, tx store.TxAccountScope, raw json.RawMessage) (creativeops.Outcome, error) {
			var in ApplyInput
			if err := creativeops.Decode(raw, &in); err != nil {
				return creativeops.Outcome{}, err
			}
			result, err := s.applyInTx(ctx, tx, in, command.OperationID)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			return creativeops.Outcome{HTTPStatus: 200, ResultKind: "image_edit", ResultID: &result.VersionID, ResultRevision: &result.ResultRevision, Response: creativegraph.Canonical(result)}, nil
		},
	}, command)
}

func (s *Service) applyInTx(ctx context.Context, tx store.TxAccountScope, in ApplyInput, operation string) (ApplyResult, error) {
	fail := func(err error) (ApplyResult, error) { return ApplyResult{}, err }
	sourceID := in.ExpectedSourceRevisionID
	target, err := creativecanvas.PrepareImageEditInTx(ctx, tx, creativecanvas.NodeTarget{CanvasID: in.CanvasID, NodeID: in.NodeID, ExpectedDataRevision: in.ExpectedDataRevision}, sourceID)
	if errors.Is(err, creativecanvas.ErrImageEditSourceChanged) {
		return fail(ErrSourceChanged)
	}
	if errors.Is(err, creativecanvas.ErrImageEditNodeBusy) {
		return fail(ErrNodeBusy)
	}
	if err != nil {
		return fail(err)
	}
	r, err := readRender(ctx, tx, true, "id=$2", in.RenderID)
	if err != nil {
		return fail(err)
	}
	if r.CanvasID != in.CanvasID || r.NodeID != in.NodeID || r.SourceRevisionID != sourceID {
		return fail(ErrSourceChanged)
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return fail(err)
	}
	if r.State == "expired" || r.State == "discarded" || (r.active() && !now.Before(r.ExpiresAt)) {
		return fail(ErrExpired)
	}
	if r.State == "not_reduced" {
		return fail(ErrNotReduced)
	}
	if r.State == "failed" && r.ErrorCode == "colour_unknown" {
		return fail(ErrColour)
	}
	if r.State != "ready" || r.original == nil || r.Output == nil {
		return fail(ErrRenderNotReady)
	}
	source, err := creativecontent.RequireUsable(ctx, tx, sourceID, "display")
	if err != nil {
		return fail(err)
	}
	display := ""
	if r.display != nil {
		display = *r.display
	}
	provenance := creativegraph.Canonical(map[string]any{"action": r.Action, "parameters": r.Parameters, "source_revision_id": sourceID, "render_id": r.ID, "encoder_policy": r.encoderPolicy, "caller_kind": r.callerKind, "output": r.Output, "source_size": r.SourceSize})
	var result ApplyResult
	revision, err := s.media.BindEditBlobsInTx(ctx, tx, source, *r.original, display, func(revision creativecontent.Revision) error {
		var err error
		result.VersionID, result.ChangeResult, err = target.ApplyInTx(ctx, revision, provenance, operation)
		return err
	})
	if errors.Is(err, creativemedia.ErrExpired) {
		return fail(ErrExpired)
	}
	if err != nil {
		return fail(err)
	}
	result.ContentRevisionID = revision.ID
	_, err = tx.Update(ctx, "creative_image_edit_renders", "state='applied',applied_operation_id=$3,result_revision_id=$4,applied_version_id=$5,change_id=$6,revision=revision+1,updated_at=clock_timestamp()", "id=$2", r.ID, operation, revision.ID, result.VersionID, result.ChangeID)
	return result, err
}
