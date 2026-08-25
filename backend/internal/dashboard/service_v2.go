// service_v2.go 编排 GET /dashboard/v2：取设置 → 算窗口 → 调仓库 → 纯算法派生
// （空档/利用率走 schedule 权威实现，环比/AOV/复购/矩阵排序在本包单点）。
package dashboard

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	orderdomain "github.com/samson/customer-manage-platform/backend/internal/order"
	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
	"github.com/samson/customer-manage-platform/backend/internal/schedule"
	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// V2SettingsReader 是 v2 读模型取账号设置的最小依赖（settings.Service 实现全部方法）。
// dashboard 包因此不 import httpapi，也不注入他域业务 service。
type V2SettingsReader interface {
	TimezoneForAccount(ctx context.Context, accountID string) (string, error)
	AvailabilityForAccount(ctx context.Context, accountID string) (settings.ScheduleAvailability, error)
	HealthTiersForAccount(ctx context.Context, accountID string) (settings.HealthTiers, error)
}

// nextShootHorizonDays 是 next_shoot 候选扫描的工程上界；语义本身无界（最早未取消 shoot）。
const nextShootHorizonDays = 365

// orderStatusCancelledValue 是订单状态契约枚举值（与 schedule 包同源按值比较）。
const orderStatusCancelledValue = "cancelled"

// GetV2 按账号时区聚合经营台 v2 读模型。设置读取/解析失败即返回 error（handler 落 500），
// 绝不回退默认时区或默认偏好继续算（design D3 / S12）。
func (s *Service) GetV2(ctx context.Context, scope store.AccountScope, accountID string) (V2, error) {
	timezone, err := s.settingsReader.TimezoneForAccount(ctx, accountID)
	if err != nil {
		return V2{}, err
	}
	availability, err := s.settingsReader.AvailabilityForAccount(ctx, accountID)
	if err != nil {
		return V2{}, err
	}
	healthTiers, err := s.settingsReader.HealthTiersForAccount(ctx, accountID)
	if err != nil {
		return V2{}, err
	}
	accountClock, err := clock.NewAccountClock(timezone)
	if err != nil {
		return V2{}, err
	}
	today := accountClock.LocalDate(s.now())
	dayStart, dayEnd := accountClock.DayBounds(today)
	recentStart, _ := accountClock.DayBounds(clock.AddDays(today, -29))
	previousStart, _ := accountClock.DayBounds(clock.AddDays(today, -59))
	repeatStart, _ := accountClock.DayBounds(clock.AddDays(today, -89))
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthDays := schedule.DaysInMonth(monthStart)
	// 当月末日 = 月起点 + MonthDays-1 天；其 DayBounds 上界即月档期查询的半开上界。
	_, monthEnd := accountClock.DayBounds(clock.AddDays(monthStart, monthDays-1))
	window := V2Window{
		Today:         today,
		DueBefore:     clock.AddDays(today, 2),
		DayStart:      dayStart,
		DayEnd:        dayEnd,
		RecentStart:   recentStart,
		RecentEnd:     dayEnd,
		PreviousStart: previousStart,
		RepeatStart:   repeatStart,
		MonthStart:    monthStart,
		MonthDays:     monthDays,
		MonthEnd:      monthEnd,
		NextShootTo:   dayStart.AddDate(0, 0, nextShootHorizonDays),
	}
	facts, err := s.repo.LoadDashboardV2(ctx, scope, window)
	if err != nil {
		return V2{}, err
	}
	return s.assembleV2(accountClock, window, availabilityPlanFromSettings(availability), healthTiers, facts)
}

