package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// GenerationImport 是账号绑定的内部取件能力，不可作为公开响应序列化。
// 输出身份不含会过期的定位；媒体端仍须校验实际字节。
type GenerationImport struct {
	DeploymentID string           `json:"-"`
	RequestID    string           `json:"-"`
	AttemptID    string           `json:"-"`
	Output       GenerationOutput `json:"-"`
	account      string
}

func (r *GenerationRuntime) ImportableOutputs(ctx context.Context, scope store.AccountScope, id, caller, consumer string) ([]GenerationImport, error) {
	var out []GenerationImport
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := r.prelockCaller(ctx, tx); err != nil {
			return err
		}
		var err error
		id, err = currentGenerationInTx(ctx, tx, id)
		if err != nil {
			return err
		}
		allowed, err := generationRouteAllowsOutput(ctx, tx, id)
		if err != nil || !allowed {
			return err
		}
		if _, err := r.lockGenerationConsumer(ctx, tx, id, caller, consumer); err != nil {
			return err
		}
		adm, err := generationAdmissionOf(ctx, tx, id)
		if err != nil {
			return err
		}
		rows, err := tx.QueryPage(ctx, "llm_generation_outputs", "attempt_id,projection,location_cipher", "request_id=$2 AND import_state='pending'", []store.OrderBy{{Column: "ordinal"}}, 64, 0, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			item := GenerationImport{RequestID: id, DeploymentID: adm.deploymentID, account: scope.AccountID()}
			var raw, location []byte
			if err := rows.Scan(&item.AttemptID, &raw, &location); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &item.Output); err != nil {
				return err
			}
			if item.Output.Media != nil {
				plain, err := r.open(scope.AccountID(), item.AttemptID, "output:"+item.Output.ID, location)
				if err != nil {
					return err
				}
				item.Output.Media.FetchURL = string(plain)
			}
			out = append(out, item)
		}
		return rows.Err()
	})
	return out, err
}

// 请求锁先于业务锁；费用与预算不在交接中变化，因此本路径不取预算锁。
func (r *GenerationRuntime) lockGenerationConsumer(ctx context.Context, tx store.TxAccountScope, id, caller, consumer string) (string, error) {
	request, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", id))
	if err != nil {
		return "", err
	}
	if request.kind != requestKindGeneration || request.callerService != caller {
		return "", ErrState
	}
	var verify bool
	if err = tx.QueryRow(ctx, "llm_generation_requests", "result_verification_required", "request_id=$2", id).Scan(&verify); err != nil {
		return "", err
	}
	if verify {
		return "", ErrState
	}
	var state string
	err = tx.QueryRowForUpdate(ctx, "llm_result_consumers", "state", "request_id=$2 AND caller_service=$3 AND consumer_key=$4", id, caller, consumer).Scan(&state)
	if errors.Is(err, store.ErrNoRows) {
		return "", ErrState
	}
	if err != nil {
		return "", err
	}
	if state == "abandoned" {
		return "", ErrState
	}
	return state, nil
}

