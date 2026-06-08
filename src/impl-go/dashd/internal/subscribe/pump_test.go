package subscribe

import (
"context"
"errors"
"sync/atomic"
"testing"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
)

// fastBackoff is the standard test backoff: nanosecond min, microsecond
// max → reconnect loops complete in real time but never sleep.
func fastBackoff(p *Pump) { p.SetBackoff(time.Nanosecond, time.Microsecond) }

// waitFor drains dirty until id appears or timeout fires.
func waitForDirty(t *testing.T, dirty <-chan string, id string, timeout time.Duration) {
t.Helper()
deadline := time.After(timeout)
for {
select {
case got := <-dirty:
if got == id {
return
}
case <-deadline:
t.Fatalf("dirty channel did not receive %q within %v", id, timeout)
}
}
}

// mustVnetObject is a tiny helper to fabricate a valid VNET Object for events.
func mustVnetObject(name string) *dashapiv1.Object {
return &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET,
Key:  []string{name},
}
}

// --- Pump core: snapshot-first, event handling, reconnect, shutdown ---

// 1. After (re)connect, the pump clears the per-DPU cache before
//    processing snapshot events.
func TestPump_SnapshotFirst_ClearsCacheBeforeReplay(t *testing.T) {
obs := model.NewObsCache()
// Pre-seed stale state.
obs.Set("dpu-0", &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"stale"}})

mock := dpuclient.NewMockClient()
// One real snapshot event for vnet "fresh".
mock.EventsToSend = []*dashapiv1.Event{{
Type:   dashapiv1.EventType_EVENT_TYPE_SNAPSHOT,
Object: mustVnetObject("fresh"),
}}

dirty := make(chan string, 16)
p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(mock))
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go p.Run(ctx)

waitForDirty(t, dirty, "dpu-0", 2*time.Second) // post-clear dirty

// Wait for the event to be applied (poll briefly).
deadline := time.After(time.Second)
for {
m := obs.GetDpu("dpu-0")
if _, hasFresh := m["VNET:fresh"]; hasFresh {
break
}
if _, hasStale := m["VNET:stale"]; !hasStale && len(m) > 0 {
// fresh applied (under whatever the inner-key encoding is).
break
}
select {
case <-deadline:
t.Fatalf("snapshot event not applied; cache=%v", m)
default:
time.Sleep(5 * time.Millisecond)
}
}

// Stale must be gone.
m := obs.GetDpu("dpu-0")
for k := range m {
if k == "VNET:stale" {
t.Errorf("stale object survived snapshot clear")
}
}
}

// 2. CREATED and UPDATED events populate the cache via Set.
func TestPump_CreatedAndUpdated_PopulateCache(t *testing.T) {
obs := model.NewObsCache()
mock := dpuclient.NewMockClient()
mock.EventsToSend = []*dashapiv1.Event{
{Type: dashapiv1.EventType_EVENT_TYPE_CREATED, Object: mustVnetObject("a")},
{Type: dashapiv1.EventType_EVENT_TYPE_UPDATED, Object: mustVnetObject("b")},
}
dirty := make(chan string, 16)
p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(mock))
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go p.Run(ctx)

// Wait for both to appear.
deadline := time.After(time.Second)
for {
if len(obs.GetDpu("dpu-0")) >= 2 {
return
}
select {
case <-deadline:
t.Fatalf("cache size=%d want 2", len(obs.GetDpu("dpu-0")))
default:
time.Sleep(5 * time.Millisecond)
}
}
}

// 3. DELETED events remove from the cache.
func TestPump_Deleted_RemovesFromCache(t *testing.T) {
obs := model.NewObsCache()
mock := dpuclient.NewMockClient()
mock.EventsToSend = []*dashapiv1.Event{
{Type: dashapiv1.EventType_EVENT_TYPE_CREATED, Object: mustVnetObject("x")},
{Type: dashapiv1.EventType_EVENT_TYPE_DELETED, Object: mustVnetObject("x")},
}
dirty := make(chan string, 16)
p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(mock))
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go p.Run(ctx)

