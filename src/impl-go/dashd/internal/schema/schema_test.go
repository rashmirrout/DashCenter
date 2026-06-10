// Schema gate (PB-3) unit tests covering PB-G3 (ServiceTunnel on
// incapable DPU → FailedPrecondition) and PB-G4 (ServiceTunnel on
// capable DPU → success) plus the spec-level IPv6 gate.
package schema

import (
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// fakeInv is a stand-in for *inventory.Inventory. We construct DpuEntry
// values directly so tests can advertise arbitrary Capabilities.
type fakeInv struct {
	entries map[string]inventory.DpuEntry
}

func newFakeInv(entries ...inventory.DpuEntry) *fakeInv {
	m := map[string]inventory.DpuEntry{}
	for _, e := range entries {
		m[e.ID] = e
	}
	return &fakeInv{entries: m}
}

func (f *fakeInv) Get(id string) (inventory.DpuEntry, error) {
	e, ok := f.entries[id]
	if !ok {
		return inventory.DpuEntry{}, errors.New("not found")
	}
	return e, nil
}

func (f *fakeInv) List() []inventory.DpuEntry {
	out := make([]inventory.DpuEntry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out
}

func dpu(id string, caps *dashcenterv1.DpuCapabilities) inventory.DpuEntry {
	return inventory.DpuEntry{
		ID:           id,
		Endpoint:     id + ":50051",
		Capabilities: caps,
	}
}

// --- PB-G3: ServiceTunnel on incapable DPU ----------------------------

func TestCheckKind_ServiceTunnel_AllCapable_OK_PB_G4(t *testing.T) {
	g := NewGate(newFakeInv(
		dpu("dpu-1", &dashcenterv1.DpuCapabilities{ServiceTunnel: true}),
		dpu("dpu-2", &dashcenterv1.DpuCapabilities{ServiceTunnel: true}),
	))
	if err := g.CheckKind(nil, "service_tunnel"); err != nil {
		t.Errorf("CheckKind: %v; want nil (both DPUs capable)", err)
	}
}

func TestCheckKind_ServiceTunnel_OneIncapable_Rejected_PB_G3(t *testing.T) {
	g := NewGate(newFakeInv(
		dpu("dpu-1", &dashcenterv1.DpuCapabilities{ServiceTunnel: true}),
		dpu("dpu-2", &dashcenterv1.DpuCapabilities{ServiceTunnel: false}), // incapable
	))
	err := g.CheckKind(nil, "service_tunnel")
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "dpu-2") {
		t.Errorf("expected dpu-2 in error; got %q", err)
	}
	if !strings.Contains(err.Error(), "caps.service_tunnel=false") {
		t.Errorf("expected caps.service_tunnel=false in error; got %q", err)
	}
}

func TestCheckKind_ServiceTunnel_NilCaps_AllowedMC3(t *testing.T) {
	// MC-3: nil caps == "not yet advertised" → allow with log warn.
	g := NewGate(newFakeInv(dpu("dpu-1", nil)))
	if err := g.CheckKind(nil, "service_tunnel"); err != nil {
		t.Errorf("CheckKind with nil caps: %v; want nil (MC-3)", err)
	}
}

func TestCheckKind_HaSet_NoModes_Rejected(t *testing.T) {
	g := NewGate(newFakeInv(
		dpu("dpu-1", &dashcenterv1.DpuCapabilities{HaActiveActive: false, HaActiveStandby: false}),
	))
	err := g.CheckKind([]string{"dpu-1"}, "ha_set")
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
}

func TestCheckKind_HaSet_ActiveStandby_OK(t *testing.T) {
	g := NewGate(newFakeInv(
		dpu("dpu-1", &dashcenterv1.DpuCapabilities{HaActiveStandby: true}),
	))
	if err := g.CheckKind([]string{"dpu-1"}, "ha_set"); err != nil {
		t.Errorf("CheckKind: %v; want nil (active_standby capable)", err)
	}
}

func TestCheckKind_OtherKinds_AlwaysOK(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{})))
	for _, k := range []string{"vnet", "eni", "vnet_mapping", "acl_policy", "route_policy"} {
		if err := g.CheckKind(nil, k); err != nil {
			t.Errorf("CheckKind(%q): %v; want nil (no gate for this kind)", k, err)
		}
	}
}

// --- spec-level IPv6 gating ------------------------------------------

func TestCheckSpec_EniIPv4Underlay_OK(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{Ipv6: false})))
	spec := &dashcenterv1.EniSpec{Name: "e1", UnderlayIp: "10.0.5.11", PlacementHintDpuIds: []string{"dpu-1"}}
	if err := g.CheckSpec(spec.GetPlacementHintDpuIds(), "eni", spec); err != nil {
		t.Errorf("v4 underlay: %v; want nil", err)
	}
}

