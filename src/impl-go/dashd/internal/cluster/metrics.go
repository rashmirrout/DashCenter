// metrics.go — Prometheus instrumentation for ClusterService.
//
// All metrics use the `dashd_cluster_*` namespace so operators can
// route alerts via Grafana / Alertmanager dashboards without
// collision with other dashd surfaces.
//
// Registration is via the global prometheus.DefaultRegisterer to keep
// the admin port's /admin/metrics handler simple; dashd's PD-G2 plan
// already wires that endpoint. A future PR can swap in a custom
// registry if multi-tenancy is needed.
package cluster

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// clusterMetricSet is the bundle of counters / gauges instrumented
// from broadcaster.go + registry.go.
type clusterMetricSet struct {
	// subscribers gauge — live WatchTopology streams.
	subscribers *prometheus.GaugeVec
	// maxSubscribers gauge — operator-visible cap.
	maxSubscribers *prometheus.GaugeVec
	// subscribeRejected counter — labelled by reason (global / per_subject).
	subscribeRejected *prometheus.CounterVec

	// published counter — events fed into the broadcaster, labelled by kind.
	published *prometheus.CounterVec
	// delivered counter — events successfully sent to a subscriber.
	delivered *prometheus.CounterVec
	// dropped counter — events not delivered, labelled by reason
	// (buffer_full / resume_replay / marshal_error).
	dropped *prometheus.CounterVec
	// coalesced counter — events merged in a coalescing window.
	coalesced *prometheus.CounterVec
	// suppressed counter — events the leaky bucket discarded.
	suppressed *prometheus.CounterVec

	// peers gauge — registry-tracked peer count.
	peers *prometheus.GaugeVec
	// registryChanges counter — peer add/remove/update events.
	registryChanges *prometheus.CounterVec
}

var (
	clusterMetricsOnce sync.Once
	clusterMetrics     clusterMetricSet
)

func init() {
	// Lazy: defer to first init() of the cluster package which is
	// guaranteed once per process. We don't want registration in the
	// broadcaster constructor path because tests construct many
	// broadcasters per process.
	clusterMetricsOnce.Do(func() {
		clusterMetrics.subscribers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "subscribers",
			Help:      "Number of live WatchTopology subscribers on this dashd.",
		}, []string{})

		clusterMetrics.maxSubscribers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "max_subscribers",
			Help:      "Configured per-process subscriber cap.",
		}, []string{})

		clusterMetrics.subscribeRejected = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "subscribe_rejected_total",
			Help:      "Subscribe attempts rejected by cap. reason: global | per_subject.",
		}, []string{"reason"})

		clusterMetrics.published = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "events_published_total",
			Help:      "Events accepted by the broadcaster (post-coalesce, post-rate-limit), labelled by kind.",
		}, []string{"kind"})

		clusterMetrics.delivered = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "events_delivered_total",
			Help:      "Events successfully written to a subscriber channel, labelled by kind.",
		}, []string{"kind"})

		clusterMetrics.dropped = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "events_dropped_total",
			Help:      "Events that could not be delivered, labelled by reason (buffer_full | resume_replay | marshal_error).",
		}, []string{"reason"})

		clusterMetrics.coalesced = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "events_coalesced_total",
			Help:      "Events merged with an earlier event in the coalescing window.",
		}, []string{})

		clusterMetrics.suppressed = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "broadcaster",
			Name:      "events_suppressed_total",
			Help:      "Events discarded by the leaky-bucket rate limiter.",
		}, []string{})

		clusterMetrics.peers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashd_cluster",
			Subsystem: "registry",
			Name:      "peers",
			Help:      "Number of dashd peers visible to this node's registry (including self).",
		}, []string{})

		clusterMetrics.registryChanges = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_cluster",
			Subsystem: "registry",
			Name:      "changes_total",
			Help:      "Peer registry changes seen by this node, labelled by kind (added | removed | updated).",
		}, []string{"kind"})
	})
}

// ObservePeerCount updates the registry peer gauge.
func ObservePeerCount(n int) {
	clusterMetrics.peers.WithLabelValues().Set(float64(n))
}

// ObserveRegistryChange increments the registry-change counter.
func ObserveRegistryChange(kind ChangeKind) {
	clusterMetrics.registryChanges.WithLabelValues(kind.String()).Inc()
}
