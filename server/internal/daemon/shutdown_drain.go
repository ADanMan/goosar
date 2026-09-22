package daemon

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const DefaultDrainTimeout = 10 * time.Minute

const drainTimeoutEnvVar = "GOOSAR_DAEMON_DRAIN_TIMEOUT"

func drainTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(drainTimeoutEnvVar))
	if raw == "" {
		return DefaultDrainTimeout
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		slog.Warn("invalid GOOSAR_DAEMON_DRAIN_TIMEOUT, using default",
			"value", raw, "default", DefaultDrainTimeout.String(), "error", err)
		return DefaultDrainTimeout
	}
	return parsed
}

func taskParentContext(root context.Context) (context.Context, context.CancelFunc) {
	detached := context.WithoutCancel(root)
	return context.WithCancel(detached)
}

func drainInFlightTasks(wg *sync.WaitGroup, grace time.Duration, force bool, taskCancel context.CancelFunc, logger *slog.Logger) bool {
	if force {
		logger.Warn("forced stop: cancelling in-flight tasks")
		taskCancel()
	} else {
		logger.Info("draining in-flight tasks before stopping", "max_wait", grace.String())
	}

	allDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(allDone)
	}()

	deadline := time.NewTimer(grace)
	defer deadline.Stop()

	select {
	case <-allDone:
		logger.Info("in-flight tasks drained")
		return true
	case <-deadline.C:
		logger.Warn("drain grace expired with tasks still running; exiting without reporting them — the server will settle them as runtime_offline",
			"grace", grace.String())
		return false
	}
}
