package service

import (
"context"
"testing"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func newTestService(t *testing.T) ControlPlaneService {
t.Helper()
dir := t.TempDir()
fs, err := filstore.Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
t.Cleanup(func() { fs.Close() })
inv := inventory.New()
return NewControlPlane(fs, inv, nil, nil, nil)
}

func TestPutVnet_OK(t *testing.T) {
svc := newTestService(t)
res, err := svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})
if err != nil {
t.Fatalf("PutVnet: %v", err)
}
if !res.Accepted || res.Generation < 1 {
t.Errorf("unexpected result: %+v", res)
}
}

func TestPutVnet_NilSpec(t *testing.T) {
svc := newTestService(t)
_, err := svc.PutVnet(context.Background(), "", nil)
if err == nil {
t.Fatal("expected error for nil spec")
}
}

func TestPutVnet_EmptyName(t *testing.T) {
svc := newTestService(t)
_, err := svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{})
if err == nil {
t.Fatal("expected error for empty name")
}
}

func TestPutEni_OK(t *testing.T) {
svc := newTestService(t)
// PA-5: ENI references a Vnet — the namespace validator now requires
// the Vnet to exist in the same namespace before the ENI is accepted.
if _, err := svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v1", Vni: 1}); err != nil {
	t.Fatalf("seed Vnet: %v", err)
}
res, err := svc.PutEni(context.Background(), "", &dashcenterv1.EniSpec{Name: "eni-1", VnetName: "v1"})
if err != nil {
t.Fatalf("PutEni: %v", err)
}
if !res.Accepted {
t.Error("expected accepted")
}
}

func TestPutEni_NilSpec(t *testing.T) {
svc := newTestService(t)
_, err := svc.PutEni(context.Background(), "", nil)
if err == nil {
t.Fatal("expected error")
}
}

func TestPutAclPolicy_OK(t *testing.T) {
svc := newTestService(t)
res, err := svc.PutAclPolicy(context.Background(), "", &dashcenterv1.AclPolicySpec{Name: "pol-1", Stage: "inbound"})
if err != nil {
t.Fatalf("PutAclPolicy: %v", err)
}
if !res.Accepted {
t.Error("expected accepted")
}
}

func TestPutRoutePolicy_OK(t *testing.T) {
svc := newTestService(t)
res, err := svc.PutRoutePolicy(context.Background(), "", &dashcenterv1.RoutePolicySpec{Name: "rp-1"})
if err != nil {
t.Fatalf("PutRoutePolicy: %v", err)
}
if !res.Accepted {
t.Error("expected accepted")
}
}

func TestPutHaSet_OK(t *testing.T) {
svc := newTestService(t)
res, err := svc.PutHaSet(context.Background(), "", &dashcenterv1.HaSetSpec{Name: "ha-1", Mode: "active_standby"})
if err != nil {
t.Fatalf("PutHaSet: %v", err)
}
if !res.Accepted {
t.Error("expected accepted")
}
}

func TestPutServiceTunnel_OK(t *testing.T) {
svc := newTestService(t)
res, err := svc.PutServiceTunnel(context.Background(), "", &dashcenterv1.ServiceTunnelSpec{Name: "st-1", Vni: 42})
if err != nil {
t.Fatalf("PutServiceTunnel: %v", err)
}
if !res.Accepted {
t.Error("expected accepted")
}
}

func TestPutVnetMapping_OK(t *testing.T) {
svc := newTestService(t)
// PA-5: VnetMapping references a Vnet — must exist in same namespace.
if _, err := svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v1", Vni: 1}); err != nil {
	t.Fatalf("seed Vnet: %v", err)
}
res, err := svc.PutVnetMapping(context.Background(), "", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"})
if err != nil {
t.Fatalf("PutVnetMapping: %v", err)
}
if !res.Accepted {
t.Error("expected accepted")
}
}

func TestPutVnetMapping_EmptyVnetName(t *testing.T) {
svc := newTestService(t)
_, err := svc.PutVnetMapping(context.Background(), "", &dashcenterv1.VnetMappingSpec{})
if err == nil {
t.Fatal("expected error for empty vnet_name")
}
}

func TestGetAfterPut(t *testing.T) {
svc := newTestService(t)
svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})
item, err := svc.Get(context.Background(), "", "vnet", "v1")
if err != nil {
t.Fatalf("Get: %v", err)
}
if item.Kind != "vnet" || item.Name != "v1" {
t.Errorf("unexpected item: %+v", item)
}
if item.Generation < 1 {
t.Error("expected generation >= 1")
}
}

func TestGet_NotFound(t *testing.T) {
svc := newTestService(t)
_, err := svc.Get(context.Background(), "", "vnet", "nope")
if err == nil {
t.Fatal("expected error")
}
}

