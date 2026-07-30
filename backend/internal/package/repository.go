package pkgcatalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository {
	return PostgresRepository{}
}

func (PostgresRepository) Create(ctx context.Context, scope store.AccountScope, input CreateInput) (Package, error) {
	id := "pkg_" + uuid.NewString()
	if _, err := scope.InsertReturningID(ctx, "packages",
		[]string{
			"id",
			"name",
			"shoot_type",
			"pricing_mode",
			"base_price",
			"duration_minutes",
			"shot_count_min",
			"shot_count_max",
			"raw_delivery_count",
			"retouch_count",
			"note",
			"status",
		},
		id,
		input.Name,
		input.ShootType,
		input.PricingMode,
		input.BasePrice,
		nullableIntArg(input.DurationMinutes),
		nullableIntArg(input.ShotCountMin),
		nullableIntArg(input.ShotCountMax),
		nullableIntArg(input.RawDeliveryCount),
		nullableIntArg(input.RetouchCount),
		nullableStringArg(input.Note),
		StatusActive,
	); err != nil {
		return Package{}, err
	}
	return findPackage(ctx, scope, id)
}

func (PostgresRepository) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	cond, args := buildPackageFilter(filter)
	total, err := scope.Count(ctx, "packages", cond, args...)
	if err != nil {
		return ListResult{}, err
	}
	rows, err := scope.QueryPage(ctx, "packages", packageColumns, cond,
		[]store.OrderBy{{Column: "created_at", Desc: true}, {Column: "id", Desc: true}},
		filter.PageSize, (filter.Page-1)*filter.PageSize, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	items := make([]ListItem, 0, filter.PageSize)
	for rows.Next() {
		pkg, err := scanPackage(rows)
		if err != nil {
			return ListResult{}, err
		}
		ordersCount, err := countActiveOrderReferences(ctx, scope, pkg.ID)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, ListItem{Package: pkg, OrdersCount: int(ordersCount)})
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}
	return ListResult{Items: items, Total: total}, nil
}

func (PostgresRepository) Update(ctx context.Context, scope store.AccountScope, id string, input UpdateInput) (Package, error) {
	var updated Package
	err := scope.WithinTx(ctx, func(tx store.AccountScope) error {
		current, err := findPackageForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := validateEffectiveShotRange(current, input); err != nil {
			return err
		}

		sets := make([]string, 0, 11)
		args := make([]any, 0, 11)
		set := func(column string, value any) {
			args = append(args, value)
			sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)+1))
		}
		if input.Name != nil {
			set("name", *input.Name)
		}
		if input.ShootType != nil {
			set("shoot_type", *input.ShootType)
		}
		if input.PricingMode != nil {
			set("pricing_mode", *input.PricingMode)
		}
		if input.BasePrice != nil {
			set("base_price", *input.BasePrice)
		}
		applyClearableInt(set, "duration_minutes", input.DurationMinutes)
		applyClearableInt(set, "shot_count_min", input.ShotCountMin)
		applyClearableInt(set, "shot_count_max", input.ShotCountMax)
		applyClearableInt(set, "raw_delivery_count", input.RawDeliveryCount)
		applyClearableInt(set, "retouch_count", input.RetouchCount)
		if input.Note != nil {
			set("note", nullableStringArg(input.Note))
		}
		if input.Status != nil {
			set("status", *input.Status)
		}
		if len(sets) > 0 {
			cond := fmt.Sprintf("id = $%d", len(args)+2)
			args = append(args, id)
			if _, err := tx.Update(ctx, "packages", strings.Join(sets, ", "), cond, args...); err != nil {
				return err
			}
		}
		updated, err = findPackage(ctx, tx, id)
		return err
	})
	if err != nil {
		return Package{}, err
	}
	return updated, nil
}

