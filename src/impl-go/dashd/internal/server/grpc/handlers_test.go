// handlers_test.go exercises the gRPC ControlPlane and ObservabilityService
// handlers over a real (bufconn) wire transport. This is the only place the
// proto-v2 codec, the generated ServiceDesc, and our adapter logic in
// control_plane.go / observability.go all run together end-to-end inside a
// single process.
//
// These tests close the coverage gap identified during Phase 1B audit: the
// pre-existing server_test.go only exercises interceptors, helpers, and
// server lifecycle. Without bufconn round-trips, marshal/unmarshal failures
// in the codec (which previously masked a broken proto-stub situation) would
// go undetected.
//
// Design
// ------
// * The test server is built from a richControlPlaneStub and richObservabilityStub
//   that record every call and return scriptable results. Stubs are deliberate;
//   real persistence is covered by service-layer tests.
// * Every test uses a fresh bufconn listener + grpc.Server pair created by
//   newBufServer(t), with t.Cleanup tearing both down.
// * The clients are the *generated* dashcenterv1.NewControlPlaneClient and
//   NewObservabilityServiceClient — same code path operators / dashctl use.
package grpcserver

import (
"context"
"errors"
"io"
"net"
"testing"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
"google.golang.org/grpc"
"google.golang.org/grpc/codes"
"google.golang.org/grpc/credentials/insecure"
"google.golang.org/grpc/status"
"google.golang.org/grpc/test/bufconn"
)

// --- Stubs that record calls + return scripted results -----------------------

// richControlPlaneStub implements service.ControlPlaneService for handler
// tests. It records the most recent (kind, namespace, name) and returns the
// scripted PutResult / error. Unscripted methods return zero values.
type richControlPlaneStub struct {
lastKind  string
lastNs    string
lastName  string
putResult *service.PutResult
putErr    error

getItem *service.StoredItem
getErr  error

deleteErr    error
reconcileErr error
}

