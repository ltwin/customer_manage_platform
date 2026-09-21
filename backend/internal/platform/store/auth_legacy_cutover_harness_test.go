package store_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func TestAuthLegacyCutoverHarness(t *testing.T) {
	// down 走查的覆盖度先查：漏扩时几毫秒内就报错，不必等容器起完再失败。
	downWalk := legacyRollbackDownWalk(t)
	url := startPostgres(t)
	cutover := runLegacyCutoverFixture(t, url)
	rollback := runLegacyRollbackFixtures(t, url, downWalk)
	logHarnessReport(t, cutover)
	logHarnessReport(t, rollback)
}

type legacyCutoverHarnessReport struct {
	Report                  string `json:"report"`
	DryRunState             string `json:"dry_run_state"`
	ClaimState              string `json:"claim_state"`
	AccountRef              string `json:"account_ref"`
	EmailRef                string `json:"email_ref"`
	AccountIDPreserved      bool   `json:"account_id_preserved"`
	BusinessCountsPreserved bool   `json:"business_counts_preserved"`
	AvatarChecksumPreserved bool   `json:"avatar_checksum_preserved"`
	LegacyUnclaimedCount    int    `json:"legacy_unclaimed_count"`
	PendingClaimCount       int    `json:"pending_claim_count"`
	ActiveClaimedCount      int    `json:"active_claimed_count"`
}

type legacyRollbackHarnessReport struct {
	Report                       string `json:"report"`
	MigrationChecksum            string `json:"migration_checksum"`
	LegacySchemaVersionBefore    uint   `json:"legacy_schema_version_before"`
	LegacySchemaVersionAfter     uint   `json:"legacy_schema_version_after"`
	LimiterSchemaRollback        bool   `json:"limiter_schema_rollback"`
	LegacyDownPassed             bool   `json:"legacy_down_passed"`
	LegacyDataPreserved          bool   `json:"legacy_data_preserved"`
	NewStyleDownBlocked          bool   `json:"new_style_down_blocked"`
	NewStyleMarker               string `json:"new_style_marker"`
	NewStyleAccountPreserved     bool   `json:"new_style_account_preserved"`
	OldBinaryDegradationBoundary string `json:"old_binary_degradation_boundary"`
}

type legacyBusinessSnapshot struct {
	AccountID        string
	Customers        int
	Orders           int
	Schedules        int
	Reminders        int
	Settings         int
	Checkpoints      int
	AvatarVersion    string
	AvatarObjectID   string
	AvatarRevision   int64
	AvatarFileDigest string
}

func runLegacyCutoverFixture(t *testing.T, url string) legacyCutoverHarnessReport {
	t.Helper()
	resetAuthSchema(t, url)
	ctx := context.Background()
	database, err := store.Open(ctx, url)
	if err != nil {
		t.Fatalf("open cutover fixture store: %v", err)
	}
	defer database.Close()
	const accountID = "legacy-cutover-account"
	if err := database.CreateAccount(ctx, accountID, "deprecated-legacy-hash"); err != nil {
		t.Fatalf("create legacy cutover account: %v", err)
	}
	avatarPath := seedLegacyBusinessFixture(t, url, accountID)
	before := readLegacyBusinessSnapshot(t, url, accountID, avatarPath)

	now := time.Date(2026, 7, 31, 8, 9, 10, 0, time.UTC)
	mail := &capturingAuthMail{}
	service := newTestAuthService(database, &now, mail, 51, auth.RegistrationPublic, "synthetic-root")
	email := syntheticEmail("legacy-cutover")
	dryRun, err := service.BeginLegacyClaim(ctx, email, true)
	if err != nil || dryRun.State != auth.LegacyClaimReady || !dryRun.DryRun {
		t.Fatalf("legacy cutover dry-run: state=%q dry_run=%t err=%v", dryRun.State, dryRun.DryRun, err)
	}
	if mail.Count() != 0 {
		t.Fatal("legacy cutover dry-run sent mail")
	}
	if afterDryRun := readLegacyBusinessSnapshot(t, url, accountID, avatarPath); afterDryRun != before {
		t.Fatalf("legacy dry-run changed business fixture: before=%+v after=%+v", before, afterDryRun)
	}

	claim, err := service.BeginLegacyClaim(ctx, email, false)
	if err != nil || claim.State != auth.LegacyClaimReady || !claim.Delivery.Accepted {
		t.Fatalf("legacy claim: state=%q accepted=%t err=%v", claim.State, claim.Delivery.Accepted, err)
	}
	if _, err := service.VerifyEmail(ctx, actionTokenFromMail(t, mail.Last()), testClientMeta()); err != nil {
		t.Fatalf("verify legacy claim: %v", err)
	}
	after := readLegacyBusinessSnapshot(t, url, accountID, avatarPath)
	state, err := service.InspectLegacyState(ctx)
	if err != nil {
		t.Fatalf("inspect claimed legacy state: %v", err)
	}
	return legacyCutoverHarnessReport{
		Report: "auth_legacy_cutover", DryRunState: string(dryRun.State), ClaimState: string(claim.State),
		AccountRef: claim.AccountIDRedacted, EmailRef: claim.EmailRedacted,
		AccountIDPreserved:      before.AccountID == after.AccountID,
		BusinessCountsPreserved: businessCountsEqual(before, after),
		AvatarChecksumPreserved: before.AvatarVersion == after.AvatarVersion &&
			before.AvatarObjectID == after.AvatarObjectID && before.AvatarRevision == after.AvatarRevision &&
			before.AvatarFileDigest == after.AvatarFileDigest,
		LegacyUnclaimedCount: state.LegacyUnclaimedCount,
		PendingClaimCount:    state.PendingClaimCount,
		ActiveClaimedCount:   state.ActiveClaimedCount,
	}
}

