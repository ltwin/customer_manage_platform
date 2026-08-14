package reminder

import (
	"fmt"
	"time"
)

// ComputeAssignmentDueDate 按账号时区本地拍摄日减 lead 日历日，得到 date-only due。
// 仅当 slot.StartAt > now 时可调度；validUntil 恒为 absolute StartAt。
// 禁止 24h*n DST 推算；不读取 Order.shot_at。
func ComputeAssignmentDueDate(
	slotStartAt time.Time,
	timezone string,
	preparationLeadDays int,
	now time.Time,
) (dueDate time.Time, validUntil time.Time, err error) {
	if preparationLeadDays < 0 {
		return time.Time{}, time.Time{}, fmt.Errorf("preparation_lead_days_snapshot must be non-negative")
	}
	if !slotStartAt.After(now) {
		return time.Time{}, time.Time{}, fmt.Errorf("slot start is not in the future")
	}
	clock, err := NewAccountClock(timezone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	shootLocalDate := clock.LocalDate(slotStartAt)
	dueDate = AddDays(shootLocalDate, -preparationLeadDays)
	return dueDate, slotStartAt.UTC(), nil
}
