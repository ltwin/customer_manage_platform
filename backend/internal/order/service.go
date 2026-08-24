package order

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	MaxPageSize        = 100
	maxPostgresInteger = 1<<31 - 1
)

type Repository interface {
	Create(context.Context, store.AccountScope, PreparedCreate) (Order, error)
	CreatePreparedInScope(context.Context, store.TxAccountScope, PreparedCreate) (Order, error)
	List(context.Context, store.AccountScope, ListFilter) (ListResult, error)
	Update(context.Context, store.AccountScope, string, UpdateInput) (Order, error)
	Delete(context.Context, store.AccountScope, string) error
}

// DeliveryPolicyProvider 提供账号级应交付日派生上下文（时区 + 默认 SLA 天数）。
// 由 composition root 注入 settings 域实现；order 域不反向 import settings。
type DeliveryPolicyProvider interface {
	DeliveryPolicyForAccount(context.Context, store.AccountScope) (DeliveryPolicy, error)
}

type Service struct {
	repo           Repository
	now            func() time.Time
	deliveryPolicy DeliveryPolicyProvider
}

func NewService(repo Repository) *Service {
	return NewServiceWithClock(repo, time.Now)

}

func NewServiceWithClock(repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repo: repo, now: now}
}

// WithDeliveryPolicyProvider 注入应交付日派生上下文来源；未注入时不派生（只接受显式覆盖）。
func (s *Service) WithDeliveryPolicyProvider(provider DeliveryPolicyProvider) *Service {
	s.deliveryPolicy = provider
	return s
}

// resolveDeliveryPolicy 取账号派生上下文；未注入 provider 时返回未就绪策略。
func (s *Service) resolveDeliveryPolicy(ctx context.Context, scope store.AccountScope) (DeliveryPolicy, error) {
	if s.deliveryPolicy == nil {
		return DeliveryPolicy{}, nil
	}
	return s.deliveryPolicy.DeliveryPolicyForAccount(ctx, scope)
}

func (s *Service) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (Order, error) {
	policy, err := s.resolveDeliveryPolicy(ctx, scope)
	if err != nil {
		return Order{}, err
	}
	prepared, err := s.prepareCreateWithPolicy(input, policy)
	if err != nil {
		return Order{}, err
	}
	return s.repo.Create(ctx, scope, prepared)
}

// PrepareCreateInScope 在已有账号 scope 下准备创建输入，使组合流程（档期建单）也能派生应交付日。
func (s *Service) PrepareCreateInScope(
	ctx context.Context,
	scope store.AccountScope,
	input CreateInput,
) (PreparedCreate, error) {
	policy, err := s.resolveDeliveryPolicy(ctx, scope)
	if err != nil {
		return PreparedCreate{}, err
	}
	return s.prepareCreateWithPolicy(input, policy)
}

func (s *Service) prepareCreateWithPolicy(input CreateInput, policy DeliveryPolicy) (PreparedCreate, error) {
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return PreparedCreate{}, err
	}
	normalized.deliveryPolicy = policy
	initial, err := ApplyCreateInput(normalized)
	if err != nil {
		return PreparedCreate{}, err
	}
	return PreparedCreate{Input: normalized, initial: initial}, nil
}

func (s *Service) CreatePreparedInScope(
	ctx context.Context,
	scope store.TxAccountScope,
	prepared PreparedCreate,
) (Order, error) {
	if prepared.initial.Status == "" {
		return Order{}, ValidationError{Message: "订单创建输入未准备"}
	}
	return s.repo.CreatePreparedInScope(ctx, scope, prepared)
}

func (s *Service) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	normalized, err := normalizeListFilter(filter)
	if err != nil {
		return ListResult{}, err
	}
	if normalized.SchedulableAt != nil {
		normalized.schedulableNow = s.now().UTC()
	}
	return s.repo.List(ctx, scope, normalized)
}

func (s *Service) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Order, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Order{}, ValidationError{Message: "订单 id 必填"}
	}
	normalized, err := normalizeUpdateInput(input)
	if err != nil {
		return Order{}, err
	}
	if normalized.deliveryPolicy, err = s.resolveDeliveryPolicy(ctx, scope); err != nil {
		return Order{}, err
	}
	return s.repo.Update(ctx, scope, id, normalized)
}

