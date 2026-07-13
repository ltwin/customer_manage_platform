package reminder

import (
	"testing"
	"time"
)

func TestNextBirthdayOccurrenceWindowAndLeap(t *testing.T) {
	scan := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)

	due, ok := NextBirthdayOccurrence(scan, "07-12")
	if !ok || !due.Equal(time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("same-year birthday: ok=%v due=%v", ok, due)
	}

	due, ok = NextBirthdayOccurrence(scan, "1990-01-05")
	if !ok || !due.Equal(time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("cross-year birthday: ok=%v due=%v", ok, due)
	}

	// 非闰年 02-29 → 02-28（A1）
	scan = time.Date(2025, 2, 20, 0, 0, 0, 0, time.UTC)
	due, ok = NextBirthdayOccurrence(scan, "02-29")
	if !ok || !due.Equal(time.Date(2025, 2, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("leap clamp: ok=%v due=%v", ok, due)
	}

	if _, ok := NextBirthdayOccurrence(scan, ""); ok {
		t.Fatal("empty birthday should not trigger")
	}
}

func TestBirthdayDedupUsesOccurrenceYear(t *testing.T) {
	due := time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC)
	if got := BirthdayDedupKey("cus_1", due); got != "birthday:cus_1:2027" {
		t.Fatalf("dedup key: %s", got)
	}
}

func TestAccountClockLocalDate(t *testing.T) {
	clock, err := NewAccountClock("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// 2026-07-11 02:00 UTC = 2026-07-10 22:00 EDT
	instant := time.Date(2026, 7, 11, 2, 0, 0, 0, time.UTC)
	local := clock.LocalDate(instant)
	if !local.Equal(time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("local date: %v", local)
	}
}
