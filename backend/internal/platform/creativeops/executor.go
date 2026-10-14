package creativeops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const retention = 90 * 24 * time.Hour

// Operation is registered by application composition, never selected from a
// client-supplied method or SQL name. Validate must run before effects.
type Operation struct {
	Key        string
	Capability string
	Validate   func(json.RawMessage) error
	Apply      func(context.Context, store.TxAccountScope, json.RawMessage) (Outcome, error)
}

type Executor struct{}

func (Executor) Run(ctx context.Context, scope store.AccountScope, op Operation, command Command) (Receipt, error) {
	var receipt Receipt
	if !ValidOperationID(command.OperationID) || command.CreatedAt.IsZero() || op.Key == "" || op.Validate == nil || op.Apply == nil {
		return receipt, ErrValidation
	}
	canonicalPayload, hash, err := normalizeCommand(op.Key, command)
	if err != nil {
		return receipt, err
	}
	err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.LockCreativeOperation(ctx, command.OperationID); err != nil {
			return err
		}
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		existing, existingHash, err := readReceipt(ctx, tx, command.OperationID)
		if err == nil {
			if existingHash != hash {
				return ErrConflict
			}
			if !now.Before(existing.RetainedUntil) {
				return ErrExpired
			}
			receipt = existing
			return nil
		}
		if !errors.Is(err, store.ErrNoRows) {
			return err
		}
		if command.CreatedAt.Before(now.Add(-retention)) || command.CreatedAt.After(now.Add(5*time.Minute)) {
			return ErrExpired
		}
		if err := tx.RequireCreativeCapability(ctx, op.Capability); err != nil {
			return err
		}
		if err := op.Validate(append(json.RawMessage(nil), canonicalPayload...)); err != nil {
			return err
		}
		if command.Guard != nil {
			if err := command.Guard(ctx, tx, op.Key, command); err != nil {
				return err
			}
		}
		outcome, err := op.Apply(ctx, tx, append(json.RawMessage(nil), canonicalPayload...))
		if err != nil {
			return err
		}
		if err := validateOutcome(outcome); err != nil {
			return err
		}
		receipt = Receipt{requestHash: hash, OperationID: command.OperationID, OperationType: op.Key, CreatedAt: now, RetainedUntil: now.Add(retention), Outcome: outcome}
		return tx.Insert(ctx, "creative_operation_receipts", []string{"operation_id", "operation_type", "client_created_at", "request_hash", "http_status", "response", "result_kind", "result_id", "result_revision", "created_at", "retained_until"}, command.OperationID, op.Key, command.CreatedAt, hash, outcome.HTTPStatus, outcome.Response, outcome.ResultKind, outcome.ResultID, outcome.ResultRevision, now, receipt.RetainedUntil)
	})
	// Even a populated callback result is not proof that COMMIT succeeded.
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (e Executor) Lookup(ctx context.Context, scope store.AccountScope, id string) (Receipt, error) {
	return e.lookup(ctx, scope, id, false)
}

// LookupLocked waits for in-flight execution of this operation before reading.
// Absence is final only when the caller has also fenced future execution.
func (e Executor) LookupLocked(ctx context.Context, scope store.AccountScope, id string) (Receipt, error) {
	return e.lookup(ctx, scope, id, true)
}

// LookupCommandLocked proves that a receipt belongs to this exact frozen
// request, not merely to an operation ID that another command claimed.
func (e Executor) LookupCommandLocked(ctx context.Context, scope store.AccountScope, key string, command Command) (Receipt, error) {
	var receipt Receipt
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		receipt, err = e.LookupCommandInTx(ctx, tx, key, command)
		return err
	})
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// LookupCommandInTx locks the operation and verifies the exact frozen request.
// The lock remains held even on ErrNotFound until the caller's transaction
// ends, allowing absence to be atomically followed by a future-execution fence.
// Call it before taking the guard's row locks, matching Run's lock order.
func (Executor) LookupCommandInTx(ctx context.Context, tx store.TxAccountScope, key string, command Command) (Receipt, error) {
	if !ValidOperationID(command.OperationID) || key == "" || command.CreatedAt.IsZero() {
		return Receipt{}, ErrValidation
	}
	_, hash, err := normalizeCommand(key, command)
	if err != nil {
		return Receipt{}, err
	}
	if err := tx.LockCreativeOperation(ctx, command.OperationID); err != nil {
		return Receipt{}, err
	}
	receipt, err := lookupReceiptInTx(ctx, tx, command.OperationID)
	if err != nil {
		return Receipt{}, err
	}
	if receipt.OperationType != key || receipt.requestHash != hash {
		return Receipt{}, ErrConflict
	}
	return receipt, nil
}

func (Executor) lookup(ctx context.Context, scope store.AccountScope, id string, lock bool) (Receipt, error) {
	var receipt Receipt
	if !ValidOperationID(id) {
		return receipt, ErrValidation
	}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if lock {
			if err := tx.LockCreativeOperation(ctx, id); err != nil {
				return err
			}
		}
		var err error
		receipt, err = lookupReceiptInTx(ctx, tx, id)
		return err
	})
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func lookupReceiptInTx(ctx context.Context, tx store.TxAccountScope, id string) (Receipt, error) {
	if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
		return Receipt{}, err
	}
	receipt, _, err := readReceipt(ctx, tx, id)
	if errors.Is(err, store.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	if err != nil {
		return Receipt{}, err
	}
	now, err := tx.CreativeNow(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if !now.Before(receipt.RetainedUntil) {
		return Receipt{}, ErrExpired
	}
	return receipt, nil
}

func readReceipt(ctx context.Context, tx store.TxAccountScope, id string) (Receipt, string, error) {
	var r Receipt
	var hash string
	var revision *int64
	err := tx.QueryRow(ctx, "creative_operation_receipts", "operation_id, operation_type, request_hash, http_status, response, result_kind, result_id, result_revision, created_at, retained_until", "operation_id = $2", id).Scan(&r.OperationID, &r.OperationType, &hash, &r.Outcome.HTTPStatus, &r.Outcome.Response, &r.Outcome.ResultKind, &r.Outcome.ResultID, &revision, &r.CreatedAt, &r.RetainedUntil)
	if revision != nil {
		v := Revision(*revision)
		r.Outcome.ResultRevision = &v
	}
	r.requestHash = hash
	return r, hash, err
}

func validateOutcome(out Outcome) error {
	if out.HTTPStatus != 200 && out.HTTPStatus != 201 && out.HTTPStatus != 202 {
		return ErrValidation
	}
	if out.ResultKind == "" || len(out.Response) > 64<<10 {
		return fmt.Errorf("%w: receipt must be bounded", ErrValidation)
	}
	var object map[string]any
	if err := Decode(out.Response, &object); err != nil {
		return err
	}
	if out.ResultRevision != nil && *out.ResultRevision < 1 {
		return ErrValidation
	}
	return nil
}

func normalizeCommand(key string, command Command) ([]byte, string, error) {
	var payload map[string]any
	if err := Decode(command.Payload, &payload); err != nil {
		return nil, "", err
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, "", ErrValidation
	}
	normalized, err := json.Marshal(struct {
		Type      string         `json:"type"`
		CreatedAt string         `json:"client_created_at"`
		Payload   map[string]any `json:"payload"`
	}{key, command.CreatedAt.UTC().Format(time.RFC3339Nano), payload})
	if err != nil {
		return nil, "", ErrValidation
	}
	sum := sha256.Sum256(normalized)
	return canonicalPayload, hex.EncodeToString(sum[:]), nil
}
