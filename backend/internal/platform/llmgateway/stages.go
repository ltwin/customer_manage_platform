package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// CallProgress 表示一次有界推进后的持久事实；成功不代表业务已经消费结果。
// Dispatched 表示取得过派发许可，不能用它推断供应商是否受理或是否收费。
// 出错且无法重读持久状态时只返回请求ID，调用方必须处理错误后再查询。
type CallProgress struct {
	Request    RequestView
	Dispatched bool
}

// PrepareCallInTx 将预算预留、固定请求和结果保留登记加入业务受理事务。
// 调用方须先取得既有协议要求的平台/账号守卫，再准入Gateway，最后锁业务资源；
// 任一步失败必须回滚外层事务。此方法不执行网络，也不调用派发时的Admit回调。
// 当前载荷仅支持既有Chat契约；媒体生成不能把图片/视频输出塞入这个入口。
func (s *Service) PrepareCallInTx(ctx context.Context, tx store.TxAccountScope, session CallSession, key string, chat ChatRequest) (RequestView, error) {
	chat, binding, err := stageInput(session, key, chat)
	if err != nil {
		return RequestView{}, err
	}
	if tx.AccountID() != session.Scope.AccountID() {
		return RequestView{}, ErrValidation
	}
	// 已存在的请求按冻结快照校验，避免恢复时重新取价或增加预算预留。

	existing, err := boundRequest(ctx, tx, session, binding)
	if errors.Is(err, ErrNotFound) {
		if _, _, err = s.reserveCallInTx(ctx, tx, session, binding, chat); err != nil {
			return RequestView{}, err
		}
		// 首次查询与准入之间可能已有同键事务提交；必须核对实际取得的绑定。
		existing, err = boundRequest(ctx, tx, session, binding)
	}
	if err != nil {
		return RequestView{}, err
	}
	if err = matchesPreparedChat(existing, chat); err != nil {
		return RequestView{}, err
	}
	settlement, err := settlementOf(ctx, tx, existing.id)
	return existing.view(settlement), err

}

// AdvanceCall 只推进已受理的请求；每次最多执行一次供应商调用，不消费结果。
// 后续重试必须再次唤醒同一绑定；未受理证据可恢复，未知受理只返回已有事实。
// Admit不可省略，并沿用CallSession既有的最终许可事务与公共锁序。
func (s *Service) AdvanceCall(ctx context.Context, session CallSession, key string, chat ChatRequest, stream bool) (CallProgress, error) {
	chat, binding, err := stageInput(session, key, chat)
	if err != nil {
		return CallProgress{}, err
	}
	var view RequestView
	var generation int64
	err = session.Scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		row, err := boundRequest(ctx, tx, session, binding)
		if err != nil {
			return err
		}
		if err = matchesPreparedChat(row, chat); err != nil {
			return err
		}
		reservation, err := scanReservation(tx.QueryRow(ctx, "llm_usage_reservations", reservationColumns, "request_id=$2", row.id))
		if err != nil {
			return err
		}
		view = row.view(reservation.state)
		generation = reservation.generation
		return nil
	})
	if err != nil {
		return CallProgress{}, err
	}
	next, sent, err := s.advanceTurn(ctx, session, binding, view, &generation, chat, stream)
	return CallProgress{Request: next, Dispatched: sent}, err
}

// ConsumeCallInTx 将业务结果落库与释放Gateway保留根绑定为同一事务。
// save必须只操作调用方传入的事务；其中应复核业务资格，禁止网络或对象存储I/O。
// 重放已消费结果不会再次调用save；消费失败必须回滚外层事务。
func (s *Service) ConsumeCallInTx(ctx context.Context, tx store.TxAccountScope, session CallSession, key string, save func(Result) error) (CallOutcome, error) {
	if err := validateStageSession(session, key); err != nil {
		return CallOutcome{}, err
	}
	if tx.AccountID() != session.Scope.AccountID() || save == nil {
		return CallOutcome{}, ErrValidation
	}
	binding := session.bindingOf(key)
	row, err := boundRequest(ctx, tx, session, binding)
	if err != nil {
		return CallOutcome{}, err
	}
	result, already, err := s.ConsumeInTx(ctx, tx, row.id, session.CallerService, binding.consumerKey, save)
	return CallOutcome{RequestID: row.id, Result: result, AlreadyConsumed: already}, err
}

