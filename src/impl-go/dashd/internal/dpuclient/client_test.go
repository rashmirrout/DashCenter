package dpuclient

import (
"context"
"errors"
"io"
"sync"
"testing"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
)

// --- Real client construction & error surfaces ---

func TestNew_EmptyEndpoint_Errors(t *testing.T) {
c, err := New("")
if err == nil {
t.Fatalf("expected error for empty endpoint, got client=%v", c)
}
if c != nil {
t.Fatalf("expected nil client on error, got %v", c)
}
}

func TestNew_ValidEndpoint_Succeeds(t *testing.T) {
// grpc.NewClient is lazy — it does NOT dial until first RPC, so a
// fake hostname is fine for construction.
c, err := New("127.0.0.1:1")
if err != nil {
t.Fatalf("New: %v", err)
}
if c == nil {
t.Fatal("expected non-nil client")
}
if err := c.Close(); err != nil {
t.Fatalf("Close: %v", err)
}
}

func TestRealClient_Close_Idempotent(t *testing.T) {
c, err := New("127.0.0.1:1")
if err != nil {
t.Fatalf("New: %v", err)
}
if err := c.Close(); err != nil {
t.Fatalf("first Close: %v", err)
}
// Second close must not panic and must return nil.
if err := c.Close(); err != nil {
t.Fatalf("second Close: %v", err)
}
// Third for good measure.
if err := c.Close(); err != nil {
t.Fatalf("third Close: %v", err)
}
}

func TestRealClient_Apply_NilObject_Errors(t *testing.T) {
c, _ := New("127.0.0.1:1")
defer c.Close()
err := c.Apply(context.Background(), nil)
if err == nil {
t.Fatal("expected error for nil object")
}
}

func TestRealClient_NilReceiver_CloseSafe(t *testing.T) {
// Documented contract: nil-safe Close. Helps shutdown paths that
// may be called even when New failed.
var c *realClient
if err := c.Close(); err != nil {
t.Fatalf("nil receiver Close should return nil, got %v", err)
}
}

// --- isNotFound / containsFold pure helpers ---

func TestIsNotFound_TableDriven(t *testing.T) {
cases := []struct {
in   string
want bool
}{
{"", false},
{"not found", true},
{"NotFound", true},
{"NOT FOUND", true},
{"key does not exist: not found", true},
{"object NotFound on dpu", true},
{"generic error", false},
{"already exists", false},
{"timeout", false},
}
for _, tc := range cases {
got := isNotFound(tc.in)
if got != tc.want {
t.Errorf("isNotFound(%q)=%v want %v", tc.in, got, tc.want)
}
}
}

func TestContainsFold_EdgeCases(t *testing.T) {
cases := []struct {
s, sub string
want   bool
}{
{"", "", true},
{"x", "", true},
{"", "x", false},
{"abc", "abcdef", false},
{"AbCdEf", "cde", true},
{"AbCdEf", "xyz", false},
{"hello", "HELLO", true},
}
for _, tc := range cases {
got := containsFold(tc.s, tc.sub)
if got != tc.want {
t.Errorf("containsFold(%q,%q)=%v want %v", tc.s, tc.sub, got, tc.want)
}
}
}

func TestEqFold_EdgeCases(t *testing.T) {
if !eqFold("ABC", "abc") {
t.Error("ABC/abc should match")
}
if eqFold("ABC", "abcd") {
t.Error("different lengths should not match")
}
if eqFold("abc", "xyz") {
t.Error("different chars should not match")
}
if !eqFold("", "") {
t.Error("empty/empty should match")
}
}

// --- MockClient behaviour ---

func TestMockClient_Apply_RecordsObjectAndReturnsNil(t *testing.T) {
m := NewMockClient()
obj := &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}}

if err := m.Apply(context.Background(), obj); err != nil {
t.Fatalf("Apply: %v", err)
}
if got, want := m.ApplyCallCount(), 1; got != want {
t.Errorf("ApplyCallCount=%d want %d", got, want)
}
if len(m.ApplyCalls) != 1 || m.ApplyCalls[0] != obj {
t.Errorf("Apply did not record object correctly: %+v", m.ApplyCalls)
}
}

func TestMockClient_Apply_ReturnsConfiguredErr(t *testing.T) {
m := NewMockClient()
wantErr := errors.New("boom")
m.ApplyErr = wantErr
obj := &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}}
if err := m.Apply(context.Background(), obj); !errors.Is(err, wantErr) {
t.Fatalf("Apply err=%v want %v", err, wantErr)
}
// Even on error the call is still recorded.
if got := m.ApplyCallCount(); got != 1 {
t.Errorf("ApplyCallCount=%d want 1", got)
}
}

