// openings.go 是「可约空档 / 档期利用率」的服务端权威实现（dashboard-v2-redesign DEC-9）。
//
// 语义与前端 TS 实现（frontend/src/pages/calendar/model.ts、components/schedule/timezone.ts）
// 通过共享 golden fixtures（api/golden/openings/）逐字段对拍；双实现并存期两端口径一致，
// Calendar 前端切换为消费服务端结果后本实现成为唯一权威。禁止单侧改语义不重生成 fixtures。

package schedule

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"time"
)

// orderStatusCancelled 是订单状态契约枚举值（api/openapi.yaml Order.status）。
// 按值比较而不 import order 域，保持 schedule 无域间依赖（compound cross-domain-read-model）。
const orderStatusCancelled = "cancelled"

// AvailabilityPlan 是空档/利用率算法的账号可约偏好输入；由调用方从
// settings.ScheduleAvailability 转换（键为 ISO 星期：1=周一…7=周日；nil 值=当日停用）。
type AvailabilityPlan struct {
	Weekly            map[int]*DailyWindow
	MinOpeningMinutes int
}

// DailyWindow 是 "HH:MM" 起止的当日工作窗声明。
type DailyWindow struct {
	Start string
	End   string
}

// DayWindow 是某本地日工作窗解析成瞬时后的结果；Start/End 为账号时区本地 HH:MM
// （按解析后的瞬时取本地墙钟，DST 日与前端展示一致，而非回显配置串）。
type DayWindow struct {
	Date    time.Time // date-only（UTC 午夜）
	Start   string
	End     string
	StartAt time.Time
	EndAt   time.Time
}

// Opening 是工作窗内一段 ≥ 最小可约时长的空档；Start/End 为本地 HH:MM。
type Opening struct {
	Date    time.Time // date-only（UTC 午夜）
	Start   string
	End     string
	StartAt time.Time
	EndAt   time.Time
}

// DaySlot 是参与空档/利用率计算的档期最小投影。
type DaySlot struct {
	ID          string
	Type        string // shoot | hold | busy
	StartAt     time.Time
	EndAt       time.Time
	OrderStatus *string
}

// IsCancelledShoot 与前端 isCancelledShoot 同源：shoot 且引用订单已取消。
// 取消档期不占空档、不计利用率分子（仍保留在时间轴上，归前端展示）。
func (s DaySlot) IsCancelledShoot() bool {
	return s.Type == TypeShoot && s.OrderStatus != nil && *s.OrderStatus == orderStatusCancelled
}

// DayProjection 是档期在某本地日的分段（半开区间，按日界裁剪）。
type DayProjection struct {
	SlotID    string
	Type      string
	Cancelled bool
	StartAt   time.Time
	EndAt     time.Time
}

// DayError 是某日工作窗解析失败的记录；错误日跳过空档与分母，不中断其余日期。
type DayError struct {
	Date    time.Time
	Message string
}

// MonthOverviewResult 是 DEC-2 口径的月度利用率概览（与 Calendar v2 月概览同值）。
type MonthOverviewResult struct {
	ShootCount   int
	HoldDays     int
	ConflictDays int
	OpenDays     int
	// Utilization：未取消 shoot+hold 与工作窗相交分钟 ÷ 工作窗分钟，两位小数、封顶 100；
	// 分母 0（无任何已配置工作日）→ nil。
	Utilization *float64
}

// 算法错误消息与前端 model.ts 的 resolveWorkingWindow 保持逐字一致（golden errors 面对拍）。
var (
	errAvailabilityInvalid = errors.New("可约时段或账号时区无效")
	errWindowNotAfterStart = errors.New("DST 映射后的可约结束时间不晚于开始时间")
)

// ResolveDayWindow 把 date（date-only）当日的工作窗声明解析成瞬时；未配置该日 → (nil, nil)。
// 墙钟时刻按 Temporal 'compatible' 语义解析（歧义取较早、不存在推移到跳跃后）；
// HH:MM 非法或映射后 end ≤ start → error。
func ResolveDayWindow(date time.Time, loc *time.Location, plan AvailabilityPlan) (*DayWindow, error) {
	window, ok := plan.Weekly[isoWeekday(date)]
	if !ok || window == nil {
		return nil, nil
	}
	startHour, startMinute, err := parseWallClock(window.Start)
	if err != nil {
		return nil, errAvailabilityInvalid
	}
	endHour, endMinute, err := parseWallClock(window.End)
	if err != nil {
		return nil, errAvailabilityInvalid
	}
	d := date.UTC()
	startAt := resolveLocalInstant(d.Year(), d.Month(), d.Day(), startHour, startMinute, loc)
	endAt := resolveLocalInstant(d.Year(), d.Month(), d.Day(), endHour, endMinute, loc)
	if !endAt.After(startAt) {
		return nil, errWindowNotAfterStart
	}
	return &DayWindow{
		Date:    dateOnlyUTC(date),
		Start:   startAt.In(loc).Format("15:04"),
		End:     endAt.In(loc).Format("15:04"),
		StartAt: startAt,
		EndAt:   endAt,
	}, nil
}

