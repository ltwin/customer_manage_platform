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
	"github.com/samson/customer-manage-platform/backend/internal/platform/txcap"
)

type Operation string

const (
	OperationOrderCreate                           Operation = "order.create.v1"
	OperationScheduleSlotCreate                    Operation = "schedule-slot.create.v1"
	OperationShootPlanCreate                       Operation = "shoot-plan.create.v1"
	OperationShootPlanCommand                      Operation = "shoot-plan.command.v1"
	OperationShootPlanTransition                   Operation = "shoot-plan.transition.v1"
	OperationRunSessionOpen                        Operation = "shoot-plan.run-session.open.v1"
	OperationShotCapture                           Operation = "shoot-plan.shot.capture.v1"
	OperationExecutionEventVoid                    Operation = "shoot-plan.execution-event.void.v1"
	OperationShootPlanBatch                        Operation = "shoot-plan.batch-commit.v1"
	OperationPlanningMediaUpload                   Operation = "planning-media.upload.v1"
	OperationPlanningMediaBindingCreate            Operation = "planning-media.binding.create.v1"
	OperationPlanningMediaBindingRelease           Operation = "planning-media.binding.release.v1"
	OperationPlanningMediaLeaseReserve             Operation = "planning-media.lease.reserve.v1"
	OperationPlanningMediaLeaseRelease             Operation = "planning-media.lease.release.v1"
	OperationPlanIngestionCreate                   Operation = "plan-ingestion.create.v1"
	OperationPlanIngestionPreview                  Operation = "plan-ingestion.preview.v1"
	OperationPlanIngestionTransition               Operation = "plan-ingestion.transition.v1"
	OperationPlanIngestionCommit                   Operation = "plan-ingestion.commit.v1"
	OperationShootPlanCRMLink                      Operation = "shoot-plan.crm-link.v1"
	OperationPlanShareIssue                        Operation = "plan-share.issue.v1"
	OperationPlanShareRotate                       Operation = "plan-share.rotate.v1"
	OperationPlanShareRevoke                       Operation = "plan-share.revoke.v1"
	OperationPlanShareFeedbackDisposition          Operation = "plan-share.feedback-disposition.v1"
	OperationPlanShareFeedbackPlanCreate           Operation = "plan-share.feedback-plan-create.v1"
	OperationPlanShareFeedbackShotCreate           Operation = "plan-share.feedback-shot-create.v1"
	OperationPlanShareOfferCreate                  Operation = "plan-share.offer-create.v1"
	OperationPlanShareOfferClose                   Operation = "plan-share.offer-close.v1"
	OperationPlanShareAssignmentPhotographerRevoke Operation = "plan-share.assignment-photographer-revoke.v1"
	OperationPlanShareAssignmentClaim              Operation = "plan-share.assignment-claim.v1"
	OperationPlanShareAssignmentSelfRevoke         Operation = "plan-share.assignment-self-revoke.v1"
	OperationShootPlanBusinessFacts                Operation = "shoot-plan.business-facts.v1"
	OperationShootPlanBusinessDraftGenerate        Operation = "shoot-plan.business-drafts.generate.v1"
	OperationShootPlanBusinessDraftDecision        Operation = "shoot-plan.business-draft.decision.v1"
	defaultTTL                                               = 24 * time.Hour
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

type ResourceIdentity struct {
	kind        string
	primaryID   string
	secondaryID string
}

type Request struct {
	Operation        Operation
	Key              string
	ResourceIdentity ResourceIdentity
	CanonicalBody    []byte
	// ExactFrameHash, when set, is the ledger request_hash (hex SHA-256 of the
	// caller-owned exact frame bytes). Anonymous mutations must set this to the
	// hash of CanonicalAnonymousMutationFrameV1Bytes so ledger and admission
	// consume the same slice without a second wrap.
	ExactFrameHash string
}

func PlanCollectionResource() ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-collection"}
}
func PlanResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan", primaryID: planID}
}
func TransitionResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-transition", primaryID: planID}
}
func RunSessionResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-run-session", primaryID: planID}
}
func ShotCaptureResource(planID, shotID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-shot", primaryID: planID, secondaryID: shotID}
}
func EventVoidResource(planID, eventID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-event", primaryID: planID, secondaryID: eventID}
}
func BatchResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-batch", primaryID: planID}
}
func BusinessDraftResource(planID, draftID string) ResourceIdentity {
	return ResourceIdentity{kind: "shoot-plan-business-draft", primaryID: planID, secondaryID: draftID}
}

func PlanningMediaUploadResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "planning-media-upload", primaryID: planID}
}
func PlanningMediaBindingResource(planID, assetID string) ResourceIdentity {
	return ResourceIdentity{kind: "planning-media-binding", primaryID: planID, secondaryID: assetID}
}
func PlanIngestionCreateResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-ingestion-create", primaryID: planID}
}
func PlanIngestionSessionResource(planID, sessionID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-ingestion-session", primaryID: planID, secondaryID: sessionID}
}
func ShareIssueResource(planID, viewLevel string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-issue", primaryID: planID, secondaryID: viewLevel}
}
func ShareGenerationResource(planID, shareID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-generation", primaryID: planID, secondaryID: shareID}
}
func FeedbackResource(planID, feedbackID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-feedback", primaryID: planID, secondaryID: feedbackID}
}
func AnonymousPlanFeedbackResource(planID, generationID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-plan-feedback", primaryID: planID, secondaryID: generationID}
}
func AnonymousShotFeedbackResource(planID, secondaryTuple string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-shot-feedback", primaryID: planID, secondaryID: secondaryTuple}
}
func OfferCollectionResource(planID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-offer-collection", primaryID: planID}
}
func OfferResource(planID, offerID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-offer", primaryID: planID, secondaryID: offerID}
}
func AssignmentResource(planID, assignmentID string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-assignment", primaryID: planID, secondaryID: assignmentID}
}
func AssignmentClaimResource(planID, secondaryTuple string) ResourceIdentity {
	return ResourceIdentity{kind: "plan-share-assignment-claim", primaryID: planID, secondaryID: secondaryTuple}
}

func (r ResourceIdentity) Kind() string        { return r.kind }
func (r ResourceIdentity) PrimaryID() string   { return r.primaryID }
func (r ResourceIdentity) SecondaryID() string { return r.secondaryID }

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
	return e.executeWithRunner(ctx, scope, operation, key, hashRequest(canonicalRequest), callback)
}

func (e *Executor) Execute(
	ctx context.Context,
	scope store.AccountScope,
	request Request,
	callback func(store.TxAccountScope) (StoredResponse, error),
) (StoredResponse, error) {
	requestHash, err := validateAndHashTypedRequest(request)
	if err != nil || callback == nil {
		if err == nil {
			err = fmt.Errorf("%w: callback 必填", ErrValidation)
		}
		return StoredResponse{}, err
	}
	return e.executeWithRunner(ctx, scope, request.Operation, request.Key, requestHash, callback)
}

func (e *Executor) ExecuteInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	request Request,
	callback func(store.TxAccountScope) (StoredResponse, error),
) (StoredResponse, error) {
	requestHash, err := validateAndHashTypedRequest(request)
	if err != nil || callback == nil {
		if err == nil {
			err = fmt.Errorf("%w: callback 必填", ErrValidation)
		}
		return StoredResponse{}, err
	}
	if err := e.validateConfigured(false); err != nil {
		return StoredResponse{}, err
	}
	return e.executeInTransaction(ctx, tx.IdempotencyLedgerView(), request.Operation, request.Key, requestHash, func() (StoredResponse, error) {
		return callback(tx)
	})
}

func ExecuteInCapability[T any](
	ctx context.Context,
	executor *Executor,
	runner txcap.TransactionRunner[T],
	capability txcap.ShareTransactionCapability,
	request Request,
	callback func(T) (StoredResponse, error),
) (StoredResponse, error) {
	requestHash, err := validateAndHashTypedRequest(request)
	if err != nil || executor == nil || runner == nil || capability == nil || callback == nil {
		if err == nil {
			err = fmt.Errorf("%w: capability executor 参数不完整", ErrValidation)
		}
		return StoredResponse{}, err
	}
	if err := executor.validateConfigured(false); err != nil {
		return StoredResponse{}, err
	}
	var response StoredResponse
	err = runner.Run(ctx, capability, func(ledger txcap.LedgerTxView, scope T) error {
		var runErr error
		response, runErr = executor.executeInTransaction(ctx, ledger, request.Operation, request.Key, requestHash, func() (StoredResponse, error) {
			return callback(scope)
		})
		return runErr
	})
	if err != nil {
		return StoredResponse{}, err
	}
	return response, nil
}

