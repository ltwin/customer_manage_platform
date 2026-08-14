package planningmedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrHolderAuthorizationRequired = errors.New("holder_authorization_required")
	ErrPlanArchived                = errors.New("plan_archived")
	ErrPlanReopenRequired          = errors.New("plan_reopen_required")
	ErrAssetReferenceStale         = errors.New("asset_reference_stale")
	ErrAssetGCPending              = errors.New("asset_gc_in_progress")
	ErrAssetCorrupt                = errors.New("asset_corrupt")
	ErrProjectionUnavailable       = errors.New("planning_media_projection_unavailable")
	ErrValidation                  = errors.New("planning_media_validation_failed")
)

type ApplicationOption func(*Application)

func WithHolderAuthorizer(authorizer HolderAuthorizer) ApplicationOption {
	return func(a *Application) { a.authorizer = authorizer }
}
func WithClock(now func() time.Time) ApplicationOption { return func(a *Application) { a.now = now } }

type Application struct {
	repo           Repository
	executor       *idempotency.Executor
	objects        immutablefs.ObjectStore
	authorizer     HolderAuthorizer
	now            func() time.Time
	pendingPermits sync.Map // permit id → *liveDisplayPermit (pre-commit)
	livePermits    sync.Map // permit id → *liveDisplayPermit (openable)
}

func (a *Application) BatchShotAccessRefsInScope(ctx context.Context, tx store.TxAccountScope, planID string, shotIDs []string) (map[string][]AssetAccessRef, error) {
	refs := make(map[string][]AssetAccessRef, len(shotIDs))
	wanted := make(map[string]struct{}, len(shotIDs))
	for _, id := range shotIDs {
		wanted[id] = struct{}{}
		refs[id] = []AssetAccessRef{}
	}
	if len(wanted) == 0 {
		return refs, nil
	}
	if len(wanted) > 30 {
		return nil, fmt.Errorf("%w: too many shots", ErrProjectionUnavailable)
	}
	placeholders := make([]string, 0, len(wanted))
	args := make([]any, 0, len(wanted)+1)
	args = append(args, planID)
	seen := make(map[string]struct{}, len(wanted))
	for _, id := range shotIDs {
		if _, ok := wanted[id]; !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)+2))
		args = append(args, id)
	}
	rows, err := tx.QueryPage(ctx, "planning_media_shot_access_refs", "shot_id,asset_id,generation,display_checksum,display_name", "plan_id = $2 AND shot_id IN ("+strings.Join(placeholders, ",")+")", []store.OrderBy{{Column: "shot_id"}, {Column: "created_at"}, {Column: "binding_id"}}, 100, 0, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectionUnavailable, err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var shotID, assetID, checksum, name string
		var generation int
		if err := rows.Scan(&shotID, &assetID, &generation, &checksum, &name); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrProjectionUnavailable, err)
		}
		if _, ok := wanted[shotID]; !ok {
			continue
		}
		refs[shotID] = append(refs[shotID], AssetAccessRef{AssetID: assetID, Generation: generation, DisplayChecksum: checksum, DisplayName: name})
		count++
		if count >= 100 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProjectionUnavailable, err)
	}
	return refs, nil
}

func NewApplication(repo Repository, executor *idempotency.Executor, objects immutablefs.ObjectStore, options ...ApplicationOption) *Application {
	a := &Application{repo: repo, executor: executor, objects: objects, now: time.Now}
	for _, option := range options {
		if option != nil {
			option(a)
		}
	}
	return a
}

type UploadInput struct {
	PlanID               string                 `json:"plan_id"`
	ExpectedPlanRevision int64                  `json:"expected_plan_revision"`
	DisplayName          string                 `json:"display_name"`
	Content              *UploadSpool           `json:"-"`
	Rights               RightsDeclarationInput `json:"rights"`
	IntendedPurpose      Purpose                `json:"intended_purpose"`
}
type UploadResult struct {
	Asset       PlanAsset       `json:"asset"`
	Generation  AssetGeneration `json:"generation"`
	AccessRef   AssetAccessRef  `json:"access_ref"`
	StagedUntil time.Time       `json:"staged_until"`
	Original    AssetRendition  `json:"original"`
	Display     AssetRendition  `json:"display"`
}

