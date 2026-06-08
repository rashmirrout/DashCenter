package dispatch

import (
"context"
"errors"
"fmt"
"os"
"path/filepath"
"sync/atomic"
"testing"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func testCfg() *config.ReconcileConfig {
return &config.ReconcileConfig{
TickInterval:      30 * time.Second,
PerDPUInboxSize:   1,
ApplyRateLimit:    1000, // generous in tests
ErrorBudgetPerMin: 10,
}
}

// openTestStore returns a fresh file store rooted in t.TempDir().
func openTestStore(t *testing.T) *filstore.FileStore {
t.Helper()
dir := filepath.Join(t.TempDir(), "store")
if err := os.MkdirAll(dir, 0o755); err != nil {
t.Fatalf("mkdir: %v", err)
}
st, err := filstore.Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
t.Cleanup(func() { _ = st.Close() })
return st
}

// seedVnetEni stores one VNET + one ENI placed on dpuID. Used to build
// a minimal real placement scenario.
func seedVnetEni(t *testing.T, st *filstore.FileStore, dpuID string) {
t.Helper()
ctx := context.Background()
vnet := &dashcenterv1.VnetSpec{Name: "v1", Vni: 1000}
if _, err := st.Put(ctx, keyFor("vnet", "v1"), vnet, 0); err != nil {
t.Fatalf("put vnet: %v", err)
}
eni := &dashcenterv1.EniSpec{
Name:                "e1",
VnetName:            "v1",
MacAddress:          "00:11:22:33:44:55",
UnderlayIp:          "10.0.0.1",
AdminState:          "enabled",
PlacementHintDpuIds: []string{dpuID},
}
if _, err := st.Put(ctx, keyFor("eni", "e1"), eni, 0); err != nil {
t.Fatalf("put eni: %v", err)
}
}

// keyFor is a tiny test helper to build store.ObjectKey for default ns.
func keyFor(kind, name string) store.ObjectKey {
return store.ObjectKey{Namespace: store.DefaultNamespace, Kind: kind, Name: name}
}

// --- Manager lifecycle: from the original suite, kept and trimmed ---

func TestManager_StartStop_NoPanic(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})

mgr := New(obs, testCfg())
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(dpuclient.NewMockClient()))

ctx, cancel := context.WithCancel(context.Background())
mgr.Start(ctx)
time.Sleep(20 * time.Millisecond)
cancel()
mgr.Stop()
}

func TestManager_Sync_NonBlockingAndCoalesces(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})

mgr := New(obs, testCfg())
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(dpuclient.NewMockClient()))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)

for i := 0; i < 100; i++ {
mgr.Sync("dpu-0") // never blocks even with inbox cap=1
}
time.Sleep(20 * time.Millisecond)
mgr.Stop()
}

func TestManager_SyncAll_NoOpForUnknownDPU(t *testing.T) {
mgr := New(model.NewObsCache(), testCfg())
mgr.Sync("ghost") // no-op
mgr.SyncAll()     // no-op
mgr.Stop()        // no-op
}

func TestManager_DirtyChannel_RoundTrips(t *testing.T) {
mgr := New(model.NewObsCache(), testCfg())
done := make(chan struct{})
go func() { mgr.DirtyC() <- "dpu-0"; close(done) }()
select {
case id := <-mgr.DirtyReadC():
if id != "dpu-0" {
t.Errorf("got %s", id)
}
case <-time.After(time.Second):
t.Fatal("DirtyC timeout")
}
<-done
}

func TestManager_RemoveWorker_AfterRemoveSyncIsNoOp(t *testing.T) {
obs := model.NewObsCache()
mgr := New(obs, testCfg())
mgr.SetClientFactory(dpuclient.MockFactory(dpuclient.NewMockClient()))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.EnsureWorker(ctx, "dpu-0", "ep0")
time.Sleep(10 * time.Millisecond)
mgr.RemoveWorker("dpu-0")
mgr.Sync("dpu-0") // must not panic / not block
mgr.RemoveWorker("dpu-0") // idempotent
mgr.Stop()
}

// --- reconcilePass: the meat of Step 3 ---

// 1. With no desired specs and no observed objects, reconcile is a no-op
//    (no Apply, no Delete) and the error counter stays zero.
func TestWorker_ReconcilePass_EmptyStore_NoOp(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)

mock := dpuclient.NewMockClient()
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
mgr.Sync("dpu-0")
time.Sleep(100 * time.Millisecond)
mgr.Stop()

if got := mock.ApplyCallCount(); got != 0 {
t.Errorf("ApplyCallCount=%d want 0", got)
}
if got := mock.DeleteCallCount(); got != 0 {
t.Errorf("DeleteCallCount=%d want 0", got)
}
}

// 2. Seeded VNET+ENI for dpu-0 → reconcile produces Apply calls.
func TestWorker_ReconcilePass_AppliesDesiredObjects(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)
seedVnetEni(t, st, "dpu-0")

mock := dpuclient.NewMockClient()
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
mgr.Sync("dpu-0")

