// Validator tests. Uses the file-backed store directly because it is
// already a faithful implementation of store.DesiredStore and avoids
// the test-mock-drift risk of a hand-rolled stub. Each test gets its
// own temp dir, so they are fully isolated.
package namespace

import (
	"context"
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// --- harness ---------------------------------------------------------

func newValidator(t *testing.T) (*Validator, *filstore.FileStore) {
	t.Helper()
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return NewValidator(st), st
}

// seed plants an object in the store so cross-reference checks can
// resolve it.
func seed(t *testing.T, st *filstore.FileStore, ns, kind, name string, spec any) {
	t.Helper()
	if _, err := st.Put(context.Background(),
		store.ObjectKey{Namespace: ns, Kind: kind, Name: name},
		spec,
		0,
	); err != nil {
		t.Fatalf("seed %s/%s/%s: %v", ns, kind, name, err)
	}
}

// --- spec-namespace consistency -------------------------------------

func TestCheckSpecNamespace_EmptyAllowed(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckSpecNamespace("default", ""); err != nil {
		t.Errorf("empty spec.namespace under op=default: %v; want nil", err)
	}
}

func TestCheckSpecNamespace_MatchingAllowed(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckSpecNamespace("ns-a", "ns-a"); err != nil {
		t.Errorf("matching ns: %v; want nil", err)
	}
}

func TestCheckSpecNamespace_MismatchRejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckSpecNamespace("ns-a", "ns-b")
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

// --- Vnet (no cross-references) -------------------------------------

func TestCheckVnet_NilOK(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckVnet(context.Background(), "default", nil); err != nil {
		t.Errorf("nil spec: %v; want nil", err)
	}
}

func TestCheckVnet_MatchingNS(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckVnet(context.Background(), "ns-a",
		&dashcenterv1.VnetSpec{Namespace: "ns-a", Name: "v1"})
	if err != nil {
		t.Errorf("matching ns: %v; want nil", err)
	}
}

func TestCheckVnet_MismatchedNS(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckVnet(context.Background(), "ns-a",
		&dashcenterv1.VnetSpec{Namespace: "ns-b", Name: "v1"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

// --- Eni → Vnet ------------------------------------------------------

func TestCheckEni_VnetInSameNS_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-1", &dashcenterv1.VnetSpec{Name: "vnet-1", Vni: 1})

	err := v.CheckEni(context.Background(), "ns-a",
		&dashcenterv1.EniSpec{Name: "eni-1", VnetName: "vnet-1"})
	if err != nil {
		t.Errorf("vnet in same ns: %v; want nil", err)
	}
}

func TestCheckEni_VnetInDifferentNS_Rejected(t *testing.T) {
	v, st := newValidator(t)
	// vnet-1 lives in ns-b — should be invisible from ns-a's perspective.
	seed(t, st, "ns-b", "vnet", "vnet-1", &dashcenterv1.VnetSpec{Name: "vnet-1", Vni: 1})

	err := v.CheckEni(context.Background(), "ns-a",
		&dashcenterv1.EniSpec{Name: "eni-1", VnetName: "vnet-1"})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

func TestCheckEni_VnetMissing_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckEni(context.Background(), "ns-a",
		&dashcenterv1.EniSpec{Name: "eni-1", VnetName: "nonexistent"})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

func TestCheckEni_EmptyVnetName_Allowed(t *testing.T) {
	v, _ := newValidator(t)
	// Phase-2 admission-control concern, not a namespace concern.
	err := v.CheckEni(context.Background(), "ns-a",
		&dashcenterv1.EniSpec{Name: "eni-1"})
	if err != nil {
		t.Errorf("empty vnet_name: %v; want nil (out of scope here)", err)
	}
}

func TestCheckEni_SpecNamespaceMismatch_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckEni(context.Background(), "ns-a",
		&dashcenterv1.EniSpec{Namespace: "ns-b", Name: "eni-1"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

// --- VnetMapping → Vnet ---------------------------------------------

func TestCheckVnetMapping_VnetInSameNS_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "v-x", &dashcenterv1.VnetSpec{Name: "v-x", Vni: 99})

	err := v.CheckVnetMapping(context.Background(), "ns-a",
		&dashcenterv1.VnetMappingSpec{VnetName: "v-x", IpAddress: "10.0.0.1"})
	if err != nil {
		t.Errorf("vnet in same ns: %v; want nil", err)
	}
}

func TestCheckVnetMapping_VnetInDifferentNS_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-b", "vnet", "v-x", &dashcenterv1.VnetSpec{Name: "v-x", Vni: 99})

	err := v.CheckVnetMapping(context.Background(), "ns-a",
		&dashcenterv1.VnetMappingSpec{VnetName: "v-x", IpAddress: "10.0.0.1"})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

// --- AclPolicy → []Eni ----------------------------------------------

func TestCheckAclPolicy_AllEnisInSameNS_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "eni", "eni-2", &dashcenterv1.EniSpec{Name: "eni-2"})

	err := v.CheckAclPolicy(context.Background(), "ns-a",
		&dashcenterv1.AclPolicySpec{
			Name:     "policy-1",
			Stage:    "inbound",
			EniNames: []string{"eni-1", "eni-2"},
		})
	if err != nil {
		t.Errorf("acl with same-ns enis: %v; want nil", err)
	}
}

func TestCheckAclPolicy_OneEniMissing_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	// eni-2 absent.

	err := v.CheckAclPolicy(context.Background(), "ns-a",
		&dashcenterv1.AclPolicySpec{
			Name:     "policy-1",
			EniNames: []string{"eni-1", "eni-2"},
		})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

func TestCheckAclPolicy_EmptyEnis_Allowed(t *testing.T) {
	v, _ := newValidator(t)
	// Operator may stage an ACL before attaching it.
	err := v.CheckAclPolicy(context.Background(), "ns-a",
		&dashcenterv1.AclPolicySpec{Name: "policy-1", EniNames: nil})
	if err != nil {
		t.Errorf("empty eni_names: %v; want nil", err)
	}
}

func TestCheckAclPolicy_EmptyStringEni_Skipped(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})

	// Empty entries are skipped; the present one still resolves.
	err := v.CheckAclPolicy(context.Background(), "ns-a",
		&dashcenterv1.AclPolicySpec{Name: "policy-1", EniNames: []string{"", "eni-1", ""}})
	if err != nil {
		t.Errorf("with empty-string entries: %v; want nil", err)
	}
}

