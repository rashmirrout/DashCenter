package inventory

import (
"context"
"errors"
"sync/atomic"
"testing"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func alwaysOK(_ context.Context, _ string) error { return nil }
func alwaysFail(_ context.Context, _ string) error { return errors.New("unreachable") }

// 1. First success → REGISTERING → UP
func TestProberFirstSuccess(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

p := NewProber(inv, 50*time.Millisecond, alwaysOK)
go p.Run(ctx)

// Wait for probe to fire.
time.Sleep(200 * time.Millisecond)
cancel()

e, _ := inv.Get("dpu-0")
if e.State != dashcenterv1.DpuState_DPU_STATE_UP {
t.Errorf("expected UP, got %v", e.State)
}
}

// 2. 3 consecutive failures → UNREACHABLE
func TestProberThreeFailures(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

p := NewProber(inv, 30*time.Millisecond, alwaysFail)
go p.Run(ctx)

// Wait for at least 3 probe cycles.
time.Sleep(250 * time.Millisecond)
cancel()

e, _ := inv.Get("dpu-0")
if e.State != dashcenterv1.DpuState_DPU_STATE_UNREACHABLE {
t.Errorf("expected UNREACHABLE after 3 failures, got %v", e.State)
}
}

// 3. Recovery: UP → UNREACHABLE → UP
func TestProberRecovery(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

var failCount int32
probe := func(_ context.Context, _ string) error {
n := atomic.AddInt32(&failCount, 1)
if n <= 4 {
return errors.New("down")
}
return nil
}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

p := NewProber(inv, 30*time.Millisecond, probe)
go p.Run(ctx)

// Wait for failure phase + recovery.
time.Sleep(400 * time.Millisecond)
cancel()

e, _ := inv.Get("dpu-0")
if e.State != dashcenterv1.DpuState_DPU_STATE_UP {
t.Errorf("expected UP after recovery, got %v", e.State)
}
}

// 4. New DPU added mid-flight picked up on next tick
func TestProberNewDpuPickedUp(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

p := NewProber(inv, 50*time.Millisecond, alwaysOK)
go p.Run(ctx)

time.Sleep(100 * time.Millisecond)
// Add new DPU mid-flight.
inv.Register(entry("dpu-1", "localhost:50052"))
time.Sleep(200 * time.Millisecond)
cancel()

e, _ := inv.Get("dpu-1")
if e.State != dashcenterv1.DpuState_DPU_STATE_UP {
t.Errorf("expected new DPU to be UP, got %v", e.State)
}
}

// 5. Deregistered DPU stops probing
func TestProberDeregisteredStops(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

var probeCount int32
probe := func(_ context.Context, _ string) error {
atomic.AddInt32(&probeCount, 1)
return nil
}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

p := NewProber(inv, 50*time.Millisecond, probe)
go p.Run(ctx)

time.Sleep(150 * time.Millisecond)
beforeDeregister := atomic.LoadInt32(&probeCount)
inv.Deregister("dpu-0")
time.Sleep(200 * time.Millisecond)
afterDeregister := atomic.LoadInt32(&probeCount)
cancel()

// After deregister, probes should not grow much (at most 1-2 in-flight).
growth := afterDeregister - beforeDeregister
if growth > 3 {
t.Errorf("probes grew by %d after deregister (expected ≤3)", growth)
}
}

// 6. ctx cancel → clean shutdown
func TestProberCancelClean(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

ctx, cancel := context.WithCancel(context.Background())
p := NewProber(inv, 50*time.Millisecond, alwaysOK)

done := make(chan struct{})
go func() {
p.Run(ctx)
close(done)
}()

time.Sleep(100 * time.Millisecond)
cancel()

select {
case <-done:
// Clean shutdown.
case <-time.After(2 * time.Second):
t.Fatal("prober did not stop within 2s")
}
}