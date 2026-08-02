package dataexport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/customer"
	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	pkgcatalog "github.com/samson/customer-manage-platform/backend/internal/package"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/reminder"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

const customerColumns = "id, account_id, created_at, display_name, real_name, phone, birthday, channel, referrer_customer_id, status, merged_into_customer_id, avatar_revision, avatar_version"
const identityColumns = "id, account_id, created_at, customer_id, platform, handle, remark"
const noteColumns = "id, account_id, created_at, customer_id, content"
const packageColumns = "id, account_id, created_at, name, shoot_type, pricing_mode, base_price, duration_minutes, shot_count_min, shot_count_max, raw_delivery_count, retouch_count, note, status"
const orderColumns = "id, account_id, created_at, customer_id, package_id, title, status, price, deposit_paid, balance_paid, shot_at, delivered_at, note"
const slotColumns = "id, account_id, created_at, start_at, end_at, type, order_id, note"
const reminderColumns = "id, account_id, created_at, type, customer_id, order_id, due_date, content, status, dedup_key"
const settingsColumns = "timezone, birthday_lead_days, follow_up_after_days, churn_thresholds, digest_hour, telegram_chat_id, availability, updated_at"

// PostgresRepository loads the export allowlist without going through paginated domain services.
type PostgresRepository struct{}

func NewPostgresRepository() PostgresRepository { return PostgresRepository{} }

