package llmgateway_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// genPolicyFixture 构造USD计价的视频生成策略：output_ms按秒ceil计价125000微单位，
// 硬上限10000ms。mutate允许按用例改写费率/版本/上限/币种。
func genPolicyFixture(t *testing.T, mutate func(d *gw.GenerationPolicyDefinition, target *gw.GenerationPolicyTarget)) (*gw.CompiledGenerationPolicy, gw.GenerationPolicyDefinition, gw.GenerationPolicyTarget) {
	t.Helper()
	d, target := policyFixture(t)
	d.Currency = "USD"
	if mutate != nil {
		mutate(&d, &target)
	}
	return compilePolicy(t, d, target), d, target
}

// genPrepared 产出允许受理的冻结生成（默认duration事实1000ms、参数默认5000ms）。
func genPrepared(t *testing.T, p *gw.CompiledGenerationPolicy, duration *int64) gw.PreparedPolicyGeneration {
	t.Helper()
	r := policyRequest()
	facts, err := gw.ReadGenerationFacts(context.Background(), r, &policyMediaReader{duration: duration})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(r, facts)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("prepare: verdict=%s issues=%+v err=%v", result.Verdict, result.Issues, err)
	}
	return result.Prepared
}

type genAdmitOptions struct {
	operationID string
	group       string
	groupMicros int64
	deadline    time.Time
}

// admitGeneration 走真实受理事务；operationID缺省时生成新UUID。
func (f *fixture) admitGeneration(t *testing.T, scope store.AccountScope, prepared gw.PreparedPolicyGeneration, o genAdmitOptions) (gw.RequestView, error) {
	t.Helper()
	if o.operationID == "" {
		o.operationID = uuid.NewString()
	}
	if o.group == "" {
		o.group = "gen-1"
	}
	if o.groupMicros == 0 {
		o.groupMicros = 5_000_000
	}
	if o.deadline.IsZero() {
		o.deadline = f.now.Add(2 * time.Hour)
	}
	var view gw.RequestView
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		view, err = f.gateway.PrepareGenerationInTx(t.Context(), tx, gw.GenerationAdmission{
			CallerService:     "creative_agent",
			CallerOperationID: o.operationID,
			CallerGroupID:     o.group,
			GroupLimitMicros:  o.groupMicros,
			GroupTokenLimit:   1000,
			GroupDeadline:     o.deadline,
			ConsumerKey:       "canvas:" + o.operationID,
			Deadline:          o.deadline,
			Prepared:          prepared,
		})
		return err
	})
	return view, err
}

// seedGenerationAttempt 直接落一行已完结attempt：G2不派发，attempt由G3的Submit创建，
// 结算路径只要求归属存在。permit_digest是按attempt派生的占位规范形态，不代表真实
// 派发许可，仅满足唯一键。
func (f *fixture) seedGenerationAttempt(t *testing.T, accountID, requestID string) string {
	t.Helper()
	var reservationID string
	var generation int64
	if err := f.db.QueryRow(`SELECT id,hold_generation FROM llm_usage_reservations WHERE account_id=$1 AND request_id=$2`, accountID, requestID).
		Scan(&reservationID, &generation); err != nil {
		t.Fatal(err)
	}
	attemptID := "llma_" + uuid.NewString()
	sum := sha256.Sum256([]byte(attemptID))
	_, err := f.db.Exec(`INSERT INTO llm_attempts(id,account_id,request_id,attempt_number,reservation_id,
reservation_generation,dispatch_state,dispatch_epoch,permit_digest,limit_key,dispatched_at,finished_at)
VALUES ($1,$2,$3,1,$4,$5,'succeeded',$5,$6,'gen-dep',$7,$7)`,
		attemptID, accountID, requestID, reservationID, generation,
		"sha256-"+hex.EncodeToString(sum[:]), f.now)
	if err != nil {
		t.Fatal(err)
	}
	return attemptID
}

func (f *fixture) recordGenerationUsage(t *testing.T, scope store.AccountScope, requestID, attemptID, eventKey string, usage map[string]int64, providerRevision string) (gw.SettlementReceipt, error) {
	t.Helper()
	var receipt gw.SettlementReceipt
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		receipt, err = f.gateway.RecordGenerationUsageInTx(t.Context(), tx, requestID, attemptID, eventKey, usage, providerRevision)
		return err
	})
	return receipt, err
}

func queryCount(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// sha256Digest 生成满足evidence_digest列约束的证据摘要（sha256-+64hex）。
func sha256Digest(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
	}
	return "sha256-" + hex.EncodeToString(h.Sum(nil))
}

func (f *fixture) reservedMicros(t *testing.T) (reserved, spent int64) {
	t.Helper()
	if err := f.db.QueryRow(`SELECT reserved_micros,spent_micros FROM llm_budgets WHERE account_id='gw-a'`).Scan(&reserved, &spent); err != nil {
		t.Fatal(err)
	}
	return reserved, spent
}

