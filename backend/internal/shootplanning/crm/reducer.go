package crm

import (
	"fmt"
	"time"
)

type ReduceInput struct {
	Connection Connection
	Projection *Projection
	Window     *Window
	Plan       PlanFact
	Command    Command
	Customer   *CustomerFact
	Order      *OrderFact
	Slot       *SlotFact
	Timezone   string
	Now        time.Time
	NewEpochID string
}

func Reduce(in ReduceInput) (Result, error) {
	if in.Timezone == "" {
		in.Timezone = "Asia/Shanghai"
	}
	if in.Now.IsZero() {
		in.Now = time.Now().UTC()
	}
	nextConn := cloneConnection(in.Connection)
	nextProj := cloneProjection(in.Projection)
	nextWindow := cloneWindow(in.Window)
	out := Result{Connection: nextConn, Projection: nextProj, Window: nextWindow}

	if err := guardExplicitLifecycle(in); err != nil {
		return Result{}, err
	}

	switch in.Command.Kind {
	case KindLinkCustomer:
		return reduceLinkCustomer(in, out)
	case KindLinkOrder:
		return reduceLinkOrder(in, out)
	case KindUnlinkOrder:
		return reduceUnlinkOrder(in, out)
	case KindUnlinkCustomer:
		return reduceUnlinkCustomer(in, out)
	case KindAdopt:
		return reduceAdopt(in, out)
	case KindSetManual:
		return reduceSetManual(in, out)
	case KindClearWindow:
		return reduceClearWindow(in, out)
	case KindOrderCancelled:
		return reduceOrderCancelled(in, out)
	case KindOrderDeleted:
		return reduceOrderDeleted(in, out)
	case KindSchedule:
		return reduceSchedule(in, out)
	case KindCustomerMerged:
		return reduceCustomerMerged(in, out)
	default:
		return Result{}, ValidationError{Message: "unknown crm command"}
	}
}

func guardExplicitLifecycle(in ReduceInput) error {
	switch in.Command.Kind {
	case KindLinkCustomer, KindLinkOrder, KindUnlinkOrder, KindUnlinkCustomer, KindAdopt, KindSetManual, KindClearWindow:
		switch in.Plan.Status {
		case "completed":
			return ErrReopenRequired
		case "archived":
			return ErrArchivedReadOnly
		}
	}
	return nil
}

func reduceLinkCustomer(in ReduceInput, out Result) (Result, error) {
	if in.Command.CustomerID == nil || *in.Command.CustomerID == "" {
		return Result{}, ValidationError{Message: "customer_id 必填"}
	}
	if in.Customer == nil {
		return Result{}, ErrNotFound
	}
	if in.Customer.Status == "merged" {
		return Result{}, ErrCustomerMerged
	}
	target := *in.Command.CustomerID
	if out.Connection.CustomerID != nil && *out.Connection.CustomerID != target {
		return Result{}, ErrCustomerLinkConflict
	}
	if out.Connection.CustomerID != nil && *out.Connection.CustomerID == target {
		return finalize(in, out)
	}
	out.Connection.CustomerID = stringPtr(target)
	out.Connection.State = StateCustomerLinked
	out.ReminderLinkChange = linkChangePtr(PlanLinkChangeLink)
	out.Events = append(out.Events, Event{
		Kind:     EventCustomerLinked,
		FromRefs: refsOf(in.Connection),
		ToRefs:   refsOf(out.Connection),
	})
	return finalize(in, out)
}

