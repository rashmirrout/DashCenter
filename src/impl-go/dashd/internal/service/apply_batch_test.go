// PC-8 ApplyBatch service-layer integration tests.
package service

import (
	"context"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func TestApplyBatch_AllCommit(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	res, err := svc.ApplyBatch(ctx, []BatchOp{
		{Action: "put", Kind: "vnet", VnetSpec: &dashcenterv1.VnetSpec{Name: "v1", Vni: 100}},
		{Action: "put", Kind: "vnet", VnetSpec: &dashcenterv1.VnetSpec{Name: "v2", Vni: 101}},
		{Action: "put", Kind: "eni", EniSpec: &dashcenterv1.EniSpec{Name: "e1", VnetName: "v1", MacAddress: "00:11:22:00:00:01", UnderlayIp: "10.0.0.1", AdminState: "up"}},
	})
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	if !res.Committed {
		t.Errorf("Committed=false; want true. failed_index=%d failed_err=%q", res.FailedIndex, res.FailedError)
	}
	if res.OpsCommitted != 3 {
		t.Errorf("OpsCommitted=%d; want 3", res.OpsCommitted)
	}
	// Verify the store has all 3.
	if _, err := svc.Get(ctx, "default", "vnet", "v1"); err != nil {
		t.Errorf("Get v1: %v", err)
	}
	if _, err := svc.Get(ctx, "default", "eni", "e1"); err != nil {
		t.Errorf("Get e1: %v", err)
	}
}

func TestApplyBatch_FailureRollsBackAllPriorOps_PC_G8(t *testing.T) {
	// 3 vnets where the 3rd has empty name → fails ErrInvalidArgument.
	// The first 2 succeed then must be rolled back.
	svc := newTestService(t)
	ctx := context.Background()
	res, err := svc.ApplyBatch(ctx, []BatchOp{
		{Action: "put", Kind: "vnet", VnetSpec: &dashcenterv1.VnetSpec{Name: "v-a", Vni: 100}},
		{Action: "put", Kind: "vnet", VnetSpec: &dashcenterv1.VnetSpec{Name: "v-b", Vni: 101}},
		{Action: "put", Kind: "vnet", VnetSpec: &dashcenterv1.VnetSpec{Name: "", Vni: 102}}, // bad
	})
	if err == nil {
		t.Fatal("ApplyBatch: want error")
	}
	if res == nil {
		t.Fatal("res is nil; want envelope")
	}
	if res.Committed {
		t.Error("Committed=true; want false")
	}
	if res.FailedIndex != 2 {
		t.Errorf("FailedIndex=%d; want 2", res.FailedIndex)
	}
	if res.Compensated != 2 {
		t.Errorf("Compensated=%d; want 2 (both prior ops undone)", res.Compensated)
	}
	// Verify rollback: v-a and v-b should be absent.
	if _, err := svc.Get(ctx, "default", "vnet", "v-a"); err == nil {
		t.Error("v-a should be absent after rollback")
	}
	if _, err := svc.Get(ctx, "default", "vnet", "v-b"); err == nil {
		t.Error("v-b should be absent after rollback")
	}
}

func TestApplyBatch_CapacityExceeded_MidBatch_RollsBack(t *testing.T) {
	// Service with 1-ENI MaxEnis on dpu-1. Batch: 2 ENIs on dpu-1.
	// First succeeds, second exceeds → rolled back. Final state:
	// 0 ENIs on dpu-1.
	fs, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })
	inv := inventory.New()
	if err := inv.Register(inventory.DpuEntry{ID: "dpu-1", Endpoint: "dpu-1:50051"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := inv.SetLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 1}); err != nil {
		t.Fatalf("SetLimits: %v", err)
	}
	tr := capacity.NewTracker(inv)
	svc := NewControlPlane(fs, inv, nil, tr, nil, nil, nil)
	ctx := context.Background()
	// Seed parent vnet via service.
	if _, err := svc.PutVnet(ctx, "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}

	res, err := svc.ApplyBatch(ctx, []BatchOp{
		{Action: "put", Kind: "eni", EniSpec: &dashcenterv1.EniSpec{
			Name: "e1", VnetName: "vnet-app", MacAddress: "00:00:00:00:00:01",
			UnderlayIp: "10.0.0.1", AdminState: "up", PlacementHintDpuIds: []string{"dpu-1"}}},
		{Action: "put", Kind: "eni", EniSpec: &dashcenterv1.EniSpec{
			Name: "e2", VnetName: "vnet-app", MacAddress: "00:00:00:00:00:02",
			UnderlayIp: "10.0.0.2", AdminState: "up", PlacementHintDpuIds: []string{"dpu-1"}}},
	})
	if err == nil {
		t.Fatal("want capacity error")
	}
	if res.Committed {
		t.Error("Committed=true; want false")
	}
	// Final state: e1 should have been rolled back (deleted from store).
	if _, err := svc.Get(ctx, "default", "eni", "e1"); err == nil {
		t.Error("e1 should be absent after rollback")
	}
	// Capacity counter should be 0 too.
	enis, _, _ := tr.SnapshotForDPU("dpu-1")
	if enis != 0 {
		t.Errorf("dpu-1 enis after rollback=%d; want 0", enis)
	}
}

func TestApplyBatch_EmptyOps(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.ApplyBatch(context.Background(), nil)
	if err == nil {
		t.Error("want ErrInvalidArgument for empty ops")
	}
}

func TestApplyBatch_UnknownAction(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.ApplyBatch(context.Background(), []BatchOp{{Action: "patch", Kind: "vnet"}})
	if err == nil {
		t.Error("want error for unknown action")
	}
}

func TestApplyBatch_UnknownKind(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.ApplyBatch(context.Background(), []BatchOp{{Action: "put", Kind: "rocket"}})
	if err == nil {
		t.Error("want error for unknown kind")
	}
}

func TestApplyBatch_DeleteOp(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	// Seed a vnet.
	if _, err := svc.PutVnet(ctx, "default", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := svc.ApplyBatch(ctx, []BatchOp{
		{Action: "delete", Kind: "vnet", Name: "v1"},
	})
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	if !res.Committed {
		t.Error("Committed=false; want true")
	}
	if _, err := svc.Get(ctx, "default", "vnet", "v1"); err == nil {
		t.Error("v1 should be deleted")
	}
}
