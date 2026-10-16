// Package creativegeneration 编排Gateway、媒体与画布的生成交接，不实现供应商协议。
package creativegeneration

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	gw "github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const caller = "creative_canvas"
const jobKind = "canvas.generation_handoff"
const stepLease = 5 * time.Minute

var ErrBusy = errors.New("generation handoff already owned")

type Config struct {
	Gateway       *gw.Service
	Media         *creativemedia.Service
	ProtectionKey []byte
	Adapters      []gw.GenerationAdapterRegistration
	// Authorization 可选的附加守卫。外发不做逐次授权（owner 2026-10-09：同类产品只在
	// 注册时同意一次协议），素材生成用途与画布资格始终由guard复核。
	Authorization gw.GenerationGuard
	Fetchers      map[string]creativemedia.GenerationFetcher
	// References 在Submit时按账号打开参考字节；不带参考的部署可以留空。
	References func(store.AccountScope) gw.GenerationReferenceOpener
}
type Service struct {
	gateway       *gw.Service
	generation    *gw.GenerationRuntime
	media         *creativemedia.Service
	fetchers      map[string]creativemedia.GenerationFetcher
	queue         jobs.Runtime
	authorization gw.GenerationGuard
}

// New 显式受信装配；没有默认Adapter或取件白名单，也不自动开启生产部署。
func New(cfg Config) (*Service, error) {
	if cfg.Gateway == nil || cfg.Media == nil {
		return nil, gw.ErrValidation
	}
	s := &Service{gateway: cfg.Gateway, media: cfg.Media, fetchers: map[string]creativemedia.GenerationFetcher{}, authorization: cfg.Authorization}
	for key, f := range cfg.Fetchers {
		if f == nil {
			return nil, gw.ErrValidation
		}
		s.fetchers[key] = f
	}
	runtime, err := gw.NewGenerationRuntime(gw.GenerationRuntimeConfig{Gateway: cfg.Gateway, ProtectionKey: cfg.ProtectionKey, Adapters: cfg.Adapters, Guard: s.guard, References: cfg.References, Prelock: func(ctx context.Context, tx store.TxAccountScope) error { return tx.LockCreativeWrite(ctx) }})
	if err != nil {
		return nil, err
	}
	s.generation = runtime
	return s, nil
}
func (s *Service) GatewayRuntime() *gw.GenerationRuntime { return s.generation }
func (s *Service) SetRuntime(q jobs.Runtime)             { s.queue = q; s.generation.SetRuntime(q) }

