//go:build integration

// grpc_test.go: end-to-end scenarios that exercise the dashcenter.v1
// ControlPlane and ObservabilityService gRPC surfaces of the dashd daemon.
//
// These tests close P1B-G9 (integration suite) for the gRPC half: scenarios
// 3, 5, 12-gRPC, 13, and 14 from the Phase 1B integration matrix. They run
// against the same dashd + dash-sim harness as rest_test.go; the only new
// dependency is the generated dashcenterv1 client stubs.
//
// Test build tag: `go test -tags=integration ./test/integration/...`.

package integration

import (
"context"
"io"
"strings"
"testing"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"google.golang.org/grpc"
"google.golang.org/grpc/credentials/insecure"
)

// grpcDial wires a gRPC client connection to the dashd gRPC endpoint with
// insecure credentials (Phase 1 dev mode). Registers the close on t.Cleanup
// so callers don't have to defer.
func grpcDial(t *testing.T, addr string) *grpc.ClientConn {
t.Helper()
conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
if err != nil {
t.Fatalf("grpc.NewClient %s: %v", addr, err)
}
t.Cleanup(func() { _ = conn.Close() })
return conn
}

// 10. (Spec #3) PUT VNet via gRPC ControlPlane converges; REST GET sees it.
// Verifies the full gRPC write path: codec → service layer → store → reconciler
// → dispatch worker → dash-sim Apply, then REST GET reads through the shared
// service layer.
func TestIntegration_PutVnet_Converges_GRPC(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

conn := grpcDial(t, h.grpcAddr)
client := dashcenterv1.NewControlPlaneClient(conn)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

ack, err := client.PutVnet(ctx, &dashcenterv1.VnetSpec{
Namespace: "default", Name: "v1", Vni: 100,
})
if err != nil {
t.Fatalf("PutVnet via gRPC: %v", err)
}
if ack.GetTxnId() == "" {
t.Errorf("Ack.TxnId is empty; want g<gen>")
}

h.waitConverged(t, 30*time.Second)

// Cross-transport read: REST GET sees the same VNet written via gRPC,
// proving both adapters hit the same service-layer state.
code, body := httpDo(t, "GET", h.restURL+"/v1/vnets/v1", "")
if code != 200 {
t.Fatalf("REST GET after gRPC PUT: code=%d body=%s", code, body)
}
if !strings.Contains(string(body), `"v1"`) {
t.Errorf("REST GET response missing v1: %s", body)
}
}

// 11. (Spec #5) PUT ENI via gRPC ControlPlane converges; admin/eni-placement
// reports observed=true (proves dash-sim received the Apply and the subscribe
// pump returned the CREATED event).
func TestIntegration_PutEni_Converges_GRPC(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

// VNet prerequisite — written via REST to isolate the gRPC PutEni path.
httpDo(t, "PUT", h.restURL+"/v1/vnets/v1", `{"name":"v1","vni":100}`)

conn := grpcDial(t, h.grpcAddr)
client := dashcenterv1.NewControlPlaneClient(conn)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

ack, err := client.PutEni(ctx, &dashcenterv1.EniSpec{
Namespace:           "default",
Name:                "e1",
VnetName:            "v1",
MacAddress:          "aa:bb:cc:dd:ee:01",
UnderlayIp:          "10.1.1.1",
AdminState:          "enabled",
PlacementHintDpuIds: []string{h.dpuID},
})
if err != nil {
t.Fatalf("PutEni via gRPC: %v", err)
}
if ack.GetTxnId() == "" {
t.Errorf("Ack.TxnId is empty")
}

h.waitConverged(t, 30*time.Second)

code, body := httpDo(t, "GET", h.adminURL+"/admin/eni-placement?eni=e1", "")
if code != 200 {
t.Fatalf("eni-placement: %d %s", code, body)
}
if !strings.Contains(string(body), `"observed":true`) {
t.Errorf("e1 not observed after gRPC PUT: %s", body)
}
}

