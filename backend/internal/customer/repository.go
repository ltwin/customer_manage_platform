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
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (PostgresRepository) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (Customer, error) {
	id := "cus_" + uuid.NewString()
	var created Customer
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		if input.Channel == ChannelReferral {
			// 介绍人须存在且 active（design D8，与 PATCH 面同口径）。
			if err := requireActiveReferrer(ctx, tx, deref(input.ReferrerCustomerID)); err != nil {
				return err
			}
		}

		if _, err := tx.InsertReturningID(ctx, "customers",
			[]string{"id", "display_name", "channel", "referrer_customer_id", "status"},
			id, input.DisplayName, input.Channel, nullableArg(input.ReferrerCustomerID), StatusActive,
		); err != nil {
			return err
		}
		for _, identity := range input.Identities {
			if _, err := tx.InsertReturningID(ctx, "social_identities",
				[]string{"id", "customer_id", "platform", "handle", "remark"},
				"sid_"+uuid.NewString(), id, identity.Platform, identity.Handle, nullableArg(identity.Remark),
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
		stats, err := orderStatsForCustomer(ctx, scope, customer.ID)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, ListItem{
			Customer:    customer,
			OrdersCount: stats.OrdersCount,
			LastShotAt:  stats.LastShotAt,
		})
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
	notes, err := listNotes(ctx, scope, id)
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
	stats, err := orderStatsForCustomer(ctx, scope, id)
	if err != nil {
		return Detail{}, err
	}
	return Detail{
		Customer:   customer,
		Identities: identities,
		Notes:      notes,
		Referrer:   referrer,
		Stats:      stats,
	}, nil
}

// Update 在同一事务内完成 PATCH 的状态矩阵校验、channel↔referrer 联动与字段更新
// （design D2/D4）；merged 客户一切写操作拒绝。状态读取带 FOR UPDATE 行锁，
// 与并发 Merge 串行化，防 TOCTOU 绕过 merged 只读守护（review REV-001）。
func (PostgresRepository) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Customer, error) {
	var updated Customer
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		current, err := findCustomerForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if current.Status == StatusMerged {
			return fmt.Errorf("%w: 客户已合并，档案只读", ErrCustomerMerged)
		}

		// 联动规则的状态相关半边：生效 channel 非 referral 时单独传 referrer 拒绝；
		// 静态半边（缺介绍人 / 自指 / 显式非 referral 带 referrer）已在 service 校验。
		effectiveChannel := current.Channel
		if input.Channel != nil {
			effectiveChannel = *input.Channel
		}
		if input.ReferrerCustomerID != nil && effectiveChannel != ChannelReferral {
			return ValidationError{Message: "channel 非 referral 不接受 referrer_customer_id"}
		}
		if input.ReferrerCustomerID != nil {
			if err := requireActiveReferrer(ctx, tx, *input.ReferrerCustomerID); err != nil {
				return err
			}
		}

		sets := make([]string, 0, 8)
		args := make([]any, 0, 8)
		set := func(column string, value any) {
			args = append(args, value)
			sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)+1))
		}
		if input.DisplayName != nil {
			set("display_name", *input.DisplayName)
		}
		applyClearable(set, "real_name", input.RealName)
		applyClearable(set, "phone", input.Phone)
		applyClearable(set, "birthday", input.Birthday)
		if input.Channel != nil {
			set("channel", *input.Channel)
			if *input.Channel != ChannelReferral {
				// 从 referral 改走：服务端自动清空介绍人（design D2）。
				set("referrer_customer_id", nil)
			}
		}
		if input.ReferrerCustomerID != nil {
			set("referrer_customer_id", *input.ReferrerCustomerID)
		}
		if input.Status != nil {
			set("status", *input.Status)
		}
		if len(sets) > 0 {
			cond := fmt.Sprintf("id = $%d", len(args)+2)
			args = append(args, id)
			if _, err := tx.Update(ctx, "customers", strings.Join(sets, ", "), cond, args...); err != nil {
				return err
			}
		}
		updated, err = findCustomer(ctx, tx, id)
		return err
	})
	if err != nil {
		return Customer{}, err
	}
	return updated, nil
}

// applyClearable 把三态字段落进 SET 列表：未传跳过、传值更新、传 null 清空。
func applyClearable(set func(string, any), column string, value nullable.Nullable[string]) {
	if !value.IsSpecified() {
		return
	}
	if value.IsNull() {
		set(column, nil)
		return
	}
	set(column, value.MustGet())
}

// requireActiveReferrer 校验介绍人存在、本账号可见且 active（design D8）；
// 不满足统一按 404 not_found 处理，不泄露跨账号存在性。
func requireActiveReferrer(ctx context.Context, scope store.AccountScope, referrerID string) error {
	exists, err := scope.Exists(ctx, "customers", "id = $2 AND status = $3", referrerID, StatusActive)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: 介绍人不存在或不可用", ErrNotFound)
	}
	return nil
}

