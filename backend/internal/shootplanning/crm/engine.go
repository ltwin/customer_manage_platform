package crm

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
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/planningreminder"
)

type Engine struct {
	reminder        CRMReminderLifecycleParticipant
	reminderEnabled bool
	now             func() time.Time
}

func NewEngine() *Engine {
	return &Engine{
		reminder: DisabledCRMReminderLifecycleParticipant{},
		now:      time.Now,
	}
}

func (e *Engine) WithReminder(participant CRMReminderLifecycleParticipant, enabled bool) (*Engine, error) {
	if e == nil {
		return nil, errors.New("crm engine is required")
	}
	if participant == nil {
		return nil, errors.New("crm reminder participant is required")
	}
	if enabled && isDisabledReminderParticipant(participant) {
		return nil, ErrReminderWiringMismatch
	}
	if !enabled && !isDisabledReminderParticipant(participant) {
		return nil, ErrReminderWiringMismatch
	}
	e.reminder = participant
	e.reminderEnabled = enabled
	return e, nil
}

func (e *Engine) clock() time.Time {
	if e == nil || e.now == nil {
		return time.Now().UTC()
	}
	return e.now().UTC()
}

type ApplyOutcome struct {
	PlanID     string
	Revision   int64
	Status     string
	Connection Connection
	Projection *Projection
	Window     *Window
	Noop       bool
}

func (e *Engine) ApplyCommandInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	expectedRevision int64,
	command Command,
) (ApplyOutcome, error) {
	now := e.clock()
	needsReminder := e.reminderEnabled && reminderCommand(command.Kind)
	var fence planningreminder.LockedFenceTx
	var err error
	if needsReminder {
		fence, err = tx.PlanningReminderFence().LockCurrentAccount(ctx)
		if err != nil {
			return ApplyOutcome{}, err
		}
	}
	preOrder, preSlot, preCustomerIDs, err := e.preReadSources(ctx, tx, planID, command)
	if err != nil {
		return ApplyOutcome{}, err
	}
	if err := e.lockSources(ctx, tx, command, preOrder, preSlot, preCustomerIDs); err != nil {
		return ApplyOutcome{}, err
	}
	if err := e.recheckSources(ctx, tx, preOrder, preSlot); err != nil {
		return ApplyOutcome{}, err
	}
	if err := lockPlanID(ctx, tx, planID); err != nil {
		return ApplyOutcome{}, err
	}
	plan, err := loadPlanFact(ctx, tx, planID)
	if err != nil {
		return ApplyOutcome{}, err
	}
	if plan.Revision != expectedRevision {
		return ApplyOutcome{}, ErrPlanRevisionConflict
	}
	conn, err := LoadConnection(ctx, tx, planID, true)
	if err != nil {
		return ApplyOutcome{}, err
	}
	proj, err := LoadProjection(ctx, tx, planID, true)
	if err != nil {
		return ApplyOutcome{}, err
	}
	window, err := LoadWindow(ctx, tx, planID)
	if err != nil {
		return ApplyOutcome{}, err
	}
	timezone, err := LoadAccountTimezone(ctx, tx)
	if err != nil {
		return ApplyOutcome{}, err
	}
	customer, order, slot, err := e.loadLockedFacts(ctx, tx, command, conn)
	if err != nil {
		return ApplyOutcome{}, err
	}
	result, err := Reduce(ReduceInput{
		Connection: conn,
		Projection: proj,
		Window:     window,
		Plan:       plan,
		Command:    command,
		Customer:   customer,
		Order:      order,
		Slot:       slot,
		Timezone:   timezone,
		Now:        now,
		NewEpochID: "ple_" + uuid.NewString(),
	})
	if err != nil {
		return ApplyOutcome{}, err
	}
	if err := PersistResult(ctx, tx, plan, conn, proj, window, result); err != nil {
		return ApplyOutcome{}, err
	}
	revision := plan.Revision
	if result.WindowChanged && activeLifecycle(plan.Status) {
		revision++
	}
	if err := e.applyReminder(ctx, tx, fence, plan.ID, result, order, slot, now); err != nil {
		return ApplyOutcome{}, err
	}
	return ApplyOutcome{
		PlanID: plan.ID, Revision: revision, Status: plan.Status,
		Connection: result.Connection, Projection: result.Projection, Window: result.Window,
		Noop: !result.ConnectionChanged && !result.ProjectionChanged && !result.WindowChanged,
	}, nil
}

