package llmgateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// generationCallLifetime 限制生图的单次外部操作（提交、查询、取消）。它必须短于
// generationLease：调用还没结束租约就到期，另一个 worker 会接手同一任务。对话的
// transportLifetime 为长时间 thinking 放宽到 15 分钟，生图不能跟着用。
const generationCallLifetime = 2 * time.Minute
const generationLease = 3 * time.Minute
const generationPoll = 15 * time.Second
const generationRecoveryWindow = 24 * time.Hour

// maxBlindFailures 限制受理与否尚未确定时的连续失败；已知任务的查询不受此限。
const maxBlindFailures = 5

// maxGenerationBackoff 是已知任务查询连续失败时的最长退避。
const maxGenerationBackoff = time.Minute

// generationRateLimitBackoff 是供应商以限流明确拒收后的重试间隔。平台不再设
// 共享名额，供应商账号的并发上限只能靠这次等待避开（RunningHub建议30–120秒）。
const generationRateLimitBackoff = time.Minute

// GenerationCall 只由已提交的派发/观察许可构建；TaskID和输入不进入普通API或日志。
// Adapter必须服从ctx期限。Submit的错误一律表示受理未知，绝不授权再次Submit。
type GenerationCall struct {
	RequestID string
	AttemptID string
	TaskID    string
	Prepared  PreparedPolicyGeneration
	// References 只在Submit时提供，按账号绑定；其他阶段为nil。
	References GenerationReferenceOpener
}

// GenerationReferenceOpener 打开已冻结参考素材的受保护字节。授权由实现按账号
// 复核；返回的字节仍须由Adapter按冻结摘要核对后才能外发。
type GenerationReferenceOpener interface {
	OpenGenerationReference(context.Context, GenerationReference) (io.ReadCloser, error)
}

// GenerationEvent 是受信Adapter归一化的单份证据。EventKey在attempt内稳定；
// 修订号是供应商单调整数，没有可比较顺序的矛盾证据必须隔离。
type GenerationEvent struct {
	// RefreshableMedia 将临时取件位置与期限排除在业务证据摘要之外。
	// 只由明确支持查询刷新位置的适配器设置；内容身份及费用仍参与去重。
	RefreshableMedia bool                  `json:"refreshable_media,omitempty"`
	EventKey         string                `json:"event_key"`
	ProviderRevision string                `json:"provider_revision,omitempty"`
	Observation      GenerationObservation `json:"observation"`
	Usage            map[string]int64      `json:"usage,omitempty"`
}

type GenerationAdapter interface {
	Submit(context.Context, GenerationCall) (GenerationEvent, error)
	Observe(context.Context, GenerationCall) (GenerationEvent, error)
	Cancel(context.Context, GenerationCall) (GenerationEvent, error)
}

// GenerationSubmissionLookup 只查询同一AttemptID的受理身份，不得实现为重新Submit。
type GenerationSubmissionLookup interface {
	LookupSubmission(context.Context, GenerationCall) (GenerationEvent, error)
}

type GenerationAdapterRegistration struct {
	DeploymentID string
	Version      string
	Adapter      GenerationAdapter
	// Available 在新派发前检查凭证和配置有效期，已有任务观察不受影响。
	Available func() bool
}

// GenerationGuard 在最终许可事务内复核素材用途/外发同意和调用方执行资格。
// 只允许数据库操作；组合根必须提供真实守卫，缺省时禁止构造可派发runtime。
type GenerationGuardRequest struct {
	RootRequestID string
	Call          RequestView
}
type GenerationGuard func(context.Context, store.TxAccountScope, GenerationGuardRequest, PreparedPolicyGeneration) error

type GenerationRuntimeConfig struct {
	Gateway       *Service
	ProtectionKey []byte
	Adapters      []GenerationAdapterRegistration
	Guard         GenerationGuard
	// Prelock只取得调用方外层事务锁，不读取供应商或执行网络。
	Prelock func(context.Context, store.TxAccountScope) error
	// References 为Submit绑定账号的参考素材读取端口；缺省时带参考的请求无法提交。
	References func(store.AccountScope) GenerationReferenceOpener
}

