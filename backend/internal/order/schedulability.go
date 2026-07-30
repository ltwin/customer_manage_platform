package order

import "time"

// CanScheduleAt 是订单候选查询与 shoot slot 写入共用的未来/历史矩阵节点。
func CanScheduleAt(orderStatus, customerStatus string, slotEnd, now time.Time) bool {
	orderStatuses, customerStatuses := schedulableStatuses(slotEnd, now)
	return containsStatus(orderStatuses, orderStatus) && containsStatus(customerStatuses, customerStatus)
}

func schedulableStatuses(slotEnd, now time.Time) ([]string, []string) {
	if slotEnd.After(now) {
		return []string{StatusConsulting, StatusScheduled}, []string{"active"}
	}
	return []string{
		StatusScheduled,
		StatusShot,
		StatusSelected,
		StatusRetouching,
		StatusDelivered,
		StatusClosed,
	}, []string{"active", "archived"}
}

func containsStatus(statuses []string, candidate string) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}
