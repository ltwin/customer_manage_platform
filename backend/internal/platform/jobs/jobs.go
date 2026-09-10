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

type Request struct {
	Kind        string          `json:"kind"`
	OperationID string          `json:"operation_id"`
	CreatedAt   time.Time       `json:"client_created_at"`
	Payload     json.RawMessage `json:"payload"`
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