func (e *Engine) ApplyManualInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	expectedRevision int64,
	command Command,
) (ApplyOutcome, error) {
	command.Kind = normalizeManual(command.Kind)
	return e.ApplyCommandInScope(ctx, tx, planID, expectedRevision, command)
}

func (e *Engine) MigratePlanCustomersInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	sourceID, targetID string,
	now time.Time,
) error {
	ids := uniqueSorted(sourceID, targetID)
	if err := lockIDs(ctx, tx, "customers", ids); err != nil {
		return err
	}
	planIDs, err := listPlanIDsByCustomers(ctx, tx, ids)
	if err != nil {
		return err
	}
	orderIDs, err := listOrderIDsByCustomers(ctx, tx, ids)
	if err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "orders", orderIDs); err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "shoot_plans", planIDs); err != nil {
		return err
	}
	for _, planID := range planIDs {
		if err := e.applySource(ctx, tx, planID, Command{
			Kind: KindCustomerMerged, MergeTargetCustomerID: stringPtr(targetID),
		}, now); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) BeforeOrderDeleteInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	orderID, customerID, status string,
	now time.Time,
) error {
	return e.applyOrderLifecycle(ctx, tx, orderID, customerID, status, KindOrderDeleted, now)
}

func (e *Engine) OnOrderCancelledInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	orderID, customerID, status string,
	now time.Time,
) error {
	return e.applyOrderLifecycle(ctx, tx, orderID, customerID, status, KindOrderCancelled, now)
}

type ScheduleMutationFact struct {
	Change        ScheduleChange
	SlotID        string
	OldOrderID    *string
	NewOrderID    *string
	OldCustomerID *string
	NewCustomerID *string
}

func (e *Engine) MaybeLockFence(ctx context.Context, tx store.TxAccountScope) (planningreminder.LockedFenceTx, error) {
	if e == nil || !e.reminderEnabled {
		return nil, nil
	}
	return tx.PlanningReminderFence().LockCurrentAccount(ctx)
}

func (e *Engine) LockPlanningReminderFenceInScope(ctx context.Context, tx store.TxAccountScope) error {
	_, err := e.MaybeLockFence(ctx, tx)
	return err
}

