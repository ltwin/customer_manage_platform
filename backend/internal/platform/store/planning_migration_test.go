package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/planningcapability"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestShootPlanningCoreMigrationSeedIsStableAndDownIsComplete(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	var capability string
	var revision int64
	if err := db.QueryRowContext(ctx, `SELECT capability, revision FROM planning_archive_capability_state WHERE singleton_key='planning-archive-v1'`).Scan(&capability, &revision); err != nil {
		t.Fatalf("read bootstrap marker: %v", err)
	}
	if capability != "core-v1" || revision != 1 {
		t.Fatalf("bootstrap marker = %s/%d", capability, revision)
	}
	if _, err := db.ExecContext(ctx, `UPDATE planning_archive_capability_state SET capability='planning-share-v1', revision=2`); err != nil {
		t.Fatalf("promote marker fixture: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("repeat migrate up: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT capability, revision FROM planning_archive_capability_state`).Scan(&capability, &revision); err != nil {
		t.Fatalf("read marker after repeat up: %v", err)
	}
	if capability != "planning-share-v1" || revision != 2 {
		t.Fatalf("repeat migration overwrote marker: %s/%d", capability, revision)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// share(0021-0024), security-budget(0020), plan-crm(0019), plan-ingestion(0018),
	// planning-media(0017) must be rolled back before the core planning migration(0016).
	for _, label := range []string{
		"creative-canvas-commands",
		"creative-library-organization",
		"creative-text-canvas",
		"creative-foundation",
		"creative-workspace",
		"settings-health-tiers",
		"orders-attribution-snapshot",
		"orders-payment-facts",
		"orders-delivery-due",
		"media-gallery-rights-bindings",
		"plan-list-enrichment",
		"plan-business-feedback",
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal", "plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share-assignments", "share-feedbacks", "share-anonymous", "share-generations",
		"security-budget", "plan-crm", "plan-ingestion", "planning-media", "planning",
	} {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("migrate %s down: %v", label, err)
		}
	}
	db, err = sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var relations int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE 'shoot_plan%'`).Scan(&relations); err != nil {
		t.Fatal(err)
	}
	if relations != 0 {
		t.Fatalf("planning down left %d shoot_plan relations", relations)
	}
}

func TestArchiveCapabilityPromotionWaitsForTransactionReaderSharedLock(t *testing.T) {
	ctx := context.Background()
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateAccount(ctx, "capability-lock-account", "hash"); err != nil {
		t.Fatal(err)
	}
	scope := db.ScopeFor(auth.AccountContext{AccountID: "capability-lock-account"})
	readerReady := make(chan struct{})
	releaseReader := make(chan struct{})
	readerDone := make(chan error, 1)
	go func() {
		readerDone <- scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if _, err := tx.ArchiveCapability().Current(ctx); err != nil {
				return err
			}
			close(readerReady)
			<-releaseReader
			return nil
		})
	}()
	<-readerReady
	now := time.Now().UTC()
	promotionDone := make(chan error, 1)
	go func() {
		_, err := db.ArchiveCapabilityPromoter().Promote(ctx, 1,
			planningcapability.ArchiveCapabilityPlanningShare,
			planningcapability.ReleaseReadinessEvidence{
				Digest: "fixture-digest", Environment: "test", Deployment: "test-1",
				Target: planningcapability.ArchiveCapabilityPlanningShare, CurrentRevision: 1,
				GeneratedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute),
				LiveBuildsCompatible: true, TargetWiringReady: true,
			})
		promotionDone <- err
	}()
	select {
	case err := <-promotionDone:
		t.Fatalf("promotion bypassed shared transaction lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseReader)
	if err := <-readerDone; err != nil {
		t.Fatalf("transaction reader: %v", err)
	}
	if err := <-promotionDone; err != nil {
		t.Fatalf("promotion after reader commit: %v", err)
	}
	state, err := db.ArchiveCapabilityStartupReader().Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Capability != planningcapability.ArchiveCapabilityPlanningShare || state.Revision != 2 {
		t.Fatalf("unexpected promoted state: %+v", state)
	}
}
