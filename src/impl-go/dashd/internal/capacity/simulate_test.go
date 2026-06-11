// Simulate (PB-2) tests. Uses the same fakeInventory / dpuWithLimits
// helpers from capacity_test.go.
package capacity

import (
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func TestSimulate_EmptyBatch(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})))
	res := tr.Simulate(nil)
	if !res.WouldSucceed {
		t.Errorf("empty batch: WouldSucceed=false; want true")
	}
	if len(res.PerDPU) != 0 {
		t.Errorf("empty batch should produce no rows; got %d", len(res.PerDPU))
	}
}

func TestSimulate_PutEni_WithinCapacity_PositiveDelta(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5}))
	tr := NewTracker(inv)

	res := tr.Simulate([]SimOp{{
		Action: "put", Namespace: "default", Kind: "eni",
		Spec: &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}},
	}})
	if !res.WouldSucceed {
		t.Fatalf("WouldSucceed=false; errors=%v", res.Errors)
	}
	if len(res.PerDPU) != 1 || res.PerDPU[0].DpuID != "dpu-1" || res.PerDPU[0].DeltaEnis != 1 {
		t.Errorf("perDPU=%v; want [{dpu-1 +1 0 0}]", res.PerDPU)
	}
}

func TestSimulate_PutEni_ExceedsCapacity_PB_G2(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 2}))
	tr := NewTracker(inv)
	// Seed: 2 ENIs already → at limit.
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "a", PlacementHintDpuIds: []string{"dpu-1"}})
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "b", PlacementHintDpuIds: []string{"dpu-1"}})

	res := tr.Simulate([]SimOp{{
		Action: "put", Namespace: "default", Kind: "eni",
		Spec: &dashcenterv1.EniSpec{Name: "c", PlacementHintDpuIds: []string{"dpu-1"}},
	}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (3rd ENI over MaxEnis=2)")
	}
	if len(res.Errors) == 0 {
		t.Fatal("expected at least one error")
	}
	if !strings.Contains(res.Errors[0].Reason, "max_enis") {
		t.Errorf("expected max_enis in error; got %q", res.Errors[0].Reason)
	}
	if !strings.Contains(res.Errors[0].Reason, "limit=2") {
		t.Errorf("expected limit=2 in error; got %q", res.Errors[0].Reason)
	}
	// PerDPU should flag exceeds_capacity=true on dpu-1.
	found := false
	for _, row := range res.PerDPU {
		if row.DpuID == "dpu-1" && row.ExceedsCapacity {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PerDPU row for dpu-1 with ExceedsCapacity=true; got %v", res.PerDPU)
	}
}

func TestSimulate_TrackerStateUnchanged(t *testing.T) {
	// Simulate must not mutate live counters — repeated calls must
	// return the same answer.
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 2}))
	tr := NewTracker(inv)

	op := []SimOp{{
		Action: "put", Namespace: "default", Kind: "eni",
		Spec: &dashcenterv1.EniSpec{Name: "a", PlacementHintDpuIds: []string{"dpu-1"}},
	}}
	r1 := tr.Simulate(op)
	r2 := tr.Simulate(op)
	if r1.WouldSucceed != r2.WouldSucceed {
		t.Errorf("second Simulate diverged: r1=%v r2=%v", r1.WouldSucceed, r2.WouldSucceed)
	}
	enis, _, _ := tr.SnapshotForDPU("dpu-1")
	if enis != 0 {
		t.Errorf("tracker mutated by Simulate: enis=%d; want 0", enis)
	}
}

func TestSimulate_OverlayWithinBatch(t *testing.T) {
	// Two Put ops in the same batch must compose: 1st takes the only
	// remaining slot, 2nd must be rejected by the overlay.
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 1}))
	tr := NewTracker(inv)

	res := tr.Simulate([]SimOp{
		{Action: "put", Namespace: "default", Kind: "eni",
			Spec: &dashcenterv1.EniSpec{Name: "a", PlacementHintDpuIds: []string{"dpu-1"}}},
		{Action: "put", Namespace: "default", Kind: "eni",
			Spec: &dashcenterv1.EniSpec{Name: "b", PlacementHintDpuIds: []string{"dpu-1"}}},
	})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (2nd ENI exceeds within-batch overlay)")
	}
	// First op should not appear in errors; second should.
	for _, e := range res.Errors {
		if e.Op == 0 {
			t.Errorf("op[0] should be admitted; got error %q", e.Reason)
		}
	}
}

func TestSimulate_PutVnetMapping_OverLimit(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 2}))
	tr := NewTracker(inv)
	tr.ApplyVnetMapping("default", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"})
	tr.ApplyVnetMapping("default", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.2"})

	res := tr.Simulate([]SimOp{{
		Action: "put", Namespace: "default", Kind: "vnet_mapping",
		Spec: &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.3"},
	}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false")
	}
	if !strings.Contains(res.Errors[0].Reason, "max_vnet_mappings") {
		t.Errorf("expected max_vnet_mappings; got %q", res.Errors[0].Reason)
	}
}

func TestSimulate_PutAclPolicy_ExceedsRules(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10, MaxAclRulesPerGroup: 3}))
	tr := NewTracker(inv)
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}})

	res := tr.Simulate([]SimOp{{
		Action: "put", Namespace: "default", Kind: "acl_policy",
		Spec: &dashcenterv1.AclPolicySpec{
			Name: "p1", EniNames: []string{"eni-1"}, Rules: makeRules(5),
		},
	}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (5 rules > MaxAclRulesPerGroup=3)")
	}
	if !strings.Contains(res.Errors[0].Reason, "max_acl_rules_per_group") {
		t.Errorf("expected max_acl_rules_per_group; got %q", res.Errors[0].Reason)
	}
}

