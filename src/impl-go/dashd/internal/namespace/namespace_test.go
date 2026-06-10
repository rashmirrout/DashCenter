// Validator tests. Uses the file-backed store directly because it is
// already a faithful implementation of store.DesiredStore and avoids
// the test-mock-drift risk of a hand-rolled stub. Each test gets its
// own temp dir, so they are fully isolated.
package namespace

import (
	"context"
	"errors"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
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

	// next_hop_type=service_tunnel — target validation is PB's job,
	// not ours. Validator must accept.
	err := v.CheckRoutePolicy(context.Background(), "ns-a",
		&dashcenterv1.RoutePolicySpec{
			EniNames: []string{"eni-1"},
			Routes: []*dashcenterv1.RouteSpec{
				{NextHopType: "service_tunnel", NextHopTarget: "tunnel-x"},
				{NextHopType: "drop"},
			},
		})
	if err != nil {
		t.Errorf("non-vnet next hops: %v; want nil (out of scope here)", err)
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

// --- VnetMapping nil + Vnet nil branches -----------------------------

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
