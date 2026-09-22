// Сэмплер бизнес-метрик: работает в момент scrape /metrics, ходит в реплику
// чтения, включается явно. Каждый запрос — в короткой read-only транзакции
// с statement_timeout 500ms и LIMIT 100.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultSamplerCacheTTL     = 8 * time.Second
	defaultSamplerQueryTimeout = 500 * time.Millisecond
	samplerRowLimit            = 100

	windowFiveMinutes = "5m"

	runtimeOnlineWindowSeconds = 60

	stuckRunningInterval = "30 minutes"
)

var samplerWindows = []struct {
	label string
	d     time.Duration
}{
	{windowFiveMinutes, 5 * time.Minute},
}

type BusinessSamplerOptions struct {
	Pool *pgxpool.Pool

	CacheTTL time.Duration

	QueryTimeout time.Duration
}

type samplerQuerier interface {
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
}

type BusinessSamplerCollector struct {
	pool         samplerQuerier
	cacheTTL     time.Duration
	queryTimeout time.Duration
	now          func() time.Time
	logger       *slog.Logger

	refreshFn func(ctx context.Context, now time.Time) *samplerSnapshot

	queryDuration *prometheus.HistogramVec
	queryErrors   *prometheus.CounterVec

	descActiveUsers      *prometheus.Desc
	descActiveWorkspaces *prometheus.Desc
	descTaskQueued       *prometheus.Desc
	descTaskRunning      *prometheus.Desc
	descTaskStuck        *prometheus.Desc
	descRuntimeOnline    *prometheus.Desc
	descHeartbeatAgeHist *prometheus.Desc
	descWorkspaceTotal   *prometheus.Desc

	mu       sync.Mutex
	snapshot *samplerSnapshot
}

func NewBusinessSamplerCollector(opts *BusinessSamplerOptions) *BusinessSamplerCollector {
	if opts == nil || opts.Pool == nil {
		return nil
	}
	cacheTTL := opts.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultSamplerCacheTTL
	}
	queryTimeout := opts.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = defaultSamplerQueryTimeout
	}
	c := &BusinessSamplerCollector{
		pool:         opts.Pool,
		cacheTTL:     cacheTTL,
		queryTimeout: queryTimeout,
		now:          time.Now,
		logger:       slog.Default(),

		queryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "goosar",
			Subsystem: "business_sampler",
			Name:      "query_seconds",
			Help:      "Per-query duration of the BusinessSamplerCollector. The `name` label is one of the fixed query identifiers and never user-controlled.",
			Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"name"}),
		queryErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "goosar",
			Subsystem: "business_sampler",
			Name:      "query_errors_total",
			Help:      "Per-query error count. Includes statement_timeout cancellations, which are the expected outcome of a hung database and the smoking gun for SET LOCAL working as intended.",
		}, []string{"name"}),

		descActiveUsers: prometheus.NewDesc(
			"goosar_active_users",
			"Distinct users with chat / task activity in the rolling window. Sampled from the database; stale up to the sampler cache TTL.",
			[]string{"window"}, nil),
		descActiveWorkspaces: prometheus.NewDesc(
			"goosar_active_workspaces",
			"Distinct workspaces with chat / task activity in the rolling window. Sampled from the database.",
			[]string{"window"}, nil),
		descTaskQueued: prometheus.NewDesc(
			"goosar_agent_task_queued",
			"Current agent_task_queue rows in `queued` status by inferred source. Sampled from the database.",
			[]string{"source"}, nil),
		descTaskRunning: prometheus.NewDesc(
			"goosar_agent_task_running",
			"Current agent_task_queue rows in `dispatched` or `running` status by inferred source and runtime mode. Sampled from the database.",
			[]string{"source", "runtime_mode"}, nil),
		descTaskStuck: prometheus.NewDesc(
			"goosar_agent_task_stuck_total",
			"Current `running` agent_task_queue rows whose started_at is older than the stuck threshold. Sampled from the database.",
			[]string{"source"}, nil),
		descRuntimeOnline: prometheus.NewDesc(
			"goosar_runtime_online",
			"Count of agent_runtime rows with last_seen_at within the online heartbeat window. Sampled from the database.",
			[]string{"runtime_mode", "provider"}, nil),
		descHeartbeatAgeHist: prometheus.NewDesc(
			"goosar_runtime_heartbeat_age_seconds",
			"Distribution of (now() - agent_runtime.last_seen_at) for runtimes considered online by the sampler.",
			[]string{"runtime_mode"}, nil),
		descWorkspaceTotal: prometheus.NewDesc(
			"goosar_workspace_total",
			"Lifetime workspace row count. Useful for sizing alerts and dashboards.",
			nil, nil),
	}
	c.refreshFn = c.refreshFromDB
	return c
}

