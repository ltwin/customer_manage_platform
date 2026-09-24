package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

var errGenerationBusy = errors.New("generation stage already owned")

type generationTask struct {
	requestID, attemptID, deploymentID, adapterVersion, limitKey string
	taskCipher                                                   []byte
	taskDigest                                                   *string
	state                                                        GenerationObservationState
	providerRevision                                             *string
	epoch                                                        int64
	leaseUntil                                                   *time.Time
	action                                                       string
	failures                                                     int
	cancelSent                                                   bool
	recoveryUntil                                                time.Time
}

const generationTaskColumns = "request_id,attempt_id,deployment_id,adapter_version,limit_key,task_cipher,task_digest,state,provider_revision,epoch,lease_until,action,failures,cancel_sent,recovery_until"

func scanGenerationTask(row store.Row) (generationTask, error) {
	var t generationTask
	err := row.Scan(&t.requestID, &t.attemptID, &t.deploymentID, &t.adapterVersion, &t.limitKey, &t.taskCipher, &t.taskDigest, &t.state, &t.providerRevision, &t.epoch, &t.leaseUntil, &t.action, &t.failures, &t.cancelSent, &t.recoveryUntil)
	if errors.Is(err, store.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// GenerationClaim 标识一次已持久领取的有界阶段；旧epoch可留存证据，不能推进结果。
type GenerationClaim struct {
	RequestID string
	AttemptID string
	Epoch     int64
}
type generationAction struct {
	claim   GenerationClaim
	kind    string
	call    GenerationCall
	adapter GenerationAdapter
}

// AdvanceGeneration 每次至多调用一个Submit/Observe/Cancel/Lookup；恢复只从数据库
// 重建身份。提交许可在同一事务内登记为已领取，进程丢失后不存在再次消费的路径。
func (r *GenerationRuntime) AdvanceGeneration(ctx context.Context, scope store.AccountScope, requestID string) (GenerationProgress, error) {
	var action *generationAction
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		action, err = r.claimGeneration(ctx, tx, requestID)
		return err
	})
	if errors.Is(err, errGenerationBusy) {
		err = nil
	}
	if err != nil {
		return GenerationProgress{}, err
	}
	if action != nil {
		callCtx, cancel := context.WithTimeout(ctx, transportLifetime)
		var event GenerationEvent
		var callErr error
		switch action.kind {
		case "submit":
			event, callErr = action.adapter.Submit(callCtx, action.call)
		case "observe":
			event, callErr = action.adapter.Observe(callCtx, action.call)
		case "cancel":
			event, callErr = action.adapter.Cancel(callCtx, action.call)
		case "lookup":
			lookup, ok := action.adapter.(GenerationSubmissionLookup)
			if !ok {
				callErr = ErrUnavailable
			} else {
				event, callErr = lookup.LookupSubmission(callCtx, action.call)
			}
		}
		cancel()
		finishCtx, finish := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
		defer finish()
		err = scope.WithTxScope(finishCtx, func(tx store.TxAccountScope) error {
			if callErr != nil {
				return r.failGenerationStage(finishCtx, tx, action.claim)
			}
			return r.RecordGenerationObservationInTx(finishCtx, tx, action.claim, event)
		})
		if err != nil {
			return GenerationProgress{}, err
		}
	}
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	return r.GetGeneration(readCtx, scope, requestID)
}

