package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

type PlanningOrderLifecycleParticipant interface {
	LockPlanningReminderFenceInScope(context.Context, store.TxAccountScope) error
	BeforeOrderDeleteInScope(context.Context, store.TxAccountScope, string, string, string, time.Time) error
	OnOrderCancelledInScope(context.Context, store.TxAccountScope, string, string, string, time.Time) error
}

type PostgresRepository struct {
	lifecycle PlanningOrderLifecycleParticipant
}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (r PostgresRepository) WithLifecycleParticipant(participant PlanningOrderLifecycleParticipant) PostgresRepository {
	r.lifecycle = participant
	return r
}

func (r PostgresRepository) Create(ctx context.Context, scope store.AccountScope, prepared PreparedCreate) (Order, error) {
	var created Order
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		var err error
		created, err = r.CreatePreparedInScope(ctx, tx, prepared)
		return err
	})
	if err != nil {
		return Order{}, err
	}
	return created, nil
}

func (PostgresRepository) CreatePreparedInScope(
	ctx context.Context,
	scope store.TxAccountScope,
	prepared PreparedCreate,
) (Order, error) {
	input := prepared.Input
	customerChannel, err := requireUsableCustomer(ctx, scope, input.CustomerID, input.CreationMode)
	if err != nil {
		return Order{}, err
	}
	var packageShootType *string
	if input.PackageID != nil {
		shootType, err := requireUsablePackage(ctx, scope, *input.PackageID, input.CreationMode)
		if err != nil {
			return Order{}, err
		}
		packageShootType = &shootType
	}
	id := "ord_" + uuid.NewString()
	if _, err := scope.InsertReturningID(ctx, "orders",
		[]string{
			"id",
			"customer_id",
			"package_id",
			"title",
			"status",
			"price",
			"deposit_paid",
			"balance_paid",
			"shot_at",
			"delivered_at",
			"note",
			"delivery_due_at",
			"delivery_due_is_override",
			"amount_paid",
			"outstanding_amount",
			"paid_at",
			"channel_snapshot",
			"shoot_type_snapshot",
		},
		id,
		input.CustomerID,
		nullableStringArg(input.PackageID),
		nullableStringArg(input.Title),
		prepared.initial.Status,
		nullableIntArg(input.Price),
		prepared.initial.DepositPaid,
		prepared.initial.BalancePaid,
		nullableTimeArg(prepared.initial.ShotAt),
		nullableTimeArg(prepared.initial.DeliveredAt),
		nullableStringArg(input.Note),
		nullableDateArg(prepared.initial.DeliveryDueAt),
		prepared.initial.DeliveryDueIsOverride,
		prepared.initial.AmountPaid,
		nullableIntArg(prepared.initial.OutstandingAmount),
		nullableTimeArg(prepared.initial.PaidAt),
		customerChannel,
		nullableStringArg(packageShootType),
	); err != nil {
		return Order{}, err
	}
	return findOrder(ctx, scope, id)
}

func (PostgresRepository) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	cond, args := buildOrderFilter(filter)
	total, err := scope.Count(ctx, "orders", cond, args...)
	if err != nil {
		return ListResult{}, err
	}
	rows, err := scope.QueryPage(ctx, "orders", orderColumns, cond,
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}},
		filter.PageSize, (filter.Page-1)*filter.PageSize, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	orders := make([]Order, 0, filter.PageSize)
	customerIDs := make([]string, 0, filter.PageSize)
	packageIDs := make([]string, 0, filter.PageSize)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return ListResult{}, err
		}
		orders = append(orders, order)
		customerIDs = append(customerIDs, order.CustomerID)
		if order.PackageID != nil {
			packageIDs = append(packageIDs, *order.PackageID)
		}
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	customerNames, err := fetchCustomerNames(ctx, scope, uniqueStrings(customerIDs))
	if err != nil {
		return ListResult{}, err
	}
	packageNames, err := fetchPackageNames(ctx, scope, uniqueStrings(packageIDs))
	if err != nil {
		return ListResult{}, err
	}
	items := make([]ListItem, 0, len(orders))
	for _, order := range orders {
		item := ListItem{
			Order:               order,
			CustomerDisplayName: customerNames[order.CustomerID],
		}
		if order.PackageID != nil {
			if name, ok := packageNames[*order.PackageID]; ok {
				item.PackageName = &name
			}
		}
		items = append(items, item)
	}
	orderIDs := make([]string, 0, len(items))
	for _, item := range items {
		orderIDs = append(orderIDs, item.ID)
	}
	summaries, err := crm.LoadSummaries(ctx, scope, crm.SummaryViewByOrder, orderIDs)
	if err != nil {
		return ListResult{}, err
	}
	for i := range items {
		if summary, ok := summaries[items[i].ID]; ok {
			copied := summary
			items[i].PlanningSummary = &copied
		}
	}
	return ListResult{Items: items, Total: total}, nil
}