// legacyAuthMigrationVersion 是 account-auth 迁移（0012）的版本号，回滚走查的终点。
const legacyAuthMigrationVersion uint = 12

// legacyRollbackDownWalk 返回从迁移顶点逐级 down 回 0012 要走的标签，并校验步数与
// migrations 目录的顶点一致。步数曾被硬钉成常量，加迁移时漏改过三次（18→24、
// 29→32、34→35），每次都让 limiter_schema_rollback 假红；顶点本身不是要守的不变量，
// 要守的是「down 走查覆盖了 0012 以上的每一级」。
func legacyRollbackDownWalk(t *testing.T) []string {
	t.Helper()
	walk := []string{
		"generation-media-facts", "execution-continuations", "agent-controls", "agent-checkpoints", "agent-context", "agent-runs", "skill-foundation", "agent-conversations", "llm-gateway",
		"creative-canvas-events",
		"creative-media",
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
		"plan-assignment-reminder-digest-intent", "plan-assignment-reminder-temporal",
		"plan-assignment-reminder-ingestion", "plan-assignment-reminders",
		"share assignments", "share feedbacks", "share anonymous projection", "share generations",
		"security attempt budget", "plan crm", "plan ingestion", "planning media", "shoot planning",
		"account profiles", "settings availability", "limiter schema",
	}
	want := int(migrationTip(t)) - int(legacyAuthMigrationVersion)
	if len(walk) != want {
		t.Fatalf("legacy rollback down walk covers %d migrations, migrations tip needs %d; "+
			"extend the walk when adding a migration", len(walk), want)
	}
	return walk
}

// migrationTip 从 migrations 目录读出最高迁移号。
func migrationTip(t *testing.T) uint {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join("migrations", "*.up.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations found under migrations/")
	}
	var tip uint
	for _, entry := range entries {
		name := filepath.Base(entry)
		prefix, _, found := strings.Cut(name, "_")
		if !found {
			t.Fatalf("migration %q has no NNNN_ prefix", name)
		}
		version, err := strconv.ParseUint(prefix, 10, 32)
		if err != nil {
			t.Fatalf("migration %q has no numeric prefix: %v", name, err)
		}
		if uint(version) > tip {
			tip = uint(version)
		}
	}
	return tip
}