// TestGenerationAdmissionPersistsRequestSnapshotReservationAndConsumer 验证受理
// 单事务产出全部持久事实，且重放同operation不新增预算或身份。
func TestGenerationAdmissionPersistsRequestSnapshotReservationAndConsumer(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	operation := uuid.NewString()

	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{operationID: operation})
	if err != nil {
		t.Fatal(err)
	}
	if view.Kind != "generation" || view.State != gw.StatePrepared || view.Settlement != gw.SettlementReserved {
		t.Fatalf("view: %+v", view)
	}
	if view.RequestHash != "sha256-"+prepared.Fingerprint() {
		t.Fatalf("request hash must carry the frozen fingerprint: %s", view.RequestHash)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_requests WHERE account_id='gw-a' AND request_kind='generation'`); n != 1 {
		t.Fatalf("generation requests: %d", n)
	}
	// 快照、上界与身份投影齐全；output_limit固定0，不携带伪token上限。
	var outputLimit int
	var upper int64
	var fingerprint string
	var snapshot []byte
	if err := f.db.QueryRow(`SELECT r.output_limit,g.fingerprint,g.reserved_upper_bound_micros,g.policy_snapshot
FROM llm_requests r JOIN llm_generation_requests g ON g.request_id=r.id WHERE r.id=$1`, view.ID).
		Scan(&outputLimit, &fingerprint, &upper, &snapshot); err != nil {
		t.Fatal(err)
	}
	if outputLimit != 0 || fingerprint != prepared.Fingerprint() || upper != 1250000 || len(snapshot) == 0 {
		t.Fatalf("extension row: output_limit=%d upper=%d snapshot=%d", outputLimit, upper, len(snapshot))
	}
	if reserved, _ := f.reservedMicros(t); reserved != 1250000 {
		t.Fatalf("budget reserved: %d", reserved)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_result_consumers WHERE request_id=$1 AND state='pending'`, view.ID); n != 1 {
		t.Fatalf("consumers: %d", n)
	}

	// 同operation同输入重放：返回原请求，不新增预留。
	replayed, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{operationID: operation})
	if err != nil || replayed.ID != view.ID {
		t.Fatalf("replay: %+v err=%v", replayed, err)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("reservations after replay: %d", n)
	}
	if reserved, _ := f.reservedMicros(t); reserved != 1250000 {
		t.Fatalf("budget reserved after replay: %d", reserved)
	}
}

// TestGenerationAdmissionRollsBackAtomically 对应GEN-04：受理事务任一后续失败，
// 请求/扩展/预留/保留根/组行/预算桶全部回滚。
func TestGenerationAdmissionRollsBackAtomically(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))

	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := f.gateway.PrepareGenerationInTx(t.Context(), tx, gw.GenerationAdmission{
			CallerService: "creative_agent", CallerOperationID: uuid.NewString(), CallerGroupID: "gen-1",
			GroupLimitMicros: 5_000_000, GroupTokenLimit: 1000, GroupDeadline: f.now.Add(time.Hour),
			ConsumerKey: "canvas:1", Deadline: f.now.Add(time.Hour), Prepared: prepared,
		})
		if err != nil {
			return err
		}
		return errors.New("business write failed after admission")
	})
	if err == nil {
		t.Fatal("expected the wrapping transaction to fail")
	}
	for _, table := range []string{"llm_requests", "llm_generation_requests", "llm_usage_reservations", "llm_result_consumers", "llm_call_groups", "llm_budgets"} {
		if n := queryCount(t, f.db, `SELECT COUNT(*) FROM `+table+` WHERE account_id='gw-a'`); n != 0 {
			t.Fatalf("%s rows survived rollback: %d", table, n)
		}
	}
}

// TestGenerationAdmissionSameKeyConflictsOnDifferentInput 对应GEN-05异输入分支：
// 同operation不同指纹明确冲突。
func TestGenerationAdmissionSameKeyConflictsOnDifferentInput(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	operation := uuid.NewString()
	if _, err := f.admitGeneration(t, f.a, genPrepared(t, p, newInt64(1000)), genAdmitOptions{operationID: operation}); err != nil {
		t.Fatal(err)
	}
	// 参数默认duration改为8000：报价与指纹都不同于首次受理。
	p2, _, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
		d.Modes[0].Defaults["duration"] = json.RawMessage(`8000`)
	})
	_, err := f.admitGeneration(t, f.a, genPrepared(t, p2, newInt64(1000)), genAdmitOptions{operationID: operation})
	if !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("different input must conflict, got %v", err)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_requests WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("requests: %d", n)
	}
}

// TestGenerationAdmissionConcurrentSameKeyYieldsSingleHold 对应GEN-05并发分支：
// 同operation并发受理恰好一份hold/请求；输方在组锁串行点重读绑定，拿回胜者身份
// 而不是报错（P2-4修复后的语义）。
func TestGenerationAdmissionConcurrentSameKeyYieldsSingleHold(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	operation := uuid.NewString()

	views := make([]gw.RequestView, 2)
	errs := make([]error, 2)
	var ready, done sync.WaitGroup
	ready.Add(2)
	done.Add(2)
	gate := make(chan struct{})
	for i := range 2 {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-gate
			views[i], errs[i] = f.admitGeneration(t, f.a, prepared, genAdmitOptions{operationID: operation})
		}(i)
	}
	ready.Wait()
	close(gate)
	done.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent call %d must not fail: %v", i, err)
		}
	}
	if views[0].ID == "" || views[0].ID != views[1].ID {
		t.Fatalf("both calls must return the winner identity: %s vs %s", views[0].ID, views[1].ID)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_requests WHERE account_id='gw-a' AND request_kind='generation'`); n != 1 {
		t.Fatalf("requests: %d", n)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_usage_reservations WHERE account_id='gw-a'`); n != 1 {
		t.Fatalf("reservations: %d", n)
	}
	if reserved, _ := f.reservedMicros(t); reserved != 1250000 {
		t.Fatalf("budget reserved once: %d", reserved)
	}
	// 再次调用走重放路径，身份不变。
	retry, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{operationID: operation})
	if err != nil || retry.ID != views[0].ID {
		t.Fatalf("retry replay: %+v err=%v", retry, err)
	}
}

