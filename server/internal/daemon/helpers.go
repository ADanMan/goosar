package daemon

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func readEnv(key string) (string, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	return v, v != ""
}

func envOrDefault(key, fallback string) string {
	if v, ok := readEnv(key); ok {
		return v
	}
	return fallback
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	v, ok := readEnv(key)
	if !ok {
		return fallback, nil
	}
	d, err := parseFlexDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", key, v, err)
	}
	return d, nil
}

var dayUnit = regexp.MustCompile(`(\d*\.\d+|\d+)d`)

func parseFlexDuration(value string) (time.Duration, error) {
	var parseErr error
	expanded := dayUnit.ReplaceAllStringFunc(value, func(match string) string {
		days, err := strconv.ParseFloat(match[:len(match)-1], 64)
		if err != nil {
			parseErr = err
			return match
		}

		return strconv.FormatFloat(days*24, 'f', -1, 64) + "h"
	})
	if parseErr != nil {
		return 0, parseErr
	}
	return time.ParseDuration(expanded)
}

func intFromEnv(key string, fallback int) (int, error) {
	v, ok := readEnv(key)
	if !ok {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, v, err)
	}
	return n, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func sleepWithContextOrWakeup(ctx context.Context, d time.Duration, wakeups <-chan struct{}) error {
	if wakeups == nil {
		return sleepWithContext(ctx, d)
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wakeups:
		return nil
	case <-timer.C:
		return nil
	}
}
