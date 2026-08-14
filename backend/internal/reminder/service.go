package reminder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// SettingsLoader 加载扫描所需设置（由 settings 域适配）。
type SettingsLoader interface {
	Load(ctx context.Context, scope store.AccountScope) (SettingsView, error)
}

// Repository 是 reminder 持久化 + 扫描取数面。
type Repository interface {
	List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error)
	Find(ctx context.Context, scope store.AccountScope, id string) (Reminder, error)
	InsertIdempotent(ctx context.Context, scope store.AccountScope, r Reminder) (inserted bool, err error)
	CreateCustom(ctx context.Context, scope store.AccountScope, input CreateCustomInput) (Reminder, error)
	SetStatus(ctx context.Context, scope store.AccountScope, id, status string) (Reminder, error)
	AutoDismissByIDs(ctx context.Context, scope store.AccountScope, ids []string) (int, error)
	ListPendingWithOrder(ctx context.Context, scope store.AccountScope) ([]Reminder, error)
	ListPendingChurn(ctx context.Context, scope store.AccountScope) ([]Reminder, error)
	LoadScanState(ctx context.Context, scope store.AccountScope) (last time.Time, found bool, err error)
	SaveScanState(ctx context.Context, scope store.AccountScope, date time.Time) error
	ReassignCustomer(ctx context.Context, scope store.AccountScope, targetID, sourceID string) error
	ListActiveCustomers(ctx context.Context, scope store.AccountScope) ([]ActiveCustomer, error)
	ListNonCancelledOrders(ctx context.Context, scope store.AccountScope) ([]ScanOrder, error)
	ListPackageMeta(ctx context.Context, scope store.AccountScope) (map[string]PackageMeta, error)
	OrderExists(ctx context.Context, scope store.AccountScope, orderID string) (bool, error)
	CustomerExists(ctx context.Context, scope store.AccountScope, customerID string) (bool, error)
}

// Service 是提醒域服务。
type Service struct {
	repo      Repository
	settings  SettingsLoader
	logger    *slog.Logger
	now       func() time.Time
	freshness *AssignmentReminderFreshness
}

func NewService(repo Repository, settings SettingsLoader, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		repo:     repo,
		settings: settings,
		logger:   logger,
		now:      time.Now,
	}
}

// WithFreshness wires Bearer final-read freshness (S4). Missing wiring keeps legacy List.
func (s *Service) WithFreshness(freshness *AssignmentReminderFreshness) *Service {
	s.freshness = freshness
	return s
}

// WithClock 注入 now，供测试。
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

const (
	MaxPageSize = 100
)

func (s *Service) List(ctx context.Context, scope store.AccountScope, filter ListFilter) (ListResult, error) {
	normalized, err := normalizeListFilter(filter)
	if err != nil {
		return ListResult{}, err
	}
	if s.freshness != nil {
		fresh, err := s.ListWithFreshnessFinalRead(ctx, scope, normalized, *s.freshness)
		if err != nil {
			return ListResult{}, err
		}
		return ListResult(fresh), nil
	}
	return s.repo.List(ctx, scope, normalized)
}

func (s *Service) CreateCustom(ctx context.Context, scope store.AccountScope, input CreateCustomInput) (Reminder, error) {
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return Reminder{}, ValidationError{Message: "content 必填"}
	}
	if input.DueDate.IsZero() {
		return Reminder{}, ValidationError{Message: "due_date 必填"}
	}
	if input.CustomerID != nil {
		id := strings.TrimSpace(*input.CustomerID)
		if id == "" {
			input.CustomerID = nil
		} else {
			input.CustomerID = &id
			exists, err := s.repo.CustomerExists(ctx, scope, id)
			if err != nil {
				return Reminder{}, err
			}
			if !exists {
				return Reminder{}, fmt.Errorf("%w: 客户不存在", ErrNotFound)
			}
		}
	}
	input.Content = content
	input.DueDate = dateOnly(input.DueDate)
	return s.repo.CreateCustom(ctx, scope, input)
}

