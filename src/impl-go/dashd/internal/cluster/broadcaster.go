// broadcaster.go — server-streaming fan-out for ClusterService.WatchTopology.
//
// Pattern mirrors internal/ha/orchestrator/broadcaster.go (PC-G3): per-
// subscriber buffered channel, drop-on-full, never blocks the producer.
// A slow client loses events and the operator-visible drop counter goes
// up — that's the documented contract.
//
// Producers (registry, inventory, elector observer) all funnel into one
// Publish call. The broadcaster is otherwise stateless.
package cluster

import (
	"sync"
	"sync/atomic"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// defaultSubBuffer caps each subscriber's pending-event buffer.
const defaultSubBuffer = 64

// Broadcaster owns the WatchTopology fan-out for one dashd process.
// Construct with NewBroadcaster; ClusterService owns one.
type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[*subscription]struct{}
	totalSent   atomic.Uint64
	totalDrop   atomic.Uint64
}

type subscription struct {
	ch       chan *dashcenterv1.TopologyEvent
	dropped  atomic.Uint64
	closedMu sync.Mutex
	closed   bool
}

// NewBroadcaster returns an empty Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subscribers: map[*subscription]struct{}{}}
}

// Subscribe registers a new stream. Returns the receive channel and a
// cancel function that MUST be called to release resources (typically
// in the gRPC handler's defer).
func (b *Broadcaster) Subscribe() (<-chan *dashcenterv1.TopologyEvent, func()) {
	sub := &subscription{ch: make(chan *dashcenterv1.TopologyEvent, defaultSubBuffer)}
	b.mu.Lock()
	b.subscribers[sub] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		sub.closedMu.Lock()
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		sub.closedMu.Unlock()
		b.mu.Lock()
		delete(b.subscribers, sub)
		b.mu.Unlock()
	}
	return sub.ch, cancel
}

// Publish fans the event out to every subscriber. Non-blocking: a
// subscriber whose buffer is full loses the event and its dropped
// counter increments.
func (b *Broadcaster) Publish(ev *dashcenterv1.TopologyEvent) {
	if ev == nil {
		return
	}
	b.mu.Lock()
	subs := make([]*subscription, 0, len(b.subscribers))
	for s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.Unlock()

	for _, sub := range subs {
		sub.closedMu.Lock()
		if sub.closed {
			sub.closedMu.Unlock()
			continue
		}
		select {
		case sub.ch <- ev:
			b.totalSent.Add(1)
		default:
			sub.dropped.Add(1)
			b.totalDrop.Add(1)
		}
		sub.closedMu.Unlock()
	}
}

// Stats is a snapshot of broadcaster activity for /admin/health or
// Prometheus.
type Stats struct {
	Subscribers int
	TotalSent   uint64
	TotalDrop   uint64
}

// Stats returns a snapshot.
func (b *Broadcaster) Stats() Stats {
	b.mu.Lock()
	n := len(b.subscribers)
	b.mu.Unlock()
	return Stats{
		Subscribers: n,
		TotalSent:   b.totalSent.Load(),
		TotalDrop:   b.totalDrop.Load(),
	}
}
