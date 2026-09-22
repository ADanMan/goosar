package service

import (
	"testing"
	"time"
)

func TestNextOccurrencesUTCEnumeratesInTimezone(t *testing.T) {

	cron := "0 9 * * MON-FRI"
	tz := "Asia/Shanghai"

	after := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 24, 1, 0, 0, 0, time.UTC)

	got, err := NextOccurrencesUTC(cron, tz, after, until)
	if err != nil {
		t.Fatalf("NextOccurrencesUTC: %v", err)
	}

	want := []time.Time{
		time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 24, 1, 0, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d occurrences, got %d: %v", len(want), len(got), got)
	}
	for i, g := range got {
		if !g.Equal(want[i]) {
			t.Fatalf("occurrence[%d]: got %s, want %s",
				i, g.Format(time.RFC3339), want[i].Format(time.RFC3339))
		}
		if g.Location() != time.UTC {
			t.Fatalf("occurrence[%d] must be UTC, got %s", i, g.Location())
		}
	}
}

func TestNextOccurrencesUTCEmptyWindow(t *testing.T) {
	cron := "*/5 * * * *"
	tz := "UTC"

	after := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	until := after.Add(-time.Minute)

	got, err := NextOccurrencesUTC(cron, tz, after, until)
	if err != nil {
		t.Fatalf("NextOccurrencesUTC: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no occurrences for empty window, got %v", got)
	}
}

func TestNextOccurrencesUTCExclusiveAfter(t *testing.T) {
	cron := "*/5 * * * *"
	tz := "UTC"

	after := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	until := time.Date(2026, 6, 23, 12, 10, 0, 0, time.UTC)

	got, err := NextOccurrencesUTC(cron, tz, after, until)
	if err != nil {
		t.Fatalf("NextOccurrencesUTC: %v", err)
	}
	want := []time.Time{
		time.Date(2026, 6, 23, 12, 5, 0, 0, time.UTC),
		time.Date(2026, 6, 23, 12, 10, 0, 0, time.UTC),
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d occurrences, got %d: %v", len(want), len(got), got)
	}
	for i, g := range got {
		if !g.Equal(want[i]) {
			t.Fatalf("occurrence[%d]: got %s want %s",
				i, g.Format(time.RFC3339), want[i].Format(time.RFC3339))
		}
	}
}

func TestNextOccurrencesUTCInvalidInputs(t *testing.T) {
	t.Run("bad cron", func(t *testing.T) {
		_, err := NextOccurrencesUTC("not a cron", "UTC", time.Now(), time.Now().Add(time.Hour))
		if err == nil {
			t.Fatal("expected error for bad cron expression")
		}
	})
	t.Run("bad timezone", func(t *testing.T) {
		_, err := NextOccurrencesUTC("* * * * *", "Mars/Olympus", time.Now(), time.Now().Add(time.Hour))
		if err == nil {
			t.Fatal("expected error for invalid timezone")
		}
	})
}

func TestNextOccurrenceAfterUTCIgnoresWallClock(t *testing.T) {
	cron := "30 14 * * *"

	after := time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	want := time.Date(2026, 6, 23, 14, 30, 0, 0, time.UTC)

	got, err := NextOccurrenceAfterUTC(cron, "UTC", after)
	if err != nil {
		t.Fatalf("NextOccurrenceAfterUTC: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestNextOccurrenceAdvancesPastFiredSlot(t *testing.T) {
	const cron = "0 * * * *"
	const tz = "America/New_York"

	fired := time.Date(2026, 6, 26, 7, 0, 0, 0, time.UTC)
	want := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC)

	got, err := NextOccurrenceAfterUTC(cron, tz, fired)
	if err != nil {
		t.Fatalf("NextOccurrenceAfterUTC: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if !got.After(fired) {
		t.Fatalf("next occurrence %s must be strictly after the fired slot %s", got, fired)
	}
}

func TestParseCronScheduleTimezonePrefix(t *testing.T) {
	t.Run("prefix without a schedule errors instead of panicking", func(t *testing.T) {

		for _, expr := range []string{"TZ=UTC", "CRON_TZ=Asia/Tokyo", "TZ="} {
			if _, _, err := parseCronSchedule(expr, "UTC"); err == nil {
				t.Fatalf("expected error for %q", expr)
			}
		}
	})
	t.Run("embedded timezone overrides the timezone argument", func(t *testing.T) {
		after := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
		got, err := NextOccurrenceAfterUTC("CRON_TZ=Asia/Tokyo 0 9 * * *", "UTC", after)
		if err != nil {
			t.Fatal(err)
		}

		want := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
		if !got.Equal(want) {
			t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	})
	t.Run("prefix and column are equivalent spellings", func(t *testing.T) {
		after := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
		embedded, err := NextOccurrenceAfterUTC("CRON_TZ=Asia/Tokyo 0 9 * * *", "UTC", after)
		if err != nil {
			t.Fatal(err)
		}
		column, err := NextOccurrenceAfterUTC("0 9 * * *", "Asia/Tokyo", after)
		if err != nil {
			t.Fatal(err)
		}
		if !embedded.Equal(column) {
			t.Fatalf("embedded %s != column %s", embedded, column)
		}
	})
	t.Run("prefix detection is exact", func(t *testing.T) {

		for _, expr := range []string{"tz=UTC 0 9 * * *", " TZ=UTC 0 9 * * *"} {
			if _, _, err := parseCronSchedule(expr, "UTC"); err == nil {
				t.Fatalf("expected error for %q", expr)
			}
		}
	})
}
