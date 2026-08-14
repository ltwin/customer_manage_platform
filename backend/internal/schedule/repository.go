package schedule

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

const (
	slotColumns                = "id, account_id, created_at, start_at, end_at, type, order_id, note"
	shootOrderUniqueConstraint = "schedule_slots_account_shoot_order_uidx"
)

type PlanningScheduleProjectionSink interface {
	LockPlanningReminderFenceInScope(context.Context, store.TxAccountScope) error
	ReprojectScheduleMutationInScope(context.Context, store.TxAccountScope, crm.ScheduleMutationFact, time.Time) error
}

type PostgresRepository struct {
	sink PlanningScheduleProjectionSink
}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (r PostgresRepository) WithProjectionSink(sink PlanningScheduleProjectionSink) PostgresRepository {
	r.sink = sink
	return r
}

func (r PostgresRepository) Create(
	ctx context.Context,
	scope store.AccountScope,
	prepared PreparedCreate,
	now time.Time,
) (CreateResult, error) {
	expectedCustomerID, err := r.LookupOrderCustomerID(ctx, scope, prepared)
	if err != nil {
		return CreateResult{}, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		var result CreateResult
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			var txErr error
			result, txErr = r.CreatePreparedInScope(ctx, tx, prepared, expectedCustomerID, now)
			return txErr
		})
		var moved CustomerChangedError
		if errors.As(err, &moved) {
			if attempt == 0 {
				expectedCustomerID = moved.CustomerID
				continue
			}
			return CreateResult{}, ErrCustomerChanged
		}
		return result, err
	}
	return CreateResult{}, ErrCustomerChanged
}

func (r PostgresRepository) LookupOrderCustomerID(
	ctx context.Context,
	scope store.AccountScope,
	prepared PreparedCreate,
) (string, error) {
	return lookupPreparedOrderCustomerID(ctx, scope, prepared)
}

func (r PostgresRepository) LookupOrderCustomerIDInScope(
	ctx context.Context,
	scope store.TxAccountScope,
	prepared PreparedCreate,
) (string, error) {
	return lookupPreparedOrderCustomerID(ctx, scope, prepared)
}

func lookupPreparedOrderCustomerID(ctx context.Context, scope rowScope, prepared PreparedCreate) (string, error) {
	if prepared.slot.Type != TypeShoot {
		return "", nil
	}
	if prepared.slot.OrderID == nil {
		return "", ValidationError{Message: "shoot 档期必须关联订单"}
	}
	return lookupOrderCustomerID(ctx, scope, *prepared.slot.OrderID)
}

func (r PostgresRepository) CreatePreparedInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	prepared PreparedCreate,
	expectedCustomerID string,
	now time.Time,
) (CreateResult, error) {
	if err := r.lockFence(ctx, tx); err != nil {
		return CreateResult{}, err
	}
	candidate := prepared.slot
	candidate.ID = "slot_" + uuid.NewString()
	if candidate.Type == TypeShoot {
		if err := validateShootReference(ctx, tx, candidate, expectedCustomerID, now); err != nil {
			return CreateResult{}, err
		}
		conflict, err := findOrderConflict(ctx, tx, candidate)
		if err != nil {
			return CreateResult{}, err
		}
		if conflict != nil {
			return CreateResult{}, newOrderConflict(*conflict)
		}
	}
	overlaps, err := findOverlaps(ctx, tx, candidate)
	if err != nil {
		return CreateResult{}, err
	}
	overlaps, err = excludeCancelledShootOverlaps(ctx, tx, overlaps)
	if err != nil {
		return CreateResult{}, err
	}
	var insertedID string
	err = tx.InsertOnConflictDoNothingReturning(
		ctx,
		"schedule_slots",
		[]string{"id", "start_at", "end_at", "type", "order_id", "note"},
		nil,
		[]string{"id"},
		candidate.ID,
		candidate.StartAt,
		candidate.EndAt,
		candidate.Type,
		nullableStringArg(candidate.OrderID),
		nullableStringArg(candidate.Note),
	).Scan(&insertedID)
	if errors.Is(err, store.ErrNoRows) && candidate.Type == TypeShoot {
		conflict, conflictErr := findOrderConflict(ctx, tx, candidate)
		if conflictErr != nil {
			return CreateResult{}, conflictErr
		}
		if conflict != nil {
			return CreateResult{}, newOrderConflict(*conflict)
		}
	}
	if err != nil {
		return CreateResult{}, fmt.Errorf("insert schedule slot: %w", err)
	}
	created, err := findSlot(ctx, tx, insertedID, false)
	if err != nil {
		return CreateResult{}, err
	}
	if err := r.reproject(ctx, tx, crm.ScheduleMutationFact{
		Change:        crm.ScheduleCreate,
		SlotID:        created.ID,
		NewOrderID:    created.OrderID,
		NewCustomerID: stringPtrFromValue(expectedCustomerID),
	}, now); err != nil {
		return CreateResult{}, err
	}
	return CreateResult{Slot: created, Overlaps: slotIDs(overlaps)}, nil
}