func (s *Service) assembleV2(
	accountClock clock.AccountClock,
	window V2Window,
	plan schedule.AvailabilityPlan,
	healthTiers settings.HealthTiers,
	facts V2Facts,
) (V2, error) {
	loc := accountClock.Location()

	nextShoot := (*schedule.ListItem)(nil)
	for i := range facts.NextShootPool {
		if facts.NextShootPool[i].Type == schedule.TypeShoot && facts.NextShootPool[i].OrderStatus != orderStatusCancelledValue {
			nextShoot = &facts.NextShootPool[i]
			break
		}
	}

	todayWindow, err := schedule.ResolveDayWindow(window.Today, loc, plan)
	if err != nil {
		return V2{}, fmt.Errorf("dashboard v2 今日工作窗解析失败: %w", err)
	}
	todayOpenings := schedule.DayOpenings(window.Today, todayWindow, daySlotsFromListItems(facts.TodaySlots), plan.MinOpeningMinutes, loc)

	overview, dayErrors := schedule.MonthOverview(daySlotsFromListItems(facts.MonthSlots), window.MonthStart, window.MonthDays, loc, plan)
	if len(dayErrors) > 0 {
		return V2{}, fmt.Errorf("dashboard v2 月利用率解析失败: %v", dayErrors[0])
	}

	return V2{
		NextShoot:     nextShoot,
		TodaySlots:    facts.TodaySlots,
		TodayOpenings: TodayOpenings{WorkingWindow: todayWindow, Openings: todayOpenings},
		DeliveryQueue: buildDeliveryQueue(window.Today, facts.Queue),
		Waterfall:     buildWaterfall(window, facts),
		Utilization: UtilizationOverview{
			Month:        window.MonthStart.Format("2006-01"),
			Utilization:  overview.Utilization,
			ShootCount:   overview.ShootCount,
			HoldDays:     overview.HoldDays,
			OpenDays:     overview.OpenDays,
			ConflictDays: overview.ConflictDays,
		},
		ChannelMatrix: buildChannelMatrix(facts.MatrixRows),
		DueReminders:  facts.DueReminders,
		CustomerHealth: func() CustomerHealth {
			healthCustomers, healthOrders := healthInputRows(accountClock, facts)
			return buildCustomerHealth(window.Today, healthTiers, healthCustomers, healthOrders)
		}(),
	}, nil
}

// healthInputRows 把仓储瞬时事实行折算成账号本地 date-only 输入行（节奏按自然日计）。
func healthInputRows(accountClock clock.AccountClock, facts V2Facts) ([]HealthCustomerRow, []HealthOrderRow) {
	customers := make([]HealthCustomerRow, 0, len(facts.HealthCustomers))
	for _, row := range facts.HealthCustomers {
		customers = append(customers, HealthCustomerRow{
			ID:          row.ID,
			DisplayName: row.DisplayName,
			Channel:     row.Channel,
			CreatedAt:   accountClock.LocalDate(row.CreatedAt),
		})
	}
	orders := make([]HealthOrderRow, 0, len(facts.HealthOrders))
	for _, row := range facts.HealthOrders {
		var shotDate *time.Time
		if row.ShotAt != nil {
			date := accountClock.LocalDate(*row.ShotAt)
			shotDate = &date
		}
		orders = append(orders, HealthOrderRow{
			CustomerID:  row.CustomerID,
			ShotDate:    shotDate,
			Price:       row.Price,
			BalancePaid: row.BalancePaid,
			AmountPaid:  row.AmountPaid,
		})
	}
	return customers, orders
}

// 健康度基线三值（roadmap §4.3）：personal=≥2 拍个人节奏；fallback=仅 1 拍通用基线
// （UI 须标注）；none=新客无拍摄。
const (
	HealthBaselinePersonal = "personal"
	HealthBaselineFallback = "fallback"
	HealthBaselineNone     = "none"
)

// healthTierItemCap 是单层 items 契约上限（§4.3：超出截断，count 仍为全量数）。
const healthTierItemCap = 50

// healthSameDayGapFloorDays：同日多单（加急件）间隔 0 会把基线拉失真甚至除零，
// 按此下限兜底（工程防除零，不参数化）。
const healthSameDayGapFloorDays = 7

// healthCustomerFacts 是单客户的健康度聚合事实（buildCustomerHealth 内部用）。
type healthCustomerFacts struct {
	customer      HealthCustomerRow
	shotDates     []time.Time
	settledLTV    int
	unsettledPaid int
}