func (s *richControlPlaneStub) PutVnet(_ context.Context, ns string, spec *dashcenterv1.VnetSpec) (*service.PutResult, error) {
s.lastKind, s.lastNs, s.lastName = "vnet", ns, spec.GetName()
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutEni(_ context.Context, ns string, spec *dashcenterv1.EniSpec) (*service.PutResult, error) {
s.lastKind, s.lastNs, s.lastName = "eni", ns, spec.GetName()
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutVnetMapping(_ context.Context, ns string, _ *dashcenterv1.VnetMappingSpec) (*service.PutResult, error) {
s.lastKind, s.lastNs = "vnet_mapping", ns
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutAclPolicy(_ context.Context, ns string, spec *dashcenterv1.AclPolicySpec) (*service.PutResult, error) {
s.lastKind, s.lastNs, s.lastName = "acl_policy", ns, spec.GetName()
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutRoutePolicy(_ context.Context, ns string, spec *dashcenterv1.RoutePolicySpec) (*service.PutResult, error) {
s.lastKind, s.lastNs, s.lastName = "route_policy", ns, spec.GetName()
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutHaSet(_ context.Context, ns string, spec *dashcenterv1.HaSetSpec) (*service.PutResult, error) {
s.lastKind, s.lastNs, s.lastName = "ha_set", ns, spec.GetName()
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutServiceTunnel(_ context.Context, ns string, _ *dashcenterv1.ServiceTunnelSpec) (*service.PutResult, error) {
s.lastKind, s.lastNs = "service_tunnel", ns
return s.putResult, s.putErr
}
func (s *richControlPlaneStub) PutInventory(_ context.Context, _ []service.DpuInput) error {
return nil
}
func (s *richControlPlaneStub) GetInventory(_ context.Context) ([]service.DpuStatus, error) {
return nil, nil
}
func (s *richControlPlaneStub) Delete(_ context.Context, ns, kind, name string) error {
s.lastKind, s.lastNs, s.lastName = kind, ns, name
return s.deleteErr
}
func (s *richControlPlaneStub) Get(_ context.Context, ns, kind, name string) (*service.StoredItem, error) {
s.lastKind, s.lastNs, s.lastName = kind, ns, name
return s.getItem, s.getErr
}
func (s *richControlPlaneStub) List(_ context.Context, _ string, _ string) ([]*service.StoredItem, error) {
return nil, nil
}
func (s *richControlPlaneStub) Reconcile(_ context.Context) error { return s.reconcileErr }

func (s *richControlPlaneStub) SimulateApply(_ context.Context, _ []service.SimulateOp) (*service.SimulateResult, error) {
	return &service.SimulateResult{WouldSucceed: true}, nil
}

// richObservabilityStub implements service.ObservabilityService.
type richObservabilityStub struct {
statuses []service.DpuStatus
statErr  error

driftItems []service.DriftItem
driftErr   error

lastDriftDpuID string
}

func (s *richObservabilityStub) GetDpuStatus(_ context.Context, _ []string) ([]service.DpuStatus, error) {
return s.statuses, s.statErr
}
func (s *richObservabilityStub) GetDrift(_ context.Context, dpuID string) ([]service.DriftItem, error) {
s.lastDriftDpuID = dpuID
return s.driftItems, s.driftErr
}
func (s *richObservabilityStub) GetHealth(_ context.Context) (*service.HealthStatus, error) {
return &service.HealthStatus{Status: "ok"}, nil
}

// --- Bufconn test harness -----------------------------------------------------

// bufServer pairs a running gRPC server with its bufconn listener and a
// pre-dialed client connection. The harness goroutine that hosts gs.Serve
// exits cleanly on Stop().
type bufServer struct {
cp   *richControlPlaneStub
obs  *richObservabilityStub
gs   *grpc.Server
conn *grpc.ClientConn
}

// newBufServer wires a fresh stub-backed gRPC server over an in-process
// bufconn pipe and returns a client connection ready for RPCs. Cleanup is
// registered with t so callers don't need to defer.
func newBufServer(t *testing.T) *bufServer {
t.Helper()
lis := bufconn.Listen(1 << 20) // 1 MiB buffer
cp := &richControlPlaneStub{}
obs := &richObservabilityStub{}

gs := grpc.NewServer()
dashcenterv1.RegisterControlPlaneServer(gs, &controlPlaneHandler{cp: cp})
dashcenterv1.RegisterObservabilityServiceServer(gs, &observabilityHandler{obs: obs})

serveErr := make(chan error, 1)
go func() { serveErr <- gs.Serve(lis) }()

dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
conn, err := grpc.NewClient("passthrough://bufnet",
grpc.WithContextDialer(dialer),
grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
gs.Stop()
t.Fatalf("grpc.NewClient: %v", err)
}

t.Cleanup(func() {
_ = conn.Close()
gs.Stop()
select {
case <-serveErr:
case <-time.After(2 * time.Second):
t.Logf("bufServer: Serve goroutine did not exit within 2s")
}
})

return &bufServer{cp: cp, obs: obs, gs: gs, conn: conn}
}

// --- ControlPlane handler tests -----------------------------------------------

// 1. PutVnet round-trip: client sends VnetSpec, server records the call and
// returns an Ack with the generation-derived TxnId.
func TestHandler_PutVnet_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 42}

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

ack, err := client.PutVnet(ctx, &dashcenterv1.VnetSpec{Namespace: "ns-a", Name: "v1", Vni: 100})
if err != nil {
t.Fatalf("PutVnet: %v", err)
}
if got, want := ack.GetTxnId(), "g42"; got != want {
t.Errorf("Ack.TxnId=%q want %q", got, want)
}
if b.cp.lastKind != "vnet" || b.cp.lastNs != "ns-a" || b.cp.lastName != "v1" {
t.Errorf("stub call=(kind=%q ns=%q name=%q) want (vnet ns-a v1)", b.cp.lastKind, b.cp.lastNs, b.cp.lastName)
}
}

// 2. PutVnet propagates InvalidArgument from the service layer.
func TestHandler_PutVnet_InvalidArg(t *testing.T) {
b := newBufServer(t)
b.cp.putErr = errors.Join(service.ErrInvalidArgument, errors.New("name required"))

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

_, err := client.PutVnet(ctx, &dashcenterv1.VnetSpec{})
if status.Code(err) != codes.InvalidArgument {
t.Errorf("PutVnet code=%v want InvalidArgument; err=%v", status.Code(err), err)
}
}

// 3. PutEni round-trip with placement hints. Verifies the spec namespace is
// extracted by the handler and forwarded to the service layer.
func TestHandler_PutEni_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 7}

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

ack, err := client.PutEni(ctx, &dashcenterv1.EniSpec{
Namespace: "team-a", Name: "e1", VnetName: "v1",
MacAddress: "aa:bb:cc:dd:ee:01", UnderlayIp: "10.1.1.1",
PlacementHintDpuIds: []string{"dpu-0"},
})
if err != nil {
t.Fatalf("PutEni: %v", err)
}
if ack.GetTxnId() != "g7" {
t.Errorf("Ack.TxnId=%q want g7", ack.GetTxnId())
}
if b.cp.lastNs != "team-a" {
t.Errorf("namespace=%q want team-a", b.cp.lastNs)
}
}

// 4. Get returns a PolicyObject with the correct oneof arm populated. This
// exercises both the wire codec on the response side AND the oneof setter
// path in storedItemToPolicyObject.
func TestHandler_Get_Vnet_Found(t *testing.T) {
b := newBufServer(t)
b.cp.getItem = &service.StoredItem{
Kind: "vnet", Name: "v1", Namespace: "ns-a", Generation: 5,
Spec: []byte(`{"name":"v1","vni":100,"namespace":"ns-a"}`),
}

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

po, err := client.Get(ctx, &dashcenterv1.NameRef{Namespace: "ns-a", Kind: "vnet", Name: "v1"})
if err != nil {
t.Fatalf("Get: %v", err)
}
if po.GetVnet() == nil {
t.Fatal("PolicyObject.Vnet is nil; want populated")
}
if got := po.GetVnet().GetName(); got != "v1" {
t.Errorf("Vnet.Name=%q want v1", got)
}
if got := po.GetVnet().GetVni(); got != 100 {
t.Errorf("Vnet.Vni=%d want 100", got)
}
if po.GetGeneration() != 5 {
t.Errorf("PolicyObject.Generation=%d want 5", po.GetGeneration())
}
}

// 5. Get for ENI populates the Eni oneof arm.
func TestHandler_Get_Eni_Found(t *testing.T) {
b := newBufServer(t)
b.cp.getItem = &service.StoredItem{
Kind: "eni", Name: "e1", Namespace: "default", Generation: 3,
Spec: []byte(`{"name":"e1","vnet_name":"v1","mac_address":"aa:bb:cc:dd:ee:01"}`),
}

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

po, err := client.Get(ctx, &dashcenterv1.NameRef{Namespace: "default", Kind: "eni", Name: "e1"})
if err != nil {
t.Fatalf("Get: %v", err)
}
if po.GetEni() == nil {
t.Fatal("PolicyObject.Eni is nil")
}
if got := po.GetEni().GetVnetName(); got != "v1" {
t.Errorf("Eni.VnetName=%q want v1", got)
}
}

// 6. Get propagates ErrNotFound from the store as codes.NotFound.
func TestHandler_Get_NotFound(t *testing.T) {
b := newBufServer(t)
b.cp.getErr = store.ErrNotFound

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

_, err := client.Get(ctx, &dashcenterv1.NameRef{Namespace: "default", Kind: "vnet", Name: "missing"})
if status.Code(err) != codes.NotFound {
t.Errorf("code=%v want NotFound", status.Code(err))
}
}

// 7. Delete success returns an empty Ack with no TxnId (no PutResult to derive
// one from) and forwards the ref tuple to the service layer.
func TestHandler_Delete_OK(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

_, err := client.Delete(ctx, &dashcenterv1.NameRef{Namespace: "ns-x", Kind: "vnet", Name: "v9"})
if err != nil {
t.Fatalf("Delete: %v", err)
}
if b.cp.lastKind != "vnet" || b.cp.lastNs != "ns-x" || b.cp.lastName != "v9" {
t.Errorf("stub call=(kind=%q ns=%q name=%q) want (vnet ns-x v9)", b.cp.lastKind, b.cp.lastNs, b.cp.lastName)
}
}

// 8. Reconcile success returns an Ack.
func TestHandler_Reconcile_OK(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

ack, err := client.Reconcile(ctx, &dashcenterv1.ReconcileRequest{})
if err != nil {
t.Fatalf("Reconcile: %v", err)
}
if ack == nil {
t.Fatal("Reconcile Ack is nil")
}
}

// 9. PutInventory is unimplemented (inherited from the embedded base) and
// must surface as codes.Unimplemented over the wire — proving the embed pattern
// works for the Phase 2 RPCs we deliberately did not override.
func TestHandler_PutInventory_Unimplemented(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

_, err := client.PutInventory(ctx, &dashcenterv1.PutInventoryRequest{})
if status.Code(err) != codes.Unimplemented {
t.Errorf("PutInventory code=%v want Unimplemented", status.Code(err))
}
}

// 10. RegisterDpu is also unimplemented (same family as PutInventory).
func TestHandler_RegisterDpu_Unimplemented(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

_, err := client.RegisterDpu(ctx, &dashcenterv1.DpuRegistration{})
if status.Code(err) != codes.Unimplemented {
t.Errorf("RegisterDpu code=%v want Unimplemented", status.Code(err))
}
}

// --- ObservabilityService handler tests ---------------------------------------

// 11. GetDpuStatus is server-streaming. Verify the handler streams one
// DpuStatusReport per stub status, then closes the stream cleanly.
func TestHandler_GetDpuStatus_Streams(t *testing.T) {
b := newBufServer(t)
b.obs.statuses = []service.DpuStatus{
{ID: "dpu-0", Endpoint: "127.0.0.1:9001", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"zone": "a"}},
{ID: "dpu-1", Endpoint: "127.0.0.1:9002", State: dashcenterv1.DpuState_DPU_STATE_DEGRADED},
}

client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

stream, err := client.GetDpuStatus(ctx, &dashcenterv1.DpuStatusRequest{})
if err != nil {
t.Fatalf("GetDpuStatus: %v", err)
}
var reports []*dashcenterv1.DpuStatusReport
for {
r, err := stream.Recv()
if err == io.EOF {
break
}
if err != nil {
t.Fatalf("Recv: %v", err)
}
reports = append(reports, r)
}
if got, want := len(reports), 2; got != want {
t.Fatalf("received %d reports want %d", got, want)
}
if reports[0].GetIdentity().GetDpuId() != "dpu-0" {
t.Errorf("report[0] dpu_id=%q want dpu-0", reports[0].GetIdentity().GetDpuId())
}
if reports[0].GetState() != dashcenterv1.DpuState_DPU_STATE_UP {
t.Errorf("report[0] state=%v want UP", reports[0].GetState())
}
if reports[1].GetState() != dashcenterv1.DpuState_DPU_STATE_DEGRADED {
t.Errorf("report[1] state=%v want DEGRADED", reports[1].GetState())
}
}

// 12. GetDpuStatus with empty inventory streams zero reports and closes
// cleanly. Confirms the empty-snapshot edge case.
func TestHandler_GetDpuStatus_Empty(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

stream, err := client.GetDpuStatus(ctx, &dashcenterv1.DpuStatusRequest{})
if err != nil {
t.Fatalf("GetDpuStatus: %v", err)
}
_, err = stream.Recv()
if err != io.EOF {
t.Errorf("Recv on empty stream: %v want EOF", err)
}
}

// 13. GetDrift translates service drift items to a wire DriftReport, mapping
// "apply" → DECLARED_NOT_OBSERVED and "delete" → OBSERVED_NOT_DECLARED.
func TestHandler_GetDrift_Translates(t *testing.T) {
b := newBufServer(t)
b.obs.driftItems = []service.DriftItem{
{Kind: "vnet/v1", DpuID: "dpu-0", Op: "apply", Detail: "missing on DPU"},
{Kind: "eni/e9", DpuID: "dpu-0", Op: "delete", Detail: "ghost on DPU"},
{Kind: "weird", DpuID: "dpu-0", Op: "unknown"},
}

client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

report, err := client.GetDrift(ctx, &dashcenterv1.DriftRequest{DpuIds: []string{"dpu-0"}})
if err != nil {
t.Fatalf("GetDrift: %v", err)
}
if got, want := report.GetItemsTotal(), int32(3); got != want {
t.Errorf("items_total=%d want %d", got, want)
}
items := report.GetItems()
if len(items) != 3 {
t.Fatalf("len(items)=%d want 3", len(items))
}
if items[0].GetKind() != dashcenterv1.DriftItem_DRIFT_KIND_DECLARED_NOT_OBSERVED {
t.Errorf("items[0].Kind=%v want DECLARED_NOT_OBSERVED", items[0].GetKind())
}
if items[1].GetKind() != dashcenterv1.DriftItem_DRIFT_KIND_OBSERVED_NOT_DECLARED {
t.Errorf("items[1].Kind=%v want OBSERVED_NOT_DECLARED", items[1].GetKind())
}
if items[2].GetKind() != dashcenterv1.DriftItem_DRIFT_KIND_UNSPECIFIED {
t.Errorf("items[2].Kind=%v want UNSPECIFIED (unknown op)", items[2].GetKind())
}
if b.obs.lastDriftDpuID != "dpu-0" {
t.Errorf("service GetDrift called with dpuID=%q want dpu-0", b.obs.lastDriftDpuID)
}
}

// 14. GetDrift with no DpuIds is treated as the empty-scope case and forwards
// an empty dpuID to the service layer (which the layer itself maps to nil).
func TestHandler_GetDrift_NoDpuIds(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

report, err := client.GetDrift(ctx, &dashcenterv1.DriftRequest{})
if err != nil {
t.Fatalf("GetDrift: %v", err)
}
if report.GetItemsTotal() != 0 {
t.Errorf("items_total=%d want 0", report.GetItemsTotal())
}
if b.obs.lastDriftDpuID != "" {
t.Errorf("service GetDrift dpuID=%q want empty", b.obs.lastDriftDpuID)
}
}

// 15. GetCounters is unimplemented (inherited from base) and must surface as
// codes.Unimplemented on the wire. Confirms the embed pattern works for the
// streaming RPCs we intentionally did not override.
func TestHandler_GetCounters_Unimplemented(t *testing.T) {
b := newBufServer(t)

client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

stream, err := client.GetCounters(ctx, &dashcenterv1.CounterRequest{})
if err != nil {
t.Fatalf("GetCounters open: %v", err)
}
_, err = stream.Recv()
if status.Code(err) != codes.Unimplemented {
t.Errorf("Recv code=%v want Unimplemented", status.Code(err))
}
}

// 16. ackFor with nil PutResult yields an empty Ack (defensive path; the
// service layer should never return nil/nil but we want the handler to not
// crash if it ever does).
func TestAckFor_NilResult(t *testing.T) {
a := ackFor(nil)
if a == nil {
t.Fatal("ackFor(nil) returned nil; want empty Ack")
}
if a.GetTxnId() != "" {
t.Errorf("Ack.TxnId=%q want empty for nil result", a.GetTxnId())
}
}

// 17. ackFor with a populated PutResult formats the generation as "g<N>".
func TestAckFor_FormatsTxnId(t *testing.T) {
a := ackFor(&service.PutResult{Accepted: true, Generation: 999})
if a.GetTxnId() != "g999" {
t.Errorf("Ack.TxnId=%q want g999", a.GetTxnId())
}
}

// 18. driftOpToKind exhaustively covers the enum mapping branches so coverage
// tooling sees every arm of the switch.
func TestDriftOpToKind_AllBranches(t *testing.T) {
cases := []struct {
op   string
want dashcenterv1.DriftItem_Kind
}{
{"apply", dashcenterv1.DriftItem_DRIFT_KIND_DECLARED_NOT_OBSERVED},
{"delete", dashcenterv1.DriftItem_DRIFT_KIND_OBSERVED_NOT_DECLARED},
{"", dashcenterv1.DriftItem_DRIFT_KIND_UNSPECIFIED},
{"garbage", dashcenterv1.DriftItem_DRIFT_KIND_UNSPECIFIED},
}
for _, tc := range cases {
got := driftOpToKind(tc.op)
if got != tc.want {
t.Errorf("driftOpToKind(%q)=%v want %v", tc.op, got, tc.want)
}
}
}

// 19. storedItemToPolicyObject with nil input returns Internal error. This is
// a defensive contract test — the handler should never pass nil here, but if
// the service layer ever returns (nil, nil) we surface it as an internal bug.
func TestStoredItemToPolicyObject_Nil(t *testing.T) {
_, err := storedItemToPolicyObject(nil)
if status.Code(err) != codes.Internal {
t.Errorf("nil item: code=%v want Internal", status.Code(err))
}
}

// 20. storedItemToPolicyObject with unknown kind returns Internal. Same
// defensive contract — the service layer validates kinds at Put time, so an
// unknown kind here means data corruption.
func TestStoredItemToPolicyObject_UnknownKind(t *testing.T) {
_, err := storedItemToPolicyObject(&service.StoredItem{
Kind: "nonexistent", Name: "x", Spec: []byte(`{}`),
})
if status.Code(err) != codes.Internal {
t.Errorf("unknown kind: code=%v want Internal", status.Code(err))
}
}

// 21. storedItemToPolicyObject for each spec kind. This batch test ensures
// every oneof arm in the proto is wired and unmarshaled correctly.
func TestStoredItemToPolicyObject_AllKinds(t *testing.T) {
cases := []struct {
kind   string
spec   string
verify func(t *testing.T, po *dashcenterv1.PolicyObject)
}{
{"vnet", `{"name":"v"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetVnet() == nil {
t.Error("Vnet arm not set")
}
}},
{"eni", `{"name":"e"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetEni() == nil {
t.Error("Eni arm not set")
}
}},
{"vnet_mapping", `{"ip_address":"10.0.0.1"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetVnetMapping() == nil {
t.Error("VnetMapping arm not set")
}
}},
{"acl_policy", `{"name":"a"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetAclPolicy() == nil {
t.Error("AclPolicy arm not set")
}
}},
{"route_policy", `{"name":"r"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetRoutePolicy() == nil {
t.Error("RoutePolicy arm not set")
}
}},
{"ha_set", `{"name":"h"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetHaSet() == nil {
t.Error("HaSet arm not set")
}
}},
{"service_tunnel", `{"name":"s"}`, func(t *testing.T, po *dashcenterv1.PolicyObject) {
if po.GetServiceTunnel() == nil {
t.Error("ServiceTunnel arm not set")
}
}},
}
for _, tc := range cases {
t.Run(tc.kind, func(t *testing.T) {
po, err := storedItemToPolicyObject(&service.StoredItem{
Kind: tc.kind, Name: "x", Spec: []byte(tc.spec),
})
if err != nil {
t.Fatalf("err=%v", err)
}
tc.verify(t, po)
})
}
}

// 22. storedItemToPolicyObject with malformed JSON for a known kind returns
// Internal. Covers the json.Unmarshal failure path inside each case branch.
func TestStoredItemToPolicyObject_MalformedJSON(t *testing.T) {
_, err := storedItemToPolicyObject(&service.StoredItem{
Kind: "vnet", Name: "v", Spec: []byte(`not-json`),
})
if status.Code(err) != codes.Internal {
t.Errorf("malformed JSON: code=%v want Internal", status.Code(err))
}
}

// 23. Per-kind wire tests for the remaining 5 Put* handlers. Each verifies
// the handler is wired into the generated ServiceDesc, the namespace is
// extracted from the spec, and the Ack TxnId is populated from the service
// PutResult. Without these the coverage tool sees the bodies as dead arms.
func TestHandler_PutVnetMapping_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 11}
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
ack, err := client.PutVnetMapping(ctx, &dashcenterv1.VnetMappingSpec{
Namespace: "ns-vm", VnetName: "v1", IpAddress: "10.0.0.1",
})
if err != nil {
t.Fatalf("PutVnetMapping: %v", err)
}
if ack.GetTxnId() != "g11" {
t.Errorf("TxnId=%q want g11", ack.GetTxnId())
}
if b.cp.lastKind != "vnet_mapping" || b.cp.lastNs != "ns-vm" {
t.Errorf("stub call=(kind=%q ns=%q) want (vnet_mapping ns-vm)", b.cp.lastKind, b.cp.lastNs)
}
}

