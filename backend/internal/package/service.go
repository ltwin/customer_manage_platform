package pkgcatalog

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	MaxPageSize        = 100
	maxPostgresInteger = 1<<31 - 1
)

type Repository interface {
	Create(context.Context, store.AccountScope, CreateInput) (Package, error)
	List(context.Context, store.AccountScope, ListFilter) (ListResult, error)
	Update(context.Context, store.AccountScope, string, UpdateInput) (Package, error)
	Delete(context.Context, store.AccountScope, string) error
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (Package, error) {
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return Package{}, err
	}
	return s.repo.Create(ctx, scope, normalized)
}

func (s *Service) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	normalized, err := normalizeListFilter(filter)
	if err != nil {
		return ListResult{}, err
	}
	return s.repo.List(ctx, scope, normalized)
}

func (s *Service) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Package, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Package{}, ValidationError{Message: "套系 id 必填"}
	}
	normalized, err := normalizeUpdateInput(input)
	if err != nil {
		return Package{}, err
	}
	return s.repo.Update(ctx, scope, id, normalized)
}

func (s *Service) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return ValidationError{Message: "套系 id 必填"}
	}
	return s.repo.Delete(ctx, scope, id)
}

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ShootType = strings.TrimSpace(input.ShootType)
	input.PricingMode = strings.TrimSpace(input.PricingMode)
	input.Note = trimmedPtr(input.Note)
	if input.Name == "" {
		return CreateInput{}, ValidationError{Message: "name 必填"}
	}
	if !validShootType(input.ShootType) {
		return CreateInput{}, ValidationError{Message: "shoot_type 非法"}
	}
	if !validPricingMode(input.PricingMode) {
		return CreateInput{}, ValidationError{Message: "pricing_mode 非法"}
	}
	if err := validatePackageInteger(input.BasePrice, "base_price"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.DurationMinutes, "duration_minutes"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.ShotCountMin, "shot_count_min"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.ShotCountMax, "shot_count_max"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.RawDeliveryCount, "raw_delivery_count"); err != nil {
		return CreateInput{}, err
	}
	if err := validateOptionalIntPtr(input.RetouchCount, "retouch_count"); err != nil {
		return CreateInput{}, err
	}
	if err := validateShotRange(input.ShotCountMin, input.ShotCountMax); err != nil {
		return CreateInput{}, err
	}
	if err := validateNote(input.Note); err != nil {
		return CreateInput{}, err
	}
	return input, nil
}

func normalizeUpdateInput(input UpdateInput) (UpdateInput, error) {
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if trimmed == "" {
			return UpdateInput{}, ValidationError{Message: "name 不能为空"}
		}
		input.Name = &trimmed
	}
	if input.ShootType != nil {
		trimmed := strings.TrimSpace(*input.ShootType)
		if !validShootType(trimmed) {
			return UpdateInput{}, ValidationError{Message: "shoot_type 非法"}
		}
		input.ShootType = &trimmed
	}
	if input.PricingMode != nil {
		trimmed := strings.TrimSpace(*input.PricingMode)
		if !validPricingMode(trimmed) {
			return UpdateInput{}, ValidationError{Message: "pricing_mode 非法"}
		}
		input.PricingMode = &trimmed
	}
	if input.BasePrice != nil {
		if err := validatePackageInteger(*input.BasePrice, "base_price"); err != nil {
			return UpdateInput{}, err
		}
	}
	var err error
	if input.DurationMinutes, err = normalizeClearableInt(input.DurationMinutes, "duration_minutes"); err != nil {
		return UpdateInput{}, err
	}
	if input.ShotCountMin, err = normalizeClearableInt(input.ShotCountMin, "shot_count_min"); err != nil {
		return UpdateInput{}, err
	}
	if input.ShotCountMax, err = normalizeClearableInt(input.ShotCountMax, "shot_count_max"); err != nil {
		return UpdateInput{}, err
	}
	if input.RawDeliveryCount, err = normalizeClearableInt(input.RawDeliveryCount, "raw_delivery_count"); err != nil {
		return UpdateInput{}, err
	}
	if input.RetouchCount, err = normalizeClearableInt(input.RetouchCount, "retouch_count"); err != nil {
		return UpdateInput{}, err
	}
	if input.ShotCountMin.IsSpecified() && input.ShotCountMax.IsSpecified() {
		if err := validateShotRange(nullableIntPtr(input.ShotCountMin), nullableIntPtr(input.ShotCountMax)); err != nil {
			return UpdateInput{}, err
		}
	}
	if input.Note != nil {
		trimmed := strings.TrimSpace(*input.Note)
		input.Note = &trimmed
		if err := validateNote(input.Note); err != nil {
			return UpdateInput{}, err
		}
	}
	if input.Status != nil {
		trimmed := strings.TrimSpace(*input.Status)
		if trimmed != StatusActive && trimmed != StatusArchived {
			return UpdateInput{}, ValidationError{Message: "status 只能是 active 或 archived"}
		}
		input.Status = &trimmed
	}
	return input, nil
}

func normalizeListFilter(filter ListFilter) (ListFilter, error) {
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Status == "" {
		filter.Status = StatusActive
	}
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
	if !validListStatus(filter.Status) {
		return ListFilter{}, ValidationError{Message: "status 非法"}
	}
	return filter, nil
}

func normalizeClearableInt(value nullable.Nullable[int], field string) (nullable.Nullable[int], error) {
	if !value.IsSpecified() || value.IsNull() {
		return value, nil
	}
	if err := validatePackageInteger(value.MustGet(), field); err != nil {
		return value, err
	}
	return value, nil
}

func validateOptionalIntPtr(value *int, field string) error {
	if value != nil {
		return validatePackageInteger(*value, field)
	}
	return nil
}

func validatePackageInteger(value int, field string) error {
	if value < 0 {
		return ValidationError{Message: field + " 不能为负"}
	}
	if value > maxPostgresInteger {
		return ValidationError{Message: field + " 超出范围"}
	}
	return nil
}

func validateShotRange(minValue, maxValue *int) error {
	if minValue != nil && maxValue != nil && *minValue > *maxValue {
		return ValidationError{Message: "shot_count_min 不能大于 shot_count_max"}
	}
	return nil
}

func validateNote(note *string) error {
	if note != nil && utf8.RuneCountInString(*note) > 500 {
		return ValidationError{Message: "note 不能超过 500 字"}
	}
	return nil
}

func validShootType(value string) bool {
	switch value {
	case ShootTypePortrait, ShootTypeCosplay, ShootTypeOther:
		return true
	default:
		return false
	}
}

func validPricingMode(value string) bool {
	switch value {
	case PricingModePerDuration, PricingModePerPhoto, PricingModeFixed:
		return true
	default:
		return false
	}
}

func validListStatus(value string) bool {
	switch value {
	case StatusActive, StatusArchived, StatusAll:
		return true
	default:
		return false
	}
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

func nullableIntPtr(value nullable.Nullable[int]) *int {
	if !value.IsSpecified() || value.IsNull() {
		return nil
	}
	v := value.MustGet()
	return &v
}