// HandoffOutputInTx 只在同事务的save已经建立真实保留根后登记交接。
// 多输出全部交接才释放consumer；重复调用不重复运行save，失败全部回滚。
func (r *GenerationRuntime) HandoffOutputInTx(ctx context.Context, tx store.TxAccountScope, item GenerationImport, caller, consumer string, save func() (string, error)) error {
	if err := r.prelockCaller(ctx, tx); err != nil {
		return err
	}
	allowed, err := generationRouteAllowsOutput(ctx, tx, item.RequestID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrState
	}
	if item.account != tx.AccountID() || save == nil {
		return ErrValidation
	}
	if _, err := r.lockGenerationConsumer(ctx, tx, item.RequestID, caller, consumer); err != nil {
		return err
	}
	var state string
	var raw []byte
	if err := tx.QueryRowForUpdate(ctx, "llm_generation_outputs", "import_state,projection", "request_id=$2 AND attempt_id=$3 AND output_id=$4", item.RequestID, item.AttemptID, item.Output.ID).Scan(&state, &raw); err != nil {
		return err
	}
	if state != "pending" {
		return nil
	}
	var prior GenerationOutput
	if err := json.Unmarshal(raw, &prior); err != nil {
		return err
	}
	if !sameGenerationOutput(prior, item.Output) {
		return ErrConflict
	}
	revision, err := save()
	if err != nil {
		return err
	}
	if revision == "" {
		return ErrValidation
	}
	if _, err = tx.Update(ctx, "llm_generation_outputs", "import_state='imported',content_revision_id=$5", "request_id=$2 AND attempt_id=$3 AND output_id=$4", item.RequestID, item.AttemptID, item.Output.ID, revision); err != nil {
		return err
	}
	pending, err := tx.Exists(ctx, "llm_generation_outputs", "request_id=$2 AND import_state='pending'", item.RequestID)
	if err != nil {
		return err
	}
	if !pending {
		_, err = tx.Update(ctx, "llm_result_consumers", "state='consumed',released_at=$5", "request_id=$2 AND caller_service=$3 AND consumer_key=$4 AND state='pending'", item.RequestID, caller, consumer, r.gateway.now().UTC())
	}
	return err
}
func sameGenerationOutput(a, b GenerationOutput) bool {
	if a.ID != b.ID || a.Index != b.Index || a.Kind != b.Kind || a.Text != b.Text || (a.Media == nil) != (b.Media == nil) {
		return false
	}
	return a.Media == nil || a.Media.MIME == b.Media.MIME && a.Media.ByteSize == b.Media.ByteSize
}

// LockPublicationInTx 先锁部署门和请求，再由调用方取得画布锁。
// 取消/费用不参与此事务；停用与发布通过持久部署门线性化。
func (r *GenerationRuntime) LockPublicationInTx(ctx context.Context, tx store.TxAccountScope, id string) (bool, error) {
	if err := r.prelockCaller(ctx, tx); err != nil {
		return false, err
	}
	var err error
	id, err = currentGenerationInTx(ctx, tx, id)
	if err != nil {
		return false, err
	}
	allowed, err := generationRouteAllowsOutput(ctx, tx, id)
	if err != nil || !allowed {
		return false, err
	}
	adm, err := generationAdmissionOf(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if err = tx.LockGenerationDeploymentGate(ctx, adm.deploymentID); err != nil {
		return false, err
	}
	row, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", id))
	if err != nil {
		return false, err
	}
	var enabled, verify bool
	var until time.Time
	if err = tx.QueryRow(ctx, "llm_generation_deployments", "enabled,valid_until", "deployment_id=$2", adm.deploymentID).Scan(&enabled, &until); err != nil {
		return false, err
	}
	if err = tx.QueryRow(ctx, "llm_generation_requests", "result_verification_required", "request_id=$2", id).Scan(&verify); err != nil {
		return false, err
	}
	return enabled && until.After(r.gateway.now()) && !verify && row.cancelAt == nil && row.closedAt == nil && row.state == StateSucceeded && row.deadline.After(r.gateway.now()), nil
}

// RefreshImportLocation 只查询已知任务并核对同一输出，永不Submit。
// 原始Observe仍交给G3证据链，刷新只改变取件定位，不接受新的费用/终态。
func (r *GenerationRuntime) RefreshImportLocation(ctx context.Context, scope store.AccountScope, item GenerationImport) (GenerationImport, error) {
	if item.account != scope.AccountID() || item.Output.Media == nil {
		return item, ErrValidation
	}
	var task generationTask
	var prepared PreparedPolicyGeneration
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		task, err = scanGenerationTask(tx.QueryRow(ctx, "llm_generation_tasks", generationTaskColumns, "request_id=$2 AND attempt_id=$3", item.RequestID, item.AttemptID))
		if err != nil {
			return err
		}
		adm, err := generationAdmissionOf(ctx, tx, item.RequestID)
		if err != nil {
			return err
		}
		prepared, err = RestorePolicyGeneration(adm.snapshot, adm.fingerprint)
		return err
	})
	if err != nil {
		return item, err
	}
	a, ok := r.adapters[task.deploymentID+"/"+task.adapterVersion]
	if !ok || task.taskDigest == nil || task.state != GenerationCompleted {
		return item, ErrUnavailable
	}
	taskID, err := r.open(scope.AccountID(), task.attemptID, "task", task.taskCipher)
	if err != nil {
		return item, err
	}
	callCtx, cancel := context.WithTimeout(ctx, generationCallLifetime)
	defer cancel()
	event, err := a.Adapter.Observe(callCtx, GenerationCall{RequestID: item.RequestID, AttemptID: item.AttemptID, TaskID: string(taskID), Prepared: prepared})
	if err != nil {
		return item, ErrUnavailable
	}
	if err = event.Observation.Validate(prepared.Generation()); err != nil {
		return item, err
	}
	if event.Observation.State != GenerationCompleted || event.Observation.TaskID != string(taskID) {
		return item, ErrConflict
	}
	var refreshed *GenerationOutput
	for _, out := range event.Observation.Outputs {
		if out.ID == item.Output.ID {
			v := out
			refreshed = &v
		}
	}
	if refreshed == nil || !sameGenerationOutput(item.Output, *refreshed) || !refreshed.Media.ExpiresAt.After(r.gateway.now()) {
		return item, ErrConflict
	}
	// 不改持久投影：本次取件使用新定位，失败后仍只查询同一任务。原证据原样保留。
	item.Output = *refreshed
	return item, nil
}