func (PostgresRepository) Delete(ctx context.Context, scope store.AccountScope, id string) error {
	ordersReady, err := ordersTableExists(ctx, scope)
	if err != nil {
		return err
	}
	return scope.WithinTx(ctx, func(tx store.AccountScope) error {
		current, err := findPackageForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		count, err := countOrderReferences(ctx, tx, current.ID, ordersReady)
		if err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("%w: 套系已被订单引用，不可删除", ErrPackageInUse)
		}
		rows, err := tx.Delete(ctx, "packages", "id = $2", id)
		if err != nil {
			return err
		}
		if rows == 0 {
			return fmt.Errorf("%w: 套系不存在", ErrNotFound)
		}
		return nil
	})
}

func validateEffectiveShotRange(current Package, input UpdateInput) error {
	minValue := current.ShotCountMin
	maxValue := current.ShotCountMax
	if input.ShotCountMin.IsSpecified() {
		minValue = nullableIntPtr(input.ShotCountMin)
	}
	if input.ShotCountMax.IsSpecified() {
		maxValue = nullableIntPtr(input.ShotCountMax)
	}
	return validateShotRange(minValue, maxValue)
}

func applyClearableInt(set func(string, any), column string, value nullable.Nullable[int]) {
	if !value.IsSpecified() {
		return
	}
	if value.IsNull() {
		set(column, nil)
		return
	}
	set(column, value.MustGet())
}

func ordersTableExists(ctx context.Context, scope store.AccountScope) (bool, error) {
	if _, err := scope.Count(ctx, "orders", "1 = 0"); err != nil {
		if isUndefinedTable(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func countOrderReferences(ctx context.Context, scope store.AccountScope, packageID string, ordersReady bool) (int64, error) {
	if !ordersReady {
		return 0, nil
	}
	return scope.Count(ctx, "orders", "package_id = $2", packageID)
}

func countActiveOrderReferences(ctx context.Context, scope store.AccountScope, packageID string) (int64, error) {
	return scope.Count(ctx, "orders", "package_id = $2 AND status <> $3", packageID, "cancelled")
}

func isUndefinedTable(err error) bool {
	var stateErr interface{ SQLState() string }
	return errors.As(err, &stateErr) && stateErr.SQLState() == "42P01"
}

const packageColumns = "id, account_id, created_at, name, shoot_type, pricing_mode, base_price, duration_minutes, shot_count_min, shot_count_max, raw_delivery_count, retouch_count, note, status"

type scanner interface {
	Scan(dest ...any) error
}

func findPackage(ctx context.Context, scope store.AccountScope, id string) (Package, error) {
	pkg, err := scanPackage(scope.QueryRow(ctx, "packages", packageColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Package{}, fmt.Errorf("%w: 套系不存在", ErrNotFound)
	}
	return pkg, err
}

func findPackageForUpdate(ctx context.Context, scope store.AccountScope, id string) (Package, error) {
	pkg, err := scanPackage(scope.QueryRowForUpdate(ctx, "packages", packageColumns, "id = $2", id))
	if errors.Is(err, store.ErrNoRows) {
		return Package{}, fmt.Errorf("%w: 套系不存在", ErrNotFound)
	}
	return pkg, err
}

func scanPackage(row scanner) (Package, error) {
	var pkg Package
	var duration, shotMin, shotMax, rawDelivery, retouch sql.NullInt64
	var note sql.NullString
	if err := row.Scan(
		&pkg.ID,
		&pkg.AccountID,
		&pkg.CreatedAt,
		&pkg.Name,
		&pkg.ShootType,
		&pkg.PricingMode,
		&pkg.BasePrice,
		&duration,
		&shotMin,
		&shotMax,
		&rawDelivery,
		&retouch,
		&note,
		&pkg.Status,
	); err != nil {
		return Package{}, err
	}
	pkg.DurationMinutes = intPtr(duration)
	pkg.ShotCountMin = intPtr(shotMin)
	pkg.ShotCountMax = intPtr(shotMax)
	pkg.RawDeliveryCount = intPtr(rawDelivery)
	pkg.RetouchCount = intPtr(retouch)
	pkg.Note = stringPtr(note)
	return pkg, nil
}

func buildPackageFilter(filter ListFilter) (string, []any) {
	if filter.Status == StatusAll {
		return "", nil
	}
	return "status = $2", []any{filter.Status}
}

func intPtr(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	v := int(value.Int64)
	return &v
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringArg(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}