func (e *Executor) executeWithRunner(
	ctx context.Context,
	scope store.AccountScope,
	operation Operation,
	key, requestHash string,
	callback func(store.TxAccountScope) (StoredResponse, error),
) (StoredResponse, error) {
	if err := e.validateConfigured(true); err != nil {
		return StoredResponse{}, err
	}
	var response StoredResponse
	err := e.runner.Run(ctx, scope, func(tx store.TxAccountScope) error {
		var err error
		response, err = e.executeInTransaction(ctx, tx.IdempotencyLedgerView(), operation, key, requestHash, func() (StoredResponse, error) {
			return callback(tx)
		})
		return err
	})
	if err != nil {
		return StoredResponse{}, err
	}
	return response, nil
}

func (e *Executor) executeInTransaction(
	ctx context.Context,
	ledger txcap.LedgerTxView,
	operation Operation,
	key, requestHash string,
	callback func() (StoredResponse, error),
) (StoredResponse, error) {
	if ledger == nil {
		return StoredResponse{}, fmt.Errorf("%w: ledger transaction view 缺失", ErrValidation)
	}
	now := e.now().UTC()
	owner := false
	for !owner {
		var err error
		owner, err = claim(ctx, ledger, operation, key, requestHash, now.Add(e.ttl))
		if err != nil {
			return StoredResponse{}, err
		}
		if owner {
			break
		}
		record, err := loadRecordForUpdate(ctx, ledger, operation, key)
		if errors.Is(err, store.ErrNoRows) {
			if err := ctx.Err(); err != nil {
				return StoredResponse{}, err
			}
			continue
		}
		if err != nil {
			return StoredResponse{}, err
		}
		if !record.ExpiresAt.After(now) {
			if err := replaceExpiredClaim(ctx, ledger, operation, key, requestHash, now.Add(e.ttl)); err != nil {
				return StoredResponse{}, err
			}
			owner = true
			continue
		}
		if record.RequestHash != requestHash {
			return StoredResponse{}, fmt.Errorf("%w: key 已绑定其他请求", ErrConflict)
		}
		if !record.ResponseStatus.Valid || len(record.ResponseBody) == 0 {
			return StoredResponse{}, errors.New("idempotency record 缺成功响应")
		}
		return StoredResponse{Status: int(record.ResponseStatus.Int64), Body: append([]byte(nil), record.ResponseBody...)}, nil
	}

	created, err := callback()
	if err != nil {
		return StoredResponse{}, err
	}
	if created.Status < 200 || created.Status >= 300 || !json.Valid(created.Body) {
		return StoredResponse{}, ErrInvalidResponse
	}
	if err := storeSuccess(ctx, ledger, operation, key, requestHash, created, now.Add(e.ttl)); err != nil {
		return StoredResponse{}, err
	}
	return StoredResponse{Status: created.Status, Body: append([]byte(nil), created.Body...)}, nil
}

func (e *Executor) validateConfigured(requireRunner bool) error {
	if e == nil || e.now == nil || e.ttl <= 0 || requireRunner && e.runner == nil {
		return fmt.Errorf("%w: executor 未正确配置", ErrValidation)
	}
	return nil
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

func validateAndHashTypedRequest(request Request) (string, error) {
	if len(request.Key) < 8 || len(request.Key) > 128 || !keyPattern.MatchString(request.Key) {
		return "", fmt.Errorf("%w: key 非法", ErrValidation)
	}
	if !json.Valid(request.CanonicalBody) {
		return "", fmt.Errorf("%w: canonical body 必须是 JSON", ErrValidation)
	}
	if !resourceMatchesOperation(request.Operation, request.ResourceIdentity) {
		return "", fmt.Errorf("%w: operation 与 resource identity 不匹配", ErrValidation)
	}
	if request.ExactFrameHash != "" {
		if len(request.ExactFrameHash) != 64 {
			return "", fmt.Errorf("%w: exact frame hash 非法", ErrValidation)
		}
		for _, r := range request.ExactFrameHash {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				return "", fmt.Errorf("%w: exact frame hash 非法", ErrValidation)
			}
		}
		return request.ExactFrameHash, nil
	}
	frame := struct {
		FrameVersion int             `json:"frame_version"`
		Operation    Operation       `json:"operation"`
		Resource     resourceFrame   `json:"resource"`
		Body         json.RawMessage `json:"body"`
	}{
		FrameVersion: 1,
		Operation:    request.Operation,
		Resource: resourceFrame{
			Kind:        request.ResourceIdentity.kind,
			PrimaryID:   request.ResourceIdentity.primaryID,
			SecondaryID: request.ResourceIdentity.secondaryID,
		},
		Body: json.RawMessage(request.CanonicalBody),
	}
	canonical, err := json.Marshal(frame)
	if err != nil {
		return "", fmt.Errorf("%w: canonical frame: %v", ErrValidation, err)
	}
	return hashRequest(canonical), nil
}

