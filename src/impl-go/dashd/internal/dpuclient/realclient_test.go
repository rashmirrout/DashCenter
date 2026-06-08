package dpuclient

import (
"context"
"errors"
"net"
"strings"
"testing"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"google.golang.org/grpc"
"google.golang.org/grpc/credentials/insecure"
"google.golang.org/grpc/test/bufconn"
)

// stubDashApiServer is a minimal DashApiServer used to exercise the
// realClient's RPC encoding/decoding code paths in-process via bufconn.
// Every method is independently scriptable through the *Resp / *Err
// fields, which is enough to cover Apply/Delete/Subscribe success and
// failure branches plus the NotFound idempotency rule.
type stubDashApiServer struct {
dashapiv1.UnimplementedDashApiServer

ApplyAck     *dashapiv1.Ack
ApplyErr     error
DeleteAck    *dashapiv1.Ack
DeleteErr    error
EventsToSend []*dashapiv1.Event
SubscribeErr error

LastApply     *dashapiv1.ApplyRequest
LastDelete    *dashapiv1.DeleteRequest
LastSubscribe *dashapiv1.SubscribeRequest
}

func (s *stubDashApiServer) Apply(ctx context.Context, req *dashapiv1.ApplyRequest) (*dashapiv1.Ack, error) {
s.LastApply = req
if s.ApplyErr != nil {
return nil, s.ApplyErr
}
if s.ApplyAck != nil {
return s.ApplyAck, nil
}
return &dashapiv1.Ack{}, nil
}

func (s *stubDashApiServer) Delete(ctx context.Context, req *dashapiv1.DeleteRequest) (*dashapiv1.Ack, error) {
s.LastDelete = req
if s.DeleteErr != nil {
return nil, s.DeleteErr
}
if s.DeleteAck != nil {
return s.DeleteAck, nil
}
return &dashapiv1.Ack{}, nil
}

func (s *stubDashApiServer) Subscribe(req *dashapiv1.SubscribeRequest, stream grpc.ServerStreamingServer[dashapiv1.Event]) error {
s.LastSubscribe = req
if s.SubscribeErr != nil {
return s.SubscribeErr
}
for _, ev := range s.EventsToSend {
if err := stream.Send(ev); err != nil {
return err
}
}
// Hold the stream open briefly so the client side can fully receive
// scripted events; return cleanly so the client sees io.EOF.
<-stream.Context().Done()
return nil
}

// startBufconnServer registers the stub and returns a connected gRPC
// client conn together with a cleanup func.
func startBufconnServer(t *testing.T, stub *stubDashApiServer) (*grpc.ClientConn, func()) {
t.Helper()
const bufSize = 1 << 20

lis := bufconn.Listen(bufSize)
srv := grpc.NewServer()
dashapiv1.RegisterDashApiServer(srv, stub)

go func() {
if err := srv.Serve(lis); err != nil &&
!errors.Is(err, grpc.ErrServerStopped) &&
!strings.Contains(err.Error(), "closed") {
t.Errorf("bufconn server: %v", err)
}
}()

dialer := func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }
cc, err := grpc.NewClient("passthrough://bufnet",
grpc.WithContextDialer(dialer),
grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
t.Fatalf("dial bufnet: %v", err)
}

cleanup := func() {
_ = cc.Close()
srv.Stop()
_ = lis.Close()
}
return cc, cleanup
}

// makeRealClient builds a realClient backed by a bufconn server.
func makeRealClient(t *testing.T, stub *stubDashApiServer) (*realClient, func()) {
t.Helper()
cc, cleanup := startBufconnServer(t, stub)
return &realClient{cc: cc, api: dashapiv1.NewDashApiClient(cc)}, cleanup
}

// --- Apply ---

func TestRealClient_Apply_SuccessfulRoundTrip(t *testing.T) {
stub := &stubDashApiServer{}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

obj := &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}}
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

if err := c.Apply(ctx, obj); err != nil {
t.Fatalf("Apply: %v", err)
}
if stub.LastApply == nil || stub.LastApply.GetObject().GetKey()[0] != "v1" {
t.Errorf("server did not receive the object: %+v", stub.LastApply)
}
}

func TestRealClient_Apply_ServerErrorAckPropagates(t *testing.T) {
stub := &stubDashApiServer{ApplyAck: &dashapiv1.Ack{Error: "schema-invalid"}}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

err := c.Apply(context.Background(), &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET})
if err == nil || !strings.Contains(err.Error(), "schema-invalid") {
t.Errorf("Apply err=%v want contains schema-invalid", err)
}
}

