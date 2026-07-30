package customer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type AvatarRepository interface {
	Snapshot(context.Context, store.AccountScope, string) (Customer, error)
	Lock(context.Context, store.TxAccountScope, string) (Customer, error)
	GCExistsForUpdate(context.Context, store.TxAccountScope, string, string) (bool, error)
	SwitchPointer(context.Context, store.TxAccountScope, string, int64, ObjectRef, ObjectMeta) (Customer, error)
	ClearPointer(context.Context, store.TxAccountScope, string, int64) (Customer, error)
	EnqueueGC(context.Context, store.TxAccountScope, string, ObjectRef, time.Time) error
}

type PostgresAvatarRepository struct{}

func NewPostgresAvatarRepository() PostgresAvatarRepository { return PostgresAvatarRepository{} }

func (PostgresAvatarRepository) Snapshot(ctx context.Context, scope store.AccountScope, id string) (Customer, error) {
	return findCustomer(ctx, scope, id)
}

func (PostgresAvatarRepository) Lock(ctx context.Context, scope store.TxAccountScope, id string) (Customer, error) {
	customer, err := scanCustomer(scope.QueryRowForUpdate(ctx, "customers", customerColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Customer{}, fmt.Errorf("%w: 客户不存在", ErrNotFound)
	}
	return customer, err
}

func (PostgresAvatarRepository) GCExistsForUpdate(
	ctx context.Context,
	scope store.TxAccountScope,
	customerID, objectID string,
) (bool, error) {
	var found string
	err := scope.QueryRowForUpdate(ctx, "avatar_object_gc", "avatar_object_id",
		"customer_id = $2 AND avatar_object_id = $3", customerID, objectID).Scan(&found)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (PostgresAvatarRepository) SwitchPointer(
	ctx context.Context,
	scope store.TxAccountScope,
	customerID string,
	expectedRevision int64,
	ref ObjectRef,
	meta ObjectMeta,
) (Customer, error) {
	rows, err := scope.Update(ctx, "customers", `
		avatar_revision = avatar_revision + 1,
		avatar_version = $2,
		avatar_object_id = $3,
		avatar_media_type = $4,
		avatar_size = $5,
		avatar_updated_at = $6`,
		"id = $7 AND avatar_revision = $8",
		ref.AvatarVersion, ref.AvatarObjectID, meta.MediaType, meta.Size, meta.ModifiedAt,
		customerID, expectedRevision,
	)
	if err != nil {
		return Customer{}, err
	}
	if rows != 1 {
		return Customer{}, ErrAvatarRevisionConflict
	}
	return scanCustomer(scope.QueryRow(ctx, "customers", customerColumns, "id = $2", customerID))
}

func (PostgresAvatarRepository) ClearPointer(
	ctx context.Context,
	scope store.TxAccountScope,
	customerID string,
	expectedRevision int64,
) (Customer, error) {
	rows, err := scope.Update(ctx, "customers", `
		avatar_revision = avatar_revision + 1,
		avatar_version = NULL,
		avatar_object_id = NULL,
		avatar_media_type = NULL,
		avatar_size = NULL,
		avatar_updated_at = NULL`,
		"id = $2 AND avatar_revision = $3", customerID, expectedRevision)
	if err != nil {
		return Customer{}, err
	}
	if rows != 1 {
		return Customer{}, ErrAvatarRevisionConflict
	}
	return scanCustomer(scope.QueryRow(ctx, "customers", customerColumns, "id = $2", customerID))
}

func (PostgresAvatarRepository) EnqueueGC(
	ctx context.Context,
	scope store.TxAccountScope,
	customerID string,
	ref ObjectRef,
	notBefore time.Time,
) error {
	var objectID string
	err := scope.InsertOnConflictDoNothingReturning(ctx, "avatar_object_gc",
		[]string{"customer_id", "avatar_version", "avatar_object_id", "not_before", "next_attempt_at"},
		[]string{"account_id", "customer_id", "avatar_object_id"},
		[]string{"avatar_object_id"},
		customerID, ref.AvatarVersion, ref.AvatarObjectID, notBefore, notBefore,
	).Scan(&objectID)
	if errors.Is(err, store.ErrNoRows) {
		return nil
	}
	return err
}
