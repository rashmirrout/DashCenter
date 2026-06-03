// Package events is the fan-out bus that powers DashApi.Subscribe.
package events

import (
	"sync"
	"sync/atomic"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
)

// DefaultBuffer is the per-subscriber channel capacity.
const DefaultBuffer = 256

// Subscription is a handle returned by Subscribe. Read from C; call Close.
type Subscription struct {
	C      <-chan *dashapi.Event
	closed atomic.Bool
	ch     chan *dashapi.Event
	bus    *Bus
	id     uint64
	kinds  map[dashapi.ObjectKind]struct{}
}

// Close idempotently removes the subscription.
func (s *Subscription) Close() {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.bus.unsubscribe(s.id)
	close(s.ch)
}

func (s *Subscription) matches(kind dashapi.ObjectKind) bool {
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

	dropped atomic.Uint64
}

// New returns an empty Bus.
func New() *Bus {
	return &Bus{subs: make(map[uint64]*Subscription)}
}

// Subscribe registers a new subscriber. `kinds` is a filter: empty == all.
func (b *Bus) Subscribe(kinds []dashapi.ObjectKind) *Subscription {
	ch := make(chan *dashapi.Event, DefaultBuffer)
	kindSet := make(map[dashapi.ObjectKind]struct{}, len(kinds))
	for _, k := range kinds {
		kindSet[k] = struct{}{}
	}
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	sub := &Subscription{
		C: ch, ch: ch, bus: b, id: id, kinds: kindSet,
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

// Publish delivers an event non-blocking. Drops on full subscriber buffers.
func (b *Bus) Publish(ev *dashapi.Event) {
	b.mu.RLock()
	subs := make([]*Subscription, 0, len(b.subs))
	for _, s := range b.subs {
		if s.matches(ev.GetObject().GetKind()) {
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

// Dropped returns the cumulative count of dropped events.
func (b *Bus) Dropped() uint64 { return b.dropped.Load() }