func TestMockClient_Apply_CancelledCtx_ReturnsCtxErr(t *testing.T) {
m := NewMockClient()
ctx, cancel := context.WithCancel(context.Background())
cancel()
err := m.Apply(ctx, &dashapiv1.Object{})
if !errors.Is(err, context.Canceled) {
t.Errorf("Apply err=%v want Canceled", err)
}
}

func TestMockClient_Apply_CallHookOverridesErr(t *testing.T) {
m := NewMockClient()
hookErr := errors.New("hook-rejected")
m.CallHook = func(kind string, n int) error {
if kind == "apply" && n == 2 {
return hookErr
}
return nil
}
ctx := context.Background()
obj := &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}}

if err := m.Apply(ctx, obj); err != nil {
t.Fatalf("call 1: %v", err)
}
if err := m.Apply(ctx, obj); !errors.Is(err, hookErr) {
t.Fatalf("call 2 err=%v want %v", err, hookErr)
}
}

func TestMockClient_Delete_RecordsKindAndKey(t *testing.T) {
m := NewMockClient()
err := m.Delete(context.Background(),
dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v1"})
if err != nil {
t.Fatalf("Delete: %v", err)
}
if m.DeleteCallCount() != 1 {
t.Errorf("DeleteCallCount=%d want 1", m.DeleteCallCount())
}
calls := m.SnapshotDeletes()
if len(calls) != 1 {
t.Fatalf("snapshot len=%d", len(calls))
}
if calls[0].Kind != dashapiv1.ObjectKind_OBJECT_KIND_VNET {
t.Errorf("kind=%v", calls[0].Kind)
}
if len(calls[0].Key) != 1 || calls[0].Key[0] != "v1" {
t.Errorf("key=%v", calls[0].Key)
}
}

func TestMockClient_Delete_KeyIsDefensiveCopy(t *testing.T) {
m := NewMockClient()
key := []string{"v1", "10.0.0.1"}
_ = m.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, key)
key[0] = "MUTATED" // attempt to corrupt the mock's record
calls := m.SnapshotDeletes()
if calls[0].Key[0] != "v1" {
t.Errorf("key was not defensively copied: %v", calls[0].Key)
}
}

func TestMockClient_Delete_ReturnsConfiguredErr(t *testing.T) {
m := NewMockClient()
m.DeleteErr = errors.New("nope")
err := m.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v"})
if err == nil || err.Error() != "nope" {
t.Errorf("Delete err=%v", err)
}
}

func TestMockClient_Delete_CancelledCtx(t *testing.T) {
m := NewMockClient()
ctx, cancel := context.WithCancel(context.Background())
cancel()
err := m.Delete(ctx, dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v"})
if !errors.Is(err, context.Canceled) {
t.Errorf("Delete err=%v want Canceled", err)
}
}

func TestMockClient_Delete_CallHookOverridesErr(t *testing.T) {
m := NewMockClient()
hookErr := errors.New("delete-hook")
m.CallHook = func(kind string, n int) error {
if kind == "delete" {
return hookErr
}
return nil
}
err := m.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v"})
if !errors.Is(err, hookErr) {
t.Errorf("Delete err=%v want %v", err, hookErr)
}
}

func TestMockClient_SubscribeCallCount_Tracks(t *testing.T) {
m := NewMockClient()
if m.SubscribeCallCount() != 0 {
t.Errorf("initial SubscribeCallCount=%d", m.SubscribeCallCount())
}
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
_, _ = m.Subscribe(ctx, false)
_, _ = m.Subscribe(ctx, false)
if got := m.SubscribeCallCount(); got != 2 {
t.Errorf("SubscribeCallCount=%d want 2", got)
}
m.Reset()
if got := m.SubscribeCallCount(); got != 0 {
t.Errorf("after Reset, SubscribeCallCount=%d want 0", got)
}
}

func TestMockClient_Subscribe_RecvMsg_ErrPathReturnsErr(t *testing.T) {
m := NewMockClient()
// No events scripted → first RecvMsg will block until ctx done then EOF.
ctx, cancel := context.WithCancel(context.Background())
stream, _ := m.Subscribe(ctx, false)

// Cancel immediately so RecvMsg returns the error from Recv() before
// it can attempt the type assertion.
cancel()
var ev dashapiv1.Event
if err := stream.RecvMsg(&ev); err == nil {
t.Error("expected RecvMsg to return error after cancel")
}
}

func TestMockClient_Close_BlocksLaterCalls(t *testing.T) {
m := NewMockClient()
if err := m.Close(); err != nil {
t.Fatalf("Close: %v", err)
}
if err := m.Apply(context.Background(), &dashapiv1.Object{}); err == nil {
t.Error("Apply after Close should error")
}
if err := m.Delete(context.Background(), 0, nil); err == nil {
t.Error("Delete after Close should error")
}
if _, err := m.Subscribe(context.Background(), false); err == nil {
t.Error("Subscribe after Close should error")
}
}

func TestMockClient_Reset_ClearsCallsAndCounts(t *testing.T) {
m := NewMockClient()
_ = m.Apply(context.Background(), &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET})
_ = m.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v"})
if m.ApplyCallCount() != 1 || m.DeleteCallCount() != 1 {
t.Fatalf("pre-reset counts wrong: apply=%d del=%d", m.ApplyCallCount(), m.DeleteCallCount())
}
m.Reset()
if m.ApplyCallCount() != 0 || m.DeleteCallCount() != 0 {
t.Errorf("post-reset counts: apply=%d del=%d", m.ApplyCallCount(), m.DeleteCallCount())
}
if len(m.ApplyCalls) != 0 || len(m.DeleteCalls) != 0 {
t.Errorf("post-reset slices not cleared")
}
}

