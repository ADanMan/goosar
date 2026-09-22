package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const readinessScrapeTimeout = 3 * time.Second

type ReadinessFunc func(ctx context.Context) (ready bool, migrationsOutOfDate bool)

type ReadinessCollector struct {
	check ReadinessFunc

	descReady      *prometheus.Desc
	descMigrations *prometheus.Desc
}

func NewReadinessCollector(check ReadinessFunc) *ReadinessCollector {
	if check == nil {
		return nil
	}
	return &ReadinessCollector{
		check: check,
		descReady: prometheus.NewDesc(
			"goosar_ready",
			"1 when the readiness check passes (same verdict as /readyz), 0 otherwise.",
			nil, nil),
		descMigrations: prometheus.NewDesc(
			"goosar_migrations_out_of_date",
			"1 when the database is missing migrations this binary requires, 0 otherwise.",
			nil, nil),
	}
}

func (c *ReadinessCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.descReady
	ch <- c.descMigrations
}

func (c *ReadinessCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), readinessScrapeTimeout)
	defer cancel()

	ready, outOfDate := c.check(ctx)
	ch <- prometheus.MustNewConstMetric(c.descReady, prometheus.GaugeValue, boolGauge(ready))
	ch <- prometheus.MustNewConstMetric(c.descMigrations, prometheus.GaugeValue, boolGauge(outOfDate))
}

func boolGauge(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