func runLegacyRollbackFixtures(t *testing.T, url string, downWalk []string) legacyRollbackHarnessReport {
	t.Helper()
	ctx := context.Background()
	resetToMigrationStep(t, url, 11)
	db := openSQLDatabase(t, url)
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, password_hash) VALUES ('rollback-legacy', 'deprecated-hash')`); err != nil {
		t.Fatalf("seed rollback legacy account: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO customers (id, account_id, display_name, channel)
		VALUES ('rollback-customer', 'rollback-legacy', '回滚客户', 'other')`); err != nil {
		t.Fatalf("seed rollback legacy customer: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close rollback legacy fixture: %v", err)
	}
	if err := store.MigrateUp(url); err != nil {
		t.Fatalf("migrate rollback legacy fixture up: %v", err)
	}
	fullVersion := migrationVersion(t, url)
	for _, label := range downWalk {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("%s down: %v", label, err)
		}
	}
	versionBefore := migrationVersion(t, url)
	if err := store.MigrateDownOneForTest(url); err != nil {
		t.Fatalf("legacy-only auth down: %v", err)
	}
	versionAfter := migrationVersion(t, url)
	db = openSQLDatabase(t, url)
	var legacyHash string
	var customerCount int
	if err := db.QueryRowContext(ctx, `SELECT password_hash FROM accounts WHERE id = 'rollback-legacy'`).Scan(&legacyHash); err != nil {
		t.Fatalf("read legacy hash after down: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM customers WHERE account_id = 'rollback-legacy'`).Scan(&customerCount); err != nil {
		t.Fatalf("read legacy business data after down: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy down database: %v", err)
	}

	resetAuthSchema(t, url)
	for _, label := range downWalk {
		if err := store.MigrateDownOneForTest(url); err != nil {
			t.Fatalf("prepare %s down: %v", label, err)
		}
	}
	db = openSQLDatabase(t, url)
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (id, status, password_hash)
		VALUES ('rollback-new-style', 'pending_verification', NULL)`); err != nil {
		t.Fatalf("seed new-style rollback account: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close new-style rollback fixture: %v", err)
	}
	downErr := store.MigrateDownOneForTest(url)
	db = openSQLDatabase(t, url)
	var newStyleCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM accounts
		WHERE id = 'rollback-new-style' AND password_hash IS NULL`).Scan(&newStyleCount); err != nil {
		t.Fatalf("read blocked new-style account: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close blocked new-style database: %v", err)
	}

	downSource, err := os.ReadFile("migrations/0012_account_auth.down.sql")
	if err != nil {
		t.Fatalf("read pinned auth down migration: %v", err)
	}
	digest := sha256.Sum256(downSource)
	return legacyRollbackHarnessReport{
		Report: "auth_legacy_rollback", MigrationChecksum: "sha256:" + hex.EncodeToString(digest[:]),
		LegacySchemaVersionBefore: versionBefore, LegacySchemaVersionAfter: versionAfter,
		// 断言「从真实顶点起步，一路 down 穿过 0013 limiter schema 落到 12」，
		// 以免把「只回滚了 limiter」误报成 account-auth rollback 已通过。
		// 顶点由 legacyRollbackDownWalk 从 migrations 目录读出，不再手钉常量。
		LimiterSchemaRollback:        fullVersion == legacyAuthMigrationVersion+uint(len(downWalk)) && versionBefore == legacyAuthMigrationVersion,
		LegacyDownPassed:             versionBefore == legacyAuthMigrationVersion && versionAfter == legacyAuthMigrationVersion-1,
		LegacyDataPreserved:          legacyHash == "deprecated-hash" && customerCount == 1,
		NewStyleDownBlocked:          downErr != nil && strings.Contains(downErr.Error(), "auth_schema_down_blocked_new_accounts"),
		NewStyleMarker:               "auth_schema_down_blocked_new_accounts",
		NewStyleAccountPreserved:     newStyleCount == 1,
		OldBinaryDegradationBoundary: "new_style_accounts_unavailable_to_old_binary",
	}
}