func (e *Engine) ReprojectScheduleMutationInScope(
	ctx context.Context,
	tx store.TxAccountScope,
	fact ScheduleMutationFact,
	now time.Time,
) error {
	orderIDs := uniqueSorted(deref(fact.OldOrderID), deref(fact.NewOrderID))
	planIDs, err := listPlanIDsByOrders(ctx, tx, orderIDs)
	if err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "shoot_plans", planIDs); err != nil {
		return err
	}
	for _, planID := range planIDs {
		if err := e.applySource(ctx, tx, planID, Command{Kind: KindSchedule, ScheduleChange: fact.Change}, now); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applyOrderLifecycle(
	ctx context.Context,
	tx store.TxAccountScope,
	orderID, customerID, status string,
	kind CommandKind,
	now time.Time,
) error {
	_ = customerID
	if err := lockIDs(ctx, tx, "orders", []string{orderID}); err != nil {
		return err
	}
	slotID, err := lookupShootSlotID(ctx, tx, orderID)
	if err != nil {
		return err
	}
	if slotID != "" {
		if err := lockIDs(ctx, tx, "schedule_slots", []string{slotID}); err != nil {
			return err
		}
	}
	planIDs, err := listPlanIDsByOrders(ctx, tx, []string{orderID})
	if err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "shoot_plans", planIDs); err != nil {
		return err
	}
	_ = status
	for _, planID := range planIDs {
		if err := e.applySource(ctx, tx, planID, Command{Kind: kind, OrderID: stringPtr(orderID)}, now); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applySource(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
	command Command,
	now time.Time,
) error {
	plan, err := loadPlanFact(ctx, tx, planID)
	if err != nil {
		return err
	}
	conn, err := LoadConnection(ctx, tx, planID, true)
	if err != nil {
		return err
	}
	proj, err := LoadProjection(ctx, tx, planID, true)
	if err != nil {
		return err
	}
	window, err := LoadWindow(ctx, tx, planID)
	if err != nil {
		return err
	}
	timezone, err := LoadAccountTimezone(ctx, tx)
	if err != nil {
		return err
	}
	customer, order, slot, err := e.loadLockedFacts(ctx, tx, command, conn)
	if err != nil {
		return err
	}
	result, err := Reduce(ReduceInput{
		Connection: conn, Projection: proj, Window: window, Plan: plan,
		Command: command, Customer: customer, Order: order, Slot: slot,
		Timezone: timezone, Now: now, NewEpochID: "ple_" + uuid.NewString(),
	})
	if err != nil {
		return err
	}
	if err := PersistResult(ctx, tx, plan, conn, proj, window, result); err != nil {
		return err
	}
	if !e.reminderEnabled {
		return nil
	}
	fence, err := e.MaybeLockFence(ctx, tx)
	if err != nil {
		return err
	}
	return e.applyReminder(ctx, tx, fence, plan.ID, result, order, slot, now)
}

func (e *Engine) applyReminder(
	ctx context.Context,
	tx store.TxAccountScope,
	fence planningreminder.LockedFenceTx,
	planID string,
	result Result,
	order *OrderFact,
	slot *SlotFact,
	now time.Time,
) error {
	if fence == nil || !e.reminderEnabled {
		return nil
	}
	fact := reminderFact(planID, result, order, slot)
	if fact == nil {
		return nil
	}
	kind := planningreminder.MutationCRMPlanLinkChanged
	switch fact.(type) {
	case CRMOrderLifecycleChangedFactV1:
		kind = planningreminder.MutationCRMOrderLifecycleChanged
	case CRMScheduleChangedFactV1:
		kind = planningreminder.MutationCRMScheduleChanged
	}
	generation, err := fence.ReserveGeneration(ctx, planningreminder.MutationFact{PlanID: planID, MutationKind: kind})
	if err != nil {
		return err
	}
	if err := e.reminder.RecomputeForCRMFactInScope(ctx, tx, fact, generation, now); err != nil {
		return err
	}
	if err := WriteGenerationResolution(ctx, tx, planID, generation); err != nil {
		return err
	}
	return fence.MarkApplied(ctx, generation)
}

func reminderFact(planID string, result Result, order *OrderFact, slot *SlotFact) CRMReminderLifecycleFactV1 {
	var projRev *int64
	if result.Projection != nil && result.Projection.Exists {
		projRev = int64Ptr(result.Projection.ProjectionRevision)
	}
	currentSlot := reminderSlot(result.Projection, slot)
	if result.ReminderLinkChange != nil {
		var status *string
		if order != nil {
			status = stringPtr(order.Status)
		}
		return CRMPlanLinkChangedFactV1{
			PlanID: planID, Change: *result.ReminderLinkChange,
			ConnectionRevision: result.Connection.ConnectionRevision,
			ProjectionRevision: projRev, LinkEpochID: result.Connection.LinkEpochID,
			CustomerID: result.Connection.CustomerID, OrderID: result.Connection.OrderID,
			OrderStatus: status, CurrentShootSlot: currentSlot,
		}
	}
	if result.ReminderOrderChange != nil {
		source := deref(result.Connection.OrderID)
		if order != nil {
			source = order.ID
		}
		var status *string
		if order != nil {
			status = stringPtr(order.Status)
		}
		return CRMOrderLifecycleChangedFactV1{
			PlanID: planID, Change: *result.ReminderOrderChange, SourceOrderID: source,
			ConnectionRevision: result.Connection.ConnectionRevision, ProjectionRevision: projRev,
			LinkEpochID: result.Connection.LinkEpochID, CustomerID: result.Connection.CustomerID,
			CurrentOrderID: result.Connection.OrderID, CurrentOrderStatus: status, CurrentShootSlot: currentSlot,
		}
	}
	if result.ReminderSchedule != nil && result.Projection != nil {
		source := deref(result.Projection.SlotID)
		var status *string
		if order != nil {
			status = stringPtr(order.Status)
		}
		return CRMScheduleChangedFactV1{
			PlanID: planID, Change: *result.ReminderSchedule, SourceSlotID: source,
			ConnectionRevision: result.Connection.ConnectionRevision,
			ProjectionRevision: result.Projection.ProjectionRevision,
			LinkEpochID:        result.Connection.LinkEpochID, CustomerID: result.Connection.CustomerID,
			OrderID: result.Connection.OrderID, OrderStatus: status, CurrentShootSlot: currentSlot,
		}
	}
	return nil
}

func reminderSlot(proj *Projection, slot *SlotFact) *ReminderShootSlotFactV1 {
	if slot == nil || slot.Type != "shoot" {
		return nil
	}
	tz := ""
	if proj != nil && proj.Timezone != nil {
		tz = *proj.Timezone
	}
	return &ReminderShootSlotFactV1{
		SlotID: slot.ID, OrderID: slot.OrderID, Timezone: tz,
		StartsAt: slot.StartAt, EndsAt: slot.EndAt, SlotRevision: slot.Revision, Type: slot.Type,
	}
}

func (e *Engine) preReadSources(ctx context.Context, tx store.TxAccountScope, planID string, command Command) (*OrderFact, *SlotFact, []string, error) {
	customerIDs := make([]string, 0, 2)
	if command.CustomerID != nil {
		customerIDs = append(customerIDs, *command.CustomerID)
	}
	orderID := deref(command.OrderID)
	if orderID == "" || command.Kind != KindLinkOrder {
		conn, err := LoadConnection(ctx, tx, planID, false)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, nil, nil, err
		}
		if err == nil {
			customerIDs = append(customerIDs, deref(conn.CustomerID))
			if orderID == "" {
				orderID = deref(conn.OrderID)
			}
		}
	}
	if orderID == "" {
		return nil, nil, customerIDs, nil
	}
	order, err := loadOrderFact(ctx, tx, orderID, false)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, nil, nil, err
	}
	if errors.Is(err, ErrNotFound) {
		return nil, nil, customerIDs, nil
	}
	customerIDs = append(customerIDs, order.CustomerID)
	slot, err := loadShootSlotFact(ctx, tx, order.ID, false)
	if err != nil {
		return nil, nil, nil, err
	}
	return order, slot, customerIDs, nil
}