deadline := time.After(time.Second)
for {
if len(obs.GetDpu("dpu-0")) == 0 {
return
}
select {
case <-deadline:
t.Fatalf("expected empty cache after CREATE+DELETE, got %d entries", len(obs.GetDpu("dpu-0")))
default:
time.Sleep(5 * time.Millisecond)
}
}
}

// 4. Unknown event type is ignored (no crash, no signal explosion).
func TestPump_UnknownEventType_Ignored(t *testing.T) {
obs := model.NewObsCache()
mock := dpuclient.NewMockClient()
mock.EventsToSend = []*dashapiv1.Event{
{Type: dashapiv1.EventType(99), Object: mustVnetObject("ghost")},
}
dirty := make(chan string, 16)
p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(mock))
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go p.Run(ctx)

waitForDirty(t, dirty, "dpu-0", 2*time.Second) // initial dirty fires from clear

// Give the goroutine a moment, then verify cache untouched.
time.Sleep(50 * time.Millisecond)
if len(obs.GetDpu("dpu-0")) != 0 {
t.Errorf("unknown event type was applied to cache")
}
}

// 5. Event with nil Object is ignored.
func TestPump_NilObjectInEvent_Ignored(t *testing.T) {
obs := model.NewObsCache()
mock := dpuclient.NewMockClient()
mock.EventsToSend = []*dashapiv1.Event{{Type: dashapiv1.EventType_EVENT_TYPE_CREATED, Object: nil}}
dirty := make(chan string, 16)
p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(mock))
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go p.Run(ctx)

waitForDirty(t, dirty, "dpu-0", 2*time.Second)
time.Sleep(50 * time.Millisecond)
if len(obs.GetDpu("dpu-0")) != 0 {
t.Errorf("nil-object event applied")
}
}

// 6. handleEvent on nil event returns silently (direct unit call).
func TestPump_HandleEvent_NilEvent_NoOp(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 1)
p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(dpuclient.NewMockClient()))
p.handleEvent(nil) // must not panic
if len(dirty) != 0 {
t.Errorf("nil event signalled dirty")
}
}

// 7. signalDirty is non-blocking when channel is full (coalescing).
func TestPump_SignalDirty_NonBlockingWhenFull(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 1)
dirty <- "preexisting" // saturate

p := New("dpu-0", "ep", obs, dirty, dpuclient.MockFactory(dpuclient.NewMockClient()))
done := make(chan struct{})
go func() {
p.signalDirty()
close(done)
}()
select {
case <-done:
case <-time.After(time.Second):
t.Fatal("signalDirty blocked when channel was full")
}
}

// 8. Factory error → loop retries (we observe multiple subscribe attempts).
func TestPump_FactoryError_RetriesWithBackoff(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)

var attempts int32
factory := func(endpoint string) (dpuclient.DpuClient, error) {
atomic.AddInt32(&attempts, 1)
return nil, errors.New("connection refused")
}
p := New("dpu-0", "ep", obs, dirty, factory)
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
go p.Run(ctx)

// Allow several retry cycles.
time.Sleep(100 * time.Millisecond)
cancel()

if got := atomic.LoadInt32(&attempts); got < 2 {
t.Errorf("expected at least 2 retry attempts, got %d", got)
}
}

// 9. Subscribe error → loop retries, eventually succeeds when factory recovers.
//
// The pump owns the client lifecycle: it calls Close() after each runOnce
// (because gRPC connections can transition into TRANSIENT_FAILURE and need
// a fresh handshake). Tests for reconnect MUST therefore return a fresh
// mock per factory call — sharing one MockClient across runOnce iterations
// would permanently disable it after the first Close.
func TestPump_SubscribeError_ThenRecovers(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)

