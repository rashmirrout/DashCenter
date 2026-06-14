// metrics.go — Prometheus instrumentation for the dashw topology hub.
//
// Namespace: dashw_topology_*
//
// Registration uses the global prometheus.DefaultRegisterer so the
// existing /metrics endpoint (router.go) exposes them automatically.
// Tests construct hubs without panicking on duplicate registration
// because promauto registers via sync.Once.
package cluster

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type hubMetricSet struct {
	watchers          *prometheus.GaugeVec
	subscribeRejected *prometheus.CounterVec

	published *prometheus.CounterVec
	delivered *prometheus.CounterVec
	dropped   *prometheus.CounterVec

	snapshotCacheHits   *prometheus.CounterVec
	snapshotCacheMisses *prometheus.CounterVec

	upstreamConnected  *prometheus.GaugeVec
	upstreamReconnects *prometheus.CounterVec
}

var (
	hubMetricsOnce sync.Once
	hubMetrics     hubMetricSet
)

func init() {
	hubMetricsOnce.Do(func() {
		hubMetrics.watchers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "watchers",
			Help:      "Live downstream SSE/WS watchers on this dashw replica.",
		}, []string{})
		hubMetrics.subscribeRejected = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "subscribe_rejected_total",
			Help:      "Subscribe attempts rejected by cap. reason: global | per_ip.",
		}, []string{"reason"})

		hubMetrics.published = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "events_published_total",
			Help:      "Events flowing into the hub from the upstream stream, labelled by kind.",
		}, []string{"kind"})
		hubMetrics.delivered = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "events_delivered_total",
			Help:      "Events successfully written to a downstream watcher channel.",
		}, []string{})
		hubMetrics.dropped = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "events_dropped_total",
			Help:      "Events not delivered, labelled by reason (buffer_full | resume_replay | marshal_error).",
		}, []string{"reason"})

		hubMetrics.snapshotCacheHits = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "snapshot_cache_hits_total",
			Help:      "GetTopology requests served from the in-process cache.",
		}, []string{})
		hubMetrics.snapshotCacheMisses = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "snapshot_cache_misses_total",
			Help:      "GetTopology requests that triggered an upstream call.",
		}, []string{})

		hubMetrics.upstreamConnected = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "upstream_connected",
			Help:      "1 if the hub's upstream gRPC WatchTopology stream is open, 0 otherwise.",
		}, []string{})
		hubMetrics.upstreamReconnects = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashw_topology",
			Subsystem: "hub",
			Name:      "upstream_reconnects_total",
			Help:      "Count of upstream stream reconnect cycles.",
		}, []string{})
	})
}