func (a *Application) Upload(ctx context.Context, scope store.AccountScope, key string, input UploadInput) (UploadResult, error) {
	if a == nil || a.executor == nil || a.objects == nil {
		return UploadResult{}, ErrHolderAuthorizationRequired
	}
	input.PlanID = strings.TrimSpace(input.PlanID)
	if input.PlanID == "" || input.ExpectedPlanRevision < 1 || input.Content == nil || input.Content.size < 1 {
		return UploadResult{}, fmt.Errorf("upload_input_invalid")
	}
	if err := ValidatePurpose(input.Rights, input.IntendedPurpose); err != nil {
		return UploadResult{}, err
	}
	canonical := map[string]any{"plan_id": input.PlanID, "expected_plan_revision": input.ExpectedPlanRevision, "original_checksum": input.Content.checksum, "original_size": input.Content.size, "original_mime": input.Content.mediaType, "source_class": input.Rights.SourceClass, "rights_basis": input.Rights.RightsBasis, "evidence_summary": input.Rights.EvidenceSummary, "license_generation_reference_granted": input.Rights.LicenseGenerationReferenceGranted, "intended_purpose": input.IntendedPurpose, "image_pipeline_version": PipelineVersion, "matrix_version": MatrixVersion}
	body, err := json.Marshal(canonical)
	if err != nil {
		return UploadResult{}, err
	}
	published := make([]publishedObject, 0, 2)
	response, err := a.executor.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanningMediaUpload, Key: key, ResourceIdentity: idempotency.PlanningMediaUploadResource(input.PlanID), CanonicalBody: body}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		raw, err := input.Content.readAll(ctx)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		processed, err := ProcessImage(raw, input.Content.mediaType)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if a.authorizer == nil {
			return idempotency.StoredResponse{}, ErrHolderAuthorizationRequired
		}
		if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: input.PlanID, HolderID: input.PlanID, Kind: HolderPlan, ExpectedPlanRevision: input.ExpectedPlanRevision, Mutation: MutationUpload}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		now := a.now().UTC()
		assetID := deterministicUploadID(tx.AccountID(), input.PlanID, key, "asset")
		rightsID := deterministicUploadID(tx.AccountID(), input.PlanID, key, "rights")
		originalKey, err := planningObjectKey(tx.AccountID(), assetID, 1, RenditionOriginal)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		displayKey, err := planningObjectKey(tx.AccountID(), assetID, 1, RenditionDisplay)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		asset := PlanAsset{ID: assetID, UploadContextPlanID: input.PlanID, DisplayName: cleanDisplayName(input.DisplayName), State: AssetStaged, CurrentGeneration: 1, Revision: 1, GCEligibleAt: ptrTime(now.Add(48 * time.Hour)), GCRuleVersion: 1, CreatedAt: now, UpdatedAt: now}
		rights := RightsDeclaration{ID: rightsID, AssetID: assetID, Generation: 1, SourceClass: input.Rights.SourceClass, RightsBasis: input.Rights.RightsBasis, EvidenceSummary: input.Rights.EvidenceSummary, LicenseGenerationReferenceGranted: input.Rights.LicenseGenerationReferenceGranted, MatrixVersion: MatrixVersion, DeclaredAt: now}
		generation := AssetGeneration{AssetID: assetID, Generation: 1, Rights: rights, OriginalChecksum: processed.Original.Checksum, DisplayChecksum: processed.Display.Checksum, CreatedAt: now}
		original := AssetRendition{AssetID: assetID, Generation: 1, Kind: RenditionOriginal, MediaType: processed.Original.MediaType, ByteSize: processed.Original.Size, Width: processed.Original.Width, Height: processed.Original.Height, Checksum: processed.Original.Checksum, objectKey: originalKey}
		display := AssetRendition{AssetID: assetID, Generation: 1, Kind: RenditionDisplay, MediaType: processed.Display.MediaType, ByteSize: processed.Display.Size, Width: processed.Display.Width, Height: processed.Display.Height, Checksum: processed.Display.Checksum, objectKey: displayKey}
		originalMeta, created, err := a.objects.PutImmutable(ctx, originalKey, processed.Original.Bytes, immutablefs.Metadata{MediaType: original.MediaType, Size: original.ByteSize, Checksum: original.Checksum, Width: original.Width, Height: original.Height, ModifiedAt: now})
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if created {
			published = append(published, publishedObject{key: originalKey, metadata: originalMeta})
		}
		displayMeta, created, err := a.objects.PutImmutable(ctx, displayKey, processed.Display.Bytes, immutablefs.Metadata{MediaType: display.MediaType, Size: display.ByteSize, Checksum: display.Checksum, Width: display.Width, Height: display.Height, ModifiedAt: now})
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if created {
			published = append(published, publishedObject{key: displayKey, metadata: displayMeta})
		}
		if err := a.repo.InsertAsset(ctx, tx, CreateAssetRecord{Asset: asset, Rights: rights, Generation: generation, Original: original, Display: display}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		result := UploadResult{Asset: asset, Generation: generation, AccessRef: AssetAccessRef{AssetID: assetID, Generation: 1, DisplayChecksum: display.Checksum, DisplayName: asset.DisplayName}, StagedUntil: *asset.GCEligibleAt, Original: withoutKey(original), Display: withoutKey(display)}
		encoded, err := json.Marshal(result)
		return idempotency.StoredResponse{Status: 201, Body: encoded}, err
	})
	if err != nil {
		a.removePublishedBestEffort(ctx, published)
		return UploadResult{}, err
	}
	var result UploadResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return UploadResult{}, err
	}
	return result, nil
}

type publishedObject struct {
	key      string
	metadata immutablefs.Metadata
}

func deterministicUploadID(accountID, planID, key, kind string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(accountID+"\x00"+planID+"\x00"+key+"\x00"+kind)).String()
}

func (a *Application) removePublishedBestEffort(ctx context.Context, published []publishedObject) {
	for index := len(published) - 1; index >= 0; index-- {
		object := published[index]
		if exact, ok := a.objects.(interface {
			RemoveExact(context.Context, string, immutablefs.Metadata) error
		}); ok {
			_ = exact.RemoveExact(ctx, object.key, object.metadata)
			continue
		}
		meta, err := a.objects.Stat(ctx, object.key)
		if err == nil && meta.Checksum == object.metadata.Checksum && meta.Size == object.metadata.Size {
			_ = a.objects.Delete(ctx, object.key)
		}
	}
}

type PhysicalObjectFinding struct {
	InventoryFinding
	Deleted bool
}