func (c *BusinessSamplerCollector) Collectors() []prometheus.Collector {
	if c == nil {
		return nil
	}
	return []prometheus.Collector{c, c.queryDuration, c.queryErrors}
}

func (c *BusinessSamplerCollector) Describe(ch chan<- *prometheus.Desc) {
	if c == nil {
		return
	}
	for _, d := range []*prometheus.Desc{
		c.descActiveUsers,
		c.descActiveWorkspaces,
		c.descTaskQueued,
		c.descTaskRunning,
		c.descTaskStuck,
		c.descRuntimeOnline,
		c.descHeartbeatAgeHist,
		c.descWorkspaceTotal,
	} {
		ch <- d
	}
}

func (c *BusinessSamplerCollector) Collect(ch chan<- prometheus.Metric) {
	if c == nil || c.pool == nil {
		return
	}
	snap := c.maybeRefresh()
	if snap == nil {
		return
	}
	c.emit(ch, snap)
}

func (c *BusinessSamplerCollector) maybeRefresh() *samplerSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	if c.snapshot != nil && now.Sub(c.snapshot.takenAt) < c.cacheTTL {
		return c.snapshot
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*c.queryTimeout)
	defer cancel()

	next := c.refreshFn(ctx, now)
	if next != nil {
		c.snapshot = next
	}
	return c.snapshot
}

func (c *BusinessSamplerCollector) emit(ch chan<- prometheus.Metric, snap *samplerSnapshot) {
	for _, w := range samplerWindows {
		ch <- prometheus.MustNewConstMetric(
			c.descActiveUsers, prometheus.GaugeValue, snap.activeUsers[w.label], w.label)
		ch <- prometheus.MustNewConstMetric(
			c.descActiveWorkspaces, prometheus.GaugeValue, snap.activeWorkspaces[w.label], w.label)
	}

	for _, source := range knownSourceLabels() {
		ch <- prometheus.MustNewConstMetric(
			c.descTaskQueued, prometheus.GaugeValue, snap.taskQueued[source], source)
		ch <- prometheus.MustNewConstMetric(
			c.descTaskStuck, prometheus.GaugeValue, snap.taskStuck[source], source)
	}
	for _, source := range knownSourceLabels() {
		for _, mode := range knownRuntimeModeLabels() {
			key := taskRunningKey{source: source, runtimeMode: mode}
			ch <- prometheus.MustNewConstMetric(
				c.descTaskRunning, prometheus.GaugeValue, snap.taskRunning[key], source, mode)
		}
	}

	for key, val := range snap.runtimeOnline {
		ch <- prometheus.MustNewConstMetric(
			c.descRuntimeOnline, prometheus.GaugeValue, val, key.runtimeMode, key.provider)
	}

	for mode, hist := range snap.heartbeatAge {
		ch <- prometheus.MustNewConstHistogram(
			c.descHeartbeatAgeHist,
			hist.count,
			hist.sum,
			hist.buckets,
			mode,
		)
	}

	if snap.workspaceTotalKnown {
		ch <- prometheus.MustNewConstMetric(
			c.descWorkspaceTotal, prometheus.GaugeValue, snap.workspaceTotal)
	}
}

func knownSourceLabels() []string {
	return []string{"chat", "issue", "autopilot", "autopilot_issue", "quick_create", "manual", "api", "other"}
}