func seedLegacyBusinessFixture(t *testing.T, url, accountID string) string {
	t.Helper()
	db := openSQLDatabase(t, url)
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	avatarBody := []byte("synthetic-avatar-object")
	avatarDigest := sha256.Sum256(avatarBody)
	avatarVersion := "sha256-" + hex.EncodeToString(avatarDigest[:])
	avatarObjectID := "0123456789abcdef0123456789abcdef"
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO customers (
			id, account_id, display_name, channel, avatar_revision, avatar_version,
			avatar_object_id, avatar_media_type, avatar_size, avatar_updated_at
		) VALUES ('legacy-customer', $1, '旧客户', 'other', 1, $2, $3, 'image/png', $4, now())`,
			[]any{accountID, avatarVersion, avatarObjectID, len(avatarBody)}},
		{`INSERT INTO orders (id, account_id, customer_id, title)
			VALUES ('legacy-order', $1, 'legacy-customer', '旧订单')`, []any{accountID}},
		{`INSERT INTO schedule_slots (id, account_id, start_at, end_at, type, order_id)
			VALUES ('legacy-schedule', $1, now(), now() + interval '1 hour', 'shoot', 'legacy-order')`, []any{accountID}},
		{`INSERT INTO reminders (id, account_id, type, customer_id, order_id, due_date, content, dedup_key)
			VALUES ('legacy-reminder', $1, 'follow_up', 'legacy-customer', 'legacy-order', current_date, '旧提醒', 'legacy-dedup')`, []any{accountID}},
		{`INSERT INTO settings (account_id, timezone) VALUES ($1, 'Asia/Shanghai')`, []any{accountID}},
		{`INSERT INTO avatar_reconciliation_checkpoint (account_id, object_inventory_cycle, pointer_cycle)
			VALUES ($1, 3, 4)`, []any{accountID}},
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed legacy business fixture: %v", err)
		}
	}
	avatarPath := filepath.Join(t.TempDir(), accountID, "legacy-customer", avatarVersion, avatarObjectID)
	if err := os.MkdirAll(filepath.Dir(avatarPath), 0o750); err != nil {
		t.Fatalf("create synthetic avatar path: %v", err)
	}
	if err := os.WriteFile(avatarPath, avatarBody, 0o600); err != nil {
		t.Fatalf("write synthetic avatar object: %v", err)
	}
	return avatarPath
}

func readLegacyBusinessSnapshot(t *testing.T, url, accountID, avatarPath string) legacyBusinessSnapshot {
	t.Helper()
	db := openSQLDatabase(t, url)
	defer func() { _ = db.Close() }()
	var snapshot legacyBusinessSnapshot
	err := db.QueryRow(`SELECT
		(SELECT id FROM accounts WHERE id = $1),
		(SELECT count(*) FROM customers WHERE account_id = $1),
		(SELECT count(*) FROM orders WHERE account_id = $1),
		(SELECT count(*) FROM schedule_slots WHERE account_id = $1),
		(SELECT count(*) FROM reminders WHERE account_id = $1),
		(SELECT count(*) FROM settings WHERE account_id = $1),
		(SELECT count(*) FROM avatar_reconciliation_checkpoint WHERE account_id = $1),
		(SELECT avatar_version FROM customers WHERE account_id = $1 AND id = 'legacy-customer'),
		(SELECT avatar_object_id FROM customers WHERE account_id = $1 AND id = 'legacy-customer'),
		(SELECT avatar_revision FROM customers WHERE account_id = $1 AND id = 'legacy-customer')`, accountID).
		Scan(&snapshot.AccountID, &snapshot.Customers, &snapshot.Orders, &snapshot.Schedules,
			&snapshot.Reminders, &snapshot.Settings, &snapshot.Checkpoints,
			&snapshot.AvatarVersion, &snapshot.AvatarObjectID, &snapshot.AvatarRevision)
	if err != nil {
		t.Fatalf("read legacy business snapshot: %v", err)
	}
	avatarBody, err := os.ReadFile(avatarPath)
	if err != nil {
		t.Fatalf("read synthetic avatar object: %v", err)
	}
	digest := sha256.Sum256(avatarBody)
	snapshot.AvatarFileDigest = hex.EncodeToString(digest[:])
	return snapshot
}

func businessCountsEqual(left, right legacyBusinessSnapshot) bool {
	return left.Customers == right.Customers && left.Orders == right.Orders &&
		left.Schedules == right.Schedules && left.Reminders == right.Reminders &&
		left.Settings == right.Settings && left.Checkpoints == right.Checkpoints
}

func resetToMigrationStep(t *testing.T, url string, steps int) {
	t.Helper()
	db := openSQLDatabase(t, url)
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		_ = db.Close()
		t.Fatalf("reset migration fixture: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close reset migration fixture: %v", err)
	}
	if err := store.MigrateStepsForTest(url, steps); err != nil {
		t.Fatalf("migrate fixture to step %d: %v", steps, err)
	}
}

func migrationVersion(t *testing.T, url string) uint {
	t.Helper()
	db := openSQLDatabase(t, url)
	defer func() { _ = db.Close() }()
	var version uint
	var dirty bool
	if err := db.QueryRow(`SELECT version, dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if dirty {
		t.Fatalf("schema version %d is dirty", version)
	}
	return version
}

func openSQLDatabase(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open SQL database: %v", err)
	}
	return db
}

func logHarnessReport(t *testing.T, report any) {
	t.Helper()
	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal harness report: %v", err)
	}
	t.Logf("AUTH_LEGACY_HARNESS %s", payload)
}