func (r PostgresRepository) List(
	ctx context.Context,
	scope store.AccountScope,
	filter ListFilter,
) ([]ListItem, error) {
	return AssembleListItems(ctx, scope, filter)
}

// AssembleListItems 按半开区间 [from, to) 相交查询档期、稳定排序、批量装配 shoot 摘要，
// 返回列表项。schedule 列表与 dashboard 今日档期共用本函数，保证摘要字段与排序语义
// 单一来源、不漂移（design D8）。签名只吃 store.AccountScope，不引入 schedule.Service 注入。
func AssembleListItems(
	ctx context.Context,
	scope store.AccountScope,
	filter ListFilter,
) ([]ListItem, error) {
	rows, err := scope.Query(
		ctx,
		"schedule_slots",
		slotColumns,
		"start_at < $2 AND end_at > $3",
		filter.To,
		filter.From,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	slots := make([]Slot, 0)
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortSlots(slots)
	return assembleListItems(ctx, scope, slots)
}

func (r PostgresRepository) Get(
	ctx context.Context,
	scope store.AccountScope,
	id string,
) (ListItem, error) {
	slot, err := findSlot(ctx, scope, id, false)
	if err != nil {
		return ListItem{}, err
	}
	items, err := assembleListItems(ctx, scope, []Slot{slot})
	if err != nil {
		return ListItem{}, err
	}
	return items[0], nil
}

func assembleListItems(
	ctx context.Context,
	scope store.AccountScope,
	slots []Slot,
) ([]ListItem, error) {
	orderIDs := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.OrderID != nil {
			orderIDs = append(orderIDs, *slot.OrderID)
		}
	}
	summaries, err := fetchOrderSummaries(ctx, scope, uniqueStrings(orderIDs))
	if err != nil {
		return nil, err
	}
	items := make([]ListItem, 0, len(slots))
	for _, slot := range slots {
		item := ListItem{Slot: slot}
		if slot.OrderID != nil {
			summary, ok := summaries[*slot.OrderID]
			if !ok {
				return nil, fmt.Errorf("shoot slot %s 引用摘要缺失", slot.ID)
			}
			item.CustomerID = summary.CustomerID
			item.CustomerDisplayName = summary.CustomerDisplayName
			item.CustomerStatus = summary.CustomerStatus
			item.OrderStatus = summary.OrderStatus
			item.OrderTitle = summary.OrderTitle
			item.OrderPrice = summary.OrderPrice
			item.OrderDepositPaid = summary.OrderDepositPaid
			item.OrderBalancePaid = summary.OrderBalancePaid
			item.PackageName = summary.PackageName
			item.PackageShootType = summary.PackageShootType
		}
		items = append(items, item)
	}
	slotIDsForSummary := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type == TypeShoot {
			slotIDsForSummary = append(slotIDsForSummary, item.ID)
		}
	}
	planningSummaries, err := crm.LoadSummaries(ctx, scope, crm.SummaryViewBySlot, slotIDsForSummary)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if summary, ok := planningSummaries[items[i].ID]; ok {
			copied := summary
			items[i].PlanningSummary = &copied
		}
	}
	return items, nil
}