func TestHandler_PutAclPolicy_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 12}
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
ack, err := client.PutAclPolicy(ctx, &dashcenterv1.AclPolicySpec{
Namespace: "ns-acl", Name: "acl1", Stage: "inbound",
})
if err != nil {
t.Fatalf("PutAclPolicy: %v", err)
}
if ack.GetTxnId() != "g12" {
t.Errorf("TxnId=%q want g12", ack.GetTxnId())
}
if b.cp.lastKind != "acl_policy" || b.cp.lastName != "acl1" {
t.Errorf("stub call=(kind=%q name=%q) want (acl_policy acl1)", b.cp.lastKind, b.cp.lastName)
}
}

func TestHandler_PutRoutePolicy_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 13}
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
ack, err := client.PutRoutePolicy(ctx, &dashcenterv1.RoutePolicySpec{
Namespace: "ns-rt", Name: "rt1",
})
if err != nil {
t.Fatalf("PutRoutePolicy: %v", err)
}
if ack.GetTxnId() != "g13" {
t.Errorf("TxnId=%q want g13", ack.GetTxnId())
}
if b.cp.lastKind != "route_policy" || b.cp.lastName != "rt1" {
t.Errorf("stub call=(kind=%q name=%q) want (route_policy rt1)", b.cp.lastKind, b.cp.lastName)
}
}