// TestGenerationAdmissionRequiresBoundAndMatchingCurrency 对应GEN-10准入侧：
// 无可靠上界与币种不符都必须拒绝，且不落任何行。
func TestGenerationAdmissionRequiresBoundAndMatchingCurrency(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	unbounded, _, _ := genPolicyFixture(t, func(_ *gw.GenerationPolicyDefinition, target *gw.GenerationPolicyTarget) {
		def := target.Usage["output_ms"]
		def.UpperBound = nil
		target.Usage["output_ms"] = def
	})
	if _, err := f.admitGeneration(t, f.a, genPrepared(t, unbounded, newInt64(1000)), genAdmitOptions{}); !errors.Is(err, gw.ErrBudget) {
		t.Fatalf("unbounded quote must refuse admission, got %v", err)
	}
	foreign, _, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
		d.Currency = "EUR"
	})
	if _, err := f.admitGeneration(t, f.a, genPrepared(t, foreign, newInt64(1000)), genAdmitOptions{}); !errors.Is(err, gw.ErrValidation) {
		t.Fatalf("foreign currency must refuse admission, got %v", err)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_requests WHERE account_id='gw-a'`); n != 0 {
		t.Fatalf("refused admissions must persist nothing, got %d", n)
	}
}

// TestGenerationSettlementPricesFrozenProgramZeroAndUnknown 对应GEN-09：
// 已知金额按冻结程序计价并释放hold；已知零价结清；未知用量保留hold不伪装settled。
func TestGenerationSettlementPricesFrozenProgramZeroAndUnknown(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)

	receipt, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, "obs-1", map[string]int64{"output_ms": 6001}, "1")
	if err != nil || !receipt.Applied {
		t.Fatalf("settlement: %+v err=%v", receipt, err)
	}
	if receipt.NewBooked != 875000 || receipt.OldHold != 1250000 || receipt.NewHold != 0 {
		t.Fatalf("receipt: %+v", receipt)
	}
	report, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil {
		t.Fatal(err)
	}
	if report.BookedMicros != 875000 || report.HoldMicros != 0 || report.Settlement != gw.SettlementSettled {
		t.Fatalf("usage report: %+v", report)
	}
	reserved, spent := f.reservedMicros(t)
	if reserved != 0 || spent != 875000 {
		t.Fatalf("budget after settlement: reserved=%d spent=%d", reserved, spent)
	}

	// 已知零价：金额0是确定事实，照样结清。
	zeroView, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	zeroAttempt := f.seedGenerationAttempt(t, "gw-a", zeroView.ID)
	if _, err := f.recordGenerationUsage(t, f.a, zeroView.ID, zeroAttempt, "obs-0", map[string]int64{"output_ms": 0}, "1"); err != nil {
		t.Fatal(err)
	}
	zeroReport, err := f.gateway.GetUsage(t.Context(), f.a, zeroView.ID)
	if err != nil || zeroReport.BookedMicros != 0 || zeroReport.Settlement != gw.SettlementSettled {
		t.Fatalf("known zero: %+v err=%v", zeroReport, err)
	}

	// 未知用量：缺维度报错，账面不动、hold保留、不伪装settled。
	unknownView, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	unknownAttempt := f.seedGenerationAttempt(t, "gw-a", unknownView.ID)
	if _, err := f.recordGenerationUsage(t, f.a, unknownView.ID, unknownAttempt, "obs-u", map[string]int64{"resolution_px": 1}, ""); !errors.Is(err, gw.ErrGenerationFactsUnavailable) {
		t.Fatalf("missing usage vector: %v", err)
	}
	unknownReport, err := f.gateway.GetUsage(t.Context(), f.a, unknownView.ID)
	if err != nil || unknownReport.Settlement != gw.SettlementReserved || unknownReport.HoldMicros != 1250000 {
		t.Fatalf("unknown usage must keep the hold: %+v err=%v", unknownReport, err)
	}
}

// TestGenerationSettlementRestoresFrozenProgramAfterRebuild 对应GEN-06：受理与结算
// 发生在不同Service实例（进程重建）之间，程序来自持久快照而非活动目录。
func TestGenerationSettlementRestoresFrozenProgramAfterRebuild(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)

	// 进程重建：同一数据库上的全新Service，未编译任何生成策略。
	rebuilt, err := gw.New(gw.Config{
		Catalog:       testCatalog(t, usablePrice()),
		Providers:     map[gw.ProviderKey]gw.Provider{gw.ProviderOpenAICompatible: f.provider},
		Credential:    func(string) (string, bool) { return "secret-value", true },
		Clock:         func() time.Time { return f.now },
		DefaultBudget: defaultBudget(),
	})
	if err != nil {
		t.Fatal(err)
	}
	var receipt gw.SettlementReceipt
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		receipt, err = rebuilt.RecordGenerationUsageInTx(t.Context(), tx, view.ID, attemptID, "obs-1", map[string]int64{"output_ms": 6001}, "1")
		return err
	})
	if err != nil || !receipt.Applied || receipt.NewBooked != 875000 {
		t.Fatalf("rebuilt settlement: %+v err=%v", receipt, err)
	}
}

// TestGenerationSettlementEvidenceIdempotencyAndCorrections 对应GEN-08：重复证据
// 幂等；同事件键异内容隔离为争议（§19.6，不得凭revision直接改账）；更高修订的
// 新事件证据替换总额且只记差额。
func TestGenerationSettlementEvidenceIdempotencyAndCorrections(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)

	// 首次入账875000（6001ms→7单位）。
	if _, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, "obs-1", map[string]int64{"output_ms": 6001}, "1"); err != nil {
		t.Fatal(err)
	}
	// 完全相同的证据重放：不二次记账。
	replay, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, "obs-1", map[string]int64{"output_ms": 6001}, "1")
	if err != nil || replay.Applied {
		t.Fatalf("duplicate evidence must be idempotent: %+v err=%v", replay, err)
	}
	if _, spent := f.reservedMicros(t); spent != 875000 {
		t.Fatalf("spent after duplicate: %d", spent)
	}
	// 同事件键异内容：即使携带更高修订也隔离为争议，不得替换账面（P1-2）。
	if _, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, "obs-1", map[string]int64{"output_ms": 8000}, "2"); err != nil {
		t.Fatal(err)
	}
	report, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil || report.BookedMicros != 875000 {
		t.Fatalf("same-key different-content evidence must not move the ledger: %+v err=%v", report, err)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_measurement_dispositions d
JOIN llm_usage_measurements m ON m.id=d.measurement_id WHERE m.attempt_id=$1 AND d.state='disputed'`, attemptID); n != 1 {
		t.Fatalf("conflicting evidence must be disputed: %d", n)
	}
	// 未决争议阻断hold释放：结清状态不得伪装完成。
	if report.Settlement == gw.SettlementSettled || report.HoldMicros != 1250000 {
		t.Fatalf("unresolved dispute must keep the hold: %+v", report)
	}
	// 更高修订的合法证据使用新事件身份obs-2：替换总额、只记差额，但不解除费用
	// 争议——修订号只证明先后顺序，不构成受信核实（P1-r2）。
	correction, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, "obs-2", map[string]int64{"output_ms": 8000}, "3")
	if err != nil || !correction.Applied {
		t.Fatalf("revision correction: %+v err=%v", correction, err)
	}
	if correction.OldBooked != 875000 || correction.NewBooked != 1000000 {
		t.Fatalf("correction receipt: %+v", correction)
	}
	if _, spent := f.reservedMicros(t); spent != 1000000 {
		t.Fatalf("spent after correction: %d", spent)
	}
	stillOpen, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil || stillOpen.HoldMicros != 1250000 || stillOpen.Settlement == gw.SettlementSettled {
		t.Fatalf("a provider revision alone must not resolve the dispute: %+v err=%v", stillOpen, err)
	}
	// 受信核实入口（账单对账）落known金额：解除争议、hold释放、结清。
	amount := int64(1000000)
	var verified gw.SettlementReceipt
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		verified, err = f.gateway.RecordMeasurementInTx(t.Context(), tx, view.ID, gw.Measurement{
			AttemptID: attemptID, Key: "bill-obs-1", Component: gw.ComponentGenerationTotal, Currency: "USD",
			Dimensions: map[string]any{"source": "provider_invoice"},
			CostMicros: &amount, Certainty: "known", Kind: gw.EvidenceProviderReported,
			ProviderRev: "9", EvidenceDigest: sha256Digest("bill-obs-1"),
		})
		return err
	})
	if err != nil || !verified.Applied {
		t.Fatalf("trusted verification: %+v err=%v", verified, err)
	}
	final, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil || final.HoldMicros != 0 || final.Settlement != gw.SettlementSettled {
		t.Fatalf("trusted evidence must resolve the dispute: %+v err=%v", final, err)
	}
}