func (e *Engine) lockSources(ctx context.Context, tx store.TxAccountScope, command Command, order *OrderFact, slot *SlotFact, extraCustomers []string) error {
	customerIDs := append([]string{}, extraCustomers...)
	orderIDs := make([]string, 0, 2)
	slotIDs := make([]string, 0, 1)
	if command.CustomerID != nil {
		customerIDs = append(customerIDs, *command.CustomerID)
	}
	if order != nil {
		customerIDs = append(customerIDs, order.CustomerID)
		orderIDs = append(orderIDs, order.ID)
	}
	if command.OrderID != nil {
		orderIDs = append(orderIDs, *command.OrderID)
	}
	if slot != nil {
		slotIDs = append(slotIDs, slot.ID)
	}
	if err := lockIDs(ctx, tx, "customers", uniqueSorted(customerIDs...)); err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "orders", uniqueSorted(orderIDs...)); err != nil {
		return err
	}
	return lockIDs(ctx, tx, "schedule_slots", uniqueSorted(slotIDs...))
}

func (e *Engine) recheckSources(ctx context.Context, tx store.TxAccountScope, order *OrderFact, slot *SlotFact) error {
	if order != nil {
		current, err := loadOrderFact(ctx, tx, order.ID, false)
		if err != nil {
			return err
		}
		if current.CustomerID != order.CustomerID || current.Status != order.Status {
			return ErrSourceChangedRetry
		}
	}
	if slot != nil {
		current, err := loadShootSlotFact(ctx, tx, slot.OrderID, false)
		if err != nil {
			return err
		}
		if current == nil || current.ID != slot.ID || !current.StartAt.Equal(slot.StartAt) || !current.EndAt.Equal(slot.EndAt) || current.Type != slot.Type {
			return ErrSourceChangedRetry
		}
	}
	return nil
}

