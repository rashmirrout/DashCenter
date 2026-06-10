// Capacity tracker tests. Uses a fake Inventory + the file-backed
// store so the tests exercise the same code paths the production
// service layer will hit.
package capacity

import (
	"context"
	"errors"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// --- fakeInventory ----------------------------------------------------

// fakeInventory implements the Inventory interface with a small in-memory
// map. We construct DpuEntries directly so tests can supply arbitrary
// Limits / Capabilities without going through the prober pipeline.
type fakeInventory struct {
	entries map[string]inventory.DpuEntry
}

func newFakeInv(entries ...inventory.DpuEntry) *fakeInventory {
	m := map[string]inventory.DpuEntry{}
	for _, e := range entries {
		m[e.ID] = e
	}
	return &fakeInventory{entries: m}
}

func (f *fakeInventory) Get(id string) (inventory.DpuEntry, error) {
	e, ok := f.entries[id]
	if !ok {
		return inventory.DpuEntry{}, errors.New("not found")
	}
	return e, nil
}

func (f *fakeInventory) List() []inventory.DpuEntry {
	out := make([]inventory.DpuEntry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out
}

// dpuWithLimits builds a DpuEntry pre-populated with the given limits.
// Limits we don't care about default to MaxInt-ish so they never trigger
// rejection in tests focused on one dimension.
func dpuWithLimits(id string, l *dashcenterv1.DpuCapacityLimits) inventory.DpuEntry {
	return inventory.DpuEntry{
		ID:       id,
		Endpoint: id + ":50051",
		Limits:   l,
		State:    dashcenterv1.DpuState_DPU_STATE_UP,
	}
}

// --- ENI capacity -----------------------------------------------------

func TestCheckEni_WithinCapacity(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 5}))
	tr := NewTracker(inv)

	spec := &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}}
	if err := tr.CheckEni("default", spec); err != nil {
		t.Fatalf("CheckEni: %v", err)
	}
}

func TestCheckEni_AtLimit_Rejected(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 2}))
	tr := NewTracker(inv)

	// Seed: 2 ENIs already on dpu-1 (capacity).
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "a", PlacementHintDpuIds: []string{"dpu-1"}})
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "b", PlacementHintDpuIds: []string{"dpu-1"}})

	// Third ENI should be rejected.
	err := tr.CheckEni("default", &dashcenterv1.EniSpec{
		Name:                "c",
		PlacementHintDpuIds: []string{"dpu-1"},
	})
	if !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("got %v; want ErrResourceExhausted", err)
	}
	// Message MUST include the dimension + limit + current + delta so
	// operators can act without reading dashd logs.
	for _, want := range []string{"dpu-1", "max_enis", "limit=2", "current=2", "requested=+1"} {
		if !contains_substring(err.Error(), want) {
			t.Errorf("error message missing %q: %v", want, err)
		}
	}
}

func TestCheckEni_UpdateExisting_NoDelta(t *testing.T) {
	// Re-Put an ENI that is already counted on the target DPU → no
	// admission delta, even at the limit.
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 1}))
	tr := NewTracker(inv)
	spec := &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}}
	tr.ApplyEni("default", spec)

	// Update the same ENI (e.g. mac change) — should pass.
	updated := &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}, MacAddress: "aa:bb:cc:dd:ee:01"}
	if err := tr.CheckEni("default", updated); err != nil {
		t.Errorf("CheckEni on update: %v; want nil", err)
	}
}

func TestCheckEni_UnknownTargetDPU_Rejected(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10}))
	tr := NewTracker(inv)

	err := tr.CheckEni("default", &dashcenterv1.EniSpec{
		Name:                "eni-1",
		PlacementHintDpuIds: []string{"dpu-typo"},
	})
	if !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("got %v; want ErrResourceExhausted", err)
	}
}

func TestCheckEni_NoLimits_Allowed(t *testing.T) {
	// DPU hasn't advertised limits yet → tracker can't admission-check
	// against it. Should pass.
	inv := newFakeInv(inventory.DpuEntry{ID: "dpu-1", Endpoint: "dpu-1:50051"})
	tr := NewTracker(inv)

	err := tr.CheckEni("default", &dashcenterv1.EniSpec{
		Name:                "eni-1",
		PlacementHintDpuIds: []string{"dpu-1"},
	})
	if err != nil {
		t.Errorf("CheckEni: %v; want nil (no advertised limits)", err)
	}
}

func TestCheckEni_NilSpec(t *testing.T) {
	tr := NewTracker(newFakeInv())
	if err := tr.CheckEni("default", nil); err != nil {
		t.Errorf("nil spec: %v; want nil", err)
	}
}

// --- VnetMapping capacity ---------------------------------------------

func TestCheckVnetMapping_WithinCapacity(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 5}))
	tr := NewTracker(inv)

	err := tr.CheckVnetMapping("default", &dashcenterv1.VnetMappingSpec{
		VnetName: "v1", IpAddress: "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("CheckVnetMapping: %v", err)
	}
}

