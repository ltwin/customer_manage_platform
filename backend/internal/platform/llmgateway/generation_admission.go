package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// 生成请求在llm_requests上的类别值；旧行默认chat（0052迁移）。
const (
	requestKindChat       = "chat"
	requestKindGeneration = "generation"
)

// persistentModelKey 与llm_requests.model_key列CHECK一致：生成身份在持久准入前
// 明确拒绝超长或大写形态，不做截断（§19.8存储契约对齐）。
var persistentModelKey = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

// GenerationAdmission 是生成请求持久受理的受信输入。调用方先完成业务守卫与
// 最终许可检查，再进入本入口；这里只做预算、身份与存储契约，不调用Adapter。
type GenerationAdmission struct {
	CallerService      string
	CallerOperationID  string
	CallerGroupID      string
	GroupLimitMicros   int64
	GroupTokenLimit    int64
	GroupDeadline      time.Time
	AccountLimitMicros int64
	AccountTokenLimit  int64
	ConsumerKey        string
	Deadline           time.Time
	Prepared           PreparedPolicyGeneration
	// Route 只由受信模型目录提供；为空时保留旧单部署生命周期。
	Route []GenerationRouteCandidate
}

// generationAdmissionControl 保存首次调用的控制输入，独立于可演进的组预算与
// consumer状态。额度保留调用方原值（包括0默认值），不随活动策略重新归一；
// 时间统一为UTC字符串，保留精度且消除时区表示差异。
type generationAdmissionControl struct {
	CallerGroupID      string `json:"caller_group_id"`
	GroupLimitMicros   int64  `json:"group_limit_micros"`
	GroupTokenLimit    int64  `json:"group_token_limit"`
	GroupDeadline      string `json:"group_deadline"`
	AccountLimitMicros int64  `json:"account_limit_micros"`
	AccountTokenLimit  int64  `json:"account_token_limit"`
	ConsumerKey        string `json:"consumer_key"`
	Deadline           string `json:"deadline"`
}

func (in GenerationAdmission) control() generationAdmissionControl {
	return generationAdmissionControl{
		CallerGroupID: in.CallerGroupID, GroupLimitMicros: in.GroupLimitMicros,
		GroupTokenLimit: in.GroupTokenLimit, GroupDeadline: in.GroupDeadline.UTC().Format(time.RFC3339Nano),
		AccountLimitMicros: in.AccountLimitMicros, AccountTokenLimit: in.AccountTokenLimit,
		ConsumerKey: in.ConsumerKey, Deadline: in.Deadline.UTC().Format(time.RFC3339Nano),
	}
}

// generationAdmissionRow 是生成扩展表的持久事实；快照BYTEA是唯一受信程序来源。
type generationAdmissionRow struct {
	snapshot     []byte
	fingerprint  string
	currency     string
	upperMicros  int64
	deploymentID string
}