func validateStageSession(session CallSession, key string) error {
	if err := session.validate(); err != nil {
		return err
	}
	if session.Admit == nil || key == "" || len(key) > maxBindingKeyLen {
		return fmt.Errorf("%w: staged call requires a stable binding and a final admission guard", ErrValidation)
	}
	return nil
}
func stageInput(session CallSession, key string, chat ChatRequest) (ChatRequest, callBinding, error) {
	if err := validateStageSession(session, key); err != nil {
		return ChatRequest{}, callBinding{}, err
	}
	normalized, err := session.chatOf(chat)
	return normalized, session.bindingOf(key), err
}
func boundRequest(ctx context.Context, tx rowReader, session CallSession, binding callBinding) (requestRow, error) {
	row, err := scanRequest(tx.QueryRow(ctx, "llm_requests", requestColumns, "caller_service=$2 AND caller_operation_id=$3", session.CallerService, binding.requestOp))
	if err != nil {
		return requestRow{}, err
	}
	if row.groupID != session.CallerGroupID || row.modelKey != session.ModelKey || !row.deadline.Equal(session.Deadline.Truncate(time.Microsecond)) {
		return requestRow{}, fmt.Errorf("%w: bound request group, model, or deadline changed", ErrConflict)
	}
	return row, nil
}
func matchesPreparedChat(row requestRow, chat ChatRequest) error {
	hash, err := RequestHash(chat, row.modelSnapshot)
	if err != nil {
		return err
	}
	if hash != row.requestHash {
		return fmt.Errorf("%w: bound request input changed", ErrConflict)
	}
	return nil
}

// reserveCallInTx 是同步Call与阶段受理共用的准入实现，不维护第二份预算或身份。
func (s *Service) reserveCallInTx(ctx context.Context, tx store.TxAccountScope, session CallSession, binding callBinding, chat ChatRequest) (Reservation, RequestView, error) {
	bound := EstimateInputTokens(chat)
	if session.EstimateInputTokens != nil {
		if custom := session.EstimateInputTokens(chat); custom > bound {
			bound = custom
		}
	}
	reservation, err := s.ReserveInTx(ctx, tx, ReserveInput{
		CallerService: session.CallerService, CallerOperationID: binding.reserveOp, CallerGroupID: session.CallerGroupID,
		GroupLimitMicros: session.GroupLimitMicros, GroupTokenLimit: session.GroupTokenLimit, GroupDeadline: session.GroupDeadline,
		ModelKey: session.ModelKey, InputTokenUpperBound: bound, OutputLimit: chat.OutputLimit,
		AccountLimitMicros: session.AccountLimitMicros, AccountTokenLimit: session.AccountTokenLimit, ExpiresAt: session.Deadline,
	})
	if err != nil {
		return Reservation{}, RequestView{}, err
	}
	view, err := s.PrepareInTx(ctx, tx, PrepareInput{
		CallerService: session.CallerService, CallerOperationID: binding.requestOp, CallerGroupID: session.CallerGroupID,
		ReservationID: reservation.ID, ConsumerKey: binding.consumerKey, Chat: chat, Deadline: session.Deadline,
	})
	return reservation, view, err
}

// advanceTurn 是同步循环与阶段推进共用的单次派发，不在此处自动重试。
func (s *Service) advanceTurn(ctx context.Context, session CallSession, binding callBinding, view RequestView, generation *int64, chat ChatRequest, stream bool) (RequestView, bool, error) {
	switch view.State {
	case StateSucceeded:
		return view, false, nil
	case StateFailed:
		return view, false, fmt.Errorf("%w: request failed as %q", ErrState, view.FailureClass)
	case StateCancelled:
		return view, false, ErrCancelled
	case StateDispatching, StateStreaming, StateUnknown:
		return view, false, ErrUnknown
	case StatePrepared:
	default:
		return view, false, ErrState
	}
	var permit Permit
	var err error
	if view.RetryEligible {
		permit, *generation, err = s.redispatch(ctx, session, binding, view.ID, *generation)
	} else {
		permit, err = s.beginDispatch(ctx, session, binding, view.ID)
	}
	if err != nil {
		// 补偿可能已经取消请求并释放预算，返回补偿后的事实而不是旧prepared状态。
		current, readErr := s.Get(ctx, session.Scope, view.ID)
		if readErr != nil {
			return RequestView{ID: view.ID}, false, errors.Join(err, readErr)
		}
		return current, false, err
	}
	_, executeErr := s.Execute(ctx, session.Scope, session.Limits, permit, chat, stream)
	next, err := s.Get(ctx, session.Scope, view.ID)
	if err != nil {
		return RequestView{ID: view.ID}, true, errors.Join(executeErr, err)
	}
	// 只有Gateway已经持久证明未受理，调用方才可安排下一次有界推进。
	if executeErr != nil && (next.State != StatePrepared || !next.RetryEligible) {
		return next, true, executeErr
	}
	return next, true, nil
}