func TestSimulate_DeleteEni_FreesCapacity(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 1}))
	tr := NewTracker(inv)
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "old", PlacementHintDpuIds: []string{"dpu-1"}})

	// In one batch: delete the old ENI, then create a new one. Should succeed.
	res := tr.Simulate([]SimOp{
		{Action: "delete", Namespace: "default", Kind: "eni", Name: "old"},
		{Action: "put", Namespace: "default", Kind: "eni",
			Spec: &dashcenterv1.EniSpec{Name: "new", PlacementHintDpuIds: []string{"dpu-1"}}},
	})
	if !res.WouldSucceed {
		t.Fatalf("WouldSucceed=false; errors=%v", res.Errors)
	}
	// Net per-DPU delta should be 0 (one in, one out).
	for _, row := range res.PerDPU {
		if row.DpuID == "dpu-1" && row.DeltaEnis != 0 {
			t.Errorf("dpu-1 DeltaEnis=%d; want 0 (1 in + 1 out)", row.DeltaEnis)
		}
	}
}

func TestSimulate_UnknownDPU_FailsClosed(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5}))
	tr := NewTracker(inv)

	res := tr.Simulate([]SimOp{{
		Action: "put", Namespace: "default", Kind: "eni",
		Spec: &dashcenterv1.EniSpec{Name: "e", PlacementHintDpuIds: []string{"dpu-typo"}},
	}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (placement hint references unknown DPU)")
	}
}

func TestSimulate_UnsupportedKind(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})))
	res := tr.Simulate([]SimOp{{Action: "put", Kind: "vnet", Spec: &dashcenterv1.VnetSpec{Name: "x"}}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (PB-2 doesn't support vnet)")
	}
	if !strings.Contains(res.Errors[0].Reason, "unsupported kind") {
		t.Errorf("expected unsupported kind error; got %q", res.Errors[0].Reason)
	}
}

func TestSimulate_NilSpec(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})))
	res := tr.Simulate([]SimOp{{Action: "put", Kind: "eni", Spec: (*dashcenterv1.EniSpec)(nil)}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (nil spec)")
	}
}

// --- coverage extras: paths not exercised by primary tests --------

func TestRemoveVnetMapping_Roundtrip(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 5}))
	tr := NewTracker(inv)
	spec := &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"}
	tr.ApplyVnetMapping("default", spec)
	_, _, mappings := tr.SnapshotForDPU("dpu-1")
	if mappings != 1 {
		t.Fatalf("after Apply: mappings=%d; want 1", mappings)
	}
	// Remove using the canonical key.
	key := spec.GetVnetName() + "-" + spec.GetIpAddress()
	tr.RemoveVnetMapping("default", key)
	_, _, mappings = tr.SnapshotForDPU("dpu-1")
	if mappings != 0 {
		t.Errorf("after Remove: mappings=%d; want 0", mappings)
	}
	// Removing a non-existent key is a safe no-op.
	tr.RemoveVnetMapping("default", "nope")
	_, _, mappings = tr.SnapshotForDPU("dpu-1")
	if mappings != 0 {
		t.Errorf("after Remove of missing key: mappings=%d; want 0", mappings)
	}
}

func TestSimulate_PutVnetMapping_EmptyKey(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 5})))
	res := tr.Simulate([]SimOp{{
		Action: "put", Namespace: "default", Kind: "vnet_mapping",
		Spec: &dashcenterv1.VnetMappingSpec{},
	}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (empty key)")
	}
	if !strings.Contains(res.Errors[0].Reason, "empty key") {
		t.Errorf("expected empty key error; got %q", res.Errors[0].Reason)
	}
}

func TestSimulate_UnknownAction(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})))
	res := tr.Simulate([]SimOp{{Action: "patch", Kind: "eni"}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (unknown action)")
	}
}

func TestSimulate_DeleteUnsupportedKind(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})))
	res := tr.Simulate([]SimOp{{Action: "delete", Kind: "vnet", Name: "v"}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (delete vnet not supported in PB-2)")
	}
}

func TestSimulate_DeleteMissingName(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5})))
	res := tr.Simulate([]SimOp{{Action: "delete", Kind: "eni"}})
	if res.WouldSucceed {
		t.Fatal("WouldSucceed=true; want false (missing name)")
	}
}

func TestSimulate_DeleteVnetMapping(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 5}))
	tr := NewTracker(inv)
	tr.ApplyVnetMapping("default", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"})

	res := tr.Simulate([]SimOp{{
		Action: "delete", Namespace: "default", Kind: "vnet_mapping", Name: "v1-10.0.0.1",
	}})
	if !res.WouldSucceed {
		t.Fatalf("WouldSucceed=false; errors=%v", res.Errors)
	}
	// Should report -1 on dpu-1.
	for _, row := range res.PerDPU {
		if row.DpuID == "dpu-1" && row.DeltaVnetMappings != -1 {
			t.Errorf("dpu-1 DeltaVnetMappings=%d; want -1", row.DeltaVnetMappings)
		}
	}
}

func TestSimulate_DeleteAclPolicy(t *testing.T) {
	tr := NewTracker(newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5, MaxAclRulesPerGroup: 10})))
	res := tr.Simulate([]SimOp{{
		Action: "delete", Namespace: "default", Kind: "acl_policy", Name: "p1",
	}})
	// Delete acl_policy is a no-op in the overlay (PB-3 will fetch prior spec).
	if !res.WouldSucceed {
		t.Errorf("WouldSucceed=false; want true. Errors=%v", res.Errors)
	}
}
