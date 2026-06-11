package leader

import (
	"context"
	"sync"
)

// NoneElector is the trivial Elector for single-node / dev deployments.
// It declares this process the permanent leader: AwaitLeadership returns
// immediately, LostLeadership never fires, and IsLeader is always true
// until Close is called.
//
// Using NoneElector in cmd/dashd/main.go preserves the pre-PA-0 single-node
// behaviour exactly: every leader-only goroutine starts at boot and runs
// until shutdown. PA-3 will introduce an EtcdElector with the same
// interface, at which point cmd/dashd/main.go only swaps the constructor.
//
// The zero value is usable but anonymous (LeaderID returns ""). For a
// human-readable leader-id set NodeID explicitly:
//
//	elector := &leader.NoneElector{NodeID: "dashd-dev"}
type NoneElector struct {
	// NodeID is the stable identifier this elector reports via LeaderID.
	// Optional; defaults to "" for the zero value.
	NodeID string

	// closeOnce guards Close so it is idempotent.
	closeOnce sync.Once

	// closeCh is closed by Close. AwaitLeadership and IsLeader use it to
	// honour the post-Close contract.
	closeCh chan struct{}

	// initOnce lazily creates closeCh so the zero value works without
	// requiring callers to use a constructor.
	initOnce sync.Once
}

func (n *NoneElector) init() {
	n.initOnce.Do(func() {
		n.closeCh = make(chan struct{})
	})
}

// AwaitLeadership returns nil immediately because NoneElector is permanent
// leader. It does honour ctx cancellation (returning ctx.Err()) and Close
// (returning context.Canceled) so callers can write a transport-agnostic
// leaderLoop that behaves correctly under shutdown.
func (n *NoneElector) AwaitLeadership(ctx context.Context) error {
	n.init()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-n.closeCh:
		return context.Canceled
	default:
		return nil
	}
}

// LostLeadership returns a channel that is closed only by Close. Real
// production use of NoneElector should never observe leadership loss; the
// channel exists solely so the leaderLoop in main.go can use the same
// select statement regardless of which elector is plugged in.
func (n *NoneElector) LostLeadership() <-chan struct{} {
	n.init()
	return n.closeCh
}

// IsLeader returns true until Close is called.
func (n *NoneElector) IsLeader() bool {
	n.init()
	select {
	case <-n.closeCh:
		return false
	default:
		return true
	}
}

// LeaderID returns the configured NodeID (empty string for the zero value).
func (n *NoneElector) LeaderID() string {
	return n.NodeID
}

// Close releases the elector. After Close: IsLeader returns false,
// AwaitLeadership returns context.Canceled, and LostLeadership is closed
// (so a leaderLoop select-wake fires for graceful teardown). Idempotent.
func (n *NoneElector) Close() error {
	n.init()
	n.closeOnce.Do(func() {
		close(n.closeCh)
	})
	return nil
}