// buildCustomerHealth 按 personal 节奏把 active 客户全员分五层（纯函数，§4.3 口径）：
// ratio = 距上次拍摄自然日 ÷ 节奏基线；≥2 拍取相邻间隔均值（同日按下限兜底）、
// 仅 1 拍用 fallback 通用基线、0 拍归新客；阈值判定 ≤sleeping=active、≤at_risk=
// sleeping、≤lost=at_risk、>lost=lost。行金额双口径：SettledLTV 与渠道矩阵同源
// （at_risk/lost 排序用它），UnsettledPaid 仅展示。层内排序 at_risk/lost 按
// settled DESC → ratio DESC，active/sleeping 按 ratio DESC，new 按 created DESC，
// tie 一律 id ASC；items 每层截断 healthTierItemCap。
func buildCustomerHealth(
	today time.Time,
	params settings.HealthTiers,
	customers []HealthCustomerRow,
	orders []HealthOrderRow,
) CustomerHealth {
	byCustomer := make(map[string]*healthCustomerFacts, len(customers))
	list := make([]*healthCustomerFacts, 0, len(customers))
	for _, customer := range customers {
		entry := &healthCustomerFacts{customer: customer}
		byCustomer[customer.ID] = entry
		list = append(list, entry)
	}
	for _, order := range orders {
		entry, ok := byCustomer[order.CustomerID]
		if !ok {
			continue // 非 active 客户（archived/merged）的订单不入盘点
		}
		if order.ShotDate != nil {
			entry.shotDates = append(entry.shotDates, *order.ShotDate)
		}
		if order.BalancePaid {
			if order.Price != nil {
				entry.settledLTV += *order.Price
			}
		} else if order.AmountPaid != nil {
			entry.unsettledPaid += *order.AmountPaid
		}
	}

	health := CustomerHealth{Thresholds: params}
	buckets := map[string]*CustomerHealthBucket{
		"active":   &health.Tiers.Active,
		"sleeping": &health.Tiers.Sleeping,
		"at_risk":  &health.Tiers.AtRisk,
		"lost":     &health.Tiers.Lost,
		"new":      &health.Tiers.New,
	}
	for _, entry := range list {
		item := buildHealthItem(today, params, entry)
		bucket := buckets[itemTier(item, params)]
		bucket.Count++
		bucket.Items = append(bucket.Items, item)
		health.Total++
	}
	sortHealthItems(health.Tiers.Active.Items, "active")
	sortHealthItems(health.Tiers.Sleeping.Items, "sleeping")
	sortHealthItems(health.Tiers.AtRisk.Items, "at_risk")
	sortHealthItems(health.Tiers.Lost.Items, "lost")
	sortHealthItems(health.Tiers.New.Items, "new")
	health.Tiers.Active.Items = capHealthItems(health.Tiers.Active.Items)
	health.Tiers.Sleeping.Items = capHealthItems(health.Tiers.Sleeping.Items)
	health.Tiers.AtRisk.Items = capHealthItems(health.Tiers.AtRisk.Items)
	health.Tiers.Lost.Items = capHealthItems(health.Tiers.Lost.Items)
	health.Tiers.New.Items = capHealthItems(health.Tiers.New.Items)
	return health
}

// buildHealthItem 计算单客户健康度事实（节奏/基线/ratio/双金额）。
func buildHealthItem(today time.Time, params settings.HealthTiers, entry *healthCustomerFacts) CustomerHealthItem {
	item := CustomerHealthItem{
		CustomerID:    entry.customer.ID,
		DisplayName:   entry.customer.DisplayName,
		Channel:       entry.customer.Channel,
		CreatedAt:     entry.customer.CreatedAt,
		SettledLTV:    entry.settledLTV,
		UnsettledPaid: entry.unsettledPaid,
		Baseline:      HealthBaselineNone,
	}
	if len(entry.shotDates) == 0 {
		return item
	}
	sort.Slice(entry.shotDates, func(i, j int) bool { return entry.shotDates[i].Before(entry.shotDates[j]) })
	item.Shots = len(entry.shotDates)
	lastShot := entry.shotDates[len(entry.shotDates)-1]
	sinceDays := clock.DaysBetween(lastShot, today)
	item.SinceDays = &sinceDays

	if item.Shots >= 2 {
		gapTotal := 0
		for i := 1; i < len(entry.shotDates); i++ {
			gap := clock.DaysBetween(entry.shotDates[i-1], entry.shotDates[i])
			if gap < healthSameDayGapFloorDays {
				gap = healthSameDayGapFloorDays
			}
			gapTotal += gap
		}
		cadence := int(math.Round(float64(gapTotal) / float64(item.Shots-1)))
		item.CadenceDays = &cadence
		item.Baseline = HealthBaselinePersonal
	} else {
		cadence := params.FallbackCadenceDays
		item.CadenceDays = &cadence
		item.Baseline = HealthBaselineFallback
	}
	ratio := roundToTwo(float64(sinceDays) / float64(*item.CadenceDays))
	item.Ratio = &ratio
	return item
}