func (a *Application) ReconcilePhysicalOrphans(ctx context.Context, scope store.AccountScope, now time.Time) ([]PhysicalObjectFinding, error) {
	if a == nil || a.objects == nil {
		return nil, ErrAssetState
	}
	expected, err := a.repo.ExpectedInventory(ctx, scope)
	if err != nil {
		return nil, err
	}
	actual, err := listAllPrefix(ctx, a.objects, "planning/"+scope.AccountID())
	if err != nil {
		return nil, err
	}
	findings := ClassifyInventory(expected, actual)
	actualByKey := make(map[string]immutablefs.Metadata, len(actual))
	for _, item := range actual {
		accountID, assetID, generation, rendition, parseErr := parsePlanningObjectKey(item.Key)
		if parseErr != nil || accountID != scope.AccountID() {
			return nil, immutablefs.ErrIntegrity
		}
		actualByKey[item.Key] = item.Meta
		entry := ManifestEntry{Key: item.Key, AssetID: assetID, Generation: generation, Rendition: rendition, Metadata: item.Meta}
		if err := a.repo.RecordObjectObservation(ctx, scope, entry, now.UTC()); err != nil {
			return nil, err
		}
	}
	results := make([]PhysicalObjectFinding, 0, len(findings))
	for _, finding := range findings {
		result := PhysicalObjectFinding{InventoryFinding: finding}
		if finding.Classification == InventoryOrphan {
			metadata := actualByKey[finding.Key]
			firstSeen, err := a.repo.ObjectObservationFirstSeen(ctx, scope, finding.Key)
			if err != nil {
				return nil, err
			}
			if !metadata.ModifiedAt.IsZero() && !metadata.ModifiedAt.After(now.Add(-48*time.Hour)) && !firstSeen.After(now.Add(-48*time.Hour)) {
				if err := removeObjectExact(ctx, a.objects, finding.Key, metadata); err != nil {
					return nil, err
				}
				if err := a.repo.DeleteObjectObservation(ctx, scope, finding.Key); err != nil {
					return nil, err
				}
				result.Deleted = true
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func listAllPrefix(ctx context.Context, objects immutablefs.ObjectStore, prefix string) ([]immutablefs.Item, error) {
	items := make([]immutablefs.Item, 0)
	cursor := ""
	for {
		page, err := objects.List(ctx, prefix, cursor, 1000)
		if err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.Done {
			return items, nil
		}
		cursor = page.NextCursor
	}
}

func removeObjectExact(ctx context.Context, objects immutablefs.ObjectStore, key string, metadata immutablefs.Metadata) error {
	if exact, ok := objects.(interface {
		RemoveExact(context.Context, string, immutablefs.Metadata) error
	}); ok {
		return exact.RemoveExact(ctx, key, metadata)
	}
	actual, err := objects.Stat(ctx, key)
	if err != nil {
		return err
	}
	if !sameInventoryMetadata(actual, metadata) {
		return immutablefs.ErrIntegrity
	}
	return objects.Delete(ctx, key)
}

type CreateBindingInput struct {
	PlanID                string     `json:"plan_id"`
	AssetID               string     `json:"asset_id"`
	Generation            int        `json:"generation"`
	HolderKind            HolderKind `json:"holder_kind"`
	HolderID              string     `json:"holder_id"`
	Purpose               Purpose    `json:"purpose"`
	ExpectedPlanRevision  int64      `json:"expected_plan_revision"`
	ExpectedAssetRevision int64      `json:"expected_asset_revision"`
}
type BindingResult struct {
	Asset   PlanAsset    `json:"asset"`
	Binding AssetBinding `json:"binding"`
}

// PreparedBindingInput is the narrow in-scope seam used by ingestion combined
// commit. It intentionally contains no client-provided rights or object key.
type PreparedBindingInput struct {
	PlanID               string
	AssetID              string
	Generation           int
	HolderKind           HolderKind
	HolderID             string
	Purpose              Purpose
	ExpectedPlanRevision int64
}

// BindPreparedAssetsInScope binds an already-uploaded generation inside the
// caller's transaction. Holder authorization is re-issued here; callers may
// not pass or reuse a media proof from another capability.
func (a *Application) BindPreparedAssetsInScope(ctx context.Context, tx store.TxAccountScope, input PreparedBindingInput) (BindingResult, error) {
	if input.PlanID == "" || input.AssetID == "" || input.Generation < 1 || input.ExpectedPlanRevision < 1 || input.HolderID == "" {
		return BindingResult{}, fmt.Errorf("%w: prepared binding input invalid", ErrValidation)
	}
	asset, err := a.repo.LockAsset(ctx, tx, input.AssetID)
	if err != nil {
		return BindingResult{}, err
	}
	if asset.UploadContextPlanID != input.PlanID {
		return BindingResult{}, ErrNotFound
	}
	if asset.State == AssetGCPending || asset.State == AssetDeleted || asset.State == AssetCorrupt {
		return BindingResult{}, ErrAssetState
	}
	if asset.CurrentGeneration != input.Generation {
		return BindingResult{}, ErrAssetReferenceStale
	}
	if a.authorizer == nil {
		return BindingResult{}, ErrHolderAuthorizationRequired
	}
	if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: input.PlanID, HolderID: input.HolderID, Kind: input.HolderKind, ExpectedPlanRevision: input.ExpectedPlanRevision, Mutation: MutationBind}); err != nil {
		return BindingResult{}, err
	}
	rights, err := a.repo.RightsForGeneration(ctx, tx, input.AssetID, input.Generation)
	if err != nil {
		return BindingResult{}, err
	}
	if err := ValidatePurpose(rights, input.Purpose); err != nil {
		return BindingResult{}, err
	}
	if input.HolderKind == HolderPlan && input.Purpose != PurposeMoodboardDisplay && input.Purpose != PurposeGenerationReference {
		return BindingResult{}, ErrPurposeNotPermitted
	}
	if input.HolderKind == HolderShot && input.Purpose != PurposeShotReferenceDisplay && input.Purpose != PurposeGenerationReference {
		return BindingResult{}, ErrPurposeNotPermitted
	}
	if existing, err := a.repo.FindActiveBinding(ctx, tx, input.AssetID, input.Generation, input.HolderKind, input.HolderID, input.Purpose); err == nil {
		return BindingResult{Asset: asset, Binding: existing}, nil
	} else if !errors.Is(err, ErrNotFound) {
		return BindingResult{}, err
	}
	now := a.now().UTC()
	binding := AssetBinding{ID: uuid.NewString(), AssetID: asset.ID, Generation: input.Generation, HolderKind: input.HolderKind, HolderID: input.HolderID, PlanID: input.PlanID, Purpose: input.Purpose, State: BindingActive, Revision: 1, CreatedAt: now}
	if err := a.repo.InsertBinding(ctx, tx, binding); err != nil {
		return BindingResult{}, err
	}
	if _, err := tx.Update(ctx, "planning_media_assets", "state = $2, revision = revision + 1, updated_at = $3", "id = $4", AssetActive, now, asset.ID); err != nil {
		return BindingResult{}, err
	}
	asset.State, asset.Revision = AssetActive, asset.Revision+1
	return BindingResult{Asset: asset, Binding: binding}, nil
}

type LeaseInput struct {
	AssetID               string
	Generation            int
	BindingID             string
	OwnerKind             string
	OwnerID               string
	Purpose               Purpose
	ExpiresAt             time.Time
	ExpectedAssetRevision int64
}
type LeaseResult struct {
	Lease AssetLease `json:"lease"`
	Asset PlanAsset  `json:"asset"`
}

func (a *Application) ReserveLease(ctx context.Context, scope store.AccountScope, key string, input LeaseInput) (LeaseResult, error) {
	if input.AssetID == "" || input.Generation < 1 || input.BindingID == "" || !validLeaseOwner(input.OwnerKind, input.OwnerID) || input.Purpose == "" {
		return LeaseResult{}, fmt.Errorf("lease_input_invalid")
	}
	canonical, _ := json.Marshal(input)
	response, err := a.executor.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanningMediaLeaseReserve, Key: key, ResourceIdentity: idempotency.PlanningMediaBindingResource("lease", input.AssetID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		asset, err := a.repo.LockAsset(ctx, tx, input.AssetID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if asset.Revision != input.ExpectedAssetRevision {
			return idempotency.StoredResponse{}, ErrAssetRevisionConflict
		}
		if asset.State == AssetGCPending {
			return idempotency.StoredResponse{}, ErrAssetGCPending
		}
		if asset.State == AssetDeleted || asset.State == AssetCorrupt {
			return idempotency.StoredResponse{}, ErrAssetState
		}
		binding, err := a.repo.LockBinding(ctx, tx, input.BindingID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if binding.AssetID != input.AssetID || binding.Generation != input.Generation || binding.State != BindingActive {
			return idempotency.StoredResponse{}, ErrPurposeNotPermitted
		}
		if binding.Purpose != input.Purpose {
			return idempotency.StoredResponse{}, ErrPurposeNotPermitted
		}
		rights, err := a.repo.RightsForGeneration(ctx, tx, input.AssetID, input.Generation)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if err := ValidatePurpose(rights, input.Purpose); err != nil {
			return idempotency.StoredResponse{}, err
		}
		if a.authorizer == nil {
			return idempotency.StoredResponse{}, ErrHolderAuthorizationRequired
		}
		if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: binding.PlanID, HolderID: binding.HolderID, Kind: binding.HolderKind, ExpectedPlanRevision: 0, Mutation: MutationRead}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		if input.ExpiresAt.Before(a.now().UTC()) || input.ExpiresAt.After(a.now().UTC().Add(24*time.Hour)) {
			return idempotency.StoredResponse{}, fmt.Errorf("lease_expiry_invalid")
		}
		lease := AssetLease{ID: uuid.NewString(), AssetID: input.AssetID, Generation: input.Generation, BindingID: input.BindingID, OwnerKind: input.OwnerKind, OwnerID: input.OwnerID, Purpose: input.Purpose, State: LeaseActive, ExpiresAt: input.ExpiresAt, Revision: 1}
		if err := tx.Insert(ctx, "planning_media_leases", []string{"id", "asset_id", "generation", "binding_id", "owner_kind", "owner_id", "purpose", "state", "expires_at", "revision", "created_at"}, lease.ID, lease.AssetID, lease.Generation, lease.BindingID, lease.OwnerKind, lease.OwnerID, lease.Purpose, lease.State, lease.ExpiresAt, lease.Revision, a.now().UTC()); err != nil {
			return idempotency.StoredResponse{}, err
		}
		encoded, err := json.Marshal(LeaseResult{Lease: lease, Asset: asset})
		return idempotency.StoredResponse{Status: 200, Body: encoded}, err
	})
	if err != nil {
		return LeaseResult{}, err
	}
	var result LeaseResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return LeaseResult{}, err
	}
	return result, nil
}

func validLeaseOwner(kind, id string) bool {
	if kind == "" || len(kind) > 64 || id == "" || len(id) > 128 {
		return false
	}
	for _, value := range []string{kind, id} {
		for _, r := range value {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' && r != '_' {
				return false
			}
		}
	}
	return true
}

