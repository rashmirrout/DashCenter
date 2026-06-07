// Package subscribe manages per-DPU Subscribe pumps that ingest observed state
// from DPU agents via dashapi.v1.Subscribe and populate the ObsCache.
package subscribe

import (
"context"
"log/slog"
"sync"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
)

// Pump ingests observed state from a single DPU's Subscribe stream
// and updates the ObsCache. It reconnects with backoff on stream errors.
type Pump struct {
dpuID    string
endpoint string
obs      *model.ObsCache
dirty    chan<- string
}

// New creates a new Pump for the given DPU.
func New(dpuID, endpoint string, obs *model.ObsCache, dirty chan<- string) *Pump {
return &Pump{
dpuID:    dpuID,
endpoint: endpoint,
obs:      obs,
dirty:    dirty,
}
}

// Run blocks until ctx is cancelled. In Phase 1, this is a stub that
// signals dirty on start and periodically. The full implementation with
// dashapi.Subscribe streaming is deferred to when dash-sim-client is
// integrated with real gRPC streaming.
func (p *Pump) Run(ctx context.Context) {
slog.Info("subscribe: pump started (stub)", "dpu", p.dpuID, "endpoint", p.endpoint)

// Clear stale cache on startup (snapshot-first contract).
p.obs.ClearDpu(p.dpuID)
p.signalDirty()

// Wait for cancellation.
<-ctx.Done()
slog.Info("subscribe: pump stopped", "dpu", p.dpuID)
}

func (p *Pump) signalDirty() {
select {
case p.dirty <- p.dpuID:
default: // channel full → drop; reconciler 30s tick catches it
}
}

// PumpSet manages multiple Pumps, one per DPU.
type PumpSet struct {
obs   *model.ObsCache
dirty chan<- string
mu    sync.Mutex
pumps map[string]context.CancelFunc
wg    sync.WaitGroup
}

// NewSet creates a PumpSet.
func NewSet(obs *model.ObsCache, dirty chan<- string) *PumpSet {
return &PumpSet{
obs:   obs,
dirty: dirty,
pumps: make(map[string]context.CancelFunc),
}
}

// Start launches a Pump for dpuID if not already running. Idempotent.
func (s *PumpSet) Start(ctx context.Context, dpuID, endpoint string) {
s.mu.Lock()
defer s.mu.Unlock()

if _, ok := s.pumps[dpuID]; ok {
return // already running
}

pumpCtx, cancel := context.WithCancel(ctx)
s.pumps[dpuID] = cancel

p := New(dpuID, endpoint, s.obs, s.dirty)
s.wg.Add(1)
go func() {
defer s.wg.Done()
p.Run(pumpCtx)
}()
}

// Stop terminates pump for dpuID. Idempotent.
func (s *PumpSet) Stop(dpuID string) {
s.mu.Lock()
cancel, ok := s.pumps[dpuID]
if ok {
delete(s.pumps, dpuID)
}
s.mu.Unlock()

if ok {
cancel()
}
}

// StopAll stops every pump and waits for goroutines to exit.
func (s *PumpSet) StopAll() {
s.mu.Lock()
for id, cancel := range s.pumps {
cancel()
delete(s.pumps, id)
}
s.mu.Unlock()
s.wg.Wait()
}