// itemTier 判层：新客（Baseline=none，无拍摄记录）直入 new；其余按 ratio 阈值
// （≤ 含边界）。
func itemTier(item CustomerHealthItem, params settings.HealthTiers) string {
	if item.Baseline == HealthBaselineNone {
		return "new"
	}
	ratio := 0.0
	if item.Ratio != nil {
		ratio = *item.Ratio
	}
	switch {
	case ratio <= params.SleepingRatio:
		return "active"
	case ratio <= params.AtRiskRatio:
		return "sleeping"
	case ratio <= params.LostRatio:
		return "at_risk"
	default:
		return "lost"
	}
}

// sortHealthItems 层内排序：at_risk/lost 挽回优先看价值（settled DESC → ratio DESC），
// 其余层按超节奏倍数（ratio DESC），new 无 ratio 按建档新近（created DESC）；tie 用 id ASC 稳定。
func sortHealthItems(items []CustomerHealthItem, tier string) {
	ratioOf := func(item CustomerHealthItem) float64 {
		if item.Ratio != nil {
			return *item.Ratio
		}
		return 0
	}
	valueFirst := tier == "at_risk" || tier == "lost"
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if valueFirst && a.SettledLTV != b.SettledLTV {
			return a.SettledLTV > b.SettledLTV
		}
		if tier == "new" {
			if !a.CreatedAt.Equal(b.CreatedAt) {
				return a.CreatedAt.After(b.CreatedAt)
			}
			return a.CustomerID < b.CustomerID
		}
		if ratioOf(a) != ratioOf(b) {
			return ratioOf(a) > ratioOf(b)
		}
		return a.CustomerID < b.CustomerID
	})
}

// capHealthItems 截断到契约上限（count 已先记全量数）。
func capHealthItems(items []CustomerHealthItem) []CustomerHealthItem {
	if len(items) <= healthTierItemCap {
		return items
	}
	return items[:healthTierItemCap]
}

func buildDeliveryQueue(today time.Time, items []orderdomain.ListItem) DeliveryQueue {
	queue := DeliveryQueue{Items: make([]DeliveryQueueItem, 0, len(items))}
	for _, item := range items {
		entry := DeliveryQueueItem{Order: item}
		if item.DeliveryDueAt != nil {
			daysLeft := clock.DaysBetween(today, *item.DeliveryDueAt)
			entry.DaysLeft = &daysLeft
			entry.Overdue = daysLeft < 0
		}
		queue.Items = append(queue.Items, entry)
	}
	queue.Count = len(queue.Items)
	return queue
}

func buildWaterfall(window V2Window, facts V2Facts) RevenueWaterfall {
	waterfall := RevenueWaterfall{
		ConfirmedCurrent:  facts.ConfirmedCurrent,
		ConfirmedPrevious: facts.ConfirmedPrevious,
		ReceivableCount:   facts.Unpaid.Count,
		PipelineTotal:     facts.PipelineTotal,
		PipelineCount:     facts.PipelineCount,
		CashReceived30d:   facts.Cash30d,
	}
	if facts.ConfirmedPrevious > 0 {
		ratio := roundRatio(float64(facts.ConfirmedCurrent-facts.ConfirmedPrevious) / float64(facts.ConfirmedPrevious))
		waterfall.ConfirmedChangeRatio = &ratio
	}
	if facts.ConfirmedCurrentCount > 0 {
		value := facts.ConfirmedCurrent / facts.ConfirmedCurrentCount
		waterfall.AverageOrderValue = &value
	}
	for _, item := range facts.Unpaid.Items {
		if item.OutstandingAmount != nil {
			waterfall.ReceivableTotal += *item.OutstandingAmount
		}
		if item.DeliveredAt != nil && waterfall.OldestReceivableAgeDays == nil {
			age := clock.DaysBetween(clock.DateOnly(*item.DeliveredAt), window.Today)
			waterfall.OldestReceivableAgeDays = &age
		}
	}
	customerShots := make(map[string]int, len(facts.RepeatWindowShots))
	for _, customerID := range facts.RepeatWindowShots {
		customerShots[customerID]++
	}
	repeatCustomers := 0
	for _, count := range customerShots {
		if count >= 2 {
			repeatCustomers++
		}
	}
	if len(customerShots) > 0 {
		ratio := roundRatio(float64(repeatCustomers) / float64(len(customerShots)))
		waterfall.RepeatCustomerRatio90d = &ratio
	}
	return waterfall
}