// Scan 对单账号执行完整扫描 pipeline（单事务）。
// date 为零值时取账号时区今日。
func (s *Service) Scan(ctx context.Context, scope store.AccountScope, date *time.Time) (ScanResult, error) {
	view, err := s.settings.Load(ctx, scope)
	if err != nil {
		return ScanResult{}, err
	}
	clock, err := NewAccountClock(view.Timezone)
	if err != nil {
		return ScanResult{}, ValidationError{Message: "账号时区非法"}
	}
	scanDate := clock.LocalDate(s.now())
	if date != nil {
		scanDate = dateOnly(*date)
	}

	var result ScanResult
	err = scope.WithinTx(ctx, func(tx store.AccountScope) error {
		var txErr error
		result, txErr = s.scanInScope(ctx, tx, clock, view, scanDate)
		return txErr
	})
	if err != nil {
		return ScanResult{}, err
	}
	// account_id 由 runner/handler 上下文补充；此处至少打 date 与三计数（REV-004 由 runner 补 account）
	s.logger.Info("reminder scan completed",
		slog.String("date", FormatDate(scanDate)),
		slog.Int("created", result.Created),
		slog.Int("skipped", result.Skipped),
		slog.Int("auto_dismissed", result.AutoDismissed),
	)
	return result, nil
}

// ScanAndCheckpoint 供 runner：扫描成功后推进检查点。
func (s *Service) ScanAndCheckpoint(ctx context.Context, scope store.AccountScope, localToday time.Time) (ScanResult, error) {
	d := dateOnly(localToday)
	result, err := s.Scan(ctx, scope, &d)
	if err != nil {
		return ScanResult{}, err
	}
	if err := s.repo.SaveScanState(ctx, scope, d); err != nil {
		return result, err
	}
	return result, nil
}

// NeedsScan 判断 runner 是否应对该账号扫描。
func (s *Service) NeedsScan(ctx context.Context, scope store.AccountScope, localToday time.Time) (bool, error) {
	last, found, err := s.repo.LoadScanState(ctx, scope)
	if err != nil {
		return false, err
	}
	if !found {
		return true, nil
	}
	return last.Before(dateOnly(localToday)), nil
}

func (s *Service) scanInScope(
	ctx context.Context,
	scope store.AccountScope,
	clock AccountClock,
	view SettingsView,
	scanDate time.Time,
) (ScanResult, error) {
	var result ScanResult

	// 1) auto-dismiss
	n, err := s.autoDismiss(ctx, scope)
	if err != nil {
		return result, err
	}
	result.AutoDismissed = n

	// 2) batch load
	customers, err := s.repo.ListActiveCustomers(ctx, scope)
	if err != nil {
		return result, err
	}
	orders, err := s.repo.ListNonCancelledOrders(ctx, scope)
	if err != nil {
		return result, err
	}
	pkgMeta, err := s.repo.ListPackageMeta(ctx, scope)
	if err != nil {
		return result, err
	}

	ordersByCustomer := make(map[string][]ScanOrder, len(customers))
	for _, o := range orders {
		ordersByCustomer[o.CustomerID] = append(ordersByCustomer[o.CustomerID], o)
	}
	customerByID := make(map[string]ActiveCustomer, len(customers))
	for _, c := range customers {
		customerByID[c.ID] = c
	}

	// 3) birthday
	for _, c := range customers {
		cand, ok := evalBirthday(c, scanDate, view.BirthdayLeadDays)
		if !ok {
			continue
		}
		inserted, skip, err := s.insertCandidate(ctx, scope, cand)
		if err != nil {
			return result, err
		}
		if skip {
			result.Skipped++
		} else if inserted {
			result.Created++
		}
	}

	// 4) follow_up
	for _, o := range orders {
		if o.Status != "delivered" {
			continue
		}
		c, ok := customerByID[o.CustomerID]
		if !ok {
			continue // non-active
		}
		cand, skipReason, ok := evalFollowUp(o, c, scanDate, view.FollowUpAfterDays, clock, pkgMeta)
		if skipReason != "" {
			s.logger.Debug("reminder scan skip",
				slog.String("rule", TypeFollowUp),
				slog.String("order_id", o.ID),
				slog.String("reason", skipReason),
			)
			result.Skipped++
			continue
		}
		if !ok {
			continue
		}
		inserted, skip, err := s.insertCandidate(ctx, scope, cand)
		if err != nil {
			return result, err
		}
		if skip {
			result.Skipped++
		} else if inserted {
			result.Created++
		}
	}

	// 5) churn
	for _, c := range customers {
		cOrders := ordersByCustomer[c.ID]
		cand, skipReason, ok := evalChurn(c, cOrders, pkgMeta, scanDate, clock, view)
		if skipReason != "" {
			s.logger.Debug("reminder scan skip",
				slog.String("rule", TypeChurn),
				slog.String("customer_id", c.ID),
				slog.String("reason", skipReason),
			)
			result.Skipped++
			continue
		}
		if !ok {
			continue
		}
		inserted, skip, err := s.insertCandidate(ctx, scope, cand)
		if err != nil {
			return result, err
		}
		if skip {
			result.Skipped++
		} else if inserted {
			result.Created++
		}
	}

	return result, nil
}