func TestGet_EmptyKind(t *testing.T) {
svc := newTestService(t)
_, err := svc.Get(context.Background(), "", "", "v1")
if err == nil {
t.Fatal("expected error for empty kind")
}
}

func TestGet_EmptyName(t *testing.T) {
svc := newTestService(t)
_, err := svc.Get(context.Background(), "", "vnet", "")
if err == nil {
t.Fatal("expected error for empty name")
}
}

func TestListAfterPut(t *testing.T) {
svc := newTestService(t)
svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})
svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v2", Vni: 200})
items, err := svc.List(context.Background(), "", "vnet")
if err != nil {
t.Fatalf("List: %v", err)
}
if len(items) != 2 {
t.Errorf("expected 2 items, got %d", len(items))
}
}

func TestList_EmptyKind(t *testing.T) {
svc := newTestService(t)
_, err := svc.List(context.Background(), "", "")
if err == nil {
t.Fatal("expected error for empty kind")
}
}

func TestDeleteAfterPut(t *testing.T) {
svc := newTestService(t)
svc.PutVnet(context.Background(), "", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})
err := svc.Delete(context.Background(), "", "vnet", "v1")
if err != nil {
t.Fatalf("Delete: %v", err)
}
// Verify deleted.
_, err = svc.Get(context.Background(), "", "vnet", "v1")
if err == nil {
t.Fatal("expected not found after delete")
}
}

func TestDelete_NotFound(t *testing.T) {
svc := newTestService(t)
err := svc.Delete(context.Background(), "", "vnet", "nope")
if err == nil {
t.Fatal("expected error for not found")
}
}

func TestDelete_EmptyKind(t *testing.T) {
svc := newTestService(t)
err := svc.Delete(context.Background(), "", "", "v1")
if err == nil {
t.Fatal("expected error for empty kind")
}
}

func TestPutInventory_OK(t *testing.T) {
svc := newTestService(t)
err := svc.PutInventory(context.Background(), []DpuInput{
{ID: "dpu-0", Endpoint: "localhost:50051"},
})
if err != nil {
t.Fatalf("PutInventory: %v", err)
}
}

func TestPutInventory_EmptyID(t *testing.T) {
svc := newTestService(t)
err := svc.PutInventory(context.Background(), []DpuInput{
{Endpoint: "localhost:50051"},
})
if err == nil {
t.Fatal("expected error for empty id")
}
}

func TestPutInventory_EmptyEndpoint(t *testing.T) {
svc := newTestService(t)
err := svc.PutInventory(context.Background(), []DpuInput{
{ID: "dpu-0"},
})
if err == nil {
t.Fatal("expected error for empty endpoint")
}
}

func TestGetInventory_AfterPut(t *testing.T) {
svc := newTestService(t)
svc.PutInventory(context.Background(), []DpuInput{
{ID: "dpu-0", Endpoint: "localhost:50051"},
{ID: "dpu-1", Endpoint: "localhost:50052"},
})
statuses, err := svc.GetInventory(context.Background())
if err != nil {
t.Fatalf("GetInventory: %v", err)
}
if len(statuses) != 2 {
t.Errorf("expected 2 dpus, got %d", len(statuses))
}
}

func TestReconcile_NilReconciler(t *testing.T) {
svc := newTestService(t)
// rec is nil — should not panic.
err := svc.Reconcile(context.Background())
if err != nil {
t.Fatalf("Reconcile: %v", err)
}
}

func TestNamespace_Isolation(t *testing.T) {
svc := newTestService(t)
svc.PutVnet(context.Background(), "ns-a", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})
svc.PutVnet(context.Background(), "ns-b", &dashcenterv1.VnetSpec{Name: "v1", Vni: 200})

a, _ := svc.Get(context.Background(), "ns-a", "vnet", "v1")
b, _ := svc.Get(context.Background(), "ns-b", "vnet", "v1")
if a.Namespace != "ns-a" {
t.Errorf("expected ns-a, got %s", a.Namespace)
}
if b.Namespace != "ns-b" {
t.Errorf("expected ns-b, got %s", b.Namespace)
}

// Cross-namespace isolation.
_, err := svc.Get(context.Background(), "ns-c", "vnet", "v1")
if err == nil {
t.Fatal("expected not found for ns-c")
}
}

func TestResolveNS_Default(t *testing.T) {
ns := resolveNS("")
if ns != store.DefaultNamespace {
t.Errorf("expected %q, got %q", store.DefaultNamespace, ns)
}
ns2 := resolveNS("custom")
if ns2 != "custom" {
t.Errorf("expected custom, got %q", ns2)
}
}