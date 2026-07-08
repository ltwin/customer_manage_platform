package customer

import (
	"context"
	"strings"
	"time"

	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type Repository interface {
	Create(context.Context, store.AccountScope, CreateInput) (Customer, error)
	List(context.Context, store.AccountScope, ListFilter) (ListResult, error)
	Detail(context.Context, store.AccountScope, string) (Detail, error)
	Update(context.Context, store.AccountScope, string, UpdateInput) (Customer, error)
	AddIdentity(context.Context, store.AccountScope, string, IdentityInput) (SocialIdentity, error)
	DeleteIdentity(ctx context.Context, scope store.AccountScope, customerID, identityID string) error
	AddNote(ctx context.Context, scope store.AccountScope, customerID, content string) (CustomerNote, error)
	Merge(ctx context.Context, scope store.AccountScope, targetID, sourceID string) (Customer, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (Customer, error) {
	normalized, err := normalizeCreateInput(input)
	if err != nil {
		return Customer{}, err
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

func (s *Service) Detail(ctx context.Context, scope store.AccountScope, id string) (Detail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Detail{}, ValidationError{Message: "客户 id 必填"}
	}
	return s.repo.Detail(ctx, scope, id)
}

func (s *Service) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Customer, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Customer{}, ValidationError{Message: "客户 id 必填"}
	}
	normalized, err := normalizeUpdateInput(id, input)
	if err != nil {
		return Customer{}, err
	}
	return s.repo.Update(ctx, scope, id, normalized)
}

func (s *Service) AddIdentity(ctx context.Context, scope store.AccountScope, customerID string, input IdentityInput) (SocialIdentity, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return SocialIdentity{}, ValidationError{Message: "客户 id 必填"}
	}
	input.Platform = strings.TrimSpace(input.Platform)
	input.Handle = strings.TrimSpace(input.Handle)
	input.Remark = trimmedPtr(input.Remark)
	if !validPlatform(input.Platform) {
		return SocialIdentity{}, ValidationError{Message: "identity.platform 非法"}
	}
	if input.Handle == "" {
		return SocialIdentity{}, ValidationError{Message: "identity.handle 必填"}
	}
	return s.repo.AddIdentity(ctx, scope, customerID, input)
}

func (s *Service) DeleteIdentity(ctx context.Context, scope store.AccountScope, customerID, identityID string) error {
	customerID = strings.TrimSpace(customerID)
	identityID = strings.TrimSpace(identityID)
	if customerID == "" || identityID == "" {
		return ValidationError{Message: "客户 id 与身份 id 必填"}
	}
	return s.repo.DeleteIdentity(ctx, scope, customerID, identityID)
}

func (s *Service) AddNote(ctx context.Context, scope store.AccountScope, customerID, content string) (CustomerNote, error) {
	customerID = strings.TrimSpace(customerID)
	if customerID == "" {
		return CustomerNote{}, ValidationError{Message: "客户 id 必填"}
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return CustomerNote{}, ValidationError{Message: "content 必填"}
	}
	if len([]rune(content)) > 500 {
		return CustomerNote{}, ValidationError{Message: "content 不能超过 500 字"}
	}
	return s.repo.AddNote(ctx, scope, customerID, content)
}

func (s *Service) Merge(ctx context.Context, scope store.AccountScope, targetID, sourceID string) (Customer, error) {
	targetID = strings.TrimSpace(targetID)
	sourceID = strings.TrimSpace(sourceID)
	if targetID == "" || sourceID == "" {
		return Customer{}, ValidationError{Message: "target 与 source 客户 id 必填"}
	}
	if targetID == sourceID {
		return Customer{}, ValidationError{Message: "不能把客户 merge 进自己"}
	}
	return s.repo.Merge(ctx, scope, targetID, sourceID)
}

// normalizeUpdateInput 做无需当前档案状态的静态校验；依赖当前 channel / status 的
// 联动规则（单独传 referrer、merged 只读）在 repository 事务内完成。
func normalizeUpdateInput(id string, input UpdateInput) (UpdateInput, error) {
	if input.DisplayName != nil {
		trimmed := strings.TrimSpace(*input.DisplayName)
		if trimmed == "" {
			return UpdateInput{}, ValidationError{Message: "display_name 不能为空"}
		}
		input.DisplayName = &trimmed
	}
	var err error
	if input.RealName, err = normalizeClearable(input.RealName, "real_name"); err != nil {
		return UpdateInput{}, err
	}
	if input.Phone, err = normalizeClearable(input.Phone, "phone"); err != nil {
		return UpdateInput{}, err
	}
	if input.Birthday.IsSpecified() && !input.Birthday.IsNull() {
		value := strings.TrimSpace(input.Birthday.MustGet())
		if !validBirthday(value) {
			return UpdateInput{}, ValidationError{Message: "birthday 格式非法（MM-DD 或 YYYY-MM-DD）"}
		}
		input.Birthday.Set(value)
	}
	if input.Channel != nil {
		trimmed := strings.TrimSpace(*input.Channel)
		if !validChannel(trimmed) {
			return UpdateInput{}, ValidationError{Message: "channel 非法"}
		}
		input.Channel = &trimmed
	}
	if input.Status != nil {
		trimmed := strings.TrimSpace(*input.Status)
		// merged 是 merge 端点专属终态（design D4），PATCH 不接受。
		if trimmed != StatusActive && trimmed != StatusArchived {
			return UpdateInput{}, ValidationError{Message: "status 只能是 active 或 archived"}
		}
		input.Status = &trimmed
	}
	input.ReferrerCustomerID = trimmedPtr(input.ReferrerCustomerID)
	if input.Channel != nil && *input.Channel == ChannelReferral && input.ReferrerCustomerID == nil {
		return UpdateInput{}, ValidationError{Message: "介绍人必填"}
	}
	if input.ReferrerCustomerID != nil && *input.ReferrerCustomerID == id {
		return UpdateInput{}, ValidationError{Message: "介绍人不能是本客户自身"}
	}
	if input.ReferrerCustomerID != nil && input.Channel != nil && *input.Channel != ChannelReferral {
		return UpdateInput{}, ValidationError{Message: "channel 非 referral 不接受 referrer_customer_id"}
	}
	return input, nil
}

// normalizeClearable 归一可清空字段的「传值」分支：空白值拒绝，清空必须显式传 null。
func normalizeClearable(value nullable.Nullable[string], field string) (nullable.Nullable[string], error) {
	if !value.IsSpecified() || value.IsNull() {
		return value, nil
	}
	trimmed := strings.TrimSpace(value.MustGet())
	if trimmed == "" {
		return value, ValidationError{Message: field + " 不能为空（清空请传 null）"}
	}
	value.Set(trimmed)
	return value, nil
}

func normalizeCreateInput(input CreateInput) (CreateInput, error) {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Channel = strings.TrimSpace(input.Channel)
	if input.DisplayName == "" {
		return CreateInput{}, ValidationError{Message: "display_name 必填"}
	}
	if !validChannel(input.Channel) {
		return CreateInput{}, ValidationError{Message: "channel 非法"}
	}
	if input.Channel == ChannelReferral {
		input.ReferrerCustomerID = trimmedPtr(input.ReferrerCustomerID)
		if input.ReferrerCustomerID == nil {
			return CreateInput{}, ValidationError{Message: "介绍人必填"}
		}
	} else {
		input.ReferrerCustomerID = nil
	}
	if len(input.Identities) == 0 {
		return CreateInput{}, ValidationError{Message: "identities 至少需要一条"}
	}
	for i := range input.Identities {
		input.Identities[i].Platform = strings.TrimSpace(input.Identities[i].Platform)
		input.Identities[i].Handle = strings.TrimSpace(input.Identities[i].Handle)
		input.Identities[i].Remark = trimmedPtr(input.Identities[i].Remark)
		if !validPlatform(input.Identities[i].Platform) {
			return CreateInput{}, ValidationError{Message: "identity.platform 非法"}
		}
		if input.Identities[i].Handle == "" {
			return CreateInput{}, ValidationError{Message: "identity.handle 必填"}
		}
	}
	return input, nil
}

func normalizeListFilter(filter ListFilter) (ListFilter, error) {
	filter.Q = strings.TrimSpace(filter.Q)
	filter.Channel = strings.TrimSpace(filter.Channel)
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
	if filter.Channel != "" && !validChannel(filter.Channel) {
		return ListFilter{}, ValidationError{Message: "channel 非法"}
	}
	if !validStatus(filter.Status) {
		return ListFilter{}, ValidationError{Message: "status 非法"}
	}
	return filter, nil
}

func validChannel(channel string) bool {
	switch channel {
	case ChannelXiaohongshu, ChannelDouyin, ChannelWeibo, ChannelReferral, ChannelOther:
		return true
	default:
		return false
	}
}

func validPlatform(platform string) bool {
	switch platform {
	case PlatformWechat, PlatformQQ, PlatformTelegram, PlatformXiaohongshu, PlatformDouyin, PlatformWeibo, PlatformOther:
		return true
	default:
		return false
	}
}

func validStatus(status string) bool {
	switch status {
	case StatusActive, StatusMerged, StatusArchived, StatusAll:
		return true
	default:
		return false
	}
}

// validBirthday 校验生日特例格式（§4.1）："MM-DD"（年份缺省）或 "YYYY-MM-DD"；
// MM-DD 以闰年为基准解析，02-29 合法。
func validBirthday(value string) bool {
	if _, err := time.Parse("2006-01-02", value); err == nil {
		return true
	}
	if _, err := time.Parse("01-02", value); err == nil {
		return true
	}
	return false
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