func (r PostgresRepository) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Order, error) {
	var updated Order
	now := time.Now().UTC()
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if r.lifecycle != nil && input.Status != nil && *input.Status == StatusCancelled {
			if err := r.lifecycle.LockPlanningReminderFenceInScope(ctx, tx); err != nil {
				return err
			}
		}
		current, err := findOrderForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		next, err := ApplyUpdateInput(current, input, now)
		if err != nil {
			return err
		}
		sets := make([]string, 0, 8)
		args := make([]any, 0, 8)
		set := func(column string, value any) {
			args = append(args, value)
			sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)+1))
		}
		set("status", next.Status)
		set("title", nullableStringArg(next.Title))
		set("price", nullableIntArg(next.Price))
		set("deposit_paid", next.DepositPaid)
		set("balance_paid", next.BalancePaid)
		set("shot_at", nullableTimeArg(next.ShotAt))
		set("delivered_at", nullableTimeArg(next.DeliveredAt))
		set("note", nullableStringArg(next.Note))
		set("delivery_due_at", nullableDateArg(next.DeliveryDueAt))
		set("delivery_due_is_override", next.DeliveryDueIsOverride)
		set("amount_paid", next.AmountPaid)
		set("outstanding_amount", nullableIntArg(next.OutstandingAmount))
		set("paid_at", nullableTimeArg(next.PaidAt))
		cond := fmt.Sprintf("id = $%d", len(args)+2)
		args = append(args, id)
		if _, err := tx.Update(ctx, "orders", strings.Join(sets, ", "), cond, args...); err != nil {
			return err
		}
		if r.lifecycle != nil && current.Status != StatusCancelled && next.Status == StatusCancelled {
			if err := r.lifecycle.OnOrderCancelledInScope(ctx, tx, next.ID, next.CustomerID, next.Status, now); err != nil {
				return err
			}
		}
		updated, err = findOrder(ctx, tx, id)
		return err
	})
	if err != nil {
		return Order{}, err
	}
	return updated, nil
}

func (r PostgresRepository) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	now := time.Now().UTC()
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if r.lifecycle != nil {
			if err := r.lifecycle.LockPlanningReminderFenceInScope(ctx, tx); err != nil {
				return err
			}
		}
		current, err := findOrderForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if !terminalStatus(current.Status) {
			return fmt.Errorf("%w: 非终态订单不可删除", ErrOrderNotTerminal)
		}
		var slotID string
		var slotStartAt time.Time
		err = tx.QueryRowForUpdate(
			ctx,
			"schedule_slots",
			"id, start_at",
			"type = 'shoot' AND order_id = $2",
			id,
		).Scan(&slotID, &slotStartAt)
		if err == nil {
			return OrderInUseError{SlotID: slotID, StartAt: slotStartAt}
		}
		if !errors.Is(err, store.ErrNoRows) {
			return err
		}
		if r.lifecycle != nil {
			if err := r.lifecycle.BeforeOrderDeleteInScope(ctx, tx, current.ID, current.CustomerID, current.Status, now); err != nil {
				return err
			}
		}
		rows, err := tx.Delete(ctx, "orders", "id = $2", id)
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: 订单不存在", ErrNotFound)
		}
		return nil
	})
}