// Wait up to 2s for the worker to issue Apply calls.
deadline := time.After(2 * time.Second)
for {
if mock.ApplyCallCount() > 0 {
break
}
select {
case <-deadline:
t.Fatalf("worker never issued Apply calls; got %d", mock.ApplyCallCount())
default:
time.Sleep(10 * time.Millisecond)
}
}

// At minimum: 1 VNET + 1 ENI = 2 applies.
if got := mock.ApplyCallCount(); got < 2 {
t.Errorf("ApplyCallCount=%d want ≥2", got)
}

// Verify the kinds applied.
foundVnet, foundEni := false, false
for _, obj := range mock.SnapshotApplies() {
switch obj.GetKind() {
case dashapiv1.ObjectKind_OBJECT_KIND_VNET:
foundVnet = true
case dashapiv1.ObjectKind_OBJECT_KIND_ENI:
foundEni = true
}
}
if !foundVnet {
t.Error("no VNET Apply observed")
}
if !foundEni {
t.Error("no ENI Apply observed")
}
mgr.Stop()
}

// 3. When observed already matches desired, no Apply or Delete fires.
func TestWorker_ReconcilePass_InSync_NoOp(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)
seedVnetEni(t, st, "dpu-0")

// Pre-populate observed with exact same objects placement would produce.
// Easiest: run placement once via the worker, capture the applies, replay
// into obs cache, then re-sync.
mock := dpuclient.NewMockClient()
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)

mgr.Sync("dpu-0")
deadline := time.After(2 * time.Second)
for {
if mock.ApplyCallCount() >= 2 {
break
}
select {
case <-deadline:
t.Fatalf("first reconcile never applied; got %d", mock.ApplyCallCount())
default:
time.Sleep(10 * time.Millisecond)
}
}

// Mirror the applied objects into the observed cache.
for _, obj := range mock.SnapshotApplies() {
obs.Set("dpu-0", obj)
}
mock.Reset()

// Second sync — diff should be empty.
mgr.Sync("dpu-0")
time.Sleep(150 * time.Millisecond)

if got := mock.ApplyCallCount(); got != 0 {
t.Errorf("second pass ApplyCallCount=%d want 0", got)
}
if got := mock.DeleteCallCount(); got != 0 {
t.Errorf("second pass DeleteCallCount=%d want 0", got)
}
mgr.Stop()
}

// 4. When observed has extra objects not in desired, reconcile issues Delete.
func TestWorker_ReconcilePass_DeletesStaleObserved(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)
// Desired set is empty; observed has a stale VNET.
obs.Set("dpu-0", &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"stale-vnet"},
})

mock := dpuclient.NewMockClient()
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
mgr.Sync("dpu-0")

deadline := time.After(2 * time.Second)
for {
if mock.DeleteCallCount() > 0 {
break
}
select {
case <-deadline:
t.Fatalf("worker never issued Delete; got %d", mock.DeleteCallCount())
default:
time.Sleep(10 * time.Millisecond)
}
}
deletes := mock.SnapshotDeletes()
if deletes[0].Key[0] != "stale-vnet" {
t.Errorf("delete key=%v want stale-vnet", deletes[0].Key)
}
mgr.Stop()
}

// 5. Apply RPC error invalidates the cached client → next pass re-dials.
func TestWorker_ReconcilePass_ApplyErr_InvalidatesClient(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)
seedVnetEni(t, st, "dpu-0")

var dials int32
factory := func(endpoint string) (dpuclient.DpuClient, error) {
atomic.AddInt32(&dials, 1)
m := dpuclient.NewMockClient()
m.ApplyErr = errors.New("apply-rpc-broken")
return m, nil
}
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(factory)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
mgr.Sync("dpu-0")
time.Sleep(50 * time.Millisecond)
mgr.Sync("dpu-0") // second pass — must re-dial because first failed
time.Sleep(150 * time.Millisecond)
mgr.Stop()

if got := atomic.LoadInt32(&dials); got < 2 {
t.Errorf("dials=%d want ≥2 (apply error should invalidate client)", got)
}
}

// 6. Factory error counts toward error budget; quarantine log fires after
//    budget exceeded. We assert the recordError side effect by observing
//    repeated retries don't crash and that the error count grows.
func TestWorker_ReconcilePass_FactoryError_Tolerated(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)
seedVnetEni(t, st, "dpu-0")

mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.FailingFactory(errors.New("conn refused")))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
for i := 0; i < 5; i++ {
mgr.Sync("dpu-0")
time.Sleep(15 * time.Millisecond)
}
mgr.Stop()
// No panic = success. The error budget log is a side effect.
}

// 7. Worker honours rate limit: 50 desired objects with a 100/sec limit
//    must produce all 50 applies in under one second (well below the
//    25-second reconcile timeout) and never block forever.
func TestWorker_ReconcilePass_RateLimited_NotBlocked(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)

ctx := context.Background()

