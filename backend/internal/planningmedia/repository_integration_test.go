package planningmedia

import (
	"context"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
)

func TestMain(m *testing.M) { storetest.Main(m, store.MigrateUp) }

func TestPostgresPlanningMediaSchemaRepositoryAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	database := openPlanningMediaStore(t)
	scopeA := createPlanningMediaAccount(t, database, "planning-media-a")
	scopeB := createPlanningMediaAccount(t, database, "planning-media-b")
	if err := scopeA.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "plan-a", "策划 A", "角色 A"); err != nil {
		t.Fatal(err)
	}
	if err := scopeB.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "plan-b", "策划 B", "角色 B"); err != nil {
		t.Fatal(err)
	}

	repository := Repository{}
	record := postgresAssetRecord(t, "planning-media-a", "plan-a", "asset-a", 1, SourceOfficial, RightsCitationOrDisplay)
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return repository.InsertAsset(ctx, tx, record)
	}); err != nil {
		t.Fatalf("insert exact generation: %v", err)
	}
	page, err := repository.ListForPlan(ctx, scopeA, "plan-a", "", 40)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "asset-a" || page.Items[0].DisplayChecksum != record.Display.Checksum {
		t.Fatalf("gallery page=%+v err=%v", page, err)
	}
	crossAccount, err := repository.ListForPlan(ctx, scopeB, "plan-a", "", 40)
	if err != nil || len(crossAccount.Items) != 0 {
		t.Fatalf("cross-account gallery leaked: %+v err=%v", crossAccount, err)
	}
	expected, err := repository.ExpectedInventory(ctx, scopeA)
	if err != nil || len(expected) != 2 || expected[0].Key >= expected[1].Key {
		t.Fatalf("expected inventory=%+v err=%v", expected, err)
	}
	for _, entry := range expected {
		if entry.AssetID != "asset-a" || entry.Generation != 1 || entry.Metadata.Checksum == "" {
			t.Fatalf("inventory lost exact identity: %+v", entry)
		}
	}
	otherExpected, err := repository.ExpectedInventory(ctx, scopeB)
	if err != nil || len(otherExpected) != 0 {
		t.Fatalf("cross-account inventory leaked: %+v err=%v", otherExpected, err)
	}

	wrongGeneration := postgresAssetRecord(t, "planning-media-a", "plan-a", "asset-wrong-generation", 1, SourceOfficial, RightsCitationOrDisplay)
	wrongGeneration.Asset.CurrentGeneration = 2
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return repository.InsertAsset(ctx, tx, wrongGeneration)
	}); err == nil {
		t.Fatal("asset current_generation without exact generation committed")
	}
	count, err := scopeA.Count(ctx, "planning_media_assets", "id = $2", wrongGeneration.Asset.ID)
	if err != nil || count != 0 {
		t.Fatalf("failed deferred generation transaction was not rolled back: count=%d err=%v", count, err)
	}

	invalidRights := postgresAssetRecord(t, "planning-media-a", "plan-a", "asset-invalid-rights", 1, SourceOfficial, RightsOwnershipAttested)
	if err := scopeA.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return repository.InsertAsset(ctx, tx, invalidRights)
	}); err == nil {
		t.Fatal("database accepted invalid source/rights combination")
	}
}

func TestPostgresBatchShotAccessRefsProjectionIsBoundedAndStable(t *testing.T) {
	ctx := context.Background()
	database := openPlanningMediaStore(t)
	scope := createPlanningMediaAccount(t, database, "planning-media-projection")
	if err := scope.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "plan-projection", "Projection", "Subject"); err != nil {
		t.Fatal(err)
	}
	record := postgresAssetRecord(t, "planning-media-projection", "plan-projection", "asset-projection", 1, SourceOfficial, RightsCitationOrDisplay)
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := (Repository{}).InsertAsset(ctx, tx, record); err != nil {
			return err
		}
		return (Repository{}).InsertBinding(ctx, tx, AssetBinding{ID: "binding-projection", AssetID: record.Asset.ID, Generation: 1, HolderKind: HolderShot, HolderID: "shot-projection", PlanID: "plan-projection", Purpose: PurposeShotReferenceDisplay, State: BindingActive, Revision: 1, CreatedAt: record.Asset.CreatedAt})
	}); err != nil {
		t.Fatal(err)
	}
	var refs map[string][]AssetAccessRef
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		refs, err = (&Application{}).BatchShotAccessRefsInScope(ctx, tx, "plan-projection", []string{"shot-projection", "shot-missing"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs["shot-projection"]) != 1 || refs["shot-projection"][0].AssetID != record.Asset.ID || len(refs["shot-missing"]) != 0 {
		t.Fatalf("refs=%+v", refs)
	}
}