// Request接收服务端预检结果；command只携带画布目标，价格/额度不能来自公开JSON。
func (s *Service) Request(ctx context.Context, scope store.AccountScope, c creativeops.Command, admission gw.GenerationAdmission, controlRevision int64) (creativeops.Receipt, error) {
	var in creativecanvas.RequestExecutionInput
	if err := creativeops.Decode(c.Payload, &in); err != nil {
		return creativeops.Receipt{}, err
	}
	if s.queue == nil || controlRevision < 1 {
		return creativeops.Receipt{}, gw.ErrValidation
	}
	execution := "cwexec_" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(scope.AccountID()+"/"+c.OperationID)).String()
	admission.CallerService = caller
	admission.CallerOperationID = c.OperationID
	admission.CallerGroupID = execution
	admission.ConsumerKey = execution
	// operation身份覆盖全部受信控制输入；重放先查回执，不重新执行预检、准入或入队。
	c.Payload = creativegraph.Canonical(map[string]any{"target": in, "fingerprint": admission.Prepared.Fingerprint(), "control_revision": controlRevision, "deadline": admission.Deadline, "group_deadline": admission.GroupDeadline, "group_limit": admission.GroupLimitMicros, "account_limit": admission.AccountLimitMicros, "group_tokens": admission.GroupTokenLimit, "account_tokens": admission.AccountTokenLimit})
	return (creativeops.Executor{}).Run(ctx, scope, creativeops.Operation{Key: "canvas.request_generation", Capability: "node_generate", Validate: func(json.RawMessage) error { return nil }, Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
		return s.requestInTx(ctx, tx, in, c.OperationID, admission, controlRevision, nil)
	}}, c)
}
func (s *Service) requestInTx(ctx context.Context, tx store.TxAccountScope, in creativecanvas.RequestExecutionInput, operation string, admission gw.GenerationAdmission, controlRevision int64, admit func(context.Context, store.TxAccountScope) error) (creativeops.Outcome, error) {
	execution := "cwexec_" + uuid.NewSHA1(uuid.NameSpaceOID, []byte(tx.AccountID()+"/"+operation)).String()
	admission.CallerService = caller
	admission.CallerOperationID = operation
	admission.CallerGroupID = execution
	admission.ConsumerKey = execution
	// 在预算冻结前编译句内引用；这里只读业务快照，不反向获取业务行锁。
	prompt, err := creativecanvas.CompileGenerationPromptInTx(ctx, tx, in)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	preparedPrompt := admission.Prepared.Generation().Snapshot().Request.Input.Prompt
	if preparedPrompt != prompt.Raw && preparedPrompt != prompt.Text {
		return creativeops.Outcome{}, creativecanvas.ErrVersionConflict
	}
	for i := range admission.Route {
		admission.Route[i].Prepared, err = admission.Route[i].Prepared.WithPrompt(prompt.Text)
		if err != nil {
			return creativeops.Outcome{}, err
		}
	}
	admission.Prepared, err = admission.Prepared.WithPrompt(prompt.Text)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	// Executor已先锁账号；G2取得group/deployment/budget/request，业务锁随后进入。
	request, err := s.gateway.PrepareGenerationInTx(ctx, tx, admission)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	// 发起方复核在额度之后、画布之前：与助手停止的 额度 → 运行 顺序一致。
	if admit != nil {
		if err = admit(ctx, tx); err != nil {
			return creativeops.Outcome{}, err
		}
	}
	snapshot := admission.Prepared.Generation().Snapshot().Request
	spec := creativecanvas.GenerationSpec{Kind: string(snapshot.Kind), Prompt: snapshot.Input.Prompt, ModelKey: snapshot.ModelKey, Parameters: snapshot.Input.Parameters}
	for _, mode := range admission.Prepared.PolicyDefinition().Modes {
		if mode.Definition.Key == snapshot.Mode {
			spec.ParameterDefaults = mode.Defaults
		}
	}
	for _, ref := range snapshot.Input.References {
		spec.References = append(spec.References, creativecanvas.GenerationReference{RevisionID: ref.RevisionID, Role: ref.Role})
	}
	e, err := creativecanvas.AdmitGenerationInTx(ctx, tx, in, spec, execution, operation, request.RequestHash, admission.Deadline)
	if err != nil {
		return creativeops.Outcome{}, err
	}
	if err = tx.Insert(ctx, "creative_generation_bindings", []string{"execution_id", "request_id", "canvas_id", "next_step_at", "recovery_until"}, e.ID, request.ID, e.CanvasID, e.CreatedAt, e.CreatedAt.Add(24*time.Hour)); err != nil {
		return creativeops.Outcome{}, err
	}
	if _, err = s.generation.PrepareRouteInTx(ctx, tx, admission, controlRevision); err != nil {
		return creativeops.Outcome{}, err
	}
	target := creativecanvas.ExecutionTarget{CanvasID: e.CanvasID, ExecutionID: e.ID}
	if err = s.enqueue(ctx, tx, target); err != nil {
		return creativeops.Outcome{}, err
	}
	body, err := json.Marshal(e)
	return creativeops.Outcome{HTTPStatus: 202, ResultKind: "node_execution", ResultID: &e.ID, Response: body}, err
}