// 12. (Spec #12 gRPC) Force reconcile via ControlPlane.Reconcile RPC. Pairs
// with rest_test.go::TestIntegration_ForceReconcile_OK for the REST half.
func TestIntegration_ForceReconcile_GRPC(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

conn := grpcDial(t, h.grpcAddr)
client := dashcenterv1.NewControlPlaneClient(conn)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

ack, err := client.Reconcile(ctx, &dashcenterv1.ReconcileRequest{})
if err != nil {
t.Fatalf("Reconcile via gRPC: %v", err)
}
if ack == nil {
t.Fatal("Reconcile Ack is nil")
}
}

// 13. (Spec #13) REST↔gRPC parity. PUT a VNet via REST, then GET it via
// gRPC's ControlPlane.Get and verify the same data round-trips through both
// adapters. The original spec calls for List parity, but ControlPlane.List
// is a streaming Phase 2 RPC (Unimplemented in Phase 1); Get covers the same
// "are both transports backed by the same state" guarantee with a unary RPC.
func TestIntegration_Get_Parity(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

// Write via REST.
httpDo(t, "PUT", h.restURL+"/v1/vnets/v-parity",
`{"name":"v-parity","vni":200}`)
h.waitConverged(t, 30*time.Second)

// REST read.
code, restBody := httpDo(t, "GET", h.restURL+"/v1/vnets/v-parity", "")
if code != 200 {
t.Fatalf("REST GET: %d %s", code, restBody)
}
if !strings.Contains(string(restBody), `"v-parity"`) ||
!strings.Contains(string(restBody), `"vni":200`) {
t.Errorf("REST GET missing expected fields: %s", restBody)
}

// gRPC read.
conn := grpcDial(t, h.grpcAddr)
client := dashcenterv1.NewControlPlaneClient(conn)
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

po, err := client.Get(ctx, &dashcenterv1.NameRef{
Namespace: "default", Kind: "vnet", Name: "v-parity",
})
if err != nil {
t.Fatalf("gRPC Get: %v", err)
}
if po.GetVnet() == nil {
t.Fatal("PolicyObject.Vnet is nil from gRPC Get")
}
if po.GetVnet().GetName() != "v-parity" {
t.Errorf("gRPC vnet.name=%q want v-parity", po.GetVnet().GetName())
}
if po.GetVnet().GetVni() != 200 {
t.Errorf("gRPC vnet.vni=%d want 200", po.GetVnet().GetVni())
}
}

// 14. (Spec #14) ObservabilityService.GetDpuStatus over gRPC streams one
// DpuStatusReport per known DPU. The harness has registered a DPU and the
// prober brings it from REGISTERING to UP within one tick; we poll until the
// state stabilizes at UP instead of trusting a single fixed sleep so the test
// is robust against slow-CI variance.
func TestIntegration_GetDpuStatus_GRPC(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

conn := grpcDial(t, h.grpcAddr)
client := dashcenterv1.NewObservabilityServiceClient(conn)

// Poll the streaming RPC up to 30s; each iteration opens a fresh stream,
// drains it, and looks for our DPU in DPU_STATE_UP. We can't keep the
// stream open continuously because Phase 1 GetDpuStatus is snapshot-only
// (one DpuStatusReport per DPU, then EOF) — see observability.go.
deadline := time.Now().Add(30 * time.Second)
var lastState dashcenterv1.DpuState = dashcenterv1.DpuState_DPU_STATE_UNSPECIFIED
for time.Now().Before(deadline) {
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
stream, err := client.GetDpuStatus(ctx, &dashcenterv1.DpuStatusRequest{})
if err != nil {
cancel()
t.Fatalf("GetDpuStatus open: %v", err)
}
var found *dashcenterv1.DpuStatusReport
for {
r, err := stream.Recv()
if err == io.EOF {
break
}
if err != nil {
cancel()
t.Fatalf("Recv: %v", err)
}
if r.GetIdentity().GetDpuId() == h.dpuID {
found = r
}
}
cancel()
if found != nil {
lastState = found.GetState()
if lastState == dashcenterv1.DpuState_DPU_STATE_UP {
return // success
}
}
time.Sleep(500 * time.Millisecond)
}
t.Fatalf("DPU %q never reached DPU_STATE_UP within 30s; last state=%v", h.dpuID, lastState)
}
