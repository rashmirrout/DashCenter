// Package dispatch manages per-DPU worker goroutines that reconcile desired
// state onto each DPU via Apply/Delete calls.
package dispatch

import (
"context"
"log/slog"
"sync"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Manager owns per-DPU worker goroutines and the dirty channel shared
// with subscribe.Pump and reconciler.
type Manager struct {
cfg   *config.ReconcileConfig
obs   *model.ObsCache
st    store.DesiredStore
inv   *inventory.Inventory
dirty chan string

mu      sync.Mutex
workers map[string]*worker
wg      sync.WaitGroup
}

// New creates a Manager.
func New(obs *model.ObsCache, cfg *config.ReconcileConfig) *Manager {
cap := 16
if cfg != nil && cfg.PerDPUInboxSize > 0 {
cap = cfg.PerDPUInboxSize * 16
}
return &Manager{
cfg:     cfg,
obs:     obs,
dirty:   make(chan string, cap),
workers: make(map[string]*worker),
}
}

// SetStore wires the store (called once at startup).
func (m *Manager) SetStore(s store.DesiredStore) { m.st = s }

// SetInventory wires the inventory (called once at startup).
func (m *Manager) SetInventory(inv *inventory.Inventory) { m.inv = inv }

// DirtyC returns the writable side for subscribe.Pump.
func (m *Manager) DirtyC() chan<- string { return m.dirty }

// DirtyReadC returns the readable side for reconciler.
func (m *Manager) DirtyReadC() <-chan string { return m.dirty }

// Start launches a worker for every DPU in inventory.
func (m *Manager) Start(ctx context.Context) {
if m.inv == nil {
return
}
for _, e := range m.inv.List() {
m.EnsureWorker(ctx, e.ID, e.Endpoint)
}
}

// EnsureWorker creates a worker for dpuID if not already running.
func (m *Manager) EnsureWorker(ctx context.Context, dpuID, endpoint string) {
m.mu.Lock()
defer m.mu.Unlock()

if _, ok := m.workers[dpuID]; ok {
return
}

inboxSize := 1
if m.cfg != nil && m.cfg.PerDPUInboxSize > 0 {
inboxSize = m.cfg.PerDPUInboxSize
}

rateLimit := 100.0
if m.cfg != nil && m.cfg.ApplyRateLimit > 0 {
rateLimit = m.cfg.ApplyRateLimit
}

budget := 10
if m.cfg != nil && m.cfg.ErrorBudgetPerMin > 0 {
budget = m.cfg.ErrorBudgetPerMin
}

wCtx, cancel := context.WithCancel(ctx)
w := &worker{
id:       dpuID,
endpoint: endpoint,
inbox:    make(chan struct{}, inboxSize),
obs:      m.obs,
st:       m.st,
inv:      m.inv,
budget:   budget,
rateOps:  rateLimit,
cancel:   cancel,
}

m.workers[dpuID] = w
m.wg.Add(1)
go func() {
defer m.wg.Done()
w.run(wCtx)
}()

slog.Info("dispatch: worker started", "dpu", dpuID)
}

// RemoveWorker stops the worker for dpuID and removes it.
func (m *Manager) RemoveWorker(dpuID string) {
m.mu.Lock()
w, ok := m.workers[dpuID]
if ok {
delete(m.workers, dpuID)
}
m.mu.Unlock()

if ok {
w.cancel()
slog.Info("dispatch: worker removed", "dpu", dpuID)
}
}

// Sync requests a reconcile pass for dpuID. Non-blocking (coalescing).
func (m *Manager) Sync(dpuID string) {
m.mu.Lock()
w, ok := m.workers[dpuID]
m.mu.Unlock()

if !ok {
return
}
select {
case w.inbox <- struct{}{}:
default: // coalesced
}
}

// SyncAll calls Sync for every managed DPU.
func (m *Manager) SyncAll() {
m.mu.Lock()
ids := make([]string, 0, len(m.workers))
for id := range m.workers {
ids = append(ids, id)
}
m.mu.Unlock()

for _, id := range ids {
m.Sync(id)
}
}

// Stop gracefully stops all workers and waits.
func (m *Manager) Stop() {
m.mu.Lock()
for id, w := range m.workers {
w.cancel()
delete(m.workers, id)
}
m.mu.Unlock()
m.wg.Wait()
}