func TestMockClient_SnapshotIsDefensiveCopy(t *testing.T) {
m := NewMockClient()
obj := &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET}
_ = m.Apply(context.Background(), obj)

snap := m.SnapshotApplies()
snap[0] = nil // mutate caller's slice
again := m.SnapshotApplies()
if again[0] == nil {
t.Error("mock state was mutated by caller snapshot")
}
}

// --- MockClient.Subscribe ---

func TestMockClient_Subscribe_ReplaysAllEventsThenBlocks(t *testing.T) {
m := NewMockClient()
e1 := &dashapiv1.Event{
Type:   dashapiv1.EventType_EVENT_TYPE_SNAPSHOT,
Object: &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}},
}
e2 := &dashapiv1.Event{
Type:   dashapiv1.EventType_EVENT_TYPE_CREATED,
Object: &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_ENI, Key: []string{"e1"}},
}
m.EventsToSend = []*dashapiv1.Event{e1, e2}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

stream, err := m.Subscribe(ctx, true)
if err != nil {
t.Fatalf("Subscribe: %v", err)
}

got1, err := stream.Recv()
if err != nil || got1 != e1 {
t.Errorf("event1 got=%v err=%v", got1, err)
}
got2, err := stream.Recv()
if err != nil || got2 != e2 {
t.Errorf("event2 got=%v err=%v", got2, err)
}

// Third Recv must block; we cancel the ctx and expect io.EOF.
done := make(chan error, 1)
go func() {
_, err := stream.Recv()
done <- err
}()

time.Sleep(10 * time.Millisecond) // ensure goroutine is blocked
cancel()
select {
case err := <-done:
if !errors.Is(err, io.EOF) {
t.Errorf("after cancel, Recv err=%v want io.EOF", err)
}
case <-time.After(time.Second):
t.Fatal("Recv did not unblock after ctx cancel")
}
}

func TestMockClient_Subscribe_SubscribeErrReturned(t *testing.T) {
m := NewMockClient()
wantErr := errors.New("conn-refused")
m.SubscribeErr = wantErr
_, err := m.Subscribe(context.Background(), false)
if !errors.Is(err, wantErr) {
t.Errorf("Subscribe err=%v want %v", err, wantErr)
}
}

func TestMockClient_Subscribe_HookErrorAborts(t *testing.T) {
m := NewMockClient()
hookErr := errors.New("hook-failed")
m.SubscribeHook = func(n int) error {
if n == 2 {
return hookErr
}
return nil
}
ctx := context.Background()

// Call 1 succeeds.
s1, err := m.Subscribe(ctx, true)
if err != nil {
t.Fatalf("call1: %v", err)
}
if s1 == nil {
t.Fatal("call1 returned nil stream")
}
// Call 2 must fail via hook.
_, err = m.Subscribe(ctx, true)
if !errors.Is(err, hookErr) {
t.Errorf("call2 err=%v want %v", err, hookErr)
}
}

func TestMockClient_Subscribe_RecvMsg_CopiesEvent(t *testing.T) {
m := NewMockClient()
ev := &dashapiv1.Event{
Type:   dashapiv1.EventType_EVENT_TYPE_UPDATED,
Object: &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}},
}
m.EventsToSend = []*dashapiv1.Event{ev}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
stream, err := m.Subscribe(ctx, false)
if err != nil {
t.Fatalf("Subscribe: %v", err)
}