func (e *Engine) loadLockedFacts(ctx context.Context, tx store.TxAccountScope, command Command, conn Connection) (*CustomerFact, *OrderFact, *SlotFact, error) {
	customerID := deref(command.CustomerID)
	if customerID == "" {
		customerID = deref(conn.CustomerID)
	}
	orderID := deref(command.OrderID)
	if orderID == "" {
		orderID = deref(conn.OrderID)
	}
	var customer *CustomerFact
	var order *OrderFact
	var slot *SlotFact
	var err error
	if orderID != "" && command.Kind != KindOrderDeleted {
		order, err = loadOrderFact(ctx, tx, orderID, false)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, nil, nil, err
		}
		if order != nil {
			customerID = order.CustomerID
			slot, err = loadShootSlotFact(ctx, tx, order.ID, false)
			if err != nil {
				return nil, nil, nil, err
			}
		}
	}
	if customerID != "" {
		customer, err = loadCustomerFact(ctx, tx, customerID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, nil, nil, err
		}
	}
	return customer, order, slot, nil
}

func loadPlanFact(ctx context.Context, tx store.TxAccountScope, planID string) (PlanFact, error) {
	var plan PlanFact
	err := tx.QueryRow(ctx, "shoot_plans", "id, status, revision", "id = $2", planID).Scan(&plan.ID, &plan.Status, &plan.Revision)
	if errors.Is(err, store.ErrNoRows) {
		return PlanFact{}, ErrNotFound
	}
	return plan, err
}

