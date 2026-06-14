// store.go is the in-memory per-DPU CounterReport cache. It holds
// the most-recent typed report per DPU plus the per-ENI and per-VNET
// sub-rollups, so admin endpoints + the future ObservabilityService
// streaming impl can serve "what's the current count for X" without
// touching the southbound DPU.
//
// The store is a snapshot model — every Put atomically replaces the
// per-DPU entry. There is intentionally no merge: each poll round
// produces a complete report and partial updates would mask drops in
// counters that the poller stopped reporting.
//
// The store also publishes notifications via a fan-out broadcaster
// (a single channel that drops on slow subscribers — preserves the
// "observability is best-effort" promise). Notifications are used by
// PE-3c to drive ObservabilityService.GetCounters streaming and the
// dashw/SPA live widget; PE-3b only wires the publisher so subscribers
// in PE-3c plug in without store rework.
package counters

import (
	"sort"
	"sync"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// Entry is the cached counter snapshot for one DPU.
type Entry struct {
	DpuID    string
	Report   *dashcenterv1.CounterReport
	PerEni   map[string]*dashcenterv1.CounterReport
	PerVnet  map[string]*dashcenterv1.CounterReport
	UpdateAt time.Time
}

// Store is the per-DPU counter cache. Safe for concurrent use.
type Store struct {
	mu      sync.RWMutex
	entries map[string]*Entry

	subsMu sync.Mutex
	subs   []chan<- string // each subscriber gets the DPU id whose entry just changed
}

// NewStore builds an empty store.
func NewStore() *Store {
	return &Store{entries: make(map[string]*Entry)}
}

// Put atomically replaces the per-DPU entry. nil report is a no-op so
// callers do not have to guard on the mapper output.
func (s *Store) Put(e Entry) {
	if e.DpuID == "" || e.Report == nil {
		return
	}
	e.UpdateAt = time.Now().UTC()
	s.mu.Lock()
	s.entries[e.DpuID] = &e
	s.mu.Unlock()

	// Fan out the change notification. Drop-on-slow — the receiver
	// missed an event, the snapshot is still queryable via Get/List.
	s.subsMu.Lock()
	subs := append([]chan<- string(nil), s.subs...)
	s.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e.DpuID:
		default:
		}
	}
}

// Get returns the entry for dpuID, or (nil,false) if absent.
func (s *Store) Get(dpuID string) (*Entry, bool) {
	s.mu.RLock()
	e, ok := s.entries[dpuID]
	s.mu.RUnlock()
	return e, ok
}

// GetReport is the PE-3c streaming-surface accessor: returns just the
// DPU-wide CounterReport for dpuID, or (nil,false) if absent. The
// per-ENI and per-VNET sub-rollups (Entry.PerEni / Entry.PerVnet) are
// intentionally not surfaced here — the GetCounters stream emits the
// DPU-level rollup only, matching the operator-facing widget design.
// Admin endpoints (PE-3b) continue to expose the full Entry via Get.
func (s *Store) GetReport(dpuID string) (*dashcenterv1.CounterReport, bool) {
	s.mu.RLock()
	e, ok := s.entries[dpuID]
	s.mu.RUnlock()
	if !ok || e == nil {
		return nil, false
	}
	return e.Report, true
}

// List returns every entry, sorted ascending by DpuID. The returned
// slice references the same underlying *Entry pointers — callers MUST
// treat the values as read-only (Put replaces, never mutates in
// place).
func (s *Store) List() []*Entry {
	s.mu.RLock()
	out := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		out = append(out, e)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].DpuID < out[j].DpuID })
	return out
}

// Len returns the current number of cached DPUs.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Delete removes the entry for dpuID, if any. Returns true if an
// entry was present.
func (s *Store) Delete(dpuID string) bool {
	s.mu.Lock()
	_, ok := s.entries[dpuID]
	delete(s.entries, dpuID)
	s.mu.Unlock()
	return ok
}

// Subscribe registers ch to receive per-DPU change notifications. The
// channel MUST be buffered (size 1 is enough — drop-on-slow); the
// store never blocks on send. Returns an unsubscribe function.
func (s *Store) Subscribe(ch chan<- string) func() {
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	idx := len(s.subs) - 1
	s.subsMu.Unlock()
	return func() {
		s.subsMu.Lock()
		defer s.subsMu.Unlock()
		if idx >= len(s.subs) || s.subs[idx] != ch {
			// Slow path: subs slice was reordered (another unsubscribe).
			for i, c := range s.subs {
				if c == ch {
					s.subs = append(s.subs[:i], s.subs[i+1:]...)
					return
				}
			}
			return
		}
		s.subs = append(s.subs[:idx], s.subs[idx+1:]...)
	}
}
