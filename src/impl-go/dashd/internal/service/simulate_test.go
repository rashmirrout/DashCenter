// SimulateApply (PB-2) tests for the service layer.
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// newServiceWithTracker wires a real file store + a registered DPU with
// explicit limits + a non-nil capacity tracker, so SimulateApply has
// something meaningful to evaluate.
func newServiceWithTracker(t *testing.T, limits *dashcenterv1.DpuCapacityLimits) (ControlPlaneService, *capacity.Tracker) {
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
	if err := inv.SetLimits("dpu-1", limits); err != nil {
		t.Fatalf("inv.SetLimits: %v", err)
	}
	tr := capacity.NewTracker(inv)
	svc := NewControlPlane(fs, inv, nil, tr, nil, nil)
	return svc, tr
}

func TestSimulateApply_EmptyOps(t *testing.T) {
	svc, _ := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	_, err := svc.SimulateApply(context.Background(), nil)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("got %v; want ErrInvalidArgument for empty ops", err)
	}
}

func TestSimulateApply_NilTracker_DegradesGracefully(t *testing.T) {
	// Service constructed with nil tracker (legacy test config) must
	// not 500 — it should return a successful no-op result.
	svc := newTestService(t)
	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{
		Action: "put", Kind: "eni",
		EniSpec: &dashcenterv1.EniSpec{Name: "e", PlacementHintDpuIds: []string{"dpu-1"}},
	}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if !res.WouldSucceed {
		t.Errorf("WouldSucceed=false; want true (no admission gates when tracker is nil)")
	}
}

func TestSimulateApply_PutEni_WithinCapacity(t *testing.T) {
	svc, _ := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{
		Action: "put", Namespace: "default", Kind: "eni",
		EniSpec: &dashcenterv1.EniSpec{Name: "e1", PlacementHintDpuIds: []string{"dpu-1"}},
	}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if !res.WouldSucceed {
		t.Errorf("WouldSucceed=false; errors=%v", res.ValidationErrors)
	}
	if len(res.PerDpuImpact) != 1 || res.PerDpuImpact[0].DeltaEnis != 1 {
		t.Errorf("PerDpuImpact=%v; want [{dpu-1 +1}]", res.PerDpuImpact)
	}
}

func TestSimulateApply_PutEni_Exceeds_PB_G2(t *testing.T) {
	svc, tr := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 1})
	// Seed: one ENI already on dpu-1.
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "old", PlacementHintDpuIds: []string{"dpu-1"}})

	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{
		Action: "put", Namespace: "default", Kind: "eni",
		EniSpec: &dashcenterv1.EniSpec{Name: "new", PlacementHintDpuIds: []string{"dpu-1"}},
	}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (2nd ENI exceeds MaxEnis=1)")
	}
	if len(res.ValidationErrors) == 0 {
		t.Fatal("expected validation errors")
	}
	if !strings.Contains(res.ValidationErrors[0], "max_enis") {
		t.Errorf("expected max_enis in error; got %q", res.ValidationErrors[0])
	}
}

func TestSimulateApply_UnknownAction(t *testing.T) {
	svc, _ := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{Action: "patch", Kind: "eni"}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (unknown action)")
	}
}

func TestSimulateApply_UnsupportedKind(t *testing.T) {
	svc, _ := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{Action: "put", Kind: "vnet"}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (PB-2 doesn't support vnet)")
	}
}

func TestSimulateApply_PutEni_MissingSpec(t *testing.T) {
	svc, _ := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{
		Action: "put", Kind: "eni", // EniSpec nil
	}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (missing spec)")
	}
}

func TestSimulateApply_DeleteMissingName(t *testing.T) {
	svc, _ := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	res, err := svc.SimulateApply(context.Background(), []SimulateOp{{Action: "delete", Kind: "eni"}})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (missing name)")
	}
}

func TestSimulateApply_DeleteEni_OK(t *testing.T) {
	svc, tr := newServiceWithTracker(t, &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "e1", PlacementHintDpuIds: []string{"dpu-1"}})

	res, err := svc.SimulateApply(context.Background(), []SimulateOp{
		{Action: "delete", Namespace: "default", Kind: "eni", Name: "e1"},
	})
	if err != nil {
		t.Fatalf("SimulateApply: %v", err)
	}
	if !res.WouldSucceed {
		t.Errorf("WouldSucceed=false; errors=%v", res.ValidationErrors)
	}
	// Should show -1 on dpu-1.
	if len(res.PerDpuImpact) != 1 || res.PerDpuImpact[0].DeltaEnis != -1 {
		t.Errorf("PerDpuImpact=%v; want [{dpu-1 -1}]", res.PerDpuImpact)
	}
}