// PrepareGenerationInTx 在调用方的受理事务内固定生成请求身份：预算预留、请求行、
// 冻结快照扩展行与结果保留根一起落库，任一步失败整体回滚。不执行网络（§19.5 T1）。
// 同operation重放读取原回执；生成指纹或首次控制输入不同均明确冲突。
func (s *Service) PrepareGenerationInTx(ctx context.Context, tx store.TxAccountScope, in GenerationAdmission) (RequestView, error) {
	if err := validateCallerIdentity(in.CallerService, in.CallerOperationID, in.CallerGroupID); err != nil {
		return RequestView{}, err
	}
	if in.ConsumerKey == "" || len(in.ConsumerKey) > 120 {
		return RequestView{}, fmt.Errorf("%w: consumer key required", ErrValidation)
	}
	def := in.Prepared.PolicyDefinition()
	if def.EngineVersion == "" || def.Version == "" {
		return RequestView{}, fmt.Errorf("%w: a prepared generation policy is required", ErrValidation)
	}
	// 存储契约对齐：超长模型键/版本在持久准入前拒绝，绝不截断身份。
	if !persistentModelKey.MatchString(def.Profile.ModelKey) {
		return RequestView{}, fmt.Errorf("%w: generation model key %q is not persistable", ErrValidation, def.Profile.ModelKey)
	}
	if len(def.Version) > 64 || len(def.EngineVersion) > 64 {
		return RequestView{}, fmt.Errorf("%w: generation policy version is not persistable", ErrValidation)
	}
	if in.Deadline.IsZero() || in.GroupDeadline.IsZero() {
		return RequestView{}, fmt.Errorf("%w: generation admission deadlines", ErrValidation)
	}
	quote := in.Prepared.Quote()
	// 没有可靠费用上界就不准入；不允许以0预留冒充上界（§19.5 T0）。
	if def.Billing != GenerationBillingDisabled && quote.UpperBoundMicros == nil {
		return RequestView{}, fmt.Errorf("%w: generation quote has no reliable upper bound", ErrBudget)
	}
	// 预算桶只有一种币种；不同币种的生成报价必须拒绝，不做汇率换算（§19.6）。
	if def.Currency != s.catalog.Currency() {
		return RequestView{}, fmt.Errorf("%w: generation currency %s differs from the budget currency %s",
			ErrValidation, def.Currency, s.catalog.Currency())
	}

	fingerprint := in.Prepared.Fingerprint()
	// 重放优先：已受理的operation回读原回执，不重新取价、不新增预留。
	if existing, found, err := s.replayGenerationAdmission(ctx, tx, in, fingerprint); found || err != nil {
		return existing, err
	}

	now := s.now().UTC()
	if !in.Deadline.After(now) {
		return RequestView{}, fmt.Errorf("%w: deadline already passed", ErrDeadline)
	}
	var hold int64
	if def.Billing != GenerationBillingDisabled {
		hold = *quote.UpperBoundMicros
	}
	currency := s.catalog.Currency()
	period := monthStart(now)

	// 公共锁序：组准入 → 预算桶 → 预留/请求（§19.5）。
	group, err := s.lockGroup(ctx, tx, in.CallerService, in.CallerGroupID,
		in.GroupLimitMicros, in.GroupTokenLimit, in.GroupDeadline, currency, now)
	if err != nil {
		return RequestView{}, err
	}
	// 组锁是同组受理的串行点：等锁期间先到者可能已提交同一operation，必须
	// 重读实际绑定而不是把回放误判成新消费去查额度（P2-4）。
	if existing, found, err := s.replayGenerationAdmission(ctx, tx, in, fingerprint); found || err != nil {
		return existing, err
	}
	// 部署门先于阻断检查：争议事务的阻断写入与新准入的检查共享同一把
	// advisory锁，消除「读到未阻断→争议提交阻断→准入继续」的交错；预算桶
	// 按月分行，跨月时它是双方唯一的稳定同步边界。
	if err := tx.LockGenerationDeploymentGate(ctx, def.DeploymentID); err != nil {
		return RequestView{}, err
	}
	// 门内读阻断状态：超上界争议阻断该账号对该部署的新准入；既有operation的
	// 重放已在上面返回。
	blocked, err := s.GenerationDeploymentBlocked(ctx, tx, def.DeploymentID)
	if err != nil {
		return RequestView{}, err
	}
	if blocked && def.Billing != GenerationBillingDisabled {
		return RequestView{}, fmt.Errorf("%w: generation deployment %s is paused for admission pending verification",
			ErrUnavailable, def.DeploymentID)
	}
	if def.Billing != GenerationBillingDisabled {
		// 生成的token维度不适用：组token上限按0增量检查，不生成伪token证据。
		if err := s.checkGroupCeiling(ctx, tx, in.CallerService, in.CallerGroupID, group, hold, 0); err != nil {
			return RequestView{}, err
		}
		if err := s.applyBudgetDelta(ctx, tx, period, currency, budgetDelta{
			reservedMicros: hold,
		}, in.AccountLimitMicros, in.AccountTokenLimit, now); err != nil {
			return RequestView{}, err
		}

	}

	reservationID := newID("llmv")
	if err := s.insertGenerationReservation(ctx, tx, in, group, reservationID, def, period, currency, hold, now); err != nil {
		return RequestView{}, err
	}
	view, err := s.insertGenerationRequest(ctx, tx, in, def, quote, fingerprint, reservationID, hold, now)
	if err != nil {
		return RequestView{}, err
	}
	return view, nil
}

