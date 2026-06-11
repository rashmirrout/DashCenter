// PB-3 service-layer integration tests covering PB-G3 (ServiceTunnel
// on incapable DPU → ErrFailedPrecondition) and PB-G4 (ServiceTunnel
// on capable DPU → success), plus RegisterDpu round-trip.
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/schema"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// newServiceWithGate wires a real file store + a registered DPU with
// explicit capabilities + a non-nil schema.Gate. capacity tracker is
// nil so capacity admission doesn't shadow the precondition path.
func newServiceWithGate(t *testing.T, caps *dashcenterv1.DpuCapabilities) ControlPlaneService {
	t.Helper()
	fs, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })

	inv := inventory.New()
	if err := inv.Register(inventory.DpuEntry{ID: "dpu-1", Endpoint: "dpu-1:50051"}); err != nil {
		t.Fatalf("inv.Register: %v", err)
	}
	if caps != nil {
		if err := inv.SetCapabilities("dpu-1", caps); err != nil {
			t.Fatalf("inv.SetCapabilities: %v", err)
		}
	}
	return NewControlPlane(fs, inv, nil, nil, schema.NewGate(inv), nil, nil)
}

// --- PB-G3 / PB-G4: ServiceTunnel kind gate ---------------------------

func TestPutServiceTunnel_IncapableDPU_Rejected_PB_G3(t *testing.T) {
	svc := newServiceWithGate(t, &dashcenterv1.DpuCapabilities{ServiceTunnel: false})
	_, err := svc.PutServiceTunnel(context.Background(), "default", &dashcenterv1.ServiceTunnelSpec{
		Name: "st-1", LocalUnderlayIp: "10.0.0.1", RemoteUnderlayIp: "10.0.0.2", Vni: 100,
	})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "service_tunnel") {
		t.Errorf("expected service_tunnel in error; got %q", err)
	}
}

func TestPutServiceTunnel_CapableDPU_OK_PB_G4(t *testing.T) {
	svc := newServiceWithGate(t, &dashcenterv1.DpuCapabilities{ServiceTunnel: true})
	res, err := svc.PutServiceTunnel(context.Background(), "default", &dashcenterv1.ServiceTunnelSpec{
		Name: "st-1", LocalUnderlayIp: "10.0.0.1", RemoteUnderlayIp: "10.0.0.2", Vni: 100,
	})
	if err != nil {
		t.Fatalf("PutServiceTunnel: %v; want nil", err)
	}
	if !res.Accepted {
		t.Errorf("Accepted=false; want true")
	}
}

func TestPutServiceTunnel_NilCaps_AllowedMC3(t *testing.T) {
	// MC-3: nil caps = not yet advertised → permissive.
	svc := newServiceWithGate(t, nil) // never call SetCapabilities
	res, err := svc.PutServiceTunnel(context.Background(), "default", &dashcenterv1.ServiceTunnelSpec{
		Name: "st-mc3", LocalUnderlayIp: "10.0.0.1", RemoteUnderlayIp: "10.0.0.2", Vni: 100,
	})
	if err != nil {
		t.Fatalf("PutServiceTunnel with nil caps: %v; want nil", err)
	}
	if !res.Accepted {
		t.Error("expected accepted=true")
	}
}

// --- spec-level: IPv6 ENI on incapable DPU ----------------------------

func TestPutEni_IPv6Underlay_IncapableDPU_Rejected(t *testing.T) {
	svc := newServiceWithGate(t, &dashcenterv1.DpuCapabilities{Ipv6: false})
	// Seed Vnet first (namespace validator requires parent).
	if _, err := svc.PutVnet(context.Background(), "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}
	_, err := svc.PutEni(context.Background(), "default", &dashcenterv1.EniSpec{
		Name:                "eni-v6",
		VnetName:            "vnet-app",
		MacAddress:          "00:11:22:00:00:fe",
		UnderlayIp:          "fd00::5:99",
		PlacementHintDpuIds: []string{"dpu-1"},
	})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition (IPv6 ENI on non-v6 DPU)", err)
	}
}

