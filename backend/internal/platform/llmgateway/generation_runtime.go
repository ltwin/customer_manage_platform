package llmgateway

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const generationLease = 3 * time.Minute
const generationPoll = 15 * time.Second
const generationRecoveryWindow = 24 * time.Hour

// GenerationCall 只由已提交的派发/观察许可构建；TaskID和输入不进入普通API或日志。
// Adapter必须服从ctx期限。Submit的错误一律表示受理未知，绝不授权再次Submit。
type GenerationCall struct {
	RequestID string
	AttemptID string
	TaskID    string
	Prepared  PreparedPolicyGeneration
}

// GenerationEvent 是受信Adapter归一化的单份证据。EventKey在attempt内稳定；
// 修订号是供应商单调整数，没有可比较顺序的矛盾证据必须隔离。
type GenerationEvent struct {
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
	LimitKey     string
	Concurrency  int
	RateLimit    int
	Adapter      GenerationAdapter
}

// GenerationGuard 在最终许可事务内复核素材用途/外发同意和调用方执行资格。
// 只允许数据库操作；组合根必须提供真实守卫，缺省时禁止构造可派发runtime。
type GenerationGuard func(context.Context, store.TxAccountScope, RequestView, PreparedPolicyGeneration) error

type GenerationRuntimeConfig struct {
	Gateway       *Service
	ProtectionKey []byte
	Adapters      []GenerationAdapterRegistration
	Guard         GenerationGuard
}

type GenerationRuntime struct {
	gateway  *Service
	adapters map[string]GenerationAdapterRegistration
	guard    GenerationGuard
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
	r := &GenerationRuntime{gateway: cfg.Gateway, guard: cfg.Guard, cipher: aead, adapters: make(map[string]GenerationAdapterRegistration)}
	for _, a := range cfg.Adapters {
		key := a.DeploymentID + "/" + a.Version
		if !generationToken(a.DeploymentID) || !generationToken(a.Version) || a.LimitKey == "" || len(a.LimitKey) > 120 || a.Concurrency < 1 || a.RateLimit < 1 || a.Adapter == nil {
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
func (r *GenerationRuntime) checkDeployment(ctx context.Context, tx store.TxAccountScope, d GenerationPolicyDefinition, expected int64) error {
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
	if blocked {
		return ErrUnavailable
	}
	return nil
}

// PrepareInTx 原子复用G2准入、最终业务守卫与队列登记。纯G2请求没有control_revision，
// 不会被恢复扫描器擅自派发；只有本入口受理的请求进入G3。
func (r *GenerationRuntime) PrepareInTx(ctx context.Context, tx store.TxAccountScope, in GenerationAdmission, expectedRevision int64) (RequestView, error) {
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
	if err := r.guard(ctx, tx, view, in.Prepared); err != nil {
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
	if _, err := tx.Update(ctx, "llm_generation_requests", "verification_required=FALSE,next_advance_at=$3", "request_id=$2 AND control_revision>0", requestID, r.gateway.now().UTC()); err != nil {
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
