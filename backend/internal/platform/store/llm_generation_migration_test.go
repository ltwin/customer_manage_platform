package store_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// TestGenerationAdmissionMigrationShape 验证0052：旧行默认chat、生成行免伪token
// 上限、扩展表身份约束、generation_total分量放行；down在存在生成行时拒绝。
func TestGenerationAdmissionMigrationShape(t *testing.T) {
	url := startPostgres(t)
	if err := store.MigrateUp(url); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('ga-1','test','active')`); err != nil {
		t.Fatal(err)
	}

	// 旧行默认chat；生成行须显式声明kind且免伪token上限，chat行output_limit=0
	// 与未知kind仍被拒绝。
	var kind string
	if err := db.QueryRow(`INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,caller_group_id,
request_hash,model_key,catalog_version,price_version,model_snapshot,price_snapshot,output_limit,deadline_at,retained_until,request_kind)
VALUES ('llmr_11111111-1111-1111-1111-111111111111','ga-1','creative_agent','11111111-1111-1111-1111-111111111111','g1',
'sha256-` + hex64(t, "gen") + `','model_a','eng-1','policy-1','{}','{}',0,now(),now(),'generation') RETURNING request_kind`).Scan(&kind); err != nil {
		t.Fatalf("generation row must admit output_limit=0: %v", err)
	}
	if kind != "generation" {
		t.Fatalf("explicit kind must persist, got %q", kind)
	}
	if _, err := db.Exec(`INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,caller_group_id,
request_hash,model_key,catalog_version,price_version,model_snapshot,price_snapshot,output_limit,deadline_at,retained_until)
VALUES ('llmr_22222222-2222-2222-2222-222222222222','ga-1','creative_agent','22222222-2222-2222-2222-222222222222','g1',
'sha256-` + hex64(t, "chat") + `','chat','cat-1','p-1','{}','{}',0,now(),now())`); err == nil {
		t.Fatal("chat row with output_limit=0 must be rejected")
	}
	if _, err := db.Exec(`INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,caller_group_id,
request_hash,model_key,catalog_version,price_version,model_snapshot,price_snapshot,output_limit,deadline_at,retained_until,request_kind)
VALUES ('llmr_55555555-5555-5555-5555-555555555555','ga-1','creative_agent','55555555-5555-5555-5555-555555555555','g1',
'sha256-` + hex64(t, "bad") + `','chat','cat-1','p-1','{}','{}',10,now(),now(),'video')`); err == nil {
		t.Fatal("unknown request kind must be rejected")
	}

	// 扩展表：指纹格式、快照上限、唯一挂接。
	if _, err := db.Exec(`INSERT INTO llm_generation_requests(account_id,request_id,policy_snapshot,fingerprint,
engine_version,policy_version,model_key,model_revision,deployment_id,adapter_version,mode_key,kind,currency,
reserved_upper_bound_micros,quote_lines,admission_control)
VALUES ('ga-1','llmr_11111111-1111-1111-1111-111111111111',('\x'||repeat('61',4))::bytea,'` + hex64(t, "fp") + `','eng-1','policy-1','model_a',
'rev-1','dep-1','adp-1','reference_video','video','USD',1250000,'[]'::jsonb,'{}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"NULL", "'[]'::jsonb", "'null'::jsonb"} {
		if _, err := db.Exec(`UPDATE llm_generation_requests SET admission_control=` + invalid + ` WHERE account_id='ga-1'`); err == nil {
			t.Fatalf("invalid admission control must be rejected: %s", invalid)
		}
	}
	if _, err := db.Exec(`UPDATE llm_generation_requests SET fingerprint='nothex' WHERE request_id='llmr_11111111-1111-1111-1111-111111111111'`); err == nil {
		t.Fatal("fingerprint must be 64 lowercase hex")
	}
	if _, err := db.Exec(`UPDATE llm_generation_requests SET policy_snapshot=('\x'||repeat('61',7077888))::bytea WHERE request_id='llmr_11111111-1111-1111-1111-111111111111'`); err != nil {
		t.Fatalf("a snapshot at the documented cap must be accepted: %v", err)
	}
	if _, err := db.Exec(`UPDATE llm_generation_requests SET policy_snapshot=('\x'||repeat('61',7077889))::bytea WHERE request_id='llmr_11111111-1111-1111-1111-111111111111'`); err == nil {
		t.Fatal("oversize snapshot must be rejected")
	}
	if _, err := db.Exec(`INSERT INTO llm_generation_requests(account_id,request_id,policy_snapshot,fingerprint,
engine_version,policy_version,model_key,model_revision,deployment_id,adapter_version,mode_key,kind,currency,
reserved_upper_bound_micros,quote_lines,admission_control)
VALUES ('ga-1','llmr_11111111-1111-1111-1111-111111111111',('\x'||repeat('61',4))::bytea,'` + hex64(t, "fp2") + `','eng-1','policy-1','model_a',
'rev-1','dep-1','adp-1','reference_video','video','USD',1250000,'[]'::jsonb,'{}'::jsonb)`); err == nil {
		t.Fatal("a request admits at most one generation extension row")
	}

	// generation_total分量进入证据与成本位置。
	if _, err := db.Exec(`INSERT INTO llm_attempts(id,account_id,request_id,attempt_number,reservation_id,
reservation_generation,dispatch_state,dispatch_epoch,permit_digest,limit_key,dispatched_at,finished_at)
VALUES ('llma_33333333-3333-3333-3333-333333333333','ga-1','llmr_11111111-1111-1111-1111-111111111111',1,'llmv-1',1,'succeeded',0,'sha256-` + hex64(t, "permit") + `','dep-1',now(),now())`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO llm_usage_measurements(id,account_id,attempt_id,measurement_key,cost_component,
currency,usage_dimensions,price_snapshot,cost_micros,certainty,evidence_kind,evidence_rank,evidence_digest)
VALUES ('llmu_44444444-4444-4444-4444-444444444444','ga-1','llma_33333333-3333-3333-3333-333333333333','obs:1','generation_total','USD','{}','{}',875000,'known','provider_reported',10,
'sha256-` + hex64(t, "ev") + `')`); err != nil {
		t.Fatalf("generation_total measurement must be accepted: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO llm_cost_positions(account_id,attempt_id,cost_component,currency,
current_measurement_id,booked_cost_micros,certainty) VALUES ('ga-1','llma_33333333-3333-3333-3333-333333333333','generation_total','USD','llmu_44444444-4444-4444-4444-444444444444',875000,'known')`); err != nil {
		t.Fatalf("generation_total position must be accepted: %v", err)
	}
}