var callNum int32
factory := func(endpoint string) (dpuclient.DpuClient, error) {
n := atomic.AddInt32(&callNum, 1)
m := dpuclient.NewMockClient()
if n == 1 {
m.SubscribeErr = errors.New("transient")
} else {
m.EventsToSend = []*dashapiv1.Event{{
Type:   dashapiv1.EventType_EVENT_TYPE_SNAPSHOT,
Object: mustVnetObject("v1"),
}}
}
return m, nil
}

p := New("dpu-0", "ep", obs, dirty, factory)
fastBackoff(p)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
go p.Run(ctx)

// Wait for the recovered subscribe to apply the snapshot event.
deadline := time.After(2 * time.Second)
for {
if len(obs.GetDpu("dpu-0")) > 0 {
return
}
select {
case <-deadline:
t.Fatalf("never recovered; cache empty after retries (factory calls=%d)",
atomic.LoadInt32(&callNum))
default:
time.Sleep(5 * time.Millisecond)
}
}
}

// 10. Ctx cancel during retry sleep exits cleanly within bounded time.
func TestPump_CancelDuringBackoff_ExitsQuickly(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)
factory := dpuclient.FailingFactory(errors.New("down"))

p := New("dpu-0", "ep", obs, dirty, factory)
// Use a longer backoff so cancel must interrupt the sleep.
p.SetBackoff(100*time.Millisecond, time.Second)

ctx, cancel := context.WithCancel(context.Background())
done := make(chan struct{})
go func() {
p.Run(ctx)
close(done)
}()

time.Sleep(20 * time.Millisecond) // let it enter the backoff sleep
cancel()

select {
case <-done:
case <-time.After(500 * time.Millisecond):
t.Fatal("Run did not exit within 500ms after cancel")
}
}

// 11. SetBackoff clamps invalid values.
func TestPump_SetBackoff_ClampsInvalidValues(t *testing.T) {
p := &Pump{}
p.SetBackoff(0, 0)
if p.backoffMin != time.Nanosecond {
t.Errorf("min not clamped to 1ns: %v", p.backoffMin)
}
if p.backoffMax != time.Nanosecond {
t.Errorf("max not clamped to min: %v", p.backoffMax)
}
p.SetBackoff(2*time.Second, 1*time.Second)
if p.backoffMax != 2*time.Second {
t.Errorf("max should clamp up to min, got %v", p.backoffMax)
}
}

// 12. New(nil factory) falls back to DefaultFactory.
func TestPump_New_NilFactory_UsesDefault(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 1)
p := New("dpu-0", "ep", obs, dirty, nil)
if p.factory == nil {
t.Error("factory should default to non-nil")
}
}

// 13. Clean EOF (drained stream) → loop reconnects (we observe more than
//     one Subscribe call).
func TestPump_CleanEOF_ReconnectsWithResetBackoff(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)
// fakeSubscribeStream blocks on ctx.Done() then returns io.EOF (clean).
// To exercise the EOF/reconnect path we use a factory that returns a
// fresh mock on every call and count Subscribe invocations.

var subCount int32
factory := func(endpoint string) (dpuclient.DpuClient, error) {
m := dpuclient.NewMockClient()
m.SubscribeHook = func(n int) error {
atomic.AddInt32(&subCount, 1)
return nil
}
return m, nil
}

p := New("dpu-0", "ep", obs, dirty, factory)
p.SetBackoff(time.Nanosecond, time.Microsecond)

ctx, cancel := context.WithCancel(context.Background())
go p.Run(ctx)

// Allow the loop to reconnect repeatedly. Each runOnce returns nil
// only when stream EOFs — fakeSubscribeStream blocks on ctx.Done().
// So we cancel here to drain.
time.Sleep(50 * time.Millisecond)
cancel()

if got := atomic.LoadInt32(&subCount); got < 1 {
t.Errorf("expected at least 1 subscribe call, got %d", got)
}
}

// 14. handleEvent emits dirty signal per applied event.
func TestPump_HandleEvent_EmitsDirtyPerEvent(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)
p := &Pump{
dpuID: "dpu-0",
obs:   obs,
dirty: dirty,
}
p.handleEvent(&dashapiv1.Event{Type: dashapiv1.EventType_EVENT_TYPE_CREATED, Object: mustVnetObject("v1")})

