package planshare

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/shootplanning/crm"
)

var fullEligibleOrderStatuses = []string{
	"scheduled", "shot", "selected", "retouching", "delivered", "closed",
}

type fullEligibilityHint struct {
	PlanID      string
	CustomerID  string
	OrderID     string
	SlotID      string
	LinkEpochID string
	OrderStatus string
	ConnState   crm.ConnectionState
	ConnRev     int64
}

type fullEligibility struct {
	LinkEpochID string
	Eligible    bool
	CustomerID  string
	OrderID     string
	SlotID      string
}

func preReadFullEligibility(
	ctx context.Context,
	tx store.TxAccountScope,
	planID string,
) (fullEligibilityHint, error) {
	conn, err := crm.LoadConnection(ctx, tx, planID, false)
	if err != nil {
		if errors.Is(err, crm.ErrNotFound) {
			return fullEligibilityHint{}, ErrFullViewNotEligible
		}
		return fullEligibilityHint{}, err
	}
	if conn.State != crm.StateOrderLinked || conn.CustomerID == nil || conn.OrderID == nil || conn.LinkEpochID == nil {
		return fullEligibilityHint{}, ErrFullViewNotEligible
	}
	var orderStatus string
	err = tx.QueryRow(ctx, "orders", "status", "id = $2", *conn.OrderID).Scan(&orderStatus)
	if errors.Is(err, store.ErrNoRows) {
		return fullEligibilityHint{}, ErrFullViewNotEligible
	}
	if err != nil {
		return fullEligibilityHint{}, err
	}
	hint := fullEligibilityHint{
		PlanID:      planID,
		CustomerID:  *conn.CustomerID,
		OrderID:     *conn.OrderID,
		LinkEpochID: *conn.LinkEpochID,
		OrderStatus: orderStatus,
		ConnState:   conn.State,
		ConnRev:     conn.ConnectionRevision,
	}
	var slotID string
	err = tx.QueryRow(ctx, "schedule_slots", "id", "type = 'shoot' AND order_id = $2", *conn.OrderID).Scan(&slotID)
	if err == nil {
		hint.SlotID = slotID
	} else if !errors.Is(err, store.ErrNoRows) {
		return fullEligibilityHint{}, err
	}
	return hint, nil
}

func lockCRMOrderGraph(ctx context.Context, tx store.TxAccountScope, hint fullEligibilityHint) error {
	customers := uniqueSorted(hint.CustomerID)
	orders := uniqueSorted(hint.OrderID)
	slots := uniqueSorted(hint.SlotID)
	if err := lockIDs(ctx, tx, "customers", customers); err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "orders", orders); err != nil {
		return err
	}
	if err := lockIDs(ctx, tx, "schedule_slots", slots); err != nil {
		return err
	}
	return lockIDs(ctx, tx, "shoot_plans", []string{hint.PlanID})
}

func recheckFullEligibility(
	ctx context.Context,
	tx store.TxAccountScope,
	hint fullEligibilityHint,
) (fullEligibility, error) {
	conn, err := crm.LoadConnection(ctx, tx, hint.PlanID, true)
	if err != nil {
		if errors.Is(err, crm.ErrNotFound) {
			return fullEligibility{}, ErrFullViewNotEligible
		}
		return fullEligibility{}, err
	}
	if conn.ConnectionRevision != hint.ConnRev ||
		conn.State != crm.StateOrderLinked ||
		conn.CustomerID == nil || conn.OrderID == nil || conn.LinkEpochID == nil ||
		*conn.CustomerID != hint.CustomerID ||
		*conn.OrderID != hint.OrderID ||
		*conn.LinkEpochID != hint.LinkEpochID {
		return fullEligibility{}, ErrSourceChangedRetry
	}
	var orderCustomerID, orderStatus string
	err = tx.QueryRow(ctx, "orders", "customer_id, status", "id = $2", hint.OrderID).
		Scan(&orderCustomerID, &orderStatus)
	if errors.Is(err, store.ErrNoRows) {
		return fullEligibility{}, ErrFullViewNotEligible
	}
	if err != nil {
		return fullEligibility{}, err
	}
	if orderCustomerID != hint.CustomerID {
		return fullEligibility{}, ErrSourceChangedRetry
	}
	if hint.SlotID != "" {
		var slotOrderID string
		err = tx.QueryRow(ctx, "schedule_slots", "order_id", "id = $2", hint.SlotID).Scan(&slotOrderID)
		if errors.Is(err, store.ErrNoRows) {
			return fullEligibility{}, ErrSourceChangedRetry
		}
		if err != nil {
			return fullEligibility{}, err
		}
		if slotOrderID != hint.OrderID {
			return fullEligibility{}, ErrSourceChangedRetry
		}
	}
	eligible := slices.Contains(fullEligibleOrderStatuses, orderStatus)
	if !eligible {
		return fullEligibility{}, ErrFullViewNotEligible
	}
	return fullEligibility{
		LinkEpochID: *conn.LinkEpochID,
		Eligible:    true,
		CustomerID:  *conn.CustomerID,
		OrderID:     *conn.OrderID,
		SlotID:      hint.SlotID,
	}, nil
}

func lockIDs(ctx context.Context, tx store.TxAccountScope, table string, ids []string) error {
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
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
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
