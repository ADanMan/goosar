package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestSampler(t *testing.T, refresh func(ctx context.Context, now time.Time) *samplerSnapshot) *BusinessSamplerCollector {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://disabled@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("create dummy pool: %v", err)
	}
	t.Cleanup(pool.Close)

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{
		Pool:         pool,
		CacheTTL:     50 * time.Millisecond,
		QueryTimeout: 100 * time.Millisecond,
	})
	if c == nil {
		t.Fatalf("NewBusinessSamplerCollector returned nil")
	}
	c.refreshFn = refresh
	return c
}

func filledSnapshot(now time.Time) *samplerSnapshot {
	snap := newSamplerSnapshot(now)
	snap.activeUsers[windowFiveMinutes] = 7
	snap.activeWorkspaces[windowFiveMinutes] = 3

	snap.taskQueued["chat"] = 5
	snap.taskQueued["issue"] = 2

	snap.taskRunning[taskRunningKey{source: "chat", runtimeMode: "cloud"}] = 3
	snap.taskRunning[taskRunningKey{source: "issue", runtimeMode: "local"}] = 1

	snap.taskStuck["issue"] = 1

	snap.runtimeOnline[runtimeOnlineKey{runtimeMode: "local", provider: "runtime-c"}] = 4
	snap.runtimeOnline[runtimeOnlineKey{runtimeMode: "cloud", provider: "runtime-l"}] = 2

	snap.heartbeatAge["local"] = samplerHistogram{
		count:   3,
		sum:     45,
		buckets: bucketsFor([]float64{5, 15, 30}),
	}
	snap.heartbeatAge["cloud"] = samplerHistogram{
		count:   1,
		sum:     2,
		buckets: bucketsFor([]float64{2}),
	}

	snap.workspaceTotal = 250
	snap.workspaceTotalKnown = true
	return snap
}

func bucketsFor(observations []float64) map[float64]uint64 {
	buckets := make(map[float64]uint64, len(heartbeatAgeBuckets))
	for _, b := range heartbeatAgeBuckets {
		buckets[b] = 0
	}
	for _, o := range observations {
		for _, b := range heartbeatAgeBuckets {
			if o <= b {
				buckets[b]++
			}
		}
	}
	return buckets
}

func TestBusinessSamplerCollectorEmitsExpectedMetrics(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		return filledSnapshot(refreshAt)
	})
	c.now = func() time.Time { return now }

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	rec := httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	wantSubstrings := []string{
		`goosar_active_users{window="5m"} 7`,
		`goosar_active_workspaces{window="5m"} 3`,
		`goosar_agent_task_queued{source="chat"} 5`,
		`goosar_agent_task_queued{source="issue"} 2`,

		`goosar_agent_task_queued{source="autopilot"} 0`,
		`goosar_agent_task_queued{source="other"} 0`,
		`goosar_agent_task_running{runtime_mode="cloud",source="chat"} 3`,
		`goosar_agent_task_running{runtime_mode="local",source="issue"} 1`,
		`goosar_agent_task_stuck_total{source="issue"} 1`,
		`goosar_runtime_online{provider="runtime-c",runtime_mode="local"} 4`,
		`goosar_runtime_online{provider="runtime-l",runtime_mode="cloud"} 2`,
		`goosar_runtime_heartbeat_age_seconds_count{runtime_mode="local"} 3`,
		`goosar_runtime_heartbeat_age_seconds_sum{runtime_mode="local"} 45`,
		`goosar_workspace_total 250`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(body, want) {
			t.Errorf("metrics body missing %q\nbody:\n%s", want, body)
		}
	}
	for _, removed := range []string{
		`goosar_active_users{window="1h"}`,
		`goosar_active_users{window="24h"}`,
		`goosar_active_workspaces{window="1h"}`,
		`goosar_active_workspaces{window="24h"}`,
	} {
		if strings.Contains(body, removed) {
			t.Errorf("metrics body still exposes removed long DB window %q\nbody:\n%s", removed, body)
		}
	}
}

func TestBusinessSamplerSelfIntrospectionHistogramIsExposed(t *testing.T) {
	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		return newSamplerSnapshot(refreshAt)
	})
	c.queryDuration.WithLabelValues("active_users").Observe(0.012)

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	rec := httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `goosar_business_sampler_query_seconds_count{name="active_users"} 1`) {
		t.Fatalf("query duration histogram missing\n%s", body)
	}
}