// DayOpenings 计算某日工作窗内 ≥ minOpeningMinutes 的空档：未取消档期（含 busy）
// 均占用，占用段先裁剪到窗口再合并；window 为 nil（未配置日）→ 空结果。
// loc 用于空档起止的本地 HH:MM 投影。
func DayOpenings(date time.Time, window *DayWindow, slots []DaySlot, minOpeningMinutes int, loc *time.Location) []Opening {
	openings := make([]Opening, 0)
	if window == nil {
		return openings
	}
	occupied := make([][2]time.Time, 0, len(slots))
	for _, slot := range slots {
		if slot.IsCancelledShoot() {
			continue
		}
		start := maxTime(slot.StartAt, window.StartAt)
		end := minTime(slot.EndAt, window.EndAt)
		if start.Before(end) {
			occupied = append(occupied, [2]time.Time{start, end})
		}
	}
	sort.Slice(occupied, func(i, j int) bool {
		if !occupied[i][0].Equal(occupied[j][0]) {
			return occupied[i][0].Before(occupied[j][0])
		}
		return occupied[i][1].Before(occupied[j][1])
	})
	merged := make([][2]time.Time, 0, len(occupied))
	for _, interval := range occupied {
		if len(merged) == 0 || interval[0].After(merged[len(merged)-1][1]) {
			merged = append(merged, interval)
			continue
		}
		merged[len(merged)-1][1] = maxTime(merged[len(merged)-1][1], interval[1])
	}
	appendOpening := func(start, end time.Time) {
		if end.Sub(start) < time.Duration(minOpeningMinutes)*time.Minute {
			return
		}
		openings = append(openings, Opening{
			Date:    dateOnlyUTC(date),
			Start:   start.In(loc).Format("15:04"),
			End:     end.In(loc).Format("15:04"),
			StartAt: start,
			EndAt:   end,
		})
	}
	cursor := window.StartAt
	for _, interval := range merged {
		appendOpening(cursor, interval[0])
		cursor = maxTime(cursor, interval[1])
	}
	appendOpening(cursor, window.EndAt)
	return openings
}

// ProjectSlotToDays 把档期投影到 [firstDay, lastDay]（date-only）范围内相交的本地日，
// 每段按日界半开裁剪；结束时刻恰为午夜时归属前一天（与 TS projectSlotToLocalDays 一致）。
func ProjectSlotToDays(slot DaySlot, firstDay, lastDay time.Time, loc *time.Location) []DayProjection {
	projections := make([]DayProjection, 0)
	if !slot.StartAt.Before(slot.EndAt) {
		return projections
	}
	first := localDateIn(slot.StartAt, loc)
	last := localDateIn(slot.EndAt.Add(-time.Nanosecond), loc)
	for cur := first; !cur.After(last); cur = cur.AddDate(0, 0, 1) {
		if cur.Before(firstDay) || cur.After(lastDay) {
			continue
		}
		dayStart := resolveLocalInstant(cur.UTC().Year(), cur.UTC().Month(), cur.UTC().Day(), 0, 0, loc)
		dayEnd := resolveLocalInstant(cur.UTC().Year(), cur.UTC().Month(), cur.UTC().Day()+1, 0, 0, loc)
		start := maxTime(slot.StartAt, dayStart)
		end := minTime(slot.EndAt, dayEnd)
		if !start.Before(end) {
			continue
		}
		projections = append(projections, DayProjection{
			SlotID:    slot.ID,
			Type:      slot.Type,
			Cancelled: slot.IsCancelledShoot(),
			StartAt:   start,
			EndAt:     end,
		})
	}
	return projections
}