func buildChannelMatrix(rows []MatrixSourceRow) ChannelMatrix {
	type aggregate struct {
		channel      string
		portrait     int
		cosplay      int
		other        int
		unattributed int
		total        int
		orderCount   int
		customers    map[string]struct{}
	}
	byChannel := make(map[string]*aggregate)
	grand := 0
	for _, row := range rows {
		entry, ok := byChannel[row.ChannelSnapshot]
		if !ok {
			entry = &aggregate{channel: row.ChannelSnapshot, customers: make(map[string]struct{})}
			byChannel[row.ChannelSnapshot] = entry
		}
		switch {
		case row.ShootTypeSnapshot == nil:
			entry.unattributed += row.Price
		case *row.ShootTypeSnapshot == "portrait":
			entry.portrait += row.Price
		case *row.ShootTypeSnapshot == "cosplay":
			entry.cosplay += row.Price
		default:
			entry.other += row.Price
		}
		entry.total += row.Price
		entry.orderCount++
		entry.customers[row.CustomerID] = struct{}{}
		grand += row.Price
	}
	entries := make([]*aggregate, 0, len(byChannel))
	for _, entry := range byChannel {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].total != entries[j].total {
			return entries[i].total > entries[j].total
		}
		return entries[i].channel < entries[j].channel
	})
	matrix := ChannelMatrix{Rows: make([]ChannelMatrixRow, 0, len(entries)), GrandTotal: grand}
	for _, entry := range entries {
		matrix.Rows = append(matrix.Rows, ChannelMatrixRow{
			Channel:       entry.channel,
			Portrait:      entry.portrait,
			Cosplay:       entry.cosplay,
			Other:         entry.other,
			Unattributed:  entry.unattributed,
			Total:         entry.total,
			OrderCount:    entry.orderCount,
			CustomerCount: len(entry.customers),
		})
	}
	return matrix
}

// daySlotsFromListItems 把档期列表项转成 openings/利用率算法的最小投影
// （非 shoot 档期无订单状态 → nil，不影响取消判定）。
func daySlotsFromListItems(items []schedule.ListItem) []schedule.DaySlot {
	slots := make([]schedule.DaySlot, 0, len(items))
	for _, item := range items {
		var orderStatus *string
		if item.OrderStatus != "" {
			status := item.OrderStatus
			orderStatus = &status
		}
		slots = append(slots, schedule.DaySlot{
			ID:          item.ID,
			Type:        item.Type,
			StartAt:     item.StartAt,
			EndAt:       item.EndAt,
			OrderStatus: orderStatus,
		})
	}
	return slots
}

// availabilityPlanFromSettings 把 settings 可约偏好转成算法输入（ISO 星期 1=周一…7=周日）。
func availabilityPlanFromSettings(value settings.ScheduleAvailability) schedule.AvailabilityPlan {
	plan := schedule.AvailabilityPlan{
		Weekly:            make(map[int]*schedule.DailyWindow, 7),
		MinOpeningMinutes: value.MinOpeningMinutes,
	}
	convert := func(iso int, window *settings.ScheduleAvailabilityWindow) {
		if window == nil {
			plan.Weekly[iso] = nil
			return
		}
		plan.Weekly[iso] = &schedule.DailyWindow{Start: window.Start, End: window.End}
	}
	convert(1, value.Weekly.Monday)
	convert(2, value.Weekly.Tuesday)
	convert(3, value.Weekly.Wednesday)
	convert(4, value.Weekly.Thursday)
	convert(5, value.Weekly.Friday)
	convert(6, value.Weekly.Saturday)
	convert(7, value.Weekly.Sunday)
	return plan
}

// roundRatio 统一派生比率精度：4 位小数（避免浮点噪声进契约）。
func roundRatio(value float64) float64 {
	return math.Round(value*10000) / 10000
}

// roundToTwo 健康度 ratio 精度：两位小数（与利用率口径同级，§4.3）。
func roundToTwo(value float64) float64 {
	return math.Round(value*100) / 100
}
