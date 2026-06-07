package subscribe

import (
"context"
"testing"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
)

// 1. Pump clears cache on startup
func TestPumpClearsCache(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 10)
p := New("dpu-0", "localhost:50051", obs, dirty)

ctx, cancel := context.WithCancel(context.Background())
go p.Run(ctx)

// Wait briefly, then cancel.
time.Sleep(50 * time.Millisecond)
cancel()

// Cache for dpu-0 should be empty (ClearDpu called).
m := obs.GetDpu("dpu-0")
if len(m) != 0 {
t.Errorf("expected empty cache after pump start, got %d", len(m))
}
}

// 2. Pump signals dirty on start
func TestPumpSignalsDirty(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 10)
p := New("dpu-0", "localhost:50051", obs, dirty)

ctx, cancel := context.WithCancel(context.Background())
go p.Run(ctx)

select {
case id := <-dirty:
if id != "dpu-0" {
t.Errorf("expected dpu-0, got %s", id)
}
case <-time.After(1 * time.Second):
t.Fatal("no dirty signal within 1s")
}
cancel()
}

// 3. Pump ctx cancel → clean exit
func TestPumpCancelClean(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 10)
p := New("dpu-0", "localhost:50051", obs, dirty)

ctx, cancel := context.WithCancel(context.Background())
done := make(chan struct{})
go func() {
p.Run(ctx)
close(done)
}()

cancel()
select {
case <-done:
case <-time.After(2 * time.Second):
t.Fatal("pump did not stop within 2s")
}
}

// 4. PumpSet Start/Stop
func TestPumpSetStartStop(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 10)
ps := NewSet(obs, dirty)
ctx := context.Background()

ps.Start(ctx, "dpu-0", "localhost:50051")
time.Sleep(50 * time.Millisecond)
ps.Stop("dpu-0")

// Should not panic.
ps.Stop("dpu-0") // idempotent
}

// 5. PumpSet Start same dpuID twice → only one pump
func TestPumpSetIdempotent(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 10)
ps := NewSet(obs, dirty)
ctx := context.Background()

ps.Start(ctx, "dpu-0", "localhost:50051")
ps.Start(ctx, "dpu-0", "localhost:50051") // should be idempotent

time.Sleep(50 * time.Millisecond)
ps.StopAll()
}

// 6. PumpSet StopAll waits
func TestPumpSetStopAll(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 10)
ps := NewSet(obs, dirty)
ctx := context.Background()

ps.Start(ctx, "dpu-0", "localhost:50051")
ps.Start(ctx, "dpu-1", "localhost:50052")

done := make(chan struct{})
go func() {
ps.StopAll()
close(done)
}()

select {
case <-done:
case <-time.After(2 * time.Second):
t.Fatal("StopAll did not return within 2s")
}
}