func (s *Service) autoDismiss(ctx context.Context, scope store.AccountScope) (int, error) {
	ids := make([]string, 0)
	// 孤儿 order_id
	withOrder, err := s.repo.ListPendingWithOrder(ctx, scope)
	if err != nil {
		return 0, err
	}
	for _, r := range withOrder {
		if r.OrderID == nil {
			continue
		}
		exists, err := s.repo.OrderExists(ctx, scope, *r.OrderID)
		if err != nil {
			return 0, err
		}
		if !exists {
			ids = append(ids, r.ID)
		}
	}
	// 复购：pending churn + 客户有非终态订单
	churns, err := s.repo.ListPendingChurn(ctx, scope)
	if err != nil {
		return 0, err
	}
	if len(churns) > 0 {
		orders, err := s.repo.ListNonCancelledOrders(ctx, scope)
		if err != nil {
			return 0, err
		}
		hasNonTerminal := make(map[string]bool)
		for _, o := range orders {
			if isNonTerminalStatus(o.Status) {
				hasNonTerminal[o.CustomerID] = true
			}
		}
		for _, r := range churns {
			if r.CustomerID != nil && hasNonTerminal[*r.CustomerID] {
				ids = append(ids, r.ID)
			}
		}
	}
	// 去重 id
	seen := make(map[string]struct{}, len(ids))
	unique := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return s.repo.AutoDismissByIDs(ctx, scope, unique)
}

func (s *Service) insertCandidate(ctx context.Context, scope store.AccountScope, cand Reminder) (inserted, skipped bool, err error) {
	cand.Status = StatusPending
	ok, err := s.repo.InsertIdempotent(ctx, scope, cand)
	if err != nil {
		return false, false, err
	}
	if !ok {
		return false, true, nil
	}
	return true, false, nil
}

func normalizeListFilter(filter ListFilter) (ListFilter, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > MaxPageSize {
		filter.PageSize = MaxPageSize
	}
	if filter.Status != "" {
		switch filter.Status {
		case StatusPending, StatusDone, StatusDismissed:
		default:
			return ListFilter{}, ValidationError{Message: "status 非法"}
		}
	}
	return filter, nil
}

// isNonTerminalStatus 与订单域 terminalStatus 对齐：终态仅 closed|cancelled。
// delivered 仍是服务中态（尾款未结等），算非终态——churn 要求「当前无非终态订单」。
func isNonTerminalStatus(status string) bool {
	return status != "closed" && status != "cancelled"
}

// candidate builders

func evalBirthday(c ActiveCustomer, scanDate time.Time, leadDays int) (Reminder, bool) {
	if c.Birthday == nil {
		return Reminder{}, false
	}
	due, ok := NextBirthdayOccurrence(scanDate, *c.Birthday)
	if !ok {
		return Reminder{}, false
	}
	windowEnd := AddDays(scanDate, leadDays)
	if due.Before(scanDate) || due.After(windowEnd) {
		return Reminder{}, false
	}
	// content 模板：{display_name} 生日（MM-DD）
	mmdd := due.Format("01-02")
	cid := c.ID
	return Reminder{
		Type:       TypeBirthday,
		CustomerID: &cid,
		DueDate:    due,
		Content:    fmt.Sprintf("%s 生日（%s）", c.DisplayName, mmdd),
		DedupKey:   BirthdayDedupKey(c.ID, due),
	}, true
}

