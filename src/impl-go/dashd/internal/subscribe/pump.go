// Package subscribe manages per-DPU Subscribe pumps that ingest observed
// state from DPU agents via the dashapi.v1.Subscribe stream and populate
// the ObsCache. One Pump = one DPU; PumpSet manages the fleet.
//
// The pump implements the snapshot-first contract: every successful
// (re)connect MUST first deliver an EVENT_TYPE_SNAPSHOT prelude of all
// existing objects, then live CREATED/UPDATED/DELETED events. Before
// processing snapshot events the pump atomically clears the DPU's
// observed cache so the next reconcile diff is computed against a fresh
// authoritative view of reality.
//
// Reconnect uses exponential backoff with a 1s base and 30s cap to
// avoid hammering a flapping DPU; the reconciler's 30s tick is the
// safety net if every subscribe attempt fails.
package subscribe

import (
"context"
"errors"
"io"
"log/slog"
"sync"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
)

// Default backoff bounds. Exposed for tests via SetBackoff.
const (
defaultBackoffMin = time.Second
defaultBackoffMax = 30 * time.Second
)

// Pump ingests observed state from a single DPU's Subscribe stream
// and updates the ObsCache. It reconnects with exponential backoff
// on every stream error or clean EOF.
type Pump struct {
dpuID    string
endpoint string
obs      *model.ObsCache
dirty    chan<- string
factory  dpuclient.ClientFactory

// backoff bounds — mutable only via SetBackoff for tests.
backoffMin time.Duration
backoffMax time.Duration
}

// New creates a new Pump for the given DPU.
// If factory is nil, dpuclient.DefaultFactory is used.
func New(dpuID, endpoint string, obs *model.ObsCache, dirty chan<- string, factory dpuclient.ClientFactory) *Pump {
if factory == nil {
factory = dpuclient.DefaultFactory
}
return &Pump{
dpuID:      dpuID,
endpoint:   endpoint,
obs:        obs,
dirty:      dirty,
factory:    factory,
backoffMin: defaultBackoffMin,
backoffMax: defaultBackoffMax,
}
}

// SetBackoff overrides the reconnect backoff bounds. Tests use very
// short durations to avoid waiting. min must be ≥ 1ns; max must be
// ≥ min. Invalid values are silently clamped.
func (p *Pump) SetBackoff(min, max time.Duration) {
if min < time.Nanosecond {
min = time.Nanosecond
}
if max < min {
max = min
}
p.backoffMin = min
p.backoffMax = max
}

// Run blocks until ctx is cancelled, repeatedly opening a Subscribe
// stream and forwarding events into the ObsCache. On any error the
// loop sleeps with exponential backoff then reconnects.
func (p *Pump) Run(ctx context.Context) {
slog.Info("subscribe: pump started", "dpu", p.dpuID, "endpoint", p.endpoint)
defer slog.Info("subscribe: pump stopped", "dpu", p.dpuID)

backoff := p.backoffMin
for {
if ctx.Err() != nil {
return
}

err := p.runOnce(ctx)
if ctx.Err() != nil {
return
}

if err == nil {
// Clean EOF from the server side — reset backoff and try again.
backoff = p.backoffMin
slog.Info("subscribe: stream closed cleanly, reconnecting", "dpu", p.dpuID)
} else {
slog.Warn("subscribe: stream error, will retry",
"dpu", p.dpuID, "error", err, "backoff", backoff)
}

// Sleep with backoff, but wake up immediately on shutdown.
select {
case <-ctx.Done():
return
case <-time.After(backoff):
}

backoff *= 2
if backoff > p.backoffMax {
backoff = p.backoffMax
}
}
}