func (r PostgresRepository) Update(
	ctx context.Context,
	scope store.AccountScope,
	id string,
	input UpdateInput,
	now time.Time,
) (Slot, error) {
	current, err := findSlot(ctx, scope, id, false)
	if err != nil {
		return Slot{}, err
	}
	next, err := ApplyUpdate(current, input)
	if err != nil {
		return Slot{}, err
	}
	expectedCustomerID := ""
	if NeedsExternalReferenceValidation(current, next) && next.Type == TypeShoot {
		expectedCustomerID, err = lookupOrderCustomerID(ctx, scope, *next.OrderID)
		if err != nil {
			return Slot{}, err
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		var updated Slot
		var candidate Slot
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if err := r.lockFence(ctx, tx); err != nil {
				return err
			}
			unlocked, err := findSlot(ctx, tx, id, false)
			if err != nil {
				return err
			}
			candidate, err = ApplyUpdate(unlocked, input)
			if err != nil {
				return err
			}
			oldCustomerID, newCustomerID, err := r.lockScheduleGraph(ctx, tx, unlocked, candidate)
			if err != nil {
				return err
			}
			_ = oldCustomerID
			locked, err := findSlot(ctx, tx, id, true)
			if err != nil {
				return err
			}
			candidate, err = ApplyUpdate(locked, input)
			if err != nil {
				return err
			}
			if NeedsExternalReferenceValidation(locked, candidate) && candidate.Type == TypeShoot {
				if err := validateShootReference(ctx, tx, candidate, expectedCustomerID, now); err != nil {
					return err
				}
				conflict, err := findOrderConflict(ctx, tx, candidate)
				if err != nil {
					return err
				}
				if conflict != nil {
					return newOrderConflict(*conflict)
				}
			}
			if _, err := tx.Update(
				ctx,
				"schedule_slots",
				"start_at = $2, end_at = $3, type = $4, order_id = $5, note = $6",
				"id = $7",
				candidate.StartAt,
				candidate.EndAt,
				candidate.Type,
				nullableStringArg(candidate.OrderID),
				nullableStringArg(candidate.Note),
				id,
			); err != nil {
				return err
			}
			updated, err = findSlot(ctx, tx, id, false)
			if err != nil {
				return err
			}
			return r.reproject(ctx, tx, scheduleChangeFact(locked, updated, oldCustomerID, newCustomerID), now)
		})
		var moved CustomerChangedError
		if errors.As(err, &moved) {
			if attempt == 0 {
				expectedCustomerID = moved.CustomerID
				continue
			}
			return Slot{}, ErrCustomerChanged
		}
		if store.IsUniqueViolation(err, shootOrderUniqueConstraint) {
			conflict, conflictErr := findOrderConflict(ctx, scope, candidate)
			if conflictErr != nil {
				return Slot{}, conflictErr
			}
			if conflict != nil {
				return Slot{}, newOrderConflict(*conflict)
			}
		}
		return updated, err
	}
	return Slot{}, ErrCustomerChanged
}

func (r PostgresRepository) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	current, err := findSlot(ctx, scope, id, false)
	if err != nil {
		return err
	}
	expectedCustomerID := ""
	if current.Type == TypeShoot && current.OrderID != nil {
		expectedCustomerID, err = lookupOrderCustomerID(ctx, scope, *current.OrderID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	for attempt := 0; attempt < 3; attempt++ {
		err = scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
			if err := r.lockFence(ctx, tx); err != nil {
				return err
			}
			unlocked, err := findSlot(ctx, tx, id, false)
			if err != nil {
				return err
			}
			oldCustomerID, _, err := r.lockScheduleGraph(ctx, tx, unlocked, Slot{})
			if err != nil {
				return err
			}
			if unlocked.Type == TypeShoot && unlocked.OrderID != nil && expectedCustomerID != "" {
				actual, err := lookupOrderCustomerID(ctx, tx, *unlocked.OrderID)
				if err != nil {
					return err
				}
				if actual != expectedCustomerID {
					return CustomerChangedError{CustomerID: actual}
				}
			}
			locked, err := findSlot(ctx, tx, id, true)
			if err != nil {
				return err
			}
			if _, err := tx.Delete(ctx, "schedule_slots", "id = $2", id); err != nil {
				return err
			}
			return r.reproject(ctx, tx, crm.ScheduleMutationFact{
				Change:        crm.ScheduleDelete,
				SlotID:        locked.ID,
				OldOrderID:    locked.OrderID,
				OldCustomerID: stringPtrFromValue(oldCustomerID),
			}, time.Now().UTC())
		})
		var moved CustomerChangedError
		if errors.As(err, &moved) {
			if attempt < 2 {
				expectedCustomerID = moved.CustomerID
				continue
			}
			return ErrCustomerChanged
		}
		return err
	}
	return ErrCustomerChanged
}

type rowScope interface {
	QueryRow(context.Context, string, string, string, ...any) store.Row
	QueryRowForUpdate(context.Context, string, string, string, ...any) store.Row
}

