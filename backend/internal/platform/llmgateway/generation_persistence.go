package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// protectedGenerationEvent 显式保存普通JSON刻意省略的保护字段，整个信封加密入库。
type protectedGenerationEvent struct {
	Event     GenerationEvent   `json:"event"`
	TaskID    string            `json:"task_id,omitempty"`
	Locations map[string]string `json:"locations,omitempty"`
}

func encodeGenerationEvent(e GenerationEvent) ([]byte, error) {
	v := protectedGenerationEvent{Event: e, TaskID: e.Observation.TaskID, Locations: make(map[string]string)}
	for _, o := range e.Observation.Outputs {
		if o.Media != nil {
			v.Locations[o.ID] = o.Media.FetchURL
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(raw) > 900<<10 {
		return nil, fmt.Errorf("%w: generation evidence size", ErrProtocol)
	}
	return raw, nil
}

// RecordGenerationObservationInTx 归属、去重、顺序与epoch全部在请求锁内判断。
// 旧epoch只能留存证据和核算，绝不修改当前task、结果或发布资格。
func (r *GenerationRuntime) RecordGenerationObservationInTx(ctx context.Context, tx store.TxAccountScope, c GenerationClaim, e GenerationEvent) error {
	task, request, err := r.lockObservation(ctx, tx, c)
	if err != nil {
		return err
	}
	if c.Epoch < 1 || c.Epoch > task.epoch {
		return ErrConflict
	}
	adm, err := generationAdmissionOf(ctx, tx, c.RequestID)
	if err != nil {
		return err
	}
	prepared, err := RestorePolicyGeneration(adm.snapshot, adm.fingerprint)
	if err != nil {
		return err
	}
	if err := e.Observation.Validate(prepared.Generation()); err != nil {
		return err
	}
	if e.EventKey == "" || len(e.EventKey) > maxGenerationEventKeyLen || len(e.ProviderRevision) > 64 || len(e.Usage) > 64 {
		return ErrProtocol
	}
	raw, err := encodeGenerationEvent(e)
	if err != nil {
		return err
	}
	hash := digest(raw)
	now := r.gateway.now().UTC()
	current := c.Epoch == task.epoch
	var attemptNo int
	if err := tx.QueryRow(ctx, "llm_attempts", "attempt_number", "id=$2 AND request_id=$3", c.AttemptID, c.RequestID).Scan(&attemptNo); err != nil {
		return err
	}
	current = current && attemptNo == request.attemptsUsed
	rows, err := tx.Query(ctx, "llm_generation_observations", "evidence_digest,disposition,claim_epoch", "attempt_id=$2 AND event_key=$3", c.AttemptID, e.EventKey)
	if err != nil {
		return err
	}
	duplicate, conflict, olderClaim, recordedConflict := false, false, false, false
	for rows.Next() {
		var prior, state string
		var epoch int64
		if err := rows.Scan(&prior, &state, &epoch); err != nil {
			rows.Close()
			return err
		}
		if prior == hash {
			duplicate = true
			olderClaim = state == "stale" && epoch < c.Epoch
			recordedConflict = state == "conflict"
		} else {
			conflict = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// 旧worker先写入的同一证据可由新租约重新核实后推进；去重不能把有效完成
	// 永久钉在stale。账本仍由事件身份保证只记一次。
	if duplicate && (!current || !olderClaim || conflict) {
		if current && !conflict && !recordedConflict {
			return r.finishGenerationStage(ctx, tx, task, now)
		}
		return nil
	}
	disposition := "pending"
	if !current {
		disposition = "stale"
	}
	if task.taskDigest != nil && e.Observation.TaskID != "" && *task.taskDigest != generationTaskDigest(tx.AccountID(), task.deploymentID, e.Observation.TaskID) {
		conflict = true
	}
	if task.taskDigest != nil && e.Observation.State == GenerationRejected {
		conflict = true
	}
	if e.Observation.State == GenerationRejected {
		paid, err := tx.Exists(ctx, "llm_cost_positions", "attempt_id=$2 AND cost_component='generation_total' AND booked_cost_micros>0", c.AttemptID)
		if err != nil {
			return err
		}
		conflict = conflict || paid
		if len(e.Usage) > 0 {
			// rejected只能证明明确未受理且零费用。事件本身带正价、非法或缺失
			// 维度的用量时也是矛盾证据，不能先写零费再授权第二次Submit。
			price, err := prepared.PriceUsage(e.Usage)
			conflict = conflict || err != nil || price.TotalMicros != 0
			for _, value := range e.Usage {
				conflict = conflict || value != 0
			}
		}
	}
	if task.taskDigest == nil && e.Observation.TaskID != "" {
		bound, err := tx.Exists(ctx, "llm_generation_tasks", "deployment_id=$2 AND task_digest=$3 AND attempt_id<>$4", task.deploymentID, generationTaskDigest(tx.AccountID(), task.deploymentID, e.Observation.TaskID), c.AttemptID)
		if err != nil {
			return err
		}
		conflict = conflict || bound
	}
	if current && !conflict {
		switch {
		case generationTerminal(task.state) && generationTerminal(e.Observation.State) && task.state != e.Observation.State:
			conflict = true
		case generationTerminal(task.state) && !generationTerminal(e.Observation.State):
			disposition = "stale"
		case task.providerRevision != nil && e.ProviderRevision != "" && !revisionAdvances(task.providerRevision, e.ProviderRevision):
			disposition = "stale"
		case task.state == GenerationRunning && e.Observation.State == GenerationAccepted:
			disposition = "stale"
		}
		if e.Observation.State == GenerationCompleted {
			compatible, err := r.compatibleGenerationOutputs(ctx, tx, c.AttemptID, e.Observation.Outputs)
			if err != nil {
				return err
			}
			conflict = conflict || !compatible
		}
	}
	if conflict {
		disposition = "conflict"
	}
	sealed, err := r.seal(tx.AccountID(), c.AttemptID, "event:"+e.EventKey+":"+hash, raw)
	if err != nil {
		return err
	}
	if duplicate {
		if _, err := tx.Update(ctx, "llm_generation_observations", "disposition=$5,claim_epoch=$6", "attempt_id=$2 AND event_key=$3 AND evidence_digest=$4", c.AttemptID, e.EventKey, hash, disposition, c.Epoch); err != nil {
			return err
		}
	} else {
		if err := tx.Insert(ctx, "llm_generation_observations", []string{"id", "attempt_id", "event_key", "evidence_digest", "evidence_cipher", "disposition", "claim_epoch", "created_at"}, newID("llmo"), c.AttemptID, e.EventKey, hash, sealed, disposition, c.Epoch, now); err != nil {
			return err
		}
	}
	if conflict {
		if current && task.state == GenerationRejected && request.state == StatePrepared {
			// 未受理结论受到反证后，不能继续当作尚未派发请求清理或重试。
			if _, err := tx.Update(ctx, "llm_requests", "state='unknown',retry_eligible=FALSE", "id=$2", c.RequestID); err != nil {
				return err
			}
		}
		// 未受理结论被反驳即失去零费依据；缺少用量也必须重开费用争议，
		// 不能让此前released的预留继续冒充没有风险敞口。
		if len(e.Usage) > 0 || task.state == GenerationRejected {
			if _, err := r.gateway.disputeGenerationUsage(ctx, tx, c.RequestID, c.AttemptID, e.EventKey, e.Usage, adm.currency, task.deploymentID, e.ProviderRevision, "conflicting generation observation", false, now); err != nil {
				return err
			}
		}
		if current {
			return r.markGenerationVerification(ctx, tx, c.RequestID, "conflicting_observation", now)
		}
		return nil
	}
	if disposition == "stale" {
		if err := r.recordGenerationEventUsage(ctx, tx, c, e, now); err != nil {
			return err
		}
		if current {
			return r.finishGenerationStage(ctx, tx, task, now)
		}
		return nil
	}
	if err := r.applyGenerationState(ctx, tx, &task, request, e, now); err != nil {
		return err
	}
	if err := r.recordGenerationEventUsage(ctx, tx, c, e, now); err != nil {
		return err
	}
	if e.Observation.State == GenerationRejected {
		retry := request.attemptsUsed < maxAttempts && request.cancelAt == nil && request.closedAt == nil && request.deadline.After(now)
		if err := r.gateway.releaseHoldForRetry(ctx, tx, c.RequestID, retry, now); err != nil {
			return err
		}
		state := StateFailed
		if retry {
			state = StatePrepared
		}
		if _, err := tx.Update(ctx, "llm_requests", "state=$3,retry_eligible=$4,failure_class='generation_rejected',revision=revision+1,updated_at=$5", "id=$2", c.RequestID, string(state), retry, now); err != nil {
			return err
		}
	}
	if _, err := tx.Update(ctx, "llm_generation_observations", "disposition='applied'", "attempt_id=$2 AND event_key=$3 AND evidence_digest=$4", c.AttemptID, e.EventKey, hash); err != nil {
		return err
	}
	return r.finishGenerationStage(ctx, tx, task, now)
}
func generationTaskDigest(account, deployment, task string) string {
	return digest([]byte(account + "\x00" + deployment + "\x00" + task))
}

func (r *GenerationRuntime) applyGenerationState(ctx context.Context, tx store.TxAccountScope, task *generationTask, request requestRow, e GenerationEvent, now time.Time) error {
	o := e.Observation
	if task.taskDigest == nil && o.TaskID != "" {
		sealed, err := r.seal(tx.AccountID(), task.attemptID, "task", []byte(o.TaskID))
		if err != nil {
			return err
		}
		hash := generationTaskDigest(tx.AccountID(), task.deploymentID, o.TaskID)
		if _, err := tx.Update(ctx, "llm_generation_tasks", "task_cipher=$3,task_digest=$4", "attempt_id=$2", task.attemptID, sealed, hash); err != nil {
			return err
		}
		task.taskCipher, task.taskDigest = sealed, &hash
	}
	task.state = o.State
	if e.ProviderRevision != "" {
		v := e.ProviderRevision
		task.providerRevision = &v
	}
	if _, err := tx.Update(ctx, "llm_generation_tasks", "state=$3,provider_revision=COALESCE($4,provider_revision),progress=$5", "attempt_id=$2", task.attemptID, string(o.State), nullableText(e.ProviderRevision), o.Progress); err != nil {
		return err
	}
	state, dispatch := StateStreaming, "streaming"
	switch o.State {
	case GenerationUnknown:
		state, dispatch = StateUnknown, "unknown"
	case GenerationRejected:
		state, dispatch = StateFailed, "rejected"
	case GenerationCompleted:
		state, dispatch = StateSucceeded, "succeeded"
	case GenerationFailed:
		state, dispatch = StateFailed, "succeeded"
	case GenerationCancelled:
		state, dispatch = StateCancelled, "succeeded"
	}
	if generationTerminal(o.State) {
		if _, err := tx.Update(ctx, "llm_attempts", "dispatch_state=$3,finished_at=$4,transport_finished_at=$4", "id=$2", task.attemptID, dispatch, now); err != nil {
			return err
		}
		if err := r.gateway.releasePermit(ctx, tx, func(tx store.TxAccountScope) LimitView { return tx.LLMLimitView() }, Permit{AttemptID: task.attemptID, LimitKey: task.limitKey}, now); err != nil {
			return err
		}
	} else {
		if _, err := tx.Update(ctx, "llm_attempts", "dispatch_state=$3", "id=$2", task.attemptID, dispatch); err != nil {
			return err
		}
	}
	if o.State == GenerationCompleted {
		for _, out := range o.Outputs {
			projection, err := json.Marshal(out)
			if err != nil {
				return err
			}
			var location []byte
			if out.Media != nil {
				location, err = r.seal(tx.AccountID(), task.attemptID, "output:"+out.ID, []byte(out.Media.FetchURL))
				if err != nil {
					return err
				}
			}
			exists, err := tx.Exists(ctx, "llm_generation_outputs", "attempt_id=$2 AND output_id=$3", task.attemptID, out.ID)
			if err != nil {
				return err
			}
			if exists {
				if _, err := tx.Update(ctx, "llm_generation_outputs", "projection=$4,location_cipher=$5", "attempt_id=$2 AND output_id=$3", task.attemptID, out.ID, projection, location); err != nil {
					return err
				}
			} else {
				if err := tx.Insert(ctx, "llm_generation_outputs", []string{"request_id", "attempt_id", "output_id", "ordinal", "kind", "projection", "location_cipher"}, task.requestID, task.attemptID, out.ID, out.Index, string(out.Kind), projection, location); err != nil {
					return err
				}
			}
		}
		raw, err := json.Marshal(o.Outputs)
		if err != nil {
			return err
		}
		if request.cancelAt != nil || request.closedAt != nil || !request.deadline.After(now) {
			state = StateCancelled
		}
		if _, err := tx.Update(ctx, "llm_requests", "result_hash=$3,result_revision=result_revision+1", "id=$2", task.requestID, digest(raw)); err != nil {
			return err
		}
	}
	_, err := tx.Update(ctx, "llm_requests", "state=$3,failure_class=$4,revision=revision+1,updated_at=$5", "id=$2", task.requestID, string(state), nullableText(o.ErrorCode), now)
	return err
}
func (r *GenerationRuntime) compatibleGenerationOutputs(ctx context.Context, tx store.TxAccountScope, attemptID string, outs []GenerationOutput) (bool, error) {
	rows, err := tx.QueryPage(ctx, "llm_generation_outputs", "projection", "attempt_id=$2", []store.OrderBy{{Column: "ordinal"}}, 64, 0, attemptID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return false, err
		}
		var prior GenerationOutput
		if err := json.Unmarshal(raw, &prior); err != nil {
			return false, err
		}
		if count >= len(outs) {
			return false, nil
		}
		next := outs[count]
		if prior.ID != next.ID || prior.Index != next.Index || prior.Kind != next.Kind || prior.Text != next.Text || (prior.Media == nil) != (next.Media == nil) {
			return false, nil
		}
		if prior.Media != nil && (prior.Media.MIME != next.Media.MIME || prior.Media.ByteSize != next.Media.ByteSize) {
			return false, nil
		}
		count++
	}
	return count == 0 || count == len(outs), rows.Err()
}
func (r *GenerationRuntime) recordGenerationEventUsage(ctx context.Context, tx store.TxAccountScope, c GenerationClaim, e GenerationEvent, now time.Time) error {
	if e.Observation.State == GenerationRejected {
		adm, err := generationAdmissionOf(ctx, tx, c.RequestID)
		if err != nil {
			return err
		}
		zero := int64(0)
		_, err = r.gateway.RecordMeasurementInTx(ctx, tx, c.RequestID, Measurement{AttemptID: c.AttemptID, Key: generationEvidenceKey("rejection", e.EventKey), Component: ComponentGenerationTotal, Currency: adm.currency, CostMicros: &zero, Certainty: "known", Kind: EvidenceVerifiedRejection, ProviderRev: e.ProviderRevision, EvidenceDigest: digest([]byte(e.EventKey))})
		return err
	}
	if len(e.Usage) == 0 {
		return r.gateway.refreshSettlement(ctx, tx, c.RequestID, now)
	}
	_, err := r.gateway.RecordGenerationUsageInTx(ctx, tx, c.RequestID, c.AttemptID, e.EventKey, e.Usage, e.ProviderRevision)
	if errors.Is(err, ErrGenerationFactsUnavailable) {
		return nil
	}
	if errors.Is(err, ErrValidation) || errors.Is(err, ErrProtocol) {
		adm, readErr := generationAdmissionOf(ctx, tx, c.RequestID)
		if readErr != nil {
			return readErr
		}
		_, disputeErr := r.gateway.disputeGenerationUsage(ctx, tx, c.RequestID, c.AttemptID, e.EventKey,
			e.Usage, adm.currency, adm.deploymentID, e.ProviderRevision, "generation usage cannot be priced", true, now)
		return disputeErr
	}
	return err
}
func (r *GenerationRuntime) finishGenerationStage(ctx context.Context, tx store.TxAccountScope, task generationTask, now time.Time) error {
	// 同一用量可能已由旧epoch登记，新的终态观察仍须重算hold；不能依赖
	// 记账入口在幂等返回时重新释放运行阶段保留。
	if err := r.gateway.refreshGenerationAfterEvidence(ctx, tx, task.requestID, now); err != nil {
		return err
	}
	if _, err := tx.Update(ctx, "llm_generation_tasks", "lease_until=NULL,failures=0", "attempt_id=$2", task.attemptID); err != nil {
		return err
	}
	var state string
	var retry bool
	if err := tx.QueryRow(ctx, "llm_requests", "state,retry_eligible", "id=$2", task.requestID).Scan(&state, &retry); err != nil {
		return err
	}
	settlement, err := settlementOf(ctx, tx, task.requestID)
	if err != nil {
		return err
	}
	var next *time.Time
	verify := false
	switch {
	case retry && state == string(StatePrepared):
		n := now.Add(generationPoll)
		next = &n
	case generationTerminal(task.state) && (settlement == SettlementSettled || settlement == SettlementReleased):
	case len(task.taskCipher) == 0:
		verify = true
	default:
		n := now.Add(generationPoll)
		next = &n
	}
	// 核实状态只能由受信Resume显式解除；普通证据（包括另一事件的历史重放）
	// 不能覆盖此前隔离结论，也不能重开自动推进。prepared的截止due同样保留。
	_, err = tx.Update(ctx, "llm_generation_requests", "next_advance_at=$3,verification_required=$4", "request_id=$2 AND verification_required=FALSE", task.requestID, next, verify)
	return err
}

// GenerationProgress 不含task定位/签名URL，结果就绪与费用、人工核实状态分别呈现。
type GenerationProgress struct {
	Request              RequestView
	Outputs              []GenerationOutput
	VerificationRequired bool
	NextAdvanceAt        *time.Time
}

func (r *GenerationRuntime) GetGeneration(ctx context.Context, scope store.AccountScope, id string) (GenerationProgress, error) {
	var out GenerationProgress
	err := scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		request, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", id))
		if err != nil {
			return err
		}
		if request.kind != requestKindGeneration {
			return ErrState
		}
		settlement, err := settlementOf(ctx, tx, id)
		if err != nil {
			return err
		}
		out.Request = request.view(settlement)
		if err := tx.QueryRow(ctx, "llm_generation_requests", "verification_required,next_advance_at", "request_id=$2", id).Scan(&out.VerificationRequired, &out.NextAdvanceAt); err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "llm_generation_outputs", "projection", "request_id=$2 AND attempt_id IN (SELECT id FROM llm_attempts WHERE account_id=$1 AND request_id=$2 AND attempt_number=$3)", []store.OrderBy{{Column: "ordinal"}}, 64, 0, id, request.attemptsUsed)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			var output GenerationOutput
			if err := json.Unmarshal(raw, &output); err != nil {
				return err
			}
			out.Outputs = append(out.Outputs, output)
		}
		return rows.Err()
	})
	return out, err
}
