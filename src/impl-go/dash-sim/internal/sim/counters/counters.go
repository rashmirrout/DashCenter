// Package counters provides synthetic per-object packet/byte/drop counters.
// Counters increment deterministically based on a hash of the joined object
// key so tests can assert exact values.
package counters

import (
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
)

// Registry tracks counters per object key (joined with ":").
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

// Tick advances counters for the given key by a deterministic amount.
func (r *Registry) Tick(key string) {
	c := r.get(key)
	h := hash(key)
	c.packetsIn.Add(int64(1 + (h & 0x0f)))
	c.packetsOut.Add(int64(1 + ((h >> 4) & 0x0f)))
	c.bytesIn.Add(int64(64 * (1 + (h & 0xff))))
	c.bytesOut.Add(int64(64 * (1 + ((h >> 8) & 0xff))))
	if h%23 == 0 {
		c.drops.Add(1)
	}
}

// Snapshot returns counter values for the key, or zeros if unknown.
func (r *Registry) Snapshot(key string) map[string]int64 {
	r.mu.RLock()
	c, ok := r.counters[key]
	r.mu.RUnlock()
	if !ok {
		return map[string]int64{
			"packets_in": 0, "packets_out": 0,
			"bytes_in": 0, "bytes_out": 0, "drops": 0,
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

// Keys returns every tracked key.
func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.counters))
	for k := range r.counters {
		out = append(out, k)
	}
	return out
}

// Forget removes counters for a key.
func (r *Registry) Forget(key string) {
	r.mu.Lock()
	delete(r.counters, key)
	r.mu.Unlock()
}

// ResetAll zeroes every counter accumulator without removing the keys.
// After ResetAll, Snapshot(k) returns all-zero for every previously-
// tracked key — the next Tick will start from zero. Objects (ENIs,
// VNETs, etc.) are NOT touched; this is a counter-only operation.
// Returns the number of keys reset.
func (r *Registry) ResetAll() int {
	r.mu.Lock()
	n := len(r.counters)
	r.counters = make(map[string]*objectCounters, n)
	r.mu.Unlock()
	return n
}

func (r *Registry) get(key string) *objectCounters {
	r.mu.RLock()
	c, ok := r.counters[key]
	r.mu.RUnlock()
	if ok {
		return c
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok = r.counters[key]; ok {
		return c
	}
	c = &objectCounters{}
	r.counters[key] = c
	return c
}

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

// JoinKey turns ordered key parts into the canonical Redis-style joined key.
func JoinKey(parts []string) string {
	return strings.Join(parts, ":")
}