func TestHandler_PutHaSet_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 14}
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
ack, err := client.PutHaSet(ctx, &dashcenterv1.HaSetSpec{
Namespace: "ns-ha", Name: "ha1", Mode: "active_standby",
})
if err != nil {
t.Fatalf("PutHaSet: %v", err)
}
if ack.GetTxnId() != "g14" {
t.Errorf("TxnId=%q want g14", ack.GetTxnId())
}
if b.cp.lastKind != "ha_set" || b.cp.lastName != "ha1" {
t.Errorf("stub call=(kind=%q name=%q) want (ha_set ha1)", b.cp.lastKind, b.cp.lastName)
}
}

func TestHandler_PutServiceTunnel_OK(t *testing.T) {
b := newBufServer(t)
b.cp.putResult = &service.PutResult{Accepted: true, Generation: 15}
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
ack, err := client.PutServiceTunnel(ctx, &dashcenterv1.ServiceTunnelSpec{
Namespace: "ns-st", Name: "st1", LocalUnderlayIp: "10.0.0.1",
})
if err != nil {
t.Fatalf("PutServiceTunnel: %v", err)
}
if ack.GetTxnId() != "g15" {
t.Errorf("TxnId=%q want g15", ack.GetTxnId())
}
if b.cp.lastKind != "service_tunnel" || b.cp.lastNs != "ns-st" {
t.Errorf("stub call=(kind=%q ns=%q) want (service_tunnel ns-st)", b.cp.lastKind, b.cp.lastNs)
}
}