// TestGenerationAdmissionDownGuardAndCleanRoundTrip 在独立数据库验证：存在生成行时
// down拒绝（迁移状态置dirty是强守卫的预期代价）；空库down/up无损可逆。
func TestGenerationAdmissionDownGuardAndCleanRoundTrip(t *testing.T) {
	guardURL := startPostgres(t)
	if err := store.MigrateUp(guardURL); err != nil {
		t.Fatal(err)
	}
	guardDB, err := sql.Open("pgx", guardURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = guardDB.Close() }()
	if _, err := guardDB.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ('ga-2','test','active')`); err != nil {
		t.Fatal(err)
	}
	if _, err := guardDB.Exec(`INSERT INTO llm_requests(id,account_id,caller_service,caller_operation_id,caller_group_id,
request_hash,model_key,catalog_version,price_version,model_snapshot,price_snapshot,output_limit,deadline_at,retained_until,request_kind)
VALUES ('llmr_99999999-9999-9999-9999-999999999999','ga-2','creative_agent','99999999-9999-9999-9999-999999999999','g1',
'sha256-` + hex64(t, "guard") + `','model_a','eng-1','policy-1','{}','{}',0,now(),now(),'generation')`); err != nil {
		t.Fatal(err)
	}
	rollbackGenerationLifecycle(t, guardURL)
	if err := store.MigrateDownOneForTest(guardURL); err == nil {
		t.Fatal("down migration must refuse while generation rows exist")
	}

	cleanURL := startPostgres(t)
	if err := store.MigrateUp(cleanURL); err != nil {
		t.Fatal(err)
	}
	rollbackGenerationLifecycle(t, cleanURL)
	if err := store.MigrateDownOneForTest(cleanURL); err != nil {
		t.Fatalf("rollback generation-admission: %v", err)
	}
	if err := store.MigrateUp(cleanURL); err != nil {
		t.Fatalf("re-apply generation-admission: %v", err)
	}
	cleanDB, err := sql.Open("pgx", cleanURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cleanDB.Close() }()
	var count int
	if err := cleanDB.QueryRow(`SELECT COUNT(*) FROM llm_generation_requests`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("re-applied extension table must be empty: %d %v", count, err)
	}
}

func hex64(t *testing.T, seed string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}
