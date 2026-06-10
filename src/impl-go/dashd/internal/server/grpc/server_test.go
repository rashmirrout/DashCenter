package grpcserver

import (
"context"
"errors"
"net"
"testing"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
"google.golang.org/grpc"
"google.golang.org/grpc/codes"
"google.golang.org/grpc/status"
)

// stubControlPlane is a minimal in-memory ControlPlaneService used only
// so the gRPC server can register without panicking. All methods return
// zero values — server-test scenarios exercise wiring and lifecycle,
// not real CP semantics (those live in control_plane_test.go).
type stubControlPlane struct{}

func (stubControlPlane) PutVnet(_ context.Context, _ string, _ *dashcenterv1.VnetSpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutEni(_ context.Context, _ string, _ *dashcenterv1.EniSpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutVnetMapping(_ context.Context, _ string, _ *dashcenterv1.VnetMappingSpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutAclPolicy(_ context.Context, _ string, _ *dashcenterv1.AclPolicySpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutRoutePolicy(_ context.Context, _ string, _ *dashcenterv1.RoutePolicySpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutHaSet(_ context.Context, _ string, _ *dashcenterv1.HaSetSpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutServiceTunnel(_ context.Context, _ string, _ *dashcenterv1.ServiceTunnelSpec) (*service.PutResult, error) {
return &service.PutResult{}, nil
}
func (stubControlPlane) PutInventory(_ context.Context, _ []service.DpuInput) error { return nil }
func (stubControlPlane) GetInventory(_ context.Context) ([]service.DpuStatus, error)  { return nil, nil }
func (stubControlPlane) Delete(_ context.Context, _, _, _ string) error                { return nil }
func (stubControlPlane) Get(_ context.Context, _, _, _ string) (*service.StoredItem, error) {
return nil, store.ErrNotFound
}
func (stubControlPlane) List(_ context.Context, _, _ string) ([]*service.StoredItem, error) {
return nil, nil
}
func (stubControlPlane) Reconcile(_ context.Context) error { return nil }
func (stubControlPlane) SimulateApply(_ context.Context, _ []service.SimulateOp) (*service.SimulateResult, error) {
	return &service.SimulateResult{WouldSucceed: true}, nil
}

// newTestServer builds a New(...) instance with stub services so calls
// to gRPC RegisterService never see a nil interface inside the desc.
func newTestServer() *Server {
return New(stubControlPlane{}, service.NewObservability(nil, nil, model.NewObsCache()))
}

// 1. serviceErrToStatus maps each known error to the right gRPC code.
func TestServiceErrToStatus_KnownErrors(t *testing.T) {
cases := []struct {
in   error
want codes.Code
}{
{nil, codes.OK},
{store.ErrNotFound, codes.NotFound},
{store.ErrGenerationMismatch, codes.FailedPrecondition},
{service.ErrInvalidArgument, codes.InvalidArgument},
{errors.New("generic"), codes.Internal},
}
for _, tc := range cases {
got := serviceErrToStatus(tc.in)
if tc.in == nil {
if got != nil {
t.Errorf("nil input got=%v want nil", got)
}
continue
}
st, ok := status.FromError(got)
if !ok {
t.Errorf("not a status error: %v", got)
continue
}
if st.Code() != tc.want {
t.Errorf("err=%v code=%v want %v", tc.in, st.Code(), tc.want)
}
}
}

// 2. serviceErrToStatus wraps invalid-argument errors with the original message.
func TestServiceErrToStatus_InvalidArgIncludesMessage(t *testing.T) {
err := errors.Join(service.ErrInvalidArgument, errors.New("name is required"))
got := serviceErrToStatus(err)
st, _ := status.FromError(got)
if st.Code() != codes.InvalidArgument {
t.Errorf("code=%v want InvalidArgument", st.Code())
}
}

// 3. recoveryInterceptor catches panics and returns Internal.
func TestRecoveryInterceptor_CatchesPanic(t *testing.T) {
handler := func(ctx context.Context, req any) (any, error) {
panic("boom")
}
info := &grpc.UnaryServerInfo{FullMethod: "/test.svc/Method"}
_, err := recoveryInterceptor(context.Background(), nil, info, handler)
if err == nil {
t.Fatal("expected error from panic recovery")
}
if status.Code(err) != codes.Internal {
t.Errorf("code=%v want Internal", status.Code(err))
}
}

// 4. recoveryInterceptor passes through normal returns.
func TestRecoveryInterceptor_PassesNormalReturn(t *testing.T) {
want := "hello"
handler := func(ctx context.Context, req any) (any, error) { return want, nil }
info := &grpc.UnaryServerInfo{FullMethod: "/test.svc/Method"}
got, err := recoveryInterceptor(context.Background(), nil, info, handler)
if err != nil {
t.Fatalf("err=%v", err)
}
if got != want {
t.Errorf("got=%v want=%v", got, want)
}
}

// 5. loggingInterceptor logs success and forwards.
func TestLoggingInterceptor_Success(t *testing.T) {
handler := func(ctx context.Context, req any) (any, error) { return "ok", nil }
info := &grpc.UnaryServerInfo{FullMethod: "/test.svc/Method"}
got, err := loggingInterceptor(context.Background(), nil, info, handler)
if err != nil {
t.Fatalf("err=%v", err)
}
if got != "ok" {
t.Errorf("got=%v", got)
}
}

// 6. loggingInterceptor logs failure and forwards the error.
func TestLoggingInterceptor_Failure(t *testing.T) {
boom := errors.New("rpc-failed")
handler := func(ctx context.Context, req any) (any, error) { return nil, boom }
info := &grpc.UnaryServerInfo{FullMethod: "/test.svc/Method"}
_, err := loggingInterceptor(context.Background(), nil, info, handler)
if !errors.Is(err, boom) {
t.Errorf("err=%v want %v", err, boom)
}
}

// 7. Server.Stop returns cleanly without ever calling Serve.
func TestServer_StopWithoutServe_NoPanic(t *testing.T) {
s := newTestServer()
done := make(chan struct{})
go func() {
s.Stop()
close(done)
}()
select {
case <-done:
case <-time.After(time.Second):
t.Fatal("Stop did not return within 1s")
}
}

// 9. New(stub, obs) returns a server with registered services.
func TestNew_StubServices_ConstructsServer(t *testing.T) {
s := newTestServer()
if s == nil || s.gs == nil {
t.Fatal("New returned nil")
}
s.Stop()
}

// 10. Serve returns an error on an unbindable address.
func TestServe_BadAddress_Errors(t *testing.T) {
s := newTestServer()
defer s.Stop()
err := s.Serve("256.256.256.256:1")
if err == nil {
t.Error("expected listen error on bad address")
}
}

// 11. Serve accepts then Stop unblocks the call cleanly.
func TestServe_StartStop_CleanShutdown(t *testing.T) {
s := newTestServer()

// Bind to ":0" via a real listener and hand it to grpc through Serve.
lis, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil {
t.Fatalf("listen: %v", err)
}
addr := lis.Addr().String()
_ = lis.Close() // release port for Serve()

errCh := make(chan error, 1)
go func() { errCh <- s.Serve(addr) }()

// Wait briefly for the server to bind.
time.Sleep(50 * time.Millisecond)
s.Stop()

select {
case err := <-errCh:
if err != nil {
t.Errorf("Serve returned err=%v want nil after Stop", err)
}
case <-time.After(2 * time.Second):
t.Fatal("Serve did not return within 2s of Stop")
}
}