// 24. Error propagation on the other Put kinds — covers the `if err != nil`
// branch of each handler. We pick PutEni as representative since the others
// share an identical adapter pattern; the per-handler OK tests above already
// exercise the success arm of each.
func TestHandler_PutEni_GenerationMismatch(t *testing.T) {
b := newBufServer(t)
b.cp.putErr = store.ErrGenerationMismatch
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
_, err := client.PutEni(ctx, &dashcenterv1.EniSpec{Namespace: "ns", Name: "e1"})
if status.Code(err) != codes.FailedPrecondition {
t.Errorf("code=%v want FailedPrecondition", status.Code(err))
}
}

// 25. Delete propagates NotFound from store as codes.NotFound. Without this,
// only the success path of Delete is covered.
func TestHandler_Delete_NotFound(t *testing.T) {
b := newBufServer(t)
b.cp.deleteErr = store.ErrNotFound
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
_, err := client.Delete(ctx, &dashcenterv1.NameRef{Namespace: "ns", Kind: "vnet", Name: "x"})
if status.Code(err) != codes.NotFound {
t.Errorf("code=%v want NotFound", status.Code(err))
}
}

// 26. Reconcile propagates a service error as codes.Internal — covers the
// reconcile-error branch.
func TestHandler_Reconcile_Error(t *testing.T) {
b := newBufServer(t)
b.cp.reconcileErr = errors.New("boom")
client := dashcenterv1.NewControlPlaneClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
_, err := client.Reconcile(ctx, &dashcenterv1.ReconcileRequest{})
if status.Code(err) != codes.Internal {
t.Errorf("code=%v want Internal", status.Code(err))
}
}

// 27. GetDrift surfaces a service error as a status error. Without this the
// error branch of the GetDrift handler is uncovered.
func TestHandler_GetDrift_ServiceError(t *testing.T) {
b := newBufServer(t)
b.obs.driftErr = errors.New("inventory unreachable")
client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
_, err := client.GetDrift(ctx, &dashcenterv1.DriftRequest{DpuIds: []string{"dpu-0"}})
if status.Code(err) != codes.Internal {
t.Errorf("code=%v want Internal", status.Code(err))
}
}

// 28. GetDpuStatus surfaces a service error before any Send happens.
func TestHandler_GetDpuStatus_ServiceError(t *testing.T) {
b := newBufServer(t)
b.obs.statErr = errors.New("inventory unreachable")
client := dashcenterv1.NewObservabilityServiceClient(b.conn)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
stream, err := client.GetDpuStatus(ctx, &dashcenterv1.DpuStatusRequest{})
if err != nil {
t.Fatalf("open: %v", err)
}
_, err = stream.Recv()
if status.Code(err) != codes.Internal {
t.Errorf("Recv code=%v want Internal", status.Code(err))
}
}