func reduceLinkOrder(in ReduceInput, out Result) (Result, error) {
	if in.Command.OrderID == nil || in.Order == nil {
		return Result{}, ErrNotFound
	}
	if in.Customer != nil && in.Customer.Status == "merged" {
		return Result{}, ErrCustomerMerged
	}
	if in.Customer != nil && in.Customer.ID != in.Order.CustomerID {
		return Result{}, ErrCustomerLinkConflict
	}
	if out.Connection.CustomerID != nil && *out.Connection.CustomerID != in.Order.CustomerID {
		return Result{}, ErrCustomerLinkConflict
	}
	if out.Connection.OrderID != nil && *out.Connection.OrderID != in.Order.ID {
		return Result{}, ErrOrderLinkConflict
	}
	if out.Connection.OrderID != nil && *out.Connection.OrderID == in.Order.ID {
		return finalize(in, out)
	}
	change := PlanLinkChangeLink
	if out.Connection.State == StateOrderDeleted || out.Connection.LinkEpochID != nil {
		change = PlanLinkChangeRelink
	}
	epoch := in.NewEpochID
	if epoch == "" {
		return Result{}, ValidationError{Message: "link epoch 必填"}
	}
	snap := LinkedOrderSnapshot{
		OrderID:      in.Order.ID,
		CustomerID:   in.Order.CustomerID,
		Title:        in.Order.Title,
		PackageName:  in.Order.PackageName,
		StatusAtLink: in.Order.Status,
		LinkedAt:     in.Now.UTC(),
	}
	out.Connection.CustomerID = stringPtr(in.Order.CustomerID)
	out.Connection.OrderID = stringPtr(in.Order.ID)
	out.Connection.LinkEpochID = stringPtr(epoch)
	out.Connection.LinkedOrderSnapshot = &snap
	if in.Order.Status == "cancelled" {
		out.Connection.State = StateOrderCancelled
	} else {
		out.Connection.State = StateOrderLinked
	}
	out.ReminderLinkChange = &change
	createdProjection := out.Projection == nil || !out.Projection.Exists
	if createdProjection {
		out.Projection = &Projection{PlanID: in.Plan.ID, Exists: true, RuleVersion: RuleVersion, ProjectionRevision: 0}
	}
	out.Events = append(out.Events, Event{
		Kind:              EventOrderLinked,
		FromRefs:          refsOf(in.Connection),
		ToRefs:            refsOf(out.Connection),
		LinkEpochSnapshot: &snap,
	})
	return reprojectLinkedOrder(in, out, createdProjection)
}

func reduceUnlinkOrder(in ReduceInput, out Result) (Result, error) {
	if out.Connection.OrderID == nil && out.Connection.State != StateOrderDeleted {
		return finalize(in, out)
	}
	out.Connection.OrderID = nil
	out.Connection.LinkEpochID = nil
	out.Connection.LinkedOrderSnapshot = nil
	if out.Connection.CustomerID != nil {
		out.Connection.State = StateCustomerLinked
	} else {
		out.Connection.State = StateIndependent
	}
	out.ReminderLinkChange = linkChangePtr(PlanLinkChangeUnlink)
	out.Events = append(out.Events, Event{
		Kind:              EventOrderUnlinked,
		FromRefs:          refsOf(in.Connection),
		ToRefs:            refsOf(out.Connection),
		LinkEpochSnapshot: in.Connection.LinkedOrderSnapshot,
	})
	return unlinkedProjection(in, out, StatusInactiveUnlinked)
}

func reduceUnlinkCustomer(in ReduceInput, out Result) (Result, error) {
	if out.Connection.OrderID != nil {
		return Result{}, ErrOrderStillLinked
	}
	if out.Connection.CustomerID == nil && out.Connection.State == StateIndependent {
		return finalize(in, out)
	}
	out.Events = append(out.Events, Event{
		Kind:              EventCustomerUnlinked,
		FromRefs:          refsOf(in.Connection),
		ToRefs:            map[string]any{"state": string(StateIndependent)},
		LinkEpochSnapshot: in.Connection.LinkedOrderSnapshot,
	})
	out.Connection.CustomerID = nil
	out.Connection.OrderID = nil
	out.Connection.LinkEpochID = nil
	out.Connection.LinkedOrderSnapshot = nil
	out.Connection.State = StateIndependent
	out.ReminderLinkChange = linkChangePtr(PlanLinkChangeUnlink)
	return unlinkedProjection(in, out, StatusInactiveUnlinked)
}

func reduceAdopt(in ReduceInput, out Result) (Result, error) {
	if out.Projection == nil || !out.Projection.Exists {
		return Result{}, ErrProjectionMissing
	}
	if in.Command.ProjectionRevision == nil || *in.Command.ProjectionRevision != out.Projection.ProjectionRevision {
		return Result{}, ErrProjectionRevisionConflict
	}
	switch out.Projection.Status {
	case StatusActiveApplied, StatusActiveManualOverride, StatusActiveUnapplied:
	default:
		return Result{}, ErrProjectionNotActive
	}
	if out.Projection.EndsAt == nil || !out.Projection.EndsAt.After(in.Now) {
		return Result{}, ErrProjectionNotFuture
	}
	out.Projection.ApplySuppressed = false
	out.Projection.Status = StatusActiveApplied
	out.Window = scheduleWindow(*out.Projection, in.Timezone, nextWindowRevision(in.Window))
	out.Events = append(out.Events, Event{
		Kind:              EventScheduleAdopted,
		FromRefs:          refsOf(in.Connection),
		ToRefs:            refsOf(out.Connection),
		SourceFingerprint: out.Projection.SlotSourceFingerprint,
	})
	return finalize(in, out)
}