type queryScope interface {
	Query(context.Context, string, string, string, ...any) (store.Rows, error)
}

func lookupOrderCustomerID(ctx context.Context, scope rowScope, orderID string) (string, error) {
	var customerID string
	err := scope.QueryRow(ctx, "orders", "customer_id", "id = $2", orderID).Scan(&customerID)
	if errors.Is(err, store.ErrNoRows) {
		return "", fmt.Errorf("%w: 订单不存在", ErrNotFound)
	}
	if err != nil {
		return "", err
	}
	return customerID, nil
}

func validateShootReference(
	ctx context.Context,
	tx store.TxAccountScope,
	slot Slot,
	expectedCustomerID string,
	now time.Time,
) error {
	var customerStatus string
	err := tx.QueryRowForUpdate(
		ctx,
		"customers",
		"status",
		"id = $2",
		expectedCustomerID,
	).Scan(&customerStatus)
	if errors.Is(err, store.ErrNoRows) {
		return fmt.Errorf("%w: 客户不存在", ErrNotFound)
	}
	if err != nil {
		return err
	}

	var actualCustomerID, orderStatus string
	err = tx.QueryRowForUpdate(
		ctx,
		"orders",
		"customer_id, status",
		"id = $2",
		*slot.OrderID,
	).Scan(&actualCustomerID, &orderStatus)
	if errors.Is(err, store.ErrNoRows) {
		return fmt.Errorf("%w: 订单不存在", ErrNotFound)
	}
	if err != nil {
		return err
	}
	if actualCustomerID != expectedCustomerID {
		return CustomerChangedError{CustomerID: actualCustomerID}
	}
	if customerStatus == "merged" || (slot.EndAt.After(now) && customerStatus != "active") {
		return fmt.Errorf("%w: 客户状态不可用于目标档期", ErrCustomerArchived)
	}
	if !orderdomain.CanScheduleAt(orderStatus, customerStatus, slot.EndAt, now) {
		return ValidationError{Message: "订单状态不可用于目标档期"}
	}
	return nil
}

func findSlot(ctx context.Context, scope rowScope, id string, forUpdate bool) (Slot, error) {
	var row store.Row
	if forUpdate {
		row = scope.QueryRowForUpdate(ctx, "schedule_slots", slotColumns, "id = $2", id)
	} else {
		row = scope.QueryRow(ctx, "schedule_slots", slotColumns, "id = $2", id)
	}
	slot, err := scanSlot(row)
	if errors.Is(err, store.ErrNoRows) {
		return Slot{}, fmt.Errorf("%w: 档期不存在", ErrNotFound)
	}
	return slot, err
}

func scanSlot(row interface{ Scan(...any) error }) (Slot, error) {
	var slot Slot
	var orderID, note sql.NullString
	err := row.Scan(
		&slot.ID,
		&slot.AccountID,
		&slot.CreatedAt,
		&slot.StartAt,
		&slot.EndAt,
		&slot.Type,
		&orderID,
		&note,
	)
	if err != nil {
		return Slot{}, err
	}
	slot.OrderID = stringPtr(orderID)
	slot.Note = stringPtr(note)
	return slot, nil
}

func findOrderConflict(ctx context.Context, scope rowScope, candidate Slot) (*Slot, error) {
	if candidate.Type != TypeShoot || candidate.OrderID == nil {
		return nil, nil
	}
	cond := "type = 'shoot' AND order_id = $2"
	args := []any{*candidate.OrderID}
	if candidate.ID != "" {
		cond += " AND id <> $3"
		args = append(args, candidate.ID)
	}
	var slot Slot
	err := scope.QueryRow(ctx, "schedule_slots", "id, start_at", cond, args...).Scan(&slot.ID, &slot.StartAt)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &slot, nil
}