// --- RoutePolicy → []Eni + routes[i].next_hop_target=vnet -----------

func TestCheckRoutePolicy_AllValid_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "vnet", "vnet-target", &dashcenterv1.VnetSpec{Name: "vnet-target", Vni: 100})

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			Name:     "rp-1",
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{Prefix: "10.0.0.0/16", NextHopType: "vnet", NextHopTarget: "vnet-target"},
				{Prefix: "0.0.0.0/0", NextHopType: "drop", NextHopTarget: ""},
			},
		})
	if err != nil {
		t.Errorf("valid route policy: %v; want nil", err)
	}
}

func TestCheckRoutePolicy_VnetTargetInDifferentNS_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	// vnet-target lives in ns-b.
	seed(t, st, "ns-b", "vnet", "vnet-target", &dashcenterv1.VnetSpec{Name: "vnet-target", Vni: 100})

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			Name:     "rp-1",
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{Prefix: "10.0.0.0/16", NextHopType: "vnet", NextHopTarget: "vnet-target"},
			},
		})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

func TestCheckRoutePolicy_NonVnetNextHop_NotChecked(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "service_tunnel", "tunnel-x", &dashcenterv1.ServiceTunnelSpec{Name: "tunnel-x"})

	// service_tunnel target is now validated (exists in same ns);
	// drop has no target and is always accepted.
	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: "tunnel-x"},
				{NextHopType: "drop"},
			},
		})
	if err != nil {
		t.Errorf("valid next hops: %v; want nil", err)
	}
}

