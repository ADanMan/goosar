package autopilot

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) Schedule {
	t.Helper()
	s, err := ParseCron(expr)
	if err != nil {
		t.Fatalf("ParseCron(%q): %v", expr, err)
	}
	return s
}

func TestParseCron_Invalid(t *testing.T) {
	cases := []string{
		"", "* * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"a * * * *", "1-2-3 * * * *", "*/0 * * * *",
	}
	for _, c := range cases {
		if _, err := ParseCron(c); err == nil {
			t.Errorf("ParseCron(%q): expected error, got none", c)
		}
	}
}

func TestSchedule_Next_EveryDayAtNine(t *testing.T) {
	s := mustParse(t, "0 9 * * *")
	from := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)
	next, ok := s.Next(from)
	if !ok {
		t.Fatal("Next: expected a result")
	}
	want := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", from, next, want)
	}

	// после 9:00 того же дня — переходит на завтра
	from2 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	next2, ok := s.Next(from2)
	if !ok {
		t.Fatal("Next: expected a result")
	}
	want2 := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	if !next2.Equal(want2) {
		t.Errorf("Next(%v) = %v, want %v", from2, next2, want2)
	}
}

func TestSchedule_Next_EveryFiveMinutes(t *testing.T) {
	s := mustParse(t, "*/5 * * * *")
	from := time.Date(2026, 1, 1, 8, 3, 0, 0, time.UTC)
	next, ok := s.Next(from)
	if !ok {
		t.Fatal("Next: expected a result")
	}
	want := time.Date(2026, 1, 1, 8, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v", from, next, want)
	}
}

func TestSchedule_Next_Weekdays(t *testing.T) {
	// 9:00 по будним дням (пн-пт); 2026-01-03 — суббота.
	s := mustParse(t, "0 9 * * 1-5")
	from := time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC) // суббота, после 9
	next, ok := s.Next(from)
	if !ok {
		t.Fatal("Next: expected a result")
	}
	want := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC) // понедельник
	if !next.Equal(want) {
		t.Errorf("Next(%v) = %v, want %v (%s)", from, next, want, next.Weekday())
	}
}

func TestSchedule_DayMatches_ORRule(t *testing.T) {
	// dom=1 (1 числа месяца) OR dow=1 (понедельник) — оба поля ограничены,
	// значит правило OR, не AND (классическое cron-поведение).
	s := mustParse(t, "0 0 1 * 1")
	// 2026-01-05 — понедельник, не 1 число: должно подойти по dow.
	d := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if !s.dayMatches(d) {
		t.Errorf("dayMatches(%v): want true (dow=Monday)", d)
	}
	// 2026-02-01 — воскресенье, но 1 число: должно подойти по dom.
	d2 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !s.dayMatches(d2) {
		t.Errorf("dayMatches(%v): want true (dom=1)", d2)
	}
	// 2026-01-06 — вторник, 6 число: ни то ни другое.
	d3 := time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC)
	if s.dayMatches(d3) {
		t.Errorf("dayMatches(%v): want false", d3)
	}
}

func TestSchedule_Next_ImpossibleDateDoesNotHang(t *testing.T) {
	s := mustParse(t, "0 0 30 2 *") // 30 февраля не существует никогда
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, ok := s.Next(from); ok {
		t.Error("Next: expected no result for an impossible date, got one")
	}
}

func TestLoadTimezone(t *testing.T) {
	if loc, err := LoadTimezone(""); err != nil || loc != time.UTC {
		t.Errorf("LoadTimezone(\"\") = %v, %v; want time.UTC, nil", loc, err)
	}
	if _, err := LoadTimezone("Europe/Moscow"); err != nil {
		t.Errorf("LoadTimezone(Europe/Moscow): %v", err)
	}
	if _, err := LoadTimezone("Not/AZone"); err == nil {
		t.Error("LoadTimezone(Not/AZone): expected error")
	}
}

func TestSchedule_NextInLocation(t *testing.T) {
	s := mustParse(t, "0 9 * * *")
	moscow, _ := time.LoadLocation("Europe/Moscow")
	// 9:00 MSK (UTC+3) == 06:00 UTC.
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	next, ok := s.NextInLocation(from, moscow)
	if !ok {
		t.Fatal("NextInLocation: expected a result")
	}
	want := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("NextInLocation = %v (UTC), want %v", next, want)
	}
}
