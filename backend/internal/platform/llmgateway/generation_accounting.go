package llmgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// maxGenerationEventKeyLen 限制供应商事件键长度；measurement身份本身是定长哈希，
// 不再受列宽挤压（P2-7：不靠截断，原始event_key/版本/摘要在维度独立保留）。
const maxGenerationEventKeyLen = 60

// generationEvidenceKey 生成证据的定长内部身份：对原始event_key、内容摘要、版本
// （及用途标记）做哈希，固定68字符。同证据重放命中同一行；同事件的异内容仍是
// 新行，但是否隔离为争议由事件身份检查决定，不再依赖key拼接。
func generationEvidenceKey(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "gen-" + hex.EncodeToString(h.Sum(nil))
}

// generationEventEvidence 按「账号+attempt+原始event_key」归集既有生成证据，
// 返回每条的证据摘要。同key异内容是否隔离为争议由此判定（§19.6）。
type generationEventEvidence struct {
	id     string
	digest string
}

func generationEventEvidenceOf(
	ctx context.Context,
	tx store.TxAccountScope,
	attemptID, eventKey string,
) ([]generationEventEvidence, error) {
	rows, err := tx.Query(ctx, "llm_usage_measurements", "id,evidence_digest",
		"attempt_id=$2 AND cost_component='generation_total' AND usage_dimensions->>'event_key'=$3",
		attemptID, eventKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []generationEventEvidence
	for rows.Next() {
		var e generationEventEvidence
		if err := rows.Scan(&e.id, &e.digest); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RecordGenerationUsageInTx 把一份完整用量向量按数据库中的冻结快照计价并原子
// 登记为generation_total成本位置（§19.6）。活动规则目录不参与计价：配置更换或
// 删除后，旧任务仍按受理时冻结的程序结算。证据按原始event_key归集：同摘要幂等，
// 异摘要隔离为争议；更高修订的合法证据（新事件身份）替换总额且只记差额，但不
// 解除费用争议——争议解除只能由受信核实入口RecordMeasurementInTx完成。缺维度
// 返回ErrGenerationFactsUnavailable，由调用方补齐后重投；实际用量超冻结硬上界
// 进入争议并阻断该部署新准入，不落0、不释放hold。
func (s *Service) RecordGenerationUsageInTx(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, attemptID, eventKey string,
	usage map[string]int64,
	providerRevision string,
) (SettlementReceipt, error) {
	if eventKey == "" || len(eventKey) > maxGenerationEventKeyLen {
		return SettlementReceipt{}, fmt.Errorf("%w: generation event key", ErrValidation)
	}
	if len(usage) == 0 {
		return SettlementReceipt{}, fmt.Errorf("%w: generation usage vector required", ErrValidation)
	}
	// ADR-008：归属由服务核验，不依赖外键；记错attempt等于挪动预算桶。
	owned, err := tx.Exists(ctx, "llm_attempts", "id=$2 AND request_id=$3", attemptID, requestID)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if !owned {
		return SettlementReceipt{}, fmt.Errorf("%w: attempt does not belong to this request", ErrConflict)
	}
	now := s.now().UTC()
	// 快照事实先读（受理后不可变列，无需锁）：部署门需要部署身份。随后与该
	// 部署的新准入在同一把门上串行——超界争议的阻断写入未提交时，新准入必须
	// 在门外等待并在争议提交后看到阻断，而不是凭一次「未阻断」读穿过（月桶
	// 按月分行，跨月时它不构成双方的稳定边界）。
	adm, err := generationAdmissionOf(ctx, tx, requestID)
	if err != nil {
		return SettlementReceipt{}, err
	}
	if err := tx.LockGenerationDeploymentGate(ctx, adm.deploymentID); err != nil {
		return SettlementReceipt{}, err
	}
	// 锁序与RecordMeasurementInTx一致：月桶先于请求级与attempt级行。
	if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
		return SettlementReceipt{}, err
	}
	// 恢复的是快照里的旧程序，不是活动目录（GEN-06）。
	restored, err := RestorePolicyGeneration(adm.snapshot, adm.fingerprint)
	if err != nil {
		return SettlementReceipt{}, err
	}
	def := restored.PolicyDefinition()

	price, err := restored.PriceUsage(usage)
	if errors.Is(err, ErrCapability) {
		// 用量超出受信硬上限：引擎拒绝是正确契约；保留证据与hold、阻断该
		// 部署新准入（GEN-10）。费用争议由受信核实入口解除，部署恢复准入
		// 由部署控制面独立决定（§19.6）。
		return s.disputeGenerationUsage(ctx, tx, requestID, attemptID, eventKey, usage,
			adm.currency, def.DeploymentID, providerRevision,
			"usage exceeds the frozen hard upper bound", true, now)
	}
	if err != nil {
		return SettlementReceipt{}, err
	}
	if price.Currency != adm.currency {
		return SettlementReceipt{}, fmt.Errorf("%w: frozen program currency %s differs from the admitted %s",
			ErrConflict, price.Currency, adm.currency)
	}

	evidence := struct {
		Attempt      string           `json:"attempt_id"`
		EventKey     string           `json:"event_key"`
		Usage        map[string]int64 `json:"usage"`
		Total        int64            `json:"total_micros"`
		Currency     string           `json:"currency"`
		ProviderRev  string           `json:"provider_revision"`
		DeploymentID string           `json:"deployment_id"`
	}{attemptID, eventKey, usage, price.TotalMicros, price.Currency, providerRevision, def.DeploymentID}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return SettlementReceipt{}, err
	}
	evDigest := digest(payload)
	existing, err := generationEventEvidenceOf(ctx, tx, attemptID, eventKey)
	if err != nil {
		return SettlementReceipt{}, err
	}
	// 事件身份检查先于记账（§19.6）：本证据已被记录（同摘要）则幂等返回；
	// 同事件键存在异内容则隔离为争议，不得绕过冲突判定凭revision直接改账
	// （P1-2）。既有争议不影响同摘要重放的幂等。
	var recorded, conflict bool
	for _, prior := range existing {
		if prior.digest == evDigest {
			recorded = true
		} else {
			conflict = true
		}
	}
	if recorded {
		return SettlementReceipt{AttemptID: attemptID, PositionKey: string(ComponentGenerationTotal)}, nil
	}
	if conflict {
		return s.disputeGenerationUsage(ctx, tx, requestID, attemptID, eventKey, usage,
			adm.currency, def.DeploymentID, providerRevision,
			"same event key carries different content", false, now)
	}

	key := generationEvidenceKey(eventKey, evDigest, def.Version)
	priceRaw, err := json.Marshal(price)
	if err != nil {
		return SettlementReceipt{}, err
	}
	total := price.TotalMicros
	m := Measurement{
		AttemptID: attemptID,
		Key:       key,
		Component: ComponentGenerationTotal,
		Currency:  price.Currency,
		Dimensions: map[string]any{
			"usage":          usage,
			"price_lines":    price.Lines,
			"event_key":      eventKey,
			"policy_version": def.Version,
			"digest":         evDigest,
		},
		PriceRaw:       priceRaw,
		CostMicros:     &total,
		Certainty:      "known",
		Kind:           EvidenceDerivedFromUsage,
		ProviderRev:    providerRevision,
		EvidenceDigest: evDigest,
	}
	receipt, err := s.applyMeasurement(ctx, tx, requestID, m, now)
	if err != nil {
		return SettlementReceipt{}, err
	}
	// 更高修订的合法证据只替换总额，不解除费用争议：修订号证明先后顺序，不
	// 构成受信核实——争议解除收敛到RecordMeasurementInTx受信入口（§19.6）。
	// 重算hold与结算状态。
	if err := s.refreshGenerationAfterEvidence(ctx, tx, requestID, now); err != nil {
		return SettlementReceipt{}, err
	}
	return receipt, nil
}

// disputeGenerationUsage 保存争议证据（unknown certainty、disputed处置），保留
// 供应商修订号；超上界争议额外写入部署阻断并要求核实。同一事务内重算hold与
// 结算状态：未决争议参与未决判定，已入账金额不被回滚（P1-1）。重复送达同一份
// 争议证据幂等。
func (s *Service) disputeGenerationUsage(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, attemptID, eventKey string,
	usage map[string]int64,
	currency, deploymentID, providerRevision, reason string,
	blockDeployment bool,
	now time.Time,
) (SettlementReceipt, error) {
	payload, err := json.Marshal(struct {
		Attempt      string           `json:"attempt_id"`
		EventKey     string           `json:"event_key"`
		Usage        map[string]int64 `json:"usage"`
		Reason       string           `json:"reason"`
		ProviderRev  string           `json:"provider_revision"`
		DeploymentID string           `json:"deployment_id"`
	}{attemptID, eventKey, usage, reason, providerRevision, deploymentID})
	if err != nil {
		return SettlementReceipt{}, err
	}
	evDigest := digest(payload)
	key := generationEvidenceKey(eventKey, evDigest, "dispute")
	dimensions, err := json.Marshal(map[string]any{
		"usage":             usage,
		"event_key":         eventKey,
		"dispute_reason":    reason,
		"provider_revision": providerRevision,
	})
	if err != nil {
		return SettlementReceipt{}, err
	}
	id := newID("llmu")
	row := tx.InsertOnConflictDoNothingReturning(ctx, "llm_usage_measurements",
		[]string{"id", "attempt_id", "measurement_key", "cost_component", "currency", "usage_dimensions",
			"price_snapshot", "cost_micros", "token_count", "certainty", "evidence_kind", "evidence_rank",
			"provider_revision", "supersedes_id", "evidence_digest", "created_at"},
		[]string{"account_id", "attempt_id", "measurement_key"},
		[]string{"id"},
		id, attemptID, key, string(ComponentGenerationTotal), currency, dimensions,
		[]byte("{}"), nil, nil, "unknown", string(EvidenceProviderReported), EvidenceProviderReported.rank(),
		nullableText(providerRevision), nullableText(""), evDigest, now)
	var storedID string
	switch err := row.Scan(&storedID); {
	case errors.Is(err, store.ErrNoRows):
		// 同一份争议证据已保存：阻断与hold重算早已生效，不重复登记。
		storedID = ""
	case err != nil:
		return SettlementReceipt{}, err
	}
	if storedID != "" {
		if err := tx.Insert(ctx, "llm_measurement_dispositions",
			[]string{"measurement_id", "state", "reason", "created_at", "updated_at"},
			storedID, "disputed", reason, now, now); err != nil {
			return SettlementReceipt{}, err
		}
		if blockDeployment {
			if err := blockGenerationDeployment(ctx, tx, requestID, storedID, deploymentID, reason, now); err != nil {
				return SettlementReceipt{}, err
			}
		}
	}
	// 未决争议改变hold与结算：重算不得回滚已入账金额（P1-1）。
	if err := s.refreshGenerationAfterEvidence(ctx, tx, requestID, now); err != nil {
		return SettlementReceipt{}, err
	}
	s.logger.ErrorContext(ctx, "llm gateway disputed generation usage",
		"request_id", requestID, "attempt_id", attemptID, "event_key", eventKey, "reason", reason)
	return SettlementReceipt{ID: storedID, MeasurementID: storedID, AttemptID: attemptID, PositionKey: string(ComponentGenerationTotal)}, nil
}

// blockGenerationDeployment 持久化最小部署阻断：该账号对该部署的新准入暂停，
// 要求人工核实后解除（GEN-10）。写入幂等：同部署后续争议不覆盖首次阻断的来源
// 事实，且不因主键冲突回滚——每份后续争议证据都必须完整保留。
func blockGenerationDeployment(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID, measurementID, deploymentID, reason string,
	now time.Time,
) error {
	row := tx.InsertOnConflictDoNothingReturning(ctx, "llm_generation_deployment_blocks",
		[]string{"deployment_id", "reason", "source_request_id", "source_measurement_id", "blocked_at"},
		[]string{"account_id", "deployment_id"},
		[]string{"deployment_id"},
		deploymentID, reason, requestID, measurementID, now)
	var inserted string
	if err := row.Scan(&inserted); err != nil && !errors.Is(err, store.ErrNoRows) {
		return err
	}
	return nil
}

// GenerationDeploymentBlocked 报告该账号对某部署的新准入是否被争议阻断；
// 既有operation的幂等回读不受影响。
func (s *Service) GenerationDeploymentBlocked(ctx context.Context, tx store.TxAccountScope, deploymentID string) (bool, error) {
	return tx.Exists(ctx, "llm_generation_deployment_blocks", "deployment_id=$2", deploymentID)
}

// resolveGenerationDisputes 以受信核实证据解除同attempt上的未决生成争议：
// 带known金额的账单/人工对账结论才构成核实，修订号只证明先后、不单独充当
// 核实（§19.6）。resolvingMeasurementID必须是本事务内已落账的llmu_证据，
// 解除记录指向核实证据而非结算回执号；部署是否恢复准入由部署控制面独立
// 决定，不随争议解除自动恢复。
func (s *Service) resolveGenerationDisputes(
	ctx context.Context,
	tx store.TxAccountScope,
	attemptID, resolvingMeasurementID string,
	now time.Time,
) error {
	affected, err := tx.Update(ctx, "llm_measurement_dispositions",
		"state='obsolete',resolved_by_measurement_id=$3,updated_at=$4",
		"state='disputed' AND measurement_id IN (SELECT id FROM llm_usage_measurements WHERE account_id=$1 AND attempt_id=$2"+
			" AND certainty='unknown' AND cost_component='generation_total')",
		attemptID, resolvingMeasurementID, now)
	if err != nil {
		return err
	}
	if affected > 0 {
		s.logger.InfoContext(ctx, "llm gateway resolved generation disputes by trusted evidence",
			"attempt_id", attemptID, "resolving_measurement_id", resolvingMeasurementID, "resolved", affected)
	}
	return nil
}

// refreshGenerationAfterEvidence 锁定预留后重算生成hold与结算状态。未决争议
// （disputed证据）参与未决判定；hold重新抬高若撞月度上限只记日志不丢证据。
func (s *Service) refreshGenerationAfterEvidence(ctx context.Context, tx store.TxAccountScope, requestID string, now time.Time) error {
	reservation, err := s.lockReservationAfterBudget(ctx, tx, "request_id=$2", requestID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.recomputeGenerationHold(ctx, tx, requestID, reservation, now); err != nil {
		if errors.Is(err, ErrBudget) {
			// 已入账金额不回滚、争议证据必须保留：预算不足以重新抬高hold时
			// 降级为「结算状态重开、hold维持现状」，人工核实时一并处理。
			s.logger.ErrorContext(ctx, "llm gateway could not re-raise the generation hold",
				"request_id", requestID, "error", err)
		} else {
			return err
		}
	}
	return s.refreshSettlement(ctx, tx, requestID, now)
}

// generationOpenAttemptsCond 匹配仍处于未决状态的attempt：没有known成本位置、
// 派发结果未知，或存在未解决的生成争议证据。三者都必须保留预留（P1-1）。
const generationOpenAttemptsCond = "request_id=$2 AND (dispatch_state='unknown' OR NOT EXISTS (" +
	"SELECT 1 FROM llm_cost_positions p WHERE p.account_id=$1 AND p.attempt_id=llm_attempts.id" +
	" AND p.cost_component='generation_total' AND p.certainty='known')" +
	" OR EXISTS (SELECT 1 FROM llm_usage_measurements m JOIN llm_measurement_dispositions d" +
	" ON d.account_id=$1 AND d.measurement_id=m.id" +
	" WHERE m.account_id=$1 AND m.attempt_id=llm_attempts.id AND m.cost_component='generation_total'" +
	" AND d.state='disputed'))"

// recomputeGenerationHold 是生成请求的hold重算分支：仅当请求的每个attempt都有
// known的generation_total证据、无未知派发且无未决争议时，剩余hold清零；否则
// 保留（或重新抬高至）原上界。已入账金额按预留币种累计到actual，不回滚。
func (s *Service) recomputeGenerationHold(
	ctx context.Context,
	tx store.TxAccountScope,
	requestID string,
	reservation reservationRow,
	now time.Time,
) (int64, error) {
	rows, err := tx.Query(ctx, "llm_cost_positions", "attempt_id,currency,booked_cost_micros",
		"attempt_id IN (SELECT id FROM llm_attempts WHERE account_id=$1 AND request_id=$2)", requestID)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total int64
	for rows.Next() {
		var attempt, currency string
		var value int64
		if err := rows.Scan(&attempt, &currency, &value); err != nil {
			return 0, err
		}
		if currency == reservation.currency {
			total += value
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	open, err := tx.Count(ctx, "llm_attempts", generationOpenAttemptsCond, requestID)
	if err != nil {
		return 0, err
	}
	hold := reservation.initialMicros
	if open == 0 {
		hold = 0
	}
	if hold != reservation.holdMicros {
		if err := s.applyBudgetDelta(ctx, tx, reservation.period, reservation.currency, budgetDelta{
			reservedMicros: hold - reservation.holdMicros,
		}, 0, 0, now); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Update(ctx, "llm_usage_reservations",
		"remaining_hold_micros=$3,actual_micros=$4,revision=revision+1,updated_at=$5",
		"id=$2", reservation.id, hold, total, now); err != nil {
		return 0, err
	}
	return hold, nil
}