func (a *Application) ReleaseLease(ctx context.Context, scope store.AccountScope, key, leaseID string, expectedRevision int64) (LeaseResult, error) {
	canonical, _ := json.Marshal(map[string]any{"lease_id": leaseID, "expected_revision": expectedRevision})
	response, err := a.executor.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanningMediaLeaseRelease, Key: key, ResourceIdentity: idempotency.PlanningMediaBindingResource("lease", leaseID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		if leaseID == "" || expectedRevision < 1 {
			return idempotency.StoredResponse{}, fmt.Errorf("lease_input_invalid")
		}
		// All lease/binding/GC paths lock the asset before child rows.
		lease, err := a.repo.FindLease(ctx, tx, leaseID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		asset, err := a.repo.LockAsset(ctx, tx, lease.AssetID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		lease, err = a.repo.LockLease(ctx, tx, leaseID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if lease.Revision != expectedRevision {
			return idempotency.StoredResponse{}, ErrAssetRevisionConflict
		}
		if lease.State != LeaseActive || !lease.ExpiresAt.After(a.now().UTC()) {
			return idempotency.StoredResponse{}, ErrBindingAlreadyReleased
		}
		now := a.now().UTC()
		if _, err := tx.Update(ctx, "planning_media_leases", "state = $2, revision = revision + 1, released_at = $3", "id = $4 AND revision = $5", LeaseReleased, now, lease.ID, lease.Revision); err != nil {
			return idempotency.StoredResponse{}, err
		}
		lease.State = LeaseReleased
		lease.Revision++
		encoded, err := json.Marshal(LeaseResult{Lease: lease, Asset: asset})
		return idempotency.StoredResponse{Status: 200, Body: encoded}, err
	})
	if err != nil {
		return LeaseResult{}, err
	}
	var result LeaseResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return LeaseResult{}, err
	}
	return result, nil
}

type GCResult struct {
	AssetID string
	Deleted bool
	Error   string
}

type gcClaim struct {
	assetID    string
	generation int
	revision   int64
	objects    []publishedObject
}

func (a *Application) ReconcileGC(ctx context.Context, scope store.AccountScope, now time.Time, limit int) ([]GCResult, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid gc limit")
	}
	rows, err := scope.QueryPage(ctx, "planning_media_assets", "id", "state IN ('staged','gc_pending') AND gc_eligible_at IS NOT NULL AND gc_eligible_at <= $2", []store.OrderBy{{Column: "id"}}, limit, 0, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	results := make([]GCResult, 0, len(ids))
	for _, id := range ids {
		var result GCResult
		result.AssetID = id
		var claim gcClaim
		err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			asset, err := a.repo.LockAsset(ctx, tx, id)
			if err != nil {
				return err
			}
			bindings, err := a.repo.ActiveBindingsForAsset(ctx, tx, id)
			if err != nil {
				return err
			}
			leases, err := a.repo.ActiveLeaseCount(ctx, tx, id, now)
			if err != nil {
				return err
			}
			pins, err := a.repo.ActivePinCount(ctx, tx, id, now)
			if err != nil {
				return err
			}
			if bindings > 0 || leases > 0 || pins > 0 || asset.State == AssetDeleted || asset.State == AssetCorrupt {
				return nil
			}
			if asset.GCEligibleAt == nil || asset.GCEligibleAt.After(now) || (asset.State != AssetStaged && asset.State != AssetGCPending) {
				return nil
			}
			newRevision := asset.Revision + 1
			n, err := tx.Update(ctx, "planning_media_assets", "state = $2, revision = $3, updated_at = $4", "id = $5 AND revision = $6 AND state IN ('staged','gc_pending')", AssetGCPending, newRevision, now, asset.ID, asset.Revision)
			if err != nil {
				return err
			}
			if n != 1 {
				return nil
			}
			claim = gcClaim{assetID: asset.ID, generation: asset.CurrentGeneration, revision: newRevision, objects: make([]publishedObject, 0, 2)}
			rows, err := tx.Query(ctx, "planning_media_renditions", "internal_object_key,mime_type,byte_size,width,height,checksum", "asset_id = $2 AND generation = $3", id, asset.CurrentGeneration)
			if err != nil {
				return err
			}
			for rows.Next() {
				var key, mime, checksum string
				var size int64
				var width, height int
				if err := rows.Scan(&key, &mime, &size, &width, &height, &checksum); err != nil {
					return err
				}
				claim.objects = append(claim.objects, publishedObject{key: key, metadata: immutablefs.Metadata{MediaType: mime, Size: size, Width: width, Height: height, Checksum: checksum}})
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				return err
			}
			rows.Close()
			return nil
		})
		if err == nil && claim.assetID != "" {
			for _, object := range claim.objects {
				if deleteErr := removeObjectExact(ctx, a.objects, object.key, object.metadata); deleteErr != nil {
					// A prior attempt may have deleted an earlier rendition before
					// failing. Missing already-deleted objects are safe to treat as
					// success; the asset remains gc_pending until every rendition is
					// accounted for and the metadata state is finalized below.
					if errors.Is(deleteErr, immutablefs.ErrNotFound) {
						continue
					}
					result.Error = deleteErr.Error()
					break
				}
			}
			if result.Error == "" {
				err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
					asset, lockErr := a.repo.LockAsset(ctx, tx, claim.assetID)
					if lockErr != nil {
						return lockErr
					}
					if asset.State != AssetGCPending || asset.Revision != claim.revision || asset.CurrentGeneration != claim.generation {
						return ErrAssetRevisionConflict
					}
					if _, lockErr := tx.Update(ctx, "planning_media_assets", "state = $2, deleted_at = $3, revision = revision + 1, updated_at = $4", "id = $5 AND revision = $6 AND state = 'gc_pending'", AssetDeleted, now, now, asset.ID, claim.revision); lockErr != nil {
						return lockErr
					}
					if _, lockErr := tx.Delete(ctx, "planning_media_object_inventory", "asset_id = $2 AND generation = $3", claim.assetID, claim.generation); lockErr != nil {
						return lockErr
					}
					return nil
				})
				if err == nil {
					result.Deleted = true
				} else {
					result.Error = err.Error()
				}
			}
		}
		if err != nil && result.Error == "" {
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results, nil
}

