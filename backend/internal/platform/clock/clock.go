// Package clock 提供账号时区的 date-only 判定，是 reminder 与 dashboard 日界计算的唯一同源（design D9）。
package clock

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidDate 表示日期字符串不符合 YYYY-MM-DD 格式。
var ErrInvalidDate = errors.New("invalid_date")

// AccountClock 是账号时区 date-only 判定的入口。
type AccountClock struct {
	loc *time.Location
}

// NewAccountClock 加载 IANA 时区；空串回退 Asia/Shanghai，非法时区返回 error。
func NewAccountClock(timezone string) (AccountClock, error) {
	if timezone == "" {
		timezone = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return AccountClock{}, fmt.Errorf("load timezone %q: %w", timezone, err)
	}
	return AccountClock{loc: loc}, nil
}

// Location 暴露账号时区（供 openings/利用率等需要逐时刻解析的算法复用同一时区）。
func (c AccountClock) Location() *time.Location {
	return c.loc
}

// LocalDate 把瞬时时间截成账号本地 date-only（UTC 午夜表示）。
func (c AccountClock) LocalDate(t time.Time) time.Time {
	local := t.In(c.loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// DayBounds 返回账号本地自然日 date（UTC 午夜表示）在真实瞬时上的半开区间 [start, end)。
// 供「与本地今日相交」这类需要真实时间戳边界的判定使用。
func (c AccountClock) DayBounds(date time.Time) (start, end time.Time) {
	d := DateOnly(date)
	start = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, c.loc)
	end = start.AddDate(0, 0, 1)
	return start, end
}

// ParseDate 解析 "YYYY-MM-DD" 为 date-only（UTC 午夜）。
func ParseDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date 格式须为 YYYY-MM-DD", ErrInvalidDate)
	}
	return t, nil
}

// FormatDate 输出 YYYY-MM-DD。
func FormatDate(d time.Time) string {
	return d.UTC().Format("2006-01-02")
}

// AddDays 在 date-only 上加天数。
func AddDays(d time.Time, days int) time.Time {
	return d.AddDate(0, 0, days)
}

// DaysBetween 返回 b - a 的整天数（date-only）。
func DaysBetween(a, b time.Time) int {
	a = DateOnly(a)
	b = DateOnly(b)
	return int(b.Sub(a).Hours() / 24)
}

// DateOnly 把任意时间归一化为 UTC 午夜 date-only。
func DateOnly(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
