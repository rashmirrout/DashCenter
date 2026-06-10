// PC-1 cordon integration tests at the service layer: PutEni with
// explicit placement_hint on a cordoned DPU must return
// ErrFailedPrecondition. The capacity tracker's fleet-wide fallback
// (no-hint ENI on a fleet where one DPU is cordoned) must skip the
// cordoned DPU.
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/operations"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// newServiceWithOps wires a real file store + N DPUs + capacity
// tracker + operations manager (no schema gate to keep the test focused
// on the cordon path).
func newServiceWithOps(t *testing.T, ids ...string) (ControlPlaneService, *operations.Manager, *capacity.Tracker) {
	t.Helper()
	fs, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })

	inv := inventory.New()
	for _, id := range ids {
		if err := inv.Register(inventory.DpuEntry{ID: id, Endpoint: id + ":50051"}); err != nil {
			t.Fatalf("inv.Register %s: %v", id, err)
		}
	}
	tr := capacity.NewTracker(inv)
	ops := operations.New(inv)
	return NewControlPlane(fs, inv, nil, tr, nil, ops), ops, tr
}

func TestPutEni_ExplicitHintAtCordonedDPU_Rejected(t *testing.T) {
	svc, ops, _ := newServiceWithOps(t, "dpu-1", "dpu-2")
	if err := ops.Cordon("dpu-1", "maintenance"); err != nil {
		t.Fatalf("cordon: %v", err)
	}
	// Parent vnet so namespace validator passes.
	if _, err := svc.PutVnet(context.Background(), "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}
	_, err := svc.PutEni(context.Background(), "default", &dashcenterv1.EniSpec{
		Name: "eni-1", VnetName: "vnet-app", MacAddress: "00:11:22:00:00:01",
		UnderlayIp: "10.0.5.11", AdminState: "up",
		PlacementHintDpuIds: []string{"dpu-1"}, // cordoned
	})
	if !errors.Is(err, ErrFailedPrecondition) {
		t.Fatalf("got %v; want ErrFailedPrecondition", err)
	}
	if !strings.Contains(err.Error(), "cordoned") {
		t.Errorf("expected 'cordoned' in error; got %q", err)
	}
}

func TestPutEni_HintAtUncordonedDPU_OK(t *testing.T) {
	svc, ops, _ := newServiceWithOps(t, "dpu-1", "dpu-2")
	ops.Cordon("dpu-1", "maintenance")
	if _, err := svc.PutVnet(context.Background(), "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}
	if _, err := svc.PutEni(context.Background(), "default", &dashcenterv1.EniSpec{
		Name: "eni-2", VnetName: "vnet-app", MacAddress: "00:11:22:00:00:02",
		UnderlayIp: "10.0.5.12", AdminState: "up",
		PlacementHintDpuIds: []string{"dpu-2"}, // uncordoned
	}); err != nil {
		t.Errorf("PutEni at uncordoned dpu-2: %v; want nil", err)
	}
}

func TestPutEni_NoHint_CordonedFleetSkipped(t *testing.T) {
	// 3 DPUs, dpu-1 cordoned. No-hint ENI must be counted against
	// {dpu-2, dpu-3} only — not dpu-1. Set MaxEnis=1 on every DPU
	// and place 1 ENI on dpu-2 first; the capacity tracker should
	// not double-count dpu-1 (cordoned) so subsequent placement on
	// dpu-3 stays admissible.
	svc, ops, tr := newServiceWithOps(t, "dpu-1", "dpu-2", "dpu-3")
	ops.Cordon("dpu-1", "")
	// Advertise a tiny MaxEnis on every DPU so the no-hint fan-out
	// would normally count the new ENI against dpu-1's budget too.
	for _, id := range []string{"dpu-1", "dpu-2", "dpu-3"} {
		if err := svc.RegisterDpu(context.Background(), DpuRegistration{
			ID: id, Limits: &dashcenterv1.DpuCapacityLimits{MaxEnis: 1},
		}); err != nil {
			t.Fatalf("RegisterDpu %s: %v", id, err)
		}
	}

	if _, err := svc.PutVnet(context.Background(), "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}
	// Apply a no-hint ENI. Without cordon, this would fan to all 3
	// DPUs and consume the budget on each; the second no-hint ENI
	// would then exceed. With cordon, dpu-1 is skipped → fan-out is
	// {dpu-2, dpu-3}. We don't actually assert "no exceed" here
	// because no-hint fan-out still counts ENIs against every
	// included DPU (so a second ENI would still exceed). What we DO
	// assert: dpu-1 has NO ENIs charged to it.
	if _, err := svc.PutEni(context.Background(), "default", &dashcenterv1.EniSpec{
		Name: "eni-nohint", VnetName: "vnet-app", MacAddress: "00:11:22:00:00:99",
		UnderlayIp: "10.0.5.99", AdminState: "up",
		// no PlacementHintDpuIds
	}); err != nil {
		t.Fatalf("no-hint PutEni: %v", err)
	}
	enis1, _, _ := tr.SnapshotForDPU("dpu-1")
	if enis1 != 0 {
		t.Errorf("dpu-1 (cordoned) enis=%d; want 0 (fan-out skipped)", enis1)
	}
	enis2, _, _ := tr.SnapshotForDPU("dpu-2")
	enis3, _, _ := tr.SnapshotForDPU("dpu-3")
	if enis2 != 1 || enis3 != 1 {
		t.Errorf("uncordoned DPUs got enis=%d,%d; want 1,1", enis2, enis3)
	}
}

func TestCordonDpu_NilOps_Rejected(t *testing.T) {
	svc := newTestService(t) // ops=nil
	if err := svc.CordonDpu(context.Background(), "dpu-1", ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("nil ops: %v; want ErrInvalidArgument", err)
	}
}

func TestCordonDpu_UnknownDPU(t *testing.T) {
	svc, _, _ := newServiceWithOps(t, "dpu-1")
	err := svc.CordonDpu(context.Background(), "dpu-typo", "")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("got %v; want ErrInvalidArgument (unknown DPU)", err)
	}
}

func TestCordon_Uncordon_RoundTrip(t *testing.T) {
	svc, _, _ := newServiceWithOps(t, "dpu-1")
	if err := svc.CordonDpu(context.Background(), "dpu-1", "drain"); err != nil {
		t.Fatalf("cordon: %v", err)
	}
	if got := svc.ListCordonedDpus(context.Background()); len(got) != 1 || got[0] != "dpu-1" {
		t.Errorf("ListCordoned=%v; want [dpu-1]", got)
	}
	if err := svc.UncordonDpu(context.Background(), "dpu-1", "done"); err != nil {
		t.Fatalf("uncordon: %v", err)
	}
	if got := svc.ListCordonedDpus(context.Background()); len(got) != 0 {
		t.Errorf("ListCordoned after uncordon=%v; want []", got)
	}
}
