package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewReturnsRuntimeCBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-c", Config{ExecutablePath: "/nonexistent/claude"})
	if err != nil {
		t.Fatalf("New(claude) error: %v", err)
	}
	if _, ok := b.(*runtimeCBackend); !ok {
		t.Fatalf("expected *claudeBackend, got %T", b)
	}
}

func TestNewReturnsRuntimeEBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-e", Config{ExecutablePath: "/nonexistent/codex"})
	if err != nil {
		t.Fatalf("New(codex) error: %v", err)
	}
	if _, ok := b.(*runtimeEBackend); !ok {
		t.Fatalf("expected *codexBackend, got %T", b)
	}
}

func TestNewReturnsRuntimeDBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-d", Config{ExecutablePath: "/nonexistent/codebuddy"})
	if err != nil {
		t.Fatalf("New(codebuddy) error: %v", err)
	}
	if _, ok := b.(*runtimeDBackend); !ok {
		t.Fatalf("expected *codebuddyBackend, got %T", b)
	}
}

func TestNewReturnsRuntimeFBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-f", Config{ExecutablePath: "/nonexistent/copilot"})
	if err != nil {
		t.Fatalf("New(copilot) error: %v", err)
	}
	if _, ok := b.(*runtimeFBackend); !ok {
		t.Fatalf("expected *copilotBackend, got %T", b)
	}
}

func TestNewReturnsRuntimePBackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-p", Config{ExecutablePath: "/nonexistent/qodercli"})
	if err != nil {
		t.Fatalf("New(qoder) error: %v", err)
	}
	if _, ok := b.(*runtimePBackend); !ok {
		t.Fatalf("expected *qoderBackend, got %T", b)
	}
}

func TestNewReturnsRuntimeABackend(t *testing.T) {
	t.Parallel()
	b, err := New("runtime-a", Config{ExecutablePath: "/nonexistent/agy"})
	if err != nil {
		t.Fatalf("New(antigravity) error: %v", err)
	}
	if _, ok := b.(*runtimeABackend); !ok {
		t.Fatalf("expected *antigravityBackend, got %T", b)
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := New("gpt", Config{})
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestNewDefaultsLogger(t *testing.T) {
	t.Parallel()
	b, _ := New("runtime-c", Config{})
	cb := b.(*runtimeCBackend)
	if cb.cfg.Logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestDetectVersionFailsForMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := DetectVersion(context.Background(), "/nonexistent/binary")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestDetectVersionTimesOutOnHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("relies on a /bin/sh hang script")
	}

	dir := t.TempDir()
	script := filepath.Join(dir, "hang.sh")
	pidFile := filepath.Join(dir, "child.pid")

	body := fmt.Sprintf("#!/bin/sh\nsleep 60 &\necho $! > %q\nwait\n", pidFile)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write hang script: %v", err)
	}
	t.Cleanup(func() {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			return
		}
		if proc, err := os.FindProcess(pid); err == nil {
			_ = proc.Kill()
		}
	})

	orig := detectVersionTimeout
	detectVersionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { detectVersionTimeout = orig })

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := DetectVersion(context.Background(), script)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from a hanging --version probe, got nil")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("detection took %v; expected it to be bounded by the timeout", elapsed)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("DetectVersion did not return: version probe is unbounded (regression of MUL-3812)")
	}
}

func TestLaunchHeaderCoversAllSupportedBackends(t *testing.T) {
	t.Parallel()

	for _, t_ := range SupportedTypes {
		if header := LaunchHeader(t_); header == "" {
			t.Errorf("LaunchHeader(%q) returned empty string — add it to launchHeaders", t_)
		}
	}
}

func TestLaunchHeaderRuntimeAAvoidsTextOnlyPrintModeLabel(t *testing.T) {
	t.Parallel()

	header := LaunchHeader("runtime-a")
	if header != "-p (non-interactive)" {
		t.Fatalf("unexpected runtime-a launch header: %q", header)
	}
	if strings.Contains(header, "print mode") {
		t.Fatalf("runtime-a launch header must not imply a text-only mode: %q", header)
	}
}

func TestLaunchHeaderReturnsEmptyForUnknownType(t *testing.T) {
	t.Parallel()
	if header := LaunchHeader("made-up-agent"); header != "" {
		t.Errorf("expected empty header for unknown type, got %q", header)
	}
}

func TestRunContextZeroTimeoutHasNoDeadline(t *testing.T) {
	t.Parallel()

	for _, d := range []time.Duration{0, -time.Second} {
		ctx, cancel := runContext(context.Background(), d)
		if _, ok := ctx.Deadline(); ok {
			cancel()
			t.Fatalf("runContext(%s) imposed a deadline; want none", d)
		}
		cancel()
		if ctx.Err() == nil {
			t.Fatalf("runContext(%s): context should be cancelled after cancel()", d)
		}
	}
}

func TestRunContextPositiveTimeoutHasDeadline(t *testing.T) {
	t.Parallel()

	ctx, cancel := runContext(context.Background(), time.Hour)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("runContext(1h) should impose a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > time.Hour+time.Minute {
		t.Fatalf("unexpected deadline remaining: %s", remaining)
	}
}