func reduceSetManual(in ReduceInput, out Result) (Result, error) {
	if in.Command.Manual == nil {
		return Result{}, ValidationError{Message: "manual window 必填"}
	}
	manual := *in.Command.Manual
	manual.Source = "manual"
	manual.SourceRef = nil
	manual.RuleVersion = RuleVersion
	manual.Revision = nextWindowRevision(in.Window)
	out.Window = &manual
	if out.Projection != nil && out.Projection.Exists && isActiveShadow(out.Projection.Status, in.Now, out.Projection.EndsAt) {
		out.Projection.Status = StatusActiveManualOverride
	}
	out.Events = append(out.Events, Event{
		Kind:     EventManualOverrode,
		FromRefs: refsOf(in.Connection),
		ToRefs:   refsOf(out.Connection),
	})
	return finalize(in, out)
}

func reduceClearWindow(in ReduceInput, out Result) (Result, error) {
	if in.Window == nil {
		return finalize(in, out)
	}
	out.Window = nil
	out.ClearWindow = true
	if out.Projection != nil && out.Projection.Exists {
		if isActiveShadow(out.Projection.Status, in.Now, out.Projection.EndsAt) {
			out.Projection.ApplySuppressed = true
			out.Projection.Status = StatusActiveUnapplied
		}
	}
	out.Events = append(out.Events, Event{
		Kind:     EventWindowSuppressed,
		FromRefs: refsOf(in.Connection),
		ToRefs:   refsOf(out.Connection),
	})
	return finalize(in, out)
}

func reduceOrderCancelled(in ReduceInput, out Result) (Result, error) {
	if out.Connection.OrderID == nil || in.Order == nil || *out.Connection.OrderID != in.Order.ID {
		return finalize(in, out)
	}
	if out.Connection.State == StateOrderCancelled {
		return finalize(in, out)
	}
	out.Connection.State = StateOrderCancelled
	out.ReminderOrderChange = lifecyclePtr(OrderLifecycleCancelled)
	out.Events = append(out.Events, Event{
		Kind:              EventOrderCancelled,
		FromRefs:          refsOf(in.Connection),
		ToRefs:            refsOf(out.Connection),
		LinkEpochSnapshot: out.Connection.LinkedOrderSnapshot,
	})
	return deactivateProjection(in, out, StatusInactiveOrderCancelled, true)
}

func reduceOrderDeleted(in ReduceInput, out Result) (Result, error) {
	if out.Connection.OrderID == nil && out.Connection.State != StateOrderLinked && out.Connection.State != StateOrderCancelled {
		return finalize(in, out)
	}
	out.Connection.OrderID = nil
	out.Connection.State = StateOrderDeleted
	out.ReminderOrderChange = lifecyclePtr(OrderLifecycleDeleted)
	out.Events = append(out.Events, Event{
		Kind:              EventOrderDeleted,
		FromRefs:          refsOf(in.Connection),
		ToRefs:            refsOf(out.Connection),
		LinkEpochSnapshot: in.Connection.LinkedOrderSnapshot,
	})
	return deactivateProjection(in, out, StatusInactiveOrderDeleted, false)
}

func reduceSchedule(in ReduceInput, out Result) (Result, error) {
	if out.Connection.State != StateOrderLinked && out.Connection.State != StateOrderCancelled {
		return finalize(in, out)
	}
	if out.Connection.State == StateOrderCancelled {
		return deactivateProjection(in, out, StatusInactiveOrderCancelled, true)
	}
	return reprojectLinkedOrder(in, out, false)
}

func reduceCustomerMerged(in ReduceInput, out Result) (Result, error) {
	if in.Command.MergeTargetCustomerID == nil {
		return Result{}, ValidationError{Message: "merge target 必填"}
	}
	target := *in.Command.MergeTargetCustomerID
	if out.Connection.CustomerID == nil || *out.Connection.CustomerID == target {
		return finalize(in, out)
	}
	out.Connection.CustomerID = stringPtr(target)
	out.Events = append(out.Events, Event{
		Kind:     EventCustomerMerged,
		FromRefs: refsOf(in.Connection),
		ToRefs:   refsOf(out.Connection),
	})
	return finalize(in, out)
}

