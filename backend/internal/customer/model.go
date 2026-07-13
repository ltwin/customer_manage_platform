package customer

import (
	"errors"
	"time"

	"github.com/oapi-codegen/nullable"
)

const (
	ChannelXiaohongshu = "xiaohongshu"
	ChannelDouyin      = "douyin"
	ChannelWeibo       = "weibo"
	ChannelReferral    = "referral"
	ChannelOther       = "other"

	StatusActive   = "active"
	StatusMerged   = "merged"
	StatusArchived = "archived"
	StatusAll      = "all"

	PlatformWechat      = "wechat"
	PlatformQQ          = "qq"
	PlatformTelegram    = "telegram"
	PlatformXiaohongshu = "xiaohongshu"
	PlatformDouyin      = "douyin"
	PlatformWeibo       = "weibo"
	PlatformOther       = "other"
)

var (
	ErrValidation = errors.New("validation_failed")
	ErrNotFound   = errors.New("not_found")
	// ErrCustomerMerged：merged 客户档案只读，一切写操作拒绝（design D4，HTTP 409 customer_merged）。
	ErrCustomerMerged = errors.New("customer_merged")
	// ErrLastIdentity：非 merged 客户任何时刻至少保留 1 个社交身份（design D3，HTTP 409 last_identity）。
	ErrLastIdentity = errors.New("last_identity")
	// ErrMergeConflict：merge 双方必须都 active（design D5，HTTP 409 merge_conflict）。
	ErrMergeConflict = errors.New("merge_conflict")
)

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	return e.Message
}

func (e ValidationError) Unwrap() error {
	return ErrValidation
}

type Customer struct {
	ID                   string
	AccountID            string
	CreatedAt            time.Time
	DisplayName          string
	RealName             *string
	Phone                *string
	Birthday             *string
	Channel              string
	ReferrerCustomerID   *string
	Status               string
	MergedIntoCustomerID *string
	AvatarMetadata
}

// AvatarMetadata 是 PostgreSQL 上的当前头像 pointer 与独立写 revision。
// 五个 pointer 字段必须全空或全非空；对象 key 由账号、客户与 ObjectRef 派生而不落库。
type AvatarMetadata struct {
	AvatarRevision  int64
	AvatarVersion   *string
	AvatarObjectID  *string
	AvatarMediaType *string
	AvatarSize      *int64
	AvatarUpdatedAt *time.Time
}

type IdentityInput struct {
	Platform string
	Handle   string
	Remark   *string
}

type SocialIdentity struct {
	ID         string
	AccountID  string
	CreatedAt  time.Time
	CustomerID string
	Platform   string
	Handle     string
	Remark     *string
}

// CustomerNote 是随手备注（≤500 字），详情按创建倒序返回。
type CustomerNote struct {
	ID         string
	AccountID  string
	CreatedAt  time.Time
	CustomerID string
	Content    string
}

type CreateInput struct {
	DisplayName        string
	Channel            string
	ReferrerCustomerID *string
	Identities         []IdentityInput
}

// UpdateInput 是 PATCH /customers/{id} 的领域输入。可清空字段（real_name / phone /
// birthday）用 nullable.Nullable 表达「未传 / 传值 / 传 null」三态（design D9，
// 与 codegen 生成的 PATCH body 类型同源）；其余可选字段用指针表达「未传 / 传值」。
type UpdateInput struct {
	DisplayName        *string
	RealName           nullable.Nullable[string]
	Phone              nullable.Nullable[string]
	Birthday           nullable.Nullable[string]
	Channel            *string
	ReferrerCustomerID *string
	Status             *string
}

type ListFilter struct {
	Q        string
	Channel  string
	Status   string
	Page     int
	PageSize int
}

type ListItem struct {
	Customer
	OrdersCount int
	LastShotAt  *string
}

type ListResult struct {
	Items []ListItem
	Total int64
}

type CustomerSummary struct {
	ID             string
	DisplayName    string
	Channel        string
	Status         string
	AvatarRevision int64
	AvatarVersion  *string
}

type CustomerStats struct {
	OrdersCount      int
	TotalOrderAmount int
	LastShotAt       *string
}

type Detail struct {
	Customer
	Identities []SocialIdentity
	Notes      []CustomerNote
	Referrer   *CustomerSummary
	Stats      CustomerStats
}
