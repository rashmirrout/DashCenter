// metrics.go — Prometheus instrumentation for the counter broadcaster.
//
// All metrics use the `dashd_observability_broadcaster_*` namespace so
// operators can route alerts per-surface (cluster vs. observability)
// without label fan-out.
//
// Registration goes through prometheus.DefaultRegisterer at init time
// — production wires this into the existing /admin/metrics endpoint
// without per-call cost. A future cleanup row (T1.3 in
// recommended-postGA-cleanup.md) will extract the cluster+observability
// broadcaster pattern into a shared package; until then both packages
// register an identically-shaped metric set under different namespaces.
package broadcaster

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metricSet is the Prometheus bundle exported by the broadcaster.
type metricSet struct {
	subscribers       *prometheus.GaugeVec
	maxSubscribers    *prometheus.GaugeVec
	subscribeRejected *prometheus.CounterVec

	published  *prometheus.CounterVec
	delivered  *prometheus.CounterVec
	dropped    *prometheus.CounterVec
	coalesced  *prometheus.CounterVec
	suppressed *prometheus.CounterVec
}

var (
	metricsOnce sync.Once
	metrics     metricSet
)

func init() {
	metricsOnce.Do(func() {
		metrics.subscribers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "subscribers",
			Help:      "Number of live GetCounters subscribers on this dashd.",
		}, []string{})

		metrics.maxSubscribers = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "max_subscribers",
			Help:      "Configured per-process subscriber cap.",
		}, []string{})

		metrics.subscribeRejected = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "subscribe_rejected_total",
			Help:      "Subscribe attempts rejected by cap. reason: global | per_subject.",
		}, []string{"reason"})

		metrics.published = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "events_published_total",
			Help:      "CounterEvents accepted by the broadcaster (post-coalesce, post-rate-limit), labelled by kind.",
		}, []string{"kind"})

		metrics.delivered = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "events_delivered_total",
			Help:      "CounterEvents successfully written to a subscriber channel, labelled by kind.",
		}, []string{"kind"})

		metrics.dropped = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "events_dropped_total",
			Help:      "CounterEvents that could not be delivered, labelled by reason (buffer_full | resume_replay | marshal_error).",
		}, []string{"reason"})

		metrics.coalesced = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "events_coalesced_total",
			Help:      "KIND_REPORT events merged with an earlier event in the coalescing window.",
		}, []string{})

		metrics.suppressed = promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "dashd_observability",
			Subsystem: "broadcaster",
			Name:      "events_suppressed_total",
			Help:      "CounterEvents discarded by the leaky-bucket rate limiter.",
		}, []string{})
	})
}