func reprojectLinkedOrder(in ReduceInput, out Result, initializing bool) (Result, error) {
	if out.Projection == nil {
		out.Projection = &Projection{PlanID: in.Plan.ID, Exists: true, RuleVersion: RuleVersion, ProjectionRevision: 0}
		initializing = true
	}
	out.Projection.Exists = true
	if out.Connection.OrderID != nil {
		out.Projection.OrderID = stringPtr(*out.Connection.OrderID)
	}
	if out.Connection.State == StateOrderCancelled {
		return deactivateProjection(in, out, StatusInactiveOrderCancelled, !initializing)
	}
	if in.Slot == nil || in.Slot.Type != "shoot" || out.Connection.OrderID == nil || in.Slot.OrderID != *out.Connection.OrderID {
		out.Projection.SlotID = nil
		out.Projection.StartsAt = nil
		out.Projection.EndsAt = nil
		out.Projection.Timezone = nil
		out.Projection.SlotSourceFingerprint = fingerprint(out.Connection.OrderID, nil, nil, nil, "", 0, orderStatus(in.Order), in.Timezone, out.Projection.ApplySuppressed, StatusMissingSlot)
		out.Projection.Status = StatusMissingSlot
		if in.Window != nil && in.Window.Source == "schedule_slot" && activeLifecycle(in.Plan.Status) {
			out.Window = nil
			out.ClearWindow = true
		}
		if initializing {
			out.Events = append(out.Events, Event{Kind: EventScheduleProjected, FromRefs: refsOf(in.Connection), ToRefs: refsOf(out.Connection)})
		}
		return finalize(in, out)
	}
	slot := *in.Slot
	tz := in.Timezone
	out.Projection.SlotID = stringPtr(slot.ID)
	start, end := slot.StartAt.UTC(), slot.EndAt.UTC()
	out.Projection.StartsAt = &start
	out.Projection.EndsAt = &end
	out.Projection.Timezone = stringPtr(tz)
	future := end.After(in.Now)
	manual := in.Window != nil && in.Window.Source == "manual"
	status := StatusActiveApplied
	switch {
	case !future:
		status = StatusInactivePast
	case out.Projection.ApplySuppressed:
		status = StatusActiveUnapplied
	case manual:
		status = StatusActiveManualOverride
	}
	fp := fingerprint(out.Connection.OrderID, &slot.ID, &start, &end, slot.Type, slot.Revision, orderStatus(in.Order), tz, out.Projection.ApplySuppressed, status)
	out.Projection.SlotSourceFingerprint = fp
	out.Projection.Status = status
	if status == StatusActiveApplied && activeLifecycle(in.Plan.Status) {
		out.Window = scheduleWindow(*out.Projection, tz, nextWindowRevision(in.Window))
		out.Events = append(out.Events, Event{Kind: EventScheduleProjected, FromRefs: refsOf(in.Connection), ToRefs: refsOf(out.Connection), SourceFingerprint: fp})
	} else if shouldClearScheduleWindow(in.Window, &slot, status) && activeLifecycle(in.Plan.Status) {
		out.Window = nil
		out.ClearWindow = true
		out.Events = append(out.Events, Event{Kind: EventScheduleCleared, FromRefs: refsOf(in.Connection), ToRefs: refsOf(out.Connection), SourceFingerprint: fp})
	}
	out.ReminderSchedule = scheduleChangePtr(in.Command.ScheduleChange)
	return finalize(in, out)
}

func unlinkedProjection(in ReduceInput, out Result, status ProjectionStatus) (Result, error) {
	if out.Projection == nil || !out.Projection.Exists {
		if in.Window != nil && in.Window.Source == "schedule_slot" && activeLifecycle(in.Plan.Status) {
			out.Window = nil
			out.ClearWindow = true
		}
		return finalize(in, out)
	}
	out.Projection.OrderID = nil
	out.Projection.SlotID = nil
	out.Projection.StartsAt = nil
	out.Projection.EndsAt = nil
	out.Projection.Timezone = nil
	out.Projection.ApplySuppressed = false
	out.Projection.Status = status
	out.Projection.SlotSourceFingerprint = fingerprint(nil, nil, nil, nil, "", 0, "", in.Timezone, false, status)
	if in.Window != nil && in.Window.Source == "schedule_slot" && activeLifecycle(in.Plan.Status) {
		out.Window = nil
		out.ClearWindow = true
	}
	out.Events = append(out.Events, Event{Kind: EventScheduleCleared, FromRefs: refsOf(in.Connection), ToRefs: refsOf(out.Connection)})
	return finalize(in, out)
}

