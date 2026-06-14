// metrics.go — Prometheus instrumentation for the counter Hub
// (mirrors console/internal/cluster/metrics.go shape under a sibling
// namespace).

package observability

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type hubMetricSet struct {
	watchers           *prometheus.GaugeVec
	upstreamConnected  *prometheus.GaugeVec
	upstreamReconnects *prometheus.CounterVec
	subscribeRejected  *prometheus.CounterVec

	published  *prometheus.CounterVec
	delivered  *prometheus.CounterVec
	dropped    *prometheus.CounterVec
}

var (
	hubMetricsOnce sync.Once
	hubMetrics     hubMetricSet
)

func init() {
	hubMetricsOnce.Do(func() {
		hubMetrics.watchers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "watchers",
			Help:      "Number of live counter watchers on this dashw replica.",
		}, []string{})
		hubMetrics.upstreamConnected = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "upstream_connected",
			Help:      "Number of currently-connected upstream gRPC GetCounters streams.",
		}, []string{})
		hubMetrics.upstreamReconnects = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "upstream_reconnects_total",
			Help:      "Upstream GetCounters stream reconnect attempts.",
		}, []string{})
		hubMetrics.subscribeRejected = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "subscribe_rejected_total",
			Help:      "Subscribe attempts rejected by cap. reason: global | per_ip.",
		}, []string{"reason"})
		hubMetrics.published = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "events_published_total",
			Help:      "CounterEvents accepted by the hub, labelled by kind.",
		}, []string{"kind"})
		hubMetrics.delivered = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "events_delivered_total",
			Help:      "CounterEvents successfully written to a watcher channel.",
		}, []string{})
		hubMetrics.dropped = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_observability",
			Subsystem: "hub",
			Name:      "events_dropped_total",
			Help:      "CounterEvents that could not be delivered, labelled by reason (buffer_full | resume_replay | marshal_error).",
		}, []string{"reason"})
	})
}
