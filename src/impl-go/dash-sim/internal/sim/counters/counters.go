// Package counters provides synthetic per-object packet/byte/drop counters so
// that downstream components (dashd's TimeSeries path) have data to exercise
// without real traffic. Counters increment deterministically based on object
// id hash so tests can assert exact values.
package counters

import (
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// Registry tracks counters per object id.
type Registry struct {
	mu       sync.RWMutex
	counters map[string]*objectCounters
}

type objectCounters struct {
	packetsIn  atomic.Int64
	packetsOut atomic.Int64
	bytesIn    atomic.Int64
	bytesOut   atomic.Int64
	drops      atomic.Int64
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{counters: make(map[string]*objectCounters)}
}

// Tick advances counters for the given object id by a deterministic amount.
// Call this periodically from a background goroutine in main.
func (r *Registry) Tick(id string) {
	c := r.get(id)
	h := hash(id)
	c.packetsIn.Add(int64(1 + (h & 0x0f)))
	c.packetsOut.Add(int64(1 + ((h >> 4) & 0x0f)))
	c.bytesIn.Add(int64(64 * (1 + (h & 0xff))))
	c.bytesOut.Add(int64(64 * (1 + ((h >> 8) & 0xff))))
	if h%23 == 0 {
		c.drops.Add(1)
	}
}

// Snapshot returns the current counter values for the object id, or zero
// values if the id is unknown.
func (r *Registry) Snapshot(id string) map[string]int64 {
	r.mu.RLock()
	c, ok := r.counters[id]
	r.mu.RUnlock()
	if !ok {
		return map[string]int64{
			"packets_in":  0,
			"packets_out": 0,
			"bytes_in":    0,
			"bytes_out":   0,
			"drops":       0,
		}
	}
	return map[string]int64{
		"packets_in":  c.packetsIn.Load(),
		"packets_out": c.packetsOut.Load(),
		"bytes_in":    c.bytesIn.Load(),
		"bytes_out":   c.bytesOut.Load(),
		"drops":       c.drops.Load(),
	}
}

// IDs returns every id that has any counter recorded.
func (r *Registry) IDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.counters))
	for id := range r.counters {
		out = append(out, id)
	}
	return out
}

// Forget removes counters for the id (called on object delete).
func (r *Registry) Forget(id string) {
	r.mu.Lock()
	delete(r.counters, id)
	r.mu.Unlock()
}

func (r *Registry) get(id string) *objectCounters {
	r.mu.RLock()
	c, ok := r.counters[id]
	r.mu.RUnlock()
	if ok {
		return c
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok = r.counters[id]; ok {
		return c
	}
	c = &objectCounters{}
	r.counters[id] = c
	return c
}

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
