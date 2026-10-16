package llmgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const generationRouteDelay = 5 * time.Second

// GenerationRouteCandidate 已完成本次输入的能力校验与报价；不是可透传的供应商参数。
type GenerationRouteCandidate struct {
	Prepared        PreparedPolicyGeneration
	ControlRevision int64
}
type frozenGenerationCandidate struct {
	Snapshot        json.RawMessage `json:"snapshot"`
	Fingerprint     string          `json:"fingerprint"`
	ControlRevision int64           `json:"control_revision"`
}
type generationRoute struct {
	root, current, state        string
	plan                        []byte
	candidate, attempt, ordinal int
	next                        *time.Time
	final                       *string
	maxSubmissions              int
	maxCost                     int64
}

const generationRouteColumns = "root_request_id,current_request_id,state,plan,candidate_index,candidate_attempt,ordinal,next_advance_at,final_request_id,max_submissions,max_cost_micros"

func scanGenerationRoute(row store.Row) (generationRoute, error) {
	var r generationRoute
	err := row.Scan(&r.root, &r.current, &r.state, &r.plan, &r.candidate, &r.attempt, &r.ordinal, &r.next, &r.final, &r.maxSubmissions, &r.maxCost)
	return r, err
}

// PrepareRouteInTx 原子保存有序候选及首次调用；旧请求不会补写新路由。
func (r *GenerationRuntime) PrepareRouteInTx(ctx context.Context, tx store.TxAccountScope, in GenerationAdmission, revision int64) (RequestView, error) {
	if len(in.Route) == 0 {
		return r.PrepareInTx(ctx, tx, in, revision)
	}
	if len(in.Route) > 8 || in.Route[0].Prepared.Fingerprint() != in.Prepared.Fingerprint() || in.Route[0].ControlRevision != revision {
		return RequestView{}, ErrValidation
	}
	var plan []frozenGenerationCandidate
	var maximum int64
	base := in.Prepared.Generation().Snapshot().Request
	baseInput, _ := json.Marshal(base.Input)
	seen := map[string]bool{}
	for _, c := range in.Route {
		d := c.Prepared.PolicyDefinition()
		req := c.Prepared.Generation().Snapshot().Request
		input, _ := json.Marshal(req.Input)
		upper, valid := generationReservedUpperBound(c.Prepared)
		if c.ControlRevision < 1 || seen[d.DeploymentID] || req.ModelKey != base.ModelKey || req.Kind != base.Kind || d.Currency != in.Prepared.PolicyDefinition().Currency || !bytes.Equal(baseInput, input) || !valid || upper > (math.MaxInt64-maximum)/2 {
			return RequestView{}, ErrValidation
		}
		seen[d.DeploymentID] = true
		maximum += 2 * upper
		raw, err := c.Prepared.MarshalSnapshot()
		if err != nil {
			return RequestView{}, err
		}
		plan = append(plan, frozenGenerationCandidate{Snapshot: raw, Fingerprint: c.Prepared.Fingerprint(), ControlRevision: c.ControlRevision})
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return RequestView{}, err
	}
	if len(raw) > 60000000 {
		return RequestView{}, ErrValidation
	}
	view, err := r.PrepareInTx(ctx, tx, in, revision)
	if err != nil {
		return RequestView{}, err
	}
	var prior string
	err = tx.QueryRow(ctx, "llm_generation_routes", "plan_digest", "root_request_id=$2", view.ID).Scan(&prior)
	if err == nil {
		if prior != digest(raw) {
			return RequestView{}, ErrConflict
		}
		return view, nil
	}
	if !errors.Is(err, store.ErrNoRows) {
		return RequestView{}, err
	}
	if err = tx.Insert(ctx, "llm_generation_routes", []string{"root_request_id", "current_request_id", "plan", "plan_digest", "max_cost_micros", "max_submissions", "next_advance_at"}, view.ID, view.ID, raw, digest(raw), maximum, len(plan)*2, r.gateway.now()); err != nil {
		return RequestView{}, err
	}
	err = tx.Insert(ctx, "llm_generation_route_calls", []string{"root_request_id", "ordinal", "request_id"}, view.ID, 0, view.ID)
	if err == nil {
		err = r.enqueueRoute(ctx, tx, view.ID)
	}
	return view, err
}

// generationRootInTx 用不可变成员关系解析业务归属；没有路由时返回原request。
func generationRootInTx(ctx context.Context, tx store.TxAccountScope, id string) (string, error) {
	var root string
	err := tx.QueryRow(ctx, "llm_generation_route_calls", "root_request_id", "request_id=$2", id).Scan(&root)
	if errors.Is(err, store.ErrNoRows) {
		return id, nil
	}
	return root, err
}

