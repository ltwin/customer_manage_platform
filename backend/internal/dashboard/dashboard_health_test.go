package dashboard

import (
	"testing"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/settings"
)

// healthTestDate 构造 date-only 值（节奏/间隔全部按自然日计）。
func healthTestDate(month time.Month, day int) time.Time {
	return time.Date(2026, month, day, 0, 0, 0, 0, time.UTC)
}

func healthTestDateFull(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func defaultHealthParams() settings.HealthTiers {
	return settings.HealthTiers{
		SleepingRatio:       1.2,
		AtRiskRatio:         2,
		LostRatio:           3.5,
		FallbackCadenceDays: 120,
	}
}

func healthCustomer(id string, createdYear int, createdMonth time.Month, createdDay int) HealthCustomerRow {
	return HealthCustomerRow{
		ID:          id,
		DisplayName: id,
		Channel:     "douyin",
		CreatedAt:   healthTestDateFull(createdYear, createdMonth, createdDay),
	}
}

func healthShot(customerID string, year int, month time.Month, day int) HealthOrderRow {
	date := healthTestDateFull(year, month, day)
	return HealthOrderRow{CustomerID: customerID, ShotDate: &date}
}

// TestBuildCustomerHealthTiers 覆盖五层判定：个人节奏（active/sleeping）与通用基线
// fallback（at_risk/lost）、新客层；ratio 两位小数。
func TestBuildCustomerHealthTiers(t *testing.T) {
	today := healthTestDate(8, 24)
	customers := []HealthCustomerRow{
		healthCustomer("cus-active", 2026, 7, 1),
		healthCustomer("cus-sleeping", 2026, 6, 1),
		healthCustomer("cus-atrisk", 2025, 10, 1),
		healthCustomer("cus-lost", 2025, 4, 1),
		healthCustomer("cus-new", 2026, 8, 1),
	}
	orders := []HealthOrderRow{
		// active：间隔 16 天（07-25→08-10），since 14 → ratio 0.88
		healthShot("cus-active", 2026, 7, 25),
		healthShot("cus-active", 2026, 8, 10),
		// sleeping：间隔 30 天，since 54 → ratio 1.8
		healthShot("cus-sleeping", 2026, 6, 1),
		healthShot("cus-sleeping", 2026, 7, 1),
		// at_risk（fallback）：仅 1 拍 2025-11-01，since 296 → 296/120 = 2.47
		healthShot("cus-atrisk", 2025, 11, 1),
		// lost（fallback）：1 拍 2025-05-01，since 480 → 480/120 = 4
		healthShot("cus-lost", 2025, 5, 1),
	}
	health := buildCustomerHealth(today, defaultHealthParams(), customers, orders)

	if health.Total != 5 {
		t.Fatalf("total = %d, want 5", health.Total)
	}
	if health.Tiers.Active.Count != 1 || health.Tiers.Sleeping.Count != 1 ||
		health.Tiers.AtRisk.Count != 1 || health.Tiers.Lost.Count != 1 || health.Tiers.New.Count != 1 {
		t.Fatalf("tier counts = active %d / sleeping %d / at_risk %d / lost %d / new %d, want 1/1/1/1/1",
			health.Tiers.Active.Count, health.Tiers.Sleeping.Count, health.Tiers.AtRisk.Count,
			health.Tiers.Lost.Count, health.Tiers.New.Count)
	}

	active := health.Tiers.Active.Items[0]
	if active.CustomerID != "cus-active" || active.Baseline != HealthBaselinePersonal {
		t.Fatalf("active item = %+v, want cus-active personal baseline", active)
	}
	if active.SinceDays == nil || *active.SinceDays != 14 {
		t.Fatalf("active since_days = %v, want 14", active.SinceDays)
	}
	if active.CadenceDays == nil || *active.CadenceDays != 16 {
		t.Fatalf("active cadence_days = %v, want 16", active.CadenceDays)
	}
	if active.Ratio == nil || *active.Ratio != 0.88 {
		t.Fatalf("active ratio = %v, want 0.88", active.Ratio)
	}
	if active.Shots != 2 {
		t.Fatalf("active shots = %d, want 2", active.Shots)
	}

	atRisk := health.Tiers.AtRisk.Items[0]
	if atRisk.CustomerID != "cus-atrisk" || atRisk.Baseline != HealthBaselineFallback {
		t.Fatalf("at_risk item = %+v, want cus-atrisk fallback baseline", atRisk)
	}
	if atRisk.CadenceDays == nil || *atRisk.CadenceDays != 120 {
		t.Fatalf("at_risk cadence_days = %v, want 120 (fallback)", atRisk.CadenceDays)
	}
	if atRisk.Ratio == nil || *atRisk.Ratio != 2.47 {
		t.Fatalf("at_risk ratio = %v, want 2.47", atRisk.Ratio)
	}

	lost := health.Tiers.Lost.Items[0]
	if lost.CustomerID != "cus-lost" || lost.Ratio == nil || *lost.Ratio != 4 {
		t.Fatalf("lost item = %+v, want cus-lost ratio 4", lost)
	}

	newItem := health.Tiers.New.Items[0]
	if newItem.CustomerID != "cus-new" || newItem.Baseline != HealthBaselineNone {
		t.Fatalf("new item = %+v, want cus-new baseline none", newItem)
	}
	if newItem.SinceDays != nil || newItem.CadenceDays != nil || newItem.Ratio != nil {
		t.Fatalf("new item derived fields must be nil, got %+v", newItem)
	}
	if newItem.Shots != 0 {
		t.Fatalf("new shots = %d, want 0", newItem.Shots)
	}
}

// TestBuildCustomerHealthSameDayFloor：同日多单间隔按 7 天下限兜底，
// 参与 cadence 均值（[7,31] → 19），不除零不失真。
func TestBuildCustomerHealthSameDayFloor(t *testing.T) {
	today := healthTestDate(8, 24)
	customers := []HealthCustomerRow{healthCustomer("cus", 2026, 5, 1)}
	orders := []HealthOrderRow{
		healthShot("cus", 2026, 6, 1),
		healthShot("cus", 2026, 6, 1), // 同日加急件
		healthShot("cus", 2026, 7, 2),
	}
	health := buildCustomerHealth(today, defaultHealthParams(), customers, orders)
	item := health.Tiers.AtRisk.Items[0]
	if item.CadenceDays == nil || *item.CadenceDays != 19 {
		t.Fatalf("cadence_days = %v, want 19 (mean of [7,31])", item.CadenceDays)
	}
	if item.Ratio == nil || *item.Ratio != 2.79 {
		t.Fatalf("ratio = %v, want 2.79 (53/19)", item.Ratio)
	}
}

// TestBuildCustomerHealthMoney：行金额双口径——settled_ltv 只计结清订单 price
// （NULL 跳过）；unsettled_paid 只计未结清订单 amount_paid（NULL→0）。
func TestBuildCustomerHealthMoney(t *testing.T) {
	today := healthTestDate(8, 24)
	price := func(v int) *int { return &v }
	paid := func(v int) *int { return &v }
	customers := []HealthCustomerRow{healthCustomer("cus", 2026, 1, 1)}
	orders := []HealthOrderRow{
		{CustomerID: "cus", ShotDate: ptrHealthDate(2026, 8, 1), Price: price(50000), BalancePaid: true},
		{CustomerID: "cus", ShotDate: ptrHealthDate(2026, 8, 8), BalancePaid: true}, // price NULL 跳过
		{CustomerID: "cus", ShotDate: ptrHealthDate(2026, 8, 15), Price: price(80000), BalancePaid: false, AmountPaid: paid(30000)},
		{CustomerID: "cus", ShotDate: ptrHealthDate(2026, 8, 22), Price: price(80000), BalancePaid: false}, // amount NULL→0
	}
	health := buildCustomerHealth(today, defaultHealthParams(), customers, orders)
	item := health.Tiers.Active.Items[0]
	if item.SettledLTV != 50000 {
		t.Fatalf("settled_ltv = %d, want 50000", item.SettledLTV)
	}
	if item.UnsettledPaid != 30000 {
		t.Fatalf("unsettled_paid = %d, want 30000", item.UnsettledPaid)
	}
	if item.Shots != 4 {
		t.Fatalf("shots = %d, want 4", item.Shots)
	}
}

// TestBuildCustomerHealthOrdering：at_risk/lost 按 settled_ltv DESC → ratio DESC；
// new 按 created_at DESC；tie 一律 customer_id ASC。
func TestBuildCustomerHealthOrdering(t *testing.T) {
	today := healthTestDate(8, 24)
	price := func(v int) *int { return &v }
	customers := []HealthCustomerRow{
		healthCustomer("cus-b", 2026, 8, 1),
		healthCustomer("cus-a", 2026, 8, 3),
		healthCustomer("cus-c", 2026, 8, 2),
		healthCustomer("cus-n1", 2026, 8, 10),
		healthCustomer("cus-n2", 2026, 8, 1),
	}
	orders := []HealthOrderRow{
		// 三人都是 fallback 基线（仅 1 拍），since 302/282/266 → ratio 2.52/2.35/2.22 全落 at_risk
		{CustomerID: "cus-a", ShotDate: ptrHealthDate(2025, 10, 26), Price: price(50000), BalancePaid: true},
		{CustomerID: "cus-b", ShotDate: ptrHealthDate(2025, 11, 15), Price: price(50000), BalancePaid: true},
		{CustomerID: "cus-c", ShotDate: ptrHealthDate(2025, 12, 1), Price: price(30000), BalancePaid: true},
	}
	health := buildCustomerHealth(today, defaultHealthParams(), customers, orders)
	if health.Tiers.AtRisk.Count != 3 || health.Tiers.New.Count != 2 {
		t.Fatalf("at_risk %d / new %d, want 3/2", health.Tiers.AtRisk.Count, health.Tiers.New.Count)
	}
	gotOrder := []string{
		health.Tiers.AtRisk.Items[0].CustomerID,
		health.Tiers.AtRisk.Items[1].CustomerID,
		health.Tiers.AtRisk.Items[2].CustomerID,
	}
	if gotOrder[0] != "cus-a" || gotOrder[1] != "cus-b" || gotOrder[2] != "cus-c" {
		t.Fatalf("at_risk order = %v, want [cus-a cus-b cus-c] (settled DESC → ratio DESC)", gotOrder)
	}
	if health.Tiers.AtRisk.Items[0].Ratio == nil || *health.Tiers.AtRisk.Items[0].Ratio != 2.52 {
		t.Fatalf("cus-a ratio = %v, want 2.52", health.Tiers.AtRisk.Items[0].Ratio)
	}
	newOrder := []string{health.Tiers.New.Items[0].CustomerID, health.Tiers.New.Items[1].CustomerID}
	if newOrder[0] != "cus-n1" || newOrder[1] != "cus-n2" {
		t.Fatalf("new order = %v, want [cus-n1 cus-n2] (created_at DESC)", newOrder)
	}
}

// TestBuildCustomerHealthCapFifty：单层 items 上限 50 行，count 仍为全量数。
func TestBuildCustomerHealthCapFifty(t *testing.T) {
	today := healthTestDate(8, 24)
	customers := make([]HealthCustomerRow, 0, 52)
	orders := make([]HealthOrderRow, 0, 52)
	for i := 0; i < 52; i++ {
		id := fmtCustomerID(i)
		customers = append(customers, healthCustomer(id, 2026, 1, 1))
		orders = append(orders, healthShot(id, 2025, 1, 1)) // since 600 → ratio 5 → lost
	}
	health := buildCustomerHealth(today, defaultHealthParams(), customers, orders)
	if health.Tiers.Lost.Count != 52 {
		t.Fatalf("lost count = %d, want 52", health.Tiers.Lost.Count)
	}
	if len(health.Tiers.Lost.Items) != healthTierItemCap {
		t.Fatalf("lost items len = %d, want cap %d", len(health.Tiers.Lost.Items), healthTierItemCap)
	}
	if health.Total != 52 {
		t.Fatalf("total = %d, want 52", health.Total)
	}
}

func ptrHealthDate(year int, month time.Month, day int) *time.Time {
	value := healthTestDateFull(year, month, day)
	return &value
}

func fmtCustomerID(i int) string {
	// 稳定字典序：两位补零。
	return "cus-" + string(rune('0'+i/10)) + string(rune('0'+i%10))
}
