package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/planshare"
	"github.com/samson/customer-manage-platform/backend/internal/platform/auth"
	"github.com/samson/customer-manage-platform/backend/internal/platform/idempotency"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/business"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
	shootplanningapi "github.com/samson/customer-manage-platform/backend/internal/shootplanning/httpcontract"
)

const maxShootPlanningRequestBytes = 1 << 20
const shootPlanningRawBodyKey = "shoot-planning-raw-body"

type shootPlanningHandlers struct {
	app          *shootplanning.Application
	planShare    *planshare.Application
	business     *business.Application
	scopeFactory ScopeFactory
}

var _ shootplanningapi.ServerInterface = (*shootPlanningHandlers)(nil)

func (h *shootPlanningHandlers) ListShootPlans(c *gin.Context, params shootplanningapi.ListShootPlansParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if !onlyQueryParameters(c, "status", "archived", "page", "page_size", "customer_id", "order_id", "q", "sort") ||
		(params.Status != nil && !params.Status.Valid()) ||
		(params.Sort != nil && !params.Sort.Valid()) {
		abortShootPlanningValidation(c)
		return
	}
	filter := shootplanning.ListPlansFilter{Page: 1, PageSize: 20}
	if params.Q != nil {
		keyword, ok := shootplanning.PlanListKeyword(*params.Q)
		if !ok {
			abortShootPlanningValidation(c)
			return
		}
		filter.Q = keyword
	}
	if params.Sort != nil {
		filter.Sort = shootplanning.PlanListSort(*params.Sort)
	}
	if params.Status != nil {
		status := shootplanning.PlanStatus(*params.Status)
		filter.Status = &status
	}
	if params.Archived != nil {
		filter.ArchivedOnly = *params.Archived
	}
	if params.Page != nil {
		filter.Page = *params.Page
	}
	if params.PageSize != nil {
		filter.PageSize = *params.PageSize
	}
	if params.CustomerId != nil {
		filter.CustomerID = *params.CustomerId
	}
	if params.OrderId != nil {
		filter.OrderID = *params.OrderId
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.app.ListPlans(c.Request.Context(), scope, filter)
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *shootPlanningHandlers) CreateShootPlan(c *gin.Context, params shootplanningapi.CreateShootPlanParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	var body shootplanningapi.CreateShootPlanInput
	if err := decodeStrictRequest(c, &body, "title", "subject"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	detail, err := h.app.CreatePlan(c.Request.Context(), scope, params.IdempotencyKey, shootplanning.CreatePlanInput{
		Title: body.Title, Subject: body.Subject,
	})
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, detail)
}

func (h *shootPlanningHandlers) GetShootPlan(c *gin.Context, id shootplanningapi.Id, params shootplanningapi.GetShootPlanParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if !onlyQueryParameters(c, "include") || (params.Include != nil && !params.Include.Valid()) {
		abortShootPlanningValidation(c)
		return
	}
	includeHistory := params.Include != nil && *params.Include == shootplanningapi.ExecutionHistory
	detail, err := h.app.GetPlan(c.Request.Context(), scope, id, includeHistory)
	if h.abortError(c, err) {
		return
	}
	if h.business == nil {
		_ = c.Error(errors.New("shoot planning business dependencies missing"))
		return
	}
	businessDetail, err := h.business.GetDetail(c.Request.Context(), scope, id)
	if h.abortBusinessError(c, err) {
		return
	}
	c.JSON(http.StatusOK, struct {
		shootplanning.PlanDetail
		Business business.Detail `json:"business"`
	}{PlanDetail: detail, Business: businessDetail})
}

func (h *shootPlanningHandlers) ApplyShootPlanCommand(c *gin.Context, id shootplanningapi.Id, params shootplanningapi.ApplyShootPlanCommandParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	expectedRevision, crmCommand, businessCommand, command, err := decodeShootPlanMutation(c)
	if err != nil {
		abortShootPlanningValidation(c)
		return
	}
	var result shootplanning.PlanMutationResult
	if businessCommand != nil {
		if h.business == nil {
			_ = c.Error(errors.New("shoot planning business dependencies missing"))
			return
		}
		businessResult, businessErr := h.business.SetFacts(
			c.Request.Context(), scope, params.IdempotencyKey, id,
			businessCommand.expectedPlanRevision, businessCommand.expectedFactsRevision,
			businessCommand.facts,
		)
		if h.abortBusinessError(c, businessErr) {
			return
		}
		c.JSON(http.StatusOK, businessResult)
		return
	} else if crmCommand != nil {
		result, err = h.app.ApplyCRMLink(c.Request.Context(), scope, params.IdempotencyKey, id, expectedRevision, *crmCommand)
	} else {
		result, err = h.app.ApplyPlanCommand(c.Request.Context(), scope, params.IdempotencyKey, id, expectedRevision, command)
	}
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *shootPlanningHandlers) GenerateShootPlanBusinessDrafts(
	c *gin.Context,
	id shootplanningapi.Id,
	params shootplanningapi.GenerateShootPlanBusinessDraftsParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.business == nil {
		_ = c.Error(errors.New("shoot planning business dependencies missing"))
		return
	}
	var body shootplanningapi.GenerateBusinessDraftsInput
	if err := decodeStrictRequest(c, &body, "expected_plan_revision", "expected_business_facts_revision", "draft_kinds"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	kinds := make([]business.DraftKind, 0, len(body.DraftKinds))
	for _, kind := range body.DraftKinds {
		kinds = append(kinds, business.DraftKind(kind))
	}
	result, err := h.business.Generate(c.Request.Context(), scope, params.IdempotencyKey, id, business.GenerateInput{
		ExpectedPlanRevision:  body.ExpectedPlanRevision,
		ExpectedFactsRevision: body.ExpectedBusinessFactsRevision,
		DraftKinds:            kinds,
		AbsoluteTargetPrice:   nullablePointer(body.AbsoluteTargetPrice),
	})
	if h.abortBusinessError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *shootPlanningHandlers) DecideShootPlanBusinessDraft(
	c *gin.Context,
	id shootplanningapi.Id,
	draftID string,
	params shootplanningapi.DecideShootPlanBusinessDraftParams,
) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	if h.business == nil {
		_ = c.Error(errors.New("shoot planning business dependencies missing"))
		return
	}
	var body shootplanningapi.BusinessDraftDecisionInput
	if err := decodeStrictRequest(c, &body, "expected_draft_revision", "decision"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	var acknowledgement *business.Acknowledgement
	if body.Acknowledgement != nil {
		acknowledgement = &business.Acknowledgement{
			Version: string(body.Acknowledgement.Version),
			Effects: append([]string(nil), body.Acknowledgement.Effects...),
		}
	}
	result, err := h.business.Decide(c.Request.Context(), scope, params.IdempotencyKey, id, draftID, business.DecisionInput{
		ExpectedDraftRevision: body.ExpectedDraftRevision,
		Decision:              business.Decision(body.Decision),
		Acknowledgement:       acknowledgement,
	})
	if h.abortBusinessError(c, err) {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *shootPlanningHandlers) VoidShootPlanExecutionEvent(c *gin.Context, id shootplanningapi.Id, eventID string, params shootplanningapi.VoidShootPlanExecutionEventParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	var body shootplanningapi.VoidExecutionEventInput
	if err := decodeStrictRequest(c, &body, "expected_execution_revision", "reason"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.app.VoidExecutionEvent(c.Request.Context(), scope, params.IdempotencyKey, id, eventID, shootplanning.VoidExecutionEventInput{
		ExpectedExecutionRevision: body.ExpectedExecutionRevision,
		Reason:                    body.Reason,
	})
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *shootPlanningHandlers) OpenShootPlanRunSession(c *gin.Context, id shootplanningapi.Id, params shootplanningapi.OpenShootPlanRunSessionParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	var body shootplanningapi.OpenShootPlanRunSessionJSONBody
	if err := decodeStrictRequest(c, &body, "expected_revision"); err != nil {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.app.OpenRunSession(c.Request.Context(), scope, params.IdempotencyKey, id, body.ExpectedRevision)
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *shootPlanningHandlers) AppendShootPlanShotResult(c *gin.Context, id shootplanningapi.Id, shotID string, params shootplanningapi.AppendShootPlanShotResultParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	var body shootplanningapi.AppendShotResultInput
	if err := decodeStrictRequest(c, &body, "expected_execution_revision", "result"); err != nil ||
		rejectNullRequestFields(c, "skip_reason") != nil || !body.Result.Valid() ||
		(body.SkipReason != nil && !body.SkipReason.Valid()) {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.app.AppendShotResult(c.Request.Context(), scope, params.IdempotencyKey, id, shotID, shootplanning.AppendShotResultInput{
		ExpectedExecutionRevision: body.ExpectedExecutionRevision,
		SessionID:                 nullablePointer(body.SessionId),
		Result:                    shootplanning.ShotResult(body.Result),
		SkipReason:                enumPointer(body.SkipReason),
		Notes:                     nullablePointer(body.Notes),
		SupersedesEventID:         nullablePointer(body.SupersedesEventId),
	})
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *shootPlanningHandlers) TransitionShootPlan(c *gin.Context, id shootplanningapi.Id, params shootplanningapi.TransitionShootPlanParams) {
	scope, ok := h.scope(c)
	if !ok {
		return
	}
	transition, err := decodePlanTransition(c)
	if err != nil {
		abortShootPlanningValidation(c)
		return
	}
	result, err := h.app.TransitionPlan(c.Request.Context(), scope, params.IdempotencyKey, id, transition)
	if h.abortError(c, err) {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *shootPlanningHandlers) scope(c *gin.Context) (store.AccountScope, bool) {
	account, ok := auth.AccountContextFrom(c.Request.Context())
	if !ok {
		abortError(c, http.StatusUnauthorized, CodeUnauthorized, "未认证")
		return store.AccountScope{}, false
	}
	if h.app == nil || h.scopeFactory == nil {
		_ = c.Error(errors.New("shoot planning route dependencies missing"))
		return store.AccountScope{}, false
	}
	return creativeLegacyScope(c, h.scopeFactory.ScopeFor(account)), true
}

func (h *shootPlanningHandlers) abortError(c *gin.Context, err error) bool {
	if abortLegacyWriteError(c, err) {
		return true
	}
	if err == nil {
		return false
	}
	var acknowledgementError *shootplanning.ArchiveAcknowledgementRequiredError
	if errors.As(err, &acknowledgementError) {
		details, detailsErr := archiveAcknowledgementErrorDetails(acknowledgementError.Required)
		if detailsErr != nil {
			_ = c.Error(detailsErr)
			return true
		}
		abortErrorWithTypedDetails(c, http.StatusConflict, CodeArchiveAcknowledgementRequired,
			"归档影响确认已变化，请刷新后重新确认", details)
		return true
	}
	type conflict struct {
		err     error
		code    string
		message string
	}
	conflicts := []conflict{
		{shootplanning.ErrPlanRevisionConflict, CodePlanRevisionConflict, "策划版本已变化，请刷新后重试"},
		{shootplanning.ErrExecutionRevisionConflict, CodeExecutionRevisionConflict, "镜头执行版本已变化，请刷新后重试"},
		{shootplanning.ErrInvalidPlanTransition, CodeInvalidPlanTransition, "当前状态不允许此操作"},
		{shootplanning.ErrReadinessIncomplete, CodeReadinessIncomplete, "必需准备项尚未完成"},
		{shootplanning.ErrReadinessAssignmentActive, CodeReadinessAssignmentActive, "准备项仍有关联中的认领"},
		{shootplanning.ErrShotsIncomplete, CodeShotsIncomplete, "仍有镜头未记录执行结果"},
		{shootplanning.ErrArchivedReadOnly, CodeArchivedReadOnly, "已归档策划不可修改"},
		{shootplanning.ErrReopenRequired, CodeReopenRequired, "已完成策划需重新打开后才能修改"},
		{shootplanning.ErrExecutionHistoryAckRequired, CodeExecutionHistoryAckRequired, "移除前需确认保留执行历史"},
		{shootplanning.ErrExecutionEventAlreadyVoid, CodeExecutionEventAlreadyVoid, "执行事实已作废"},
		{shootplanning.ErrSupersedesMismatch, CodeSupersedesEventMismatch, "被替代事实与当前结果不一致"},
		{idempotency.ErrConflict, CodeIdempotencyConflict, "幂等键已被其他请求使用"},
		{crm.ErrCustomerLinkConflict, CodeCustomerLinkConflict, "已关联其他客户，请先解除后再关联"},
		{crm.ErrOrderLinkConflict, CodeOrderLinkConflict, "已关联其他订单，请先解除后再关联"},
		{crm.ErrProjectionRevisionConflict, CodeProjectionRevisionConflict, "档期投影版本已变化，请刷新后重试"},
		{crm.ErrProjectionMissing, CodeProjectionMissing, "当前没有可采纳的档期投影"},
		{crm.ErrProjectionNotActive, CodeProjectionNotActive, "当前档期投影不可采纳"},
		{crm.ErrProjectionNotFuture, CodeProjectionNotFuture, "档期已结束，不能作为未来拍摄时间"},
		{crm.ErrSourceChangedRetry, CodeSourceChanged, "关联的客户或订单刚刚发生变化，请刷新后重试"},
		{crm.ErrSourceChanged, CodeSourceChanged, "关联的客户或订单刚刚发生变化，请刷新后重试"},
		{crm.ErrOrderStillLinked, CodeOrderStillLinked, "请先解除订单关联再解除客户"},
		{crm.ErrCustomerMerged, CodeCustomerMerged, "客户已合并，档案只读"},
	}
	for _, item := range conflicts {
		if errors.Is(err, item.err) {
			abortError(c, http.StatusConflict, item.code, item.message)
			return true
		}
	}
	if errors.Is(err, shootplanning.ErrPlanNotFound) || errors.Is(err, shootplanning.ErrShotNotFound) ||
		errors.Is(err, shootplanning.ErrReadinessNotFound) || errors.Is(err, shootplanning.ErrRunSessionNotFound) ||
		errors.Is(err, shootplanning.ErrExecutionEventNotFound) || errors.Is(err, crm.ErrNotFound) {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return true
	}
	if errors.Is(err, shootplanning.ErrValidation) || errors.Is(err, idempotency.ErrValidation) || errors.Is(err, crm.ErrValidation) {
		abortShootPlanningValidation(c)
		return true
	}
	_ = c.Error(fmt.Errorf("shoot planning request: %w", err))
	return true
}

func (h *shootPlanningHandlers) abortBusinessError(c *gin.Context, err error) bool {
	if abortLegacyWriteError(c, err) {
		return true
	}
	if err == nil {
		return false
	}
	var unavailable *business.DraftUnavailableError
	if errors.As(err, &unavailable) {
		details, detailsErr := businessDraftUnavailableErrorDetails(unavailable)
		if detailsErr != nil {
			_ = c.Error(detailsErr)
			return true
		}
		abortErrorWithTypedDetails(c, http.StatusConflict, "business_draft_unavailable",
			"当前信息不足以生成经营草稿", details)
		return true
	}
	var stale *business.StaleDraftError
	if errors.As(err, &stale) {
		details, detailsErr := staleBusinessDraftErrorDetails(stale)
		if detailsErr != nil {
			_ = c.Error(detailsErr)
			return true
		}
		abortErrorWithTypedDetails(c, http.StatusConflict, "stale_business_draft",
			"经营草稿已过期，请重新生成", details)
		return true
	}
	if errors.Is(err, business.ErrNotFound) {
		abortError(c, http.StatusNotFound, CodeNotFound, "资源不存在")
		return true
	}
	if errors.Is(err, business.ErrInvalidBusinessFact) || errors.Is(err, business.ErrInvalidBusinessRule) ||
		errors.Is(err, business.ErrDraftDecision) || errors.Is(err, business.ErrInvalidInput) {
		abortError(c, http.StatusBadRequest, CodeValidationFailed, "经营草稿请求不合法")
		return true
	}
	type businessConflict struct {
		err     error
		code    string
		message string
	}
	conflicts := []businessConflict{
		{business.ErrPlanRevisionConflict, "plan_revision_conflict", "策划版本已变化，请刷新后重试"},
		{business.ErrRevisionConflict, "business_revision_conflict", "经营信息版本已变化，请刷新后重试"},
		{business.ErrDraftUnavailable, "business_draft_unavailable", "当前信息不足以生成经营草稿"},
		{business.ErrDraftStale, "stale_business_draft", "经营草稿已过期，请重新生成"},
		{business.ErrNoMaterialChange, "business_draft_no_material_change", "目标内容无需更新"},
		{business.ErrUnknownTotal, "business_draft_unknown_total", "订单建议总价尚不明确"},
		{business.ErrScheduleCreate, "business_draft_requires_schedule_create", "请前往日历选择拍摄时间"},
		{business.ErrReopenRequired, CodeReopenRequired, "已完成策划需重新打开后才能修改"},
		{business.ErrArchivedReadOnly, CodeArchivedReadOnly, "已归档策划不可修改"},
		{idempotency.ErrConflict, CodeIdempotencyConflict, "幂等键已被其他请求使用"},
	}
	for _, conflict := range conflicts {
		if errors.Is(err, conflict.err) {
			abortError(c, http.StatusConflict, conflict.code, conflict.message)
			return true
		}
	}
	_ = c.Error(err)
	return true
}

func businessDraftUnavailableErrorDetails(source *business.DraftUnavailableError) (ErrorDetails, error) {
	details := BusinessDraftUnavailableDetails{}
	if source.OrderAdjustment != nil {
		details.OrderAdjustment = &UnavailableOrderBusinessDraftItem{
			State:  UnavailableOrderBusinessDraftItemStateUnavailable,
			Reason: OrderBusinessDraftUnavailableReason(*source.OrderAdjustment),
		}
	}
	if source.ScheduleDuration != nil {
		details.ScheduleDuration = &UnavailableScheduleBusinessDraftItem{
			State:  UnavailableScheduleBusinessDraftItemStateUnavailable,
			Reason: ScheduleBusinessDraftUnavailableReason(*source.ScheduleDuration),
		}
	}
	var union ErrorDetails
	err := union.FromBusinessDraftUnavailableDetails(details)
	return union, err
}

func staleBusinessDraftErrorDetails(source *business.StaleDraftError) (ErrorDetails, error) {
	var union ErrorDetails
	err := union.FromStaleBusinessDraftDetails(StaleBusinessDraftDetails{
		DraftId:     source.DraftID,
		Kind:        StaleBusinessDraftDetailsKind(source.Kind),
		Revision:    source.Revision,
		StaleReason: StaleBusinessDraftDetailsStaleReason(source.Reason),
		Status:      Stale,
	})
	return union, err
}

func archiveAcknowledgementErrorDetails(required shootplanning.ArchiveAcknowledgement) (ErrorDetails, error) {
	body, err := json.Marshal(required)
	if err != nil {
		return ErrorDetails{}, err
	}
	var acknowledgement ArchiveAcknowledgement
	if err := json.Unmarshal(body, &acknowledgement); err != nil {
		return ErrorDetails{}, err
	}
	var details ErrorDetails
	err = details.FromArchiveAcknowledgementRequiredDetails(ArchiveAcknowledgementRequiredDetails{
		RequiredArchiveAcknowledgement: acknowledgement,
	})
	return details, err
}

func abortShootPlanningValidation(c *gin.Context) {
	abortError(c, http.StatusBadRequest, CodeValidationFailed, "请求参数不合法")
}

func onlyQueryParameters(c *gin.Context, allowed ...string) bool {
	allow := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allow[name] = struct{}{}
	}
	for name, values := range c.Request.URL.Query() {
		if _, ok := allow[name]; !ok || len(values) != 1 {
			return false
		}
	}
	return true
}

func decodeStrictRequest(c *gin.Context, destination any, required ...string) error {
	if c.ContentType() != "application/json" {
		return errors.New("content type must be application/json")
	}
	body, err := readRequestBody(c)
	if err != nil {
		return err
	}
	c.Set(shootPlanningRawBodyKey, body)
	if err := requireJSONFields(body, required...); err != nil {
		return err
	}
	return decodeStrictJSON(body, destination)
}

func readRequestBody(c *gin.Context) ([]byte, error) {
	reader := http.MaxBytesReader(c.Writer, c.Request.Body, maxShootPlanningRequestBytes)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("request body is required")
	}
	if err := rejectDuplicateJSONKeys(body); err != nil {
		return nil, err
	}
	return body, nil
}

func rejectDuplicateJSONKeys(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			if _, exists := seen[key]; exists {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("JSON array is not closed")
		}
	default:
		return errors.New("unexpected closing JSON delimiter")
	}
	return nil
}

func decodeStrictJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}

func requireJSONFields(body []byte, names ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("JSON object is required")
	}
	for _, name := range names {
		value, ok := object[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("field %s is required", name)
		}
	}
	return nil
}

func requireJSONKeys(body []byte, names ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return err
	}
	if object == nil {
		return errors.New("JSON object is required")
	}
	for _, name := range names {
		if _, ok := object[name]; !ok {
			return fmt.Errorf("field %s is required", name)
		}
	}
	return nil
}

func rejectNullFields(body []byte, names ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return err
	}
	for _, name := range names {
		if value, ok := object[name]; ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("field %s cannot be null", name)
		}
	}
	return nil
}