// AdvanceGeneration 保留原单部署执行语义。只有路由根驱动切换；旧调用的后台任务
// 继续观察自身以核实费用，不能重新成为路由的胜出结果。
func (r *GenerationRuntime) AdvanceGeneration(ctx context.Context, scope store.AccountScope, id string) (GenerationProgress, error) {
	var exists bool
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := requireGenerationRoot(ctx, tx, id); err != nil {
			return err
		}
		var e error
		exists, e = tx.Exists(ctx, "llm_generation_routes", "root_request_id=$2", id)
		return e
	})
	if err != nil {
		return GenerationProgress{}, err
	}
	if !exists {
		return r.advanceGeneration(ctx, scope, id)
	}
	route, err := r.advanceGenerationRoute(ctx, scope, id)
	if err != nil {
		return GenerationProgress{}, err
	}
	if route.state == "active" && (route.next == nil || !route.next.After(r.gateway.now())) {
		if _, err = r.advanceGeneration(ctx, scope, route.current); err != nil {
			return GenerationProgress{}, err
		}
		route, err = r.advanceGenerationRoute(ctx, scope, id)
		if err != nil {
			return GenerationProgress{}, err
		}
	}
	if route.state == "cancelled" {
		if _, err = r.advanceGeneration(ctx, scope, route.current); err != nil {
			return GenerationProgress{}, err
		}
	}
	p, err := r.GetGeneration(ctx, scope, route.current)
	if err != nil {
		return p, err
	}
	p.Route = &GenerationRouteProgress{RootRequestID: route.root, CurrentRequestID: route.current, State: route.state}
	if route.final != nil {
		p.Route.FinalRequestID = *route.final
	}
	if route.state == "active" {
		p.NextAdvanceAt = route.next
	}
	return p, nil
}

