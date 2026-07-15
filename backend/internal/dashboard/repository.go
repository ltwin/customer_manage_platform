package dashboard

import (
	"context"
	"database/sql"
	"sort"
	"time"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	reminderdomain "github.com/samson/customer-manage-platform/backend/internal/reminder"
	scheduledomain "github.com/samson/customer-manage-platform/backend/internal/schedule"
)

const (
	reminderColumns = "id, account_id, created_at, type, customer_id, order_id, due_date, content, status, dedup_key"
	orderColumns    = "id, account_id, created_at, customer_id, package_id, title, status, price, deposit_paid, balance_paid, shot_at, delivered_at, note"
)

// PostgresRepository 经 AccountScope 直查 reminders / schedule_slots / orders 装配五块（D1）。
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (PostgresRepository) LoadDashboard(
	ctx context.Context,
	scope store.AccountScope,
	window Window,
) (Dashboard, error) {
	due, err := loadDueReminders(ctx, scope, window.DueBefore)
	if err != nil {
		return Dashboard{}, err
	}
	churn, err := loadChurnAlerts(ctx, scope)
	if err != nil {
		return Dashboard{}, err
	}
	// 今日档期复用 schedule 导出的装配函数，保证摘要字段与排序与 GET /schedule/slots 单一来源（D8）。
	slots, err := scheduledomain.AssembleListItems(ctx, scope, scheduledomain.ListFilter{
		From: window.DayStart,
		To:   window.DayEnd,
	})
	if err != nil {
		return Dashboard{}, err
	}
	unpaid, err := loadUnpaidOrders(ctx, scope)
	if err != nil {
		return Dashboard{}, err
	}
	stats, err := loadRecentStats(ctx, scope, window.RecentStart, window.RecentEnd)
	if err != nil {
		return Dashboard{}, err
	}
	return Dashboard{
		DueReminders: due,
		TodaySlots:   slots,
		UnpaidOrders: unpaid,
		ChurnAlerts:  churn,
		RecentStats:  stats,
	}, nil
}

// loadDueReminders：pending 且 due_date ≤ 今日+2（date-only 字符串比较）；排序 due_date ASC, id ASC。
func loadDueReminders(ctx context.Context, scope store.AccountScope, dueBefore time.Time) ([]reminderdomain.Reminder, error) {
	rows, err := scope.Query(ctx, "reminders", reminderColumns,
		"status = $2 AND due_date <= $3",
		reminderdomain.StatusPending, clock.FormatDate(dueBefore))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanReminders(rows)
	if err != nil {
		return nil, err
	}
	sortReminders(items)
	return items, nil
}

