package accountprofile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const profileColumns = `
	display_name, profile_revision, avatar_revision,
	avatar_version, avatar_object_id, avatar_media_type, avatar_size, avatar_updated_at, updated_at`

// Repository 是账号资料持久化端口。
type Repository interface {
	Snapshot(context.Context, store.AccountScope) (Profile, bool, error)
	Lock(context.Context, store.TxAccountScope) (Profile, bool, error)
	InsertDisplayName(context.Context, store.TxAccountScope, *string, time.Time) (Profile, error)
	UpdateDisplayName(context.Context, store.TxAccountScope, int64, *string, time.Time) (Profile, error)
	InsertAvatarPointer(context.Context, store.TxAccountScope, ObjectRef, ObjectMeta) (Profile, error)
	SwitchAvatarPointer(context.Context, store.TxAccountScope, int64, ObjectRef, ObjectMeta) (Profile, error)
	ClearAvatarPointer(context.Context, store.TxAccountScope, int64, time.Time) (Profile, error)
	GCExistsForUpdate(context.Context, store.TxAccountScope, string) (bool, error)
	EnqueueGC(context.Context, store.TxAccountScope, ObjectRef, time.Time) error
	ListDueGC(context.Context, store.AccountScope, time.Time, int) ([]GCItem, error)
	LoadGCForUpdate(context.Context, store.TxAccountScope, GCItem) (GCItem, error)
	DeleteGC(context.Context, store.TxAccountScope, GCItem) error
	RecordGCFailure(context.Context, store.TxAccountScope, GCItem, time.Time) error
	ListCurrentPointers(context.Context, store.AccountScope, string, int) ([]Profile, error)
}

// PostgresRepository 是 PostgreSQL 实现。
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository { return PostgresRepository{} }

func (PostgresRepository) Snapshot(ctx context.Context, scope store.AccountScope) (Profile, bool, error) {
	profile, err := scanProfile(scope.QueryRow(ctx, "account_profiles", profileColumns, ""))
	if errors.Is(err, store.ErrNoRows) {
		return VirtualDefault(), false, nil
	}
	return profile, err == nil, err
}

func (PostgresRepository) Lock(ctx context.Context, scope store.TxAccountScope) (Profile, bool, error) {
	profile, err := scanProfile(scope.QueryRowForUpdate(ctx, "account_profiles", profileColumns, ""))
	if errors.Is(err, store.ErrNoRows) {
		return VirtualDefault(), false, nil
	}
	return profile, err == nil, err
}

func (PostgresRepository) InsertDisplayName(
	ctx context.Context,
	scope store.TxAccountScope,
	displayName *string,
	now time.Time,
) (Profile, error) {
	var accountID string
	err := scope.InsertOnConflictDoNothingReturning(ctx, "account_profiles",
		[]string{"display_name", "profile_revision", "updated_at"},
		[]string{"account_id"},
		[]string{"account_id"},
		displayName, int64(1), now,
	).Scan(&accountID)
	if errors.Is(err, store.ErrNoRows) {
		return Profile{}, store.ErrNoRows
	}
	if err != nil {
		return Profile{}, err
	}
	return scanProfile(scope.QueryRow(ctx, "account_profiles", profileColumns, ""))
}

func (PostgresRepository) UpdateDisplayName(
	ctx context.Context,
	scope store.TxAccountScope,
	expected int64,
	displayName *string,
	now time.Time,
) (Profile, error) {
	rows, err := scope.Update(ctx, "account_profiles",
		`display_name = $2, profile_revision = profile_revision + 1, updated_at = $3`,
		`profile_revision = $4`,
		displayName, now, expected,
	)
	if err != nil {
		return Profile{}, err
	}
	if rows != 1 {
		return Profile{}, ErrProfileRevisionConflict
	}
	return scanProfile(scope.QueryRow(ctx, "account_profiles", profileColumns, ""))
}

func (PostgresRepository) InsertAvatarPointer(
	ctx context.Context,
	scope store.TxAccountScope,
	ref ObjectRef,
	meta ObjectMeta,
) (Profile, error) {
	var accountID string
	err := scope.InsertOnConflictDoNothingReturning(ctx, "account_profiles",
		[]string{
			"avatar_revision", "avatar_version", "avatar_object_id",
			"avatar_media_type", "avatar_size", "avatar_updated_at", "updated_at",
		},
		[]string{"account_id"},
		[]string{"account_id"},
		int64(1), ref.AvatarVersion, ref.AvatarObjectID,
		meta.MediaType, meta.Size, meta.ModifiedAt, meta.ModifiedAt,
	).Scan(&accountID)
	if errors.Is(err, store.ErrNoRows) {
		return Profile{}, store.ErrNoRows
	}
	if err != nil {
		return Profile{}, err
	}
	return scanProfile(scope.QueryRow(ctx, "account_profiles", profileColumns, ""))
}

func (PostgresRepository) SwitchAvatarPointer(
	ctx context.Context,
	scope store.TxAccountScope,
	expected int64,
	ref ObjectRef,
	meta ObjectMeta,
) (Profile, error) {
	rows, err := scope.Update(ctx, "account_profiles", `
		avatar_revision = avatar_revision + 1,
		avatar_version = $2,
		avatar_object_id = $3,
		avatar_media_type = $4,
		avatar_size = $5,
		avatar_updated_at = $6,
		updated_at = $6`,
		`avatar_revision = $7`,
		ref.AvatarVersion, ref.AvatarObjectID, meta.MediaType, meta.Size, meta.ModifiedAt, expected,
	)
	if err != nil {
		return Profile{}, err
	}
	if rows != 1 {
		return Profile{}, ErrAvatarRevisionConflict
	}
	return scanProfile(scope.QueryRow(ctx, "account_profiles", profileColumns, ""))
}