// MonthOverview 计算账号本地月 [monthStart, monthStart+days) 的利用率概览（DEC-2）：
// 分母=各已配置工作日的工作窗分钟和（未配置/解析失败日不入），分子=未取消 shoot+hold
// 与工作窗相交分钟（busy 不计分子）；shoot_count 按档期 id 去重；conflict 日=当日存在
// 未取消档期两两相交；open 日=当日存在 ≥ min_opening_minutes 的空档。
func MonthOverview(slots []DaySlot, monthStart time.Time, days int, loc *time.Location, plan AvailabilityPlan) (MonthOverviewResult, []DayError) {
	result := MonthOverviewResult{}
	var dayErrors []DayError
	shootIDs := make(map[string]struct{})
	availableMinutes := 0.0
	utilizedMinutes := 0.0

	for offset := 0; offset < days; offset++ {
		date := monthStart.AddDate(0, 0, offset)
		projections := make([]DayProjection, 0)
		for _, slot := range slots {
			projections = append(projections, ProjectSlotToDays(slot, date, date, loc)...)
		}
		sort.Slice(projections, func(i, j int) bool {
			if !projections[i].StartAt.Equal(projections[j].StartAt) {
				return projections[i].StartAt.Before(projections[j].StartAt)
			}
			return projections[i].SlotID < projections[j].SlotID
		})

		holdToday := false
		for _, projection := range projections {
			if projection.Cancelled {
				continue
			}
			if projection.Type == TypeShoot {
				shootIDs[projection.SlotID] = struct{}{}
			}
			if projection.Type == TypeHold {
				holdToday = true
			}
		}
		if holdToday {
			result.HoldDays++
		}

		if markConflicts(projections) {
			result.ConflictDays++
		}

		window, err := ResolveDayWindow(date, loc, plan)
		if err != nil {
			dayErrors = append(dayErrors, DayError{Date: dateOnlyUTC(date), Message: err.Error()})
			continue
		}
		if window == nil {
			continue
		}
		availableMinutes += window.EndAt.Sub(window.StartAt).Minutes()
		for _, projection := range projections {
			if projection.Cancelled || (projection.Type != TypeShoot && projection.Type != TypeHold) {
				continue
			}
			start := maxTime(projection.StartAt, window.StartAt)
			end := minTime(projection.EndAt, window.EndAt)
			if start.Before(end) {
				utilizedMinutes += end.Sub(start).Minutes()
			}
		}
		if openings := DayOpenings(date, window, slots, plan.MinOpeningMinutes, loc); len(openings) > 0 {
			result.OpenDays++
		}
	}

	result.ShootCount = len(shootIDs)
	if availableMinutes > 0 {
		ratio := math.Min(100, utilizedMinutes/availableMinutes*100)
		value := roundToTwo(ratio)
		result.Utilization = &value
	}
	return result, dayErrors
}

// markConflicts 判断当日是否存在未取消档期两两相交（与 TS markConflicts 同序遍历：
// 按开始时刻排序后，右侧档期一旦不与左侧相交即可 break）。
func markConflicts(projections []DayProjection) bool {
	for left := 0; left < len(projections); left++ {
		if projections[left].Cancelled {
			continue
		}
		for right := left + 1; right < len(projections); right++ {
			if projections[right].Cancelled {
				continue
			}
			if !projections[right].StartAt.Before(projections[left].EndAt) {
				break
			}
			if projections[left].StartAt.Before(projections[right].EndAt) {
				return true
			}
		}
	}
	return false
}

// resolveLocalInstant 把本地墙钟时刻解析为瞬时，DST 语义与 Temporal 'compatible' 对齐：
// 歧义（秋回重叠）取较早解释，不存在（春跳空档）取较晚解释。
// Go 原生 time.Date 对 gap 会落到跳跃前 offset 的较早时刻，与前端不一致，故显式探测。
func resolveLocalInstant(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	wall := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	var valid []time.Time
	var invalid []time.Time
	seen := func(list []time.Time, candidate time.Time) bool {
		for _, existing := range list {
			if existing.Equal(candidate) {
				return true
			}
		}
		return false
	}
	for _, delta := range []time.Duration{0, -24 * time.Hour, 24 * time.Hour} {
		_, offset := wall.Add(delta).In(loc).Zone()
		candidate := wall.Add(-time.Duration(offset) * time.Second)
		back := candidate.In(loc)
		if back.Year() == year && back.Month() == month && back.Day() == day && back.Hour() == hour && back.Minute() == minute {
			if !seen(valid, candidate) {
				valid = append(valid, candidate)
			}
		} else if !seen(invalid, candidate) {
			invalid = append(invalid, candidate)
		}
	}
	if len(valid) > 0 {
		earliest := valid[0]
		for _, candidate := range valid[1:] {
			if candidate.Before(earliest) {
				earliest = candidate
			}
		}
		return earliest
	}
	// 不存在时刻：compatible 取较晚解释（跳跃后）。
	latest := invalid[0]
	for _, candidate := range invalid[1:] {
		if candidate.After(latest) {
			latest = candidate
		}
	}
	return latest
}

func parseWallClock(value string) (hour, minute int, err error) {
	if len(value) != 5 || value[2] != ':' {
		return 0, 0, errAvailabilityInvalid
	}
	hour, err = strconv.Atoi(value[0:2])
	if err != nil {
		return 0, 0, errAvailabilityInvalid
	}
	minute, err = strconv.Atoi(value[3:5])
	if err != nil {
		return 0, 0, errAvailabilityInvalid
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, errAvailabilityInvalid
	}
	return hour, minute, nil
}

// isoWeekday 返回 ISO 星期（1=周一…7=周日）。
func isoWeekday(date time.Time) int {
	return (int(date.Weekday())+6)%7 + 1
}

func dateOnlyUTC(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}

func localDateIn(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// DaysInMonth 返回 date-only 月起点所在自然月的天数。
func DaysInMonth(monthStart time.Time) int {
	next := monthStart.AddDate(0, 1, 0)
	return int(next.Sub(monthStart).Hours() / 24)
}

// roundToTwo 与 TS roundToTwo 同式（含 Number.EPSILON 预纠偏），保证 golden 对拍一致。
func roundToTwo(value float64) float64 {
	const numberEpsilon = 2.220446049250313e-16
	return math.Round((value+numberEpsilon)*100) / 100
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