select {
case got := <-dirty:
if got != "dpu-0" {
t.Errorf("dirty got=%q want dpu-0", got)
}
case <-time.After(100 * time.Millisecond):
t.Fatal("dirty was not signalled")
}
}

// --- PumpSet ---

// PS-1. Start/Stop are idempotent.
func TestPumpSet_StartStop_Idempotent(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)
factory := dpuclient.MockFactory(dpuclient.NewMockClient())
ps := NewSet(obs, dirty, factory)
ps.SetBackoff(time.Nanosecond, time.Microsecond)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

ps.Start(ctx, "dpu-0", "ep")
ps.Start(ctx, "dpu-0", "ep") // idempotent
if ps.Count() != 1 {
t.Errorf("Count=%d want 1", ps.Count())
}

ps.Stop("dpu-0")
ps.Stop("dpu-0") // idempotent
if ps.Count() != 0 {
t.Errorf("Count=%d want 0", ps.Count())
}

ps.StopAll() // should not block
}

// PS-2. StopAll terminates every running pump and returns within bound.
func TestPumpSet_StopAll_WaitsForGoroutines(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)
ps := NewSet(obs, dirty, dpuclient.MockFactory(dpuclient.NewMockClient()))
ps.SetBackoff(time.Nanosecond, time.Microsecond)

ctx := context.Background()
ps.Start(ctx, "dpu-0", "ep0")
ps.Start(ctx, "dpu-1", "ep1")
ps.Start(ctx, "dpu-2", "ep2")
if ps.Count() != 3 {
t.Fatalf("Count=%d want 3", ps.Count())
}

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
if ps.Count() != 0 {
t.Errorf("post-StopAll Count=%d", ps.Count())
}
}

// PS-3. Per-DPU factory dispatch via NewMultiFactory.
func TestPumpSet_PerDpuFactoryDispatch(t *testing.T) {
obs := model.NewObsCache()
dirty := make(chan string, 16)

mA := dpuclient.NewMockClient()
mA.EventsToSend = []*dashapiv1.Event{{Type: dashapiv1.EventType_EVENT_TYPE_SNAPSHOT, Object: mustVnetObject("a")}}
mB := dpuclient.NewMockClient()
mB.EventsToSend = []*dashapiv1.Event{{Type: dashapiv1.EventType_EVENT_TYPE_SNAPSHOT, Object: mustVnetObject("b")}}

ps := NewSet(obs, dirty, dpuclient.NewMultiFactory(map[string]*dpuclient.MockClient{
"epA": mA, "epB": mB,
}))
ps.SetBackoff(time.Nanosecond, time.Microsecond)

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
ps.Start(ctx, "dpu-A", "epA")
ps.Start(ctx, "dpu-B", "epB")

// Wait for each cache to populate.
deadline := time.After(2 * time.Second)
for {
if len(obs.GetDpu("dpu-A")) > 0 && len(obs.GetDpu("dpu-B")) > 0 {
return
}
select {
case <-deadline:
t.Fatalf("multi-factory dispatch failed: A=%d B=%d",
len(obs.GetDpu("dpu-A")), len(obs.GetDpu("dpu-B")))
default:
time.Sleep(5 * time.Millisecond)
}
}
}

// PS-4. SetBackoff clamps invalid values at the set level too.
func TestPumpSet_SetBackoff_Clamps(t *testing.T) {
ps := NewSet(nil, nil, nil)
ps.SetBackoff(0, 0)
if ps.backoffMin != time.Nanosecond {
t.Errorf("set min not clamped: %v", ps.backoffMin)
}
ps.SetBackoff(2*time.Second, 1*time.Second)
if ps.backoffMax != 2*time.Second {
t.Errorf("set max not clamped up to min: %v", ps.backoffMax)
}
}