// runOnce builds a client, opens a Subscribe stream, and drains events
// until error or EOF. Returns nil on clean EOF or non-nil on any error
// (including factory failure).
func (p *Pump) runOnce(ctx context.Context) error {
client, err := p.factory(p.endpoint)
if err != nil {
return err
}
// Close the client when this attempt ends. We never share clients
// across reconnect attempts because the underlying gRPC conn may
// have moved into TRANSIENT_FAILURE state.
defer func() {
_ = client.Close()
}()

stream, err := client.Subscribe(ctx, true /* snapshotFirst */)
if err != nil {
return err
}

// Atomically clear cache BEFORE processing snapshot events. The
// next event(s) MUST be EVENT_TYPE_SNAPSHOT per protocol — clearing
// here guarantees a consistent post-reconnect view even if some
// objects have been deleted since the last subscribe.
p.obs.ClearDpu(p.dpuID)
// Always signal dirty after a (re)connect — workers should diff
// fresh observed state against desired immediately.
p.signalDirty()

for {
ev, err := stream.Recv()
if err != nil {
if errors.Is(err, io.EOF) {
return nil
}
if ctx.Err() != nil {
return ctx.Err()
}
return err
}
p.handleEvent(ev)
}
}

// handleEvent applies one event to the ObsCache.
//   - SNAPSHOT, CREATED, UPDATED → Set
//   - DELETED                   → Delete
//
// After applying, signals dirty so the dispatch worker reconciles
// against the updated observed state.
func (p *Pump) handleEvent(ev *dashapiv1.Event) {
if ev == nil {
return
}
obj := ev.GetObject()
if obj == nil {
slog.Warn("subscribe: event without object", "dpu", p.dpuID, "type", ev.GetType())
return
}

switch ev.GetType() {
case dashapiv1.EventType_EVENT_TYPE_SNAPSHOT,
dashapiv1.EventType_EVENT_TYPE_CREATED,
dashapiv1.EventType_EVENT_TYPE_UPDATED:
p.obs.Set(p.dpuID, obj)
case dashapiv1.EventType_EVENT_TYPE_DELETED:
p.obs.Delete(p.dpuID, obj.GetKind(), obj.GetKey())
default:
slog.Warn("subscribe: unknown event type",
"dpu", p.dpuID, "type", ev.GetType())
return
}
p.signalDirty()
}

func (p *Pump) signalDirty() {
select {
case p.dirty <- p.dpuID:
default: // channel full → drop; reconciler 30s tick catches it
}
}

// PumpSet manages multiple Pumps, one per DPU.
type PumpSet struct {
obs     *model.ObsCache
dirty   chan<- string
factory dpuclient.ClientFactory

mu    sync.Mutex
pumps map[string]context.CancelFunc
wg    sync.WaitGroup

// Per-pump backoff override applied at creation time. Tests use this
// to make reconnect loops complete in milliseconds.
backoffMin time.Duration
backoffMax time.Duration
}

// NewSet creates a PumpSet. factory may be nil → uses dpuclient.DefaultFactory.
func NewSet(obs *model.ObsCache, dirty chan<- string, factory dpuclient.ClientFactory) *PumpSet {
return &PumpSet{
obs:        obs,
dirty:      dirty,
factory:    factory,
pumps:      make(map[string]context.CancelFunc),
backoffMin: defaultBackoffMin,
backoffMax: defaultBackoffMax,
}
}

// SetBackoff propagates backoff bounds to every Pump created later.
// Useful in tests to keep reconnect loops fast.
func (s *PumpSet) SetBackoff(min, max time.Duration) {
s.mu.Lock()
defer s.mu.Unlock()
if min < time.Nanosecond {
min = time.Nanosecond
}
if max < min {
max = min
}
s.backoffMin = min
s.backoffMax = max
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

p := New(dpuID, endpoint, s.obs, s.dirty, s.factory)
p.SetBackoff(s.backoffMin, s.backoffMax)

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

// Count returns the current number of running pumps. Test helper.
func (s *PumpSet) Count() int {
s.mu.Lock()
defer s.mu.Unlock()
return len(s.pumps)
}