func TestCheckSpec_EniIPv6Underlay_IncapableDPU_Rejected(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{Ipv6: false})))
	spec := &dashcenterv1.EniSpec{Name: "e1", UnderlayIp: "fd00::5:11", PlacementHintDpuIds: []string{"dpu-1"}}
	err := g.CheckSpec(spec.GetPlacementHintDpuIds(), "eni", spec)
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "caps.ipv6=false") {
		t.Errorf("expected caps.ipv6=false in error; got %q", err)
	}
}

func TestCheckSpec_EniIPv6_CapableDPU_OK(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{Ipv6: true})))
	spec := &dashcenterv1.EniSpec{Name: "e1", UnderlayIp: "fd00::5:11", PlacementHintDpuIds: []string{"dpu-1"}}
	if err := g.CheckSpec(spec.GetPlacementHintDpuIds(), "eni", spec); err != nil {
		t.Errorf("v6 underlay on capable DPU: %v; want nil", err)
	}
}

func TestCheckSpec_VnetMappingIPv6_Rejected(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{Ipv6: false})))
	spec := &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "fd00::1", UnderlayIp: "10.0.0.1"}
	err := g.CheckSpec(nil, "vnet_mapping", spec)
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
}

func TestCheckSpec_RoutePolicyIPv6Prefix_Rejected(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{Ipv6: false})))
	spec := &dashcenterv1.RoutePolicySpec{
		Name: "rp", EniNames: []string{"e1"},
		Routes: []*dashcenterv1.RouteSpec{{Prefix: "fd00::/64", NextHopType: "drop"}},
	}
	err := g.CheckSpec(nil, "route_policy", spec)
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
}

func TestCheckSpec_RoutePolicyIPv4Only_OK(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{Ipv6: false})))
	spec := &dashcenterv1.RoutePolicySpec{
		Name: "rp", EniNames: []string{"e1"},
		Routes: []*dashcenterv1.RouteSpec{{Prefix: "10.0.0.0/24", NextHopType: "drop"}},
	}
	if err := g.CheckSpec(nil, "route_policy", spec); err != nil {
		t.Errorf("v4 prefix: %v; want nil", err)
	}
}

func TestCheckSpec_ServiceTunnelIPv6Underlay_Rejected(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{ServiceTunnel: true, Ipv6: false})))
	spec := &dashcenterv1.ServiceTunnelSpec{Name: "st-1", LocalUnderlayIp: "fd00::1", RemoteUnderlayIp: "10.0.0.2"}
	err := g.CheckSpec(nil, "service_tunnel", spec)
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
}

func TestCheckSpec_NilSpec_AllNoop(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{})))
	for _, k := range []string{"eni", "vnet_mapping", "route_policy", "service_tunnel"} {
		if err := g.CheckSpec(nil, k, nil); err != nil {
			t.Errorf("nil spec %q: %v; want nil", k, err)
		}
	}
}

func TestCheckSpec_NilGate_AllNoop(t *testing.T) {
	var g *Gate
	if err := g.CheckKind(nil, "service_tunnel"); err != nil {
		t.Errorf("nil gate CheckKind: %v; want nil", err)
	}
	if err := g.CheckSpec(nil, "eni", &dashcenterv1.EniSpec{UnderlayIp: "fd00::1"}); err != nil {
		t.Errorf("nil gate CheckSpec: %v; want nil", err)
	}
	if v := g.SchemaVersionFor("x"); v != "" {
		t.Errorf("nil gate SchemaVersionFor: %q; want \"\"", v)
	}
}

func TestSchemaVersionFor(t *testing.T) {
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{DashApiSchemaVersion: "1.4.2"})))
	if v := g.SchemaVersionFor("dpu-1"); v != "1.4.2" {
		t.Errorf("SchemaVersionFor: %q; want 1.4.2", v)
	}
	if v := g.SchemaVersionFor("dpu-unknown"); v != "" {
		t.Errorf("SchemaVersionFor unknown: %q; want \"\"", v)
	}
}

func TestCheckKind_UnknownDPUTarget_Skipped(t *testing.T) {
	// Unknown target IDs are silently dropped (capacity already
	// fail-closes on unknown placement; the gate doesn't duplicate).
	g := NewGate(newFakeInv(dpu("dpu-1", &dashcenterv1.DpuCapabilities{ServiceTunnel: true})))
	if err := g.CheckKind([]string{"dpu-typo"}, "service_tunnel"); err != nil {
		t.Errorf("unknown target: %v; want nil (skipped)", err)
	}
}

func TestIsIPv6Helpers(t *testing.T) {
	cases := []struct {
		in     string
		isAddr bool
		isPfx  bool
	}{
		{"", false, false},
		{"10.0.0.1", false, false},
		{"10.0.0.0/24", false, false},
		{"fd00::1", true, true},
		{"fd00::/64", true, true},
		{"::1", true, true},
	}
	for _, c := range cases {
		if got := isIPv6Address(c.in); got != c.isAddr {
			t.Errorf("isIPv6Address(%q) = %v; want %v", c.in, got, c.isAddr)
		}
		if got := isIPv6Prefix(c.in); got != c.isPfx {
			t.Errorf("isIPv6Prefix(%q) = %v; want %v", c.in, got, c.isPfx)
		}
	}
}
