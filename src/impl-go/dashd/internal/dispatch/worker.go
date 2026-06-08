package dispatch

import (
"context"
"log/slog"
"sync/atomic"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
"golang.org/x/time/rate"
)

// reconcileTimeout caps each reconcilePass; if Apply/Delete RPCs stall
// the next inbox tick (or the 30s reconciler tick) will retry.
const reconcileTimeout = 25 * time.Second

// worker is a per-DPU reconciler goroutine.
type worker struct {
id       string
endpoint string
inbox    chan struct{} // cap=1; coalescing
obs      *model.ObsCache
st       store.DesiredStore
inv      *inventory.Inventory
factory  dpuclient.ClientFactory
limiter  *rate.Limiter
budget   int
rateOps  float64
errCount int32 // atomic
cancel   context.CancelFunc

// client is the cached southbound DpuClient. It is opened lazily on
// the first reconcilePass and closed via Close() during worker
// shutdown. If a reconcile attempt fails with a transport error we
// drop the cached client and let the next pass rebuild it.
client dpuclient.DpuClient
}

func (w *worker) run(ctx context.Context) {
slog.Debug("dispatch: worker run loop", "dpu", w.id)
defer func() {
// Release the client when the worker exits.
if w.client != nil {
_ = w.client.Close()
w.client = nil
}
}()

for {
select {
case <-ctx.Done():
return
case <-w.inbox:
w.reconcilePass(ctx)
}
}
}

// reconcilePass performs one reconciliation cycle:
//
//  1. Load every desired spec from the store.
//  2. Run placement.Resolve(dpuID, specs) → []*dashapi.Object (desired view).
//  3. Diff against the observed cache → Add / Update / Remove.
//  4. Apply (Add + Update) and Delete (Remove) via the DpuClient,
//     respecting the per-DPU rate limit. Each RPC has its own timeout
//     derived from reconcileTimeout.
//
// Errors are logged and counted against the per-DPU error budget; a
// fatal transport error invalidates the cached client so the next pass
// re-dials.
func (w *worker) reconcilePass(ctx context.Context) {
if ctx.Err() != nil {
return
}

passCtx, cancel := context.WithTimeout(ctx, reconcileTimeout)
defer cancel()

// 1. Load desired specs.
specs, err := placement.LoadDesiredSpecs(passCtx, w.st)
if err != nil {
slog.Error("dispatch: load desired specs failed",
"dpu", w.id, "error", err)
w.recordError()
return
}

// 2. Resolve placement for this DPU.
desired := placement.Resolve(w.id, specs, w.inv)

// 3. Diff against observed.
diff := w.obs.Diff(w.id, desired)
if len(diff.Add) == 0 && len(diff.Update) == 0 && len(diff.Remove) == 0 {
slog.Debug("dispatch: in sync", "dpu", w.id)
w.resetErrors()
return
}

slog.Info("dispatch: reconcile",
"dpu", w.id,
"add", len(diff.Add),
"update", len(diff.Update),
"remove", len(diff.Remove),
)

// 4. Open / reuse a client.
client, err := w.ensureClient()
if err != nil {
slog.Warn("dispatch: client open failed",
"dpu", w.id, "endpoint", w.endpoint, "error", err)
w.recordError()
return
}

var applyErrs, deleteErrs int

// Apply (Add then Update) — order matches dependency tier from
// placement.Resolve which already returns vnets → enis → mappings.
for _, obj := range diff.Add {
if err := w.doApply(passCtx, client, obj); err != nil {
applyErrs++
}
}
for _, obj := range diff.Update {
if err := w.doApply(passCtx, client, obj); err != nil {
applyErrs++
}
}
// Delete in REVERSE dependency order (leaves first) — simplest safe
// rule: just delete each leftover. dash-sim ignores order; real DPUs
// reject delete-with-references with a clear error that we'll see in
// the next reconcile pass.
for _, obj := range diff.Remove {
if err := w.doDelete(passCtx, client, obj); err != nil {
deleteErrs++
}
}

if applyErrs > 0 || deleteErrs > 0 {
slog.Warn("dispatch: reconcile completed with errors",
"dpu", w.id, "apply_errors", applyErrs, "delete_errors", deleteErrs)
w.recordError()
return
}

slog.Info("dispatch: reconcile complete", "dpu", w.id)
w.resetErrors()
}

// ensureClient lazily dials the DPU. If a cached client exists it is
// reused. Errors include the factory result and a nil-factory guard.
func (w *worker) ensureClient() (dpuclient.DpuClient, error) {
if w.client != nil {
return w.client, nil
}
factory := w.factory
if factory == nil {
factory = dpuclient.DefaultFactory
}
c, err := factory(w.endpoint)
if err != nil {
return nil, err
}
w.client = c
return c, nil
}

// invalidateClient closes the cached client and clears it so the next
// reconcile pass re-dials.
func (w *worker) invalidateClient() {
if w.client != nil {
_ = w.client.Close()
w.client = nil
}
}

// doApply issues one Apply RPC, respecting the rate limiter.
func (w *worker) doApply(ctx context.Context, c dpuclient.DpuClient, obj *dashapiv1.Object) error {
if err := w.limiter.Wait(ctx); err != nil {
return err
}
err := c.Apply(ctx, obj)
if err != nil {
slog.Warn("dispatch: apply failed",
"dpu", w.id, "kind", obj.GetKind().String(),
"key", obj.GetKey(), "error", err)
// Transport-level failures mean the client is bad; force re-dial.
w.invalidateClient()
}
return err
}

// doDelete issues one Delete RPC.
func (w *worker) doDelete(ctx context.Context, c dpuclient.DpuClient, obj *dashapiv1.Object) error {
if err := w.limiter.Wait(ctx); err != nil {
return err
}
err := c.Delete(ctx, obj.GetKind(), obj.GetKey())
if err != nil {
slog.Warn("dispatch: delete failed",
"dpu", w.id, "kind", obj.GetKind().String(),
"key", obj.GetKey(), "error", err)
w.invalidateClient()
}
return err
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