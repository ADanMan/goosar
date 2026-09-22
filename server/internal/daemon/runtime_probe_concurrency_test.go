package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestDetectBuiltinRuntimes_ProbesRunConcurrently(t *testing.T) {
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})

	const probeBlock = 100 * time.Millisecond
	var inFlight, maxInFlight int32
	detectAgentVersion = func(_ context.Context, _ string) (string, error) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			prev := atomic.LoadInt32(&maxInFlight)
			if cur <= prev || atomic.CompareAndSwapInt32(&maxInFlight, prev, cur) {
				break
			}
		}
		time.Sleep(probeBlock)
		atomic.AddInt32(&inFlight, -1)
		return "9.9.9", nil
	}
	checkAgentMinVersion = func(_, _ string) error { return nil }

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{
		"runtime-c": {Path: "/usr/bin/true"},
		"runtime-e": {Path: "/usr/bin/true"},
		"runtime-g": {Path: "/usr/bin/true"},
		"runtime-m": {Path: "/usr/bin/true"},
		"runtime-j": {Path: "/usr/bin/true"},
		"runtime-o": {Path: "/usr/bin/true"},
	}

	start := time.Now()
	runtimes := d.detectBuiltinRuntimes(context.Background())
	elapsed := time.Since(start)

	if len(runtimes) != len(d.cfg.Agents) {
		t.Fatalf("expected %d runtimes, got %d", len(d.cfg.Agents), len(runtimes))
	}
	if got := atomic.LoadInt32(&maxInFlight); got < 2 {
		t.Fatalf("probes did not overlap (peak in-flight = %d); registration is still serial", got)
	}
	if serialFloor := time.Duration(len(d.cfg.Agents)) * probeBlock; elapsed >= serialFloor {
		t.Fatalf("detectBuiltinRuntimes took %v (>= serial floor %v); not parallel", elapsed, serialFloor)
	}

	for i := 1; i < len(runtimes); i++ {
		if runtimes[i-1]["type"] > runtimes[i]["type"] {
			t.Fatalf("runtimes not sorted by type: %q before %q", runtimes[i-1]["type"], runtimes[i]["type"])
		}
	}
}

func TestDetectBuiltinRuntimes_SkipsFailedProbes(t *testing.T) {

	stubProbeRetry(t, time.Millisecond, time.Second)
	origDetect := detectAgentVersion
	origCheck := checkAgentMinVersion
	t.Cleanup(func() {
		detectAgentVersion = origDetect
		checkAgentMinVersion = origCheck
	})

	detectAgentVersion = func(_ context.Context, path string) (string, error) {
		if path == "/broken" {
			return "", context.DeadlineExceeded
		}
		return "9.9.9", nil
	}
	checkAgentMinVersion = func(provider, _ string) error {
		if provider == "tooold" {
			return context.Canceled
		}
		return nil
	}

	d := freshDaemon("")
	d.cfg.Agents = map[string]AgentEntry{
		"runtime-c": {Path: "/usr/bin/true"},
		"runtime-e": {Path: "/usr/bin/true"},
		"broken":    {Path: "/broken"},
		"tooold":    {Path: "/usr/bin/true"},
	}

	runtimes := d.detectBuiltinRuntimes(context.Background())
	got := map[string]bool{}
	for _, rt := range runtimes {
		got[rt["type"]] = true
	}
	if len(runtimes) != 2 || !got["runtime-c"] || !got["runtime-e"] {
		t.Fatalf("expected only claude+codex to register, got %v", runtimes)
	}
}