type resourceFrame struct {
	Kind        string `json:"kind"`
	PrimaryID   string `json:"primary_id,omitempty"`
	SecondaryID string `json:"secondary_id,omitempty"`
}

func resourceMatchesOperation(operation Operation, identity ResourceIdentity) bool {
	primary := identity.primaryID != ""
	secondary := identity.secondaryID != ""
	switch operation {
	case OperationShootPlanCreate:
		return identity.kind == "shoot-plan-collection" && !primary && !secondary
	case OperationShootPlanCommand:
		return identity.kind == "shoot-plan" && primary && !secondary
	case OperationShootPlanTransition:
		return identity.kind == "shoot-plan-transition" && primary && !secondary
	case OperationRunSessionOpen:
		return identity.kind == "shoot-plan-run-session" && primary && !secondary
	case OperationShotCapture:
		return identity.kind == "shoot-plan-shot" && primary && secondary
	case OperationExecutionEventVoid:
		return identity.kind == "shoot-plan-event" && primary && secondary
	case OperationShootPlanBatch:
		return identity.kind == "shoot-plan-batch" && primary && !secondary
	case OperationPlanningMediaUpload:
		return identity.kind == "planning-media-upload" && primary && !secondary
	case OperationPlanningMediaBindingCreate, OperationPlanningMediaBindingRelease,
		OperationPlanningMediaLeaseReserve, OperationPlanningMediaLeaseRelease:
		return identity.kind == "planning-media-binding" && primary && secondary
	case OperationPlanIngestionCreate:
		return identity.kind == "plan-ingestion-create" && primary && !secondary
	case OperationPlanIngestionPreview, OperationPlanIngestionTransition, OperationPlanIngestionCommit:
		return identity.kind == "plan-ingestion-session" && primary && secondary
	case OperationShootPlanCRMLink, OperationShootPlanBusinessFacts, OperationShootPlanBusinessDraftGenerate:
		return identity.kind == "shoot-plan" && primary && !secondary
	case OperationShootPlanBusinessDraftDecision:
		return identity.kind == "shoot-plan-business-draft" && primary && secondary
	case OperationPlanShareIssue:
		return identity.kind == "plan-share-issue" && primary && secondary
	case OperationPlanShareRotate, OperationPlanShareRevoke:
		return identity.kind == "plan-share-generation" && primary && secondary
	case OperationPlanShareFeedbackDisposition:
		return identity.kind == "plan-share-feedback" && primary && secondary
	case OperationPlanShareFeedbackPlanCreate:
		return identity.kind == "plan-share-plan-feedback" && primary && secondary
	case OperationPlanShareFeedbackShotCreate:
		return identity.kind == "plan-share-shot-feedback" && primary && secondary
	case OperationPlanShareOfferCreate:
		return identity.kind == "plan-share-offer-collection" && primary && !secondary
	case OperationPlanShareOfferClose:
		return identity.kind == "plan-share-offer" && primary && secondary
	case OperationPlanShareAssignmentPhotographerRevoke, OperationPlanShareAssignmentSelfRevoke:
		return identity.kind == "plan-share-assignment" && primary && secondary
	case OperationPlanShareAssignmentClaim:
		return identity.kind == "plan-share-assignment-claim" && primary && secondary
	default:
		return false
	}
}

func hashRequest(canonicalRequest []byte) string {
	sum := sha256.Sum256(canonicalRequest)
	return hex.EncodeToString(sum[:])
}

func claim(
	ctx context.Context,
	tx txcap.LedgerTxView,
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
	tx txcap.LedgerTxView,
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
	tx txcap.LedgerTxView,
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
	tx txcap.LedgerTxView,
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