func TestCheckVnetMapping_AtLimit_Rejected(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 2}))
	tr := NewTracker(inv)

	// Seed: 2 mappings (fleet-wide → both count against dpu-1).
	tr.ApplyVnetMapping("default", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"})
	tr.ApplyVnetMapping("default", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.2"})

	err := tr.CheckVnetMapping("default", &dashcenterv1.VnetMappingSpec{
		VnetName: "v1", IpAddress: "10.0.0.3",
	})
	if !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("got %v; want ErrResourceExhausted", err)
	}
}

func TestCheckVnetMapping_UpdateExisting_NoDelta(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxVnetMappings: 1}))
	tr := NewTracker(inv)
	spec := &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"}
	tr.ApplyVnetMapping("default", spec)

	// Same key, different underlay (e.g. mac change) — should pass.
	updated := &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1", MacAddress: "aa:bb:cc:dd:ee:01"}
	if err := tr.CheckVnetMapping("default", updated); err != nil {
		t.Errorf("CheckVnetMapping on update: %v; want nil", err)
	}
}

// --- AclPolicy rule-count capacity ------------------------------------

func TestCheckAclPolicy_WithinCapacity(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10, MaxAclRulesPerGroup: 100}))
	tr := NewTracker(inv)

	// ACL references an ENI we haven't placed yet → no targets, no
	// admission concern. But it's still a valid spec.
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}})

	spec := &dashcenterv1.AclPolicySpec{
		Name:     "policy-1",
		EniNames: []string{"eni-1"},
		Rules:    threeRules(),
	}
	if err := tr.CheckAclPolicy("default", spec, 0); err != nil {
		t.Fatalf("CheckAclPolicy: %v", err)
	}
}

func TestCheckAclPolicy_OverRuleLimit_Rejected(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10, MaxAclRulesPerGroup: 5}))
	tr := NewTracker(inv)
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}})

	// Seed an ACL with 4 rules — that's within limit.
	first := &dashcenterv1.AclPolicySpec{
		Name: "p1", EniNames: []string{"eni-1"},
		Rules: makeRules(4),
	}
	tr.ApplyAclPolicy("default", first, 0)

	// Add a second policy with 3 rules → 4 + 3 = 7 > 5 → reject.
	second := &dashcenterv1.AclPolicySpec{
		Name: "p2", EniNames: []string{"eni-1"},
		Rules: makeRules(3),
	}
	err := tr.CheckAclPolicy("default", second, 0)
	if !errors.Is(err, ErrResourceExhausted) {
		t.Fatalf("got %v; want ErrResourceExhausted", err)
	}
	if !contains_substring(err.Error(), "max_acl_rules_per_group") {
		t.Errorf("error should mention max_acl_rules_per_group: %v", err)
	}
}

func TestCheckAclPolicy_Update_DeltaOnly(t *testing.T) {
	// Updating an existing ACL with fewer rules → no admission concern.
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10, MaxAclRulesPerGroup: 3}))
	tr := NewTracker(inv)
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}})
	tr.ApplyAclPolicy("default", &dashcenterv1.AclPolicySpec{
		Name: "p1", EniNames: []string{"eni-1"}, Rules: makeRules(3),
	}, 0)

	// Replace p1 with a 2-rule version → delta = -1 → fine.
	updated := &dashcenterv1.AclPolicySpec{Name: "p1", EniNames: []string{"eni-1"}, Rules: makeRules(2)}
	if err := tr.CheckAclPolicy("default", updated, 3); err != nil {
		t.Errorf("CheckAclPolicy shrink: %v; want nil", err)
	}
}

// --- Apply / Remove arithmetic ----------------------------------------

func TestApplyRemoveEni_Roundtrip(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10}))
	tr := NewTracker(inv)

	spec := &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}}
	tr.ApplyEni("default", spec)
	enis, _, _ := tr.SnapshotForDPU("dpu-1")
	if enis != 1 {
		t.Errorf("after Apply: enis=%d; want 1", enis)
	}

	tr.RemoveEni("default", "eni-1")
	enis, _, _ = tr.SnapshotForDPU("dpu-1")
	if enis != 0 {
		t.Errorf("after Remove: enis=%d; want 0", enis)
	}
}

func TestApplyEni_RehostsBetweenDPUs(t *testing.T) {
	inv := newFakeInv(
		dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10}),
		dpuWithLimits("dpu-2", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10}),
	)
	tr := NewTracker(inv)

	spec := &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}}
	tr.ApplyEni("default", spec)
	enis1, _, _ := tr.SnapshotForDPU("dpu-1")
	enis2, _, _ := tr.SnapshotForDPU("dpu-2")
	if enis1 != 1 || enis2 != 0 {
		t.Fatalf("initial: dpu-1=%d dpu-2=%d; want 1, 0", enis1, enis2)
	}

	// Move it to dpu-2.
	rehosted := &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-2"}}
	tr.ApplyEni("default", rehosted)
	enis1, _, _ = tr.SnapshotForDPU("dpu-1")
	enis2, _, _ = tr.SnapshotForDPU("dpu-2")
	if enis1 != 0 || enis2 != 1 {
		t.Errorf("after rehost: dpu-1=%d dpu-2=%d; want 0, 1", enis1, enis2)
	}
}

