package reminder

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

const reminderColumns = "id, account_id, created_at, type, customer_id, order_id, due_date, content, status, dedup_key"

// PostgresRepository 实现 reminder 持久化与跨域扫描取数。
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

// --- Reminder CRUD ---

func (PostgresRepository) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	cond, args := buildListFilter(filter)
	total, err := scope.Count(ctx, "reminders", cond, args...)
	if err != nil {
		return ListResult{}, err
	}
	rows, err := scope.QueryPage(ctx, "reminders", reminderColumns, cond,
		[]store.OrderBy{{Column: "due_date", Desc: false}, {Column: "id", Desc: false}},
		filter.PageSize, (filter.Page-1)*filter.PageSize, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items := make([]Reminder, 0, filter.PageSize)
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, r)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total}, nil
}

func (PostgresRepository) Find(ctx context.Context, scope store.AccountScope, id string) (Reminder, error) {
	r, err := scanReminder(scope.QueryRow(ctx, "reminders", reminderColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Reminder{}, fmt.Errorf("%w: 提醒不存在", ErrNotFound)
	}
	return r, err
}

// InsertIdempotent 用唯一约束幂等写入；返回 inserted=false 表示冲突（skipped）。
func (PostgresRepository) InsertIdempotent(ctx context.Context, scope store.AccountScope, r Reminder) (inserted bool, err error) {
	id := r.ID
	if id == "" {
		id = "rem_" + uuid.NewString()
	}
	row := scope.InsertOnConflictDoNothingReturning(
		ctx,
		"reminders",
		[]string{"id", "type", "customer_id", "order_id", "due_date", "content", "status", "dedup_key"},
		[]string{"account_id", "dedup_key"},
		[]string{"id"},
		id,
		r.Type,
		nullableString(r.CustomerID),
		nullableString(r.OrderID),
		FormatDate(r.DueDate),
		r.Content,
		r.Status,
		r.DedupKey,
	)
	var returned string
	err = row.Scan(&returned)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// CreateCustom 插入 custom 提醒；dedup_key = custom:{id}（D7）。
func (PostgresRepository) CreateCustom(ctx context.Context, scope store.AccountScope, input CreateCustomInput) (Reminder, error) {
	id := "rem_" + uuid.NewString()
	dedup := CustomDedupKey(id)
	if _, err := scope.InsertReturningID(ctx, "reminders",
		[]string{"id", "type", "customer_id", "order_id", "due_date", "content", "status", "dedup_key"},
		id,
		TypeCustom,
		nullableString(input.CustomerID),
		nil,
		FormatDate(input.DueDate),
		input.Content,
		StatusPending,
		dedup,
	); err != nil {
		return Reminder{}, err
	}
	return PostgresRepository{}.Find(ctx, scope, id)
}

// SetStatus 更新状态；仅 pending→目标态，或已是目标态幂等 200。
// 禁止 done↔dismissed 交叉覆盖（review REV-005）。
func (PostgresRepository) SetStatus(ctx context.Context, scope store.AccountScope, id, status string) (Reminder, error) {
	n, err := scope.Update(ctx, "reminders", "status = $2", "id = $3 AND status = $4", status, id, StatusPending)
	if err != nil {
		return Reminder{}, err
	}
	if n == 0 {
		current, findErr := PostgresRepository{}.Find(ctx, scope, id)
		if findErr != nil {
			return Reminder{}, findErr
		}
		if current.Status == status {
			return current, nil
		}
		// 存在但非 pending 且非目标态：拒绝交叉覆盖
		return Reminder{}, ValidationError{Message: "仅 pending 提醒可变更状态"}
	}
	return PostgresRepository{}.Find(ctx, scope, id)
}

// AutoDismissByIDs 批量把 pending 提醒标 dismissed；返回实际更新数。
func (PostgresRepository) AutoDismissByIDs(ctx context.Context, scope store.AccountScope, ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// AccountScope Update 不支持 IN 列表动态长度的安全拼装；逐条更新（扫描批量小）。
	count := 0
	for _, id := range ids {
		n, err := scope.Update(ctx, "reminders", "status = $2", "id = $3 AND status = $4", StatusDismissed, id, StatusPending)
		if err != nil {
			return count, err
		}
		count += int(n)
	}
	return count, nil
}

// ListPendingWithOrder 列出有 order_id 的 pending 提醒（auto-dismiss 孤儿检测）。
func (PostgresRepository) ListPendingWithOrder(ctx context.Context, scope store.AccountScope) ([]Reminder, error) {
	rows, err := scope.Query(ctx, "reminders", reminderColumns, "status = $2 AND order_id IS NOT NULL", StatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectReminders(rows)
}

// ListPendingChurn 列出 pending churn 提醒。
func (PostgresRepository) ListPendingChurn(ctx context.Context, scope store.AccountScope) ([]Reminder, error) {
	rows, err := scope.Query(ctx, "reminders", reminderColumns, "status = $2 AND type = $3", StatusPending, TypeChurn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectReminders(rows)
}

// LoadScanState / SaveScanState 检查点。
func (PostgresRepository) LoadScanState(ctx context.Context, scope store.AccountScope) (last time.Time, found bool, err error) {
	var d time.Time
	err = scope.QueryRow(ctx, "reminder_scan_state", "last_scan_date", "").Scan(&d)
	if errors.Is(err, store.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	return dateOnly(d), true, nil
}

func (PostgresRepository) SaveScanState(ctx context.Context, scope store.AccountScope, date time.Time) error {
	_, found, err := PostgresRepository{}.LoadScanState(ctx, scope)
	if err != nil {
		return err
	}
	dateStr := FormatDate(date)
	if found {
		_, err = scope.Update(ctx, "reminder_scan_state", "last_scan_date = $2", "", dateStr)
		return err
	}
	return scope.Insert(ctx, "reminder_scan_state", []string{"last_scan_date"}, dateStr)
}

// ReassignCustomer 把 source 客户全部提醒改挂 target（merge）。
func (PostgresRepository) ReassignCustomer(ctx context.Context, scope store.AccountScope, targetID, sourceID string) error {
	_, err := scope.Update(ctx, "reminders", "customer_id = $2", "customer_id = $3", targetID, sourceID)
	return err
}

// --- Cross-domain scan reads (D3 batch) ---

// ActiveCustomer 是扫描用客户投影。
type ActiveCustomer struct {
	ID          string
	DisplayName string
	Birthday    *string
}

// ScanOrder 是扫描用订单投影。
type ScanOrder struct {
	ID          string
	CustomerID  string
	PackageID   *string
	Title       *string
	Status      string
	ShotAt      *time.Time
	DeliveredAt *time.Time
}

// ListActiveCustomers 批量取 active 客户。
func (PostgresRepository) ListActiveCustomers(ctx context.Context, scope store.AccountScope) ([]ActiveCustomer, error) {
	rows, err := scope.Query(ctx, "customers", "id, display_name, birthday", "status = $2", "active")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ActiveCustomer, 0)
	for rows.Next() {
		var c ActiveCustomer
		var b sql.NullString
		if err := rows.Scan(&c.ID, &c.DisplayName, &b); err != nil {
			return nil, err
		}
		if b.Valid {
			v := b.String
			c.Birthday = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListNonCancelledOrders 批量取非 cancelled 订单。
func (PostgresRepository) ListNonCancelledOrders(ctx context.Context, scope store.AccountScope) ([]ScanOrder, error) {
	rows, err := scope.Query(ctx, "orders",
		"id, customer_id, package_id, title, status, shot_at, delivered_at",
		"status <> $2", "cancelled")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ScanOrder, 0)
	for rows.Next() {
		var o ScanOrder
		var pkg, title sql.NullString
		var shot, delivered sql.NullTime
		if err := rows.Scan(&o.ID, &o.CustomerID, &pkg, &title, &o.Status, &shot, &delivered); err != nil {
			return nil, err
		}
		if pkg.Valid {
			v := pkg.String
			o.PackageID = &v
		}
		if title.Valid {
			v := title.String
			o.Title = &v
		}
		if shot.Valid {
			t := shot.Time
			o.ShotAt = &t
		}
		if delivered.Valid {
			t := delivered.Time
			o.DeliveredAt = &t
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// PackageMeta 是扫描用套系投影。
type PackageMeta struct {
	ShootType string
	Name      string
}

// ListPackageMeta 返回 package_id → 套系元数据（shoot_type + name）。
func (PostgresRepository) ListPackageMeta(ctx context.Context, scope store.AccountScope) (map[string]PackageMeta, error) {
	rows, err := scope.Query(ctx, "packages", "id, shoot_type, name", "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]PackageMeta)
	for rows.Next() {
		var id, st, name string
		if err := rows.Scan(&id, &st, &name); err != nil {
			return nil, err
		}
		out[id] = PackageMeta{ShootType: st, Name: name}
	}
	return out, rows.Err()
}

// OrderExists 检查订单是否存在。
func (PostgresRepository) OrderExists(ctx context.Context, scope store.AccountScope, orderID string) (bool, error) {
	return scope.Exists(ctx, "orders", "id = $2", orderID)
}

// CustomerExists 跨账号可见性：任意 status 存在即 true（404 判定）。
func (PostgresRepository) CustomerExists(ctx context.Context, scope store.AccountScope, customerID string) (bool, error) {
	return scope.Exists(ctx, "customers", "id = $2", customerID)
}

func buildListFilter(filter ListFilter) (string, []any) {
	parts := make([]string, 0, 3)
	args := make([]any, 0, 3)
	pos := 2 // $1 = account_id
	if filter.Status != "" {
		parts = append(parts, fmt.Sprintf("status = $%d", pos))
		args = append(args, filter.Status)
		pos++
	}
	if filter.CustomerID != "" {
		parts = append(parts, fmt.Sprintf("customer_id = $%d", pos))
		args = append(args, filter.CustomerID)
		pos++
	}
	if filter.DueBefore != nil {
		parts = append(parts, fmt.Sprintf("due_date <= $%d", pos))
		args = append(args, FormatDate(*filter.DueBefore))
	}
	return strings.Join(parts, " AND "), args
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReminder(row rowScanner) (Reminder, error) {
	var r Reminder
	var customerID, orderID sql.NullString
	var due time.Time
	if err := row.Scan(
		&r.ID, &r.AccountID, &r.CreatedAt, &r.Type,
		&customerID, &orderID, &due, &r.Content, &r.Status, &r.DedupKey,
	); err != nil {
		return Reminder{}, err
	}
	if customerID.Valid {
		v := customerID.String
		r.CustomerID = &v
	}
	if orderID.Valid {
		v := orderID.String
		r.OrderID = &v
	}
	r.DueDate = dateOnly(due)
	return r, nil
}

func collectReminders(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]Reminder, error) {
	out := make([]Reminder, 0)
	for rows.Next() {
		r, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func nullableString(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