// replayGenerationAdmission 回读同operation的既有受理。found表示存在行：
// 指纹与控制输入一致返回原视图，跨类别或异输入返回冲突。
func (s *Service) replayGenerationAdmission(
	ctx context.Context,
	tx store.TxAccountScope,
	in GenerationAdmission,
	fingerprint string,
) (RequestView, bool, error) {
	row, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns,
		"caller_service=$2 AND caller_operation_id=$3", in.CallerService, in.CallerOperationID))
	if errors.Is(err, ErrNotFound) {
		return RequestView{}, false, nil
	}
	if err != nil {
		return RequestView{}, false, err
	}
	if row.kind != requestKindGeneration {
		return RequestView{}, true, fmt.Errorf("%w: operation already bound to a %s request", ErrConflict, row.kind)
	}
	var stored string
	var controlRaw []byte
	if err := tx.QueryRow(ctx, "llm_generation_requests", "fingerprint,admission_control", "request_id=$2", row.id).Scan(&stored, &controlRaw); err != nil {
		return RequestView{}, true, err
	}
	if stored != fingerprint {
		return RequestView{}, true, fmt.Errorf("%w: operation reused with different generation content", ErrConflict)
	}
	var control generationAdmissionControl
	if err := json.Unmarshal(controlRaw, &control); err != nil {
		return RequestView{}, true, err
	}
	if control != in.control() {
		return RequestView{}, true, fmt.Errorf("%w: operation reused with different generation controls", ErrConflict)
	}
	settlement, err := settlementOf(ctx, tx, row.id)
	if err != nil {
		return RequestView{}, true, err
	}
	return row.view(settlement), true, nil
}

// insertGenerationReservation 落生成预留：金额上界即hold，token维度为0。
// 组上限沿用lockGroup归一后的值，与Chat预留同构。并发同键插入由唯一键裁决，
// 输方事务回滚后重放即得胜者回执（GEN-05）。
func (s *Service) insertGenerationReservation(
	ctx context.Context,
	tx store.TxAccountScope,
	in GenerationAdmission,
	group groupRow,
	id string,
	def GenerationPolicyDefinition,
	period time.Time,
	currency string,
	hold int64,
	now time.Time,
) error {
	err := tx.Insert(ctx, "llm_usage_reservations",
		[]string{"id", "caller_service", "caller_operation_id", "caller_group_id", "group_limit_micros",
			"group_token_limit", "budget_period", "currency", "initial_reserved_micros", "initial_reserved_tokens",
			"remaining_hold_micros", "remaining_hold_tokens", "settlement_state", "price_version", "expires_at"},
		id, in.CallerService, in.CallerOperationID, in.CallerGroupID, group.limitMicros, group.tokenLimit,
		period, currency, hold, int64(0), hold, int64(0), string(SettlementUnclaimed), def.Version, in.Deadline.UTC())
	if store.IsUniqueViolation(err, "") {
		return fmt.Errorf("%w: concurrent admission for the same operation", ErrConflict)
	}
	return err
}