// AddIdentity 为非 merged 客户追加社交身份（design D4：merged 只读）。
func (PostgresRepository) AddIdentity(ctx context.Context, scope store.AccountScope, customerID string, input IdentityInput) (SocialIdentity, error) {
	var created SocialIdentity
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		if err := requireWritableCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		id := "sid_" + uuid.NewString()
		if _, err := tx.InsertReturningID(ctx, "social_identities",
			[]string{"id", "customer_id", "platform", "handle", "remark"},
			id, customerID, input.Platform, input.Handle, nullableArg(input.Remark),
		); err != nil {
			return err
		}
		identity, err := findIdentity(ctx, tx, customerID, id)
		if err != nil {
			return err
		}
		created = identity
		return nil
	})
	if err != nil {
		return SocialIdentity{}, err
	}
	return created, nil
}

// DeleteIdentity 删除社交身份；末位身份守护（design D3）：先对客户行加锁，
// 查数与删除同事务完成，防并发删穿。
func (PostgresRepository) DeleteIdentity(ctx context.Context, scope store.AccountScope, customerID, identityID string) error {
	return scope.WithinTx(ctx, func(tx store.AccountScope) error {
		current, err := findCustomerForUpdate(ctx, tx, customerID)
		if err != nil {
			return err
		}
		if current.Status == StatusMerged {
			return fmt.Errorf("%w: 客户已合并，档案只读", ErrCustomerMerged)
		}
		exists, err := tx.Exists(ctx, "social_identities", "id = $2 AND customer_id = $3", identityID, customerID)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("%w: 身份不存在", ErrNotFound)
		}
		count, err := tx.Count(ctx, "social_identities", "customer_id = $2", customerID)
		if err != nil {
			return err
		}
		if count <= 1 {
			return fmt.Errorf("%w: 至少保留 1 个社交身份", ErrLastIdentity)
		}
		if _, err := tx.Delete(ctx, "social_identities", "id = $2 AND customer_id = $3", identityID, customerID); err != nil {
			return err
		}
		return nil
	})
}

// AddNote 为非 merged 客户追加备注（design D4：merged 只读）。
func (PostgresRepository) AddNote(ctx context.Context, scope store.AccountScope, customerID, content string) (CustomerNote, error) {
	var created CustomerNote
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		if err := requireWritableCustomer(ctx, tx, customerID); err != nil {
			return err
		}
		id := "note_" + uuid.NewString()
		if _, err := tx.InsertReturningID(ctx, "customer_notes",
			[]string{"id", "customer_id", "content"},
			id, customerID, content,
		); err != nil {
			return err
		}
		var note CustomerNote
		if err := tx.QueryRow(ctx, "customer_notes", noteColumns, "id = $2", id).Scan(
			&note.ID, &note.AccountID, &note.CreatedAt, &note.CustomerID, &note.Content,
		); err != nil {
			return err
		}
		created = note
		return nil
	})
	if err != nil {
		return CustomerNote{}, err
	}
	return created, nil
}

// Merge 把 source 客户合并进 target（design D1/D5，roadmap §4.2）：同一事务内
// 迁移身份 / 备注、把其他客户指向 source 的转介绍指针重定向到 target（target
// 自指则清空，channel 保持不变——「referral+空 referrer」是合法历史态）、
// source 置 merged + 指针；任一步失败整体回滚。两行都加 FOR UPDATE 锁，
// 防与其他写操作并发交错。
func (PostgresRepository) Merge(ctx context.Context, scope store.AccountScope, targetID, sourceID string) (Customer, error) {
	var merged Customer
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		// 固定锁序（按 id 升序）避免对向 merge 死锁。
		firstID, secondID := targetID, sourceID
		if firstID > secondID {
			firstID, secondID = secondID, firstID
		}
		for _, id := range []string{firstID, secondID} {
			locked, err := findCustomerForUpdate(ctx, tx, id)
			if err != nil {
				return err
			}
			if locked.Status != StatusActive {
				return fmt.Errorf("%w: 双方客户必须都是 active", ErrMergeConflict)
			}
		}

		// 迁移面（roadmap §4.2 随域生长；当前 = 身份 + 备注）。
		if _, err := tx.Update(ctx, "social_identities", "customer_id = $2", "customer_id = $3", targetID, sourceID); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "customer_notes", "customer_id = $2", "customer_id = $3", targetID, sourceID); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "orders", "customer_id = $2", "customer_id = $3", targetID, sourceID); err != nil {
			return err
		}
		// 转介绍指针批量重定向（D1）：指向 source 的改指 target；target 自指清空。
		if _, err := tx.Update(ctx, "customers", "referrer_customer_id = $2", "referrer_customer_id = $3", targetID, sourceID); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "customers", "referrer_customer_id = NULL", "id = $2 AND referrer_customer_id = $3", targetID, targetID); err != nil {
			return err
		}
		if _, err := tx.Update(ctx, "customers", "status = $2, merged_into_customer_id = $3", "id = $4", StatusMerged, targetID, sourceID); err != nil {
			return err
		}

		target, err := findCustomer(ctx, tx, targetID)
		if err != nil {
			return err
		}
		merged = target
		return nil
	})
	if err != nil {
		return Customer{}, err
	}
	return merged, nil
}

