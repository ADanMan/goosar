package scheduler

import (
	"testing"
	"time"
)

func TestAdvancedNextRunStrictlyAfterPlanTime(t *testing.T) {
	const cron = "0 * * * *"
	const tz = "America/New_York"
	planTime := time.Date(2026, 6, 26, 7, 0, 0, 0, time.UTC)
	want := time.Date(2026, 6, 26, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		now  time.Time
	}{
		{"app clock lags the fired slot (skew)", planTime.Add(-90 * time.Second)},
		{"app clock exactly on the fired slot", planTime},
		{"app clock just after the fired slot (normal)", planTime.Add(5 * time.Second)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := advancedNextRun(cron, tz, planTime, tc.now)
			if !ok {
				t.Fatal("expected ok=true for a valid cron/timezone")
			}
			if !got.Equal(want) {
				t.Fatalf("got %s, want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
			}
			if !got.After(planTime) {
				t.Fatalf("next_run_at %s must be strictly after the fired plan_time %s",
					got.Format(time.RFC3339), planTime.Format(time.RFC3339))
			}
		})
	}
}

func TestAdvancedNextRunInvalidInputsSignalFallback(t *testing.T) {
	planTime := time.Date(2026, 6, 26, 7, 0, 0, 0, time.UTC)
	if _, ok := advancedNextRun("not a cron", "UTC", planTime, planTime); ok {
		t.Fatal("expected ok=false for an invalid cron expression")
	}
	if _, ok := advancedNextRun("0 * * * *", "Mars/Olympus", planTime, planTime); ok {
		t.Fatal("expected ok=false for an invalid timezone")
	}
}
