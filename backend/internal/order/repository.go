package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
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
	if err := requireUsableCustomer(ctx, scope, input.CustomerID, input.CreationMode); err != nil {
		return Order{}, err
	}
	if input.PackageID != nil {
		if err := requireUsablePackage(ctx, scope, *input.PackageID, input.CreationMode); err != nil {
			return Order{}, err
		}
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
	return ListResult{Items: items, Total: total}, nil
}

func (PostgresRepository) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Order, error) {
	var updated Order
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		current, err := findOrderForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		next, err := ApplyUpdateInput(current, input, time.Now().UTC())
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
		cond := fmt.Sprintf("id = $%d", len(args)+2)
		args = append(args, id)
		if _, err := tx.Update(ctx, "orders", strings.Join(sets, ", "), cond, args...); err != nil {
			return err
		}
		updated, err = findOrder(ctx, tx, id)
		return err
	})
	if err != nil {
		return Order{}, err
	}
	return updated, nil
}

func (PostgresRepository) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	return scope.WithinTx(ctx, func(tx store.AccountScope) error {
		current, err := findOrderForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if !terminalStatus(current.Status) {
			return fmt.Errorf("%w: 非终态订单不可删除", ErrOrderNotTerminal)
		}
		var slotID string
		var slotStartAt time.Time
		err = tx.QueryRow(
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

func requireUsableCustomer(ctx context.Context, scope rowScope, customerID, mode string) error {
	var status string
	err := scope.QueryRowForUpdate(ctx, "customers", "status", "id = $2", customerID).Scan(&status)
	if errors.Is(err, store.ErrNoRows) {
		return fmt.Errorf("%w: 客户不存在", ErrNotFound)
	}
	if err != nil {
		return err
	}
	if status == "merged" || (mode == CreationModeNew && status != "active") ||
		(mode == CreationModeBackfill && status != "active" && status != "archived") {
		return fmt.Errorf("%w: 客户已归档或合并", ErrCustomerArchived)
	}
	return nil
}

func requireUsablePackage(ctx context.Context, scope rowScope, packageID, mode string) error {
	var status string
	err := scope.QueryRowForUpdate(ctx, "packages", "status", "id = $2", packageID).Scan(&status)
	if errors.Is(err, store.ErrNoRows) {
		return fmt.Errorf("%w: 套系不存在", ErrNotFound)
	}
	if err != nil {
		return err
	}
	if mode == CreationModeNew && status != "active" {
		return ValidationError{Message: "下架套系不可用于新建订单"}
	}
	if mode == CreationModeBackfill && status != "active" && status != "archived" {
		return ValidationError{Message: "套系状态不可用于历史补录"}
	}
	return nil
}

const orderColumns = "id, account_id, created_at, customer_id, package_id, title, status, price, deposit_paid, balance_paid, shot_at, delivered_at, note"

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
	var packageID, title, note sql.NullString
	var price sql.NullInt64
	var shotAt, deliveredAt sql.NullTime
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
	); err != nil {
		return Order{}, err
	}
	order.PackageID = stringPtr(packageID)
	order.Title = stringPtr(title)
	order.Price = intPtr(price)
	order.ShotAt = timePtr(shotAt)
	order.DeliveredAt = timePtr(deliveredAt)
	order.Note = stringPtr(note)
	return order, nil
}

func buildOrderFilter(filter ListFilter) (string, []any) {
	conds := make([]string, 0, 4)
	args := make([]any, 0, 5)
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

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}