func TestCheckRoutePolicy_NilRoutesAndEnis_Allowed(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckRoutePolicy(context.Background(), "ns-a", &dashcenterv1.RoutePolicySpec{})
	if err != nil {
		t.Errorf("empty route policy: %v; want nil", err)
	}
}

func TestCheckRoutePolicy_NilRouteEntry_Skipped(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			Routes: []*dashcenterv1.RouteSpec{nil},
		})
	if err != nil {
		t.Errorf("nil route entry: %v; want nil (skipped)", err)
	}
}

// --- HaSet / ServiceTunnel: only spec-namespace check ---------------

func TestCheckHaSet_NSMismatch_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Namespace: "ns-b", Name: "ha-1"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

func TestCheckHaSet_NilOK(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckHaSet(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

func TestCheckServiceTunnel_NSMismatch_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckServiceTunnel(context.Background(), "ns-a",
		&dashcenterv1.ServiceTunnelSpec{Namespace: "ns-b", Name: "tun-1"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

func TestCheckServiceTunnel_NilOK(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckServiceTunnel(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

// --- CheckEni nil branch -------------------------------------------

func TestCheckEni_NilOK(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckEni(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

// --- RoutePolicy → service_tunnel -----------------------------------

func TestCheckRoutePolicy_NilOK(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckRoutePolicy(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v; want nil", err)
	}
}

func TestCheckRoutePolicy_ServiceTunnelEmptyTarget_Skipped(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})

	// service_tunnel with empty target — should be skipped (not checked)
	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: ""},
			},
		})
	if err != nil {
		t.Errorf("empty service_tunnel target should be skipped: %v", err)
	}
}

func TestCheckRoutePolicy_EmptyStringEni_Skipped(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})

	// Empty-string ENI entries are skipped; present ones resolve.
	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{EniNames: []string{"", "eni-1", ""}})
	if err != nil {
		t.Errorf("with empty-string eni entries: %v; want nil", err)
	}
}

func TestCheckRoutePolicy_EniMissing_Rejected(t *testing.T) {
	v, _ := newValidator(t)

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{EniNames: []string{"no-such-eni"}})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

func TestCheckRoutePolicy_ServiceTunnelTarget_Missing_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: "tun-missing"},
			},
		})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

func TestCheckRoutePolicy_ServiceTunnelTarget_Exists_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "service_tunnel", "tun-1", &dashcenterv1.ServiceTunnelSpec{Name: "tun-1"})

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: "tun-1"},
			},
		})
	if err != nil {
		t.Errorf("valid service_tunnel target: %v; want nil", err)
	}
}

// --- HaSet → inventory DPU IDs -------------------------------------

func TestCheckHaSet_DpuNotInInventory_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-a", Endpoint: "localhost:50051"})
	v.WithInventory(inv)

	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"dpu-a", "dpu-missing"}})
	if !errors.Is(err, ErrDanglingReference) {
		t.Fatalf("got %v; want ErrDanglingReference", err)
	}
}

func TestCheckHaSet_AllDpusInInventory_OK(t *testing.T) {
	v, _ := newValidator(t)
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-a", Endpoint: "localhost:50051"})
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-b", Endpoint: "localhost:50052"})
	v.WithInventory(inv)

	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"dpu-a", "dpu-b"}})
	if err != nil {
		t.Errorf("all DPUs in inventory: %v; want nil", err)
	}
}

func TestCheckHaSet_EmptyDpuId_Skipped(t *testing.T) {
	v, _ := newValidator(t)
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-a", Endpoint: "localhost:50051"})
	v.WithInventory(inv)

	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"", "dpu-a", ""}})
	if err != nil {
		t.Errorf("empty DPU IDs should be skipped: %v; want nil", err)
	}
}

func TestCheckHaSet_NilInventory_Skips(t *testing.T) {
	v, _ := newValidator(t)
	// inv is nil by default — DPU checks are skipped
	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"any-dpu"}})
	if err != nil {
		t.Errorf("nil inventory should skip DPU checks: %v; want nil", err)
	}
}