func (s *Service) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ValidationError{Message: "订单 id 必填"}
	}
	return s.repo.Delete(ctx, scope, id)
}

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	input.CreationMode = strings.TrimSpace(input.CreationMode)
	if input.CreationMode == "" {
		input.CreationMode = CreationModeNew
	}
	if input.CreationMode != CreationModeNew && input.CreationMode != CreationModeBackfill {
		return CreateInput{}, ValidationError{Message: "creation_mode 非法"}
	}
	input.CustomerID = strings.TrimSpace(input.CustomerID)
	input.PackageID = trimmedPtr(input.PackageID)
	input.Title = trimmedPtr(input.Title)
	input.Note = trimmedPtr(input.Note)
	if input.Status != nil {
		trimmed := strings.TrimSpace(*input.Status)
		input.Status = &trimmed
	}
	if input.CustomerID == "" {
		return CreateInput{}, ValidationError{Message: "customer_id 必填"}
	}
	if err := validateOptionalIntPtr(input.Price, "price"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.AmountPaid, "amount_paid"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.OutstandingAmount, "outstanding_amount"); err != nil {
		return CreateInput{}, err
	}
	if err := validateNote(input.Note); err != nil {
		return CreateInput{}, err
	}
	return input, nil
}

func normalizeUpdateInput(input UpdateInput) (UpdateInput, error) {
	if input.Status != nil {
		trimmed := strings.TrimSpace(*input.Status)
		if !validStatus(trimmed) {
			return UpdateInput{}, ValidationError{Message: "status 非法"}
		}
		input.Status = &trimmed
	}
	input.Title = trimmedPtr(input.Title)
	input.Note = trimmedPtr(input.Note)
	if err := validateNote(input.Note); err != nil {
		return UpdateInput{}, err
	}
	var err error
	if input.Price, err = normalizeClearableInt(input.Price, "price"); err != nil {
		return UpdateInput{}, err
	}
	if input.AmountPaid, err = normalizeClearableInt(input.AmountPaid, "amount_paid"); err != nil {
		return UpdateInput{}, err
	}
	if input.OutstandingAmount, err = normalizeClearableInt(input.OutstandingAmount, "outstanding_amount"); err != nil {
		return UpdateInput{}, err
	}
	return input, nil
}

func normalizeListFilter(filter ListFilter) (ListFilter, error) {
	filter.ID = strings.TrimSpace(filter.ID)
	filter.CustomerID = strings.TrimSpace(filter.CustomerID)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 20
	}
	if filter.Page < 1 {
		return ListFilter{}, ValidationError{Message: "page 必须大于 0"}
	}
	if filter.PageSize < 1 {
		return ListFilter{}, ValidationError{Message: "page_size 必须大于 0"}
	}
	if filter.PageSize > MaxPageSize {
		return ListFilter{}, ValidationError{Message: "page_size 不能超过 100"}
	}
	if filter.Page-1 > maxPostgresInteger/filter.PageSize {
		return ListFilter{}, ValidationError{Message: "page 超出范围"}
	}
	if filter.Status != "" && !validStatus(filter.Status) {
		return ListFilter{}, ValidationError{Message: "status 非法"}
	}
	if filter.SchedulableAt != nil {
		value := filter.SchedulableAt.UTC()
		filter.SchedulableAt = &value
	}
	return filter, nil
}

func normalizeClearableInt(value nullable.Nullable[int], field string) (nullable.Nullable[int], error) {
	if !value.IsSpecified() || value.IsNull() {
		return value, nil
	}
	if err := validateOrderInteger(value.MustGet(), field); err != nil {
		return value, err
	}
	return value, nil
}

func validateOptionalIntPtr(value *int, field string) error {
	if value != nil {
		return validateOrderInteger(*value, field)
	}
	return nil
}

func validateOrderInteger(value int, field string) error {
	if value < 0 {
		return ValidationError{Message: field + " 不能为负"}
	}
	if value > maxPostgresInteger {
		return ValidationError{Message: field + " 超出范围"}
	}
	return nil
}

func validateNote(note *string) error {
	if note != nil && utf8.RuneCountInString(*note) > 500 {
		return ValidationError{Message: "note 不能超过 500 字"}
	}
	return nil
}

func trimmedPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
