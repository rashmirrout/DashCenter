// Package leader defines the dashd leader-election abstraction.
//
// LeaderProxy wraps an Elector with safe hot-swap so that leaderLoop
// can replace the inner elector after leadership loss (fresh etcd
// session) while admin/cluster consumers observe a stable reference.
package leader

import "sync"

// LeaderProxy is a stable wrapper that delegates IsLeader/LeaderID to
// the current inner Elector. leaderLoop swaps the inner elector on
// each re-campaign; all other consumers (admin server, cluster
// aggregator, show-leader endpoint) hold the proxy and always see the
// latest state without re-wiring.
type LeaderProxy struct {
	mu    sync.RWMutex
	inner Elector
}

// NewProxy wraps an initial elector. The proxy satisfies the admin
// server's LeaderObserver interface (IsLeader + LeaderID).
func NewProxy(initial Elector) *LeaderProxy {
	return &LeaderProxy{inner: initial}
}

// Swap replaces the inner elector. Called by leaderLoop after creating
// a fresh EtcdElector for re-campaign. The old elector must be
// Close'd by the caller BEFORE calling Swap to avoid stale keepalive
// goroutines.
func (p *LeaderProxy) Swap(next Elector) {
	p.mu.Lock()
	p.inner = next
	p.mu.Unlock()
}

// IsLeader delegates to the current inner elector.
func (p *LeaderProxy) IsLeader() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.inner == nil {
		return false
	}
	return p.inner.IsLeader()
}

// LeaderID delegates to the current inner elector.
func (p *LeaderProxy) LeaderID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.inner == nil {
		return ""
	}
	return p.inner.LeaderID()
}

// Inner returns the current elector. Used by leaderLoop to access
// AwaitLeadership / LostLeadership / Close. Not safe to hold long —
// the reference may be swapped at any time.
func (p *LeaderProxy) Inner() Elector {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.inner
}