// 只做短事务。路由行是新调用的串行点；与画布写门保持相同顺序，网络在事务外。
func (r *GenerationRuntime) advanceGenerationRoute(ctx context.Context, scope store.AccountScope, root string) (generationRoute, error) {
	var route generationRoute
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := r.prelockCaller(ctx, tx); err != nil {
			return err
		}
		var err error
		route, err = scanGenerationRoute(tx.QueryRowForUpdate(ctx, "llm_generation_routes", generationRouteColumns, "root_request_id=$2", root))
		if err != nil {
			return err
		}
		if route.state != "active" {
			return nil
		}
		now := r.gateway.now().UTC()
		row, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", route.current))
		if err != nil {
			return err
		}
		var plan []frozenGenerationCandidate
		if err = json.Unmarshal(route.plan, &plan); err != nil {
			return err
		}
		if route.candidate >= len(plan) {
			return ErrProtocol
		}
		var code string
		var failure *string
		var updated time.Time
		if err = tx.QueryRow(ctx, "llm_requests", "failure_class,updated_at", "id=$2", row.id).Scan(&failure, &updated); err != nil {
			return err
		}
		if failure != nil {
			code = *failure
		}
		terminal := row.state == StateSucceeded || row.state == StateFailed || row.state == StateCancelled
		var resultConflict bool
		if err = tx.QueryRow(ctx, "llm_generation_requests", "result_verification_required", "request_id=$2", row.id).Scan(&resultConflict); err != nil {
			return err
		}
		if resultConflict {
			return r.routeDue(ctx, tx, &route, nil)
		}
		if !terminal {
			var due *time.Time
			if err = tx.QueryRow(ctx, "llm_generation_requests", "next_advance_at", "request_id=$2", row.id).Scan(&due); err != nil {
				return err
			}
			return r.routeDue(ctx, tx, &route, due)
		}
		next := generationRouteDecision(code)
		if row.state == StateSucceeded || (row.state == StateCancelled && code != "deployment_unavailable") || !row.deadline.After(now) || next == routeStop {
			return r.finishRoute(ctx, tx, &route)
		}
		if route.next == nil {
			due := now.Add(generationRouteDelay)
			return r.routeDue(ctx, tx, &route, &due)
		}
		// 提交后的第一次终态观察必须从失败完成时间开始退避，而不是沿用提交due。
		due := updated.Add(generationRouteDelay)
		if now.Before(due) {
			return r.routeDue(ctx, tx, &route, &due)
		}
		index, attempt := route.candidate, route.attempt+1
		if next == routeFailover || attempt > 2 {
			index++
			attempt = 1
		}
		for index < len(plan) {
			candidate := plan[index]
			prepared, err := RestorePolicyGeneration(candidate.Snapshot, candidate.Fingerprint)
			if err != nil {
				return err
			}
			d := prepared.PolicyDefinition()
			if _, ok := r.adapters[d.DeploymentID+"/"+d.AdapterVersion]; !ok {
				index++
				attempt = 1
				continue
			}
			if e := r.checkDeployment(ctx, tx, d, candidate.ControlRevision); e != nil {
				if errors.Is(e, ErrUnavailable) || errors.Is(e, ErrConflict) {
					index++
					attempt = 1
					continue
				}
				return e
			}
			active, e := tx.LockGenerationAccount(ctx)
			if e != nil {
				return e
			}
			if !active {
				return r.finishRoute(ctx, tx, &route)
			}
			allowed, err := routeBudgetAllows(ctx, tx, route, prepared)
			if err != nil {
				return err
			}
			if !allowed {
				return r.finishRoute(ctx, tx, &route)
			}
			in, e := r.routeAdmission(ctx, tx, root, prepared, route.ordinal+1)
			if e != nil {
				return e
			}
			// 独立预留必须失败原子；预算不足停止当前路由，不撤销旧的未知费用。
			// 预算检查返回ErrBudget发生在任何写入之前。
			view, e := r.prepareRouteCallInTx(ctx, tx, root, route.ordinal+1, code, in, candidate.ControlRevision)
			if e != nil {
				if errors.Is(e, ErrBudget) || errors.Is(e, ErrCancelled) || errors.Is(e, ErrDeadline) || errors.Is(e, ErrCapability) || errors.Is(e, ErrConflict) || errors.Is(e, ErrUnavailable) || errors.Is(e, store.ErrCreativeAccessDenied) {
					if view.ID != "" {
						if _, cancelErr := r.gateway.CancelInTx(ctx, tx, view.ID); cancelErr != nil {
							return cancelErr
						}
					}
					return r.finishRoute(ctx, tx, &route)
				}
				return e
			}
			route.current, route.candidate, route.attempt, route.ordinal = view.ID, index, attempt, route.ordinal+1
			route.next = &now
			_, err = tx.Update(ctx, "llm_generation_routes", "current_request_id=$3,candidate_index=$4,candidate_attempt=$5,ordinal=$6,next_advance_at=$7", "root_request_id=$2", root, view.ID, index, attempt, route.ordinal, now)
			if err == nil {
				r.gateway.logger.InfoContext(ctx, "generation route advanced", "root_request_id", root, "request_id", view.ID, "deployment_id", d.DeploymentID, "ordinal", route.ordinal, "reason", code)
			}
			return err
		}
		return r.finishRoute(ctx, tx, &route)
	})
	return route, err
}
func generationRouteDecision(code string) routeStep {
	switch code {
	case "credential_refused", "insufficient_balance", "no_available_channel", "deployment_unavailable":
		return routeFailover
	case "server_error", "rate_limited", "execution_timeout", "service_unavailable", "not_dispatched":
		return routeRetry
	default:
		return routeStop
	}
}
func (r *GenerationRuntime) routeDue(ctx context.Context, tx store.TxAccountScope, route *generationRoute, next *time.Time) error {
	route.next = next
	_, err := tx.Update(ctx, "llm_generation_routes", "next_advance_at=$3", "root_request_id=$2", route.root, next)
	return err
}
func (r *GenerationRuntime) finishRoute(ctx context.Context, tx store.TxAccountScope, route *generationRoute) error {
	route.state = "finished"
	route.final = &route.current
	route.next = nil
	_, err := tx.Update(ctx, "llm_generation_routes", "state='finished',final_request_id=current_request_id,next_advance_at=NULL", "root_request_id=$2", route.root)
	return err
}
func (r *GenerationRuntime) routeAdmission(ctx context.Context, tx store.TxAccountScope, root string, p PreparedPolicyGeneration, ordinal int) (GenerationAdmission, error) {
	row, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "id=$2", root))
	if err != nil {
		return GenerationAdmission{}, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "llm_generation_requests", "admission_control", "request_id=$2", root).Scan(&raw); err != nil {
		return GenerationAdmission{}, err
	}
	var c generationAdmissionControl
	if err = json.Unmarshal(raw, &c); err != nil {
		return GenerationAdmission{}, err
	}
	deadline, err := time.Parse(time.RFC3339Nano, c.Deadline)
	if err != nil {
		return GenerationAdmission{}, err
	}
	groupDeadline, err := time.Parse(time.RFC3339Nano, c.GroupDeadline)
	if err != nil {
		return GenerationAdmission{}, err
	}
	return GenerationAdmission{CallerService: row.callerService, CallerOperationID: derivedOperationID(row.callerService, fmt.Sprintf("%s/%d", root, ordinal), "generation-route"), CallerGroupID: c.CallerGroupID, GroupLimitMicros: c.GroupLimitMicros, GroupTokenLimit: c.GroupTokenLimit, GroupDeadline: groupDeadline, AccountLimitMicros: c.AccountLimitMicros, AccountTokenLimit: c.AccountTokenLimit, ConsumerKey: c.ConsumerKey, Deadline: deadline, Prepared: p}, nil
}

