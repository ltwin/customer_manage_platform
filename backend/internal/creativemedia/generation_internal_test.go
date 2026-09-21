package creativemedia

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store/storetest"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

// probeInternalFixture 只搭 claim/retry 直接调用所需的最小骨架：账户、
// ready blob 行与 pending 探测行。领取只校验行身份，不触物理对象，
// 因此无需真实上传，也不需要队列运行时。
func probeInternalFixture(t *testing.T) (*Service, store.AccountScope, string, *sql.DB) {
	t.Helper()
	url := storetest.NewURL(t)
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	local, err := versionedfs.NewLocal(filepath.Join(t.TempDir(), "media"), func(key, session string, part int, expires time.Time) versionedfs.PartAuthorization {
		return versionedfs.PartAuthorization{}
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	svc, err := NewService(cfg, local, NewVerifier(cfg), make([]byte, 32), "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const account = "media-internal"
	if _, err := db.Exec(`INSERT INTO accounts(id,password_hash,status) VALUES ($1,'test','active')`, account); err != nil {
		t.Fatal(err)
	}
	digest := "sha256-" + strings.Repeat("a", 64)
	if _, err := db.Exec(`INSERT INTO creative_blobs(id,account_id,storage_driver,bucket,object_key,sha256,byte_size,mime,state,object_version,verified_at)
 VALUES('ccbl_probe_internal',$1,'local','','k',$2,10,'image/png','ready','v1',now())`, account, digest); err != nil {
		t.Fatal(err)
	}
	probeID := "ccmp_" + strings.Repeat("1", 32)
	if _, err := db.Exec(`INSERT INTO creative_media_probes(id,account_id,blob_id,object_version,extractor_version,state)
 VALUES($2,$1,'ccbl_probe_internal','v1',$3,'pending')`, account, probeID, FactsExtractorVersion); err != nil {
		t.Fatal(err)
	}
	return svc, st.ScopeFor(auth.AccountContext{AccountID: account}), probeID, db
}

func probeInternalRow(t *testing.T, db *sql.DB, probeID string) (state string, attempts int, pins int) {
	t.Helper()
	var pin *string
	if err := db.QueryRow(`SELECT state,attempts,read_pin_id FROM creative_media_probes WHERE id=$1`, probeID).Scan(&state, &attempts, &pin); err != nil {
		t.Fatal(err)
	}
	if pin != nil {
		if err := db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins WHERE id=$1`, *pin).Scan(&pins); err != nil {
			t.Fatal(err)
		}
	}
	return state, attempts, pins
}

func TestFactsProbeOldClaimCannotTouchNewRound(t *testing.T) {
	svc, scope, probeID, db := probeInternalFixture(t)
	old, ok, err := svc.claimFactsProbe(t.Context(), scope, probeID, 0)
	if err != nil || !ok {
		t.Fatalf("initial claim: %v %v", ok, err)
	}
	if err := svc.finishFactsProbe(t.Context(), scope, old, "failed", "test_failure"); err != nil {
		t.Fatal(err)
	}
	// 独立测试回写守卫；显式调度的实际重置路径由外部交错测试覆盖。
	if _, err := db.Exec(`UPDATE creative_media_probes SET state='pending',attempts=0,retry_round=retry_round+1,execution_epoch=execution_epoch+1,completed_at=NULL WHERE id=$1`, probeID); err != nil {
		t.Fatal(err)
	}
	current, ok, err := svc.claimFactsProbe(t.Context(), scope, probeID, 1)
	if err != nil || !ok {
		t.Fatalf("new round claim: %v %v", ok, err)
	}
	for _, finish := range []func() error{
		func() error { return svc.retryFactsProbe(t.Context(), scope, old, "late_retry", errors.New("late")) },
		func() error { return svc.finishFactsProbe(t.Context(), scope, old, "unsupported", "late_finish") },
		func() error { return svc.completeFactsProbe(t.Context(), scope, old, ProbedFacts{}) },
	} {
		if err := finish(); !errors.Is(err, ErrEpoch) {
			t.Fatalf("old round write accepted: %v", err)
		}
		if state, attempts, pins := probeInternalRow(t, db, probeID); state != "running" || attempts != 1 || pins != 1 {
			t.Fatalf("old round touched new pin: %s/%d/%d", state, attempts, pins)
		}
	}
	if err := svc.finishFactsProbe(t.Context(), scope, current, "unsupported", "test_complete"); err != nil {
		t.Fatal(err)
	}
}

func TestFactsProbeCrashRecoveryStopsAtAttemptBudget(t *testing.T) {
	svc, scope, probeID, db := probeInternalFixture(t)
	var last factsProbeClaim
	for attempt := 1; attempt <= factsProbeMaxTries; attempt++ {
		claim, ok, err := svc.claimFactsProbe(t.Context(), scope, probeID, 0)
		if err != nil || !ok || claim.Attempts != attempt {
			t.Fatalf("claim #%d: %+v ok=%v err=%v", attempt, claim, ok, err)
		}
		last = claim
		if attempt == factsProbeMaxTries {
			var deferred *jobs.DeferredError
			if _, ok, err := svc.claimFactsProbe(t.Context(), scope, probeID, 0); ok || !errors.As(err, &deferred) {
				t.Fatalf("last live attempt interrupted: ok=%v err=%v", ok, err)
			}
		}
		// 不调用失败收尾，模拟进程在领取后崩溃，随后租约过期。
		if _, err := db.Exec(`UPDATE creative_media_probes SET lease_until=now()-interval '1 second' WHERE id=$1`, probeID); err != nil {
			t.Fatal(err)
		}
	}
	claim, ok, err := svc.claimFactsProbe(t.Context(), scope, probeID, 0)
	if err != nil || ok {
		t.Fatalf("exhausted probe reclaimed: %+v ok=%v err=%v", claim, ok, err)
	}
	state, attempts, pins := probeInternalRow(t, db, probeID)
	if state != "failed" || attempts != factsProbeMaxTries || pins != 0 {
		t.Fatalf("crash budget: state=%s attempts=%d pins=%d", state, attempts, pins)
	}
	var code string
	var cleared bool
	if err := db.QueryRow(`SELECT error_code,read_pin_id IS NULL AND lease_until IS NULL AND completed_at IS NOT NULL FROM creative_media_probes WHERE id=$1`, probeID).Scan(&code, &cleared); err != nil || code != "attempts_exhausted" || !cleared {
		t.Fatalf("terminal cleanup: code=%s cleared=%v err=%v", code, cleared, err)
	}
	// 旧 worker 即使迟到恢复，也不能覆盖恢复方写下的失败终态。
	if err := svc.finishFactsProbe(t.Context(), scope, last, "unsupported", "late_result"); !errors.Is(err, ErrEpoch) {
		t.Fatalf("stale finish accepted: %v", err)
	}
}

// TestFactsProbeRetryAccountingSurvivesDeadContext 锁住两条预算语义：
// 领取事务即持久化尝试计数；失败收尾用独立短超时 ctx，执行 ctx 已被
// 整体超时取消后，记账、终态翻转与 pin 释放仍要落库。三次领取即按
// 上限进入终态失败——worker 超时死亡不再重置预算。
func TestFactsProbeRetryAccountingSurvivesDeadContext(t *testing.T) {
	svc, scope, probeID, db := probeInternalFixture(t)
	deadCtx, cancel := context.WithCancel(t.Context())
	cancel()
	cause := errors.New("read timed out")
	for attempt := 1; attempt <= factsProbeMaxTries; attempt++ {
		claim, ok, err := svc.claimFactsProbe(t.Context(), scope, probeID, 0)
		if err != nil || !ok {
			t.Fatalf("claim #%d: ok=%v err=%v", attempt, ok, err)
		}
		if claim.Attempts != attempt {
			t.Fatalf("claim #%d persisted attempts=%d", attempt, claim.Attempts)
		}
		err = svc.retryFactsProbe(deadCtx, scope, claim, "open_failed", cause)
		if attempt < factsProbeMaxTries {
			// 非终态：把原因交还队列退避；绝不能是 ctx 取消错误。
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("retry #%d must surface the cause, got %v", attempt, err)
			}
			state, attempts, pins := probeInternalRow(t, db, probeID)
			if state != "pending" || attempts != attempt || pins != 0 {
				t.Fatalf("retry #%d: state=%s attempts=%d pins=%d", attempt, state, attempts, pins)
			}
		} else if err != nil {
			t.Fatalf("terminal failure must be handled, got %v", err)
		}
	}
	state, attempts, pins := probeInternalRow(t, db, probeID)
	if state != "failed" || attempts != factsProbeMaxTries || pins != 0 {
		t.Fatalf("budget exhausted: state=%s attempts=%d pins=%d", state, attempts, pins)
	}
}