// requireWritableCustomer 校验客户存在（404）且非 merged（409 customer_merged）。
// 带 FOR UPDATE 行锁与并发 Merge 串行化（review REV-001），只应在 WithinTx 内调用。
func requireWritableCustomer(ctx context.Context, scope store.AccountScope, customerID string) error {
	current, err := findCustomerForUpdate(ctx, scope, customerID)
	if err != nil {
		return err
	}
	if current.Status == StatusMerged {
		return fmt.Errorf("%w: 客户已合并，档案只读", ErrCustomerMerged)
	}
	return nil
}

// findCustomerForUpdate 同 findCustomer，但对命中行加 FOR UPDATE 行锁；
// 供「先锁客户行、再校验状态或不变量」的写路径使用，只应在 WithinTx 内调用。
func findCustomerForUpdate(ctx context.Context, scope store.AccountScope, id string) (Customer, error) {
	customer, err := scanCustomer(scope.QueryRowForUpdate(ctx, "customers", customerColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Customer{}, fmt.Errorf("%w: 客户不存在", ErrNotFound)
	}
	return customer, err
}

func findIdentity(ctx context.Context, scope store.AccountScope, customerID, identityID string) (SocialIdentity, error) {
	var identity SocialIdentity
	var remark sql.NullString
	err := scope.QueryRow(ctx, "social_identities", identityColumns, "id = $2 AND customer_id = $3", identityID, customerID).Scan(
		&identity.ID,
		&identity.AccountID,
		&identity.CreatedAt,
		&identity.CustomerID,
		&identity.Platform,
		&identity.Handle,
		&remark,
	)
	if errors.Is(err, store.ErrNoRows) {
		return SocialIdentity{}, fmt.Errorf("%w: 身份不存在", ErrNotFound)
	}
	if err != nil {
		return SocialIdentity{}, err
	}
	identity.Remark = stringPtr(remark)
	return identity, nil
}

const customerColumns = "id, account_id, created_at, display_name, real_name, phone, birthday, channel, referrer_customer_id, status, merged_into_customer_id"
const identityColumns = "id, account_id, created_at, customer_id, platform, handle, remark"
const noteColumns = "id, account_id, created_at, customer_id, content"

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

// listNotes 返回客户全部备注，按创建倒序（created_at DESC, id DESC 消并列歧义）。
func listNotes(ctx context.Context, scope store.AccountScope, customerID string) ([]CustomerNote, error) {
	rows, err := scope.Query(ctx, "customer_notes", noteColumns, "customer_id = $2", customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notes := make([]CustomerNote, 0)
	for rows.Next() {
		var note CustomerNote
		if err := rows.Scan(&note.ID, &note.AccountID, &note.CreatedAt, &note.CustomerID, &note.Content); err != nil {
			return nil, err
		}
		notes = append(notes, note)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(notes, func(i, j int) bool {
		if notes[i].CreatedAt.Equal(notes[j].CreatedAt) {
			return notes[i].ID > notes[j].ID
		}
		return notes[i].CreatedAt.After(notes[j].CreatedAt)
	})
	return notes, nil
}

func orderStatsForCustomer(ctx context.Context, scope store.AccountScope, customerID string) (CustomerStats, error) {
	const nonCancelledOrders = "customer_id = $2 AND status <> $3"
	var count int64
	if err := scope.ScalarAggregate(ctx, "orders", store.AggregateCount, "id", nonCancelledOrders, customerID, "cancelled").Scan(&count); err != nil {
		return CustomerStats{}, err
	}
	var total int64
	if err := scope.ScalarAggregate(ctx, "orders", store.AggregateSum, "price", nonCancelledOrders, customerID, "cancelled").Scan(&total); err != nil {
		return CustomerStats{}, err
	}
	var lastShot sql.NullTime
	if err := scope.ScalarAggregate(ctx, "orders", store.AggregateMax, "shot_at", nonCancelledOrders, customerID, "cancelled").Scan(&lastShot); err != nil {
		return CustomerStats{}, err
	}
	return CustomerStats{
		OrdersCount:      int(count),
		TotalOrderAmount: int(total),
		LastShotAt:       shotDate(lastShot),
	}, nil
}

func shotDate(value sql.NullTime) *string {
	if !value.Valid {
		return nil
	}
	const shanghaiOffset = 8 * 60 * 60
	date := value.Time.In(time.FixedZone("Asia/Shanghai", shanghaiOffset)).Format("2006-01-02")
	return &date
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

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableArg(value *string) any {
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