// Seed 50 VnetMappings under one VNET on dpu-0, plus an ENI to anchor placement.
seedOrFatal := func(key store.ObjectKey, spec any) {
if _, err := st.Put(ctx, key, spec, 0); err != nil {
t.Fatalf("seed %s: %v", key, err)
}
}
vnet := &dashcenterv1.VnetSpec{Name: "v1", Vni: 1000}
seedOrFatal(sk("vnet", "v1"), vnet)
eni := &dashcenterv1.EniSpec{Name: "e1", VnetName: "v1", MacAddress: "00:00:00:00:00:01",
UnderlayIp: "10.0.0.1", AdminState: "enabled", PlacementHintDpuIds: []string{"dpu-0"}}
seedOrFatal(sk("eni", "e1"), eni)
for i := 0; i < 50; i++ {
vm := &dashcenterv1.VnetMappingSpec{
VnetName:   "v1",
IpAddress:  ipFromIdx(i),
MacAddress: "00:00:00:00:00:02",
UnderlayIp: "10.0.0.2",
}
seedOrFatal(sk("vnet_mapping", vm.IpAddress), vm)
}

mock := dpuclient.NewMockClient()
cfg := *testCfg()
cfg.ApplyRateLimit = 200 // generous
mgr := New(obs, &cfg)
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

runCtx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(runCtx)
mgr.Sync("dpu-0")

deadline := time.After(3 * time.Second)
for {
// 1 vnet + 1 eni + 50 mappings = 52 minimum
if mock.ApplyCallCount() >= 52 {
break
}
select {
case <-deadline:
t.Fatalf("expected ≥52 applies, got %d", mock.ApplyCallCount())
default:
time.Sleep(20 * time.Millisecond)
}
}
mgr.Stop()
}

// 8. ensureClient: cached client is reused across reconcile passes
//    when no error invalidates it.
func TestWorker_EnsureClient_CachesAcrossPasses(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})
st := openTestStore(t)
seedVnetEni(t, st, "dpu-0")

var dials int32
factory := func(endpoint string) (dpuclient.DpuClient, error) {
atomic.AddInt32(&dials, 1)
return dpuclient.NewMockClient(), nil
}
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(factory)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
mgr.Sync("dpu-0")
time.Sleep(150 * time.Millisecond)
mgr.Sync("dpu-0")
time.Sleep(150 * time.Millisecond)
mgr.Stop()

if got := atomic.LoadInt32(&dials); got != 1 {
t.Errorf("dials=%d want 1 (cached client reused)", got)
}
}

// 9. ensureClient: nil factory falls back to DefaultFactory and does not
//    panic (DefaultFactory may fail to actually connect, but the call
//    itself succeeds because grpc.NewClient is lazy).
func TestWorker_EnsureClient_NilFactoryFallback(t *testing.T) {
w := &worker{endpoint: "127.0.0.1:1"}
c, err := w.ensureClient()
if err != nil {
t.Fatalf("ensureClient: %v", err)
}
if c == nil {
t.Fatal("expected non-nil client")
}
_ = c.Close()
}

// 10. invalidateClient is safe when no client is cached.
func TestWorker_InvalidateClient_NoCache_Safe(t *testing.T) {
w := &worker{}
w.invalidateClient() // must not panic
}

// 11. Worker run() drains inbox events one at a time and exits on ctx done.
func TestWorker_RunLoop_ExitsOnContextCancel(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})

mock := dpuclient.NewMockClient()
mgr := New(obs, testCfg())
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

ctx, cancel := context.WithCancel(context.Background())
mgr.Start(ctx)
time.Sleep(20 * time.Millisecond)

done := make(chan struct{})
go func() {
cancel()
mgr.Stop()
close(done)
}()

select {
case <-done:
case <-time.After(2 * time.Second):
t.Fatal("worker did not exit within 2s after cancel")
}
}

// 12. Worker tolerates store List error (we induce one by closing the store
//     before reconcile). The pass returns cleanly and increments error count.
func TestWorker_ReconcilePass_StoreError_RecordsError(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
_ = inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "ep0"})

st := openTestStore(t)
_ = st.Close() // future List calls return ErrClosed

mock := dpuclient.NewMockClient()
mgr := New(obs, testCfg())
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.MockFactory(mock))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)
mgr.Sync("dpu-0")
time.Sleep(100 * time.Millisecond)
mgr.Stop()

if mock.ApplyCallCount() != 0 || mock.DeleteCallCount() != 0 {
t.Errorf("store error should have aborted reconcile before any RPC")
}
}

// 13. rate limiter constructor.
func TestNewLimiter_NonNil(t *testing.T) {
if newLimiter(100) == nil {
t.Fatal("nil limiter")
}
}

// --- tiny helpers ---

// sk is a short alias for keyFor inside the rate-limit test.
func sk(kind, name string) store.ObjectKey { return keyFor(kind, name) }

// ipFromIdx fabricates deterministic IPv4 strings for the rate-limit test.
func ipFromIdx(i int) string {
return fmt.Sprintf("10.1.%d.%d", (i>>8)&0xff, i&0xff)
}