func TestBusinessSamplerCollectorCachesSnapshot(t *testing.T) {
	var refreshCount atomic.Int32
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	current := now

	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {
		refreshCount.Add(1)
		return filledSnapshot(refreshAt)
	})
	c.cacheTTL = 100 * time.Millisecond
	c.now = func() time.Time { return current }

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	rec := httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	rec = httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if got := refreshCount.Load(); got != 1 {
		t.Fatalf("refresh count after two cached scrapes = %d, want 1", got)
	}

	current = current.Add(150 * time.Millisecond)
	rec = httptest.NewRecorder()
	NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if got := refreshCount.Load(); got != 2 {
		t.Fatalf("refresh count after TTL expiry = %d, want 2", got)
	}
}

func TestBusinessSamplerCollectorBoundedCardinality(t *testing.T) {
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	c := newTestSampler(t, func(ctx context.Context, refreshAt time.Time) *samplerSnapshot {

		snap := newSamplerSnapshot(refreshAt)
		for i := 0; i < 50; i++ {
			snap.taskQueued[NormalizeTaskSource("provider-from-user-input-"+string(rune('A'+i%26)))] += 1
		}
		for i := 0; i < 50; i++ {
			snap.runtimeOnline[runtimeOnlineKey{
				runtimeMode: NormalizeRuntimeMode("rogue-mode"),
				provider:    NormalizeRuntimeProvider("attacker-provider"),
			}] += 1
		}
		return snap
	})
	c.now = func() time.Time { return now }

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	if got := testutil.CollectAndCount(c, "goosar_agent_task_queued"); got != len(knownSourceLabels()) {
		t.Fatalf("agent_task_queued series = %d, want %d", got, len(knownSourceLabels()))
	}
	expectedRunning := len(knownSourceLabels()) * len(knownRuntimeModeLabels())
	if got := testutil.CollectAndCount(c, "goosar_agent_task_running"); got != expectedRunning {
		t.Fatalf("agent_task_running series = %d, want %d", got, expectedRunning)
	}
	if got := testutil.CollectAndCount(c, "goosar_runtime_online"); got != 1 {
		t.Fatalf("runtime_online series = %d, want 1 (collapsed by normalizers)", got)
	}
}

func TestBusinessSamplerCollectorDisabledWithoutOptions(t *testing.T) {
	registry := NewRegistry(RegistryOptions{})
	if registry.Sampler != nil {
		t.Fatalf("Sampler must be nil when BusinessSampler option is absent")
	}
	rec := httptest.NewRecorder()
	NewHandler(registry.Gatherer).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, forbidden := range []string{
		"goosar_active_users",
		"goosar_agent_task_queued",
		"goosar_runtime_online",
		"goosar_business_sampler_query_seconds",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("/metrics leaked sampler family %q when sampler disabled", forbidden)
		}
	}
}

func TestBusinessSamplerCollectorDBHangIsolation(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://hang@127.0.0.1:1/none?sslmode=disable")
	if err != nil {
		t.Fatalf("create unreachable pool: %v", err)
	}
	defer pool.Close()

	c := NewBusinessSamplerCollector(&BusinessSamplerOptions{
		Pool:         pool,
		CacheTTL:     time.Second,
		QueryTimeout: 50 * time.Millisecond,
	})
	if c == nil {
		t.Fatalf("NewBusinessSamplerCollector returned nil")
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(c.Collectors()...)

	done := make(chan struct{})
	go func() {
		rec := httptest.NewRecorder()
		NewHandler(registry).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		close(done)
	}()

	select {
	case <-done:

	case <-time.After(5 * time.Second):
		t.Fatal("metrics scrape blocked > 5s on unreachable DB")
	}

	if got := testutil.CollectAndCount(c.queryErrors); got == 0 {
		t.Fatal("expected at least one query_errors_total series after DB hang")
	}
}

func TestNewBusinessSamplerCollectorNilPool(t *testing.T) {
	if c := NewBusinessSamplerCollector(nil); c != nil {
		t.Fatalf("NewBusinessSamplerCollector(nil) = %p, want nil", c)
	}
	if c := NewBusinessSamplerCollector(&BusinessSamplerOptions{}); c != nil {
		t.Fatalf("NewBusinessSamplerCollector with nil Pool = %p, want nil", c)
	}
}

func TestSamplerHistogramBucketing(t *testing.T) {
	buckets := bucketsFor([]float64{0.5, 5, 30, 75})
	expectations := map[float64]uint64{
		1: 1, 5: 2, 15: 2, 30: 3, 60: 3, 120: 4, 300: 4, 600: 4,
	}
	for b, want := range expectations {
		if got := buckets[b]; got != want {
			t.Errorf("bucket le=%g count = %d, want %d", b, got, want)
		}
	}
}
