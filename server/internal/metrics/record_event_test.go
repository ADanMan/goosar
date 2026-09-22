package metrics_test

import (
	"testing"

	"github.com/adanman/goosar/server/internal/analytics"
	"github.com/adanman/goosar/server/internal/metrics"
)

type captureSpy struct{ names []string }

func (c *captureSpy) Capture(e analytics.Event) { c.names = append(c.names, e.Name) }
func (c *captureSpy) Close()                    {}

func TestRecordEventSkipsPostHogForMetricsOnly(t *testing.T) {
	spy := &captureSpy{}
	m := metrics.NewBusinessMetrics()

	before := metrics.SumAllCounters(m)
	metrics.RecordEvent(spy, m, analytics.RuntimeOffline("user-1", "ws-1", "rt-1", "daemon-1", "runtime-c"))
	if len(spy.names) != 0 {
		t.Fatalf("runtime_offline shipped %d events to PostHog, want 0: %v", len(spy.names), spy.names)
	}
	if metrics.SumAllCounters(m) <= before {
		t.Fatalf("runtime_offline did not increment a Prometheus counter")
	}

	before = metrics.SumAllCounters(m)
	metrics.RecordEvent(spy, m, analytics.WorkspaceCreated("user-1", "ws-1"))
	if len(spy.names) != 0 {
		t.Fatalf("workspace_created shipped to PostHog, want 0 (metrics-only since MUL-4127): %v", spy.names)
	}
	if metrics.SumAllCounters(m) <= before {
		t.Fatalf("workspace_created did not increment a Prometheus counter")
	}

	metrics.RecordEvent(spy, m, analytics.Event{Name: "frontend_only_probe", DistinctID: "user-1"})
	if len(spy.names) != 1 || spy.names[0] != "frontend_only_probe" {
		t.Fatalf("a non-metrics-only event should ship to PostHog exactly once: %v", spy.names)
	}
}