func deactivateProjection(in ReduceInput, out Result, status ProjectionStatus, keepSlot bool) (Result, error) {
	if out.Projection == nil {
		return finalize(in, out)
	}
	if !keepSlot {
		out.Projection.SlotID = nil
		out.Projection.StartsAt = nil
		out.Projection.EndsAt = nil
		out.Projection.Timezone = nil
		out.Projection.OrderID = nil
	}
	out.Projection.Status = status
	out.Projection.SlotSourceFingerprint = fingerprint(out.Connection.OrderID, out.Projection.SlotID, out.Projection.StartsAt, out.Projection.EndsAt, "", 0, orderStatus(in.Order), in.Timezone, out.Projection.ApplySuppressed, status)
	if in.Window != nil && in.Window.Source == "schedule_slot" && activeLifecycle(in.Plan.Status) {
		out.Window = nil
		out.ClearWindow = true
	}
	return finalize(in, out)
}

func finalize(in ReduceInput, out Result) (Result, error) {
	out.ConnectionChanged = !connectionEqual(in.Connection, out.Connection)
	if out.ConnectionChanged {
		out.Connection.ConnectionRevision = in.Connection.ConnectionRevision + 1
	}
	if out.Projection != nil {
		if in.Projection == nil || !in.Projection.Exists {
			out.ProjectionChanged = out.Projection.Exists
			if out.ProjectionChanged && out.Projection.ProjectionRevision == 0 {
				out.Projection.ProjectionRevision = 1
			}
		} else {
			out.ProjectionChanged = !projectionEqual(in.Projection, out.Projection)
			if out.ProjectionChanged {
				out.Projection.ProjectionRevision = in.Projection.ProjectionRevision + 1
			}
		}
	}
	out.WindowChanged = windowMaterial(in.Window, out.Window, out.ClearWindow)
	if !out.ConnectionChanged && !out.ProjectionChanged && !out.WindowChanged {
		out.Events = nil
		out.ReminderLinkChange = nil
		out.ReminderOrderChange = nil
		out.ReminderSchedule = nil
		out.Connection = cloneConnection(in.Connection)
		out.Projection = cloneProjection(in.Projection)
		out.Window = cloneWindow(in.Window)
		out.ClearWindow = false
		return out, nil
	}
	if !out.WindowChanged {
		out.Window = cloneWindow(in.Window)
		out.ClearWindow = false
	}
	annotated := make([]Event, 0, len(out.Events))
	for _, event := range out.Events {
		if out.ConnectionChanged {
			event.ConnectionRevisionAfter = int64Ptr(out.Connection.ConnectionRevision)
		}
		if out.ProjectionChanged && out.Projection != nil {
			event.ProjectionRevisionAfter = int64Ptr(out.Projection.ProjectionRevision)
		}
		if event.ConnectionRevisionAfter == nil && event.ProjectionRevisionAfter == nil {
			continue
		}
		annotated = append(annotated, event)
	}
	if len(annotated) == 0 && (out.ConnectionChanged || out.ProjectionChanged) {
		event := Event{Kind: EventScheduleProjected, FromRefs: refsOf(in.Connection), ToRefs: refsOf(out.Connection)}
		if out.ConnectionChanged {
			event.ConnectionRevisionAfter = int64Ptr(out.Connection.ConnectionRevision)
		}
		if out.ProjectionChanged && out.Projection != nil {
			event.ProjectionRevisionAfter = int64Ptr(out.Projection.ProjectionRevision)
		}
		annotated = []Event{event}
	}
	if out.WindowChanged && activeLifecycle(in.Plan.Status) {
		planAfter := in.Plan.Revision + 1
		var windowAfter *int64
		if !out.ClearWindow && out.Window != nil {
			windowAfter = int64Ptr(out.Window.Revision)
		}
		for i := range annotated {
			annotated[i].PlanRevisionAfter = int64Ptr(planAfter)
			annotated[i].WindowRevisionAfter = windowAfter
		}
	}
	out.Events = annotated
	return out, nil
}