// insertGenerationRequest 建立请求行、生成扩展行并认领预留与结果保留根。
// 指纹转为旧sha256-格式存入request_hash；快照BYTEA之外仅存可重建投影。
func (s *Service) insertGenerationRequest(
	ctx context.Context,
	tx store.TxAccountScope,
	in GenerationAdmission,
	def GenerationPolicyDefinition,
	quote GenerationQuote,
	fingerprint, reservationID string,
	hold int64,
	now time.Time,
) (RequestView, error) {
	snapshotBytes, err := in.Prepared.MarshalSnapshot()
	if err != nil {
		return RequestView{}, err
	}
	envelope := in.Prepared.Generation().Snapshot().Request
	payloadJSON, err := json.Marshal(envelope)
	if err != nil {
		return RequestView{}, err
	}
	modelJSON, err := json.Marshal(def)
	if err != nil {
		return RequestView{}, err
	}
	priceJSON, err := json.Marshal(quote)
	if err != nil {
		return RequestView{}, err
	}
	linesJSON, err := json.Marshal(quote.Lines)
	if err != nil {
		return RequestView{}, err
	}
	controlJSON, err := json.Marshal(in.control())
	if err != nil {
		return RequestView{}, err
	}

	id := newID("llmr")
	err = tx.Insert(ctx, "llm_requests",
		[]string{"id", "caller_service", "caller_operation_id", "caller_group_id", "request_hash", "model_key",
			"catalog_version", "price_version", "model_snapshot", "price_snapshot", "request_payload",
			"output_limit", "deadline_at", "state", "request_kind", "created_at", "updated_at", "retained_until"},
		id, in.CallerService, in.CallerOperationID, in.CallerGroupID, "sha256-"+fingerprint, def.Profile.ModelKey,
		def.EngineVersion, def.Version, modelJSON, priceJSON, payloadJSON,
		0, in.Deadline.UTC(), string(StatePrepared), requestKindGeneration, now, now, now.Add(s.retention))
	if store.IsUniqueViolation(err, "") {
		return RequestView{}, fmt.Errorf("%w: concurrent admission for the same operation", ErrConflict)
	}
	if err != nil {
		return RequestView{}, err
	}

	// 未计费部署保留调用许可身份行，不占预算且实际金额保持NULL。
	settlement := SettlementReserved
	if def.Billing == GenerationBillingDisabled {
		settlement = SettlementNotApplicable
	}
	// 预留只能被一个请求认领一次；失败则整个受理事务回滚。
	claimed, err := tx.Update(ctx, "llm_usage_reservations",
		"request_id=$3,settlement_state=$4,revision=revision+1,updated_at=$5",
		"id=$2 AND request_id IS NULL AND settlement_state=$6",
		reservationID, id, string(settlement), now, string(SettlementUnclaimed))
	if err != nil {
		return RequestView{}, err
	}
	if claimed == 0 {
		return RequestView{}, fmt.Errorf("%w: reservation already claimed", ErrConflict)
	}

	if err := tx.Insert(ctx, "llm_generation_requests",
		[]string{"request_id", "policy_snapshot", "fingerprint", "admission_control", "engine_version", "policy_version",
			"model_key", "model_revision", "deployment_id", "adapter_version", "mode_key", "kind",
			"currency", "reserved_upper_bound_micros", "quote_lines", "created_at", "updated_at"},
		id, snapshotBytes, fingerprint, controlJSON, def.EngineVersion, def.Version,
		def.Profile.ModelKey, def.Profile.ModelRevision, def.DeploymentID, def.AdapterVersion,
		envelope.Mode, string(envelope.Kind), def.Currency, hold, linesJSON, now, now); err != nil {
		return RequestView{}, err
	}
	if err := tx.Insert(ctx, "llm_result_consumers",
		[]string{"request_id", "caller_service", "consumer_key", "state", "registered_at"},
		id, in.CallerService, in.ConsumerKey, "pending", now); err != nil {
		return RequestView{}, err
	}
	return RequestView{
		ID:            id,
		CallerService: in.CallerService,
		OperationID:   in.CallerOperationID,
		GroupID:       in.CallerGroupID,
		Kind:          requestKindGeneration,
		ModelKey:      def.Profile.ModelKey,
		RequestHash:   "sha256-" + fingerprint,
		State:         StatePrepared,
		Deadline:      in.Deadline.UTC(),
		Settlement:    settlement,
		RetainedUntil: now.Add(s.retention),
		Revision:      1,
	}, nil
}

// generationAdmissionOf 读取生成扩展行的持久事实，供结算路径恢复冻结程序；
// deployment_id参与部署门，是不可变的受理事实，无锁先读是安全的。
func generationAdmissionOf(ctx context.Context, tx rowReader, requestID string) (generationAdmissionRow, error) {
	var row generationAdmissionRow
	err := tx.QueryRow(ctx, "llm_generation_requests",
		"policy_snapshot,fingerprint,currency,reserved_upper_bound_micros,deployment_id",
		"request_id=$2", requestID).Scan(&row.snapshot, &row.fingerprint, &row.currency, &row.upperMicros, &row.deploymentID)
	if errors.Is(err, store.ErrNoRows) {
		return generationAdmissionRow{}, ErrNotFound
	}
	return row, err
}