// loadChurnAlerts：type=churn 且 pending（无日期窗）；排序 due_date ASC, id ASC。
func loadChurnAlerts(ctx context.Context, scope store.AccountScope) ([]reminderdomain.Reminder, error) {
	rows, err := scope.Query(ctx, "reminders", reminderColumns,
		"status = $2 AND type = $3",
		reminderdomain.StatusPending, reminderdomain.TypeChurn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items, err := scanReminders(rows)
	if err != nil {
		return nil, err
	}
	sortReminders(items)
	return items, nil
}

// loadUnpaidOrders：status=delivered 且 balance_paid=false；排序 delivered_at ASC NULLS LAST, id ASC。
func loadUnpaidOrders(ctx context.Context, scope store.AccountScope) (UnpaidOrders, error) {
	rows, err := scope.Query(ctx, "orders", orderColumns,
		"status = $2 AND balance_paid = false", orderdomain.StatusDelivered)
	if err != nil {
		return UnpaidOrders{}, err
	}
	defer rows.Close()
	orders := make([]orderdomain.Order, 0)
	customerIDs := make([]string, 0)
	packageIDs := make([]string, 0)
	for rows.Next() {
		order, err := scanOrder(rows)
		if err != nil {
			return UnpaidOrders{}, err
		}
		orders = append(orders, order)
		customerIDs = append(customerIDs, order.CustomerID)
		if order.PackageID != nil {
			packageIDs = append(packageIDs, *order.PackageID)
		}
	}
	if err := rows.Err(); err != nil {
		return UnpaidOrders{}, err
	}
	sortUnpaidOrders(orders)
	customerNames, err := fetchNameMap(ctx, scope, "customers", "display_name", uniqueStrings(customerIDs))
	if err != nil {
		return UnpaidOrders{}, err
	}
	packageNames, err := fetchNameMap(ctx, scope, "packages", "name", uniqueStrings(packageIDs))
	if err != nil {
		return UnpaidOrders{}, err
	}
	items := make([]orderdomain.ListItem, 0, len(orders))
	for _, order := range orders {
		item := orderdomain.ListItem{
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
	return UnpaidOrders{Count: len(items), Items: items}, nil
}

// loadRecentStats：近 30 天滚动窗 [start, end) 三统计（design §recent_stats / A3）。
func loadRecentStats(ctx context.Context, scope store.AccountScope, start, end time.Time) (RecentStats, error) {
	created, err := scope.Count(ctx, "orders",
		"created_at >= $2 AND created_at < $3", start, end)
	if err != nil {
		return RecentStats{}, err
	}
	delivered, err := scope.Count(ctx, "orders",
		"delivered_at >= $2 AND delivered_at < $3 AND status <> $4",
		start, end, orderdomain.StatusCancelled)
	if err != nil {
		return RecentStats{}, err
	}
	var revenue int64
	err = scope.ScalarAggregate(ctx, "orders", store.AggregateSum, "price",
		"delivered_at >= $2 AND delivered_at < $3 AND balance_paid = true AND status <> $4",
		start, end, orderdomain.StatusCancelled).Scan(&revenue)
	if err != nil {
		return RecentStats{}, err
	}
	return RecentStats{
		OrdersCreated:    int(created),
		OrdersDelivered:  int(delivered),
		RevenueConfirmed: int(revenue),
	}, nil
}

func fetchNameMap(ctx context.Context, scope store.AccountScope, table, nameColumn string, ids []string) (map[string]string, error) {
	names := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := scope.Query(ctx, table, "id, "+nameColumn, "id = ANY($2::text[])", ids)
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
	return names, rows.Err()
}

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanReminders(rows rowsScanner) ([]reminderdomain.Reminder, error) {
	items := make([]reminderdomain.Reminder, 0)
	for rows.Next() {
		var r reminderdomain.Reminder
		var customerID, orderID sql.NullString
		var due time.Time
		if err := rows.Scan(
			&r.ID, &r.AccountID, &r.CreatedAt, &r.Type,
			&customerID, &orderID, &due, &r.Content, &r.Status, &r.DedupKey,
		); err != nil {
			return nil, err
		}
		if customerID.Valid {
			v := customerID.String
			r.CustomerID = &v
		}
		if orderID.Valid {
			v := orderID.String
			r.OrderID = &v
		}
		r.DueDate = clock.DateOnly(due)
		items = append(items, r)
	}
	return items, rows.Err()
}

func scanOrder(row interface{ Scan(dest ...any) error }) (orderdomain.Order, error) {
	var order orderdomain.Order
	var packageID, title, note sql.NullString
	var price sql.NullInt64
	var shotAt, deliveredAt sql.NullTime
	if err := row.Scan(
		&order.ID, &order.AccountID, &order.CreatedAt, &order.CustomerID,
		&packageID, &title, &order.Status, &price, &order.DepositPaid,
		&order.BalancePaid, &shotAt, &deliveredAt, &note,
	); err != nil {
		return orderdomain.Order{}, err
	}
	if packageID.Valid {
		order.PackageID = &packageID.String
	}
	if title.Valid {
		order.Title = &title.String
	}
	if price.Valid {
		v := int(price.Int64)
		order.Price = &v
	}
	if shotAt.Valid {
		order.ShotAt = &shotAt.Time
	}
	if deliveredAt.Valid {
		order.DeliveredAt = &deliveredAt.Time
	}
	if note.Valid {
		order.Note = &note.String
	}
	return order, nil
}

func sortReminders(items []reminderdomain.Reminder) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].DueDate.Equal(items[j].DueDate) {
			return items[i].DueDate.Before(items[j].DueDate)
		}
		return items[i].ID < items[j].ID
	})
}

func sortUnpaidOrders(orders []orderdomain.Order) {
	sort.SliceStable(orders, func(i, j int) bool {
		a, b := orders[i].DeliveredAt, orders[j].DeliveredAt
		switch {
		case a == nil && b == nil:
			return orders[i].ID < orders[j].ID
		case a == nil:
			return false // NULL 排在后
		case b == nil:
			return true
		case !a.Equal(*b):
			return a.Before(*b)
		default:
			return orders[i].ID < orders[j].ID
		}
	})
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