func (s *Service) guard(ctx context.Context, tx store.TxAccountScope, request gw.GenerationGuardRequest, p gw.PreparedPolicyGeneration) error {
	if request.Call.CallerService != caller {
		return gw.ErrValidation
	}
	var target creativecanvas.ExecutionTarget
	if err := tx.QueryRow(ctx, "creative_generation_bindings", "execution_id,canvas_id", "request_id=$2", request.RootRequestID).Scan(&target.ExecutionID, &target.CanvasID); err != nil {
		return err
	}
	if err := creativecanvas.GuardGenerationInTx(ctx, tx, target); err != nil {
		return generationGuardError(err)
	}
	if err := s.media.ValidateGenerationReferencesInTx(ctx, tx, p.Generation().Snapshot().Request.Input.References); err != nil {
		return generationGuardError(err)
	}
	if s.authorization != nil {
		return generationGuardError(s.authorization(ctx, tx, request, p))
	}
	return nil
}

// 已知业务拒绝证明未获派发许可；存储/上下文错误不假装明确拒绝。
func generationGuardError(err error) error {
	switch {
	case errors.Is(err, store.ErrCreativeAccessDenied), errors.Is(err, creativecontent.ErrUsageDenied), errors.Is(err, creativecontent.ErrMissingRoot):
		return gw.ErrCapability
	case errors.Is(err, store.ErrNoRows), errors.Is(err, creativecanvas.ErrVersionConflict), errors.Is(err, creativecanvas.ErrNotFound), errors.Is(err, creativecanvas.ErrArchived), errors.Is(err, creativecontent.ErrNotFound):
		return gw.ErrConflict
	default:
		return err
	}
}
func (s *Service) Handlers() []store.JobHandler {
	return append(s.generation.Handlers(), store.JobHandler{Kind: jobKind, AllowInactiveAccount: true, Timeout: stepLease, Work: s.Work})
}
func (s *Service) enqueue(ctx context.Context, tx store.TxAccountScope, target creativecanvas.ExecutionTarget) error {
	_, err := jobs.EnqueueInTx(ctx, tx.Jobs(s.queue), jobs.Request{Kind: jobKind, OperationID: uuid.NewString(), CreatedAt: time.Now().UTC(), Payload: creativegraph.Canonical(target), UniqueByArgs: true})
	return err
}

type binding struct {
	request  string
	epoch    int64
	failures int
	until    time.Time
}

