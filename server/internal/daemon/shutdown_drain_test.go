package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestTaskParentContextSurvivesDaemonShutdown(t *testing.T) {
	type key struct{}
	root, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "carried"))

	taskCtx, taskCancel := taskParentContext(root)
	defer taskCancel()

	cancel()
	<-root.Done()

	if err := taskCtx.Err(); err != nil {
		t.Fatalf("daemon shutdown must not cancel in-flight tasks, got %v", err)
	}
	if got := taskCtx.Value(key{}); got != "carried" {
		t.Fatalf("task context lost its request-scoped values: %v", got)
	}

	taskCancel()
	if taskCtx.Err() == nil {
		t.Fatal("the drain's own cancel must still work (that is what --force uses)")
	}
}

func TestDrainInFlightTasksWaitsForCompletion(t *testing.T) {
	var wg sync.WaitGroup
	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()

	wg.Add(1)
	finished := make(chan struct{})
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Millisecond)
		close(finished)
	}()

	drained := drainInFlightTasks(&wg, time.Second, false, taskCancel, slog.Default())
	if !drained {
		t.Fatal("drain must report completion when the task finished inside the grace")
	}
	select {
	case <-finished:
	default:
		t.Fatal("drain returned before the task finished")
	}
	if taskCtx.Err() != nil {
		t.Fatal("a completed graceful drain must not cancel the task context")
	}
}

func TestDrainInFlightTasksForceCancelsImmediately(t *testing.T) {
	var wg sync.WaitGroup
	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-taskCtx.Done()
	}()

	start := time.Now()
	if drained := drainInFlightTasks(&wg, time.Minute, true, taskCancel, slog.Default()); !drained {
		t.Fatal("force drain must still wait for the cancelled tasks to unwind")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("--force must cancel immediately, waited %s", elapsed)
	}
	if taskCtx.Err() == nil {
		t.Fatal("--force must cancel the task context")
	}
}

func TestDrainInFlightTasksDeadlineLeavesTasksForReclaim(t *testing.T) {
	var wg sync.WaitGroup
	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()

	wg.Add(1)
	release := make(chan struct{})
	go func() {
		defer wg.Done()
		<-release
	}()
	defer close(release)

	if drained := drainInFlightTasks(&wg, 50*time.Millisecond, false, taskCancel, slog.Default()); drained {
		t.Fatal("drain must report failure when the grace expires with tasks still running")
	}
	if taskCtx.Err() != nil {
		t.Fatal("an expired grace must leave the task context alone so the server reclaims the task instead of recording a cancellation")
	}
}

func TestShutdownDrainLetsFakeAgentFinish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}
	dir := t.TempDir()
	fakeAgent := filepath.Join(dir, "fake-agent")
	marker := filepath.Join(dir, "finished")
	script := "#!/bin/sh\nsleep 0.3\necho done > " + marker + "\n"
	if err := os.WriteFile(fakeAgent, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake agent: %v", err)
	}

	root, cancel := context.WithCancel(context.Background())
	taskCtx, taskCancel := taskParentContext(root)
	defer taskCancel()

	cmd := exec.CommandContext(taskCtx, fakeAgent)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fake agent: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		cmd.Wait()
	}()

	cancel()

	if drained := drainInFlightTasks(&wg, 10*time.Second, false, taskCancel, slog.Default()); !drained {
		t.Fatal("drain timed out on a 0.3s fake agent")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("fake agent was killed by daemon shutdown instead of being allowed to finish: %v", err)
	}
}

func TestDrainTimeoutFromEnv(t *testing.T) {
	t.Setenv("GOOSAR_DAEMON_DRAIN_TIMEOUT", "")
	if got := drainTimeoutFromEnv(); got != DefaultDrainTimeout {
		t.Fatalf("default drain timeout = %s, want %s", got, DefaultDrainTimeout)
	}
	t.Setenv("GOOSAR_DAEMON_DRAIN_TIMEOUT", "90s")
	if got := drainTimeoutFromEnv(); got != 90*time.Second {
		t.Fatalf("drain timeout = %s, want 90s", got)
	}

	t.Setenv("GOOSAR_DAEMON_DRAIN_TIMEOUT", "banana")
	if got := drainTimeoutFromEnv(); got != DefaultDrainTimeout {
		t.Fatalf("unparsable value must fall back to the default, got %s", got)
	}
}
