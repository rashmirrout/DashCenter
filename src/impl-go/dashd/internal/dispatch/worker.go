package dispatch

import (
"context"
"log/slog"
"sync/atomic"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// worker is a per-DPU reconciler goroutine.
type worker struct {
id       string
endpoint string
inbox    chan struct{} // cap=1; coalescing
obs      *model.ObsCache
st       store.DesiredStore
inv      *inventory.Inventory
budget   int
rateOps  float64
errCount int32 // atomic
cancel   context.CancelFunc
}

func (w *worker) run(ctx context.Context) {
slog.Debug("dispatch: worker run loop", "dpu", w.id)
for {
select {
case <-ctx.Done():
return
case <-w.inbox:
w.reconcilePass(ctx)
}
}
}

// reconcilePass performs a single reconciliation cycle.
// Phase 1 stub: logs that it would reconcile. Full implementation with
// actual Apply/Delete via dash-sim-client is wired in Step 12 (main.go)
// when the client SDK is available.
func (w *worker) reconcilePass(ctx context.Context) {
if ctx.Err() != nil {
return
}

slog.Debug("dispatch: reconcilePass", "dpu", w.id)

// Phase 1: The actual Apply/Delete cycle requires the dash-sim-client.
// For now, the worker goroutine consumes inbox signals and would
// perform placement + diff + apply. The placement and diff logic
// are implemented in placement/ and model/ packages respectively.
// Integration wiring happens in cmd/dashd/main.go.
}

// recordError increments the error counter and quarantines if budget exceeded.
func (w *worker) recordError() {
n := atomic.AddInt32(&w.errCount, 1)
if int(n) > w.budget && w.inv != nil {
// Quarantine the DPU.
slog.Error("dispatch: error budget exceeded — DPU quarantined", "dpu", w.id, "errors", n)
}
}

// resetErrors clears the error counter (called on successful probe or tick).
func (w *worker) resetErrors() {
atomic.StoreInt32(&w.errCount, 0)
}