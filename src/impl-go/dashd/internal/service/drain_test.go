// PC-G7 service-level drain integration: 5 ENIs on dpu-1 → drain →
// all migrate, source ends cordoned, capacity counters consistent.
package service

import (
	"context"
	"fmt"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/operations"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func newServiceForDrain(t *testing.T, dpus ...string) (ControlPlaneService, *capacity.Tracker, *operations.Manager) {
	t.Helper()
	fs, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { fs.Close() })
	inv := inventory.New()
	for _, id := range dpus {
		if err := inv.Register(inventory.DpuEntry{ID: id, Endpoint: id + ":50051"}); err != nil {
			t.Fatalf("Register %s: %v", id, err)
		}
	}
	tr := capacity.NewTracker(inv)
	ops := operations.New(inv)
	return NewControlPlane(fs, inv, nil, tr, nil, ops, nil), tr, ops
}

func TestDrainDpu_5ENIs_AllMoved_PC_G7(t *testing.T) {
	svc, tr, ops := newServiceForDrain(t, "dpu-1", "dpu-2", "dpu-3")
	ctx := context.Background()

	// Seed: parent vnet + 5 ENIs pinned to dpu-1.
	if _, err := svc.PutVnet(ctx, "default", &dashcenterv1.VnetSpec{Name: "vnet-app", Vni: 100}); err != nil {
		t.Fatalf("seed vnet: %v", err)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("eni-%d", i)
		if _, err := svc.PutEni(ctx, "default", &dashcenterv1.EniSpec{
			Name: name, VnetName: "vnet-app",
			MacAddress: fmt.Sprintf("00:11:22:00:00:%02x", i),
			UnderlayIp: fmt.Sprintf("10.0.5.%d", 10+i),
			AdminState: "up",
			PlacementHintDpuIds: []string{"dpu-1"},
		}); err != nil {
			t.Fatalf("seed eni %s: %v", name, err)
		}
	}

	// Sanity: capacity tracker should see 5 ENIs on dpu-1, 0 on dpu-2/dpu-3.
	if enis, _, _ := tr.SnapshotForDPU("dpu-1"); enis != 5 {
		t.Fatalf("pre-drain dpu-1 enis=%d; want 5", enis)
	}

	// Drain dpu-1.
	res, err := svc.DrainDpu(ctx, "dpu-1", operations.DrainOpts{Parallelism: 2, Reason: "PC-G7 e2e"})
	if err != nil {
		t.Fatalf("DrainDpu: %v", err)
	}
	if !res.Cordoned {
		t.Error("Cordoned=false; want true")
	}
	if res.TotalEnis != 5 {
		t.Errorf("TotalEnis=%d; want 5", res.TotalEnis)
	}
	if len(res.Migrated) != 5 {
		t.Errorf("Migrated=%d; want 5. Failed=%v", len(res.Migrated), res.Failed)
	}

	// Post-drain capacity: dpu-1 = 0, dpu-2 + dpu-3 = 5 in total.
	if enis, _, _ := tr.SnapshotForDPU("dpu-1"); enis != 0 {
		t.Errorf("post-drain dpu-1 enis=%d; want 0", enis)
	}
	a, _, _ := tr.SnapshotForDPU("dpu-2")
	b, _, _ := tr.SnapshotForDPU("dpu-3")
	if a+b != 5 {
		t.Errorf("post-drain dpu-2+dpu-3 enis=%d+%d=%d; want sum=5", a, b, a+b)
	}

	if !ops.IsCordoned("dpu-1") {
		t.Error("dpu-1 should remain cordoned after drain")
	}

	// Re-reading any ENI should now show its new placement hint
	// pointing to dpu-2 or dpu-3 only.
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("eni-%d", i)
		item, err := svc.Get(ctx, "default", "eni", name)
		if err != nil {
			t.Errorf("get %s: %v", name, err)
			continue
		}
		// Body should NOT contain dpu-1 in placement hint.
		if containsBytes(item.Spec, "dpu-1") {
			t.Errorf("post-drain eni %s still references dpu-1 in placement hint: %s", name, string(item.Spec))
		}
	}
}

func TestDrainDpu_NoDestinations_AllFail(t *testing.T) {
	svc, _, ops := newServiceForDrain(t, "dpu-1") // only one DPU in fleet
	ctx := context.Background()
	if _, err := svc.PutVnet(ctx, "default", &dashcenterv1.VnetSpec{Name: "v", Vni: 100}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := svc.PutEni(ctx, "default", &dashcenterv1.EniSpec{
		Name: "e", VnetName: "v", MacAddress: "00:11:22:00:00:01",
		UnderlayIp: "10.0.5.1", AdminState: "up",
		PlacementHintDpuIds: []string{"dpu-1"},
	}); err != nil {
		t.Fatalf("seed eni: %v", err)
	}
	res, err := svc.DrainDpu(ctx, "dpu-1", operations.DrainOpts{})
	if err != nil {
		t.Fatalf("DrainDpu: %v", err)
	}
	if len(res.Failed) != 1 {
		t.Errorf("Failed=%d; want 1 (no destination)", len(res.Failed))
	}
	if len(res.Migrated) != 0 {
		t.Errorf("Migrated=%d; want 0", len(res.Migrated))
	}
	if !ops.IsCordoned("dpu-1") {
		t.Error("source must remain cordoned after failed drain")
	}
}

func TestDrainDpu_UnknownDPU(t *testing.T) {
	svc, _, _ := newServiceForDrain(t, "dpu-1")
	_, err := svc.DrainDpu(context.Background(), "dpu-typo", operations.DrainOpts{})
	if err == nil {
		t.Error("want error for unknown DPU")
	}
}

func TestDrainDpu_NilOpsOrCap_Rejected(t *testing.T) {
	svc := newTestService(t) // no ops, no cap
	if _, err := svc.DrainDpu(context.Background(), "dpu-1", operations.DrainOpts{}); err == nil {
		t.Error("want ErrInvalidArgument when ops/cap missing")
	}
}

// containsBytes is a small helper (avoids strings.Contains import noise
// for a one-line check).
func containsBytes(b []byte, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == sub {
			return true
		}
	}
	return false
}
