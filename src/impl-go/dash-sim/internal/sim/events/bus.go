// Package events is the fan-out bus that powers the dashsim.DashSim.Subscribe
// stream. Every mutation in the model package publishes an *dashsimv1.Event;
// each Subscribe call receives a fresh bounded channel.
//
// The bus is lock-free on the hot path: publishers grab an RLock to snapshot
// the subscriber set, then send non-blockingly to each subscriber's channel.
// Slow subscribers that fill their buffer simply lose events — they should
// reconnect with snapshot_first=true to resync.
package events

import (
	"sync"
	"sync/atomic"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
)

// DefaultBuffer is the per-subscriber channel capacity.
const DefaultBuffer = 256

// Subscription is a handle returned by Subscribe. Read from C; call Close when
// done.
type Subscription struct {
	C      <-chan *dashsimv1.Event
	closed atomic.Bool
	ch     chan *dashsimv1.Event
	bus    *Bus
	id     uint64
	kinds  map[dashsimv1.ObjectKind]struct{}
}

// Close removes the subscription from the bus. Idempotent.
func (s *Subscription) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.bus.unsubscribe(s.id)
	close(s.ch)
}

// matches reports whether an event with the given kind should be delivered.
// Empty filter == match all.
func (s *Subscription) matches(kind dashsimv1.ObjectKind) bool {
	if len(s.kinds) == 0 {
		return true
	}
	_, ok := s.kinds[kind]
	return ok
}

// Bus is the per-process pub/sub for simulator events.
type Bus struct {
	mu     sync.RWMutex
	nextID uint64
	subs   map[uint64]*Subscription

	// Dropped counts events that were discarded because a subscriber's
	// buffer was full. Exposed via the admin /admin/health endpoint.
	dropped atomic.Uint64
}

// New returns an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[uint64]*Subscription)}
}

// Subscribe registers a new subscriber. `kinds` is a filter: empty == all.
func (b *Bus) Subscribe(kinds []dashsimv1.ObjectKind) *Subscription {
	ch := make(chan *dashsimv1.Event, DefaultBuffer)
	kindSet := make(map[dashsimv1.ObjectKind]struct{}, len(kinds))
	for _, k := range kinds {
		kindSet[k] = struct{}{}
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	sub := &Subscription{
		C:     ch,
		ch:    ch,
		bus:   b,
		id:    id,
		kinds: kindSet,
	}
	b.subs[id] = sub
	b.mu.Unlock()
	return sub
}

func (b *Bus) unsubscribe(id uint64) {
	b.mu.Lock()
	delete(b.subs, id)
	b.mu.Unlock()
}

// Publish delivers an event to every matching subscriber, non-blocking. If a
// subscriber's buffer is full the event is dropped for that subscriber and the
// global dropped counter is incremented.
func (b *Bus) Publish(ev *dashsimv1.Event) {
	b.mu.RLock()
	// Snapshot to a small local slice so we don't hold the read lock while
	// sending.
	subs := make([]*Subscription, 0, len(b.subs))
	for _, s := range b.subs {
		if s.matches(ev.GetKind()) {
			subs = append(subs, s)
		}
	}
	b.mu.RUnlock()

	for _, s := range subs {
		select {
		case s.ch <- ev:
		default:
			b.dropped.Add(1)
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Dropped returns the cumulative count of events dropped due to full
// subscriber buffers.
func (b *Bus) Dropped() uint64 { return b.dropped.Load() }
