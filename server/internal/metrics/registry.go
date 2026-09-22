package metrics

import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/adanman/goosar/server/internal/daemonws"
	"github.com/adanman/goosar/server/internal/realtime"
)

type RegistryOptions struct {
	Pool     *pgxpool.Pool
	Realtime *realtime.Metrics
	DaemonWS *daemonws.Metrics
	Version  string
	Commit   string

	BusinessSampler *BusinessSamplerOptions

	Readiness ReadinessFunc
}

type Registry struct {
	Gatherer     prometheus.Gatherer
	HTTP         *HTTPMetrics
	Business     *BusinessMetrics
	ChannelMedia *ChannelMediaReconcilerMetrics

	Sampler *BusinessSamplerCollector
}

func NewRegistry(opts RegistryOptions) *Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "goosar_build_info",
		Help: "Build information for the Goosar server binary.",
	}, []string{"version", "commit"})
	buildInfo.WithLabelValues(defaultLabel(opts.Version, "dev"), defaultLabel(opts.Commit, "unknown")).Set(1)
	reg.MustRegister(buildInfo)

	httpMetrics := NewHTTPMetrics()
	reg.MustRegister(httpMetrics.Collectors()...)

	businessMetrics := NewBusinessMetrics()
	reg.MustRegister(businessMetrics.Collectors()...)

	channelMedia := NewChannelMediaReconcilerMetrics()
	reg.MustRegister(channelMedia.Collectors()...)

	if opts.Pool != nil {
		reg.MustRegister(NewDBCollector(opts.Pool))
	}
	if opts.Realtime != nil {
		reg.MustRegister(NewRealtimeCollector(opts.Realtime))
	}
	if opts.DaemonWS != nil {
		reg.MustRegister(NewDaemonWSCollector(opts.DaemonWS))
	}

	if readiness := NewReadinessCollector(opts.Readiness); readiness != nil {
		reg.MustRegister(readiness)
	}

	sampler := NewBusinessSamplerCollector(opts.BusinessSampler)
	if sampler != nil {
		reg.MustRegister(sampler.Collectors()...)
	}

	return &Registry{
		Gatherer:     reg,
		HTTP:         httpMetrics,
		Business:     businessMetrics,
		ChannelMedia: channelMedia,
		Sampler:      sampler,
	}
}

func defaultLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
