package schedule

import (
	"testing"
	"time"
)

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

func dateOnly(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse date %s: %v", s, err)
	}
	return d
}

// TestResolveDayWindowDSTCompatible 锁住工作窗端点的 Temporal 'compatible' 语义：
// 不存在时刻（春跳空档）取跳跃后的较晚解释，歧义时刻（秋回重叠）取较早解释。
// Go 原生 time.Date 对 gap 会给跳跃前 offset 的较早时刻（与前端不一致），必须显式解析。
func TestResolveDayWindowDSTCompatible(t *testing.T) {
	ny := mustLocation(t, "America/New_York")
	gapDay := dateOnly(t, "2026-03-08")
	foldDay := dateOnly(t, "2026-11-01")

	// 春跳日：01:15 存在（EST）=06:15Z；02:45 不存在 → 推移到 03:45 EDT =07:45Z。
	window, err := ResolveDayWindow(gapDay, ny, AvailabilityPlan{Weekly: map[int]*DailyWindow{
		7: {Start: "01:15", End: "02:45"},
	}})
	if err != nil || window == nil {
		t.Fatalf("gap day resolve: window=%v err=%v", window, err)
	}
	if !window.StartAt.Equal(time.Date(2026, 3, 8, 6, 15, 0, 0, time.UTC)) {
		t.Fatalf("gap day start = %s, want 06:15Z", window.StartAt.UTC().Format(time.RFC3339))
	}
	if !window.EndAt.Equal(time.Date(2026, 3, 8, 7, 45, 0, 0, time.UTC)) {
		t.Fatalf("gap day end = %s, want 07:45Z（Temporal compatible：gap 推移到跳跃后）", window.EndAt.UTC().Format(time.RFC3339))
	}

	// 秋回日：01:15 歧义 → 较早（EDT）=05:15Z；02:45 稳定（EST）=07:45Z。
	window, err = ResolveDayWindow(foldDay, ny, AvailabilityPlan{Weekly: map[int]*DailyWindow{
		7: {Start: "01:15", End: "02:45"},
	}})
	if err != nil || window == nil {
		t.Fatalf("fold day resolve: window=%v err=%v", window, err)
	}
	if !window.StartAt.Equal(time.Date(2026, 11, 1, 5, 15, 0, 0, time.UTC)) {
		t.Fatalf("fold day start = %s, want 05:15Z（歧义取较早）", window.StartAt.UTC().Format(time.RFC3339))
	}
	if !window.EndAt.Equal(time.Date(2026, 11, 1, 7, 45, 0, 0, time.UTC)) {
		t.Fatalf("fold day end = %s, want 07:45Z", window.EndAt.UTC().Format(time.RFC3339))
	}
}

func TestResolveDayWindowUnconfiguredAndInvalid(t *testing.T) {
	monday := dateOnly(t, "2026-07-06")

	// 未配置该周日 → (nil, nil)，不算错误（时区仅影响时刻解析，未配置分支与 loc 无关，
	// 取固定 loc 保持用例聚焦）。
	window, err := ResolveDayWindow(monday, time.UTC, AvailabilityPlan{Weekly: map[int]*DailyWindow{
		7: {Start: "10:00", End: "19:00"},
	}})
	if err != nil || window != nil {
		t.Fatalf("unconfigured day: window=%v err=%v, want nil/nil", window, err)
	}

	// HH:MM 格式非法 → 错误消息与前端 model.ts 一致。
	_, err = ResolveDayWindow(monday, time.UTC, AvailabilityPlan{Weekly: map[int]*DailyWindow{
		1: {Start: "9:00", End: "19:00"},
	}})
	if err == nil || err.Error() != "可约时段或账号时区无效" {
		t.Fatalf("invalid format err = %v", err)
	}

	// 结束不晚于开始 → DST 映射错误消息与前端一致。
	_, err = ResolveDayWindow(monday, time.UTC, AvailabilityPlan{Weekly: map[int]*DailyWindow{
		1: {Start: "10:00", End: "10:00"},
	}})
	if err == nil || err.Error() != "DST 映射后的可约结束时间不晚于开始时间" {
		t.Fatalf("end<=start err = %v", err)
	}
}

// TestDayOpeningsCancelMergeBoundary：取消 shoot 不占用；相邻档期合并；空档恰等于最小时长计入。
func TestDayOpeningsCancelMergeBoundary(t *testing.T) {
	day := dateOnly(t, "2026-07-06")
	window := &DayWindow{
		Date:    day,
		StartAt: time.Date(2026, 7, 6, 2, 0, 0, 0, time.UTC),  // 10:00 SHA
		EndAt:   time.Date(2026, 7, 6, 11, 0, 0, 0, time.UTC), // 19:00 SHA
	}
	cancelled := "cancelled"
	slots := []DaySlot{
		{ID: "a", Type: TypeShoot, StartAt: time.Date(2026, 7, 6, 4, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC), OrderStatus: nil},
		{ID: "b", Type: TypeHold, StartAt: time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC), OrderStatus: nil},
		{ID: "c", Type: TypeShoot, StartAt: time.Date(2026, 7, 6, 8, 30, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 6, 9, 30, 0, 0, time.UTC), OrderStatus: &cancelled},
	}
	openings := DayOpenings(day, window, slots, 120, time.UTC)
	if len(openings) != 2 {
		t.Fatalf("openings = %d, want 2（10:00–12:00 恰好 120 分钟计入；12:00–16:00 被 a+b 相邻合并占用；取消 c 不占用 → 16:00–19:00 共 180 分钟计入）", len(openings))
	}
	first, second := openings[0], openings[1]
	if !first.StartAt.Equal(window.StartAt) || !first.EndAt.Equal(time.Date(2026, 7, 6, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("first opening = [%s, %s], want [10:00, 12:00] SHA", first.StartAt.UTC().Format(time.RFC3339), first.EndAt.UTC().Format(time.RFC3339))
	}
	if !second.StartAt.Equal(time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC)) || !second.EndAt.Equal(window.EndAt) {
		t.Fatalf("second opening = [%s, %s], want [16:00, 19:00] SHA", second.StartAt.UTC().Format(time.RFC3339), second.EndAt.UTC().Format(time.RFC3339))
	}
}

