package planningmedia

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/immutablefs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// This fixture exercises the cross-table lifecycle against real PostgreSQL:
// a bound read uses the binding anchor, detach preserves the 48h asset grace,
// a staged read uses upload_context, and GC only finalizes after exact delete.
func TestPostgresPlanningMediaBindingReadAndGCLifecycle(t *testing.T) {
	ctx := context.Background()
	database := openPlanningMediaStore(t)
	scope := createPlanningMediaAccount(t, database, "planning-media-lifecycle")
	if err := scope.Insert(ctx, "shoot_plans", []string{"id", "title", "subject"}, "plan-lifecycle", "Lifecycle", "Subject"); err != nil {
		t.Fatal(err)
	}
	record := postgresAssetRecord(t, "planning-media-lifecycle", "plan-lifecycle", "asset-lifecycle", 1, SourceOfficial, RightsCitationOrDisplay)
	if err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error { return (Repository{}).InsertAsset(ctx, tx, record) }); err != nil {
		t.Fatal(err)
	}
	objects, err := immutablefs.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalBody := []byte("asset-lifecycle-original")
	displayBody := []byte("asset-lifecycle-display")
	for _, object := range []struct {
		key  string
		body []byte
		meta immutablefs.Metadata
	}{
		{record.Original.objectKey, originalBody, immutablefs.Metadata{MediaType: "image/png", Size: int64(len(originalBody)), Width: 1, Height: 1, Checksum: digest(originalBody)}},
		{record.Display.objectKey, displayBody, immutablefs.Metadata{MediaType: "image/png", Size: int64(len(displayBody)), Width: 1, Height: 1, Checksum: digest(displayBody)}},
	} {
		if _, _, err := objects.PutImmutable(ctx, object.key, object.body, object.meta); err != nil {
			t.Fatal(err)
		}
	}
	clock := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	app := NewApplication(Repository{}, idempotency.NewExecutor(), objects, WithHolderAuthorizer(allowPlanningMediaHolder{}), WithClock(func() time.Time { return clock }))
	bindingResult, err := app.CreateBinding(ctx, scope, "bind-lifecycle", CreateBindingInput{
		PlanID: "plan-lifecycle", AssetID: "asset-lifecycle", Generation: 1, HolderKind: HolderPlan, HolderID: "plan-lifecycle", Purpose: PurposeMoodboardDisplay, ExpectedPlanRevision: 1, ExpectedAssetRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bindingResult.Asset.State != AssetActive {
		t.Fatalf("binding did not activate asset: %+v", bindingResult.Asset)
	}
	blocking := &blockingOpenStore{ObjectStore: objects, started: make(chan struct{}), release: make(chan struct{})}
	readApp := NewApplication(Repository{}, idempotency.NewExecutor(), blocking, WithHolderAuthorizer(allowPlanningMediaHolder{}), WithClock(func() time.Time { return clock }))
	readDone := make(chan error, 1)
	go func() {
		stream, readErr := readApp.OpenDisplay(ctx, scope, "plan-lifecycle", "asset-lifecycle", record.Display.Checksum)
		if readErr == nil {
			_, readErr = io.ReadAll(stream.Reader)
			closeErr := stream.Close()
			if readErr == nil {
				readErr = closeErr
			}
		}
		readDone <- readErr
	}()
	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("read did not reach object open while holding asset lock")
	}
	releaseDone := make(chan error, 1)
	go func() {
		_, releaseErr := app.ReleaseBinding(ctx, scope, "release-concurrent", "plan-lifecycle", "asset-lifecycle", bindingResult.Binding.ID, 1, bindingResult.Asset.Revision, 1)
		releaseDone <- releaseErr
	}()
	// Permit issuance has committed its pin before opening the stream, so
	// detach is allowed to proceed while the response body is still open.
	if releaseErr := <-releaseDone; releaseErr != nil {
		t.Fatalf("detach after permit issuance failed: %v", releaseErr)
	}
	close(blocking.release)
	if readErr := <-readDone; readErr != nil {
		t.Fatalf("bound read failed: %v", readErr)
	}
	var bindingPins, contextPins, releasedPins int64
	var countErr error
	if bindingPins, countErr = scope.Count(ctx, "planning_media_read_pins", "asset_id = $2 AND authorization_anchor = 'binding'", "asset-lifecycle"); countErr != nil {
		t.Fatal(countErr)
	}
	if contextPins, countErr = scope.Count(ctx, "planning_media_read_pins", "asset_id = $2 AND authorization_anchor = 'upload_context'", "asset-lifecycle"); countErr != nil {
		t.Fatal(countErr)
	}
	if releasedPins, countErr = scope.Count(ctx, "planning_media_read_pins", "asset_id = $2 AND state = 'released'", "asset-lifecycle"); countErr != nil {
		t.Fatal(countErr)
	}
	if bindingPins != 1 || contextPins != 0 || releasedPins != 1 {
		t.Fatalf("bound read pin anchors binding=%d context=%d released=%d", bindingPins, contextPins, releasedPins)
	}
	released, err := loadAssetForLifecycle(ctx, scope, "asset-lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	if released.State != AssetStaged || released.GCEligibleAt == nil || !released.GCEligibleAt.Equal(record.Asset.CreatedAt.Add(48*time.Hour)) {
		t.Fatalf("detach grace incorrect: %+v", released)
	}
	clock = record.Asset.CreatedAt.Add(48 * time.Hour)
	stagedStream, err := app.OpenDisplay(ctx, scope, "plan-lifecycle", "asset-lifecycle", record.Display.Checksum)
	if err != nil {
		t.Fatal(err)
	}
	stagedBytes, err := io.ReadAll(stagedStream.Reader)
	if err != nil || string(stagedBytes) != string(displayBody) {
		t.Fatalf("staged read content=%q err=%v", stagedBytes, err)
	}
	if contextPins, countErr = scope.Count(ctx, "planning_media_read_pins", "asset_id = $2 AND authorization_anchor = 'upload_context'", "asset-lifecycle"); countErr != nil {
		t.Fatal(countErr)
	}
	if contextPins != 1 {
		t.Fatalf("staged read did not use upload-context anchor: %d", contextPins)
	}
	blockedGC, err := app.ReconcileGC(ctx, scope, record.Asset.CreatedAt.Add(48*time.Hour), 10)
	if err != nil || len(blockedGC) != 1 || blockedGC[0].Deleted {
		t.Fatalf("active read pin did not block gc: results=%+v err=%v", blockedGC, err)
	}
	if err := stagedStream.Close(); err != nil {
		t.Fatal(err)
	}
	faultApp := NewApplication(Repository{}, idempotency.NewExecutor(), failingDeleteStore{ObjectStore: objects}, WithHolderAuthorizer(allowPlanningMediaHolder{}), WithClock(func() time.Time { return clock }))
	faultResults, err := faultApp.ReconcileGC(ctx, scope, record.Asset.CreatedAt.Add(48*time.Hour), 10)
	if err != nil || len(faultResults) != 1 || faultResults[0].Error == "" {
		t.Fatalf("gc delete failure was not retained: results=%+v err=%v", faultResults, err)
	}
	var state AssetState
	if err := scope.QueryRow(ctx, "planning_media_assets", "state", "id = $2", record.Asset.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != AssetGCPending {
		t.Fatalf("failed delete finalized unexpectedly: %s", state)
	}
	results, err := app.ReconcileGC(ctx, scope, record.Asset.CreatedAt.Add(48*time.Hour), 10)
	if err != nil || len(results) != 1 || !results[0].Deleted {
		t.Fatalf("gc results=%+v err=%v", results, err)
	}
	if _, err := objects.Stat(ctx, record.Display.objectKey); err == nil {
		t.Fatal("display object survived exact GC")
	}
}

func loadAssetForLifecycle(ctx context.Context, scope store.AccountScope, assetID string) (PlanAsset, error) {
	var asset PlanAsset
	err := scope.QueryRow(ctx, "planning_media_assets", "id,upload_context_plan_id,display_name,state,current_generation,revision,gc_eligible_at,gc_rule_version,created_at,updated_at,deleted_at", "id = $2", assetID).Scan(&asset.ID, &asset.UploadContextPlanID, &asset.DisplayName, &asset.State, &asset.CurrentGeneration, &asset.Revision, &asset.GCEligibleAt, &asset.GCRuleVersion, &asset.CreatedAt, &asset.UpdatedAt, &asset.DeletedAt)
	return asset, err
}

type failingDeleteStore struct{ immutablefs.ObjectStore }

func (failingDeleteStore) Delete(context.Context, string) error {
	return errors.New("injected_delete_failure")
}

type blockingOpenStore struct {
	immutablefs.ObjectStore
	started chan struct{}
	release chan struct{}
}

func (b *blockingOpenStore) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	select {
	case <-b.release:
		return b.ObjectStore.Open(ctx, key)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
