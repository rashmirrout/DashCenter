package dpuclient

import (
"context"
"fmt"
"io"
"sync"
"sync/atomic"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"google.golang.org/grpc"
"google.golang.org/grpc/metadata"
)

// MockClient is an in-process DpuClient used by unit tests. It records
// every Apply/Delete call for assertion, and replays a scripted list
// of Events on Subscribe. Failure injection is supported via the
// ApplyErr/DeleteErr/SubscribeErr fields and the per-call CallHook.
//
// All fields are safe for concurrent access — the mock is shared
// between the reconciler goroutine and the test driver.
type MockClient struct {
mu sync.Mutex

// Fields read by tests after the system under test runs.
ApplyCalls  []*dashapiv1.Object // append-only, in call order
DeleteCalls []DeleteCall

// Errors to inject on the next call. Cleared after one use unless
// you wrap them with persistent helpers in your own test.
ApplyErr     error
DeleteErr    error
SubscribeErr error

// Events delivered (in order) on every Subscribe stream until the
// list is exhausted, after which Recv blocks until ctx is done and
// then returns io.EOF.
EventsToSend []*dashapiv1.Event

// CallHook, if non-nil, runs at the start of every Apply/Delete and
// can return an override error. Useful for "fail nth call" patterns.
// kind is "apply" or "delete".
CallHook func(kind string, callNum int) error

// SubscribeHook, if non-nil, runs at the start of every Subscribe
// before EventsToSend are replayed. Returning err aborts the call.
SubscribeHook func(callNum int) error

closed       atomic.Bool
applyCount   atomic.Int64
deleteCount  atomic.Int64
subCallCount atomic.Int64
}

// DeleteCall captures one Delete invocation for assertion.
type DeleteCall struct {
Kind dashapiv1.ObjectKind
Key  []string
}

// NewMockClient returns an empty mock ready for use.
func NewMockClient() *MockClient { return &MockClient{} }

// Apply implements DpuClient. Records the object and returns ApplyErr
// or the CallHook's override, otherwise nil.
func (m *MockClient) Apply(ctx context.Context, obj *dashapiv1.Object) error {
if m.closed.Load() {
return fmt.Errorf("mockclient: closed")
}
if err := ctx.Err(); err != nil {
return err
}
n := int(m.applyCount.Add(1))
if m.CallHook != nil {
if err := m.CallHook("apply", n); err != nil {
return err
}
}
m.mu.Lock()
m.ApplyCalls = append(m.ApplyCalls, obj)
err := m.ApplyErr
m.mu.Unlock()
return err
}

// Delete implements DpuClient.
func (m *MockClient) Delete(ctx context.Context, kind dashapiv1.ObjectKind, key []string) error {
if m.closed.Load() {
return fmt.Errorf("mockclient: closed")
}
if err := ctx.Err(); err != nil {
return err
}
n := int(m.deleteCount.Add(1))
if m.CallHook != nil {
if err := m.CallHook("delete", n); err != nil {
return err
}
}
m.mu.Lock()
m.DeleteCalls = append(m.DeleteCalls, DeleteCall{Kind: kind, Key: append([]string(nil), key...)})
err := m.DeleteErr
m.mu.Unlock()
return err
}

// Subscribe implements DpuClient. Returns a fakeStream that replays
// EventsToSend and then blocks-then-EOFs.
func (m *MockClient) Subscribe(ctx context.Context, snapshotFirst bool) (grpc.ServerStreamingClient[dashapiv1.Event], error) {
if m.closed.Load() {
return nil, fmt.Errorf("mockclient: closed")
}
n := int(m.subCallCount.Add(1))
if m.SubscribeHook != nil {
if err := m.SubscribeHook(n); err != nil {
return nil, err
}
}
if m.SubscribeErr != nil {
return nil, m.SubscribeErr
}
m.mu.Lock()
events := append([]*dashapiv1.Event(nil), m.EventsToSend...)
m.mu.Unlock()
return &fakeSubscribeStream{ctx: ctx, events: events}, nil
}

// Close implements DpuClient. Idempotent.
func (m *MockClient) Close() error {
m.closed.Store(true)
return nil
}

// Reset clears recorded calls and counters. Keeps the configured
// EventsToSend / hooks intact so test setup can be reused.
func (m *MockClient) Reset() {
m.mu.Lock()
m.ApplyCalls = nil
m.DeleteCalls = nil
m.mu.Unlock()
m.applyCount.Store(0)
m.deleteCount.Store(0)
m.subCallCount.Store(0)
}