// TestProjectSlotToDays：跨午夜档期按本地日分段裁剪，区间外日期不投影。
func TestProjectSlotToDays(t *testing.T) {
	sha := mustLocation(t, "Asia/Shanghai")
	slot := DaySlot{
		ID:      "x",
		Type:    TypeShoot,
		StartAt: time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC), // 07-08 18:00 SHA
		EndAt:   time.Date(2026, 7, 9, 3, 0, 0, 0, time.UTC),  // 07-09 11:00 SHA
	}
	projections := ProjectSlotToDays(slot, dateOnly(t, "2026-07-06"), dateOnly(t, "2026-07-12"), sha)
	if len(projections) != 2 {
		t.Fatalf("projections = %d, want 2", len(projections))
	}
	first, second := projections[0], projections[1]
	// 第一段：07-08 18:00 → 当日 24:00（= 07-08T16:00Z）；第二段：07-09 00:00 → 11:00。
	if !first.StartAt.Equal(time.Date(2026, 7, 8, 10, 0, 0, 0, time.UTC)) ||
		!first.EndAt.Equal(time.Date(2026, 7, 8, 16, 0, 0, 0, time.UTC)) {
		t.Fatalf("first projection = [%s, %s]", first.StartAt.UTC(), first.EndAt.UTC())
	}
	if !second.StartAt.Equal(time.Date(2026, 7, 8, 16, 0, 0, 0, time.UTC)) ||
		!second.EndAt.Equal(time.Date(2026, 7, 9, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("second projection = [%s, %s]", second.StartAt.UTC(), second.EndAt.UTC())
	}
}

// TestMonthOverviewDenominatorAndNumerator：分母不含未配置日；busy 不进分子；
// 取消 shoot 不计 shoot_count；重叠 shoot 记 conflict 日；分母 0 → utilization nil。
func TestMonthOverviewDenominatorAndNumerator(t *testing.T) {
	sha := mustLocation(t, "Asia/Shanghai")
	// 2026-07-06 是周一；只配置周一 10:00–19:00（540 分钟）。
	plan := AvailabilityPlan{
		Weekly:            map[int]*DailyWindow{1: {Start: "10:00", End: "19:00"}},
		MinOpeningMinutes: 120,
	}
	cancelled := "cancelled"
	slots := []DaySlot{
		// 周一 12:00–14:00 shoot → 分子 120 分钟。
		{ID: "m1", Type: TypeShoot, StartAt: time.Date(2026, 7, 6, 4, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 6, 6, 0, 0, 0, time.UTC), OrderStatus: nil},
		// 周一 07-13 重叠双 shoot 12:00–14:00 / 13:00–15:00 → conflict 日，分子 180 分钟。
		{ID: "m2", Type: TypeShoot, StartAt: time.Date(2026, 7, 13, 4, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 13, 6, 0, 0, 0, time.UTC), OrderStatus: nil},
		{ID: "m3", Type: TypeShoot, StartAt: time.Date(2026, 7, 13, 5, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 13, 7, 0, 0, 0, time.UTC), OrderStatus: nil},
		// 周一 07-20 busy 10:00–18:00：不进分子。
		{ID: "m4", Type: TypeBusy, StartAt: time.Date(2026, 7, 20, 2, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC), OrderStatus: nil},
		// 周一 07-27 取消 shoot：不进分子、不计 shoot_count。
		{ID: "m5", Type: TypeShoot, StartAt: time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC), EndAt: time.Date(2026, 7, 27, 5, 0, 0, 0, time.UTC), OrderStatus: &cancelled},
	}
	overview, errs := MonthOverview(slots, dateOnly(t, "2026-07-01"), 31, sha, plan)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	// 分母 = 4 个配置周一 × 540 = 2160；分子按 Calendar v2 既有口径逐投影累加、不合并重叠：
	// m1 120 + m2 120 + m3 120（13:00–14:00 重叠小时与 TS 一样双计）= 360 → 16.67%。
	if overview.Utilization == nil || *overview.Utilization != 16.67 {
		t.Fatalf("utilization = %v, want 16.67", overview.Utilization)
	}
	if overview.ShootCount != 3 {
		t.Fatalf("shoot_count = %d, want 3（取消 m5 不计）", overview.ShootCount)
	}
	if overview.ConflictDays != 1 {
		t.Fatalf("conflict_days = %d, want 1", overview.ConflictDays)
	}
	if overview.OpenDays != 3 {
		t.Fatalf("open_days = %d, want 3（07-20 busy 吞掉空档后 18:00–19:00 仅 60 分钟 <120）", overview.OpenDays)
	}

	// 全月无配置日 → 分母 0 → utilization nil。
	empty, errs := MonthOverview(nil, dateOnly(t, "2026-07-01"), 31, sha, AvailabilityPlan{Weekly: map[int]*DailyWindow{}})
	if len(errs) != 0 || empty.Utilization != nil {
		t.Fatalf("empty overview = %+v errs=%v, want utilization nil", empty, errs)
	}
}