func evalFollowUp(
	o ScanOrder,
	c ActiveCustomer,
	scanDate time.Time,
	followDays int,
	clock AccountClock,
	pkgMeta map[string]PackageMeta,
) (Reminder, string, bool) {
	if o.DeliveredAt == nil {
		return Reminder{}, "missing_delivered_at", false
	}
	deliveredLocal := clock.LocalDate(*o.DeliveredAt)
	due := AddDays(deliveredLocal, followDays)
	if due.After(scanDate) {
		return Reminder{}, "", false
	}
	title := ""
	if o.Title != nil && strings.TrimSpace(*o.Title) != "" {
		title = strings.TrimSpace(*o.Title)
	} else if o.PackageID != nil {
		if meta, ok := pkgMeta[*o.PackageID]; ok && strings.TrimSpace(meta.Name) != "" {
			title = strings.TrimSpace(meta.Name)
		}
	}
	if title == "" {
		title = "订单"
	}
	cid, oid := c.ID, o.ID
	return Reminder{
		Type:       TypeFollowUp,
		CustomerID: &cid,
		OrderID:    &oid,
		DueDate:    due,
		Content:    fmt.Sprintf("回访 %s：%s已交付", c.DisplayName, title),
		DedupKey:   FollowUpDedupKey(o.ID),
	}, "", true
}

func evalChurn(
	c ActiveCustomer,
	orders []ScanOrder,
	pkgMeta map[string]PackageMeta,
	scanDate time.Time,
	clock AccountClock,
	view SettingsView,
) (Reminder, string, bool) {
	// 有 ≥1 单 delivered|closed；当前无非终态（非 closed/cancelled）；超阈值
	var completed []ScanOrder
	hasNonTerminal := false
	for _, o := range orders {
		if isNonTerminalStatus(o.Status) {
			hasNonTerminal = true
		}
		if o.Status == "delivered" || o.Status == "closed" {
			completed = append(completed, o)
		}
	}
	if len(completed) == 0 {
		return Reminder{}, "", false // 零成交不告警
	}
	if hasNonTerminal {
		return Reminder{}, "", false
	}
	// 最近一单 = 非 cancelled 中 shot_at 最大者（含全部非 cancelled，不只 completed）
	var latest *ScanOrder
	for i := range orders {
		o := &orders[i]
		if o.ShotAt == nil {
			continue
		}
		if latest == nil || o.ShotAt.After(*latest.ShotAt) {
			latest = o
		}
	}
	if latest == nil {
		return Reminder{}, "missing_shot_at", false
	}
	if latest.ShotAt == nil {
		return Reminder{}, "missing_shot_at", false
	}
	shootType := "other"
	if latest.PackageID != nil {
		if meta, ok := pkgMeta[*latest.PackageID]; ok && meta.ShootType != "" {
			shootType = meta.ShootType
		}
	}
	threshold := DefaultChurnDays(view, shootType)
	shotLocal := clock.LocalDate(*latest.ShotAt)
	days := DaysBetween(shotLocal, scanDate)
	if days <= threshold {
		return Reminder{}, "", false
	}
	cid, oid := c.ID, latest.ID
	return Reminder{
		Type:       TypeChurn,
		CustomerID: &cid,
		OrderID:    &oid,
		DueDate:    scanDate,
		Content:    fmt.Sprintf("%s 已 %d 天未拍摄", c.DisplayName, days),
		DedupKey:   ChurnDedupKey(c.ID, latest.ID),
	}, "", true
}

func DefaultChurnDays(view SettingsView, shootType string) int {
	if view.ChurnDaysFor != nil {
		return view.ChurnDaysFor(shootType)
	}
	return 180
}

// IsNotFound / IsValidation 供 handler 映射。
func IsNotFound(err error) bool   { return errors.Is(err, ErrNotFound) }
func IsValidation(err error) bool { return errors.Is(err, ErrValidation) }
