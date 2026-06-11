// Package leader defines the dashd leader-election abstraction.
//
// In Phase 1 / single-node deployments, dashd is always-leader (every
// reconciler/dispatch/subscribe goroutine runs unconditionally). When Phase 2
// PA introduces the etcd-backed multi-node deployment, only one dashd
// instance in the cluster must run those leader-only goroutines while
// followers serve read-only RPCs from the same strongly-consistent etcd
// store.
//
// The Elector interface is the seam that keeps cmd/dashd/main.go identical
// across both deployment shapes:
//
//	elector := leader.NoneElector{NodeID: cfg.NodeID}     // Phase 1 / dev
//	elector := leader.NewEtcdElector(cfg.HA, etcdClient)  // Phase 2 PA (PA-3)
//
//	// Same leaderLoop in both cases:
//	if err := elector.AwaitLeadership(ctx); err != nil { /* shutdown */ }
//	// ...launch leader-only goroutines...
//	select {
//	case <-ctx.Done():            // shutdown signal
//	case <-elector.LostLeadership(): // lost the lease; tear down and re-campaign
//	}
//
// The NoneElector implementation in this package collapses to "always leader,
// never lost" so a single-node dashd build behaves identically to the
// pre-PA-0 baseline.
package leader

import "context"

// Elector decides whether this dashd process is the active leader of the
// cluster. Implementations are safe for concurrent use. A typical caller
// holds at most one Elector for the lifetime of the process.
type Elector interface {
	// AwaitLeadership blocks until this process becomes the leader, the
	// supplied context is cancelled, or the elector is closed. It returns
	// nil on successful campaign and ctx.Err() on cancellation. Implementations
	// must be safe to call repeatedly: a leader that loses leadership may
	// re-campaign by calling AwaitLeadership again.
	AwaitLeadership(ctx context.Context) error

	// LostLeadership returns a channel that is closed (or receives one
	// value) when the leader role is lost — typically because the underlying
	// lease expired or was revoked. The caller MUST tear down all
	// leader-only goroutines on receipt and may then call AwaitLeadership
	// again to re-campaign.
	//
	// For elector implementations that never lose leadership (NoneElector),
	// this channel is never closed and never sends.
	LostLeadership() <-chan struct{}

	// IsLeader is a non-blocking snapshot of the current leadership state.
	// Useful for read-only health endpoints. Do NOT gate critical-section
	// logic on this — IsLeader may flip between the check and the action;
	// gate on the LostLeadership channel instead.
	IsLeader() bool

	// LeaderID returns the stable identifier of the current leader.
	// For NoneElector and EtcdElector this is the operator-supplied
	// node-id. Returns "" if leadership is currently unknown (e.g., before
	// the first AwaitLeadership campaign succeeds).
	LeaderID() string

	// Close releases any resources held by the elector (etcd lease, etcd
	// client, etc.). After Close, AwaitLeadership returns context.Canceled
	// and IsLeader returns false. Idempotent.
	Close() error
}