func scheduleWindow(proj Projection, timezone string, revision int64) *Window {
	if proj.StartsAt == nil || proj.EndsAt == nil || proj.SlotID == nil {
		return nil
	}
	return &Window{
		Source:             "schedule_slot",
		SourceRef:          stringPtr(*proj.SlotID),
		StartsAt:           proj.StartsAt.UTC(),
		EndsAt:             proj.EndsAt.UTC(),
		Timezone:           timezone,
		LiveWindowStartsAt: proj.StartsAt.UTC().Add(-2 * time.Hour),
		LiveWindowEndsAt:   proj.EndsAt.UTC().Add(2 * time.Hour),
		RuleVersion:        RuleVersion,
		Revision:           revision,
	}
}

func shouldClearScheduleWindow(window *Window, slot *SlotFact, status ProjectionStatus) bool {
	if window == nil || window.Source != "schedule_slot" || status == StatusActiveApplied {
		return false
	}
	if slot == nil || window.SourceRef == nil || *window.SourceRef != slot.ID {
		return true
	}
	return false
}

func nextWindowRevision(current *Window) int64 {
	if current == nil {
		return 1
	}
	return current.Revision + 1
}

func fingerprint(orderID, slotID *string, start, end *time.Time, slotType string, slotRev int64, orderStatus, timezone string, suppressed bool, status ProjectionStatus) string {
	return fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s|%s|%d|%t|%s",
		deref(orderID), deref(slotID), timeString(start), timeString(end), slotType, slotRev, orderStatus, timezone, RuleVersion, suppressed, status)
}

func refsOf(conn Connection) map[string]any {
	return map[string]any{
		"state":       string(conn.State),
		"customer_id": deref(conn.CustomerID),
		"order_id":    deref(conn.OrderID),
		"epoch_id":    deref(conn.LinkEpochID),
	}
}

func connectionEqual(left, right Connection) bool {
	return left.State == right.State &&
		deref(left.CustomerID) == deref(right.CustomerID) &&
		deref(left.OrderID) == deref(right.OrderID) &&
		deref(left.LinkEpochID) == deref(right.LinkEpochID) &&
		snapshotKey(left.LinkedOrderSnapshot) == snapshotKey(right.LinkedOrderSnapshot)
}

func projectionEqual(left, right *Projection) bool {
	if left == nil && right == nil {
		return true
	}
	if left == nil || right == nil {
		return false
	}
	return left.Status == right.Status &&
		left.ApplySuppressed == right.ApplySuppressed &&
		left.SlotSourceFingerprint == right.SlotSourceFingerprint &&
		deref(left.OrderID) == deref(right.OrderID) &&
		deref(left.SlotID) == deref(right.SlotID)
}

func windowMaterial(before, after *Window, clear bool) bool {
	if clear && before != nil {
		return true
	}
	if after == nil {
		return false
	}
	if before == nil {
		return true
	}
	return before.Source != after.Source || deref(before.SourceRef) != deref(after.SourceRef) ||
		!before.StartsAt.Equal(after.StartsAt) || !before.EndsAt.Equal(after.EndsAt) ||
		before.Timezone != after.Timezone ||
		!before.LiveWindowStartsAt.Equal(after.LiveWindowStartsAt) ||
		!before.LiveWindowEndsAt.Equal(after.LiveWindowEndsAt)
}

func snapshotKey(snap *LinkedOrderSnapshot) string {
	if snap == nil {
		return ""
	}
	return snap.OrderID + "|" + snap.CustomerID + "|" + snap.StatusAtLink + "|" + snap.LinkedAt.UTC().Format(time.RFC3339Nano)
}

func isActiveShadow(status ProjectionStatus, now time.Time, end *time.Time) bool {
	switch status {
	case StatusActiveApplied, StatusActiveManualOverride, StatusActiveUnapplied:
		return true
	}
	return end != nil && end.After(now)
}

func activeLifecycle(status string) bool {
	return status == "draft" || status == "ready" || status == "in_progress"
}

func orderStatus(order *OrderFact) string {
	if order == nil {
		return ""
	}
	return order.Status
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func linkChangePtr(value PlanLinkChange) *PlanLinkChange            { return &value }
func lifecyclePtr(value OrderLifecycleChange) *OrderLifecycleChange { return &value }
func scheduleChangePtr(value ScheduleChange) *ScheduleChange {
	if value == "" {
		return nil
	}
	return &value
}