func TestRealClient_Apply_TransportErrorPropagates(t *testing.T) {
// Build a client whose conn is closed → next RPC fails with a transport error.
stub := &stubDashApiServer{}
c, cleanup := makeRealClient(t, stub)
_ = c.cc.Close() // force transport failure
defer cleanup()

err := c.Apply(context.Background(), &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET})
if err == nil {
t.Error("expected transport error after Close")
}
}

// --- Delete ---

func TestRealClient_Delete_SuccessfulRoundTrip(t *testing.T) {
stub := &stubDashApiServer{}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

err := c.Delete(context.Background(),
dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"v1", "10.0.0.1"})
if err != nil {
t.Fatalf("Delete: %v", err)
}
if stub.LastDelete == nil ||
stub.LastDelete.GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING ||
len(stub.LastDelete.GetKey()) != 2 {
t.Errorf("server did not receive delete: %+v", stub.LastDelete)
}
}

func TestRealClient_Delete_NotFoundIsIdempotent(t *testing.T) {
stub := &stubDashApiServer{DeleteAck: &dashapiv1.Ack{Error: "object not found"}}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

// NotFound on Delete must be swallowed → client returns nil so the
// reconciler treats convergence as achieved.
if err := c.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"gone"}); err != nil {
t.Errorf("Delete with NotFound ack should return nil, got %v", err)
}
}

func TestRealClient_Delete_NonNotFoundAckPropagates(t *testing.T) {
stub := &stubDashApiServer{DeleteAck: &dashapiv1.Ack{Error: "internal error"}}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

err := c.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v1"})
if err == nil || !strings.Contains(err.Error(), "internal error") {
t.Errorf("Delete err=%v want contains 'internal error'", err)
}
}

func TestRealClient_Delete_KeyIsDefensivelyCopied(t *testing.T) {
stub := &stubDashApiServer{}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

key := []string{"v1", "10.0.0.1"}
_ = c.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, key)
key[0] = "MUTATED"
if stub.LastDelete.GetKey()[0] != "v1" {
t.Errorf("key was not defensively copied on the wire: %v", stub.LastDelete.GetKey())
}
}

// --- Subscribe ---

func TestRealClient_Subscribe_StreamsEventsThenEOF(t *testing.T) {
e1 := &dashapiv1.Event{Type: dashapiv1.EventType_EVENT_TYPE_SNAPSHOT,
Object: &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}}}
e2 := &dashapiv1.Event{Type: dashapiv1.EventType_EVENT_TYPE_CREATED,
Object: &dashapiv1.Object{Kind: dashapiv1.ObjectKind_OBJECT_KIND_ENI, Key: []string{"e1"}}}

stub := &stubDashApiServer{EventsToSend: []*dashapiv1.Event{e1, e2}}
c, cleanup := makeRealClient(t, stub)
defer cleanup()

ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

stream, err := c.Subscribe(ctx, true)
if err != nil {
t.Fatalf("Subscribe: %v", err)
}

got1, err := stream.Recv()
if err != nil {
t.Fatalf("recv1: %v", err)
}
if got1.GetType() != dashapiv1.EventType_EVENT_TYPE_SNAPSHOT {
t.Errorf("ev1 type=%v", got1.GetType())
}
got2, err := stream.Recv()
if err != nil {
t.Fatalf("recv2: %v", err)
}
if got2.GetType() != dashapiv1.EventType_EVENT_TYPE_CREATED {
t.Errorf("ev2 type=%v", got2.GetType())
}

// Server holds the stream open until ctx done — cancel and expect EOF.
cancel()
if _, err := stream.Recv(); err == nil {
t.Error("expected error after cancel")
}

if stub.LastSubscribe == nil || !stub.LastSubscribe.GetSnapshotFirst() {
t.Errorf("snapshot_first did not propagate: %+v", stub.LastSubscribe)
}
}

func TestRealClient_Subscribe_TransportErrorOnClosedConn(t *testing.T) {
stub := &stubDashApiServer{}
c, cleanup := makeRealClient(t, stub)
_ = c.cc.Close() // close before Subscribe
defer cleanup()

_, err := c.Subscribe(context.Background(), false)
if err == nil {
t.Error("expected error opening Subscribe on closed conn")
}
}

// TestRealClient_Delete_TransportErrorPropagates covers the transport-error
// branch of Delete that is otherwise only reachable via real gRPC failures.
func TestRealClient_Delete_TransportErrorPropagates(t *testing.T) {
stub := &stubDashApiServer{}
c, cleanup := makeRealClient(t, stub)
_ = c.cc.Close()
defer cleanup()

err := c.Delete(context.Background(), dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v1"})
if err == nil {
t.Error("expected transport error after Close")
}
}