func (a *Application) CreateBinding(ctx context.Context, scope store.AccountScope, key string, input CreateBindingInput) (BindingResult, error) {
	if strings.TrimSpace(input.PlanID) == "" || strings.TrimSpace(input.AssetID) == "" || input.Generation < 1 || input.ExpectedPlanRevision < 1 || input.ExpectedAssetRevision < 1 || strings.TrimSpace(input.HolderID) == "" || (input.HolderKind != HolderPlan && input.HolderKind != HolderShot) {
		return BindingResult{}, fmt.Errorf("%w: binding input invalid", ErrValidation)
	}
	canonical, _ := json.Marshal(input)
	response, err := a.executor.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanningMediaBindingCreate, Key: key, ResourceIdentity: idempotency.PlanningMediaBindingResource(input.PlanID, input.AssetID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		asset, err := a.repo.LockAsset(ctx, tx, input.AssetID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if asset.UploadContextPlanID != input.PlanID {
			return idempotency.StoredResponse{}, ErrNotFound
		}
		if asset.Revision != input.ExpectedAssetRevision {
			return idempotency.StoredResponse{}, ErrAssetRevisionConflict
		}
		if asset.State == AssetGCPending {
			return idempotency.StoredResponse{}, ErrAssetGCPending
		}
		if asset.State == AssetDeleted || asset.State == AssetCorrupt {
			return idempotency.StoredResponse{}, ErrAssetState
		}
		if input.Generation != asset.CurrentGeneration {
			return idempotency.StoredResponse{}, ErrAssetReferenceStale
		}
		if a.authorizer == nil {
			return idempotency.StoredResponse{}, ErrHolderAuthorizationRequired
		}
		if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: input.PlanID, HolderID: input.HolderID, Kind: input.HolderKind, ExpectedPlanRevision: input.ExpectedPlanRevision, Mutation: MutationBind}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		rights, err := a.repo.RightsForGeneration(ctx, tx, input.AssetID, input.Generation)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if err := ValidatePurpose(rights, input.Purpose); err != nil {
			return idempotency.StoredResponse{}, err
		}
		if input.HolderKind == HolderPlan && input.Purpose != PurposeMoodboardDisplay && input.Purpose != PurposeGenerationReference {
			return idempotency.StoredResponse{}, ErrPurposeNotPermitted
		}
		if input.HolderKind == HolderShot && input.Purpose != PurposeShotReferenceDisplay && input.Purpose != PurposeGenerationReference {
			return idempotency.StoredResponse{}, ErrPurposeNotPermitted
		}
		if existing, err := a.repo.FindActiveBinding(ctx, tx, input.AssetID, input.Generation, input.HolderKind, input.HolderID, input.Purpose); err == nil {
			encoded, marshalErr := json.Marshal(BindingResult{Asset: asset, Binding: existing})
			return idempotency.StoredResponse{Status: 200, Body: encoded}, marshalErr
		} else if !errors.Is(err, ErrNotFound) {
			return idempotency.StoredResponse{}, err
		}
		now := a.now().UTC()
		binding := AssetBinding{ID: uuid.NewString(), AssetID: asset.ID, Generation: input.Generation, HolderKind: input.HolderKind, HolderID: input.HolderID, PlanID: input.PlanID, Purpose: input.Purpose, State: BindingActive, Revision: 1, CreatedAt: now}
		if err := a.repo.InsertBinding(ctx, tx, binding); err != nil {
			return idempotency.StoredResponse{}, err
		}
		if _, err := tx.Update(ctx, "planning_media_assets", "state = $2, revision = revision + 1, updated_at = $3", "id = $4", AssetActive, now, asset.ID); err != nil {
			return idempotency.StoredResponse{}, err
		}
		asset.State = AssetActive
		asset.Revision++
		encoded, err := json.Marshal(BindingResult{Asset: asset, Binding: binding})
		return idempotency.StoredResponse{Status: 200, Body: encoded}, err
	})
	if err != nil {
		return BindingResult{}, err
	}
	var result BindingResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return BindingResult{}, err
	}
	return result, nil
}

