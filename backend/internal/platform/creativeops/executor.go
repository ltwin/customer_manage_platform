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
	var payload map[string]any
	if err := Decode(command.Payload, &payload); err != nil {
		return receipt, err
	}
	canonicalPayload, err := json.Marshal(payload)
	if err != nil {
		return receipt, ErrValidation
	}
	normalized, err := json.Marshal(struct {
		Type      string         `json:"type"`
		CreatedAt string         `json:"client_created_at"`
		Payload   map[string]any `json:"payload"`
	}{op.Key, command.CreatedAt.UTC().Format(time.RFC3339Nano), payload})
	if err != nil {
		return receipt, ErrValidation
	}
	sum := sha256.Sum256(normalized)
	hash := hex.EncodeToString(sum[:])
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
		outcome, err := op.Apply(ctx, tx, append(json.RawMessage(nil), canonicalPayload...))
		if err != nil {
			return err
		}
		if err := validateOutcome(outcome); err != nil {
			return err
		}
		receipt = Receipt{OperationID: command.OperationID, OperationType: op.Key, CreatedAt: now, RetainedUntil: now.Add(retention), Outcome: outcome}
		return tx.Insert(ctx, "creative_operation_receipts", []string{"operation_id", "operation_type", "client_created_at", "request_hash", "http_status", "response", "result_kind", "result_id", "result_revision", "created_at", "retained_until"}, command.OperationID, op.Key, command.CreatedAt, hash, outcome.HTTPStatus, outcome.Response, outcome.ResultKind, outcome.ResultID, outcome.ResultRevision, now, receipt.RetainedUntil)
	})
	// Even a populated callback result is not proof that COMMIT succeeded.
	if err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

func (Executor) Lookup(ctx context.Context, scope store.AccountScope, id string) (Receipt, error) {
	var receipt Receipt
	if !ValidOperationID(id) {
		return receipt, ErrValidation
	}
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var err error
		receipt, _, err = readReceipt(ctx, tx, id)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		if !now.Before(receipt.RetainedUntil) {
			return ErrExpired
		}
		return nil
	})
	if err != nil {
		return Receipt{}, err
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