// requireUsableCustomer 校验客户可引用并返回其当前渠道——归因快照的取值来源，
// 与订单插入同事务同行锁读取（dashboard-v2 ITEM-3）。
func requireUsableCustomer(ctx context.Context, scope rowScope, customerID, mode string) (string, error) {
	var status, channel string
	err := scope.QueryRowForUpdate(ctx, "customers", "status, channel", "id = $2", customerID).Scan(&status, &channel)
	if errors.Is(err, store.ErrNoRows) {
		return "", fmt.Errorf("%w: 客户不存在", ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if status == "merged" || (mode == CreationModeNew && status != "active") ||
		(mode == CreationModeBackfill && status != "active" && status != "archived") {
		return "", fmt.Errorf("%w: 客户已归档或合并", ErrCustomerArchived)
	}
	return channel, nil
}

// requireUsablePackage 校验套系可引用并返回其当前拍摄类型——归因快照的取值来源。
func requireUsablePackage(ctx context.Context, scope rowScope, packageID, mode string) (string, error) {
	var status, shootType string
	err := scope.QueryRowForUpdate(ctx, "packages", "status, shoot_type", "id = $2", packageID).Scan(&status, &shootType)
	if errors.Is(err, store.ErrNoRows) {
		return "", fmt.Errorf("%w: 套系不存在", ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	if mode == CreationModeNew && status != "active" {
		return "", ValidationError{Message: "下架套系不可用于新建订单"}
	}
	if mode == CreationModeBackfill && status != "active" && status != "archived" {
		return "", ValidationError{Message: "套系状态不可用于历史补录"}
	}
	return shootType, nil
}

const orderColumns = "id, account_id, created_at, customer_id, package_id, title, status, price, deposit_paid, balance_paid, shot_at, delivered_at, note, delivery_due_at, delivery_due_is_override, amount_paid, outstanding_amount, paid_at, channel_snapshot, shoot_type_snapshot"

type scanner interface {
	Scan(dest ...any) error
}

type rowScope interface {
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryRowForUpdate(context.Context, string, string, string, ...any) store.Row
}

func findOrder(ctx context.Context, scope rowScope, id string) (Order, error) {
	order, err := scanOrder(scope.QueryRow(ctx, "orders", orderColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: 订单不存在", ErrNotFound)
	}
	return order, err
}

func findOrderForUpdate(ctx context.Context, scope rowScope, id string) (Order, error) {
	order, err := scanOrder(scope.QueryRowForUpdate(ctx, "orders", orderColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Order{}, fmt.Errorf("%w: 订单不存在", ErrNotFound)
	}
	return order, err
}

func scanOrder(row scanner) (Order, error) {
	var order Order
	var packageID, title, note, shootTypeSnapshot sql.NullString
	var price, outstanding sql.NullInt64
	var shotAt, deliveredAt, deliveryDueAt, paidAt sql.NullTime
	if err := row.Scan(
		&order.ID,
		&order.AccountID,
		&order.CreatedAt,
		&order.CustomerID,
		&packageID,
		&title,
		&order.Status,
		&price,
		&order.DepositPaid,
		&order.BalancePaid,
		&shotAt,
		&deliveredAt,
		&note,
		&deliveryDueAt,
		&order.DeliveryDueIsOverride,
		&order.AmountPaid,
		&outstanding,
		&paidAt,
		&order.ChannelSnapshot,
		&shootTypeSnapshot,
	); err != nil {
		return Order{}, err
	}
	if deliveryDueAt.Valid {
		due := clock.DateOnly(deliveryDueAt.Time)
		order.DeliveryDueAt = &due
	}
	order.PackageID = stringPtr(packageID)
	order.Title = stringPtr(title)
	order.Price = intPtr(price)
	order.ShotAt = timePtr(shotAt)
	order.DeliveredAt = timePtr(deliveredAt)
	order.Note = stringPtr(note)
	order.OutstandingAmount = intPtr(outstanding)
	order.PaidAt = timePtr(paidAt)
	order.ShootTypeSnapshot = stringPtr(shootTypeSnapshot)
	return order, nil
}

func buildOrderFilter(filter ListFilter) (string, []any) {
	conds := make([]string, 0, 5)
	args := make([]any, 0, 6)
	if filter.ID != "" {
		args = append(args, filter.ID)
		conds = append(conds, fmt.Sprintf("id = $%d", len(args)+1))
	}
	if filter.CustomerID != "" {
		args = append(args, filter.CustomerID)
		conds = append(conds, fmt.Sprintf("customer_id = $%d", len(args)+1))
	}
	if filter.Status != "" {
		args = append(args, filter.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)+1))
	}
	if filter.UnpaidBalance {
		conds = append(conds, "balance_paid = false AND status IN ('shot', 'selected', 'retouching', 'delivered')")
	}
	if filter.SchedulableAt != nil {
		orderStatuses, customerStatuses := schedulableStatuses(*filter.SchedulableAt, filter.schedulableNow)
		args = append(args, orderStatuses)
		orderStatusesPlaceholder := len(args) + 1
		args = append(args, customerStatuses)
		customerStatusesPlaceholder := len(args) + 1
		conds = append(conds, fmt.Sprintf(`
status = ANY($%d::text[])
AND EXISTS (
	SELECT 1 FROM customers AS schedule_customer
	WHERE schedule_customer.account_id = orders.account_id
	  AND schedule_customer.id = orders.customer_id
	  AND schedule_customer.status = ANY($%d::text[])
)
AND NOT EXISTS (
	SELECT 1 FROM schedule_slots AS schedule_slot
	WHERE schedule_slot.account_id = orders.account_id
	  AND schedule_slot.order_id = orders.id
	  AND schedule_slot.type = 'shoot'
)`, orderStatusesPlaceholder, customerStatusesPlaceholder))
	}
	return strings.Join(conds, " AND "), args
}

func fetchCustomerNames(ctx context.Context, scope store.AccountScope, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := scope.Query(ctx, "customers", "id, display_name", "id = ANY($2::text[])", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func fetchPackageNames(ctx context.Context, scope store.AccountScope, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := scope.Query(ctx, "packages", "id, name", "id = ANY($2::text[])", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func intPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	v := int(value.Int64)
	return &v
}

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

func nullableStringArg(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// nullableDateArg 把 date-only 值写入 DATE 列；nil 为 SQL NULL。
func nullableDateArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return clock.DateOnly(*value)
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
