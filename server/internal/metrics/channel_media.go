package metrics

import "github.com/prometheus/client_golang/prometheus"

type ChannelMediaReconcilerMetrics struct {
	ObjectsDeleted prometheus.Counter
	RowsReferenced prometheus.Counter
	DeleteFailures prometheus.Counter
	Backlog        prometheus.Gauge
	Tombstones     prometheus.Gauge

	TombstoneReferenced prometheus.Counter
}

func NewChannelMediaReconcilerMetrics() *ChannelMediaReconcilerMetrics {
	return &ChannelMediaReconcilerMetrics{
		ObjectsDeleted: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "goosar",
			Subsystem: "channel_media",
			Name:      "reconciler_objects_deleted_total",
			Help:      "Unreferenced media objects deleted by the reconciler.",
		}),
		RowsReferenced: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "goosar",
			Subsystem: "channel_media",
			Name:      "reconciler_rows_referenced_total",
			Help:      "Ledger rows cleared because a durable attachment references the object.",
		}),
		DeleteFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "goosar",
			Subsystem: "channel_media",
			Name:      "reconciler_delete_failures_total",
			Help:      "Object-storage deletes that failed and were scheduled for retry.",
		}),
		Backlog: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "goosar",
			Subsystem: "channel_media",
			Name:      "pending_objects",
			Help:      "Live intent rows awaiting bind or reclaim (excludes tombstones).",
		}),
		Tombstones: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "goosar",
			Subsystem: "channel_media",
			Name:      "tombstoned_objects",
			Help:      "Deleted objects still tombstoned for scheduled re-deletion.",
		}),
		TombstoneReferenced: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "goosar",
			Subsystem: "channel_media",
			Name:      "reconciler_tombstone_referenced_total",
			Help:      "Tombstone passes that found an attachment referencing the object (invariant violation; the object is kept).",
		}),
	}
}

func (m *ChannelMediaReconcilerMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{m.ObjectsDeleted, m.RowsReferenced, m.DeleteFailures, m.Backlog, m.Tombstones, m.TombstoneReferenced}
}