type GenerationRuntime struct {
	gateway  *Service
	adapters map[string]GenerationAdapterRegistration
	guard    GenerationGuard
	prelock  func(context.Context, store.TxAccountScope) error
	refs     func(store.AccountScope) GenerationReferenceOpener
	cipher   cipher.AEAD
	runtime  jobs.Runtime
}

func NewGenerationRuntime(cfg GenerationRuntimeConfig) (*GenerationRuntime, error) {
	if cfg.Gateway == nil || cfg.Guard == nil || len(cfg.ProtectionKey) != 32 {
		return nil, fmt.Errorf("%w: generation runtime dependencies", ErrValidation)
	}
	block, err := aes.NewCipher(cfg.ProtectionKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	r := &GenerationRuntime{gateway: cfg.Gateway, guard: cfg.Guard, prelock: cfg.Prelock, refs: cfg.References, cipher: aead, adapters: make(map[string]GenerationAdapterRegistration)}
	for _, a := range cfg.Adapters {
		key := a.DeploymentID + "/" + a.Version
		if !generationToken(a.DeploymentID) || !generationToken(a.Version) || a.Adapter == nil {
			return nil, ErrValidation
		}
		if _, ok := r.adapters[key]; ok {
			return nil, ErrConflict
		}
		r.adapters[key] = a
	}
	return r, nil
}
func (r *GenerationRuntime) SetRuntime(q jobs.Runtime) { r.runtime = q }

// 所有保护数据绑定账号、attempt和用途；随机nonce随密文保存，密钥只由组合根注入。
func (r *GenerationRuntime) seal(account, attempt, purpose string, plain []byte) ([]byte, error) {
	nonce := make([]byte, r.cipher.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return r.cipher.Seal(nonce, nonce, plain, []byte(account+"\x00"+attempt+"\x00"+purpose)), nil
}
func (r *GenerationRuntime) open(account, attempt, purpose string, sealed []byte) ([]byte, error) {
	n := r.cipher.NonceSize()
	if len(sealed) < n+r.cipher.Overhead() {
		return nil, ErrProtocol
	}
	plain, err := r.cipher.Open(nil, sealed[:n], sealed[n:], []byte(account+"\x00"+attempt+"\x00"+purpose))
	if err != nil {
		return nil, fmt.Errorf("%w: protected generation data unavailable", ErrUnavailable)
	}
	return plain, nil
}
func generationPolicyDigest(d GenerationPolicyDefinition) string {
	raw, _ := json.Marshal(d)
	// 规则中的RawMessage可能来自原始配置或已编译目录，键序不是策略身份。
	canonical, err := generationJSONObject(raw, maxGenerationPolicyBytes)
	if err != nil {
		return ""
	}
	return digest(canonical)
}

// ConfigureDeploymentInTx 将启停/策略修订写入持久控制面。它与派发共享部署门，
// 跨进程也不能通过旧内存目录绕过停用；已运行任务的观察不受新配置限制。
func (r *GenerationRuntime) ConfigureDeploymentInTx(ctx context.Context, tx store.TxAccountScope, d GenerationPolicyDefinition, enabled bool, validUntil time.Time, expected int64) (int64, error) {
	if !generationToken(d.DeploymentID) || !generationToken(d.AdapterVersion) || generationPolicyDigest(d) == "" || expected < 0 || validUntil.IsZero() {
		return 0, ErrValidation
	}
	if enabled {
		if _, ok := r.adapters[d.DeploymentID+"/"+d.AdapterVersion]; !ok {
			return 0, ErrUnavailable
		}
		if !validUntil.After(r.gateway.now()) {
			return 0, ErrDeadline
		}
	}
	if err := tx.LockGenerationDeploymentGate(ctx, d.DeploymentID); err != nil {
		return 0, err
	}
	var rev int64
	err := tx.QueryRow(ctx, "llm_generation_deployments", "revision", "deployment_id=$2", d.DeploymentID).Scan(&rev)
	if errors.Is(err, store.ErrNoRows) {
		if expected != 0 {
			return 0, ErrConflict
		}
		err = tx.Insert(ctx, "llm_generation_deployments", []string{"deployment_id", "revision", "enabled", "policy_digest", "adapter_version", "valid_until"}, d.DeploymentID, int64(1), enabled, generationPolicyDigest(d), d.AdapterVersion, validUntil)
		return 1, err
	}
	if err != nil {
		return 0, err
	}
	if rev != expected {
		return 0, ErrConflict
	}
	_, err = tx.Update(ctx, "llm_generation_deployments", "revision=revision+1,enabled=$3,policy_digest=$4,adapter_version=$5,valid_until=$6", "deployment_id=$2", d.DeploymentID, enabled, generationPolicyDigest(d), d.AdapterVersion, validUntil)
	return rev + 1, err
}

// EnsureDeploymentInTx 为当前账号登记内置部署：没有记录时启用；策略或Adapter
// 版本变化、有效期将尽时续期。被人工停用的部署保持停用，这里不会重新打开。
// 返回当前控制修订号。
func (r *GenerationRuntime) EnsureDeploymentInTx(ctx context.Context, tx store.TxAccountScope, d GenerationPolicyDefinition, validFor time.Duration) (int64, error) {
	if validFor <= 0 {
		return 0, ErrValidation
	}
	if err := tx.LockGenerationDeploymentGate(ctx, d.DeploymentID); err != nil {
		return 0, err
	}
	var rev int64
	var enabled bool
	var hash, version string
	var until time.Time
	err := tx.QueryRow(ctx, "llm_generation_deployments", "revision,enabled,policy_digest,adapter_version,valid_until", "deployment_id=$2", d.DeploymentID).Scan(&rev, &enabled, &hash, &version, &until)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return 0, err
	}
	now := r.gateway.now()
	if rev > 0 && (!enabled || hash == generationPolicyDigest(d) && version == d.AdapterVersion && until.After(now.Add(validFor/4))) {
		return rev, nil
	}
	if rev > 0 && enabled && hash == generationPolicyDigest(d) && version == d.AdapterVersion {
		// 仅延长同一策略的租期，不撤销在途请求已经冻结的派发修订号。
		_, err := tx.Update(ctx, "llm_generation_deployments", "valid_until=$3", "deployment_id=$2", d.DeploymentID, now.Add(validFor))
		return rev, err
	}
	return r.ConfigureDeploymentInTx(ctx, tx, d, true, now.Add(validFor), rev)
}

// DeploymentRevisionInTx 只读当前账号的部署控制修订号；派发前仍由PrepareInTx在
// 部署门内复核启用状态与策略摘要。
func (r *GenerationRuntime) DeploymentRevisionInTx(ctx context.Context, tx store.TxAccountScope, deploymentID string) (int64, error) {
	var rev int64
	err := tx.QueryRow(ctx, "llm_generation_deployments", "revision", "deployment_id=$2", deploymentID).Scan(&rev)
	if errors.Is(err, store.ErrNoRows) {
		return 0, ErrUnavailable
	}
	return rev, err
}

func (r *GenerationRuntime) checkDeployment(ctx context.Context, tx store.TxAccountScope, d GenerationPolicyDefinition, expected int64) error {
	if a, ok := r.adapters[d.DeploymentID+"/"+d.AdapterVersion]; !ok || a.Available != nil && !a.Available() {
		return ErrUnavailable
	}
	var rev int64
	var enabled bool
	var hash, version string
	var until time.Time
	err := tx.QueryRow(ctx, "llm_generation_deployments", "revision,enabled,policy_digest,adapter_version,valid_until", "deployment_id=$2", d.DeploymentID).Scan(&rev, &enabled, &hash, &version, &until)
	if errors.Is(err, store.ErrNoRows) {
		return ErrUnavailable
	}
	if err != nil {
		return err
	}
	if !enabled || !until.After(r.gateway.now()) {
		return ErrUnavailable
	}
	if rev != expected || hash != generationPolicyDigest(d) || version != d.AdapterVersion {
		return ErrConflict
	}
	blocked, err := r.gateway.GenerationDeploymentBlocked(ctx, tx, d.DeploymentID)
	if err != nil {
		return err
	}
	if blocked && d.Billing != GenerationBillingDisabled {
		return ErrUnavailable
	}
	return nil
}

// PrepareInTx 原子复用G2准入、最终业务守卫与队列登记。纯G2请求没有control_revision，
// 不会被恢复扫描器擅自派发；只有本入口受理的请求进入G3。
func (r *GenerationRuntime) PrepareInTx(ctx context.Context, tx store.TxAccountScope, in GenerationAdmission, expectedRevision int64) (RequestView, error) {
	if err := r.prelockCaller(ctx, tx); err != nil {
		return RequestView{}, err
	}
	if expectedRevision < 1 || r.runtime == nil {
		return RequestView{}, ErrValidation
	}
	active, err := tx.LockGenerationAccount(ctx)
	if err != nil {
		return RequestView{}, err
	}
	if !active {
		return RequestView{}, store.ErrCreativeAccessDenied
	}
	view, err := r.gateway.PrepareGenerationInTx(ctx, tx, in)
	if err != nil {
		return RequestView{}, err
	}
	if err := tx.LockGenerationDeploymentGate(ctx, in.Prepared.PolicyDefinition().DeploymentID); err != nil {
		return RequestView{}, err
	}
	var revision int64
	if err := tx.QueryRow(ctx, "llm_generation_requests", "control_revision", "request_id=$2", view.ID).Scan(&revision); err != nil {
		return RequestView{}, err
	}
	if revision != 0 {
		if revision != expectedRevision {
			return RequestView{}, ErrConflict
		}
		return view, nil
	}
	if err := r.checkDeployment(ctx, tx, in.Prepared.PolicyDefinition(), expectedRevision); err != nil {
		return RequestView{}, err
	}
	if err := r.authorizeGeneration(ctx, tx, view, in.Prepared); err != nil {
		return RequestView{}, err
	}
	if _, err := tx.Update(ctx, "llm_generation_requests", "control_revision=$3,next_advance_at=$4", "request_id=$2", view.ID, expectedRevision, r.gateway.now().UTC()); err != nil {
		return RequestView{}, err
	}
	if err := r.enqueue(ctx, tx, view.ID); err != nil {
		return RequestView{}, err
	}
	return view, nil
}

// ResumeVerificationInTx 只恢复查询/核实，不重发已使用的提交许可。
func (r *GenerationRuntime) ResumeVerificationInTx(ctx context.Context, tx store.TxAccountScope, requestID string) error {
	adm, err := generationAdmissionOf(ctx, tx, requestID)
	if err != nil {
		return err
	}
	if err := tx.LockGenerationDeploymentGate(ctx, adm.deploymentID); err != nil {
		return err
	}
	if err := r.gateway.lockBudgetForRequest(ctx, tx, requestID, r.gateway.now()); err != nil {
		return err
	}
	if _, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID)); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "llm_generation_tasks", "failures=0,recovery_until=$4", "request_id=$2 AND (lease_until IS NULL OR lease_until<=$3)", requestID, r.gateway.now().UTC(), r.gateway.now().UTC().Add(generationRecoveryWindow)); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "llm_generation_requests", "verification_required=FALSE,result_verification_required=FALSE,next_advance_at=$3", "request_id=$2 AND control_revision>0", requestID, r.gateway.now().UTC()); err != nil {
		return err
	}
	return r.enqueue(ctx, tx, requestID)
}

