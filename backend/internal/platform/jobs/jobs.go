// Package jobs defines account-bound tasks without exposing the queue database.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidTask = errors.New("invalid creative task")
var ErrNoHandlers = errors.New("no production creative workers are registered")

// Request 的 river:"unique" 标记只标记参与队列去重的字段（任务类型与载荷）；
// operation_id 与创建时间不进去重键，同一逻辑任务的补投递在各次入队之间保持同一键。
type Request struct {
	Kind        string          `json:"kind" river:"unique"`
	OperationID string          `json:"operation_id"`
	CreatedAt   time.Time       `json:"client_created_at"`
	Payload     json.RawMessage `json:"payload" river:"unique"`
	// UniqueByArgs 请求队列按（账户、任务类型、载荷）去重：只要还有存活投递
	// （available/pending/running/retryable/scheduled），重复入队被队列跳过；
	// 投递被队列侧耗尽丢弃后，再次入队会建立新投递。
	UniqueByArgs bool `json:"unique_by_args"`
}

func (r Request) Validate() error {
	id, err := uuid.Parse(r.OperationID)
	if err != nil || id == uuid.Nil || id.String() != r.OperationID || r.Kind == "" || r.CreatedAt.IsZero() || len(r.Payload) > 1<<20 {
		return ErrInvalidTask
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(r.Payload, &object); err != nil || object == nil {
		return ErrInvalidTask
	}
	return nil
}

type Runtime interface {
	Start(context.Context) error
	Stop(context.Context) error
	Check(context.Context) error
}

// TxEnqueuer is a sealed capability bound by the store to one physical tx.
type TxEnqueuer interface {
	enqueue(context.Context, Request) (int64, error)
}

type boundEnqueuer struct {
	insert func(context.Context, Request) (int64, error)
}

func (e boundEnqueuer) enqueue(ctx context.Context, r Request) (int64, error) {
	return e.insert(ctx, r)
}

// BindTrustedTxEnqueuer is for platform adapters, not request input or domain SQL.
func BindTrustedTxEnqueuer(insert func(context.Context, Request) (int64, error)) TxEnqueuer {
	return boundEnqueuer{insert: insert}
}
func EnqueueInTx(ctx context.Context, tx TxEnqueuer, r Request) (int64, error) {
	if tx == nil {
		return 0, ErrInvalidTask
	}
	if err := r.Validate(); err != nil {
		return 0, err
	}
	return tx.enqueue(ctx, r)
}

// DeferredError keeps the same durable job pending without spending a failure
// attempt. Domain state must be committed before returning it.
type DeferredError struct{ After time.Duration }

func (e *DeferredError) Error() string { return "creative task deferred" }
func Defer(after time.Duration) error {
	if after <= 0 || after > time.Hour {
		return ErrInvalidTask
	}
	return &DeferredError{After: after}
}
