package planningmedia

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// ShareDisplayPermitRequest is the trusted share-tx request for a moodboard
// display ContentPermit. Callers must already have validated the share grant.
type ShareDisplayPermitRequest struct {
	PlanID          string
	BindingID       string
	AssetID         string
	ExactGeneration int
	DisplayChecksum string
}

type liveDisplayPermit struct {
	key        string
	expected   immutablefs.Metadata
	mediaType  string
	checksum   string
	size       int64
	width      int
	height     int
	pinID      string
	scope      store.AccountScope
	assetID    string
	generation int
}

// IssueShareDisplayPermit creates a response-lifetime read pin for an active
// moodboard_display binding inside a sealed share transaction. The returned
// ContentPermit stays pending until FinalizeShareDisplayPermit runs after the
// share transaction commits.
func (a *Application) IssueShareDisplayPermit(
	ctx context.Context,
	tx store.TxAccountScope,
	req ShareDisplayPermitRequest,
) (ContentPermit, error) {
	if a == nil || a.objects == nil {
		return ContentPermit{}, ErrNotFound
	}
	if req.PlanID == "" || req.BindingID == "" || req.AssetID == "" ||
		req.ExactGeneration < 1 || req.DisplayChecksum == "" {
		return ContentPermit{}, ErrNotFound
	}

	asset, err := a.repo.LockAsset(ctx, tx, req.AssetID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ContentPermit{}, ErrNotFound
		}
		return ContentPermit{}, err
	}
	if asset.UploadContextPlanID != req.PlanID {
		return ContentPermit{}, ErrNotFound
	}
	switch asset.State {
	case AssetCorrupt:
		return ContentPermit{}, ErrAssetCorrupt
	case AssetGCPending:
		return ContentPermit{}, ErrAssetGCPending
	case AssetDeleted:
		return ContentPermit{}, ErrNotFound
	}

	binding, err := a.repo.LockBinding(ctx, tx, req.BindingID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ContentPermit{}, ErrNotFound
		}
		return ContentPermit{}, err
	}
	if binding.State != BindingActive ||
		binding.PlanID != req.PlanID ||
		binding.AssetID != req.AssetID ||
		binding.Generation != req.ExactGeneration ||
		binding.Purpose != PurposeMoodboardDisplay ||
		binding.HolderKind != HolderPlan ||
		binding.HolderID != req.PlanID {
		return ContentPermit{}, ErrNotFound
	}

	var key, mime, checksum string
	var size int64
	var width, height int
	if err := tx.QueryRow(ctx, "planning_media_renditions",
		"internal_object_key,mime_type,byte_size,width,height,checksum",
		"asset_id = $2 AND generation = $3 AND kind = 'display'",
		req.AssetID, req.ExactGeneration,
	).Scan(&key, &mime, &size, &width, &height, &checksum); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return ContentPermit{}, ErrNotFound
		}
		return ContentPermit{}, err
	}
	if checksum != req.DisplayChecksum {
		return ContentPermit{}, ErrAssetReferenceStale
	}

	rights, err := a.repo.RightsForGeneration(ctx, tx, req.AssetID, req.ExactGeneration)
	if err != nil {
		return ContentPermit{}, err
	}
	if err := ValidatePurpose(rights, PurposeMoodboardDisplay); err != nil {
		return ContentPermit{}, ErrNotFound
	}

	pinID := uuid.NewString()
	now := a.now().UTC()
	if err := tx.Insert(ctx, "planning_media_read_pins",
		[]string{
			"id", "asset_id", "generation", "authorization_anchor", "binding_id",
			"upload_context_plan_id", "state", "expires_at", "created_at",
		},
		pinID, req.AssetID, req.ExactGeneration, "binding", req.BindingID,
		nil, "active", now.Add(5*time.Minute), now,
	); err != nil {
		return ContentPermit{}, err
	}

	permit := BindContentPermit(pinID)
	a.pendingPermits.Store(pinID, &liveDisplayPermit{
		key:        key,
		expected:   immutablefs.Metadata{MediaType: mime, Size: size, Width: width, Height: height, Checksum: checksum},
		mediaType:  mime,
		checksum:   checksum,
		size:       size,
		width:      width,
		height:     height,
		pinID:      pinID,
		scope:      tx.BoundAccountScope(),
		assetID:    req.AssetID,
		generation: req.ExactGeneration,
	})
	return permit, nil
}

// FinalizeShareDisplayPermit promotes a pending share permit after the share
// transaction has committed. Anonymous HTTP then opens via OpenDisplayWithPermit.
func (a *Application) FinalizeShareDisplayPermit(permit ContentPermit) error {
	if a == nil || permit.ID() == "" {
		return ErrNotFound
	}
	raw, ok := a.pendingPermits.LoadAndDelete(permit.ID())
	if !ok {
		return ErrNotFound
	}
	entry, ok := raw.(*liveDisplayPermit)
	if !ok || entry == nil {
		return ErrNotFound
	}
	a.livePermits.Store(permit.ID(), entry)
	return nil
}

// DiscardShareDisplayPermit drops a pending permit after a failed/retracted share tx.
func (a *Application) DiscardShareDisplayPermit(permit ContentPermit) {
	if a == nil || permit.ID() == "" {
		return
	}
	a.pendingPermits.Delete(permit.ID())
}

// OpenDisplayWithPermit opens a verified display stream for a finalized
// ContentPermit. Anonymous HTTP must only hold the permit.
func (a *Application) OpenDisplayWithPermit(
	ctx context.Context,
	permit ContentPermit,
) (*DisplayStream, error) {
	if a == nil || a.objects == nil || permit.ID() == "" {
		return nil, ErrNotFound
	}
	raw, ok := a.livePermits.LoadAndDelete(permit.ID())
	if !ok {
		return nil, ErrNotFound
	}
	entry, ok := raw.(*liveDisplayPermit)
	if !ok || entry == nil {
		return nil, ErrNotFound
	}

	var stream DisplayStream
	stream.MediaType, stream.Checksum, stream.Size = entry.mediaType, entry.checksum, entry.size
	stream.Width, stream.Height = entry.width, entry.height
	reader, err := openVerified(ctx, a.objects, entry.key, entry.expected)
	if err != nil {
		_ = a.releaseReadPin(ctx, entry.scope, entry.pinID)
		if errors.Is(err, immutablefs.ErrIntegrity) || errors.Is(err, immutablefs.ErrNotFound) {
			_ = a.markAssetCorrupt(ctx, entry.scope, entry.assetID, entry.generation)
			return nil, ErrAssetCorrupt
		}
		return nil, err
	}
	stream.Reader = reader
	stream.release = func() error {
		return a.releaseReadPin(context.Background(), entry.scope, entry.pinID)
	}
	return &stream, nil
}
