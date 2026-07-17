package digest

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestWindowForUsesFrozenTimezoneDateAndDSTHalfOpenDay(t *testing.T) {
	target := LocalTarget{LocalDate: time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC), Timezone: "America/New_York"}
	window, err := WindowFor(target)
	if err != nil {
		t.Fatalf("WindowFor: %v", err)
	}
	if got := window.End.Sub(window.Start); got != 23*time.Hour {
		t.Fatalf("DST spring day must be 23h, got %s", got)
	}
	if window.LocalDate.Format("2006-01-02") != "2026-03-08" || window.Timezone != target.Timezone {
		t.Fatalf("window did not preserve frozen target: %+v", window)
	}
}

func TestPostgresSnapshotRepositoryLoadsNarrowDigestReadModel(t *testing.T) {
	_, account := startDigestPostgres(t)
	ctx := context.Background()
	date := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	window, err := WindowFor(LocalTarget{LocalDate: date, Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("WindowFor: %v", err)
	}
	if err := account.Scope.Insert(ctx, "customers", []string{"id", "display_name", "channel"}, "cus-digest", "合成客户", "other"); err != nil {
		t.Fatalf("insert customer: %v", err)
	}
	if err := account.Scope.Insert(ctx, "packages", []string{"id", "name", "shoot_type", "pricing_mode", "base_price", "status"},
		"pkg-digest", "合成套系", "portrait", "fixed", 10000, "active"); err != nil {
		t.Fatalf("insert package: %v", err)
	}
	orders := []struct {
		id, status string
		paid       bool
	}{
		{id: "ord-unpaid", status: "delivered", paid: false},
		{id: "ord-other-status", status: "scheduled", paid: false},
		{id: "ord-paid", status: "delivered", paid: true},
	}
	for _, order := range orders {
		if err := account.Scope.Insert(ctx, "orders", []string{
			"id", "customer_id", "package_id", "status", "balance_paid",
		}, order.id, "cus-digest", "pkg-digest", order.status, order.paid); err != nil {
			t.Fatalf("insert order %s: %v", order.id, err)
		}
	}
	if err := account.Scope.Insert(ctx, "schedule_slots", []string{"id", "start_at", "end_at", "type", "order_id"},
		"slot-shoot", window.Start.Add(2*time.Hour), window.Start.Add(4*time.Hour), "shoot", "ord-unpaid"); err != nil {
		t.Fatalf("insert shoot: %v", err)
	}
	for _, slotType := range []string{"hold", "busy"} {
		if err := account.Scope.Insert(ctx, "schedule_slots", []string{"id", "start_at", "end_at", "type"},
			"slot-"+slotType, window.Start.Add(time.Hour), window.Start.Add(90*time.Minute), slotType); err != nil {
			t.Fatalf("insert %s: %v", slotType, err)
		}
	}
	for i := 0; i < 23; i++ {
		due := date
		if i < 3 {
			due = date.AddDate(0, 0, -1)
		}
		id := fmt.Sprintf("rem-%02d", i)
		if err := account.Scope.Insert(ctx, "reminders", []string{
			"id", "type", "due_date", "content", "status", "dedup_key",
		}, id, "custom", due.Format("2006-01-02"), "合成提醒 "+id, "pending", "digest:"+id); err != nil {
			t.Fatalf("insert reminder %s: %v", id, err)
		}
	}
	if err := account.Scope.Insert(ctx, "reminders", []string{
		"id", "type", "due_date", "content", "status", "dedup_key",
	}, "rem-future", "custom", date.AddDate(0, 0, 1).Format("2006-01-02"), "未来", "pending", "digest:future"); err != nil {
		t.Fatalf("insert future reminder: %v", err)
	}

	snapshot, err := NewPostgresSnapshotRepository().Load(ctx, account.Scope, window, 20)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snapshot.Reminders.Total != 23 || len(snapshot.Reminders.Items) != 20 {
		t.Fatalf("reminder limit/total mismatch: %+v", snapshot.Reminders)
	}
	if snapshot.Reminders.Items[0].ID != "rem-00" || snapshot.Reminders.Items[3].ID != "rem-03" {
		t.Fatalf("reminders are not due_date/id sorted: %+v", snapshot.Reminders.Items[:4])
	}
	if len(snapshot.TodayShootSlots) != 1 || snapshot.TodayShootSlots[0].CustomerName != "合成客户" || snapshot.TodayShootSlots[0].PackageName != "合成套系" {
		t.Fatalf("shoot slot parity/filter mismatch: %+v", snapshot.TodayShootSlots)
	}
	if snapshot.UnpaidCount != 1 {
		t.Fatalf("narrow unpaid count: want 1, got %d", snapshot.UnpaidCount)
	}
}

func TestRendererIsDeterministicShowsOverflowAndEmptyState(t *testing.T) {
	snapshot := DigestSnapshot{
		LocalDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		Timezone:  "Asia/Shanghai",
		Reminders: ReminderDigest{
			Total: 21,
			Items: []ReminderItem{{ID: "r1", DueDate: time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC), Content: "跟进合成客户"}},
		},
		UnpaidCount: 2,
	}
	renderer := NewRenderer()
	first := renderer.Render(snapshot)
	second := renderer.Render(snapshot)
	if first != second {
		t.Fatal("same snapshot rendered different text")
	}
	for _, fragment := range []string{"7月15日经营摘要", "逾期 07-14", "另有 20 条", "今日暂无拍摄", "待收尾款：2 笔"} {
		if !strings.Contains(first, fragment) {
			t.Fatalf("render lacks %q:\n%s", fragment, first)
		}
	}

	empty := renderer.Render(DigestSnapshot{LocalDate: snapshot.LocalDate, Timezone: snapshot.Timezone})
	if !strings.Contains(empty, "今日暂无待处理事项") {
		t.Fatalf("empty snapshot lacks liveness summary: %s", empty)
	}
}

func TestRendererTruncatesAt3500UnicodeCodePoints(t *testing.T) {
	content := strings.Repeat("长", 4000)
	snapshot := DigestSnapshot{
		LocalDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
		Timezone:  "Asia/Shanghai",
		Reminders: ReminderDigest{Total: 1, Items: []ReminderItem{{ID: "r1", DueDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC), Content: content}}},
	}
	text := NewRenderer().Render(snapshot)
	if count := len([]rune(text)); count > 3500 {
		t.Fatalf("renderer emitted %d code points", count)
	}
	if !strings.HasSuffix(text, "内容已截断，请到网页查看") || !utf8.ValidString(text) {
		t.Fatalf("truncation marker/UTF-8 invalid: suffix=%q", text[len(text)-minInt(len(text), 80):])
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
