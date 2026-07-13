package customer

import (
	"context"
	"errors"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type AvatarGCItem struct {
	CustomerID string
	Ref        ObjectRef
	Attempts   int
}

type AvatarReconciliationCheckpoint struct {
	ObjectCursor  string
	ObjectCycle   int64
	PointerCursor string
	PointerCycle  int64
}

func (PostgresAvatarRepository) LoadCheckpoint(
	ctx context.Context,
	scope store.AccountScope,
) (AvatarReconciliationCheckpoint, error) {
	var checkpoint AvatarReconciliationCheckpoint
	err := scope.QueryRow(ctx, "avatar_reconciliation_checkpoint",
		"object_inventory_cursor, object_inventory_cycle, pointer_customer_id_cursor, pointer_cycle", "").Scan(
		&checkpoint.ObjectCursor, &checkpoint.ObjectCycle, &checkpoint.PointerCursor, &checkpoint.PointerCycle,
	)
	if errors.Is(err, store.ErrNoRows) {
		return AvatarReconciliationCheckpoint{}, nil
	}
	return checkpoint, err
}

func (PostgresAvatarRepository) SaveCheckpoint(
	ctx context.Context,
	scope store.TxAccountScope,
	checkpoint AvatarReconciliationCheckpoint,
	now time.Time,
) error {
	var accountID string
	err := scope.InsertOnConflictDoNothingReturning(ctx, "avatar_reconciliation_checkpoint",
		[]string{"updated_at"}, []string{"account_id"}, []string{"account_id"}, now).Scan(&accountID)
	if err != nil && !errors.Is(err, store.ErrNoRows) {
		return err
	}
	_, err = scope.Update(ctx, "avatar_reconciliation_checkpoint", `
		object_inventory_cursor = $2,
		object_inventory_cycle = $3,
		pointer_customer_id_cursor = $4,
		pointer_cycle = $5,
		updated_at = $6`, "",
		checkpoint.ObjectCursor, checkpoint.ObjectCycle,
		checkpoint.PointerCursor, checkpoint.PointerCycle, now,
	)
	return err
}

func (PostgresAvatarRepository) ListCurrentPointers(
	ctx context.Context,
	scope store.AccountScope,
	cursor string,
	limit int,
) ([]Customer, error) {
	rows, err := scope.QueryPage(ctx, "customers", customerColumns,
		"avatar_version IS NOT NULL AND ($2 = '' OR id > $2)",
		[]store.OrderBy{{Column: "id"}}, limit, 0, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Customer, 0, limit)
	for rows.Next() {
		customer, err := scanCustomer(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, customer)
	}
	return result, rows.Err()
}

func (PostgresAvatarRepository) ListDueGC(
	ctx context.Context,
	scope store.AccountScope,
	now time.Time,
	limit int,
) ([]AvatarGCItem, error) {
	rows, err := scope.QueryPage(ctx, "avatar_object_gc",
		"customer_id, avatar_version, avatar_object_id, attempts",
		"not_before <= $2 AND next_attempt_at <= $2",
		[]store.OrderBy{{Column: "next_attempt_at"}, {Column: "customer_id"}, {Column: "avatar_object_id"}},
		limit, 0, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AvatarGCItem, 0, limit)
	for rows.Next() {
		var item AvatarGCItem
		if err := rows.Scan(&item.CustomerID, &item.Ref.AvatarVersion, &item.Ref.AvatarObjectID, &item.Attempts); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (PostgresAvatarRepository) LoadGCForUpdate(
	ctx context.Context,
	scope store.TxAccountScope,
	item AvatarGCItem,
) (AvatarGCItem, error) {
	var found AvatarGCItem
	err := scope.QueryRowForUpdate(ctx, "avatar_object_gc",
		"customer_id, avatar_version, avatar_object_id, attempts",
		"customer_id = $2 AND avatar_object_id = $3", item.CustomerID, item.Ref.AvatarObjectID).Scan(
		&found.CustomerID, &found.Ref.AvatarVersion, &found.Ref.AvatarObjectID, &found.Attempts,
	)
	return found, err
}

func (PostgresAvatarRepository) DeleteGC(
	ctx context.Context,
	scope store.TxAccountScope,
	item AvatarGCItem,
) error {
	_, err := scope.Delete(ctx, "avatar_object_gc", "customer_id = $2 AND avatar_object_id = $3",
		item.CustomerID, item.Ref.AvatarObjectID)
	return err
}

func (PostgresAvatarRepository) RecordGCFailure(
	ctx context.Context,
	scope store.TxAccountScope,
	item AvatarGCItem,
	now time.Time,
) error {
	delay := time.Minute << min(item.Attempts, 6)
	_, err := scope.Update(ctx, "avatar_object_gc", `
		attempts = attempts + 1,
		next_attempt_at = $2,
		last_error = $3,
		updated_at = $4`,
		"customer_id = $5 AND avatar_object_id = $6",
		now.Add(delay), "storage_delete_failed", now, item.CustomerID, item.Ref.AvatarObjectID,
	)
	return err
}
