// Package reconciler implements the select-loop that drives reconciliation
// in response to desired-state changes, dirty DPU signals, forced reconciles,
// and periodic ticks.
package reconciler

import (
"context"
"log/slog"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dispatch"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Reconciler drives the reconcile loop.
type Reconciler struct {
store   store.DesiredStore
mgr     *dispatch.Manager
tick    time.Duration
forceCh chan struct{} // cap=1; coalescing
}

// New creates a Reconciler.
func New(s store.DesiredStore, mgr *dispatch.Manager, tick time.Duration) *Reconciler {
return &Reconciler{
store:   s,
mgr:     mgr,
tick:    tick,
forceCh: make(chan struct{}, 1),
}
}

// Run blocks until ctx is cancelled. It listens on four channels:
// 1. desired-state Watch events
// 2. dirty DPU signals from subscribe pumps
// 3. force-reconcile requests
// 4. periodic tick
func (r *Reconciler) Run(ctx context.Context) error {
desCh, err := r.store.Watch(ctx)
if err != nil {
return err
}
dirtyCh := r.mgr.DirtyReadC()
ticker := time.NewTicker(r.tick)
defer ticker.Stop()

slog.Info("reconciler: started", "tick", r.tick)

for {
select {
case <-ctx.Done():
slog.Info("reconciler: stopped")
return nil

case ev, ok := <-desCh:
if !ok {
slog.Warn("reconciler: desired watch channel closed")
return nil
}
r.onDesiredChange(ev)

case dpuID := <-dirtyCh:
r.mgr.Sync(dpuID)

case <-r.forceCh:
r.mgr.SyncAll()

case <-ticker.C:
r.mgr.SyncAll()
}
}
}

// ForceReconcile triggers SyncAll. Non-blocking (coalescing).
func (r *Reconciler) ForceReconcile() {
select {
case r.forceCh <- struct{}{}:
default:
}
}

func (r *Reconciler) onDesiredChange(ev store.DesiredEvent) {
switch ev.Type {
case store.EventResync:
slog.Info("reconciler: received EventResync, syncing all")
r.mgr.SyncAll()
return
case store.EventPut, store.EventDelete:
slog.Debug("reconciler: desired change", "type", ev.Type, "key", ev.Key)
// For simplicity in Phase 1, sync all DPUs on any desired change.
// Full AffectedDpus narrowing requires loading all specs; deferred
// to when worker.reconcilePass performs full placement.
r.mgr.SyncAll()
}
}