// --- CheckDelete orphan protection ----------------------------------

func TestCheckDelete_Vnet_WithEniDependent_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-prod", &dashcenterv1.VnetSpec{Name: "vnet-prod", Vni: 100})
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1", VnetName: "vnet-prod"})

	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-prod", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_Vnet_WithVnetMappingDependent_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-prod", &dashcenterv1.VnetSpec{Name: "vnet-prod", Vni: 100})
	seed(t, st, "ns-a", "vnet_mapping", "vnet-prod-10.0.0.1",
		&dashcenterv1.VnetMappingSpec{VnetName: "vnet-prod", IpAddress: "10.0.0.1"})

	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-prod", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_Vnet_NoDependents_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-orphan", &dashcenterv1.VnetSpec{Name: "vnet-orphan", Vni: 100})

	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-orphan", false)
	if err != nil {
		t.Errorf("no dependents: %v; want nil", err)
	}
}

func TestCheckDelete_Eni_WithAclPolicyDependent_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "acl_policy", "acl-web",
		&dashcenterv1.AclPolicySpec{Name: "acl-web", EniNames: []string{"eni-1"}})

	err := v.CheckDelete(context.Background(), "ns-a", "eni", "eni-1", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_Eni_WithRoutePolicyDependent_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "route_policy", "rp-1",
		&dashcenterv1.RoutePolicySpec{Name: "rp-1", EniNames: []string{"eni-1"}})

	err := v.CheckDelete(context.Background(), "ns-a", "eni", "eni-1", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_Eni_NoDependents_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-orphan", &dashcenterv1.EniSpec{Name: "eni-orphan"})

	err := v.CheckDelete(context.Background(), "ns-a", "eni", "eni-orphan", false)
	if err != nil {
		t.Errorf("no dependents: %v; want nil", err)
	}
}

func TestCheckDelete_ServiceTunnel_WithRoutePolicyDependent_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "service_tunnel", "tun-1", &dashcenterv1.ServiceTunnelSpec{Name: "tun-1"})
	seed(t, st, "ns-a", "route_policy", "rp-1",
		&dashcenterv1.RoutePolicySpec{Name: "rp-1", Routes: []*dashcenterv1.RouteSpec{
			{NextHopType: "service_tunnel", NextHopTarget: "tun-1"},
		}})

	err := v.CheckDelete(context.Background(), "ns-a", "service_tunnel", "tun-1", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_ServiceTunnel_NoDependents_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "service_tunnel", "tun-orphan", &dashcenterv1.ServiceTunnelSpec{Name: "tun-orphan"})

	err := v.CheckDelete(context.Background(), "ns-a", "service_tunnel", "tun-orphan", false)
	if err != nil {
		t.Errorf("no dependents: %v; want nil", err)
	}
}

func TestCheckDelete_Force_BypassesCheck(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-prod", &dashcenterv1.VnetSpec{Name: "vnet-prod", Vni: 100})
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1", VnetName: "vnet-prod"})

	// force=true bypasses the check
	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-prod", true)
	if err != nil {
		t.Errorf("force=true should bypass: %v; want nil", err)
	}
}

func TestCheckDelete_UnprotectedKind_OK(t *testing.T) {
	v, _ := newValidator(t)
	// acl_policy is not a protected kind for delete orphan checks
	err := v.CheckDelete(context.Background(), "ns-a", "acl_policy", "any", false)
	if err != nil {
		t.Errorf("unprotected kind: %v; want nil", err)
	}
}

// --- Error message quality ------------------------------------------

func TestCheckDelete_ErrorContainsDependentName(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-test", &dashcenterv1.VnetSpec{Name: "vnet-test", Vni: 1})
	seed(t, st, "ns-a", "eni", "eni-specific-name", &dashcenterv1.EniSpec{Name: "eni-specific-name", VnetName: "vnet-test"})

	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-test", false)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "eni-specific-name") {
		t.Errorf("error should name the dependent: %s", msg)
	}
	if !strings.Contains(msg, "vnet-test") {
		t.Errorf("error should name the target: %s", msg)
	}
	if !strings.Contains(msg, "cannot delete") {
		t.Errorf("error should say 'cannot delete': %s", msg)
	}
}

