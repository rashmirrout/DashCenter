package inventory

import (
"context"
"log/slog"
"sync"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// ProbeFunc is the function used to test connectivity to a DPU.
// Returns nil on success, error on failure. Implementations should
// use a short context timeout (e.g. 5s).
type ProbeFunc func(ctx context.Context, endpoint string) error

// Prober periodically probes DPU liveness and updates state in the Inventory.
type Prober struct {
inv      *Inventory
interval time.Duration
probe    ProbeFunc
mu       sync.Mutex
cancels  map[string]context.CancelFunc
wg       sync.WaitGroup
}

// NewProber creates a Prober with the given probe function and interval.
func NewProber(inv *Inventory, interval time.Duration, probe ProbeFunc) *Prober {
return &Prober{
inv:      inv,
interval: interval,
probe:    probe,
cancels:  make(map[string]context.CancelFunc),
}
}

// Run launches probe goroutines for every DPU in the inventory and
// reconciles the goroutine set against inventory on each tick.
// Blocks until ctx is cancelled.
func (p *Prober) Run(ctx context.Context) {
ticker := time.NewTicker(p.interval)
defer ticker.Stop()

for {
p.reconcileProbers(ctx)

select {
case <-ctx.Done():
p.stopAll()
return
case <-ticker.C:
}
}
}

// reconcileProbers starts/stops probe goroutines to match current inventory.
func (p *Prober) reconcileProbers(ctx context.Context) {
entries := p.inv.List()
activeSet := make(map[string]struct{}, len(entries))

for _, e := range entries {
activeSet[e.ID] = struct{}{}
p.mu.Lock()
_, running := p.cancels[e.ID]
p.mu.Unlock()

if !running {
p.startProbe(ctx, e.ID, e.Endpoint)
}
}

// Stop probers for deregistered DPUs.
p.mu.Lock()
for id, cancel := range p.cancels {
if _, ok := activeSet[id]; !ok {
cancel()
delete(p.cancels, id)
}
}
p.mu.Unlock()
}

func (p *Prober) startProbe(parentCtx context.Context, dpuID, endpoint string) {
ctx, cancel := context.WithCancel(parentCtx)
p.mu.Lock()
p.cancels[dpuID] = cancel
p.mu.Unlock()

p.wg.Add(1)
go func() {
defer p.wg.Done()
p.probeLoop(ctx, dpuID, endpoint)
}()
}

func (p *Prober) probeLoop(ctx context.Context, dpuID, endpoint string) {
ticker := time.NewTicker(p.interval)
defer ticker.Stop()

for {
p.probeOnce(ctx, dpuID, endpoint)
select {
case <-ctx.Done():
return
case <-ticker.C:
}
}
}

func (p *Prober) probeOnce(ctx context.Context, dpuID, endpoint string) {
probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
defer cancel()

err := p.probe(probeCtx, endpoint)
if err != nil {
p.recordFailure(dpuID, err)
} else {
p.recordSuccess(dpuID)
}
}

func (p *Prober) recordSuccess(dpuID string) {
entry, err := p.inv.Get(dpuID)
if err != nil {
return
}

switch entry.State {
case dashcenterv1.DpuState_DPU_STATE_REGISTERING,
dashcenterv1.DpuState_DPU_STATE_UNREACHABLE,
dashcenterv1.DpuState_DPU_STATE_DEGRADED:
_ = p.inv.SetState(dpuID, dashcenterv1.DpuState_DPU_STATE_UP)
slog.Info("prober: DPU is UP", "dpu", dpuID)
}
_ = p.inv.ResetErrors(dpuID)
}

func (p *Prober) recordFailure(dpuID string, probeErr error) {
count, err := p.inv.IncrementErrors(dpuID)
if err != nil {
return
}
if count < 3 {
slog.Debug("prober: DPU probe failed", "dpu", dpuID, "count", count, "error", probeErr)
return
}

entry, err := p.inv.Get(dpuID)
if err != nil {
return
}
if entry.State != dashcenterv1.DpuState_DPU_STATE_UNREACHABLE {
_ = p.inv.SetState(dpuID, dashcenterv1.DpuState_DPU_STATE_UNREACHABLE)
slog.Warn("prober: DPU is UNREACHABLE (3 failures)", "dpu", dpuID)
}
}

func (p *Prober) stopAll() {
p.mu.Lock()
for id, cancel := range p.cancels {
cancel()
delete(p.cancels, id)
}
p.mu.Unlock()
p.wg.Wait()
}