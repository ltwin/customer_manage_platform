package reminder

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/clock"
)

// 日界 date-only 工具下沉到 platform/clock，reminder 与 dashboard 同源引用（design D9）。
// 以下别名 / 转发保留 reminder 既有内部与测试 API 不变。

// AccountClock 复用共享 date-only clock。
type AccountClock = clock.AccountClock

// NewAccountClock 加载账号时区 clock。
func NewAccountClock(timezone string) (AccountClock, error) {
	return clock.NewAccountClock(timezone)
}

// ParseDate 解析 "YYYY-MM-DD"；沿用 reminder 校验错误语义（400 validation_failed）。
func ParseDate(s string) (time.Time, error) {
	d, err := clock.ParseDate(s)
	if err != nil {
		return time.Time{}, ValidationError{Message: "date 格式须为 YYYY-MM-DD"}
	}
	return d, nil
}

// FormatDate 输出 YYYY-MM-DD。
func FormatDate(d time.Time) string {
	return clock.FormatDate(d)
}

// AddDays 在 date-only 上加天数。
func AddDays(d time.Time, days int) time.Time {
	return clock.AddDays(d, days)
}

// DaysBetween 返回 b - a 的整天数（date-only）。
func DaysBetween(a, b time.Time) int {
	return clock.DaysBetween(a, b)
}

func dateOnly(t time.Time) time.Time {
	return clock.DateOnly(t)
}

// NextBirthdayOccurrence 计算扫描日视角下的下一次生日发生日。
// birthday 支持 "MM-DD" 与 "YYYY-MM-DD"；空串返回 ok=false。
// 02-29 在非闰年落到 02-28（A1）。
func NextBirthdayOccurrence(scanDate time.Time, birthday string) (due time.Time, ok bool) {
	birthday = strings.TrimSpace(birthday)
	if birthday == "" {
		return time.Time{}, false
	}
	month, day, ok := parseBirthdayMD(birthday)
	if !ok {
		return time.Time{}, false
	}
	scan := dateOnly(scanDate)
	year := scan.Year()
	due = clampBirthday(year, month, day)
	if due.Before(scan) {
		due = clampBirthday(year+1, month, day)
	}
	return due, true
}

func parseBirthdayMD(birthday string) (month, day int, ok bool) {
	parts := strings.Split(birthday, "-")
	switch len(parts) {
	case 2:
		m, err1 := strconv.Atoi(parts[0])
		d, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return m, d, validMonthDay(m, d)
	case 3:
		m, err1 := strconv.Atoi(parts[1])
		d, err2 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return m, d, validMonthDay(m, d)
	default:
		return 0, 0, false
	}
}

func validMonthDay(m, d int) bool {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return false
	}
	// 允许 02-29；其余用非闰年试探（2 月 29 特判）。
	if m == 2 && d == 29 {
		return true
	}
	t := time.Date(2021, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return t.Month() == time.Month(m) && t.Day() == d
}

func clampBirthday(year, month, day int) time.Time {
	if month == 2 && day == 29 && !isLeap(year) {
		day = 28
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func isLeap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

// BirthdayDedupKey 生成 birthday 幂等键：birthday:{customer_id}:{发生年}（A4）。
func BirthdayDedupKey(customerID string, occurrence time.Time) string {
	return fmt.Sprintf("birthday:%s:%d", customerID, occurrence.Year())
}

// FollowUpDedupKey 生成 follow_up 幂等键。
func FollowUpDedupKey(orderID string) string {
	return "follow_up:" + orderID
}

// ChurnDedupKey 生成 churn 幂等键。
func ChurnDedupKey(customerID, orderID string) string {
	return fmt.Sprintf("churn:%s:%s", customerID, orderID)
}

// CustomDedupKey 生成 custom 幂等键（D7）。
func CustomDedupKey(reminderID string) string {
	return "custom:" + reminderID
}