// TestGenerationDisputeBlocksSettlementAndAdmission 对应GEN-10与P1-1/P1-3：
// 超上界争议参与未决判定——先争议后正常用量不释放hold；先结清后争议重新抬高
// hold且不回滚已入账金额；争议阻断该部署新准入，既有operation回放与核算保留。
func TestGenerationDisputeBlocksSettlementAndAdmission(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	operation := uuid.NewString()

	// 顺序A：先超上界争议，再同事件键的正常用量（更低修订）。
	viewA, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{operationID: operation})
	if err != nil {
		t.Fatal(err)
	}
	attemptA := f.seedGenerationAttempt(t, "gw-a", viewA.ID)
	if _, err := f.recordGenerationUsage(t, f.a, viewA.ID, attemptA, "obs-9", map[string]int64{"output_ms": 10001}, "2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.recordGenerationUsage(t, f.a, viewA.ID, attemptA, "obs-9", map[string]int64{"output_ms": 6001}, "1"); err != nil {
		t.Fatal(err)
	}
	reportA, err := f.gateway.GetUsage(t.Context(), f.a, viewA.ID)
	if err != nil || reportA.HoldMicros != 1250000 || reportA.Settlement == gw.SettlementSettled || reportA.BookedMicros != 0 {
		t.Fatalf("order A: dispute must block release without booking: %+v err=%v", reportA, err)
	}
	// 争议阻断该部署的新准入；既有operation幂等回放不受影响。
	if _, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{}); !errors.Is(err, gw.ErrUnavailable) {
		t.Fatalf("new admission must be blocked after over-bound dispute, got %v", err)
	}
	if replayed, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{operationID: operation}); err != nil || replayed.ID != viewA.ID {
		t.Fatalf("existing operation replay: %+v err=%v", replayed, err)
	}
	// 原请求仍可核算：新事件身份的合法证据正常入账，但更低修订解不开rev2争议。
	if _, err := f.recordGenerationUsage(t, f.a, viewA.ID, attemptA, "obs-10", map[string]int64{"output_ms": 6001}, "1"); err != nil {
		t.Fatal(err)
	}
	blockedReport, err := f.gateway.GetUsage(t.Context(), f.a, viewA.ID)
	if err != nil || blockedReport.BookedMicros != 875000 || blockedReport.HoldMicros != 1250000 || blockedReport.Settlement == gw.SettlementSettled {
		t.Fatalf("lower revision must not resolve the dispute: %+v err=%v", blockedReport, err)
	}
	// rev3合法证据替换总额（1000000）但不得解除rev2争议：修订号只证明先后，
	// 不构成受信核实（P1-r2）。hold保留、不结清。
	if _, err := f.recordGenerationUsage(t, f.a, viewA.ID, attemptA, "obs-11", map[string]int64{"output_ms": 8000}, "3"); err != nil {
		t.Fatal(err)
	}
	stillDisputed, err := f.gateway.GetUsage(t.Context(), f.a, viewA.ID)
	if err != nil || stillDisputed.BookedMicros != 1000000 || stillDisputed.HoldMicros != 1250000 || stillDisputed.Settlement == gw.SettlementSettled {
		t.Fatalf("provider revision alone must not resolve the dispute: %+v err=%v", stillDisputed, err)
	}
	// 被留置的证据不构成核实：等级低于已入账provider_reported的operator对账
	// 被留置（parked），争议保持未决、hold不动。
	low := int64(1375000)
	var parked gw.SettlementReceipt
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		parked, err = f.gateway.RecordMeasurementInTx(t.Context(), tx, viewA.ID, gw.Measurement{
			AttemptID: attemptA, Key: "attest-obs-9", Component: gw.ComponentGenerationTotal, Currency: "USD",
			Dimensions: map[string]any{"source": "operator_attestation"},
			CostMicros: &low, Certainty: "known", Kind: gw.EvidenceOperatorAttested,
			EvidenceDigest: sha256Digest("attest-obs-9"),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if parked.Applied {
		t.Fatalf("lower-ranked evidence must be parked, not applied: %+v", parked)
	}
	afterPark, err := f.gateway.GetUsage(t.Context(), f.a, viewA.ID)
	if err != nil || afterPark.HoldMicros != 1250000 || afterPark.Settlement == gw.SettlementSettled {
		t.Fatalf("parked evidence must not resolve the dispute: %+v err=%v", afterPark, err)
	}
	// 受信核实（账单对账）以真实超额金额1375000解除争议：金额替换、争议解除与
	// hold释放落在同一张回执（P1-r2）；解除记录指向核实证据（llmu）而非回执号
	// （llmc）；部署阻断独立保留，不随争议解除恢复准入。
	amount := int64(1375000)
	var verified gw.SettlementReceipt
	err = f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		verified, err = f.gateway.RecordMeasurementInTx(t.Context(), tx, viewA.ID, gw.Measurement{
			AttemptID: attemptA, Key: "bill-obs-9", Component: gw.ComponentGenerationTotal, Currency: "USD",
			Dimensions: map[string]any{"source": "provider_invoice"},
			CostMicros: &amount, Certainty: "known", Kind: gw.EvidenceProviderReported,
			ProviderRev: "9", EvidenceDigest: sha256Digest("bill-obs-9"),
		})
		return err
	})
	if err != nil || !verified.Applied {
		t.Fatalf("trusted verification: %+v err=%v", verified, err)
	}
	if verified.OldBooked != 1000000 || verified.NewBooked != 1375000 || verified.OldHold != 1250000 || verified.NewHold != 0 {
		t.Fatalf("one receipt must explain the amount and the hold release: %+v", verified)
	}
	if !strings.HasPrefix(verified.MeasurementID, "llmu_") || !strings.HasPrefix(verified.ID, "llmc_") {
		t.Fatalf("receipt must distinguish evidence and receipt identity: %+v", verified)
	}
	var resolvedBy string
	if err := f.db.QueryRow(`SELECT d.resolved_by_measurement_id FROM llm_measurement_dispositions d
JOIN llm_usage_measurements m ON m.account_id=d.account_id AND m.id=d.measurement_id
WHERE m.attempt_id=$1 AND d.state='obsolete'`, attemptA).Scan(&resolvedBy); err != nil {
		t.Fatal(err)
	}
	if resolvedBy != verified.MeasurementID {
		t.Fatalf("resolution must reference the verifying evidence: %s vs %s", resolvedBy, verified.MeasurementID)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_usage_measurements WHERE id=$1`, resolvedBy); n != 1 {
		t.Fatalf("resolution target must be a measurement row: %d", n)
	}
	var receiptOldHold, receiptNewHold int64
	if err := f.db.QueryRow(`SELECT old_remaining_hold,new_remaining_hold FROM llm_settlement_receipts WHERE measurement_id=$1`, verified.MeasurementID).
		Scan(&receiptOldHold, &receiptNewHold); err != nil || receiptOldHold != 1250000 || receiptNewHold != 0 {
		t.Fatalf("persisted receipt must match the returned receipt: old=%d new=%d err=%v", receiptOldHold, receiptNewHold, err)
	}
	resolved, err := f.gateway.GetUsage(t.Context(), f.a, viewA.ID)
	if err != nil || resolved.BookedMicros != 1375000 || resolved.HoldMicros != 0 || resolved.Settlement != gw.SettlementSettled {
		t.Fatalf("trusted verification must settle: %+v err=%v", resolved, err)
	}
	if reserved, spent := f.budgetMicros(t, "gw-a"); reserved != 0 || spent != 1375000 {
		t.Fatalf("budget after verification: reserved=%d spent=%d", reserved, spent)
	}
	if _, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{}); !errors.Is(err, gw.ErrUnavailable) {
		t.Fatalf("deployment admission must stay blocked until the control plane clears it, got %v", err)
	}

	// 顺序B：先结清，再收到超上界争议——hold重新抬高、已入账金额不回滚。
	viewB, err := f.admitGeneration(t, f.b, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptB := f.seedGenerationAttempt(t, "gw-b", viewB.ID)
	if _, err := f.recordGenerationUsage(t, f.b, viewB.ID, attemptB, "obs-1", map[string]int64{"output_ms": 6001}, "1"); err != nil {
		t.Fatal(err)
	}
	settled, err := f.gateway.GetUsage(t.Context(), f.b, viewB.ID)
	if err != nil || settled.Settlement != gw.SettlementSettled || settled.HoldMicros != 0 {
		t.Fatalf("order B setup: %+v err=%v", settled, err)
	}
	if _, err := f.recordGenerationUsage(t, f.b, viewB.ID, attemptB, "obs-2", map[string]int64{"output_ms": 10001}, "2"); err != nil {
		t.Fatal(err)
	}
	reopened, err := f.gateway.GetUsage(t.Context(), f.b, viewB.ID)
	if err != nil || reopened.BookedMicros != 875000 || reopened.HoldMicros != 1250000 || reopened.Settlement == gw.SettlementSettled {
		t.Fatalf("dispute after settlement must reopen without rollback: %+v err=%v", reopened, err)
	}
	if reserved, spent := f.budgetMicros(t, "gw-b"); reserved != 1250000 || spent != 875000 {
		t.Fatalf("order B budget: reserved=%d spent=%d", reserved, spent)
	}
}

func (f *fixture) budgetMicros(t *testing.T, account string) (reserved, spent int64) {
	t.Helper()
	if err := f.db.QueryRow(`SELECT reserved_micros,spent_micros FROM llm_budgets WHERE account_id=$1`, account).Scan(&reserved, &spent); err != nil {
		t.Fatal(err)
	}
	return reserved, spent
}

// TestGenerationDeploymentBlockIsScoped 阻断按账号+部署生效：其他部署的新准入不受影响。
func TestGenerationDeploymentBlockIsScoped(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)
	if _, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, "obs-9", map[string]int64{"output_ms": 10001}, "1"); err != nil {
		t.Fatal(err)
	}
	other, _, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, target *gw.GenerationPolicyTarget) {
		d.DeploymentID = "deployment-2"
		target.DeploymentID = "deployment-2"
	})
	if _, err := f.admitGeneration(t, f.a, genPrepared(t, other, newInt64(1000)), genAdmitOptions{}); err != nil {
		t.Fatalf("a different deployment must stay admissible: %v", err)
	}
	if _, err := f.admitGeneration(t, f.b, prepared, genAdmitOptions{}); err != nil {
		t.Fatalf("another account must stay admissible: %v", err)
	}
}

// TestGenerationAdmissionConcurrentSameKeyUnderTightQuota 对应P2-4：组额度只够
// 一次受理时，并发同operation的两个调用都必须返回同一身份，不得把成功操作
// 误报为额度不足。
func TestGenerationAdmissionConcurrentSameKeyUnderTightQuota(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	operation := uuid.NewString()

	views := make([]gw.RequestView, 2)
	errs := make([]error, 2)
	var ready, done sync.WaitGroup
	ready.Add(2)
	done.Add(2)
	gate := make(chan struct{})
	for i := range 2 {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-gate
			views[i], errs[i] = f.admitGeneration(t, f.a, prepared, genAdmitOptions{
				operationID: operation,
				// 组额度恰好等于一次hold：修复前输方会把回放当新消费撞ErrBudget。
				group:       "tight",
				groupMicros: 1250000,
			})
		}(i)
	}
	ready.Wait()
	close(gate)
	done.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent call %d must not fail: %v", i, err)
		}
	}
	if views[0].ID == "" || views[0].ID != views[1].ID {
		t.Fatalf("both calls must return the same identity: %s vs %s", views[0].ID, views[1].ID)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_usage_reservations WHERE account_id='gw-a' AND caller_group_id='tight'`); n != 1 {
		t.Fatalf("reservations: %d", n)
	}
	if reserved, _ := f.budgetMicros(t, "gw-a"); reserved != 1250000 {
		t.Fatalf("budget reserved once: %d", reserved)
	}
}

