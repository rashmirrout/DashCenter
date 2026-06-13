// poller.go drives the per-DPU GetDpuCounters fetch loop. One Poller
// owns the goroutine; it discovers DPUs from the supplied Inventory
// and uses a caller-supplied ClientFactory to dial them.
//
// Lifecycle:
//
//   - NewPoller builds an enabled poller with the configured interval.
//     Disabled mode is a separate constructor (NewDisabledPoller) so
//     the dashd boot path can wire something into main.go even when
//     the operator turned counters off.
//
//   - Start(ctx) launches the loop. Stop or ctx cancellation tears it
//     down deterministically (waits for the in-flight poll round).
//
//   - SetInterval atomically updates the cadence at runtime. The next
//     tick uses the new interval; the in-flight poll round runs to
//     completion. Admin endpoints route through here.
//
//   - Enabled() / Interval() are read-only accessors for /admin output.
//
// A poll round walks every DPU in inventory, dials a fresh client per
// DPU (so a flaky single DPU doesn't degrade others), fetches counters
// with the include-enis + include-vnets flags set (operators always
// want the full breakdown via the admin endpoint), translates via the
// mapper, and Put()s into the store. Per-DPU failures are logged at
// WARN and the loop continues — counters are best-effort observability,
// not control-plane state. The PE-G7-style broadcaster will fan-out
// the store change events to PE-3c subscribers automatically.
package counters

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// MinInterval is the smallest poll cadence we accept. Anything tighter
// thrashes sims + adds polling load with no observable benefit.
const MinInterval = 100 * time.Millisecond

// DefaultInterval is the default cadence when none is configured.
const DefaultInterval = 5 * time.Second

// pollTimeout caps a single GetDpuCounters RPC; covers TCP RTT + the
// in-process sim handler + protojson marshalling.
const pollTimeout = 5 * time.Second

// Poller is the dashd counter polling loop. Safe for concurrent use:
// Start / Stop / SetInterval may be called from any goroutine.
type Poller struct {
	inv     *inventory.Inventory
	factory dpuclient.ClientFactory
	store   *Store

	enabled atomic.Bool
	// interval is stored as int64 nanoseconds so SetInterval can be a
	// single atomic op observed by the tick loop.
	intervalNs atomic.Int64

	mu      sync.Mutex
	stop    context.CancelFunc
	stopped chan struct{}
}

// NewPoller builds a Poller in the enabled state with the given
// interval (clamped to MinInterval).
func NewPoller(inv *inventory.Inventory, factory dpuclient.ClientFactory, store *Store, interval time.Duration) *Poller {
	p := &Poller{inv: inv, factory: factory, store: store}
	p.enabled.Store(true)
	p.setIntervalLocked(interval)
	return p
}

// NewDisabledPoller returns a poller whose Start is a no-op until
// SetEnabled(true) is called. Used when configuration disables
// counters but admin endpoints still need a wire to flip on later.
func NewDisabledPoller(inv *inventory.Inventory, factory dpuclient.ClientFactory, store *Store, interval time.Duration) *Poller {
	p := NewPoller(inv, factory, store, interval)
	p.enabled.Store(false)
	return p
}

// Start launches the polling loop. Idempotent — second call is a no-op
// while the loop is running.
func (p *Poller) Start(parentCtx context.Context) {
	p.mu.Lock()
	if p.stop != nil {
		p.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(parentCtx)
	stopped := make(chan struct{})
	p.stop = cancel
	p.stopped = stopped
	p.mu.Unlock()

	go p.run(ctx, stopped)
}

// Stop signals the loop and waits for it to drain. Safe to call when
// not running (no-op).
func (p *Poller) Stop() {
	p.mu.Lock()
	cancel := p.stop
	stopped := p.stopped
	p.stop = nil
	p.stopped = nil
	p.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	<-stopped
}

// SetInterval updates the poll cadence. Takes effect on the next tick.
// Values below MinInterval are clamped.
func (p *Poller) SetInterval(d time.Duration) {
	p.setIntervalLocked(d)
}

// setIntervalLocked is the shared clamp + store implementation; not
// actually mutex-protected since it is a single atomic op, but the
// name keeps the contract obvious in call sites.
func (p *Poller) setIntervalLocked(d time.Duration) {
	if d < MinInterval {
		d = MinInterval
	}
	p.intervalNs.Store(int64(d))
}

// Interval returns the current poll cadence.
func (p *Poller) Interval() time.Duration {
	return time.Duration(p.intervalNs.Load())
}

// SetEnabled turns the polling loop on or off. Disabling cancels the
// next tick but does NOT interrupt an in-flight poll round.
func (p *Poller) SetEnabled(b bool) { p.enabled.Store(b) }

// Enabled reports whether polling is currently active.
func (p *Poller) Enabled() bool { return p.enabled.Load() }

// run is the loop. Each tick walks inventory and polls every DPU.
func (p *Poller) run(ctx context.Context, stopped chan struct{}) {
	defer close(stopped)

	timer := time.NewTimer(p.Interval())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if p.enabled.Load() {
				p.pollOnce(ctx)
			}
			timer.Reset(p.Interval())
		}
	}
}

// pollOnce runs a single poll round across all DPUs. Each per-DPU
// poll uses its own context with pollTimeout so a slow DPU does not
// stall the round.
func (p *Poller) pollOnce(parentCtx context.Context) {
	if p.inv == nil || p.factory == nil || p.store == nil {
		return
	}
	for _, e := range p.inv.List() {
		select {
		case <-parentCtx.Done():
			return
		default:
		}
		if e.Endpoint == "" {
			continue
		}
		p.pollDpu(parentCtx, e.ID, e.Endpoint)
	}
}

// pollDpu dials, fetches, maps, and stores. Errors are logged and
// swallowed so the poll round continues.
func (p *Poller) pollDpu(parentCtx context.Context, dpuID, endpoint string) {
	client, err := p.factory(endpoint)
	if err != nil {
		slog.Warn("counters: dial failed", "dpu", dpuID, "endpoint", endpoint, "error", err)
		return
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(parentCtx, pollTimeout)
	defer cancel()

	resp, err := client.GetDpuCounters(ctx, &dashapiv1.DpuCountersRequest{
		IncludeEnis:  true,
		IncludeVnets: true,
	})
	if err != nil {
		// Don't log at WARN for shutdown-induced cancels (rootCtx done).
		if errors.Is(err, context.Canceled) && parentCtx.Err() != nil {
			return
		}
		slog.Warn("counters: GetDpuCounters failed", "dpu", dpuID, "error", err)
		return
	}

	p.store.Put(Entry{
		DpuID:   dpuID,
		Report:  MapReport(dpuID, resp),
		PerEni:  MapPerEni(resp),
		PerVnet: MapPerVnet(resp),
	})
}
