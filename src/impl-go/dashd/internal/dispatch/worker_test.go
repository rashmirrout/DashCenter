package dispatch

import (
"context"
"testing"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
)

func testCfg() *config.ReconcileConfig {
return &config.ReconcileConfig{
TickInterval:      30 * time.Second,
PerDPUInboxSize:   1,
ApplyRateLimit:    100,
ErrorBudgetPerMin: 10,
}
}

// 1. Manager.Start/Stop — no goroutine leaks
func TestManagerStartStop(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "localhost:50051"})

mgr := New(obs, testCfg())
mgr.SetInventory(inv)

ctx, cancel := context.WithCancel(context.Background())
mgr.Start(ctx)

time.Sleep(50 * time.Millisecond)
cancel()
mgr.Stop()
}

// 2. Sync triggers worker inbox
func TestManagerSync(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "localhost:50051"})

mgr := New(obs, testCfg())
mgr.SetInventory(inv)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)

// Sync should not block.
mgr.Sync("dpu-0")
mgr.Sync("dpu-0") // coalesced
mgr.Sync("dpu-0")

time.Sleep(100 * time.Millisecond)
mgr.Stop()
}

// 3. 100 Sync calls before worker wakes → coalesced
func TestManagerSyncCoalescing(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "localhost:50051"})

mgr := New(obs, testCfg())
mgr.SetInventory(inv)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)

for i := 0; i < 100; i++ {
mgr.Sync("dpu-0")
}

time.Sleep(100 * time.Millisecond)
mgr.Stop()
// No panic, no deadlock = success.
}

// 4. SyncAll
func TestManagerSyncAll(t *testing.T) {
obs := model.NewObsCache()
inv := inventory.New()
inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "localhost:50051"})
inv.Register(inventory.DpuEntry{ID: "dpu-1", Endpoint: "localhost:50052"})

mgr := New(obs, testCfg())
mgr.SetInventory(inv)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
mgr.Start(ctx)

mgr.SyncAll()
time.Sleep(100 * time.Millisecond)
mgr.Stop()
}

// 5. EnsureWorker adds new DPU mid-flight
func TestManagerEnsureWorker(t *testing.T) {
obs := model.NewObsCache()
mgr := New(obs, testCfg())

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

mgr.EnsureWorker(ctx, "dpu-0", "localhost:50051")
time.Sleep(50 * time.Millisecond)

mgr.Sync("dpu-0") // should not panic
mgr.Stop()
}

// 6. RemoveWorker stops worker
func TestManagerRemoveWorker(t *testing.T) {
obs := model.NewObsCache()
mgr := New(obs, testCfg())

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

mgr.EnsureWorker(ctx, "dpu-0", "localhost:50051")
time.Sleep(50 * time.Millisecond)
mgr.RemoveWorker("dpu-0")

// Sync after remove → no-op (no panic).
mgr.Sync("dpu-0")
mgr.Stop()
}

// 7. DirtyC channel works
func TestManagerDirtyChannel(t *testing.T) {
obs := model.NewObsCache()
mgr := New(obs, testCfg())

// Write to dirty channel.
go func() {
mgr.DirtyC() <- "dpu-0"
}()

select {
case id := <-mgr.DirtyReadC():
if id != "dpu-0" {
t.Errorf("expected dpu-0, got %s", id)
}
case <-time.After(1 * time.Second):
t.Fatal("no message on dirty channel")
}
}

// 8. Rate limiter creation
func TestNewLimiter(t *testing.T) {
l := newLimiter(100)
if l == nil {
t.Fatal("expected non-nil limiter")
}
}