package planningmedia

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var (
	ErrNotFound               = errors.New("planning media not found")
	ErrAssetRevisionConflict  = errors.New("asset_revision_conflict")
	ErrPlanRevisionConflict   = errors.New("plan_revision_conflict")
	ErrBindingAlreadyReleased = errors.New("binding_already_released")
	ErrBindingAlreadyActive   = errors.New("binding_already_active")
	ErrAssetState             = errors.New("asset_state_invalid")
)

type Repository struct{}

type CreateAssetRecord struct {
	Asset      PlanAsset
	Rights     RightsDeclaration
	Generation AssetGeneration
	Original   AssetRendition
	Display    AssetRendition
}

func (Repository) InsertAsset(ctx context.Context, tx store.TxAccountScope, rec CreateAssetRecord) error {
	if err := tx.Insert(ctx, "planning_media_assets", []string{"id", "upload_context_plan_id", "display_name", "state", "current_generation", "revision", "gc_eligible_at", "gc_rule_version", "created_at", "updated_at"}, rec.Asset.ID, rec.Asset.UploadContextPlanID, rec.Asset.DisplayName, rec.Asset.State, rec.Asset.CurrentGeneration, rec.Asset.Revision, rec.Asset.GCEligibleAt, rec.Asset.GCRuleVersion, rec.Asset.CreatedAt, rec.Asset.UpdatedAt); err != nil {
		return err
	}
	if err := tx.Insert(ctx, "planning_media_rights_declarations", []string{"id", "asset_id", "generation", "source_class", "rights_basis", "evidence_summary", "license_generation_reference_granted", "matrix_version", "declared_at"}, rec.Rights.ID, rec.Rights.AssetID, rec.Rights.Generation, rec.Rights.SourceClass, rec.Rights.RightsBasis, rec.Rights.EvidenceSummary, rec.Rights.LicenseGenerationReferenceGranted, rec.Rights.MatrixVersion, rec.Rights.DeclaredAt); err != nil {
		return err
	}
	if err := tx.Insert(ctx, "planning_media_generations", []string{"asset_id", "generation", "rights_declaration_id", "original_checksum", "display_checksum", "created_at"}, rec.Generation.AssetID, rec.Generation.Generation, rec.Rights.ID, rec.Generation.OriginalChecksum, rec.Generation.DisplayChecksum, rec.Generation.CreatedAt); err != nil {
		return err
	}
	for _, rendition := range []AssetRendition{rec.Original, rec.Display} {
		if err := tx.Insert(ctx, "planning_media_renditions", []string{"asset_id", "generation", "kind", "mime_type", "byte_size", "width", "height", "checksum", "internal_object_key"}, rendition.AssetID, rendition.Generation, rendition.Kind, rendition.MediaType, rendition.ByteSize, rendition.Width, rendition.Height, rendition.Checksum, renditionInternalKey(rendition)); err != nil {
			return err
		}
		if err := tx.Insert(ctx, "planning_media_object_inventory", []string{"internal_object_key", "asset_id", "generation", "rendition", "checksum", "byte_size", "inventory_version", "first_seen_at"}, renditionInternalKey(rendition), rendition.AssetID, rendition.Generation, rendition.Kind, rendition.Checksum, rendition.ByteSize, 1, rec.Generation.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

// PutRenditionKey stores internal object identity separately from public DTO.
// The repository keeps it private by never copying it into response structs.
func renditionInternalKey(r AssetRendition) string { return r.objectKey }

func (Repository) ListForPlan(ctx context.Context, scope store.AccountScope, planID, cursor string, limit int) (PlanAssetPage, error) {
	if limit < 1 || limit > 100 {
		return PlanAssetPage{}, fmt.Errorf("invalid page size")
	}
	rows, err := scope.QueryPage(ctx, "planning_media_asset_gallery", "id,upload_context_plan_id,display_name,state,current_generation,revision,gc_eligible_at,gc_rule_version,created_at,updated_at,deleted_at,display_checksum,rights_generation,rights_source_class,rights_basis,rights_generation_granted,rights_declared_at,active_bindings", "upload_context_plan_id = $2 AND id > $3", []store.OrderBy{{Column: "id"}}, limit, 0, planID, cursor)
	if err != nil {
		return PlanAssetPage{}, err
	}
	defer rows.Close()
	result := PlanAssetPage{Items: make([]PlanAsset, 0, limit)}
	for rows.Next() {
		var a PlanAsset
		var (
			rightsGeneration  sql.NullInt64
			rightsSourceClass sql.NullString
			rightsBasis       sql.NullString
			rightsGranted     sql.NullBool
			rightsDeclaredAt  sql.NullTime
			activeBindings    []byte
		)
		if err := rows.Scan(&a.ID, &a.UploadContextPlanID, &a.DisplayName, &a.State, &a.CurrentGeneration, &a.Revision, &a.GCEligibleAt, &a.GCRuleVersion, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt, &a.DisplayChecksum, &rightsGeneration, &rightsSourceClass, &rightsBasis, &rightsGranted, &rightsDeclaredAt, &activeBindings); err != nil {
			return PlanAssetPage{}, err
		}
		if rightsGeneration.Valid && rightsSourceClass.Valid && rightsBasis.Valid && rightsDeclaredAt.Valid {
			a.Rights = &PlanAssetRights{
				Generation:                        int(rightsGeneration.Int64),
				SourceClass:                       SourceClass(rightsSourceClass.String),
				RightsBasis:                       RightsBasis(rightsBasis.String),
				LicenseGenerationReferenceGranted: rightsGranted.Bool,
				DeclaredAt:                        rightsDeclaredAt.Time,
			}
		}
		a.ActiveBindings = make([]AssetBinding, 0)
		if len(activeBindings) > 0 {
			if err := json.Unmarshal(activeBindings, &a.ActiveBindings); err != nil {
				return PlanAssetPage{}, fmt.Errorf("decode active bindings: %w", err)
			}
		}
		result.Items = append(result.Items, a)
	}
	if err := rows.Err(); err != nil {
		return PlanAssetPage{}, err
	}
	if len(result.Items) == 0 {
		result.Done = true
	} else {
		result.NextCursor = result.Items[len(result.Items)-1].ID
		result.Done = len(result.Items) < limit
	}
	return result, nil
}

func (Repository) ExpectedInventory(ctx context.Context, scope store.AccountScope) ([]ManifestEntry, error) {
	entries := make([]ManifestEntry, 0)
	cursor := ""
	for {
		rows, err := scope.QueryPage(ctx, "planning_media_renditions",
			"asset_id,generation,kind,mime_type,byte_size,width,height,checksum,internal_object_key",
			"internal_object_key > $2 AND asset_id IN (SELECT id FROM planning_media_assets WHERE account_id = $1 AND state <> 'deleted')", []store.OrderBy{{Column: "internal_object_key"}}, 1000, 0, cursor)
		if err != nil {
			return nil, err
		}
		count := 0
		for rows.Next() {
			var entry ManifestEntry
			if err := rows.Scan(&entry.AssetID, &entry.Generation, &entry.Rendition, &entry.Metadata.MediaType,
				&entry.Metadata.Size, &entry.Metadata.Width, &entry.Metadata.Height, &entry.Metadata.Checksum, &entry.Key); err != nil {
				rows.Close()
				return nil, err
			}
			accountID, assetID, generation, rendition, err := parsePlanningObjectKey(entry.Key)
			if err != nil || accountID != scope.AccountID() || assetID != entry.AssetID || generation != entry.Generation || rendition != entry.Rendition {
				rows.Close()
				return nil, ErrAssetState
			}
			entries = append(entries, entry)
			cursor = entry.Key
			count++
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		if count < 1000 {
			return entries, nil
		}
	}
}

func (Repository) RecordObjectObservation(ctx context.Context, scope store.AccountScope, entry ManifestEntry, firstSeenAt time.Time) error {
	return scope.Upsert(ctx, "planning_media_object_inventory",
		[]string{"internal_object_key", "asset_id", "generation", "rendition", "checksum", "byte_size", "inventory_version", "first_seen_at"},
		[]string{"account_id", "internal_object_key"},
		[]string{"asset_id", "generation", "rendition", "checksum", "byte_size", "inventory_version"},
		entry.Key, entry.AssetID, entry.Generation, entry.Rendition, entry.Metadata.Checksum, entry.Metadata.Size, 1, firstSeenAt)
}

func (Repository) ObjectObservationFirstSeen(ctx context.Context, scope store.AccountScope, key string) (time.Time, error) {
	var firstSeen time.Time
	err := scope.QueryRow(ctx, "planning_media_object_inventory", "first_seen_at", "internal_object_key = $2", key).Scan(&firstSeen)
	if errors.Is(err, store.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	return firstSeen, err
}

func (Repository) DeleteObjectObservation(ctx context.Context, scope store.AccountScope, key string) error {
	_, err := scope.Delete(ctx, "planning_media_object_inventory", "internal_object_key = $2", key)
	return err
}

func (Repository) LockAsset(ctx context.Context, tx store.TxAccountScope, assetID string) (PlanAsset, error) {
	var a PlanAsset
	err := tx.QueryRowForUpdate(ctx, "planning_media_assets", "id,upload_context_plan_id,display_name,state,current_generation,revision,gc_eligible_at,gc_rule_version,created_at,updated_at,deleted_at", "id = $2", assetID).Scan(&a.ID, &a.UploadContextPlanID, &a.DisplayName, &a.State, &a.CurrentGeneration, &a.Revision, &a.GCEligibleAt, &a.GCRuleVersion, &a.CreatedAt, &a.UpdatedAt, &a.DeletedAt)
	if errors.Is(err, store.ErrNoRows) {
		return PlanAsset{}, ErrNotFound
	}
	return a, err
}

func (Repository) RightsForGeneration(ctx context.Context, tx store.TxAccountScope, assetID string, generation int) (RightsDeclarationInput, error) {
	var rights RightsDeclarationInput
	err := tx.QueryRow(ctx, "planning_media_rights_declarations", "source_class,rights_basis,evidence_summary,license_generation_reference_granted", "asset_id = $2 AND generation = $3", assetID, generation).Scan(&rights.SourceClass, &rights.RightsBasis, &rights.EvidenceSummary, &rights.LicenseGenerationReferenceGranted)
	if errors.Is(err, store.ErrNoRows) {
		return RightsDeclarationInput{}, ErrNotFound
	}
	return rights, err
}

func (Repository) InsertBinding(ctx context.Context, tx store.TxAccountScope, b AssetBinding) error {
	return tx.Insert(ctx, "planning_media_bindings", []string{"id", "asset_id", "generation", "holder_kind", "holder_id", "plan_id", "purpose", "state", "revision", "created_at"}, b.ID, b.AssetID, b.Generation, b.HolderKind, b.HolderID, b.PlanID, b.Purpose, b.State, b.Revision, b.CreatedAt)
}
func (Repository) FindActiveBinding(ctx context.Context, tx store.TxAccountScope, assetID string, generation int, kind HolderKind, holderID string, purpose Purpose) (AssetBinding, error) {
	var b AssetBinding
	err := tx.QueryRow(ctx, "planning_media_bindings", "id,asset_id,generation,holder_kind,holder_id,plan_id,purpose,state,revision,created_at,released_at", "asset_id = $2 AND generation = $3 AND holder_kind = $4 AND holder_id = $5 AND purpose = $6 AND state = 'active'", assetID, generation, kind, holderID, purpose).Scan(&b.ID, &b.AssetID, &b.Generation, &b.HolderKind, &b.HolderID, &b.PlanID, &b.Purpose, &b.State, &b.Revision, &b.CreatedAt, &b.ReleasedAt)
	if errors.Is(err, store.ErrNoRows) {
		return AssetBinding{}, ErrNotFound
	}
	return b, err
}
func (Repository) FindActiveBindingForPlan(ctx context.Context, tx store.TxAccountScope, assetID string, generation int, planID string) (AssetBinding, error) {
	var b AssetBinding
	rows, err := tx.Query(ctx, "planning_media_bindings", "id,asset_id,generation,holder_kind,holder_id,plan_id,purpose,state,revision,created_at,released_at", "asset_id = $2 AND generation = $3 AND plan_id = $4 AND state = 'active'", assetID, generation, planID)
	if err != nil {
		return AssetBinding{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return AssetBinding{}, err
		}
		return AssetBinding{}, ErrNotFound
	}
	if err := rows.Scan(&b.ID, &b.AssetID, &b.Generation, &b.HolderKind, &b.HolderID, &b.PlanID, &b.Purpose, &b.State, &b.Revision, &b.CreatedAt, &b.ReleasedAt); err != nil {
		return AssetBinding{}, err
	}
	return b, nil
}
func (Repository) LockBinding(ctx context.Context, tx store.TxAccountScope, id string) (AssetBinding, error) {
	var b AssetBinding
	err := tx.QueryRowForUpdate(ctx, "planning_media_bindings", "id,asset_id,generation,holder_kind,holder_id,plan_id,purpose,state,revision,created_at,released_at", "id = $2", id).Scan(&b.ID, &b.AssetID, &b.Generation, &b.HolderKind, &b.HolderID, &b.PlanID, &b.Purpose, &b.State, &b.Revision, &b.CreatedAt, &b.ReleasedAt)
	if errors.Is(err, store.ErrNoRows) {
		return AssetBinding{}, ErrNotFound
	}
	return b, err
}
func (Repository) LockLease(ctx context.Context, tx store.TxAccountScope, id string) (AssetLease, error) {
	var lease AssetLease
	err := tx.QueryRowForUpdate(ctx, "planning_media_leases", "id,asset_id,generation,binding_id,owner_kind,owner_id,purpose,state,expires_at,revision", "id = $2", id).Scan(&lease.ID, &lease.AssetID, &lease.Generation, &lease.BindingID, &lease.OwnerKind, &lease.OwnerID, &lease.Purpose, &lease.State, &lease.ExpiresAt, &lease.Revision)
	if errors.Is(err, store.ErrNoRows) {
		return AssetLease{}, ErrNotFound
	}
	return lease, err
}
func (Repository) FindLease(ctx context.Context, tx store.TxAccountScope, id string) (AssetLease, error) {
	var lease AssetLease
	err := tx.QueryRow(ctx, "planning_media_leases", "id,asset_id,generation,binding_id,owner_kind,owner_id,purpose,state,expires_at,revision", "id = $2", id).Scan(&lease.ID, &lease.AssetID, &lease.Generation, &lease.BindingID, &lease.OwnerKind, &lease.OwnerID, &lease.Purpose, &lease.State, &lease.ExpiresAt, &lease.Revision)
	if errors.Is(err, store.ErrNoRows) {
		return AssetLease{}, ErrNotFound
	}
	return lease, err
}
func (Repository) ReleaseBinding(ctx context.Context, tx store.TxAccountScope, b AssetBinding, now time.Time) error {
	n, err := tx.Update(ctx, "planning_media_bindings", "state = $2, revision = revision + 1, released_at = $3", "id = $4 AND revision = $5 AND state = 'active'", BindingReleased, now, b.ID, b.Revision)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAssetRevisionConflict
	}
	return nil
}
func (Repository) ActiveBindingsForAsset(ctx context.Context, tx store.TxAccountScope, assetID string) (int64, error) {
	return tx.Count(ctx, "planning_media_bindings", "asset_id = $2 AND state = 'active'", assetID)
}
func (Repository) ActiveLeaseCount(ctx context.Context, tx store.TxAccountScope, assetID string, now time.Time) (int64, error) {
	return tx.Count(ctx, "planning_media_leases", "asset_id = $2 AND state = 'active' AND expires_at > $3", assetID, now)
}
func (Repository) ActivePinCount(ctx context.Context, tx store.TxAccountScope, assetID string, now time.Time) (int64, error) {
	return tx.Count(ctx, "planning_media_read_pins", "asset_id = $2 AND state = 'active' AND expires_at > $3", assetID, now)
}