// ReleaseEmptyGenerationInTx只释放已经终态且没有输出的结果保留，费用核实不变。
func (r *GenerationRuntime) ReleaseEmptyGenerationInTx(ctx context.Context, tx store.TxAccountScope, id, caller, consumer string) error {
	if err := r.prelockCaller(ctx, tx); err != nil {
		return err
	}
	var err error
	id, err = currentGenerationInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	allowed, err := generationRouteAllowsOutput(ctx, tx, id)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrState
	}
	state, err := r.lockGenerationConsumer(ctx, tx, id, caller, consumer)
	if errors.Is(err, ErrState) {
		return nil
	}
	if err != nil {
		return err
	}
	if state != "pending" {
		return nil
	}
	request, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", id))
	if err != nil {
		return err
	}
	if !request.view(SettlementUnclaimed).Terminal() {
		return ErrState
	}
	any, err := tx.Exists(ctx, "llm_generation_outputs", "request_id=$2", id)
	if err != nil || any {
		return err
	}
	return r.gateway.AbandonConsumerInTx(ctx, tx, id, caller, consumer)
}

// StopUndispatchedInTx在driver停驻前撤回尚未消费的许可；不关闭已派发/未知费用。
func (r *GenerationRuntime) StopUndispatchedInTx(ctx context.Context, tx store.TxAccountScope, id string) (bool, error) {
	if err := r.prelockCaller(ctx, tx); err != nil {
		return false, err
	}
	if err := requireGenerationRoot(ctx, tx, id); err != nil {
		return false, err
	}
	root := id
	route, routeErr := scanGenerationRoute(tx.QueryRowForUpdate(ctx, "llm_generation_routes", generationRouteColumns, "root_request_id=$2", root))
	if routeErr != nil && !errors.Is(routeErr, store.ErrNoRows) {
		return false, routeErr
	}
	if routeErr == nil {
		id = route.current
	}
	if err := r.gateway.lockBudgetForRequest(ctx, tx, id, r.gateway.now()); err != nil {
		return false, err
	}
	row, err := scanRequest(tx.QueryRowForUpdate(ctx, "llm_requests", requestColumns, "id=$2", id))
	if err != nil {
		return false, err
	}
	if row.kind != requestKindGeneration {
		return false, ErrState
	}
	if !generationCanStopBeforeDispatch(row.state, routeErr == nil && route.state == "active") {
		return false, nil
	}
	if routeErr == nil {
		if _, err := tx.Update(ctx, "llm_generation_routes", "state='cancelled',next_advance_at=NULL", "root_request_id=$2", root); err != nil {
			return false, err
		}
	}
	_, err = r.gateway.CancelInTx(ctx, tx, id)
	return err == nil, err
}

func generationCanStopBeforeDispatch(state RequestState, activeRoute bool) bool {
	return state == StatePrepared || (activeRoute && (state == StateFailed || state == StateCancelled))
}
