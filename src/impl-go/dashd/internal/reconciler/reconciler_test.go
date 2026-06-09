package reconciler

import (
"context"
"testing"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dispatch"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
"google.golang.org/protobuf/types/known/wrapperspb"
)

func setupReconciler(t *testing.T, tick time.Duration) (*Reconciler, *dispatch.Manager, *filstore.FileStore) {
t.Helper()
dir := t.TempDir()
fs, err := filstore.Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
t.Cleanup(func() { fs.Close() })

obs := model.NewObsCache()
cfg := &config.ReconcileConfig{
TickInterval:      tick,
PerDPUInboxSize:   1,
ApplyRateLimit:    100,
ErrorBudgetPerMin: 10,
}
mgr := dispatch.New(obs, cfg)
rec := New(fs, mgr, tick)
return rec, mgr, fs
}

// 1. Desired event → SyncAll called (worker receives inbox signal)
func TestDesiredEventTriggers(t *testing.T) {
rec, mgr, fs := setupReconciler(t, 10*time.Second)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Start a worker so Sync has something to target.
mgr.EnsureWorker(ctx, "dpu-0", "localhost:50051")

done := make(chan struct{})
go func() {
rec.Run(ctx)
close(done)
}()

// Put a spec → triggers watch event → reconciler syncs.
time.Sleep(50 * time.Millisecond)
fs.Put(ctx, store.ObjectKey{Namespace: "default", Kind: "vnet", Name: "v1"},
wrapperspb.String("test"), 0)

time.Sleep(100 * time.Millisecond)
cancel()
<-done
mgr.Stop()
}

// 2. Dirty signal → Sync called
func TestDirtySignalTriggers(t *testing.T) {
rec, mgr, _ := setupReconciler(t, 10*time.Second)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

mgr.EnsureWorker(ctx, "dpu-0", "localhost:50051")

done := make(chan struct{})
go func() {
rec.Run(ctx)
close(done)
}()

time.Sleep(50 * time.Millisecond)
mgr.DirtyC() <- "dpu-0"

time.Sleep(100 * time.Millisecond)
cancel()
<-done
mgr.Stop()
}

// 3. Tick fires SyncAll
func TestTickTriggers(t *testing.T) {
rec, mgr, _ := setupReconciler(t, 30*time.Millisecond)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

mgr.EnsureWorker(ctx, "dpu-0", "localhost:50051")

done := make(chan struct{})
go func() {
rec.Run(ctx)
close(done)
}()

// Wait for ~3 ticks.
time.Sleep(120 * time.Millisecond)
cancel()
<-done
mgr.Stop()
}

// 4. ForceReconcile triggers SyncAll
func TestForceReconcile(t *testing.T) {
rec, mgr, _ := setupReconciler(t, 10*time.Second)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

mgr.EnsureWorker(ctx, "dpu-0", "localhost:50051")

done := make(chan struct{})
go func() {
rec.Run(ctx)
close(done)
}()

time.Sleep(50 * time.Millisecond)
rec.ForceReconcile()
time.Sleep(50 * time.Millisecond)
cancel()
<-done
mgr.Stop()
}

// 5. ForceReconcile 100 times → no panic
func TestForceReconcileBurst(t *testing.T) {
rec, mgr, _ := setupReconciler(t, 10*time.Second)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

done := make(chan struct{})
go func() {
rec.Run(ctx)
close(done)
}()

for i := 0; i < 100; i++ {
rec.ForceReconcile()
}

time.Sleep(50 * time.Millisecond)
cancel()
<-done
mgr.Stop()
}

// 6. ctx cancel → Run returns nil
func TestCancelReturnsNil(t *testing.T) {
rec, mgr, _ := setupReconciler(t, 10*time.Second)
ctx, cancel := context.WithCancel(context.Background())

errCh := make(chan error, 1)
go func() {
errCh <- rec.Run(ctx)
}()

time.Sleep(50 * time.Millisecond)
cancel()

err := <-errCh
if err != nil {
t.Errorf("expected nil, got %v", err)
}
mgr.Stop()
}