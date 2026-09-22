package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func NextOccurrenceAfterUTC(cronExpr, timezone string, after time.Time) (time.Time, error) {
	sched, loc, err := parseCronSchedule(cronExpr, timezone)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after.In(loc)).UTC(), nil
}

func NextOccurrencesAfterUTC(cronExpr, timezone string, after time.Time, count int) ([]time.Time, error) {
	sched, loc, err := parseCronSchedule(cronExpr, timezone)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, count)
	cursor := after.In(loc)
	for i := 0; i < count; i++ {
		next := sched.Next(cursor)
		if next.IsZero() {
			break
		}
		out = append(out, next.UTC())
		cursor = next
	}
	return out, nil
}

func NextOccurrencesUTC(cronExpr, timezone string, after, until time.Time) ([]time.Time, error) {
	sched, loc, err := parseCronSchedule(cronExpr, timezone)
	if err != nil {
		return nil, err
	}
	const hardCap = 1024
	out := make([]time.Time, 0, 8)
	cursor := after.In(loc)
	untilLocal := until.In(loc)
	for len(out) < hardCap {
		next := sched.Next(cursor)
		if next.After(untilLocal) {
			break
		}
		out = append(out, next.UTC())
		cursor = next
	}
	return out, nil
}

func ComputeNextRun(cronExpr, timezone string) (time.Time, error) {
	return NextOccurrenceAfterUTC(cronExpr, timezone, time.Now())
}

func ValidateTimezone(timezone string) error {
	_, err := time.LoadLocation(timezone)
	if err != nil {
		return fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return nil
}

func parseCronSchedule(cronExpr, timezone string) (cron.Schedule, *time.Location, error) {

	if (strings.HasPrefix(cronExpr, "TZ=") || strings.HasPrefix(cronExpr, "CRON_TZ=")) &&
		!strings.Contains(cronExpr, " ") {
		return nil, nil, fmt.Errorf("parse cron: missing schedule after timezone prefix %q", cronExpr)
	}
	sched, err := cronParser.Parse(cronExpr)
	if err != nil {
		return nil, nil, fmt.Errorf("parse cron: %w", err)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid timezone %q: %w", timezone, err)
	}
	return sched, loc, nil
}