func loadCustomerFact(ctx context.Context, tx store.TxAccountScope, id string) (*CustomerFact, error) {
	var fact CustomerFact
	err := tx.QueryRow(ctx, "customers", "id, status", "id = $2", id).Scan(&fact.ID, &fact.Status)
	if errors.Is(err, store.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &fact, nil
}

func loadOrderFact(ctx context.Context, tx store.TxAccountScope, id string, forUpdate bool) (*OrderFact, error) {
	var row store.Row
	if forUpdate {
		row = tx.QueryRowForUpdate(ctx, "orders", "id, customer_id, status, title, package_id", "id = $2", id)
	} else {
		row = tx.QueryRow(ctx, "orders", "id, customer_id, status, title, package_id", "id = $2", id)
	}
	var fact OrderFact
	var title, packageID sql.NullString
	err := row.Scan(&fact.ID, &fact.CustomerID, &fact.Status, &title, &packageID)
	if errors.Is(err, store.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if title.Valid {
		fact.Title = &title.String
	}
	if packageID.Valid {
		var name sql.NullString
		if err := tx.QueryRow(ctx, "packages", "name", "id = $2", packageID.String).Scan(&name); err != nil && !errors.Is(err, store.ErrNoRows) {
			return nil, err
		} else if name.Valid {
			fact.PackageName = &name.String
		}
	}
	return &fact, nil
}

func loadShootSlotFact(ctx context.Context, tx store.TxAccountScope, orderID string, forUpdate bool) (*SlotFact, error) {
	var row store.Row
	if forUpdate {
		row = tx.QueryRowForUpdate(ctx, "schedule_slots", "id, order_id, type, start_at, end_at", "type = 'shoot' AND order_id = $2", orderID)
	} else {
		row = tx.QueryRow(ctx, "schedule_slots", "id, order_id, type, start_at, end_at", "type = 'shoot' AND order_id = $2", orderID)
	}
	var fact SlotFact
	var order sql.NullString
	err := row.Scan(&fact.ID, &order, &fact.Type, &fact.StartAt, &fact.EndAt)
	if errors.Is(err, store.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	fact.OrderID = order.String
	fact.StartAt = fact.StartAt.UTC()
	fact.EndAt = fact.EndAt.UTC()
	fact.Revision = 1
	return &fact, nil
}

func lookupShootSlotID(ctx context.Context, tx store.TxAccountScope, orderID string) (string, error) {
	var id string
	err := tx.QueryRowForUpdate(ctx, "schedule_slots", "id", "type = 'shoot' AND order_id = $2", orderID).Scan(&id)
	if errors.Is(err, store.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func listPlanIDsByCustomers(ctx context.Context, tx store.TxAccountScope, customerIDs []string) ([]string, error) {
	if len(customerIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "plan_crm_connections", "plan_id", "customer_id = ANY($2::text[])", customerIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

func listPlanIDsByOrders(ctx context.Context, tx store.TxAccountScope, orderIDs []string) ([]string, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "plan_crm_connections", "plan_id", "order_id = ANY($2::text[])", orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

func listOrderIDsByCustomers(ctx context.Context, tx store.TxAccountScope, customerIDs []string) ([]string, error) {
	if len(customerIDs) == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, "orders", "id", "customer_id = ANY($2::text[])", customerIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIDs(rows)
}

func scanIDs(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]string, error) {
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func lockPlanID(ctx context.Context, tx store.TxAccountScope, planID string) error {
	return lockIDs(ctx, tx, "shoot_plans", []string{planID})
}

func lockIDs(ctx context.Context, tx store.TxAccountScope, table string, ids []string) error {
	ids = uniqueSorted(ids...)
	for _, id := range ids {
		if id == "" {
			continue
		}
		var found string
		err := tx.QueryRowForUpdate(ctx, table, "id", "id = $2", id).Scan(&found)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock %s %s: %w", table, id, err)
		}
	}
	return nil
}

func uniqueSorted(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func reminderCommand(kind CommandKind) bool {
	switch kind {
	case KindLinkCustomer, KindLinkOrder, KindUnlinkOrder, KindUnlinkCustomer, KindOrderCancelled, KindOrderDeleted, KindSchedule:
		return true
	default:
		return false
	}
}

func normalizeManual(kind CommandKind) CommandKind {
	if kind == KindSetManual || kind == KindClearWindow {
		return kind
	}
	return kind
}

func LoadDetailView(ctx context.Context, tx store.TxAccountScope, planID string) (*DetailView, error) {
	conn, err := LoadConnection(ctx, tx, planID, false)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	proj, err := LoadProjection(ctx, tx, planID, false)
	if err != nil {
		return nil, err
	}
	return &DetailView{Connection: conn, Projection: proj}, nil
}

type DetailView struct {
	Connection Connection
	Projection *Projection
}

type Summary struct {
	PlanCount       int64
	ActivePlanCount int64
	PrimaryPlanID   string
	PrimaryTitle    string
	PrimaryStatus   string
	ShotCount       int64
	UncheckedCount  int64
	WindowStartsAt  *time.Time
	WindowEndsAt    *time.Time
	WindowTimezone  *string
	WindowSource    *string
	LinkWarning     *string
}

const SummaryViewColumns = summaryColumns