var out dashapiv1.Event
if err := stream.RecvMsg(&out); err != nil {
t.Fatalf("RecvMsg: %v", err)
}
if out.GetType() != ev.GetType() {
t.Errorf("type mismatch got=%v want=%v", out.GetType(), ev.GetType())
}
}

func TestMockClient_Subscribe_RecvMsg_WrongTypeErrors(t *testing.T) {
m := NewMockClient()
m.EventsToSend = []*dashapiv1.Event{{Type: dashapiv1.EventType_EVENT_TYPE_SNAPSHOT}}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()
stream, _ := m.Subscribe(ctx, false)

var wrong dashapiv1.Object
err := stream.RecvMsg(&wrong)
if err == nil {
t.Error("expected error on wrong message type")
}
}

func TestMockClient_Subscribe_StreamAuxMethods(t *testing.T) {
m := NewMockClient()
ctx, cancel := context.WithCancel(context.Background())
defer cancel()
stream, _ := m.Subscribe(ctx, false)

// These exist mainly to satisfy the interface but are exercised here
// for coverage so future changes don't accidentally break them.
if _, err := stream.Header(); err != nil {
t.Errorf("Header err=%v", err)
}
if md := stream.Trailer(); md == nil {
t.Errorf("Trailer returned nil")
}
if err := stream.CloseSend(); err != nil {
t.Errorf("CloseSend err=%v", err)
}
if stream.Context() != ctx {
t.Errorf("Context did not propagate")
}
if err := stream.SendMsg("ignored"); err != nil {
t.Errorf("SendMsg err=%v", err)
}
}

// --- Factories ---

func TestMockFactory_AlwaysYieldsSameInstance(t *testing.T) {
m := NewMockClient()
f := MockFactory(m)

c1, err := f("any-endpoint:1")
if err != nil {
t.Fatalf("f: %v", err)
}
c2, err := f("other-endpoint:2")
if err != nil {
t.Fatalf("f: %v", err)
}
if c1.(*MockClient) != m || c2.(*MockClient) != m {
t.Error("MockFactory should return the same mock instance")
}
}

func TestNewMultiFactory_DispatchesByEndpoint(t *testing.T) {
mA := NewMockClient()
mB := NewMockClient()
f := NewMultiFactory(map[string]*MockClient{
"dpu-a:9443": mA,
"dpu-b:9443": mB,
})

a, err := f("dpu-a:9443")
if err != nil {
t.Fatalf("dpu-a: %v", err)
}
if a.(*MockClient) != mA {
t.Error("dpu-a should resolve to mA")
}
b, err := f("dpu-b:9443")
if err != nil {
t.Fatalf("dpu-b: %v", err)
}
if b.(*MockClient) != mB {
t.Error("dpu-b should resolve to mB")
}

if _, err := f("unknown:9443"); err == nil {
t.Error("unknown endpoint should error")
}
}

func TestFailingFactory_AlwaysErrors(t *testing.T) {
want := errors.New("transport-down")
f := FailingFactory(want)
c, err := f("anything:1")
if !errors.Is(err, want) {
t.Errorf("err=%v want=%v", err, want)
}
if c != nil {
t.Errorf("expected nil client, got %v", c)
}
}

func TestDefaultFactory_IsNew(t *testing.T) {
// DefaultFactory must be the production New constructor so that
// main.go can wire it without referencing implementation details.
c, err := DefaultFactory("127.0.0.1:1")
if err != nil {
t.Fatalf("DefaultFactory: %v", err)
}
defer c.Close()
if c == nil {
t.Fatal("DefaultFactory returned nil client")
}
}

// --- Concurrent stress: ensure mutex protects internal state ---

func TestMockClient_ConcurrentApplyDelete(t *testing.T) {
m := NewMockClient()
const N = 50
var wg sync.WaitGroup
ctx := context.Background()

for i := 0; i < N; i++ {
wg.Add(2)
go func(i int) {
defer wg.Done()
_ = m.Apply(ctx, &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v"}})
}(i)
go func(i int) {
defer wg.Done()
_ = m.Delete(ctx, dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v"})
}(i)
}
wg.Wait()

if got := m.ApplyCallCount(); got != N {
t.Errorf("ApplyCallCount=%d want %d", got, N)
}
if got := m.DeleteCallCount(); got != N {
t.Errorf("DeleteCallCount=%d want %d", got, N)
}
}