// ApplyCallCount returns the total number of Apply invocations
// (including those that returned an error).
func (m *MockClient) ApplyCallCount() int { return int(m.applyCount.Load()) }

// DeleteCallCount returns the total number of Delete invocations.
func (m *MockClient) DeleteCallCount() int { return int(m.deleteCount.Load()) }

// SubscribeCallCount returns the total number of Subscribe invocations.
func (m *MockClient) SubscribeCallCount() int { return int(m.subCallCount.Load()) }

// SnapshotApplies returns a defensive copy of recorded Apply objects.
func (m *MockClient) SnapshotApplies() []*dashapiv1.Object {
m.mu.Lock()
defer m.mu.Unlock()
out := make([]*dashapiv1.Object, len(m.ApplyCalls))
copy(out, m.ApplyCalls)
return out
}

// SnapshotDeletes returns a defensive copy of recorded Delete calls.
func (m *MockClient) SnapshotDeletes() []DeleteCall {
m.mu.Lock()
defer m.mu.Unlock()
out := make([]DeleteCall, len(m.DeleteCalls))
copy(out, m.DeleteCalls)
return out
}

// --- MockFactory plumbing ---

// MockFactory returns a ClientFactory that always yields the same
// MockClient, regardless of endpoint. Use this when one DPU is under
// test. For multi-DPU scenarios, see NewMultiFactory.
func MockFactory(m *MockClient) ClientFactory {
return func(endpoint string) (DpuClient, error) {
return m, nil
}
}

// NewMultiFactory builds a ClientFactory that dispatches by endpoint.
// Endpoints not in the map produce an error.
func NewMultiFactory(perEndpoint map[string]*MockClient) ClientFactory {
return func(endpoint string) (DpuClient, error) {
m, ok := perEndpoint[endpoint]
if !ok {
return nil, fmt.Errorf("mockclient: no mock for endpoint %q", endpoint)
}
return m, nil
}
}

// FailingFactory returns a ClientFactory that always fails with err.
// Used to test reconnect/backoff behaviour.
func FailingFactory(err error) ClientFactory {
return func(endpoint string) (DpuClient, error) {
return nil, err
}
}

// --- fake Subscribe stream ---

// fakeSubscribeStream is an in-memory implementation of
// grpc.ServerStreamingClient[dashapiv1.Event]. After draining
// the events slice, Recv blocks until ctx is done, then returns
// io.EOF — mirroring how a real stream behaves when the server
// closes cleanly.
type fakeSubscribeStream struct {
ctx    context.Context
events []*dashapiv1.Event
idx    int
mu     sync.Mutex
}

// Recv pops the next event, or blocks until ctx is done, then returns io.EOF.
func (s *fakeSubscribeStream) Recv() (*dashapiv1.Event, error) {
s.mu.Lock()
if s.idx < len(s.events) {
ev := s.events[s.idx]
s.idx++
s.mu.Unlock()
return ev, nil
}
s.mu.Unlock()
// No more scripted events — block until context is cancelled.
<-s.ctx.Done()
return nil, io.EOF
}

// Header is not used by dashd but must satisfy the interface.
func (s *fakeSubscribeStream) Header() (metadata.MD, error) { return metadata.MD{}, nil }

// Trailer is not used by dashd but must satisfy the interface.
func (s *fakeSubscribeStream) Trailer() metadata.MD { return metadata.MD{} }

// CloseSend is a no-op for server-streaming.
func (s *fakeSubscribeStream) CloseSend() error { return nil }

// Context returns the call ctx.
func (s *fakeSubscribeStream) Context() context.Context { return s.ctx }

// SendMsg is not used by dashd.
func (s *fakeSubscribeStream) SendMsg(m any) error { return nil }

// RecvMsg type-asserts m into *Event and delegates to Recv. Required
// to satisfy grpc.ClientStream.
//
// We copy field-by-field rather than `*out = *ev` because the generated
// proto types embed internal locks; a plain struct copy is unsafe (and
// `go vet` flags it). The fields we touch are exactly the ones dashd
// observes in tests.
func (s *fakeSubscribeStream) RecvMsg(m any) error {
ev, err := s.Recv()
if err != nil {
return err
}
out, ok := m.(*dashapiv1.Event)
if !ok {
return fmt.Errorf("fakeSubscribeStream: RecvMsg expects *Event, got %T", m)
}
out.Type = ev.Type
out.Object = ev.Object
return nil
}
