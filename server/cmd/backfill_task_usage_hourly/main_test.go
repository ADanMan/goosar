package main

import (
	"testing"
	"time"
)

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return loc
}

func TestMonthFloorNormalizesZonedInputToUTCMonth(t *testing.T) {
	t.Parallel()

	loc := time.FixedZone("plus2", 2*3600)
	got := monthFloor(time.Date(2026, 3, 1, 0, 30, 0, 0, loc).UTC())
	want := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("monthFloor = %s, want %s", got, want)
	}
}

func TestBackfillWindow(t *testing.T) {
	t.Parallel()

	utc := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	t.Run("floors min and extends end past max month", func(t *testing.T) {
		t.Parallel()
		from, end, truncated := backfillWindow(
			time.Date(2025, 11, 17, 9, 30, 0, 0, time.UTC),
			time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
			now, 0)
		if truncated {
			t.Fatal("unexpected truncation")
		}
		if !from.Equal(utc(2025, 11, 1)) || !end.Equal(utc(2026, 3, 1)) {
			t.Fatalf("window = [%s, %s)", from, end)
		}
	})

	t.Run("min and max in the same month yield a one-month window", func(t *testing.T) {
		t.Parallel()
		from, end, _ := backfillWindow(
			time.Date(2026, 6, 2, 1, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 28, 23, 0, 0, 0, time.UTC),
			now, 0)
		if !from.Equal(utc(2026, 6, 1)) || !end.Equal(utc(2026, 7, 1)) {
			t.Fatalf("window = [%s, %s)", from, end)
		}
	})

	t.Run("months-back cutoff truncates older history", func(t *testing.T) {
		t.Parallel()
		from, end, truncated := backfillWindow(
			utc(2024, 1, 15), utc(2026, 8, 10), now, 3)
		if !truncated {
			t.Fatal("expected truncation")
		}
		if !from.Equal(utc(2026, 6, 1)) || !end.Equal(utc(2026, 9, 1)) {
			t.Fatalf("window = [%s, %s)", from, end)
		}
	})

	t.Run("months-back wider than history is a no-op", func(t *testing.T) {
		t.Parallel()
		from, _, truncated := backfillWindow(
			utc(2026, 7, 15), utc(2026, 8, 10), now, 12)
		if truncated {
			t.Fatal("unexpected truncation")
		}
		if !from.Equal(utc(2026, 7, 1)) {
			t.Fatalf("from = %s", from)
		}
	})

	t.Run("zoned min/max are converted to UTC before flooring", func(t *testing.T) {
		t.Parallel()
		berlin := mustLoadLocation(t, "Europe/Berlin")

		from, end, _ := backfillWindow(
			time.Date(2026, 1, 1, 0, 30, 0, 0, berlin),
			time.Date(2026, 1, 1, 0, 30, 0, 0, berlin),
			now, 0)
		if !from.Equal(utc(2025, 12, 1)) || !end.Equal(utc(2026, 1, 1)) {
			t.Fatalf("window = [%s, %s)", from, end)
		}
	})
}

func TestMonthSlices(t *testing.T) {
	t.Parallel()

	utc := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	t.Run("empty when from equals end", func(t *testing.T) {
		t.Parallel()
		if got := monthSlices(utc(2026, 4, 1), utc(2026, 4, 1)); len(got) != 0 {
			t.Fatalf("expected no slices, got %#v", got)
		}
	})

	t.Run("consecutive whole months with no gaps or overlaps", func(t *testing.T) {
		t.Parallel()
		got := monthSlices(utc(2025, 11, 1), utc(2026, 3, 1))
		if len(got) != 4 {
			t.Fatalf("expected 4 slices, got %d: %#v", len(got), got)
		}
		if !got[0][0].Equal(utc(2025, 11, 1)) || !got[3][1].Equal(utc(2026, 3, 1)) {
			t.Fatalf("unexpected bounds: %#v", got)
		}
		for i := 1; i < len(got); i++ {
			if !got[i][0].Equal(got[i-1][1]) {
				t.Fatalf("gap/overlap between slices %d and %d: %#v", i-1, i, got)
			}
		}
	})

	t.Run("leap February slice covers exactly 29 days", func(t *testing.T) {
		t.Parallel()
		got := monthSlices(utc(2024, 2, 1), utc(2024, 3, 1))
		if len(got) != 1 {
			t.Fatalf("expected 1 slice, got %#v", got)
		}
		if d := got[0][1].Sub(got[0][0]); d != 29*24*time.Hour {
			t.Fatalf("slice duration = %s, want 696h", d)
		}
	})

	t.Run("DST transition months keep UTC-uniform boundaries", func(t *testing.T) {
		t.Parallel()

		got := monthSlices(utc(2026, 3, 1), utc(2026, 4, 1))
		if len(got) != 1 {
			t.Fatalf("expected 1 slice, got %#v", got)
		}
		if d := got[0][1].Sub(got[0][0]); d != 31*24*time.Hour {
			t.Fatalf("slice duration = %s, want 744h", d)
		}
		for _, edge := range []time.Time{got[0][0], got[0][1]} {
			if h, m, s := edge.Clock(); h != 0 || m != 0 || s != 0 || edge.Location() != time.UTC {
				t.Fatalf("boundary not midnight UTC: %s", edge)
			}
		}
	})
}
