// Broadcaster fan-out for HA events. PC-G3 WatchHaEvents subscribers
// register here; every Orchestrator role transition publishes to all
// active subscribers.
package orchestrator

import (
	"sync"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// defaultSubBuffer caps each subscriber's pending-event buffer. A
// subscriber that fills its buffer is silently dropped — dashd never
// blocks the orchestrator on a stuck WatchHaEvents client.
const defaultSubBuffer = 32

// Broadcaster owns the per-orchestrator event fan-out. Construct with
// NewBroadcaster; Orchestrator owns one of these and re-exposes it via
// Orchestrator.Broadcaster().
type Broadcaster struct {
	mu          sync.Mutex
	subscribers map[*subscription]struct{}
}

// subscription is one WatchHaEvents stream's local state.
type subscription struct {
	ch       chan dashcenterv1.HaEvent
	filter   Filter
	dropped  int
	closedMu sync.Mutex
	closed   bool
}

// Filter narrows the events a subscriber receives. All fields are
// AND-combined; an empty list means "no filter on this dimension".
type Filter struct {
	Namespaces []string
	HaSetNames []string
	Types      []dashcenterv1.HaEvent_Type
}

func (f Filter) accept(e dashcenterv1.HaEvent) bool {
	if len(f.Namespaces) > 0 && !contains(f.Namespaces, e.Namespace) {
		return false
	}
	if len(f.HaSetNames) > 0 && !contains(f.HaSetNames, e.HaSetName) {
		return false
	}
	if len(f.Types) > 0 {
		ok := false
		for _, t := range f.Types {
			if t == e.Type {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// NewBroadcaster returns an empty Broadcaster.
func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subscribers: map[*subscription]struct{}{}}
}

// Subscribe registers a new event stream. Returns the receive channel
// and a cancel function that must be called to release resources.
func (b *Broadcaster) Subscribe(filter Filter) (<-chan dashcenterv1.HaEvent, func()) {
	sub := &subscription{
		ch:     make(chan dashcenterv1.HaEvent, defaultSubBuffer),
		filter: filter,
	}
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

// Publish fans the event out to every matching subscriber. Non-blocking:
// a slow subscriber that has filled its buffer simply has the event
// dropped (sub.dropped++) and the broadcaster moves on.
func (b *Broadcaster) Publish(e dashcenterv1.HaEvent) {
	b.mu.Lock()
	subs := make([]*subscription, 0, len(b.subscribers))
	for s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.Unlock()
	for _, sub := range subs {
		if !sub.filter.accept(e) {
			continue
		}
		sub.closedMu.Lock()
		if sub.closed {
			sub.closedMu.Unlock()
			continue
		}
		select {
		case sub.ch <- e:
		default:
			sub.dropped++
		}
		sub.closedMu.Unlock()
	}
}

// Count returns the live subscriber count. Used by tests and by
// /admin/health for observability.
func (b *Broadcaster) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subscribers)
}
