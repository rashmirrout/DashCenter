// bridge.go connects counters.Store change notifications to
// Broadcaster.Publish. One Bridge per dashd process.
//
// The bridge is a separate type (not folded into Broadcaster) so:
//
//   1. Broadcaster has zero dependency on the counters package and
//      can be unit-tested in isolation (broadcaster_test.go uses
//      Publish directly).
//   2. Tests can swap the store with a fake that drives change
//      notifications on demand.
//   3. The "what to do on each change" logic (which is small but
//      operator-visible: store.Get failure handling, nil-entry
//      handling) lives in one obvious place.
//
// Lifecycle
// ---------
//   * NewBridge(store, bcast, logger) constructs the bridge.
//   * Run(ctx) starts the goroutine that consumes store notifications.
//     Returns when ctx is cancelled. Idempotent.
//   * Each store change becomes one Broadcaster.Publish call carrying
//     the freshly-fetched CounterReport. Per-DPU and per-ENI/VNET
//     sub-rollups stay inside the store (admin endpoint surfaces them);
//     the streaming surface emits the DPU-wide rollup only — matches
//     the operator-facing widget design.
//
// Pattern: this is the Adapter pattern (counters.Store → broadcaster
// public API).

package broadcaster

import (
	"context"
	"log/slog"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// CounterStore is the minimal contract the bridge needs from
// counters.Store. Modelled as an interface so the bridge package
// stays decoupled from counters and tests can inject fakes.
//
// counters.Store satisfies this interface:
//
//   * Subscribe(ch chan<- string) func()  — returns unsubscribe func
//   * Get(dpuID string) (*counters.Entry, bool)  — the Entry's
//     Report field is the *dashcenterv1.CounterReport we forward.
//
// We DO NOT depend on counters.Entry directly (avoids import cycle
// risk if counters ever needs to reference broadcaster). Instead the
// interface returns the CounterReport pointer + a found flag.
type CounterStore interface {
	Subscribe(ch chan<- string) (cancel func())
	GetReport(dpuID string) (*dashcenterv1.CounterReport, bool)
}

// Bridge wires CounterStore change notifications to Broadcaster.Publish.
type Bridge struct {
	store  CounterStore
	bcast  *Broadcaster
	logger *slog.Logger

	// buffered so the store's Put never blocks if the bridge goroutine
	// is briefly slow. 256 = ~50s of headroom at the default 5s poll
	// cadence × 50 DPUs (typical fleet). Drops from this channel are
	// fine — the store has the latest snapshot; we just miss one
	// notification cycle for those DPUs. The next poll round will
	// re-publish.
	notifyCh chan string
}

// NewBridge constructs an unstarted bridge. Call Run(ctx) to start
// consuming store notifications.
func NewBridge(store CounterStore, bcast *Broadcaster, logger *slog.Logger) *Bridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &Bridge{
		store:    store,
		bcast:    bcast,
		logger:   logger,
		notifyCh: make(chan string, 256),
	}
}

// Run consumes change notifications until ctx is cancelled. Idempotent
// — second invocation returns immediately. Callers MUST spawn this
// in a goroutine.
func (b *Bridge) Run(ctx context.Context) {
	if b.store == nil || b.bcast == nil {
		// Defensive nil-guard for misconfigured callers; observability
		// is best-effort and shouldn't panic dashd.
		b.logger.Warn("counter broadcaster bridge: nil store or broadcaster; bridge inactive")
		return
	}
	cancel := b.store.Subscribe(b.notifyCh)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return
		case dpuID, ok := <-b.notifyCh:
			if !ok {
				// Store closed the channel; bail.
				return
			}
			report, found := b.store.GetReport(dpuID)
			if !found || report == nil {
				// Race: entry was deleted between notify and read.
				// Skip; the next poll round will re-publish if relevant.
				continue
			}
			b.bcast.Publish(report)
		}
	}
}
