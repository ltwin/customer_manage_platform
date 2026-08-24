package schedule

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// DEC-9 golden 对拍（Go 侧）：共享 fixtures 位于 api/golden/openings/，
// 由前端 TS 实现（frontend/src/pages/calendar/model.ts + scripts/gen-openings-golden.ts）
// 生成。Go 权威实现必须与 TS 对同一批 fixtures 给出逐字段一致的结果；
// fixtures 变更须经生成脚本再生成，禁止手改期望值迁就实现。

const goldenFixturesDir = "../../../api/golden/openings"

type goldenWindowSpec struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type goldenSlot struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	StartAt     string  `json:"start_at"`
	EndAt       string  `json:"end_at"`
	OrderStatus *string `json:"order_status"`
}

type goldenDayExpected struct {
	Date          string            `json:"date"`
	WorkingWindow *goldenDayWindow  `json:"working_window"`
	Openings      []goldenOpeningEV `json:"openings"`
}

type goldenDayWindow struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type goldenOpeningEV struct {
	Date    string `json:"date"`
	Start   string `json:"start"`
	End     string `json:"end"`
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type goldenMonthOverview struct {
	ShootCount   int      `json:"shoot_count"`
	HoldDays     int      `json:"hold_days"`
	ConflictDays int      `json:"conflict_days"`
	OpenDays     int      `json:"open_days"`
	Utilization  *float64 `json:"utilization"`
}

type goldenFixture struct {
	Name         string `json:"name"`
	Timezone     string `json:"timezone"`
	Availability struct {
		Weekly            map[string]*goldenWindowSpec `json:"weekly"`
		MinOpeningMinutes int                          `json:"min_opening_minutes"`
	} `json:"availability"`
	Slots    []goldenSlot `json:"slots"`
	Dates    []string     `json:"dates"`
	Month    *string      `json:"month"`
	Expected struct {
		Days          []goldenDayExpected  `json:"days"`
		Errors        []goldenDayErrorEV   `json:"errors"`
		MonthOverview *goldenMonthOverview `json:"month_overview"`
	} `json:"expected"`
}

type goldenDayErrorEV struct {
	Date    string `json:"date"`
	Message string `json:"message"`
}

func loadGoldenFixtures(t *testing.T) []goldenFixture {
	t.Helper()
	entries, err := os.ReadDir(goldenFixturesDir)
	if err != nil {
		t.Fatalf("read golden fixtures dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".json" {
			names = append(names, entry.Name())
		}
	}
	// 必备用例名单：DST/月利用率等关键语义被误删且未重生成时，守护测试直接失败。
	for _, required := range []string{
		"basic-weekday.json", "dst-window-resolution.json", "openings-boundary.json", "month-utilization.json",
	} {
		found := false
		for _, name := range names {
			if name == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("golden fixture %s 缺失：关键语义用例不得删除，需求变更走 scripts/gen-openings-golden.ts 重生成", required)
		}
	}
	fixtures := make([]goldenFixture, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(goldenFixturesDir, name))
		if err != nil {
			t.Fatalf("read fixture %s: %v", name, err)
		}
		var fixture goldenFixture
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatalf("parse fixture %s: %v", name, err)
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures
}

func goldenPlan(f goldenFixture) AvailabilityPlan {
	plan := AvailabilityPlan{
		Weekly:            make(map[int]*DailyWindow, len(f.Availability.Weekly)),
		MinOpeningMinutes: f.Availability.MinOpeningMinutes,
	}
	for key, window := range f.Availability.Weekly {
		iso, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		if window == nil {
			plan.Weekly[iso] = nil
			continue
		}
		plan.Weekly[iso] = &DailyWindow{Start: window.Start, End: window.End}
	}
	return plan
}

func parseGoldenInstant(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse instant %s: %v", s, err)
	}
	return parsed
}

func TestOpeningsGoldenParityWithTS(t *testing.T) {
	for _, fixture := range loadGoldenFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			loc, err := time.LoadLocation(fixture.Timezone)
			if err != nil {
				t.Fatalf("load timezone: %v", err)
			}
			plan := goldenPlan(fixture)
			slots := make([]DaySlot, 0, len(fixture.Slots))
			for _, slot := range fixture.Slots {
				slots = append(slots, DaySlot{
					ID:          slot.ID,
					Type:        slot.Type,
					StartAt:     parseGoldenInstant(t, slot.StartAt),
					EndAt:       parseGoldenInstant(t, slot.EndAt),
					OrderStatus: slot.OrderStatus,
				})
			}

			days := make([]goldenDayExpected, 0, len(fixture.Dates))
			var dayErrors []goldenDayErrorEV
			for _, dateStr := range fixture.Dates {
				date, err := time.Parse("2006-01-02", dateStr)
				if err != nil {
					t.Fatalf("parse date %s: %v", dateStr, err)
				}
				window, err := ResolveDayWindow(date, loc, plan)
				if err != nil {
					dayErrors = append(dayErrors, goldenDayErrorEV{Date: dateStr, Message: err.Error()})
					days = append(days, goldenDayExpected{Date: dateStr, WorkingWindow: nil, Openings: []goldenOpeningEV{}})
					continue
				}
				expectedWindow := (*goldenDayWindow)(nil)
				if window != nil {
					expectedWindow = &goldenDayWindow{
						Start:   window.StartAt.In(loc).Format("15:04"),
						End:     window.EndAt.In(loc).Format("15:04"),
						StartAt: window.StartAt.UTC().Format(time.RFC3339),
						EndAt:   window.EndAt.UTC().Format(time.RFC3339),
					}
				}
				openings := DayOpenings(date, window, slots, plan.MinOpeningMinutes, loc)
				openingEVs := make([]goldenOpeningEV, 0, len(openings))
				for _, opening := range openings {
					openingEVs = append(openingEVs, goldenOpeningEV{
						Date:    opening.Date.Format("2006-01-02"),
						Start:   opening.StartAt.In(loc).Format("15:04"),
						End:     opening.EndAt.In(loc).Format("15:04"),
						StartAt: opening.StartAt.UTC().Format(time.RFC3339),
						EndAt:   opening.EndAt.UTC().Format(time.RFC3339),
					})
				}
				days = append(days, goldenDayExpected{Date: dateStr, WorkingWindow: expectedWindow, Openings: openingEVs})
			}
			if !reflect.DeepEqual(days, fixture.Expected.Days) {
				t.Fatalf("days 与 TS golden 不一致:\ngot  %+v\nwant %+v", days, fixture.Expected.Days)
			}
			errorsEV := fixture.Expected.Errors
			if len(dayErrors) == 0 {
				// 统一 nil/空比较：TS 侧序列化为 []。
				dayErrors = []goldenDayErrorEV{}
			}
			if !reflect.DeepEqual(dayErrors, errorsEV) {
				t.Fatalf("errors 与 TS golden 不一致: got %+v want %+v", dayErrors, errorsEV)
			}

			if fixture.Month != nil {
				monthStart, err := time.Parse("2006-01-02", *fixture.Month+"-01")
				if err != nil {
					t.Fatalf("parse month %s: %v", *fixture.Month, err)
				}
				days := DaysInMonth(monthStart)
				overview, overviewErrs := MonthOverview(slots, monthStart, days, loc, plan)
				_ = overviewErrs // 月概览错误已在逐日 errors 面对拍；这里不重复断言
				got := goldenMonthOverview(overview)
				if !reflect.DeepEqual(got, *fixture.Expected.MonthOverview) {
					t.Fatalf("month_overview 与 TS golden 不一致: got %+v want %+v", got, *fixture.Expected.MonthOverview)
				}
			}
		})
	}
}