func (a *Application) ReleaseBinding(ctx context.Context, scope store.AccountScope, key, planID, assetID, bindingID string, expectedPlanRevision, expectedAssetRevision, expectedBindingRevision int64) (BindingResult, error) {
	if strings.TrimSpace(planID) == "" || strings.TrimSpace(assetID) == "" || strings.TrimSpace(bindingID) == "" || expectedPlanRevision < 1 || expectedAssetRevision < 1 || expectedBindingRevision < 1 {
		return BindingResult{}, fmt.Errorf("%w: binding input invalid", ErrValidation)
	}
	input := map[string]any{"plan_id": planID, "asset_id": assetID, "binding_id": bindingID, "expected_plan_revision": expectedPlanRevision, "expected_asset_revision": expectedAssetRevision, "expected_binding_revision": expectedBindingRevision}
	canonical, _ := json.Marshal(input)
	response, err := a.executor.Execute(ctx, scope, idempotency.Request{Operation: idempotency.OperationPlanningMediaBindingRelease, Key: key, ResourceIdentity: idempotency.PlanningMediaBindingResource(planID, assetID), CanonicalBody: canonical}, func(tx store.TxAccountScope) (idempotency.StoredResponse, error) {
		if a.authorizer == nil {
			return idempotency.StoredResponse{}, ErrHolderAuthorizationRequired
		}
		asset, err := a.repo.LockAsset(ctx, tx, assetID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if asset.UploadContextPlanID != planID || asset.Revision != expectedAssetRevision {
			return idempotency.StoredResponse{}, ErrAssetRevisionConflict
		}
		b, err := a.repo.LockBinding(ctx, tx, bindingID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		if b.AssetID != assetID || b.PlanID != planID {
			return idempotency.StoredResponse{}, ErrNotFound
		}
		if b.Revision != expectedBindingRevision {
			return idempotency.StoredResponse{}, ErrAssetRevisionConflict
		}
		if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: planID, HolderID: b.HolderID, Kind: b.HolderKind, ExpectedPlanRevision: expectedPlanRevision, Mutation: MutationRelease}); err != nil {
			return idempotency.StoredResponse{}, err
		}
		now := a.now().UTC()
		if err := a.repo.ReleaseBinding(ctx, tx, b, now); err != nil {
			return idempotency.StoredResponse{}, err
		}
		remaining, err := a.repo.ActiveBindingsForAsset(ctx, tx, assetID)
		if err != nil {
			return idempotency.StoredResponse{}, err
		}
		asset.Revision++
		asset.UpdatedAt = now
		if remaining == 0 {
			asset.State = AssetStaged
			eligibleAt := asset.CreatedAt.Add(48 * time.Hour)
			if now.After(eligibleAt) {
				eligibleAt = now
			}
			if asset.GCEligibleAt == nil || asset.GCEligibleAt.Before(eligibleAt) {
				asset.GCEligibleAt = &eligibleAt
			}
		}
		if _, err := tx.Update(ctx, "planning_media_assets", "state = $2, revision = $3, gc_eligible_at = $4, updated_at = $5", "id = $6", asset.State, asset.Revision, asset.GCEligibleAt, asset.UpdatedAt, asset.ID); err != nil {
			return idempotency.StoredResponse{}, err
		}
		b.State = BindingReleased
		b.Revision++
		b.ReleasedAt = &now
		encoded, err := json.Marshal(BindingResult{Asset: asset, Binding: b})
		return idempotency.StoredResponse{Status: 200, Body: encoded}, err
	})
	if err != nil {
		return BindingResult{}, err
	}
	var result BindingResult
	if err := json.Unmarshal(response.Body, &result); err != nil {
		return BindingResult{}, err
	}
	return result, nil
}