// CancelRouteInTx 关闭路由选择并取消当前调用；旧调用的费用恢复继续进行。
func (r *GenerationRuntime) CancelRouteInTx(ctx context.Context, tx store.TxAccountScope, root string) (RequestView, error) {
	if err := requireGenerationRoot(ctx, tx, root); err != nil {
		return RequestView{}, err
	}
	if err := r.prelockCaller(ctx, tx); err != nil {
		return RequestView{}, err
	}
	route, err := scanGenerationRoute(tx.QueryRowForUpdate(ctx, "llm_generation_routes", generationRouteColumns, "root_request_id=$2", root))
	if errors.Is(err, store.ErrNoRows) {
		return r.gateway.CancelInTx(ctx, tx, root)
	}
	if err != nil {
		return RequestView{}, err
	}
	if _, err = tx.Update(ctx, "llm_generation_routes", "state='cancelled',next_advance_at=NULL", "root_request_id=$2", root); err != nil {
		return RequestView{}, err
	}
	return r.gateway.CancelInTx(ctx, tx, route.current)
}

func currentGenerationInTx(ctx context.Context, tx store.TxAccountScope, id string) (string, error) {
	if err := requireGenerationRoot(ctx, tx, id); err != nil {
		return "", err
	}
	var current string
	err := tx.QueryRowForUpdate(ctx, "llm_generation_routes", "current_request_id", "root_request_id=$2", id).Scan(&current)
	if errors.Is(err, store.ErrNoRows) {
		return id, nil
	}
	return current, err
}

// 只有已选中的调用可以交接作品。取消后允许保留当前调用的迟到候选，不能切换。
func generationRouteAllowsOutput(ctx context.Context, tx store.TxAccountScope, id string) (bool, error) {
	var root string
	err := tx.QueryRow(ctx, "llm_generation_route_calls", "root_request_id", "request_id=$2", id).Scan(&root)
	if errors.Is(err, store.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return tx.Exists(ctx, "llm_generation_routes", "root_request_id=$2 AND ((state='finished' AND final_request_id=$3) OR (state='cancelled' AND current_request_id=$3))", root, id)
}

// 根身份属于业务操作；部署调用身份只在Gateway的队列、证据及取件能力中流转。
func requireGenerationRoot(ctx context.Context, tx store.TxAccountScope, id string) error {
	root, err := generationRootInTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if root != id {
		return ErrState
	}
	return nil
}

// 创建调用、建立归属、授权及排队是一个不可拆分的事务操作。
func (r *GenerationRuntime) prepareRouteCallInTx(ctx context.Context, tx store.TxAccountScope, root string, ordinal int, reason string, in GenerationAdmission, revision int64) (RequestView, error) {
	view, err := r.gateway.PrepareGenerationInTx(ctx, tx, in)
	if err != nil {
		return view, err
	}
	if err = tx.Insert(ctx, "llm_generation_route_calls", []string{"root_request_id", "ordinal", "request_id", "reason"}, root, ordinal, view.ID, reason); err != nil {
		return view, err
	}
	_, err = r.PrepareInTx(ctx, tx, in, revision)
	return view, err
}

// 最大16次调用，逐行扣减避免聚合溢出；包含已拒绝调用以落实冻结的总尝试上界。
func routeBudgetAllows(ctx context.Context, tx store.TxAccountScope, route generationRoute, prepared PreparedPolicyGeneration) (bool, error) {
	upper, valid := generationReservedUpperBound(prepared)
	if route.ordinal+1 >= route.maxSubmissions || !valid || upper > route.maxCost {
		return false, nil
	}
	remaining := route.maxCost - upper
	rows, err := tx.QueryPage(ctx, "llm_generation_requests", "reserved_upper_bound_micros", "request_id IN (SELECT request_id FROM llm_generation_route_calls WHERE account_id=$1 AND root_request_id=$2)", []store.OrderBy{{Column: "request_id"}}, 17, 0, route.root)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var cost int64
		if err := rows.Scan(&cost); err != nil {
			return false, err
		}
		count++
		if count >= route.maxSubmissions || cost > remaining {
			return false, nil
		}
		remaining -= cost
	}
	return true, rows.Err()
}

// 返回预算约束涉及的预留金额；未启用计费不等于供应商零价。
func generationReservedUpperBound(p PreparedPolicyGeneration) (int64, bool) {
	if p.PolicyDefinition().Billing == GenerationBillingDisabled {
		return 0, true
	}
	q := p.Quote()
	if q.UpperBoundMicros == nil || *q.UpperBoundMicros < 0 {
		return 0, false
	}
	return *q.UpperBoundMicros, true
}