// ResolveDeploymentBlockInTx 是独立受信控制动作，不随任何账单自动放行。
func (r *GenerationRuntime) ResolveDeploymentBlockInTx(ctx context.Context, tx store.TxAccountScope, deploymentID string) error {
	if err := tx.LockGenerationDeploymentGate(ctx, deploymentID); err != nil {
		return err
	}
	_, err := tx.Delete(ctx, "llm_generation_deployment_blocks", "deployment_id=$2", deploymentID)
	return err
}

func (r *GenerationRuntime) prelockCaller(ctx context.Context, tx store.TxAccountScope) error {
	if r.prelock != nil {
		return r.prelock(ctx, tx)
	}
	return nil
}

// CheckDeploymentInTx 是受理前的只读候选筛选；最终派发仍在部署门内复核。
func (r *GenerationRuntime) CheckDeploymentInTx(ctx context.Context, tx store.TxAccountScope, d GenerationPolicyDefinition, revision int64) error {
	return r.checkDeployment(ctx, tx, d, revision)
}

func (r *GenerationRuntime) authorizeGeneration(ctx context.Context, tx store.TxAccountScope, call RequestView, prepared PreparedPolicyGeneration) error {
	root, err := generationRootInTx(ctx, tx, call.ID)
	if err != nil {
		return err
	}
	return r.guard(ctx, tx, GenerationGuardRequest{RootRequestID: root, Call: call}, prepared)
}