func rejectNullRequestFields(c *gin.Context, names ...string) error {
	value, ok := c.Get(shootPlanningRawBodyKey)
	if !ok {
		return errors.New("request body is unavailable")
	}
	body, ok := value.([]byte)
	if !ok {
		return errors.New("request body has invalid type")
	}
	return rejectNullFields(body, names...)
}

func jsonObjectField(body []byte, name string) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return nil, err
	}
	value, ok := object[name]
	if !ok {
		return nil, fmt.Errorf("field %s is required", name)
	}
	return value, nil
}

type decodedBusinessFactsCommand struct {
	expectedPlanRevision  int64
	expectedFactsRevision int64
	facts                 business.Facts
}

func decodeShootPlanMutation(c *gin.Context) (int64, *crm.Command, *decodedBusinessFactsCommand, shootplanning.PlanCommand, error) {
	if c.ContentType() != "application/json" {
		return 0, nil, nil, nil, errors.New("content type must be application/json")
	}
	body, err := readRequestBody(c)
	if err != nil {
		return 0, nil, nil, nil, err
	}
	if err := requireJSONFields(body, "expected_revision", "operation"); err != nil {
		return 0, nil, nil, nil, err
	}
	var discriminator struct {
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(body, &discriminator); err != nil {
		return 0, nil, nil, nil, err
	}
	switch discriminator.Operation {
	case "link_customer", "link_order", "unlink_order", "unlink_customer", "adopt_schedule_projection":
		command, err := decodeCRMCommand(body, discriminator.Operation)
		if err != nil {
			return 0, nil, nil, nil, err
		}
		return command.expected, &command.command, nil, nil, nil
	case "set_business_facts":
		if err := requireJSONFields(body, "expected_business_facts_revision", "facts"); err != nil {
			return 0, nil, nil, nil, err
		}
		factsBody, err := jsonObjectField(body, "facts")
		if err != nil {
			return 0, nil, nil, nil, err
		}
		if err := requireJSONKeys(factsBody, "rented_location_count", "assistant_count", "retouched_photo_count", "estimated_duration_minutes"); err != nil {
			return 0, nil, nil, nil, err
		}
		var request shootplanningapi.SetBusinessFactsPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.SetBusinessFacts {
			return 0, nil, nil, nil, invalidUnion(err)
		}
		decoded := &decodedBusinessFactsCommand{
			expectedPlanRevision:  request.ExpectedRevision,
			expectedFactsRevision: request.ExpectedBusinessFactsRevision,
			facts: business.Facts{
				RentedLocationCount:      nullablePointer(request.Facts.RentedLocationCount),
				AssistantCount:           nullablePointer(request.Facts.AssistantCount),
				RetouchedPhotoCount:      nullablePointer(request.Facts.RetouchedPhotoCount),
				EstimatedDurationMinutes: nullablePointer(request.Facts.EstimatedDurationMinutes),
			},
		}
		return request.ExpectedRevision, nil, decoded, nil, nil
	default:
		c.Set(shootPlanningRawBodyKey, body)
		revision, planCommand, err := decodePlanCommandFromBody(body)
		return revision, nil, nil, planCommand, err
	}
}

type decodedCRMCommand struct {
	expected int64
	command  crm.Command
}

func decodeCRMCommand(body []byte, operation string) (decodedCRMCommand, error) {
	switch operation {
	case "link_customer":
		if err := requireJSONFields(body, "customer_id"); err != nil {
			return decodedCRMCommand{}, err
		}
		var request shootplanningapi.LinkCustomerCrmCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.LinkCustomer {
			return decodedCRMCommand{}, invalidUnion(err)
		}
		return decodedCRMCommand{expected: request.ExpectedRevision, command: crm.Command{Kind: crm.KindLinkCustomer, CustomerID: &request.CustomerId}}, nil
	case "link_order":
		if err := requireJSONFields(body, "order_id"); err != nil {
			return decodedCRMCommand{}, err
		}
		var request shootplanningapi.LinkOrderCrmCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.LinkOrder {
			return decodedCRMCommand{}, invalidUnion(err)
		}
		return decodedCRMCommand{expected: request.ExpectedRevision, command: crm.Command{Kind: crm.KindLinkOrder, OrderID: &request.OrderId}}, nil
	case "unlink_order":
		var request shootplanningapi.UnlinkOrderCrmCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.UnlinkOrder {
			return decodedCRMCommand{}, invalidUnion(err)
		}
		return decodedCRMCommand{expected: request.ExpectedRevision, command: crm.Command{Kind: crm.KindUnlinkOrder}}, nil
	case "unlink_customer":
		var request shootplanningapi.UnlinkCustomerCrmCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.UnlinkCustomer {
			return decodedCRMCommand{}, invalidUnion(err)
		}
		return decodedCRMCommand{expected: request.ExpectedRevision, command: crm.Command{Kind: crm.KindUnlinkCustomer}}, nil
	case "adopt_schedule_projection":
		if err := requireJSONFields(body, "projection_revision"); err != nil {
			return decodedCRMCommand{}, err
		}
		var request shootplanningapi.AdoptScheduleProjectionCrmCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.AdoptScheduleProjection {
			return decodedCRMCommand{}, invalidUnion(err)
		}
		revision := request.ProjectionRevision
		return decodedCRMCommand{expected: request.ExpectedRevision, command: crm.Command{Kind: crm.KindAdopt, ProjectionRevision: &revision}}, nil
	default:
		return decodedCRMCommand{}, errors.New("unknown crm command discriminator")
	}
}

func decodePlanCommandFromBody(body []byte) (int64, shootplanning.PlanCommand, error) {
	if err := requireJSONFields(body, "expected_revision", "operation"); err != nil {
		return 0, nil, err
	}
	var discriminator struct {
		Operation string `json:"operation"`
	}
	if err := json.Unmarshal(body, &discriminator); err != nil {
		return 0, nil, err
	}
	switch discriminator.Operation {
	case "update_brief":
		if err := rejectNullFields(body, "title", "subject", "creative_brief"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.UpdateBriefPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.UpdateBrief {
			return 0, nil, invalidUnion(err)
		}
		if request.Title == nil && request.Subject == nil && request.CreativeBrief == nil {
			return 0, nil, errors.New("empty brief patch")
		}
		if request.CreativeBrief != nil && !creativeBriefPatchSpecified(*request.CreativeBrief) {
			return 0, nil, errors.New("empty creative brief patch")
		}
		return request.ExpectedRevision, shootplanning.UpdateBriefCommand{
			Title: request.Title, Subject: request.Subject, CreativeBrief: creativeBriefPatch(request.CreativeBrief),
		}, nil
	case "upsert_shot":
		if err := requireJSONFields(body, "shot"); err != nil {
			return 0, nil, err
		}
		if err := rejectNullFields(body, "shot_id"); err != nil {
			return 0, nil, err
		}
		shotBody, err := jsonObjectField(body, "shot")
		if err != nil {
			return 0, nil, err
		}
		if err := rejectNullFields(shotBody, "title"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.UpsertShotPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.UpsertShot {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.UpsertShotCommand{
			ShotID: request.ShotId, Shot: shotWrite(request.Shot), InsertAfterShotID: nullablePointer(request.InsertAfterShotId),
		}, nil
	case "reorder_shots":
		if err := requireJSONFields(body, "ordered_shot_ids"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.ReorderShotsPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.ReorderShots {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.ReorderShotsCommand{OrderedShotIDs: request.OrderedShotIds}, nil
	case "remove_shot":
		if err := requireJSONFields(body, "shot_id", "acknowledge_execution_history"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.RemoveShotPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.RemoveShot {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.RemoveShotCommand{ShotID: request.ShotId, AcknowledgeExecutionHistory: request.AcknowledgeExecutionHistory}, nil
	case "upsert_readiness":
		if err := requireJSONFields(body, "item"); err != nil {
			return 0, nil, err
		}
		if err := rejectNullFields(body, "readiness_id"); err != nil {
			return 0, nil, err
		}
		itemBody, err := jsonObjectField(body, "item")
		if err != nil {
			return 0, nil, err
		}
		if err := rejectNullFields(itemBody, "category", "title", "requirement", "preflight_status", "responsibility_hint"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.UpsertReadinessPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.UpsertReadiness {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.UpsertReadinessCommand{ReadinessID: request.ReadinessId, Item: readinessWrite(request.Item)}, nil
	case "remove_readiness":
		if err := requireJSONFields(body, "readiness_id"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.RemoveReadinessPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.RemoveReadiness {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.RemoveReadinessCommand{ReadinessID: request.ReadinessId}, nil
	case "set_preflight":
		if err := requireJSONFields(body, "readiness_id", "preflight_status"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.SetPreflightPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.SetPreflight {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.SetPreflightCommand{ReadinessID: request.ReadinessId, PreflightStatus: string(request.PreflightStatus)}, nil
	case "link_readiness":
		if err := requireJSONFields(body, "shot_id", "readiness_id"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.LinkReadinessPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.LinkReadiness {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.LinkReadinessCommand{ShotID: request.ShotId, ReadinessID: request.ReadinessId}, nil
	case "unlink_readiness":
		if err := requireJSONFields(body, "shot_id", "readiness_id"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.UnlinkReadinessPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.UnlinkReadiness {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.UnlinkReadinessCommand{ShotID: request.ShotId, ReadinessID: request.ReadinessId}, nil
	case "set_public_scale":
		var request shootplanningapi.SetPublicScalePlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.SetPublicScale {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.SetPublicScaleCommand{
			PlannedLookCount: optionalValue(request.PlannedLookCount), PlannedSceneCount: optionalValue(request.PlannedSceneCount),
		}, nil
	case "set_execution_window":
		if err := requireJSONFields(body, "starts_at", "ends_at", "timezone", "live_window_starts_at", "live_window_ends_at"); err != nil {
			return 0, nil, err
		}
		var request shootplanningapi.SetExecutionWindowPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.SetExecutionWindow {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.SetExecutionWindowCommand{
			StartsAt: request.StartsAt, EndsAt: request.EndsAt, Timezone: request.Timezone,
			LiveWindowStartsAt: request.LiveWindowStartsAt, LiveWindowEndsAt: request.LiveWindowEndsAt,
		}, nil
	case "clear_execution_window":
		var request shootplanningapi.ClearExecutionWindowPlanCommand
		if err := decodeStrictJSON(body, &request); err != nil || request.Operation != shootplanningapi.ClearExecutionWindow {
			return 0, nil, invalidUnion(err)
		}
		return request.ExpectedRevision, shootplanning.ClearExecutionWindowCommand{}, nil
	default:
		return 0, nil, errors.New("unknown plan command discriminator")
	}
}

func decodePlanTransition(c *gin.Context) (shootplanning.PlanTransition, error) {
	if c.ContentType() != "application/json" {
		return shootplanning.PlanTransition{}, errors.New("content type must be application/json")
	}
	body, err := readRequestBody(c)
	if err != nil {
		return shootplanning.PlanTransition{}, err
	}
	if err := requireJSONFields(body, "expected_revision", "transition", "payload"); err != nil {
		return shootplanning.PlanTransition{}, err
	}
	var discriminator struct {
		Transition string          `json:"transition"`
		Payload    json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(body, &discriminator); err != nil {
		return shootplanning.PlanTransition{}, err
	}
	switch discriminator.Transition {
	case "mark_ready":
		var request shootplanningapi.MarkReadyPlanTransition
		if err := decodeStrictJSON(body, &request); err != nil || request.Transition != shootplanningapi.MarkReady {
			return shootplanning.PlanTransition{}, invalidUnion(err)
		}
		if len(request.Payload) != 0 {
			return shootplanning.PlanTransition{}, errors.New("mark_ready payload must be empty")
		}
		return shootplanning.PlanTransition{ExpectedRevision: request.ExpectedRevision, Kind: shootplanning.TransitionMarkReady}, nil
	case "start":
		var request shootplanningapi.StartPlanTransition
		if err := decodeStrictJSON(body, &request); err != nil || request.Transition != shootplanningapi.Start {
			return shootplanning.PlanTransition{}, invalidUnion(err)
		}
		if len(request.Payload) != 0 {
			return shootplanning.PlanTransition{}, errors.New("start payload must be empty")
		}
		return shootplanning.PlanTransition{ExpectedRevision: request.ExpectedRevision, Kind: shootplanning.TransitionStart}, nil
	case "complete":
		var request shootplanningapi.CompletePlanTransition
		if err := decodeStrictJSON(body, &request); err != nil || request.Transition != shootplanningapi.Complete {
			return shootplanning.PlanTransition{}, invalidUnion(err)
		}
		if err := requireJSONFields(discriminator.Payload, "expected_execution_fact_revision"); err != nil {
			return shootplanning.PlanTransition{}, err
		}
		executionRevision := request.Payload.ExpectedExecutionFactRevision
		return shootplanning.PlanTransition{ExpectedRevision: request.ExpectedRevision, Kind: shootplanning.TransitionComplete, ExpectedExecutionFactRevision: &executionRevision}, nil
	case "reopen":
		var request shootplanningapi.ReopenPlanTransition
		if err := decodeStrictJSON(body, &request); err != nil || request.Transition != shootplanningapi.Reopen {
			return shootplanning.PlanTransition{}, invalidUnion(err)
		}
		if len(request.Payload) != 0 {
			return shootplanning.PlanTransition{}, errors.New("reopen payload must be empty")
		}
		return shootplanning.PlanTransition{ExpectedRevision: request.ExpectedRevision, Kind: shootplanning.TransitionReopen}, nil
	case "archive":
		var request shootplanningapi.ArchivePlanTransition
		if err := decodeStrictJSON(body, &request); err != nil || request.Transition != shootplanningapi.Archive {
			return shootplanning.PlanTransition{}, invalidUnion(err)
		}
		acknowledgement, err := decodeArchiveAcknowledgement(discriminator.Payload)
		if err != nil {
			return shootplanning.PlanTransition{}, err
		}
		return shootplanning.PlanTransition{ExpectedRevision: request.ExpectedRevision, Kind: shootplanning.TransitionArchive, ArchiveAcknowledgement: &acknowledgement}, nil
	default:
		return shootplanning.PlanTransition{}, errors.New("unknown transition discriminator")
	}
}

func decodeArchiveAcknowledgement(body []byte) (shootplanning.ArchiveAcknowledgement, error) {
	if err := requireJSONFields(body, "version", "effects"); err != nil {
		return shootplanning.ArchiveAcknowledgement{}, err
	}
	var discriminator struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &discriminator); err != nil {
		return shootplanning.ArchiveAcknowledgement{}, err
	}
	acknowledgement := shootplanning.ArchiveAcknowledgement{Version: discriminator.Version}
	switch discriminator.Version {
	case "core-v1":
		var value shootplanningapi.CoreArchiveAcknowledgement
		if err := decodeStrictJSON(body, &value); err != nil || !value.Version.Valid() {
			return shootplanning.ArchiveAcknowledgement{}, invalidUnion(err)
		}
		for _, effect := range value.Effects {
			acknowledgement.Effects = append(acknowledgement.Effects, string(effect))
		}
	case "planning-share-v1":
		var value shootplanningapi.PlanningShareArchiveAcknowledgement
		if err := decodeStrictJSON(body, &value); err != nil || !value.Version.Valid() {
			return shootplanning.ArchiveAcknowledgement{}, invalidUnion(err)
		}
		for _, effect := range value.Effects {
			acknowledgement.Effects = append(acknowledgement.Effects, string(effect))
		}
	case "planning-share-reminder-v1":
		var value shootplanningapi.PlanningShareReminderArchiveAcknowledgement
		if err := decodeStrictJSON(body, &value); err != nil || !value.Version.Valid() {
			return shootplanning.ArchiveAcknowledgement{}, invalidUnion(err)
		}
		for _, effect := range value.Effects {
			acknowledgement.Effects = append(acknowledgement.Effects, string(effect))
		}
	default:
		return shootplanning.ArchiveAcknowledgement{}, errors.New("unknown archive acknowledgement version")
	}
	return acknowledgement, nil
}

func invalidUnion(err error) error {
	if err != nil {
		return err
	}
	return errors.New("union discriminator does not match body")
}

func creativeBriefPatchSpecified(value shootplanningapi.CreativeBriefPatch) bool {
	return value.WorkTitle.IsSpecified() || value.CharacterName.IsSpecified() || value.ThemeStatement.IsSpecified() ||
		value.Mood.IsSpecified() || value.VisualKeywords.IsSpecified()
}

func creativeBriefPatch(value *shootplanningapi.CreativeBriefPatch) *shootplanning.CreativeBriefPatch {
	if value == nil {
		return nil
	}
	return &shootplanning.CreativeBriefPatch{
		WorkTitle: optionalValue(value.WorkTitle), CharacterName: optionalValue(value.CharacterName),
		ThemeStatement: optionalValue(value.ThemeStatement), Mood: optionalValue(value.Mood),
		VisualKeywords: optionalValue(value.VisualKeywords),
	}
}

func shotWrite(value shootplanningapi.ShotWrite) shootplanning.ShotWrite {
	return shootplanning.ShotWrite{
		Title: value.Title, Scene: optionalValue(value.Scene), Action: optionalValue(value.Action),
		Expression: optionalValue(value.Expression), Composition: optionalValue(value.Composition),
		Lighting: optionalValue(value.LightingText), Notes: optionalValue(value.Notes),
		FramingTag: optionalString(value.FramingTag), LightingDirectionTag: optionalString(value.LightingDirectionTag),
		LightingQualityTag: optionalString(value.LightingQualityTag), PaletteTag: optionalString(value.PaletteTag),
		ShotTypeTag: optionalString(value.ShotTypeTag),
	}
}

func readinessWrite(value shootplanningapi.ReadinessWrite) shootplanning.ReadinessWrite {
	return shootplanning.ReadinessWrite{
		Category: enumPointer(value.Category), Title: value.Title, Requirement: enumPointer(value.Requirement),
		PreflightStatus: enumPointer(value.PreflightStatus), ResponsibilityHint: enumPointer(value.ResponsibilityHint),
		DefaultPreparationLeadDays: optionalValue(value.DefaultPreparationLeadDays),
	}
}

func optionalValue[T any](value nullable.Nullable[T]) shootplanning.Optional[T] {
	result := shootplanning.Optional[T]{Specified: value.IsSpecified(), Null: value.IsNull()}
	if concrete, err := value.Get(); err == nil {
		result.Value = concrete
	}
	return result
}

func optionalString[T ~string](value nullable.Nullable[T]) shootplanning.Optional[string] {
	result := shootplanning.Optional[string]{Specified: value.IsSpecified(), Null: value.IsNull()}
	if concrete, err := value.Get(); err == nil {
		result.Value = string(concrete)
	}
	return result
}

func nullablePointer[T any](value nullable.Nullable[T]) *T {
	concrete, err := value.Get()
	if err != nil {
		return nil
	}
	return &concrete
}

func enumPointer[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	converted := string(*value)
	return &converted
}

func registerShootPlanningHandlers(
	router gin.IRouter,
	app *shootplanning.Application,
	planShare *planshare.Application,
	businessApp *business.Application,
	scopeFactory ScopeFactory,
) {
	shootplanningapi.RegisterHandlersWithOptions(router, &shootPlanningHandlers{
		app: app, planShare: planShare, business: businessApp, scopeFactory: scopeFactory,
	}, shootplanningapi.GinServerOptions{
		ErrorHandler: func(c *gin.Context, _ error, _ int) {
			abortShootPlanningValidation(c)
		},
	})
}