func (PostgresRepository) LoadSnapshot(ctx context.Context, scope store.AccountScope) (Snapshot, error) {
	snapshot := EmptySnapshot()
	err := scope.WithReadSnapshot(ctx, func(readScope store.ReadTxAccountScope) error {
		customers, err := loadCustomers(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load customers: %w", err)
		}
		snapshot.Customers = customers
		identities, err := loadSocialIdentities(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load social identities: %w", err)
		}
		snapshot.SocialIdentities = identities
		notes, err := loadCustomerNotes(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load customer notes: %w", err)
		}
		snapshot.CustomerNotes = notes
		packages, err := loadPackages(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load packages: %w", err)
		}
		snapshot.Packages = packages
		orders, err := loadOrders(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load orders: %w", err)
		}
		snapshot.Orders = orders
		slots, err := loadScheduleSlots(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load schedule slots: %w", err)
		}
		snapshot.ScheduleSlots = slots
		reminders, err := loadReminders(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load reminders: %w", err)
		}
		snapshot.Reminders = reminders
		effective, err := loadEffectiveSettings(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}
		snapshot.Settings = effective
		profile, err := loadAccountProfile(ctx, readScope)
		if err != nil {
			return fmt.Errorf("load account profile: %w", err)
		}
		snapshot.AccountProfile = profile
		return nil
	})
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func loadSocialIdentities(ctx context.Context, scope store.ReadTxAccountScope) ([]customer.SocialIdentity, error) {
	rows, err := scope.Query(ctx, "social_identities", identityColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]customer.SocialIdentity, 0)
	for rows.Next() {
		var item customer.SocialIdentity
		var remark sql.NullString
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.CreatedAt, &item.CustomerID,
			&item.Platform, &item.Handle, &remark,
		); err != nil {
			return nil, err
		}
		item.Remark = nullStringPointer(remark)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadCustomerNotes(ctx context.Context, scope store.ReadTxAccountScope) ([]customer.CustomerNote, error) {
	rows, err := scope.Query(ctx, "customer_notes", noteColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]customer.CustomerNote, 0)
	for rows.Next() {
		var item customer.CustomerNote
		if err := rows.Scan(&item.ID, &item.AccountID, &item.CreatedAt, &item.CustomerID, &item.Content); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadPackages(ctx context.Context, scope store.ReadTxAccountScope) ([]pkgcatalog.Package, error) {
	rows, err := scope.Query(ctx, "packages", packageColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]pkgcatalog.Package, 0)
	for rows.Next() {
		var item pkgcatalog.Package
		var duration, shotMin, shotMax, rawDelivery, retouch sql.NullInt64
		var note sql.NullString
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.CreatedAt, &item.Name, &item.ShootType,
			&item.PricingMode, &item.BasePrice, &duration, &shotMin, &shotMax,
			&rawDelivery, &retouch, &note, &item.Status,
		); err != nil {
			return nil, err
		}
		item.DurationMinutes = nullIntPointer(duration)
		item.ShotCountMin = nullIntPointer(shotMin)
		item.ShotCountMax = nullIntPointer(shotMax)
		item.RawDeliveryCount = nullIntPointer(rawDelivery)
		item.RetouchCount = nullIntPointer(retouch)
		item.Note = nullStringPointer(note)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadOrders(ctx context.Context, scope store.ReadTxAccountScope) ([]orderdomain.Order, error) {
	rows, err := scope.Query(ctx, "orders", orderColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orderdomain.Order, 0)
	for rows.Next() {
		var item orderdomain.Order
		var packageID, title, note sql.NullString
		var price sql.NullInt64
		var shotAt, deliveredAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.CreatedAt, &item.CustomerID, &packageID,
			&title, &item.Status, &price, &item.DepositPaid, &item.BalancePaid,
			&shotAt, &deliveredAt, &note,
		); err != nil {
			return nil, err
		}
		item.PackageID = nullStringPointer(packageID)
		item.Title = nullStringPointer(title)
		item.Price = nullIntPointer(price)
		item.ShotAt = nullTimePointer(shotAt)
		item.DeliveredAt = nullTimePointer(deliveredAt)
		item.Note = nullStringPointer(note)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadScheduleSlots(ctx context.Context, scope store.ReadTxAccountScope) ([]schedule.Slot, error) {
	rows, err := scope.Query(ctx, "schedule_slots", slotColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]schedule.Slot, 0)
	for rows.Next() {
		var item schedule.Slot
		var orderID, note sql.NullString
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.CreatedAt, &item.StartAt, &item.EndAt,
			&item.Type, &orderID, &note,
		); err != nil {
			return nil, err
		}
		item.OrderID = nullStringPointer(orderID)
		item.Note = nullStringPointer(note)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadReminders(ctx context.Context, scope store.ReadTxAccountScope) ([]reminder.Reminder, error) {
	rows, err := scope.Query(ctx, "reminders", reminderColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]reminder.Reminder, 0)
	for rows.Next() {
		var item reminder.Reminder
		var customerID, orderID sql.NullString
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.CreatedAt, &item.Type, &customerID,
			&orderID, &item.DueDate, &item.Content, &item.Status, &item.DedupKey,
		); err != nil {
			return nil, err
		}
		item.CustomerID = nullStringPointer(customerID)
		item.OrderID = nullStringPointer(orderID)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadCustomers(ctx context.Context, scope store.ReadTxAccountScope) ([]customer.Customer, error) {
	rows, err := scope.Query(ctx, "customers", customerColumns, "")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]customer.Customer, 0)
	for rows.Next() {
		var item customer.Customer
		var realName, phone, birthday, referrer, merged sql.NullString
		var avatarVersion sql.NullString
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.CreatedAt, &item.DisplayName,
			&realName, &phone, &birthday, &item.Channel, &referrer, &item.Status, &merged,
			&item.AvatarRevision, &avatarVersion,
		); err != nil {
			return nil, err
		}
		item.RealName = nullStringPointer(realName)
		item.Phone = nullStringPointer(phone)
		item.Birthday = nullStringPointer(birthday)
		item.ReferrerCustomerID = nullStringPointer(referrer)
		item.MergedIntoCustomerID = nullStringPointer(merged)
		item.AvatarVersion = nullStringPointer(avatarVersion)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func loadEffectiveSettings(ctx context.Context, scope store.ReadTxAccountScope) (settings.Settings, error) {
	var stored settings.Settings
	var thresholds []byte
	var availability []byte
	var chatID sql.NullString
	err := scope.QueryRow(ctx, "settings", settingsColumns, "").Scan(
		&stored.Timezone,
		&stored.BirthdayLeadDays,
		&stored.FollowUpAfterDays,
		&thresholds,
		&stored.DigestHour,
		&chatID,
		&availability,
		&stored.UpdatedAt,
	)
	if errors.Is(err, store.ErrNoRows) {
		return settings.DefaultSettings(), nil
	}
	if err != nil {
		return settings.Settings{}, err
	}
	if err := json.Unmarshal(thresholds, &stored.ChurnThresholds); err != nil {
		return settings.Settings{}, err
	}
	decodedAvailability, err := settings.DecodeScheduleAvailabilityJSON(availability)
	if err != nil {
		return settings.Settings{}, fmt.Errorf("decode stored settings availability: %v", err)
	}
	stored.Availability = decodedAvailability
	stored.TelegramChatID = nullStringPointer(chatID)
	return settings.EffectiveSettings(stored), nil
}

func loadAccountProfile(ctx context.Context, scope store.ReadTxAccountScope) (AccountProfileExport, error) {
	var (
		displayName   sql.NullString
		profileRev    int64
		avatarRev     int64
		avatarVersion sql.NullString
		mediaType     sql.NullString
		size          sql.NullInt64
		avatarAt      sql.NullTime
		updatedAt     sql.NullTime
	)
	err := scope.QueryRow(ctx, "account_profiles",
		"display_name, profile_revision, avatar_revision, avatar_version, avatar_media_type, avatar_size, avatar_updated_at, updated_at",
		"",
	).Scan(&displayName, &profileRev, &avatarRev, &avatarVersion, &mediaType, &size, &avatarAt, &updatedAt)
	if errors.Is(err, store.ErrNoRows) {
		return VirtualAccountProfile(), nil
	}
	if err != nil {
		return AccountProfileExport{}, err
	}
	out := AccountProfileExport{
		DisplayName:     nullStringPointer(displayName),
		ProfileRevision: profileRev,
		AvatarRevision:  avatarRev,
		UpdatedAt:       nullTimePointer(updatedAt),
	}
	if avatarVersion.Valid && mediaType.Valid && size.Valid && avatarAt.Valid {
		out.Avatar = &AccountProfileAvatarExport{
			Version:   avatarVersion.String,
			MediaType: mediaType.String,
			Size:      size.Int64,
			UpdatedAt: avatarAt.Time.UTC(),
		}
	}
	return out, nil
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullIntPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

func nullTimePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
