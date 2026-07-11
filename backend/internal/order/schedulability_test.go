package order

import (
	"testing"
	"time"
)

func TestCanScheduleAtUsesSharedFutureHistoryMatrix(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Second)
	history := now

	tests := []struct {
		name           string
		orderStatus    string
		customerStatus string
		slotEnd        time.Time
		want           bool
	}{
		{name: "future consulting active", orderStatus: StatusConsulting, customerStatus: "active", slotEnd: future, want: true},
		{name: "future scheduled active", orderStatus: StatusScheduled, customerStatus: "active", slotEnd: future, want: true},
		{name: "future later status rejected", orderStatus: StatusShot, customerStatus: "active", slotEnd: future},
		{name: "future archived customer rejected", orderStatus: StatusScheduled, customerStatus: "archived", slotEnd: future},
		{name: "end equal now is history", orderStatus: StatusShot, customerStatus: "archived", slotEnd: history, want: true},
		{name: "history scheduled active", orderStatus: StatusScheduled, customerStatus: "active", slotEnd: history, want: true},
		{name: "history closed archived", orderStatus: StatusClosed, customerStatus: "archived", slotEnd: history, want: true},
		{name: "history consulting rejected", orderStatus: StatusConsulting, customerStatus: "active", slotEnd: history},
		{name: "cancelled always rejected", orderStatus: StatusCancelled, customerStatus: "active", slotEnd: history},
		{name: "merged always rejected", orderStatus: StatusScheduled, customerStatus: "merged", slotEnd: history},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanScheduleAt(tc.orderStatus, tc.customerStatus, tc.slotEnd, now); got != tc.want {
				t.Fatalf("CanScheduleAt() = %v, want %v", got, tc.want)
			}
		})
	}
}