func (s *Service) claim(ctx context.Context, scope store.AccountScope, target creativecanvas.ExecutionTarget) (binding, error) {
	var b binding
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var lease, next *time.Time
		var verify bool
		if err := tx.QueryRowForUpdate(ctx, "creative_generation_bindings", "request_id,epoch,failures,recovery_until,lease_until,next_step_at,verification_required", "execution_id=$2 AND canvas_id=$3", target.ExecutionID, target.CanvasID).Scan(&b.request, &b.epoch, &b.failures, &b.until, &lease, &next, &verify); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if verify {
			// 旧版本把收尾失败的生成停在 reconciling 且不再调度；重新领取一次，由 step 收为失败。
			stuck, err := tx.Exists(ctx, "creative_node_executions", "id=$2 AND state='reconciling'", target.ExecutionID)
			if err != nil {
				return err
			}
			if !stuck {
				return gw.ErrState
			}
		} else if next == nil {
			return gw.ErrState
		}
		if lease != nil && now.Before(*lease) || next != nil && now.Before(*next) {
			return ErrBusy
		}
		b.epoch++
		_, err = tx.Update(ctx, "creative_generation_bindings", "epoch=$3,lease_until=$4", "execution_id=$2", target.ExecutionID, b.epoch, now.Add(stepLease))
		return err
	})
	return b, err
}
func checkEpoch(ctx context.Context, tx store.TxAccountScope, target creativecanvas.ExecutionTarget, b binding) error {
	var epoch int64
	var lease *time.Time
	if err := tx.QueryRowForUpdate(ctx, "creative_generation_bindings", "epoch,lease_until", "execution_id=$2 AND canvas_id=$3", target.ExecutionID, target.CanvasID).Scan(&epoch, &lease); err != nil {
		return err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return err
	}
	if epoch != b.epoch || lease == nil || !now.Before(*lease) {
		return ErrBusy
	}
	return nil
}
func (s *Service) Work(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	var target creativecanvas.ExecutionTarget
	if err := creativeops.Decode(job.Payload, &target); err != nil {
		return err
	}
	b, err := s.claim(ctx, scope, target)
	if errors.Is(err, gw.ErrState) {
		return nil
	}
	if errors.Is(err, ErrBusy) {
		return jobs.Defer(30 * time.Second)
	}
	if err != nil {
		return err
	}
	// 领到任务即进入生成中；锁顺序与收尾一致（账号写锁 → 绑定 → 画布）。
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(ctx); err != nil {
			return err
		}
		if err := checkEpoch(ctx, tx, target, b); err != nil {
			return err
		}
		return creativecanvas.StartGenerationInTx(ctx, tx, target)
	})
	if err != nil {
		return err
	}
	// 取件可能较慢，给持久收尾留出独立时间；lease晚于网络截止。
	stepCtx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	done, stepErr := s.step(stepCtx, scope, target, b)
	if stepErr != nil {
		done = false
		s.gateway.Logger().WarnContext(ctx, "creative generation step failed", "execution_id", target.ExecutionID, "canvas_id", target.CanvasID, "request_id", b.request, "consecutive_failures", b.failures+1, "error", stepErr.Error())
	}
	cancel()
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer finishCancel()
	// 只数连续失败：任务跑得久不能让零星抖动累积成停驻。
	failures := 0
	if stepErr != nil {
		failures = b.failures + 1
	}
	parked := !done && (failures >= 5 || time.Now().After(b.until))
	// 达到重试边界时节点收为失败；已交接的作品仍按部分成功发布。
	if parked {
		// 先撤回未派发请求资格；已派发/unknown保留原费用恢复，绝不伪装零费。
		err = scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
			if _, err := s.generation.StopUndispatchedInTx(finishCtx, tx, b.request); err != nil {
				return err
			}
			return checkEpoch(finishCtx, tx, target, b)
		})
		if err != nil {
			return err
		}
		s.gateway.Logger().WarnContext(ctx, "creative generation parked", "execution_id", target.ExecutionID, "canvas_id", target.CanvasID, "request_id", b.request, "consecutive_failures", failures, "lease_expired", time.Now().After(b.until), "reason", creativecanvas.GenerationFailureGeneric)
		// 收尾失败不留无人推进的状态：保持调度，下一轮再收。
		done = s.complete(finishCtx, scope, target, b, true, creativecanvas.GenerationFailureGeneric) == nil
	}
	err = scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeWrite(finishCtx); err != nil {
			return err
		}
		if err := checkEpoch(finishCtx, tx, target, b); err != nil {
			return err
		}
		var next *time.Time
		if !done {
			v := time.Now().Add(30 * time.Second)
			next = &v
		}
		_, err := tx.Update(finishCtx, "creative_generation_bindings", "lease_until=NULL,next_step_at=$3,failures=$4,verification_required=FALSE", "execution_id=$2", target.ExecutionID, next, min(failures, 5))
		return err
	})
	if err != nil {
		return err
	}
	if !done {
		return jobs.Defer(30 * time.Second)
	}
	return nil
}
func (s *Service) step(ctx context.Context, scope store.AccountScope, target creativecanvas.ExecutionTarget, b binding) (bool, error) {
	e, err := creativecanvas.GetNodeExecution(ctx, scope, target)
	if err != nil && !errors.Is(err, store.ErrCreativeAccessDenied) {
		return false, err
	}
	expired := !e.Deadline.After(time.Now())
	if err != nil || e.State == "cancelled" || expired {
		if err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			_, err := s.generation.CancelRouteInTx(ctx, tx, b.request)
			return err
		}); err != nil {
			return false, err
		}
	}
	progress, err := s.generation.AdvanceGeneration(ctx, scope, b.request)
	if err != nil {
		return false, err
	}
	// 网关判为待核实后不会自动推进，节点直接收为失败；费用核实仍由网关维护。
	if progress.ResultVerificationRequired || progress.VerificationRequired && !progress.Request.Terminal() {
		s.logNodeFailure(ctx, target, b, progress, creativecanvas.GenerationFailureUnverified)
		return true, s.complete(ctx, scope, target, b, true, creativecanvas.GenerationFailureUnverified)
	}
	outputs, err := s.generation.ImportableOutputs(ctx, scope, b.request, caller, target.ExecutionID)
	if err != nil {
		return false, err
	}
	if len(outputs) == 0 {
		reason := failureReason(progress.Request.FailureClass)
		if expired {
			reason = creativecanvas.GenerationFailureTimeout
		}
		if !progress.Terminal() {
			// 过了截止的结果不再采用，节点不必陪供应商等下去；网关自行跑完并核算。
			if expired {
				s.logNodeFailure(ctx, target, b, progress, reason)
				return true, s.complete(ctx, scope, target, b, true, reason)
			}
			return false, nil
		}
		if progress.Request.State != gw.StateSucceeded {
			s.logNodeFailure(ctx, target, b, progress, reason)
		}
		// 已经交接或终态无作品：都不再生成。费用核实仍由G3维护。
		return true, s.complete(ctx, scope, target, b, progress.Request.State != gw.StateSucceeded, reason)
	}
	// 失败时轮换下一个未交接输出，单个坏结果不挡住其余作品。
	item := outputs[b.failures%len(outputs)]
	var media creativemedia.PreparedMediaImport
	if item.Output.Media != nil {
		fetcher := s.fetchers[item.DeploymentID]
		if fetcher == nil {
			return false, gw.ErrUnavailable
		}
		m := item.Output.Media
		media, err = s.media.PrepareGeneratedMedia(ctx, scope, item.RequestID+"/"+item.AttemptID+"/"+item.Output.ID, string(item.Output.Kind), m.MIME, m.ByteSize, !m.SizeIsLimit, func(ctx context.Context) (io.ReadCloser, error) {
			if !m.ExpiresAt.After(time.Now()) {
				refreshed, err := s.generation.RefreshImportLocation(ctx, scope, item)
				if err != nil {
					return nil, err
				}
				return fetcher.Open(ctx, refreshed.Output.Media.FetchURL)
			}
			return fetcher.Open(ctx, m.FetchURL)
		})
		if err != nil {
			return false, err
		}
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		return s.generation.HandoffOutputInTx(ctx, tx, item, caller, target.ExecutionID, func() (string, error) {
			if err := checkEpoch(ctx, tx, target, b); err != nil {
				return "", err
			}
			if _, err := creativelibrary.LockLibraryInTx(ctx, tx); err != nil {
				return "", err
			}
			// 先取得画布/执行锁，随后创建修订；Retain不在内容锁之后反向取画布。
			if err := creativecanvas.LockGenerationHandoffInTx(ctx, tx, target); err != nil {
				return "", err
			}
			retain := func(r creativecontent.Revision) error {
				return creativecanvas.RetainGenerationOutputInTx(ctx, tx, target, item.Output.Index, r)
			}
			var revision creativecontent.Revision
			var err error
			if item.Output.Media != nil {
				revision, err = s.media.CommitGeneratedMediaInTx(ctx, tx, media, retain)
			} else {
				body := item.Output.Text
				revision, err = creativecontent.WriteAndRetain(ctx, tx, creativecontent.Draft{Kind: "text", Payload: creativecontent.Payload{Body: &body}}, "", nil, retain)
			}
			return revision.ID, err
		})
	})
	return false, err
}