func (r *GenerationRuntime) claimGeneration(ctx context.Context, tx store.TxAccountScope, requestID string) (*generationAction, error) {
	s := r.gateway
	now := s.now().UTC()
	peek, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return nil, err
	}
	if peek.kind != requestKindGeneration {
		return nil, ErrState
	}
	var control int64
	var next *time.Time
	var verify bool
	if err := tx.QueryRow(ctx, "llm_generation_requests", "control_revision,next_advance_at,verification_required", "request_id=$2", requestID).Scan(&control, &next, &verify); err != nil {
		return nil, err
	}
	if control == 0 {
		return nil, nil
	}
	fresh := peek.state == StatePrepared
	if fresh {
		active, err := tx.LockGenerationAccount(ctx)
		if err != nil {
			return nil, err
		}
		// 未提交的截止/取消/账号停用收尾不需要供应商名额，即使名额被未知任务
		// 占满也能释放未使用的预算。当前请求状态在预算/请求锁内再次核对。
		if !active || !peek.deadline.After(now) || peek.cancelAt != nil || peek.closedAt != nil {
			return nil, r.cancelUnsubmittedGeneration(ctx, tx, requestID)
		}
	}
	if next == nil || verify || next.After(now) {
		return nil, nil
	}
	adm, err := generationAdmissionOf(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	prepared, err := RestorePolicyGeneration(adm.snapshot, adm.fingerprint)
	if err != nil {
		return nil, err
	}
	def := prepared.PolicyDefinition()
	reg, registered := r.adapters[def.DeploymentID+"/"+def.AdapterVersion]
	if fresh {
		// 只用无锁预读选择禁止派发的分支，进入部署门后再次确认；恢复启用则
		// 本轮退回，由下一轮按名额优先的正常锁序重新判断。
		if err := r.checkDeployment(ctx, tx, def, control); err != nil {
			if !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrConflict) {
				return nil, err
			}
			if err := tx.LockGenerationDeploymentGate(ctx, adm.deploymentID); err != nil {
				return nil, err
			}
			if err := r.checkDeployment(ctx, tx, def, control); err == nil {
				return nil, errGenerationBusy
			} else if !errors.Is(err, ErrUnavailable) && !errors.Is(err, ErrConflict) {
				return nil, err
			}
			return nil, r.cancelUnsubmittedGeneration(ctx, tx, requestID)
		}
	}
	// 固定锁序：账号状态（仅提交）→供应商名额→组→部署门→预算桶→请求→task。
	var task generationTask
	if !fresh {
		task, err = scanGenerationTask(tx.QueryRow(ctx, "llm_generation_tasks", generationTaskColumns, "request_id=$2 AND attempt_id IN (SELECT id FROM llm_attempts WHERE account_id=$1 AND request_id=$2 AND attempt_number=$3)", requestID, peek.attemptsUsed))
		if err != nil {
			return nil, err
		}
		if err := tx.LLMLimitView().Lock(ctx, task.limitKey); err != nil {
			return nil, err
		}
	} else {
		if !registered {
			if err := tx.LockGenerationDeploymentGate(ctx, adm.deploymentID); err != nil {
				return nil, err
			}
			if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
				return nil, err
			}
			if _, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID)); err != nil {
				return nil, err
			}
			return nil, r.markGenerationVerification(ctx, tx, requestID, "adapter_version_unavailable", now)
		}
		granted, err := tx.LLMLimitView().Acquire(ctx, reg.LimitKey, reg.Concurrency, reg.RateLimit, 60, now)
		if err != nil {
			return nil, err
		}
		if !granted {
			return nil, ErrRateLimited
		}
		var groupID string
		if err := tx.QueryRowForUpdate(ctx, "llm_call_groups", "caller_group_id", "caller_service=$2 AND caller_group_id=$3", peek.callerService, peek.groupID).Scan(&groupID); err != nil {
			return nil, err
		}
	}
	if err := tx.LockGenerationDeploymentGate(ctx, adm.deploymentID); err != nil {
		return nil, err
	}
	if err := s.lockBudgetForRequest(ctx, tx, requestID, now); err != nil {
		return nil, err
	}
	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return nil, err
	}
	// 无锁预读只决定锁资源，不授权操作；串行点后复核全部可变事实。
	if request.state != peek.state || request.attemptsUsed != peek.attemptsUsed {
		return nil, errGenerationBusy
	}
	if err := tx.QueryRow(ctx, "llm_generation_requests", "control_revision,next_advance_at,verification_required", "request_id=$2", requestID).Scan(&control, &next, &verify); err != nil {
		return nil, err
	}
	if next == nil || verify || next.After(now) {
		return nil, errGenerationBusy
	}
	if fresh {
		return r.beginGeneration(ctx, tx, request, prepared, reg, control, now)
	}
	task, err = scanGenerationTask(tx.QueryRowForUpdate(ctx, "llm_generation_tasks", generationTaskColumns, "attempt_id=$2", task.attemptID))
	if err != nil {
		return nil, err
	}
	if task.leaseUntil != nil && task.leaseUntil.After(now) {
		return nil, errGenerationBusy
	}
	if !registered || task.failures >= 5 || !task.recoveryUntil.After(now) {
		return nil, r.markGenerationVerification(ctx, tx, requestID, "recovery_requires_verification", now)
	}
	action := "observe"
	taskID := ""
	if len(task.taskCipher) == 0 {
		if _, ok := reg.Adapter.(GenerationSubmissionLookup); !ok {
			return nil, r.markGenerationVerification(ctx, tx, requestID, "submission_outcome_unknown", now)
		}
		action = "lookup"
	} else {
		raw, err := r.open(tx.AccountID(), task.attemptID, "task", task.taskCipher)
		if err != nil {
			return nil, r.markGenerationVerification(ctx, tx, requestID, "protected_identity_unavailable", now)
		}
		taskID = string(raw)
		if (request.cancelAt != nil || !request.deadline.After(now)) && !generationTerminal(task.state) && !task.cancelSent {
			action = "cancel"
		}
	}
	if !request.deadline.After(now) && request.cancelAt == nil {
		if _, err := tx.Update(ctx, "llm_requests", "cancel_requested_at=$3,closed_at=$3,revision=revision+1", "id=$2", requestID, now); err != nil {
			return nil, err
		}
	}
	epoch := task.epoch + 1
	until := now.Add(generationLease)
	if _, err := tx.Update(ctx, "llm_generation_tasks", "epoch=$3,lease_until=$4,action=$5,failures=failures+1,cancel_sent=cancel_sent OR $6", "attempt_id=$2", task.attemptID, epoch, until, action, action == "cancel"); err != nil {
		return nil, err
	}
	if _, err := tx.Update(ctx, "llm_generation_requests", "next_advance_at=$3", "request_id=$2", requestID, until); err != nil {
		return nil, err
	}
	return &generationAction{claim: GenerationClaim{RequestID: requestID, AttemptID: task.attemptID, Epoch: epoch}, kind: action, call: GenerationCall{RequestID: requestID, AttemptID: task.attemptID, TaskID: taskID, Prepared: prepared}, adapter: reg.Adapter}, nil
}

