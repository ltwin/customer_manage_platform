package idempotency

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type Operation string

const (
	OperationOrderCreate        Operation = "order.create.v1"
	OperationScheduleSlotCreate Operation = "schedule-slot.create.v1"
	defaultTTL                            = 24 * time.Hour
)

var (
	ErrConflict        = errors.New("idempotency_conflict")
	ErrValidation      = errors.New("idempotency validation failed")
	ErrInvalidResponse = errors.New("idempotency callback returned an invalid success response")
	keyPattern         = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
)

type StoredResponse struct {
	Status int
	Body   []byte
}

type transactionRunner interface {
	Run(context.Context, store.AccountScope, func(store.TxAccountScope) error) error
}

type accountScopeRunner struct{}

func (accountScopeRunner) Run(
	ctx context.Context,
	scope store.AccountScope,
	fn func(store.TxAccountScope) error,
) error {
	return scope.WithTxScope(ctx, fn)
}

type Executor struct {
	runner transactionRunner
	now    func() time.Time
	ttl    time.Duration
}

func NewExecutor() *Executor {
	return newExecutor(accountScopeRunner{}, time.Now, defaultTTL)
}

func newExecutor(runner transactionRunner, now func() time.Time, ttl time.Duration) *Executor {
	return &Executor{runner: runner, now: now, ttl: ttl}
}

func (e *Executor) ExecuteCreate(
	ctx context.Context,
	scope store.AccountScope,
	operation Operation,
	key string,
	canonicalRequest []byte,
	callback func(store.TxAccountScope) (StoredResponse, error),
) (StoredResponse, error) {
	if err := validateRequest(operation, key, canonicalRequest, callback); err != nil {
		return StoredResponse{}, err
	}
	if e == nil || e.runner == nil || e.now == nil || e.ttl <= 0 {
		return StoredResponse{}, fmt.Errorf("%w: executor 未正确配置", ErrValidation)
	}

	requestHash := hashRequest(canonicalRequest)
	var response StoredResponse
	err := e.runner.Run(ctx, scope, func(tx store.TxAccountScope) error {
		now := e.now().UTC()
		owner := false
		for !owner {
			var err error
			owner, err = claim(ctx, tx, operation, key, requestHash, now.Add(e.ttl))
			if err != nil {
				return err
			}
			if owner {
				break
			}
			record, err := loadRecordForUpdate(ctx, tx, operation, key)
			if errors.Is(err, store.ErrNoRows) {
				if err := ctx.Err(); err != nil {
					return err
				}
				continue
			}
			if err != nil {
				return err
			}
			if !record.ExpiresAt.After(now) {
				if err := replaceExpiredClaim(ctx, tx, operation, key, requestHash, now.Add(e.ttl)); err != nil {
					return err
				}
				owner = true
			} else {
				if record.RequestHash != requestHash {
					return fmt.Errorf("%w: key 已绑定其他请求", ErrConflict)
				}
				if !record.ResponseStatus.Valid || len(record.ResponseBody) == 0 {
					return errors.New("idempotency record 缺成功响应")
				}
				response = StoredResponse{
					Status: int(record.ResponseStatus.Int64),
					Body:   append([]byte(nil), record.ResponseBody...),
				}
				return nil
			}
		}

		if owner {
			created, err := callback(tx)
			if err != nil {
				return err
			}
			if created.Status < 200 || created.Status >= 300 || !json.Valid(created.Body) {
				return ErrInvalidResponse
			}
			if err := storeSuccess(ctx, tx, operation, key, requestHash, created, now.Add(e.ttl)); err != nil {
				return err
			}
			response = StoredResponse{Status: created.Status, Body: append([]byte(nil), created.Body...)}
		}
		return nil
	})
	if err != nil {
		return StoredResponse{}, err
	}
	return response, nil
}

func validateRequest(
	operation Operation,
	key string,
	canonicalRequest []byte,
	callback func(store.TxAccountScope) (StoredResponse, error),
) error {
	if operation != OperationOrderCreate && operation != OperationScheduleSlotCreate {
		return fmt.Errorf("%w: operation 非法", ErrValidation)
	}
	if len(key) < 8 || len(key) > 128 || !keyPattern.MatchString(key) {
		return fmt.Errorf("%w: key 非法", ErrValidation)
	}
	if !json.Valid(canonicalRequest) {
		return fmt.Errorf("%w: canonical request 必须是 JSON", ErrValidation)
	}
	if callback == nil {
		return fmt.Errorf("%w: callback 必填", ErrValidation)
	}
	return nil
}

func hashRequest(canonicalRequest []byte) string {
	sum := sha256.Sum256(canonicalRequest)
	return hex.EncodeToString(sum[:])
}

func claim(
	ctx context.Context,
	tx store.TxAccountScope,
	operation Operation,
	key, requestHash string,
	expiresAt time.Time,
) (bool, error) {
	var claimedOperation string
	err := tx.InsertOnConflictDoNothingReturning(
		ctx,
		"idempotency_records",
		[]string{"operation", "key", "request_hash", "expires_at"},
		[]string{"account_id", "operation", "key"},
		[]string{"operation"},
		string(operation),
		key,
		requestHash,
		expiresAt,
	).Scan(&claimedOperation)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim idempotency key: %w", err)
	}
	return true, nil
}

type storedRecord struct {
	RequestHash    string
	ResponseStatus sql.NullInt64
	ResponseBody   []byte
	ExpiresAt      time.Time
}

func loadRecordForUpdate(
	ctx context.Context,
	tx store.TxAccountScope,
	operation Operation,
	key string,
) (storedRecord, error) {
	var record storedRecord
	err := tx.QueryRowForUpdate(
		ctx,
		"idempotency_records",
		"request_hash, response_status, response_body, expires_at",
		"operation = $2 AND key = $3",
		string(operation),
		key,
	).Scan(&record.RequestHash, &record.ResponseStatus, &record.ResponseBody, &record.ExpiresAt)
	if err != nil {
		return storedRecord{}, fmt.Errorf("load idempotency key: %w", err)
	}
	return record, nil
}

func replaceExpiredClaim(
	ctx context.Context,
	tx store.TxAccountScope,
	operation Operation,
	key, requestHash string,
	expiresAt time.Time,
) error {
	deleted, err := tx.Delete(
		ctx,
		"idempotency_records",
		"operation = $2 AND key = $3",
		string(operation),
		key,
	)
	if err != nil {
		return fmt.Errorf("delete expired idempotency key: %w", err)
	}
	if deleted != 1 {
		return errors.New("expired idempotency record disappeared while locked")
	}
	owner, err := claim(ctx, tx, operation, key, requestHash, expiresAt)
	if err != nil {
		return err
	}
	if !owner {
		return errors.New("failed to replace locked expired idempotency record")
	}
	return nil
}

func storeSuccess(
	ctx context.Context,
	tx store.TxAccountScope,
	operation Operation,
	key, requestHash string,
	response StoredResponse,
	expiresAt time.Time,
) error {
	updated, err := tx.Update(
		ctx,
		"idempotency_records",
		"response_status = $2, response_body = $3, expires_at = $4",
		"operation = $5 AND key = $6 AND request_hash = $7",
		response.Status,
		response.Body,
		expiresAt,
		string(operation),
		key,
		requestHash,
	)
	if err != nil {
		return fmt.Errorf("store idempotency response: %w", err)
	}
	if updated != 1 {
		return errors.New("idempotency claim missing before success")
	}
	return nil
}