// StopUndispatched 终止还没发给供应商的生成：网关请求仍在排队时关闭它、退回预留，
// 节点回到生成前。已发出的照常完成、采用并计费（owner 2026-10-14）。与派发并发时
// 谁先锁到请求行谁说了算。返回是否终止了；执行不存在或已发出时为 false。
func (s *Service) StopUndispatched(ctx context.Context, scope store.AccountScope, target creativecanvas.ExecutionTarget) (bool, error) {
	stopped := false
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var request string
		err := tx.QueryRow(ctx, "creative_generation_bindings", "request_id", "execution_id=$2 AND canvas_id=$3", target.ExecutionID, target.CanvasID).Scan(&request)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// 锁顺序与派发、收尾一致：账号写锁 → 额度 → 请求 → 画布。
		if stopped, err = s.generation.StopUndispatchedInTx(ctx, tx, request); err != nil || !stopped {
			return err
		}
		return creativecanvas.CancelUndispatchedGenerationInTx(ctx, tx, target)
	})
	return stopped, err
}

func (s *Service) complete(ctx context.Context, scope store.AccountScope, target creativecanvas.ExecutionTarget, b binding, partial bool, reason string) error {
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if _, err := s.generation.LockPublicationInTx(ctx, tx, b.request); err != nil {
			return err
		}
		if err := checkEpoch(ctx, tx, target, b); err != nil {
			return err
		}
		if err := creativecanvas.CompleteGenerationInTx(ctx, tx, target, partial, reason); err != nil {
			return err
		}
		// 网关请求未到终态时没有可释放的空结果；节点照常收尾，后续结果由网关保留与核算。
		err := s.generation.ReleaseEmptyGenerationInTx(ctx, tx, b.request, caller, target.ExecutionID)
		if partial && errors.Is(err, gw.ErrState) {
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	_, err = creativecanvas.PublishGenerationExecution(ctx, scope, target, func(ctx context.Context, tx store.TxAccountScope) (bool, error) {
		allowed, err := s.generation.LockPublicationInTx(ctx, tx, b.request)
		if err != nil {
			return false, err
		}
		if err = checkEpoch(ctx, tx, target, b); err != nil {
			return false, err
		}
		if err = tx.RequireCreativeCapability(ctx, "generation_apply"); errors.Is(err, store.ErrCreativeAccessDenied) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return allowed, nil
	})
	return err
}

// logNodeFailure 把节点失败与网关请求串起来：节点只显示收敛后的原因码，
// 具体的供应商/未派发原因按 current_request_id 去网关日志 "generation provider outcome" 查。
func (s *Service) logNodeFailure(ctx context.Context, target creativecanvas.ExecutionTarget, b binding, progress gw.GenerationProgress, reason string) {
	s.gateway.Logger().WarnContext(ctx, "creative generation node failed",
		"execution_id", target.ExecutionID,
		"canvas_id", target.CanvasID,
		"request_id", b.request,
		"current_request_id", progress.Request.ID,
		"request_state", string(progress.Request.State),
		"failure_class", progress.Request.FailureClass,
		"verification_required", progress.VerificationRequired,
		"result_verification_required", progress.ResultVerificationRequired,
		"reason", reason,
	)
}

// failureReason 把网关记录的供应商失败分类收敛为节点可展示的原因。
// 额度、凭证、限流、部署不可用等平台侧问题统一归为通用失败。
func failureReason(class string) string {
	switch class {
	case "content_rejected", "content_policy_violation":
		return creativecanvas.GenerationFailureContentRejected
	case "invalid_parameter":
		return creativecanvas.GenerationFailureInvalidInput
	case "execution_timeout", "generation_timeout":
		return creativecanvas.GenerationFailureTimeout
	default:
		return creativecanvas.GenerationFailureGeneric
	}
}