func (r *GenerationRuntime) beginGeneration(ctx context.Context, tx store.TxAccountScope, request requestRow, p PreparedPolicyGeneration, reg GenerationAdapterRegistration, control int64, now time.Time) (*generationAction, error) {
	s := r.gateway
	guardErr := r.checkDeployment(ctx, tx, p.PolicyDefinition(), control)
	if request.cancelAt != nil || request.closedAt != nil {
		guardErr = ErrCancelled
	} else if !request.deadline.After(now) {
		guardErr = ErrDeadline
	}
	if guardErr == nil {
		guardErr = r.guard(ctx, tx, request.view(SettlementReserved), p)
	}
	if guardErr != nil {
		if !errors.Is(guardErr, ErrCancelled) && !errors.Is(guardErr, ErrDeadline) && !errors.Is(guardErr, ErrUnavailable) && !errors.Is(guardErr, ErrConflict) && !errors.Is(guardErr, ErrCapability) {
			return nil, guardErr
		}
		if _, err := tx.Update(ctx, "llm_requests", "state='cancelled',cancel_requested_at=$3,closed_at=$3,failure_class='generation_guard_rejected',revision=revision+1,updated_at=$3", "id=$2", request.id, now); err != nil {
			return nil, err
		}
		if err := s.releaseReservation(ctx, tx, request.id, now); err != nil {
			return nil, err
		}
		if err := tx.LLMLimitView().Release(ctx, reg.LimitKey); err != nil {
			return nil, err
		}
		_, err := tx.Update(ctx, "llm_generation_requests", "next_advance_at=NULL", "request_id=$2", request.id)
		return nil, err
	}
	reservation, err := scanReservation(tx.QueryRowForUpdate(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", request.id))
	if err != nil {
		return nil, err
	}
	if request.retryEligible {
		if _, err := s.RearmInTx(ctx, tx, request.id, reservation.generation, 0, 0); err != nil {
			return nil, err
		}
		reservation, err = scanReservation(tx.QueryRow(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", request.id))
		if err != nil {
			return nil, err
		}
	}
	if request.attemptsUsed >= maxAttempts {
		return nil, ErrAttemptsUsed
	}
	if reservation.state != SettlementReserved || reservation.holdMicros != reservation.initialMicros {
		return nil, ErrBudget
	}
	if err := s.assertNoLiveAttempt(ctx, tx, request.id); err != nil {
		return nil, err
	}
	token, err := randomToken()
	if err != nil {
		return nil, err
	}
	attempt := newID("llma")
	until := now.Add(generationLease)
	if err := tx.Insert(ctx, "llm_attempts", []string{"id", "request_id", "attempt_number", "reservation_id", "reservation_generation", "dispatch_state", "dispatch_epoch", "permit_digest", "limit_key", "dispatched_at", "permit_claimed_at"}, attempt, request.id, request.attemptsUsed+1, reservation.id, reservation.generation, "dispatching", reservation.generation, digest([]byte(token)), reg.LimitKey, now, now); err != nil {
		return nil, err
	}
	if err := tx.Insert(ctx, "llm_generation_tasks", []string{"request_id", "attempt_id", "deployment_id", "adapter_version", "limit_key", "lease_until", "action", "failures", "created_at", "recovery_until"}, request.id, attempt, reg.DeploymentID, reg.Version, reg.LimitKey, until, "submit", 1, now, now.Add(generationRecoveryWindow)); err != nil {
		return nil, err
	}
	if _, err := tx.Update(ctx, "llm_requests", "state='dispatching',attempts_used=attempts_used+1,retry_eligible=FALSE,revision=revision+1,updated_at=$3", "id=$2", request.id, now); err != nil {
		return nil, err
	}
	if _, err := tx.Update(ctx, "llm_generation_requests", "next_advance_at=$3", "request_id=$2", request.id, until); err != nil {
		return nil, err
	}
	return &generationAction{claim: GenerationClaim{RequestID: request.id, AttemptID: attempt, Epoch: 1}, kind: "submit", call: GenerationCall{RequestID: request.id, AttemptID: attempt, Prepared: p}, adapter: reg.Adapter}, nil
}

// lockObservation 延续名额→部署门→预算→请求→task，核验attempt归属之后才写任何费用。
func (r *GenerationRuntime) lockObservation(ctx context.Context, tx store.TxAccountScope, c GenerationClaim) (generationTask, requestRow, error) {
	task, err := scanGenerationTask(tx.QueryRow(ctx, "llm_generation_tasks", generationTaskColumns, "attempt_id=$2 AND request_id=$3", c.AttemptID, c.RequestID))
	if err != nil {
		return task, requestRow{}, err
	}
	if err := tx.LLMLimitView().Lock(ctx, task.limitKey); err != nil {
		return task, requestRow{}, err
	}
	if err := tx.LockGenerationDeploymentGate(ctx, task.deploymentID); err != nil {
		return task, requestRow{}, err
	}
	if err := r.gateway.lockBudgetForRequest(ctx, tx, c.RequestID, r.gateway.now()); err != nil {
		return task, requestRow{}, err
	}
	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", c.RequestID))
	if err != nil {
		return task, request, err
	}
	task, err = scanGenerationTask(tx.QueryRowForUpdate(ctx, "llm_generation_tasks", generationTaskColumns, "attempt_id=$2", c.AttemptID))
	return task, request, err
}
func (r *GenerationRuntime) failGenerationStage(ctx context.Context, tx store.TxAccountScope, c GenerationClaim) error {
	task, _, err := r.lockObservation(ctx, tx, c)
	if err != nil {
		return err
	}
	if task.epoch != c.Epoch {
		return nil
	}
	now := r.gateway.now().UTC()
	if _, err := tx.Update(ctx, "llm_generation_tasks", "lease_until=NULL", "attempt_id=$2", c.AttemptID); err != nil {
		return err
	}
	if task.action == "submit" {
		if _, err := tx.Update(ctx, "llm_requests", "state='unknown',failure_class='submission_outcome_unknown',revision=revision+1,updated_at=$3", "id=$2", c.RequestID, now); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "llm_attempts", "dispatch_state='unknown'", "id=$2", c.AttemptID); err != nil {
			return err
		}
	}
	reg := r.adapters[task.deploymentID+"/"+task.adapterVersion]
	_, canLookup := reg.Adapter.(GenerationSubmissionLookup)
	if task.failures >= 5 || len(task.taskCipher) == 0 && !canLookup {
		return r.markGenerationVerification(ctx, tx, c.RequestID, "recovery_requires_verification", now)
	}
	_, err = tx.Update(ctx, "llm_generation_requests", "next_advance_at=$3", "request_id=$2", c.RequestID, now.Add(generationPoll*time.Duration(task.failures)))
	return err
}
func (r *GenerationRuntime) markGenerationVerification(ctx context.Context, tx store.TxAccountScope, id, reason string, now time.Time) error {
	if _, err := tx.Update(ctx, "llm_generation_requests", "verification_required=TRUE,next_advance_at=(SELECT CASE WHEN state='prepared' THEN deadline_at ELSE NULL END FROM llm_requests WHERE account_id=$1 AND id=$2)", "request_id=$2", id); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "llm_requests", "state=CASE WHEN state IN ('dispatching','streaming') THEN 'unknown' ELSE state END,failure_class=$3,revision=revision+1,updated_at=$4", "id=$2", id, reason, now); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "llm_attempts", "dispatch_state='unknown'", "request_id=$2 AND dispatch_state IN ('dispatching','streaming')", id); err != nil {
		return err
	}
	return r.gateway.refreshSettlement(ctx, tx, id, now)
}
func generationTerminal(s GenerationObservationState) bool {
	return s == GenerationCompleted || s == GenerationRejected || s == GenerationFailed || s == GenerationCancelled
}

func (r *GenerationRuntime) CancelGenerationInTx(ctx context.Context, tx store.TxAccountScope, requestID string) (RequestView, error) {
	var kind string
	if err := tx.QueryRow(ctx, "llm_requests", "request_kind", "id=$2", requestID).Scan(&kind); err != nil {
		return RequestView{}, err
	}
	if kind != requestKindGeneration {
		return RequestView{}, fmt.Errorf("%w: generation request required", ErrState)
	}
	return r.gateway.CancelInTx(ctx, tx, requestID)
}

// cancelUnsubmittedGeneration 只关闭仍未派发的请求；并发先取得许可时本轮不
// 改动它的费用或名额，由正常观察/取消流程处理。
func (r *GenerationRuntime) cancelUnsubmittedGeneration(ctx context.Context, tx store.TxAccountScope, requestID string) error {
	if err := r.gateway.lockBudgetForRequest(ctx, tx, requestID, r.gateway.now()); err != nil {
		return err
	}
	row, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", requestID))
	if err != nil {
		return err
	}
	if row.state != StatePrepared {
		return errGenerationBusy
	}
	_, err = r.gateway.CancelInTx(ctx, tx, requestID)
	return err
}