func findOverlaps(ctx context.Context, scope queryScope, candidate Slot) ([]Slot, error) {
	cond := "start_at < $2 AND end_at > $3"
	args := []any{candidate.EndAt, candidate.StartAt}
	if candidate.ID != "" {
		cond += " AND id <> $4"
		args = append(args, candidate.ID)
	}
	rows, err := scope.Query(ctx, "schedule_slots", slotColumns, cond, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	slots := make([]Slot, 0)
	for rows.Next() {
		slot, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortSlots(slots)
	return slots, nil
}

func excludeCancelledShootOverlaps(ctx context.Context, scope queryScope, slots []Slot) ([]Slot, error) {
	orderIDs := make([]string, 0, len(slots))
	for _, slot := range slots {
		if slot.Type == TypeShoot && slot.OrderID != nil {
			orderIDs = append(orderIDs, *slot.OrderID)
		}
	}
	orderIDs = uniqueStrings(orderIDs)
	if len(orderIDs) == 0 {
		return slots, nil
	}

	rows, err := scope.Query(ctx, "orders", "id, status", "id = ANY($2::text[])", orderIDs)
	if err != nil {
		return nil, err
	}
	statuses := make(map[string]string, len(orderIDs))
	for rows.Next() {
		var id, status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			return nil, err
		}
		statuses[id] = status
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	result := make([]Slot, 0, len(slots))
	for _, slot := range slots {
		if slot.Type == TypeShoot && slot.OrderID != nil {
			status, ok := statuses[*slot.OrderID]
			if !ok {
				return nil, fmt.Errorf("shoot slot %s 引用订单状态缺失", slot.ID)
			}
			if status == orderdomain.StatusCancelled {
				continue
			}
		}
		result = append(result, slot)
	}
	return result, nil
}

func newOrderConflict(slot Slot) error {
	return OrderAlreadyScheduledError{SlotID: slot.ID, StartAt: slot.StartAt}
}

func sortSlots(slots []Slot) {
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].StartAt.Equal(slots[j].StartAt) {
			return slots[i].ID < slots[j].ID
		}
		return slots[i].StartAt.Before(slots[j].StartAt)
	})
}

func slotIDs(slots []Slot) []string {
	ids := make([]string, 0, len(slots))
	for _, slot := range slots {
		ids = append(ids, slot.ID)
	}
	return ids
}

type orderSummary struct {
	CustomerID          string
	CustomerDisplayName string
	CustomerStatus      string
	OrderStatus         string
	OrderTitle          *string
	OrderPrice          *int
	OrderDepositPaid    bool
	OrderBalancePaid    bool
	PackageName         *string
	PackageShootType    *string
}

type orderRow struct {
	ID          string
	CustomerID  string
	PackageID   *string
	Title       *string
	Status      string
	Price       *int
	DepositPaid bool
	BalancePaid bool
}