func TestCheckHaSet_ErrorContainsDpuId(t *testing.T) {
	v, _ := newValidator(t)
	inv := inventory.New()
	v.WithInventory(inv)

	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"dpu-ghost-99"}})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "dpu-ghost-99") {
		t.Errorf("error should name the missing DPU: %s", msg)
	}
	if !strings.Contains(msg, "not found in inventory") {
		t.Errorf("error should say 'not found in inventory': %s", msg)
	}
}

// --- RoutePolicy → service_tunnel (new FK) --------------------------

func TestCheckRoutePolicy_ServiceTunnelTarget_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "service_tunnel", "tun-prod", &dashcenterv1.ServiceTunnelSpec{Name: "tun-prod"})

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: "tun-prod"},
			},
		})
	if err != nil {
		t.Errorf("valid service_tunnel: %v; want nil", err)
	}
}

func TestCheckRoutePolicy_ServiceTunnelMissing_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	// service_tunnel "tun-ghost" does not exist

	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: "tun-ghost"},
			},
		})
	if !errors.Is(err, ErrCrossNamespace) {
		t.Fatalf("got %v; want ErrCrossNamespace", err)
	}
}

// --- HaSet → DPU IDs ------------------------------------------------

func TestCheckHaSet_ValidDpuIds_OK(t *testing.T) {
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-01", Endpoint: "localhost:50051"})
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-02", Endpoint: "localhost:50052"})
	v, _ := newValidator(t)
	v.WithInventory(inv)

	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"dpu-01", "dpu-02"}})
	if err != nil {
		t.Errorf("valid DPU IDs: %v; want nil", err)
	}
}

func TestCheckHaSet_MissingDpuId_Rejected(t *testing.T) {
	inv := inventory.New()
	_ = inv.Register(inventory.DpuEntry{ID: "dpu-01", Endpoint: "localhost:50051"})
	v, _ := newValidator(t)
	v.WithInventory(inv)

	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"dpu-01", "dpu-ghost"}})
	if !errors.Is(err, ErrDanglingReference) {
		t.Fatalf("got %v; want ErrDanglingReference", err)
	}
}

func TestCheckHaSet_NilInventory_Skipped(t *testing.T) {
	v, _ := newValidator(t)
	// No inventory — DPU ID checks are skipped
	err := v.CheckHaSet(context.Background(), "ns-a",
		&dashcenterv1.HaSetSpec{Name: "ha-1", MemberDpuIds: []string{"any", "thing"}})
	if err != nil {
		t.Errorf("nil inventory should skip DPU checks: %v", err)
	}
}

// --- Delete orphan protection ----------------------------------------

func TestCheckDelete_VnetWithDependentEni_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-prod", &dashcenterv1.VnetSpec{Name: "vnet-prod", Vni: 1001})
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1", VnetName: "vnet-prod"})

	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-prod", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_VnetWithDependentMapping_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-prod", &dashcenterv1.VnetSpec{Name: "vnet-prod", Vni: 1001})
	seed(t, st, "ns-a", "vnet_mapping", "vnet-prod-10.0.0.1",
		&dashcenterv1.VnetMappingSpec{VnetName: "vnet-prod", IpAddress: "10.0.0.1"})

	err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-prod", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_VnetNoDependents_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-orphan", &dashcenterv1.VnetSpec{Name: "vnet-orphan", Vni: 42})

	if err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-orphan", false); err != nil {
		t.Errorf("no dependents: %v; want nil", err)
	}
}

func TestCheckDelete_VnetForce_Bypasses(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "vnet", "vnet-prod", &dashcenterv1.VnetSpec{Name: "vnet-prod", Vni: 1001})
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1", VnetName: "vnet-prod"})

	if err := v.CheckDelete(context.Background(), "ns-a", "vnet", "vnet-prod", true); err != nil {
		t.Errorf("force=true should bypass: %v; want nil", err)
	}
}

