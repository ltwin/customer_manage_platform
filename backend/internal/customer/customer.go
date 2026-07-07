package customer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
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

type CreateInput struct {
	DisplayName        string
	Channel            string
	ReferrerCustomerID *string
	Identities         []IdentityInput
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
	ID          string
	DisplayName string
	Channel     string
	Status      string
}

type CustomerStats struct {
	OrdersCount      int
	TotalOrderAmount int
	LastShotAt       *string
}

type Detail struct {
	Customer
	Identities []SocialIdentity
	Notes      []string
	Referrer   *CustomerSummary
	Stats      CustomerStats
}

type Repository interface {
	Create(context.Context, store.AccountScope, CreateInput) (Customer, error)
	List(context.Context, store.AccountScope, ListFilter) (ListResult, error)
	Detail(context.Context, store.AccountScope, string) (Detail, error)
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

type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (PostgresRepository) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (Customer, error) {
	id := "cus_" + uuid.NewString()
	var created Customer
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		if input.Channel == ChannelReferral {
			exists, err := tx.Exists(ctx, "customers", "id = $2", deref(input.ReferrerCustomerID))
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("%w: 介绍人不存在", ErrNotFound)
			}
		}

		if _, err := tx.InsertReturningID(ctx, "customers",
			[]string{"id", "display_name", "channel", "referrer_customer_id", "status"},
			id, input.DisplayName, input.Channel, nullable(input.ReferrerCustomerID), StatusActive,
		); err != nil {
			return err
		}
		for _, identity := range input.Identities {
			if _, err := tx.InsertReturningID(ctx, "social_identities",
				[]string{"id", "customer_id", "platform", "handle", "remark"},
				"sid_"+uuid.NewString(), id, identity.Platform, identity.Handle, nullable(identity.Remark),
			); err != nil {
				return err
			}
		}
		customer, err := findCustomer(ctx, tx, id)
		if err != nil {
			return err
		}
		created = customer
		return nil
	})
	if err != nil {
		return Customer{}, err
	}
	return created, nil
}

func (PostgresRepository) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	cond, args := buildCustomerFilter(filter)
	total, err := scope.Count(ctx, "customers", cond, args...)
	if err != nil {
		return ListResult{}, err
	}
	// 排序分页在 DB 完成：created_at DESC 为主序，id DESC 消除并列歧义（REV-001）。
	rows, err := scope.QueryPage(ctx, "customers", customerColumns, cond,
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}},
		filter.PageSize, (filter.Page-1)*filter.PageSize, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	items := make([]ListItem, 0, filter.PageSize)
	for rows.Next() {
		customer, err := scanCustomer(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, ListItem{Customer: customer})
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total}, nil
}

func (PostgresRepository) Detail(ctx context.Context, scope store.AccountScope, id string) (Detail, error) {
	customer, err := findCustomer(ctx, scope, id)
	if err != nil {
		return Detail{}, err
	}
	identities, err := listIdentities(ctx, scope, id)
	if err != nil {
		return Detail{}, err
	}
	var referrer *CustomerSummary
	if customer.ReferrerCustomerID != nil {
		ref, err := findCustomer(ctx, scope, *customer.ReferrerCustomerID)
		if err != nil {
			return Detail{}, err
		}
		referrer = &CustomerSummary{
			ID:          ref.ID,
			DisplayName: ref.DisplayName,
			Channel:     ref.Channel,
			Status:      ref.Status,
		}
	}
	return Detail{
		Customer:   customer,
		Identities: identities,
		Notes:      []string{},
		Referrer:   referrer,
		Stats:      CustomerStats{},
	}, nil
}

const customerColumns = "id, account_id, created_at, display_name, real_name, phone, birthday, channel, referrer_customer_id, status, merged_into_customer_id"
const identityColumns = "id, account_id, created_at, customer_id, platform, handle, remark"

type scanner interface {
	Scan(dest ...any) error
}

func findCustomer(ctx context.Context, scope store.AccountScope, id string) (Customer, error) {
	customer, err := scanCustomer(scope.QueryRow(ctx, "customers", customerColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Customer{}, fmt.Errorf("%w: 客户不存在", ErrNotFound)
	}
	return customer, err
}

func scanCustomer(row scanner) (Customer, error) {
	var customer Customer
	var realName, phone, birthday, referrer, merged sql.NullString
	if err := row.Scan(
		&customer.ID,
		&customer.AccountID,
		&customer.CreatedAt,
		&customer.DisplayName,
		&realName,
		&phone,
		&birthday,
		&customer.Channel,
		&referrer,
		&customer.Status,
		&merged,
	); err != nil {
		return Customer{}, err
	}
	customer.RealName = stringPtr(realName)
	customer.Phone = stringPtr(phone)
	customer.Birthday = stringPtr(birthday)
	customer.ReferrerCustomerID = stringPtr(referrer)
	customer.MergedIntoCustomerID = stringPtr(merged)
	return customer, nil
}

func listIdentities(ctx context.Context, scope store.AccountScope, customerID string) ([]SocialIdentity, error) {
	rows, err := scope.Query(ctx, "social_identities", identityColumns, "customer_id = $2", customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	identities := make([]SocialIdentity, 0)
	for rows.Next() {
		var identity SocialIdentity
		var remark sql.NullString
		if err := rows.Scan(
			&identity.ID,
			&identity.AccountID,
			&identity.CreatedAt,
			&identity.CustomerID,
			&identity.Platform,
			&identity.Handle,
			&remark,
		); err != nil {
			return nil, err
		}
		identity.Remark = stringPtr(remark)
		identities = append(identities, identity)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(identities, func(i, j int) bool {
		return identities[i].CreatedAt.Before(identities[j].CreatedAt)
	})
	return identities, nil
}

func buildCustomerFilter(filter ListFilter) (string, []any) {
	conds := make([]string, 0, 3)
	args := make([]any, 0, 3)
	if filter.Status != StatusAll {
		args = append(args, filter.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
	}
	if filter.Channel != "" {
		args = append(args, filter.Channel)
		conds = append(conds, fmt.Sprintf("channel = $%d", len(args)+1))
	}
	if filter.Q != "" {
		args = append(args, "%"+filter.Q+"%")
		placeholder := fmt.Sprintf("$%d", len(args)+1)
		conds = append(conds, fmt.Sprintf(`(
			display_name ILIKE %[1]s
			OR real_name ILIKE %[1]s
			OR phone ILIKE %[1]s
			OR EXISTS (
				SELECT 1 FROM social_identities
				WHERE social_identities.account_id = customers.account_id
				  AND social_identities.customer_id = customers.id
				  AND social_identities.handle ILIKE %[1]s
			)
		)`, placeholder))
	}
	return strings.Join(conds, " AND "), args
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

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
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

func nullable(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