func postgresAssetRecord(t *testing.T, accountID, planID, assetID string, generation int, source SourceClass, basis RightsBasis) CreateAssetRecord {
	t.Helper()
	now := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	originalBody := []byte(assetID + "-original")
	displayBody := []byte(assetID + "-display")
	originalKey, err := planningObjectKey(accountID, assetID, generation, RenditionOriginal, digest(originalBody))
	if err != nil {
		t.Fatal(err)
	}
	displayKey, err := planningObjectKey(accountID, assetID, generation, RenditionDisplay, digest(displayBody))
	if err != nil {
		t.Fatal(err)
	}
	rights := RightsDeclaration{
		ID: "rights-" + assetID, AssetID: assetID, Generation: generation,
		SourceClass: source, RightsBasis: basis, MatrixVersion: MatrixVersion, DeclaredAt: now,
	}
	eligibleAt := now.Add(48 * time.Hour)
	return CreateAssetRecord{
		Asset: PlanAsset{
			ID: assetID, UploadContextPlanID: planID, DisplayName: assetID, State: AssetStaged,
			CurrentGeneration: generation, Revision: 1, GCEligibleAt: &eligibleAt, GCRuleVersion: 1,
			CreatedAt: now, UpdatedAt: now,
		},
		Rights: rights,
		Generation: AssetGeneration{
			AssetID: assetID, Generation: generation, Rights: rights,
			OriginalChecksum: digest(originalBody), DisplayChecksum: digest(displayBody), CreatedAt: now,
		},
		Original: AssetRendition{
			AssetID: assetID, Generation: generation, Kind: RenditionOriginal, MediaType: "image/png",
			ByteSize: int64(len(originalBody)), Width: 1, Height: 1, Checksum: digest(originalBody), objectKey: originalKey,
		},
		Display: AssetRendition{
			AssetID: assetID, Generation: generation, Kind: RenditionDisplay, MediaType: "image/png",
			ByteSize: int64(len(displayBody)), Width: 1, Height: 1, Checksum: digest(displayBody), objectKey: displayKey,
		},
	}
}

func openPlanningMediaStore(t *testing.T) *store.Store {
	database, _ := openPlanningMediaStoreWithURL(t)
	return database
}

func openPlanningMediaStoreWithURL(t *testing.T) (*store.Store, string) {
	t.Helper()
	ctx := context.Background()
	url := storetest.NewURL(t)
	database, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	return database, url
}

func createPlanningMediaAccount(t *testing.T, database *store.Store, accountID string) store.AccountScope {
	t.Helper()
	if err := database.CreateAccount(context.Background(), accountID, "test-hash"); err != nil {
		t.Fatal(err)
	}
	return database.ScopeFor(auth.AccountContext{AccountID: accountID})
}

func TestGalleryProjectionReturnsRightsSnapshotAndActiveBindings(t *testing.T) {
	ctx := context.Background()
	database := openPlanningMediaStore(t)
	scope := createPlanningMediaAccount(t, database, "planning-media-gallery")
	if err := scope.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "plan-gallery", "策划", "角色"); err != nil {
		t.Fatal(err)
	}
	repository := Repository{}
	record := postgresAssetRecord(t, "planning-media-gallery", "plan-gallery", "asset-gallery", 1, SourceAnimeScreenshot, RightsCitationOrDisplay)
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return repository.InsertAsset(ctx, tx, record)
	}); err != nil {
		t.Fatal(err)
	}

	page, err := repository.ListForPlan(ctx, scope, "plan-gallery", "", 40)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("gallery page=%+v err=%v", page, err)
	}
	asset := page.Items[0]
	if asset.Rights == nil || asset.Rights.SourceClass != SourceAnimeScreenshot ||
		asset.Rights.RightsBasis != RightsCitationOrDisplay || asset.Rights.Generation != 1 {
		t.Fatalf("rights snapshot=%+v", asset.Rights)
	}
	if asset.ActiveBindings == nil || len(asset.ActiveBindings) != 0 {
		t.Fatalf("expected empty active bindings, got %+v", asset.ActiveBindings)
	}

	now := time.Now().UTC()
	bindingColumns := []string{"id", "asset_id", "generation", "holder_kind", "holder_id", "plan_id", "purpose", "state", "revision", "created_at"}
	if err := scope.Insert(ctx, "planning_media_bindings", bindingColumns,
		"bind-plan", "asset-gallery", 1, "plan", "plan-gallery", "plan-gallery", "moodboard_display", "active", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := scope.Insert(ctx, "planning_media_bindings", bindingColumns,
		"bind-shot", "asset-gallery", 1, "shot", "shot-1", "plan-gallery", "shot_reference_display", "active", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := scope.Insert(ctx, "planning_media_bindings", append(bindingColumns, "released_at"),
		"bind-released", "asset-gallery", 1, "plan", "plan-gallery", "plan-gallery", "moodboard_display", "released", 1, now, now); err != nil {
		t.Fatal(err)
	}

	page, err = repository.ListForPlan(ctx, scope, "plan-gallery", "", 40)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("gallery page after binding=%+v err=%v", page, err)
	}
	bindings := page.Items[0].ActiveBindings
	if len(bindings) != 2 {
		t.Fatalf("expected 2 active bindings, got %+v", bindings)
	}
	byHolder := map[string]AssetBinding{}
	for _, binding := range bindings {
		byHolder[string(binding.HolderKind)+"/"+binding.HolderID] = binding
	}
	if _, ok := byHolder["plan/plan-gallery"]; !ok {
		t.Fatalf("missing plan binding: %+v", bindings)
	}
	shot, ok := byHolder["shot/shot-1"]
	if !ok || shot.Purpose != PurposeShotReferenceDisplay || shot.PlanID != "plan-gallery" {
		t.Fatalf("missing shot binding: %+v", shot)
	}
}