// TestChatDispatchRefusesGenerationRequest 对应P2-5：生成模型键与Chat目录重合时，
// Chat派发端口必须拒绝生成请求，不得持久化错误的attempt与派发状态。
func TestChatDispatchRefusesGenerationRequest(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, target *gw.GenerationPolicyTarget) {
		d.Profile.ModelKey = "chat"
		target.ModelKey = "chat"
	})
	// 请求信封同步使用Chat目录里的模型键，制造两端口身份重合的场景。
	request := policyRequest()
	request.ModelKey = "chat"
	facts, err := gw.ReadGenerationFacts(context.Background(), request, &policyMediaReader{duration: newInt64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Prepare(request, facts)
	if err != nil || result.Verdict != gw.GenerationAllow {
		t.Fatalf("prepare: verdict=%s issues=%+v err=%v", result.Verdict, result.Issues, err)
	}
	prepared := result.Prepared
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.begin(t, f.a, view.ID); !errors.Is(err, gw.ErrState) {
		t.Fatalf("chat dispatch must refuse a generation request, got %v", err)
	}
	var state string
	var attempts int
	if err := f.db.QueryRow(`SELECT state,attempts_used FROM llm_requests WHERE id=$1`, view.ID).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != string(gw.StatePrepared) || attempts != 0 {
		t.Fatalf("refused dispatch must leave no attempt: state=%s attempts=%d", state, attempts)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_attempts WHERE request_id=$1`, view.ID); n != 0 {
		t.Fatalf("attempts persisted: %d", n)
	}
}

// TestGenerationSettlementAcceptsMaximalIdentities 对应P2-7：合法上限的策略版本
// （64字符）与事件键（60字符）组合必须能完成受理与结算，不因身份拼接超长失败。
func TestGenerationSettlementAcceptsMaximalIdentities(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	longVersion := strings.Repeat("v", 64)
	p, _, _ := genPolicyFixture(t, func(d *gw.GenerationPolicyDefinition, _ *gw.GenerationPolicyTarget) {
		d.Version = longVersion
	})
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)
	receipt, err := f.recordGenerationUsage(t, f.a, view.ID, attemptID, strings.Repeat("e", 60), map[string]int64{"output_ms": 6001}, "1")
	if err != nil || !receipt.Applied || receipt.NewBooked != 875000 {
		t.Fatalf("maximal identities must settle: %+v err=%v", receipt, err)
	}
	report, err := f.gateway.GetUsage(t.Context(), f.a, view.ID)
	if err != nil || report.Settlement != gw.SettlementSettled || report.HoldMicros != 0 {
		t.Fatalf("maximal identities settlement: %+v err=%v", report, err)
	}
}

// TestGenerationSettlementRejectsForeignAttemptAndRequest 账号隔离：他人request与
// 他人attempt都不能记账。
func TestGenerationSettlementRejectsForeignAttemptAndRequest(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{})
	if err != nil {
		t.Fatal(err)
	}
	attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)
	if _, err := f.recordGenerationUsage(t, f.b, view.ID, attemptID, "obs-1", map[string]int64{"output_ms": 1}, "1"); err == nil {
		t.Fatal("cross-account settlement must fail")
	}
	if _, err := f.recordGenerationUsage(t, f.b, "llmr_"+uuid.NewString(), attemptID, "obs-1", map[string]int64{"output_ms": 1}, "1"); err == nil {
		t.Fatal("unknown request must fail")
	}
	if _, err := f.recordGenerationUsage(t, f.a, view.ID, "llma_"+uuid.NewString(), "obs-1", map[string]int64{"output_ms": 1}, "1"); !errors.Is(err, gw.ErrConflict) {
		t.Fatalf("foreign attempt: %v", err)
	}
}

// TestGenerationDisputeKeepsEveryOverBoundEvidence 对应P1-r2（阻断主键冲突）：
// 同一attempt连续超界与同部署多个已受理请求分别超界，都必须完整保留每份争议
// 证据；阻断写入幂等，只保留首次来源，不因主键冲突回滚整笔核算事务。
func TestGenerationDisputeKeepsEveryOverBoundEvidence(t *testing.T) {
	f := setupGateway(t, defaultBudget())
	p, _, _ := genPolicyFixture(t, nil)
	prepared := genPrepared(t, p, newInt64(1000))
	view1, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{group: "multi"})
	if err != nil {
		t.Fatal(err)
	}
	attempt1 := f.seedGenerationAttempt(t, "gw-a", view1.ID)
	view2, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{group: "multi"})
	if err != nil {
		t.Fatal(err)
	}
	attempt2 := f.seedGenerationAttempt(t, "gw-a", view2.ID)

	first, err := f.recordGenerationUsage(t, f.a, view1.ID, attempt1, "obs-o1", map[string]int64{"output_ms": 10001}, "1")
	if err != nil {
		t.Fatal(err)
	}
	// 同一attempt的第二份超界证据（不同事件）：修复前撞阻断表主键（23505），
	// 整笔事务回滚，第二份证据丢失。
	second, err := f.recordGenerationUsage(t, f.a, view1.ID, attempt1, "obs-o2", map[string]int64{"output_ms": 10002}, "2")
	if err != nil {
		t.Fatalf("a second over-bound event must not roll back: %v", err)
	}
	// 同部署另一已受理请求超界：证据同样保留，阻断不重复、来源不被覆盖。
	third, err := f.recordGenerationUsage(t, f.a, view2.ID, attempt2, "obs-o3", map[string]int64{"output_ms": 10001}, "1")
	if err != nil {
		t.Fatalf("an over-bound dispute on another request must not roll back: %v", err)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_usage_measurements WHERE attempt_id IN ($1,$2) AND certainty='unknown' AND cost_component='generation_total'`, attempt1, attempt2); n != 3 {
		t.Fatalf("every dispute measurement must be kept: %d", n)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_measurement_dispositions d
JOIN llm_usage_measurements m ON m.account_id=d.account_id AND m.id=d.measurement_id
WHERE m.attempt_id IN ($1,$2) AND d.state='disputed'`, attempt1, attempt2); n != 3 {
		t.Fatalf("every dispute must be recorded: %d", n)
	}
	if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_generation_deployment_blocks WHERE account_id='gw-a' AND deployment_id='deployment-1'`); n != 1 {
		t.Fatalf("block must be idempotent: %d", n)
	}
	var source string
	if err := f.db.QueryRow(`SELECT source_measurement_id FROM llm_generation_deployment_blocks WHERE account_id='gw-a' AND deployment_id='deployment-1'`).Scan(&source); err != nil {
		t.Fatal(err)
	}
	if source != first.MeasurementID {
		t.Fatalf("block source must keep the first dispute: %s vs %s", source, first.MeasurementID)
	}
	if second.MeasurementID == "" || second.MeasurementID == first.MeasurementID || third.MeasurementID == first.MeasurementID {
		t.Fatalf("each dispute evidence must carry its own identity: %+v %+v", second, third)
	}
}

// TestGenerationAdmissionWaitsForDeploymentGate 对应P1-r2（并发准入穿过阻断）：
// 争议事务取得部署门并写入阻断但尚未提交时，新准入必须等在门上，争议提交后
// 看到阻断并拒绝，而不是凭一次「未阻断」读穿过。同月与跨月（争议锁旧月、
// 准入锁当月，双方不共享月桶行锁）两种交错都要成立。
func TestGenerationAdmissionWaitsForDeploymentGate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		crossMonth bool
		invoice    bool
	}{
		{"usage same month", false, false}, {"usage cross month", true, false},
		{"invoice same month", false, true}, {"invoice cross month", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupGateway(t, defaultBudget())
			p, _, _ := genPolicyFixture(t, nil)
			prepared := genPrepared(t, p, newInt64(1000))
			view, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{group: "gate-src"})
			if err != nil {
				t.Fatal(err)
			}
			attemptID := f.seedGenerationAttempt(t, "gw-a", view.ID)
			if tc.crossMonth {
				prev := f.now.UTC().AddDate(0, -1, 0)
				prevMonth := time.Date(prev.Year(), prev.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
				if _, err := f.db.Exec(`INSERT INTO llm_budgets(account_id,period_start,currency,limit_micros,token_limit,reserved_micros)
VALUES('gw-a',$1,'USD',50000000,1000,1250000)`, prevMonth); err != nil {
					t.Fatal(err)
				}
				// 迁移测试种子到旧月时一并移动hold，保持两个月预算与预留一致。
				if _, err := f.db.Exec(`UPDATE llm_budgets SET reserved_micros=0 WHERE account_id='gw-a' AND period_start<>$1`, prevMonth); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Exec(`UPDATE llm_usage_reservations SET budget_period=$1 WHERE account_id='gw-a' AND request_id=$2`,
					prevMonth, view.ID); err != nil {
					t.Fatal(err)
				}
			}

			// 争议事务：取部署门→写阻断→停在提交前，制造评审给出的交错窗口。
			proceed := make(chan struct{})
			holding := make(chan struct{})
			disputeDone := make(chan error, 1)
			go func() {
				disputeDone <- f.a.WithTxScope(context.Background(), func(tx store.TxAccountScope) error {
					var err error
					if tc.invoice {
						_, err = f.gateway.RecordMeasurementInTx(context.Background(), tx, view.ID,
							generationInvoice(attemptID, "invoice-gate", 1375000))
					} else {
						_, err = f.gateway.RecordGenerationUsageInTx(context.Background(), tx, view.ID, attemptID,
							"obs-9", map[string]int64{"output_ms": 10001}, "2")
					}
					if err != nil {
						return err
					}
					close(holding)
					<-proceed
					return nil
				})
			}()
			select {
			case <-holding:
			case err := <-disputeDone:
				t.Fatalf("dispute transaction failed: %v", err)
			}

			admitDone := make(chan error, 1)
			go func() {
				// 跨组新准入：与争议事务不共享组锁。
				_, err := f.admitGeneration(t, f.a, prepared, genAdmitOptions{group: "gate-new", operationID: uuid.NewString()})
				admitDone <- err
			}()

			// 新准入必须等在部署门上：pg_locks出现未获授的advisory锁请求。
			deadline := time.Now().Add(5 * time.Second)
			waited := false
			for time.Now().Before(deadline) {
				var n int
				if err := f.db.QueryRow(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND granted=false`).Scan(&n); err == nil && n > 0 {
					waited = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waited {
				close(proceed)
				<-disputeDone
				<-admitDone
				t.Fatal("new admission must wait on the deployment gate while the dispute transaction is open")
			}
			close(proceed)
			if err := <-disputeDone; err != nil {
				t.Fatal(err)
			}
			if err := <-admitDone; !errors.Is(err, gw.ErrUnavailable) {
				t.Fatalf("admission must see the committed block, got %v", err)
			}
			if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_requests WHERE account_id='gw-a' AND request_kind='generation'`); n != 1 {
				t.Fatalf("blocked admission must persist nothing: %d", n)
			}
			if n := queryCount(t, f.db, `SELECT COUNT(*) FROM llm_generation_deployment_blocks WHERE account_id='gw-a'`); n != 1 {
				t.Fatalf("block must be committed: %d", n)
			}
		})
	}
}