func TestRemoveAclPolicy_DecrementsRules(t *testing.T) {
	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10, MaxAclRulesPerGroup: 100}))
	tr := NewTracker(inv)
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-1"}})
	spec := &dashcenterv1.AclPolicySpec{Name: "p1", EniNames: []string{"eni-1"}, Rules: makeRules(5)}
	tr.ApplyAclPolicy("default", spec, 0)

	_, rules, _ := tr.SnapshotForDPU("dpu-1")
	if rules != 5 {
		t.Fatalf("after Apply: rules=%d; want 5", rules)
	}
	tr.RemoveAclPolicy("default", spec)
	_, rules, _ = tr.SnapshotForDPU("dpu-1")
	if rules != 0 {
		t.Errorf("after Remove: rules=%d; want 0", rules)
	}
}

// --- Recount from store -----------------------------------------------

func TestRecount_RebuildsFromStore(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx := context.Background()
	// Seed: 2 ENIs on dpu-1, 1 mapping (fleet-wide), 1 ACL with 3 rules
	// bound to eni-a.
	put := func(ns, kind, name string, spec any) {
		t.Helper()
		if _, err := st.Put(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name}, spec, 0); err != nil {
			t.Fatalf("seed Put %s/%s: %v", kind, name, err)
		}
	}
	put("default", "eni", "eni-a", &dashcenterv1.EniSpec{Name: "eni-a", PlacementHintDpuIds: []string{"dpu-1"}})
	put("default", "eni", "eni-b", &dashcenterv1.EniSpec{Name: "eni-b", PlacementHintDpuIds: []string{"dpu-1"}})
	put("default", "vnet_mapping", "v1-10.0.0.1", &dashcenterv1.VnetMappingSpec{VnetName: "v1", IpAddress: "10.0.0.1"})
	put("default", "acl_policy", "policy-1", &dashcenterv1.AclPolicySpec{
		Name: "policy-1", EniNames: []string{"eni-a"}, Rules: makeRules(3),
	})

	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 10, MaxVnetMappings: 10, MaxAclRulesPerGroup: 100}))
	tr := NewTracker(inv)

	if err := tr.Recount(ctx, st); err != nil {
		t.Fatalf("Recount: %v", err)
	}

	enis, rules, mappings := tr.SnapshotForDPU("dpu-1")
	if enis != 2 {
		t.Errorf("enis=%d; want 2", enis)
	}
	if mappings != 1 {
		t.Errorf("mappings=%d; want 1", mappings)
	}
	if rules != 3 {
		t.Errorf("rules=%d; want 3", rules)
	}
}

func TestRecount_NoDPUs_Clears(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	tr := NewTracker(newFakeInv())
	// Seed some state then call Recount — should clear cleanly.
	tr.ApplyEni("default", &dashcenterv1.EniSpec{Name: "eni-1", PlacementHintDpuIds: []string{"dpu-x"}})
	if err := tr.Recount(context.Background(), st); err != nil {
		t.Fatalf("Recount: %v", err)
	}
	enis, _, _ := tr.SnapshotForDPU("dpu-x")
	if enis != 0 {
		t.Errorf("after Recount with no DPUs: enis=%d; want 0", enis)
	}
}

func TestRecount_CtxCancelled(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	inv := newFakeInv(dpuWithLimits("dpu-1", &dashcenterv1.DpuCapacityLimits{MaxEnis: 1}))
	tr := NewTracker(inv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tr.Recount(ctx, st); err == nil {
		t.Error("Recount with cancelled ctx should return ctx.Err()")
	}
}

// --- placement / nil-spec edge cases ----------------------------------

func TestCheckVnetMapping_NilSpec(t *testing.T) {
	tr := NewTracker(newFakeInv())
	if err := tr.CheckVnetMapping("default", nil); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

func TestCheckAclPolicy_NilSpec(t *testing.T) {
	tr := NewTracker(newFakeInv())
	if err := tr.CheckAclPolicy("default", nil, 0); err != nil {
		t.Errorf("nil spec: %v", err)
	}
}

// --- helpers ----------------------------------------------------------

func threeRules() []*dashcenterv1.AclRuleSpec {
	return makeRules(3)
}

func makeRules(n int) []*dashcenterv1.AclRuleSpec {
	out := make([]*dashcenterv1.AclRuleSpec, n)
	for i := 0; i < n; i++ {
		out[i] = &dashcenterv1.AclRuleSpec{Priority: uint32(i + 100), Action: "allow"}
	}
	return out
}

func contains_substring(s, sub string) bool {
	return indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
