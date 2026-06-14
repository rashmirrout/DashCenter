// Package counters: Bucket aggregator + scope rollups (PE-3a / PE-G8).
//
// The legacy Snapshot(key) → map[string]int64 API stays for back-compat with
// the existing dashapi.v1.GetCounters per-object RPC. This file adds the
// typed accessors that GetDpuCounters needs:
//
//   - Bucket            typed value carrying packets_in/out, bytes_in/out, drops.
//   - SnapshotBucket    typed equivalent of Snapshot — one (kind,key) → Bucket.
//   - Rollup            sum every tracked key whose first joined-component
//                       matches the supplied scope. Used to compute per-ENI +
//                       per-VNET aggregates from the model store's key set
//                       without ever introspecting object payloads.
//
// Rollup scope semantics
// ----------------------
// A scope `S` claims a key `K` iff:
//
//   - K equals S exactly (single-component key — e.g. ENI key = ["eni-1"]
//     produces the joined form "eni-1" which matches scope "eni-1"), OR
//   - K begins with S followed by ":" (multi-component key — e.g. ENI route
//     key = ["eni-1", "10.0.0.0/24"] joined as "eni-1:10.0.0.0/24" matches
//     scope "eni-1").
//
// This matches the model.Store key conventions used by every dash-sim
// scenario today; see proto/dashapi/v1/dashapi.proto comments around
// ObjectKind for the full per-kind key shape table.

package counters

import "strings"

// Bucket is the typed counter snapshot for one or more (kind,key) pairs.
// Zero value is meaningful (all five counters at 0). Add new fields
// ADDITIVELY to preserve back-compat.
type Bucket struct {
	PacketsIn  int64
	PacketsOut int64
	BytesIn    int64
	BytesOut   int64
	Drops      int64
}

// Add accumulates other into b. Sum is commutative, so the caller can
// fold a sequence of Buckets by repeated Add(other).
func (b *Bucket) Add(other Bucket) {
	b.PacketsIn += other.PacketsIn
	b.PacketsOut += other.PacketsOut
	b.BytesIn += other.BytesIn
	b.BytesOut += other.BytesOut
	b.Drops += other.Drops
}

// SnapshotBucket returns the typed counter values for a single joined key,
// or the zero Bucket if the key has never been Ticked. Safe for concurrent
// callers (mirrors Snapshot's RW-locked read).
func (r *Registry) SnapshotBucket(key string) Bucket {
	r.mu.RLock()
	c, ok := r.counters[key]
	r.mu.RUnlock()
	if !ok {
		return Bucket{}
	}
	return Bucket{
		PacketsIn:  c.packetsIn.Load(),
		PacketsOut: c.packetsOut.Load(),
		BytesIn:    c.bytesIn.Load(),
		BytesOut:   c.bytesOut.Load(),
		Drops:      c.drops.Load(),
	}
}

// TotalBucket returns the sum of every tracked key's counters. Used to
// produce the DPU-wide rollup in dashapi.v1.DpuCountersResponse.dpu.
//
// Implementation note: takes a single RLock for the whole walk; per-key
// atomic loads guarantee a consistent read of each row even as Tick() runs
// concurrently. The total may straddle Ticks (some rows seen before a tick,
// others after) — acceptable for a counter snapshot.
func (r *Registry) TotalBucket() Bucket {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total Bucket
	for _, c := range r.counters {
		total.Add(Bucket{
			PacketsIn:  c.packetsIn.Load(),
			PacketsOut: c.packetsOut.Load(),
			BytesIn:    c.bytesIn.Load(),
			BytesOut:   c.bytesOut.Load(),
			Drops:      c.drops.Load(),
		})
	}
	return total
}

// Rollup returns the summed Bucket for every tracked key that belongs to
// `scope`. Empty scope returns the zero Bucket (defensive: callers should
// never rollup an empty scope).
//
// Membership rule: key == scope OR strings.HasPrefix(key, scope+":").
func (r *Registry) Rollup(scope string) Bucket {
	if scope == "" {
		return Bucket{}
	}
	prefix := scope + ":"
	r.mu.RLock()
	defer r.mu.RUnlock()
	var total Bucket
	for k, c := range r.counters {
		if k != scope && !strings.HasPrefix(k, prefix) {
			continue
		}
		total.Add(Bucket{
			PacketsIn:  c.packetsIn.Load(),
			PacketsOut: c.packetsOut.Load(),
			BytesIn:    c.bytesIn.Load(),
			BytesOut:   c.bytesOut.Load(),
			Drops:      c.drops.Load(),
		})
	}
	return total
}

// RollupAll returns a map[scope]Bucket containing Rollup(s) for every s in
// `scopes`. Convenient for batching the per-ENI / per-VNET pass when the
// caller has already enumerated the scope keys via model.Store.
//
// A scope that has zero matching keys is still represented in the result
// with a zero Bucket — the caller can decide whether to surface or skip
// empty rollups in its response.
func (r *Registry) RollupAll(scopes []string) map[string]Bucket {
	out := make(map[string]Bucket, len(scopes))
	for _, s := range scopes {
		out[s] = r.Rollup(s)
	}
	return out
}