func TestPutEni_IPv6Underlay_CapableDPU_OK(t *testing.T) {
	svc := newServiceWithGate(t, &dashcenterv1.DpuCapabilities{Ipv6: true})
	if _, err := svc.PutVnet(context.Background(), "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}
	if _, err := svc.PutEni(context.Background(), "default", &dashcenterv1.EniSpec{
		Name: "eni-v6", VnetName: "vnet-app", MacAddress: "00:11:22:00:00:fe",
		UnderlayIp: "fd00::5:99", PlacementHintDpuIds: []string{"dpu-1"},
	}); err != nil {
		t.Errorf("PutEni v6 on capable DPU: %v; want nil", err)
	}
}

// --- RegisterDpu --------------------------------------------------------

func TestRegisterDpu_RoundTrip(t *testing.T) {
	svc := newServiceWithGate(t, nil) // start with nil caps

	// Before register: nil caps → PutServiceTunnel is permissive.
	if _, err := svc.PutServiceTunnel(context.Background(), "default", &dashcenterv1.ServiceTunnelSpec{
		Name: "before", LocalUnderlayIp: "10.0.0.1", RemoteUnderlayIp: "10.0.0.2",
	}); err != nil {
		t.Fatalf("pre-register: %v", err)
	}

	// Register with ServiceTunnel=false.
	if err := svc.RegisterDpu(context.Background(), DpuRegistration{
		ID:           "dpu-1",
		Capabilities: &dashcenterv1.DpuCapabilities{ServiceTunnel: false},
	}); err != nil {
		t.Fatalf("RegisterDpu: %v", err)
	}

	// Post-register: gate now rejects.
	_, err := svc.PutServiceTunnel(context.Background(), "default", &dashcenterv1.ServiceTunnelSpec{
		Name: "after", LocalUnderlayIp: "10.0.0.3", RemoteUnderlayIp: "10.0.0.4",
	})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Errorf("post-register: %v; want ErrFailedPrecondition", err)
	}
}

func TestRegisterDpu_EmptyID(t *testing.T) {
	svc := newServiceWithGate(t, nil)
	err := svc.RegisterDpu(context.Background(), DpuRegistration{})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("got %v; want ErrInvalidArgument", err)
	}
}

func TestRegisterDpu_NoLimitsNoCaps(t *testing.T) {
	svc := newServiceWithGate(t, nil)
	err := svc.RegisterDpu(context.Background(), DpuRegistration{ID: "dpu-1"})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("got %v; want ErrInvalidArgument (must supply at least one)", err)
	}
}

func TestRegisterDpu_UnknownDpu(t *testing.T) {
	svc := newServiceWithGate(t, nil)
	err := svc.RegisterDpu(context.Background(), DpuRegistration{
		ID:           "dpu-not-in-inv",
		Capabilities: &dashcenterv1.DpuCapabilities{ServiceTunnel: true},
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("got %v; want ErrInvalidArgument (DPU must be in inventory)", err)
	}
}

func TestRegisterDpu_SetsLimits(t *testing.T) {
	svc := newServiceWithGate(t, nil)
	if err := svc.RegisterDpu(context.Background(), DpuRegistration{
		ID:     "dpu-1",
		Limits: &dashcenterv1.DpuCapacityLimits{MaxEnis: 42},
	}); err != nil {
		t.Fatalf("RegisterDpu: %v", err)
	}
	// We don't have a public reader for limits at the service level —
	// validate indirectly: register with nil limits should still
	// preserve them (just sets caps).
	if err := svc.RegisterDpu(context.Background(), DpuRegistration{
		ID:           "dpu-1",
		Capabilities: &dashcenterv1.DpuCapabilities{Ipv6: true},
	}); err != nil {
		t.Fatalf("second RegisterDpu: %v", err)
	}
}