func knownRuntimeModeLabels() []string {
	return []string{"local", "cloud", "unknown"}
}

var heartbeatAgeBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600}

type taskRunningKey struct {
	source      string
	runtimeMode string
}

type runtimeOnlineKey struct {
	runtimeMode string
	provider    string
}

type samplerHistogram struct {
	count   uint64
	sum     float64
	buckets map[float64]uint64
}

type samplerSnapshot struct {
	takenAt time.Time

	activeUsers      map[string]float64
	activeWorkspaces map[string]float64

	taskQueued  map[string]float64
	taskRunning map[taskRunningKey]float64
	taskStuck   map[string]float64

	runtimeOnline map[runtimeOnlineKey]float64
	heartbeatAge  map[string]samplerHistogram

	workspaceTotal      float64
	workspaceTotalKnown bool
}

func newSamplerSnapshot(t time.Time) *samplerSnapshot {
	return &samplerSnapshot{
		takenAt:          t,
		activeUsers:      map[string]float64{},
		activeWorkspaces: map[string]float64{},
		taskQueued:       map[string]float64{},
		taskRunning:      map[taskRunningKey]float64{},
		taskStuck:        map[string]float64{},
		runtimeOnline:    map[runtimeOnlineKey]float64{},
		heartbeatAge:     map[string]samplerHistogram{},
	}
}

func (c *BusinessSamplerCollector) refreshFromDB(ctx context.Context, now time.Time) *samplerSnapshot {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		c.queryErrors.WithLabelValues("acquire").Inc()
		c.logger.Warn("business sampler: acquire connection failed", "error", err)

		return c.snapshot
	}
	defer conn.Release()

	snap := newSamplerSnapshot(now)

	c.runQuery(ctx, conn, "active_users", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryActiveUsers(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "active_workspaces", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryActiveWorkspaces(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "task_queued", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryTaskQueued(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "task_running", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryTaskRunning(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "task_stuck", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryTaskStuck(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "runtime_online", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryRuntimeOnline(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "runtime_heartbeat_age", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryRuntimeHeartbeatAge(ctx, tx, snap)
	})
	c.runQuery(ctx, conn, "workspace_total", func(ctx context.Context, tx pgx.Tx) error {
		return c.queryWorkspaceTotal(ctx, tx, snap)
	})

	return snap
}

func (c *BusinessSamplerCollector) runQuery(
	ctx context.Context,
	conn *pgxpool.Conn,
	name string,
	body func(ctx context.Context, tx pgx.Tx) error,
) {
	queryCtx, cancel := context.WithTimeout(ctx, c.queryTimeout+50*time.Millisecond)
	defer cancel()

	start := c.now()
	defer func() {
		c.queryDuration.WithLabelValues(name).Observe(c.now().Sub(start).Seconds())
	}()

	tx, err := conn.BeginTx(queryCtx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
		IsoLevel:   pgx.ReadCommitted,
	})
	if err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		c.logger.Warn("business sampler: begin tx failed", "name", name, "error", err)
		return
	}
	committed := false
	defer func() {
		if !committed {

			_ = tx.Rollback(context.Background())
		}
	}()

	timeoutMs := int(c.queryTimeout / time.Millisecond)
	if _, err := tx.Exec(queryCtx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs)); err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		c.logger.Warn("business sampler: SET LOCAL statement_timeout failed", "name", name, "error", err)
		return
	}

	if err := body(queryCtx, tx); err != nil {
		c.queryErrors.WithLabelValues(name).Inc()

		level := slog.LevelWarn
		if isStatementTimeout(err) {
			level = slog.LevelInfo
		}
		c.logger.Log(ctx, level, "business sampler: query failed", "name", name, "error", err)
		return
	}

	if err := tx.Commit(queryCtx); err != nil {
		c.queryErrors.WithLabelValues(name).Inc()
		c.logger.Warn("business sampler: commit failed", "name", name, "error", err)
		return
	}
	committed = true
}

func isStatementTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := err.Error()
	return contains(msg, "57014") || contains(msg, "canceling statement due to statement timeout")
}

func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