func TestCheckDelete_EniWithDependentAclPolicy_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "acl_policy", "acl-1",
		&dashcenterv1.AclPolicySpec{Name: "acl-1", EniNames: []string{"eni-1"}})

	err := v.CheckDelete(context.Background(), "ns-a", "eni", "eni-1", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_EniWithDependentRoutePolicy_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-1", &dashcenterv1.EniSpec{Name: "eni-1"})
	seed(t, st, "ns-a", "route_policy", "rp-1",
		&dashcenterv1.RoutePolicySpec{Name: "rp-1", EniNames: []string{"eni-1"}})

	err := v.CheckDelete(context.Background(), "ns-a", "eni", "eni-1", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_EniNoDependents_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "eni", "eni-orphan", &dashcenterv1.EniSpec{Name: "eni-orphan"})

	if err := v.CheckDelete(context.Background(), "ns-a", "eni", "eni-orphan", false); err != nil {
		t.Errorf("no dependents: %v; want nil", err)
	}
}

func TestCheckDelete_ServiceTunnelWithDependentRoute_Rejected(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "service_tunnel", "tun-1", &dashcenterv1.ServiceTunnelSpec{Name: "tun-1"})
	seed(t, st, "ns-a", "route_policy", "rp-1",
		&dashcenterv1.RoutePolicySpec{Name: "rp-1", Routes: []*dashcenterv1.RouteSpec{
			{NextHopType: "service_tunnel", NextHopTarget: "tun-1"},
		}})

	err := v.CheckDelete(context.Background(), "ns-a", "service_tunnel", "tun-1", false)
	if !errors.Is(err, ErrHasDependents) {
		t.Fatalf("got %v; want ErrHasDependents", err)
	}
}

func TestCheckDelete_ServiceTunnelNoDependents_OK(t *testing.T) {
	v, st := newValidator(t)
	seed(t, st, "ns-a", "service_tunnel", "tun-orphan", &dashcenterv1.ServiceTunnelSpec{Name: "tun-orphan"})

	if err := v.CheckDelete(context.Background(), "ns-a", "service_tunnel", "tun-orphan", false); err != nil {
		t.Errorf("no dependents: %v; want nil", err)
	}
}

func TestCheckDelete_UnknownKind_NoCheck(t *testing.T) {
	v, _ := newValidator(t)
	// Unknown kinds have no orphan check — just pass
	if err := v.CheckDelete(context.Background(), "ns-a", "route_policy", "rp-1", false); err != nil {
		t.Errorf("unknown kind: %v; want nil", err)
	}
}

func TestCheckVnet_NilSpec(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckVnet(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

func TestCheckVnetMapping_NilSpec(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckVnetMapping(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

func TestCheckAclPolicy_NilSpec(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckAclPolicy(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

func TestCheckRoutePolicy_NilSpec(t *testing.T) {
	v, _ := newValidator(t)
	if err := v.CheckRoutePolicy(context.Background(), "ns-a", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

// --- VnetMapping spec-namespace mismatch -----------------------------

func TestCheckVnetMapping_NSMismatch_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckVnetMapping(context.Background(), "ns-a",
		&dashcenterv1.VnetMappingSpec{Namespace: "ns-b", VnetName: "v-x"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

func TestCheckAclPolicy_NSMismatch_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckAclPolicy(context.Background(), "ns-a",
		&dashcenterv1.AclPolicySpec{Namespace: "ns-b", Name: "p"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}

func TestCheckRoutePolicy_NSMismatch_Rejected(t *testing.T) {
	v, _ := newValidator(t)
	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{Namespace: "ns-b", Name: "rp"})
	if !errors.Is(err, ErrSpecNamespaceMismatch) {
		t.Fatalf("got %v; want ErrSpecNamespaceMismatch", err)
	}
}