func (PostgresRepository) ClearAvatarPointer(
	ctx context.Context,
	scope store.TxAccountScope,
	expected int64,
	now time.Time,
) (Profile, error) {
	rows, err := scope.Update(ctx, "account_profiles", `
		avatar_revision = avatar_revision + 1,
		avatar_version = NULL,
		avatar_object_id = NULL,
		avatar_media_type = NULL,
		avatar_size = NULL,
		avatar_updated_at = NULL,
		updated_at = $2`,
		`avatar_revision = $3`,
		now, expected,
	)
	if err != nil {
		return Profile{}, err
	}
	if rows != 1 {
		return Profile{}, ErrAvatarRevisionConflict
	}
	return scanProfile(scope.QueryRow(ctx, "account_profiles", profileColumns, ""))
}

func (PostgresRepository) GCExistsForUpdate(
	ctx context.Context,
	scope store.TxAccountScope,
	objectID string,
) (bool, error) {
	var found string
	err := scope.QueryRowForUpdate(ctx, "account_profile_avatar_gc", "avatar_object_id",
		"avatar_object_id = $2", objectID).Scan(&found)
	if errors.Is(err, store.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (PostgresRepository) EnqueueGC(
	ctx context.Context,
	scope store.TxAccountScope,
	ref ObjectRef,
	notBefore time.Time,
) error {
	var objectID string
	err := scope.InsertOnConflictDoNothingReturning(ctx, "account_profile_avatar_gc",
		[]string{"avatar_version", "avatar_object_id", "not_before", "next_attempt_at"},
		[]string{"account_id", "avatar_object_id"},
		[]string{"avatar_object_id"},
		ref.AvatarVersion, ref.AvatarObjectID, notBefore, notBefore,
	).Scan(&objectID)
	if errors.Is(err, store.ErrNoRows) {
		return nil
	}
	return err
}

func (PostgresRepository) ListDueGC(
	ctx context.Context,
	scope store.AccountScope,
	now time.Time,
	limit int,
) ([]GCItem, error) {
	rows, err := scope.QueryPage(ctx, "account_profile_avatar_gc",
		"avatar_version, avatar_object_id, attempts",
		"not_before <= $2 AND next_attempt_at <= $2",
		[]store.OrderBy{{Column: "next_attempt_at"}, {Column: "avatar_object_id"}},
		limit, 0, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]GCItem, 0, limit)
	for rows.Next() {
		var item GCItem
		if err := rows.Scan(&item.Ref.AvatarVersion, &item.Ref.AvatarObjectID, &item.Attempts); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (PostgresRepository) LoadGCForUpdate(
	ctx context.Context,
	scope store.TxAccountScope,
	item GCItem,
) (GCItem, error) {
	var found GCItem
	err := scope.QueryRowForUpdate(ctx, "account_profile_avatar_gc",
		"avatar_version, avatar_object_id, attempts",
		"avatar_object_id = $2", item.Ref.AvatarObjectID,
	).Scan(&found.Ref.AvatarVersion, &found.Ref.AvatarObjectID, &found.Attempts)
	return found, err
}

func (PostgresRepository) DeleteGC(ctx context.Context, scope store.TxAccountScope, item GCItem) error {
	_, err := scope.Delete(ctx, "account_profile_avatar_gc", "avatar_object_id = $2", item.Ref.AvatarObjectID)
	return err
}

func (PostgresRepository) RecordGCFailure(
	ctx context.Context,
	scope store.TxAccountScope,
	item GCItem,
	now time.Time,
) error {
	delay := time.Duration(item.Attempts+1) * time.Minute
	if delay > time.Hour {
		delay = time.Hour
	}
	_, err := scope.Update(ctx, "account_profile_avatar_gc", `
		attempts = attempts + 1,
		next_attempt_at = $2,
		last_error_class = $3,
		updated_at = $4`,
		`avatar_object_id = $5`,
		now.Add(delay), "delete_failed", now, item.Ref.AvatarObjectID,
	)
	return err
}

func (r PostgresRepository) ListCurrentPointers(
	ctx context.Context,
	scope store.AccountScope,
	cursor string,
	_ int,
) ([]Profile, error) {
	// 每个账号最多一行；cursor 非空表示本账号已返回。
	if cursor != "" {
		return nil, nil
	}
	profile, exists, err := r.Snapshot(ctx, scope)
	if err != nil {
		return nil, err
	}
	if !exists || profile.AvatarVersion == nil {
		return nil, nil
	}
	return []Profile{profile}, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProfile(row rowScanner) (Profile, error) {
	var (
		profile     Profile
		displayName sql.NullString
		version     sql.NullString
		objectID    sql.NullString
		mediaType   sql.NullString
		size        sql.NullInt64
		avatarAt    sql.NullTime
		updatedAt   sql.NullTime
	)
	if err := row.Scan(
		&displayName, &profile.ProfileRevision, &profile.AvatarRevision,
		&version, &objectID, &mediaType, &size, &avatarAt, &updatedAt,
	); err != nil {
		if errors.Is(err, store.ErrNoRows) {
			return Profile{}, store.ErrNoRows
		}
		return Profile{}, fmt.Errorf("scan account profile: %w", err)
	}
	profile.DisplayName = nullStringPointer(displayName)
	profile.AvatarVersion = nullStringPointer(version)
	profile.AvatarObjectID = nullStringPointer(objectID)
	profile.AvatarMediaType = nullStringPointer(mediaType)
	profile.AvatarSize = nullInt64Pointer(size)
	profile.AvatarUpdatedAt = nullTimePointer(avatarAt)
	profile.UpdatedAt = nullTimePointer(updatedAt)
	return profile, nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time.UTC()
	return &t
}