func fetchOrderSummaries(
	ctx context.Context,
	scope store.AccountScope,
	orderIDs []string,
) (map[string]orderSummary, error) {
	if len(orderIDs) == 0 {
		return map[string]orderSummary{}, nil
	}
	rows, err := scope.Query(
		ctx,
		"orders",
		"id, customer_id, package_id, title, status, price, deposit_paid, balance_paid",
		"id = ANY($2::text[])",
		orderIDs,
	)
	if err != nil {
		return nil, err
	}
	orders := make([]orderRow, 0, len(orderIDs))
	customerIDs := make([]string, 0, len(orderIDs))
	packageIDs := make([]string, 0, len(orderIDs))
	for rows.Next() {
		var order orderRow
		var packageID, title sql.NullString
		var price sql.NullInt64
		if err := rows.Scan(
			&order.ID,
			&order.CustomerID,
			&packageID,
			&title,
			&order.Status,
			&price,
			&order.DepositPaid,
			&order.BalancePaid,
		); err != nil {
			rows.Close()
			return nil, err
		}
		order.PackageID = stringPtr(packageID)
		order.Title = stringPtr(title)
		order.Price = intPtr(price)
		orders = append(orders, order)
		customerIDs = append(customerIDs, order.CustomerID)
		if order.PackageID != nil {
			packageIDs = append(packageIDs, *order.PackageID)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	customers, err := fetchCustomers(ctx, scope, uniqueStrings(customerIDs))
	if err != nil {
		return nil, err
	}
	packages, err := fetchPackages(ctx, scope, uniqueStrings(packageIDs))
	if err != nil {
		return nil, err
	}
	summaries := make(map[string]orderSummary, len(orders))
	for _, order := range orders {
		customer, ok := customers[order.CustomerID]
		if !ok {
			return nil, fmt.Errorf("订单 %s 客户摘要缺失", order.ID)
		}
		summary := orderSummary{
			CustomerID:          order.CustomerID,
			CustomerDisplayName: customer.Name,
			CustomerStatus:      customer.Status,
			OrderStatus:         order.Status,
			OrderTitle:          order.Title,
			OrderPrice:          order.Price,
			OrderDepositPaid:    order.DepositPaid,
			OrderBalancePaid:    order.BalancePaid,
		}
		if order.PackageID != nil {
			if pkg, ok := packages[*order.PackageID]; ok {
				name := pkg.Name
				shootType := pkg.ShootType
				summary.PackageName = &name
				summary.PackageShootType = &shootType
			}
		}
		summaries[order.ID] = summary
	}
	return summaries, nil
}

type customerSummary struct {
	Name   string
	Status string
}

func fetchCustomers(ctx context.Context, scope store.AccountScope, ids []string) (map[string]customerSummary, error) {
	result := make(map[string]customerSummary, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := scope.Query(ctx, "customers", "id, display_name, status", "id = ANY($2::text[])", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var summary customerSummary
		if err := rows.Scan(&id, &summary.Name, &summary.Status); err != nil {
			return nil, err
		}
		result[id] = summary
	}
	return result, rows.Err()
}

type packageSummary struct {
	Name      string
	ShootType string
}

func fetchPackages(ctx context.Context, scope store.AccountScope, ids []string) (map[string]packageSummary, error) {
	result := make(map[string]packageSummary, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := scope.Query(ctx, "packages", "id, name, shoot_type", "id = ANY($2::text[])", ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var summary packageSummary
		if err := rows.Scan(&id, &summary.Name, &summary.ShootType); err != nil {
			return nil, err
		}
		result[id] = summary
	}
	return result, rows.Err()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func (r PostgresRepository) lockFence(ctx context.Context, tx store.TxAccountScope) error {
	if r.sink == nil {
		return nil
	}
	return r.sink.LockPlanningReminderFenceInScope(ctx, tx)
}

func (r PostgresRepository) reproject(ctx context.Context, tx store.TxAccountScope, fact crm.ScheduleMutationFact, now time.Time) error {
	if r.sink == nil {
		return nil
	}
	return r.sink.ReprojectScheduleMutationInScope(ctx, tx, fact, now)
}

func (r PostgresRepository) lockScheduleGraph(ctx context.Context, tx store.TxAccountScope, oldSlot, newSlot Slot) (string, string, error) {
	oldCustomerID, err := customerIDOfSlot(ctx, tx, oldSlot)
	if err != nil {
		return "", "", err
	}
	newCustomerID, err := customerIDOfSlot(ctx, tx, newSlot)
	if err != nil {
		return "", "", err
	}
	if err := lockSortedIDs(ctx, tx, "customers", oldCustomerID, newCustomerID); err != nil {
		return "", "", err
	}
	if err := lockSortedIDs(ctx, tx, "orders", derefOrderID(oldSlot.OrderID), derefOrderID(newSlot.OrderID)); err != nil {
		return "", "", err
	}
	return oldCustomerID, newCustomerID, nil
}

func customerIDOfSlot(ctx context.Context, tx store.TxAccountScope, slot Slot) (string, error) {
	if slot.Type != TypeShoot || slot.OrderID == nil || *slot.OrderID == "" {
		return "", nil
	}
	id, err := lookupOrderCustomerID(ctx, tx, *slot.OrderID)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	return id, err
}

func lockSortedIDs(ctx context.Context, tx store.TxAccountScope, table string, ids ...string) error {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	sort.Strings(unique)
	for _, id := range unique {
		var found string
		err := tx.QueryRowForUpdate(ctx, table, "id", "id = $2", id).Scan(&found)
		if errors.Is(err, store.ErrNoRows) {
			return fmt.Errorf("%w: %s 不存在", ErrNotFound, table)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func scheduleChangeFact(before, after Slot, oldCustomerID, newCustomerID string) crm.ScheduleMutationFact {
	change := crm.ScheduleUpdate
	if before.Type != after.Type {
		change = crm.ScheduleTypeChanged
	} else if derefOrderID(before.OrderID) != derefOrderID(after.OrderID) {
		change = crm.ScheduleOrderMoved
	}
	return crm.ScheduleMutationFact{
		Change:        change,
		SlotID:        after.ID,
		OldOrderID:    before.OrderID,
		NewOrderID:    after.OrderID,
		OldCustomerID: stringPtrFromValue(oldCustomerID),
		NewCustomerID: stringPtrFromValue(newCustomerID),
	}
}

func derefOrderID(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func stringPtrFromValue(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
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
	converted := int(value.Int64)
	return &converted
}