func (a *Application) ListForPlan(ctx context.Context, scope store.AccountScope, planID, cursor string, limit int) (PlanAssetPage, error) {
	if a.authorizer == nil {
		return PlanAssetPage{}, ErrHolderAuthorizationRequired
	}
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{
			PlanID: planID, HolderID: planID, Kind: HolderPlan, Mutation: MutationRead,
		})
		return err
	}); err != nil {
		return PlanAssetPage{}, err
	}
	return a.repo.ListForPlan(ctx, scope, planID, cursor, limit)
}

type DisplayContent struct {
	Bytes     []byte
	MediaType string
	Checksum  string
	Size      int64
	Width     int
	Height    int
}
type DisplayStream struct {
	Reader    io.ReadCloser
	MediaType string
	Checksum  string
	Size      int64
	Width     int
	Height    int
	release   func() error
	once      sync.Once
	err       error
}

func (s *DisplayStream) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.Reader != nil {
			s.err = s.Reader.Close()
		}
		if s.release != nil {
			if err := s.release(); s.err == nil {
				s.err = err
			}
		}
	})
	return s.err
}

// OpenDisplay establishes a response-lifetime read pin before opening the
// verified display object. The caller must Close the returned stream; Close
// is idempotent and releases the pin after the response body is consumed.
func (a *Application) OpenDisplay(ctx context.Context, scope store.AccountScope, planID, assetID, checksumValue string) (*DisplayStream, error) {
	if checksumValue == "" {
		return nil, ErrAssetReferenceStale
	}
	var stream DisplayStream
	var pinID, key string
	var generation int
	var expected immutablefs.Metadata
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		asset, err := a.repo.LockAsset(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if asset.UploadContextPlanID != planID {
			return ErrNotFound
		}
		if asset.State == AssetCorrupt {
			return ErrAssetCorrupt
		}
		if asset.State == AssetGCPending {
			return ErrAssetGCPending
		}
		if asset.State == AssetDeleted {
			return ErrAssetState
		}
		var mime, checksum string
		var size int64
		var width, height int
		if err := tx.QueryRow(ctx, "planning_media_renditions", "internal_object_key,mime_type,byte_size,width,height,checksum", "asset_id = $2 AND generation = $3 AND kind = 'display'", assetID, asset.CurrentGeneration).Scan(&key, &mime, &size, &width, &height, &checksum); err != nil {
			return err
		}
		if checksum != checksumValue {
			return ErrAssetReferenceStale
		}
		rights, err := a.repo.RightsForGeneration(ctx, tx, assetID, asset.CurrentGeneration)
		if err != nil {
			return err
		}
		if a.authorizer == nil {
			return ErrHolderAuthorizationRequired
		}
		anchor := "upload_context"
		var bindingID any
		var uploadContextPlanID any = planID
		binding, bindingErr := a.repo.FindActiveBindingForPlan(ctx, tx, assetID, asset.CurrentGeneration, planID)
		if bindingErr == nil {
			if binding.PlanID != planID {
				return ErrNotFound
			}
			if err := ValidatePurpose(rights, binding.Purpose); err != nil {
				return err
			}
			if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: binding.PlanID, HolderID: binding.HolderID, Kind: binding.HolderKind, Mutation: MutationRead}); err != nil {
				return err
			}
			anchor = "binding"
			bindingID = binding.ID
			uploadContextPlanID = nil
		} else if !errors.Is(bindingErr, ErrNotFound) {
			return bindingErr
		} else {
			if asset.State == AssetActive {
				return ErrAssetState
			}
			if err := ValidatePurpose(rights, PurposeMoodboardDisplay); err != nil {
				return err
			}
			if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: planID, HolderID: planID, Kind: HolderPlan, Mutation: MutationRead}); err != nil {
				return err
			}
		}
		pinID = uuid.NewString()
		generation = asset.CurrentGeneration
		expected = immutablefs.Metadata{MediaType: mime, Size: size, Width: width, Height: height, Checksum: checksum}
		now := a.now().UTC()
		if err := tx.Insert(ctx, "planning_media_read_pins", []string{"id", "asset_id", "generation", "authorization_anchor", "binding_id", "upload_context_plan_id", "state", "expires_at", "created_at"}, pinID, assetID, generation, anchor, bindingID, uploadContextPlanID, "active", now.Add(5*time.Minute), now); err != nil {
			return err
		}
		stream.MediaType, stream.Checksum, stream.Size, stream.Width, stream.Height = mime, checksum, size, width, height
		return nil
	})
	if err != nil {
		return nil, err
	}
	reader, err := openVerified(ctx, a.objects, key, expected)
	if err != nil {
		_ = a.releaseReadPin(ctx, scope, pinID)
		if errors.Is(err, immutablefs.ErrIntegrity) || errors.Is(err, immutablefs.ErrNotFound) {
			_ = a.markAssetCorrupt(ctx, scope, assetID, generation)
			return nil, ErrAssetCorrupt
		}
		return nil, err
	}
	stream.Reader = reader
	stream.release = func() error { return a.releaseReadPin(context.Background(), scope, pinID) }
	return &stream, nil
}

func (a *Application) releaseReadPin(ctx context.Context, scope store.AccountScope, pinID string) error {
	if pinID == "" {
		return nil
	}
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		_, err := tx.Update(ctx, "planning_media_read_pins", "state = $2, released_at = $3", "id = $4 AND state = 'active'", LeaseReleased, a.now().UTC(), pinID)
		return err
	})
}

func (a *Application) ReadDisplay(ctx context.Context, scope store.AccountScope, planID, assetID, checksumValue string) (DisplayContent, error) {
	if checksumValue == "" {
		return DisplayContent{}, ErrAssetReferenceStale
	}
	var result DisplayContent
	var corruptGeneration int
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		asset, err := a.repo.LockAsset(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if asset.UploadContextPlanID != planID {
			return ErrNotFound
		}
		if asset.State == AssetCorrupt {
			return ErrAssetCorrupt
		}
		if asset.State == AssetGCPending {
			return ErrAssetGCPending
		}
		if asset.State == AssetDeleted {
			return ErrAssetState
		}
		var key, mime, storedChecksum string
		var size int64
		var width, height int
		err = tx.QueryRow(ctx, "planning_media_renditions", "internal_object_key,mime_type,byte_size,width,height,checksum", "asset_id = $2 AND generation = $3 AND kind = 'display'", assetID, asset.CurrentGeneration).Scan(&key, &mime, &size, &width, &height, &storedChecksum)
		if err != nil {
			return err
		}
		if storedChecksum != checksumValue {
			return ErrAssetReferenceStale
		}
		rights, err := a.repo.RightsForGeneration(ctx, tx, assetID, asset.CurrentGeneration)
		if err != nil {
			return err
		}
		if a.authorizer == nil {
			return ErrHolderAuthorizationRequired
		}
		anchor := "upload_context"
		var bindingID any
		var uploadContextPlanID any = planID
		binding, bindingErr := a.repo.FindActiveBindingForPlan(ctx, tx, assetID, asset.CurrentGeneration, planID)
		if bindingErr == nil {
			if err := ValidatePurpose(rights, binding.Purpose); err != nil {
				return err
			}
			if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: binding.PlanID, HolderID: binding.HolderID, Kind: binding.HolderKind, ExpectedPlanRevision: 0, Mutation: MutationRead}); err != nil {
				return err
			}
			anchor = "binding"
			bindingID = binding.ID
			uploadContextPlanID = nil
		} else if !errors.Is(bindingErr, ErrNotFound) {
			return bindingErr
		} else {
			if asset.State == AssetActive {
				return ErrAssetState
			}
			if err := ValidatePurpose(rights, PurposeMoodboardDisplay); err != nil {
				return err
			}
			if _, err := a.authorizer.AuthorizeMediaHolderInScope(ctx, tx, HolderRequest{PlanID: planID, HolderID: planID, Kind: HolderPlan, ExpectedPlanRevision: 0, Mutation: MutationRead}); err != nil {
				return err
			}
		}
		pinID := uuid.NewString()
		now := a.now().UTC()
		if err := tx.Insert(ctx, "planning_media_read_pins", []string{"id", "asset_id", "generation", "authorization_anchor", "binding_id", "upload_context_plan_id", "state", "expires_at", "created_at"}, pinID, assetID, asset.CurrentGeneration, anchor, bindingID, uploadContextPlanID, "active", now.Add(5*time.Minute), now); err != nil {
			return err
		}
		corruptGeneration = asset.CurrentGeneration
		expected := immutablefs.Metadata{MediaType: mime, Size: size, Width: width, Height: height, Checksum: storedChecksum}
		reader, err := openVerified(ctx, a.objects, key, expected)
		if err != nil {
			return err
		}
		defer func() { _ = reader.Close() }()
		bytes, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if int64(len(bytes)) != size || digest(bytes) != storedChecksum {
			return immutablefs.ErrIntegrity
		}
		if _, err := tx.Update(ctx, "planning_media_read_pins", "state = $2, released_at = $3", "id = $4", LeaseReleased, a.now().UTC(), pinID); err != nil {
			return err
		}
		result = DisplayContent{Bytes: bytes, MediaType: mime, Checksum: storedChecksum, Size: size, Width: width, Height: height}
		return nil
	})
	if (errors.Is(err, immutablefs.ErrIntegrity) || errors.Is(err, immutablefs.ErrNotFound)) && corruptGeneration > 0 {
		_ = a.markAssetCorrupt(ctx, scope, assetID, corruptGeneration)
		return DisplayContent{}, ErrAssetCorrupt
	}
	return result, err
}

func (a *Application) markAssetCorrupt(ctx context.Context, scope store.AccountScope, assetID string, generation int) error {
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		asset, err := a.repo.LockAsset(ctx, tx, assetID)
		if err != nil {
			return err
		}
		if asset.CurrentGeneration != generation || asset.State == AssetDeleted || asset.State == AssetGCPending || asset.State == AssetCorrupt {
			return nil
		}
		_, err = tx.Update(ctx, "planning_media_assets", "state = $2, revision = revision + 1, updated_at = $3", "id = $4", AssetCorrupt, a.now().UTC(), assetID)
		return err
	})
}

func cleanDisplayName(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	r := []rune(name)
	if len(r) > 160 {
		r = r[:160]
	}
	return string(r)
}
func ptrTime(t time.Time) *time.Time             { return &t }
func withoutKey(r AssetRendition) AssetRendition { r.objectKey = ""; return r }
