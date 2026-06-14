// Tests for cluster.Aggregator. Uses a real file-backed DesiredStore +
// real inventory.Inventory; no etcd needed (registry is OpenSelfOnly).
package cluster

import (
	"context"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// ── harness ────────────────────────────────────────────────────────────

func openFileStore(t *testing.T) store.DesiredStore {
	t.Helper()
	st, err := file.Open(t.TempDir())
	if err != nil {
		t.Fatalf("file.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func newInv(t *testing.T, entries ...inventory.DpuEntry) *inventory.Inventory {
	t.Helper()
	inv := inventory.New()
	for _, e := range entries {
		if err := inv.Register(e); err != nil {
			t.Fatalf("inv.Register(%s): %v", e.ID, err)
		}
		if e.State != 0 {
			if err := inv.SetState(e.ID, e.State); err != nil {
				t.Fatalf("inv.SetState(%s): %v", e.ID, err)
			}
		}
		if e.Cordoned {
			if err := inv.SetCordoned(e.ID, true); err != nil {
				t.Fatalf("inv.SetCordoned(%s): %v", e.ID, err)
			}
		}
	}
	return inv
}

func putVnet(t *testing.T, st store.DesiredStore, ns, name string, vni uint32) {
	t.Helper()
	spec := &dashcenterv1.VnetSpec{Name: name, Vni: vni}
	_, err := st.Put(context.Background(), store.ObjectKey{Namespace: ns, Kind: "vnet", Name: name}, spec, 0)
	if err != nil {
		t.Fatalf("Put vnet %s/%s: %v", ns, name, err)
	}
}

func putEni(t *testing.T, st store.DesiredStore, ns, name, vnet string, dpus ...string) {
	t.Helper()
	spec := &dashcenterv1.EniSpec{Name: name, VnetName: vnet, PlacementHintDpuIds: dpus, AdminState: "up"}
	_, err := st.Put(context.Background(), store.ObjectKey{Namespace: ns, Kind: "eni", Name: name}, spec, 0)
	if err != nil {
		t.Fatalf("Put eni %s/%s: %v", ns, name, err)
	}
}

func putAcl(t *testing.T, st store.DesiredStore, ns, name string) {
	t.Helper()
	spec := &dashcenterv1.AclPolicySpec{Name: name}
	_, err := st.Put(context.Background(), store.ObjectKey{Namespace: ns, Kind: "acl_policy", Name: name}, spec, 0)
	if err != nil {
		t.Fatalf("Put acl %s/%s: %v", ns, name, err)
	}
}

// newAgg builds an aggregator with a self-only registry seeded with one
// peer (self).
func newAgg(t *testing.T, st store.DesiredStore, inv *inventory.Inventory, leader string, opts ...func(*AggregatorConfig)) *Aggregator {
	t.Helper()
	reg := OpenSelfOnly(PeerInfo{
		NodeID:    "dashd-1",
		RESTAddr:  "dashd-1:8443",
		GRPCAddr:  "dashd-1:9443",
		AdminAddr: "dashd-1:7443",
		Version:   "test",
		StartedAt: time.Date(2026, 6, 12, 0, 0, 0, 0, time.UTC),
		Labels:    map[string]string{"zone": "us-west-2a"},
	})
	t.Cleanup(func() { _ = reg.Close() })

	cfg := AggregatorConfig{
		Registry:   reg,
		Inventory:  inv,
		Store:      st,
		Elector:    constLeader{id: leader},
		Version:    "test",
		NodeID:     "dashd-1",
		Namespaces: []string{store.DefaultNamespace, "edge"},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	agg, err := NewAggregator(cfg)
	if err != nil {
		t.Fatalf("NewAggregator: %v", err)
	}
	return agg
}

// ── tests ──────────────────────────────────────────────────────────────

func TestNewAggregator_ValidatesRequiredFields(t *testing.T) {
	if _, err := NewAggregator(AggregatorConfig{}); err == nil {
		t.Error("empty config should error")
	}
	if _, err := NewAggregator(AggregatorConfig{Registry: OpenSelfOnly(PeerInfo{NodeID: "n"})}); err == nil {
		t.Error("missing Inventory should error")
	}
	if _, err := NewAggregator(AggregatorConfig{Registry: OpenSelfOnly(PeerInfo{NodeID: "n"}), Inventory: newInv(t)}); err == nil {
		t.Error("missing NodeID should error")
	}
}

func TestBuild_EmptyFleet(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t)
	agg := newAgg(t, st, inv, "dashd-1")

	resp, err := agg.Build(context.Background(), &dashcenterv1.GetTopologyRequest{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if resp.Cluster == nil || resp.Cluster.NodeCount != 1 {
		t.Errorf("expected 1 cluster node, got %+v", resp.Cluster)
	}
	if resp.Cluster.LeaderId != "dashd-1" || !resp.Cluster.Nodes[0].IsLeader {
		t.Errorf("self should be leader: %+v", resp.Cluster)
	}
	if !resp.Cluster.Healthy {
		t.Error("expected healthy=true (1 node, has leader)")
	}
	if resp.Summary.TotalDpus != 0 {
		t.Errorf("TotalDpus = %d; want 0", resp.Summary.TotalDpus)
	}
	if len(resp.Appliances) != 0 {
		t.Errorf("Appliances = %d; want 0", len(resp.Appliances))
	}
	if resp.Objects["default"] == nil || resp.Objects["default"].Vnets != 0 {
		t.Errorf("default objects should be present with zero counts: %+v", resp.Objects)
	}
}

func TestBuild_GroupsDpusByAppliance(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t,
		inventory.DpuEntry{ID: "dpu-1", Endpoint: "h:1", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"rack": "a1", "slot": "0", "zone": "us-west-2a", "tier": "gold"}},
		inventory.DpuEntry{ID: "dpu-2", Endpoint: "h:2", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"rack": "a1", "slot": "1", "zone": "us-west-2a", "tier": "gold"}},
		inventory.DpuEntry{ID: "dpu-3", Endpoint: "h:3", State: dashcenterv1.DpuState_DPU_STATE_FAILED, Labels: map[string]string{"rack": "a2", "slot": "0", "zone": "us-west-2b", "tier": "silver"}},
	)
	agg := newAgg(t, st, inv, "dashd-1")

	resp, err := agg.Build(context.Background(), nil) // nil req → defaults
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(resp.Appliances) != 2 {
		t.Fatalf("Appliances = %d; want 2", len(resp.Appliances))
	}
	if resp.Appliances[0].Id != "a1" || resp.Appliances[1].Id != "a2" {
		t.Errorf("expected sorted [a1, a2], got %v / %v", resp.Appliances[0].Id, resp.Appliances[1].Id)
	}
	if len(resp.Appliances[0].Dpus) != 2 {
		t.Errorf("a1 should have 2 dpus, got %d", len(resp.Appliances[0].Dpus))
	}
	// Sub-list also sorted by id.
	if resp.Appliances[0].Dpus[0].Id != "dpu-1" {
		t.Errorf("dpu order wrong: %s", resp.Appliances[0].Dpus[0].Id)
	}
	// Zones aggregated.
	if len(resp.Zones) != 2 {
		t.Errorf("Zones = %d; want 2", len(resp.Zones))
	}
	// Summary math.
	if resp.Summary.HealthyDpus != 2 || resp.Summary.OfflineDpus != 1 {
		t.Errorf("summary = %+v; want healthy=2 offline=1", resp.Summary)
	}
}

func TestBuild_EniIncludeToggle(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t, inventory.DpuEntry{ID: "dpu-1", Endpoint: "h:1", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"rack": "a1"}})
	putVnet(t, st, "default", "vnet-a", 100)
	putEni(t, st, "default", "eni-a-1", "vnet-a", "dpu-1")
	putEni(t, st, "default", "eni-a-2", "vnet-a", "dpu-1")

	agg := newAgg(t, st, inv, "dashd-1")

	// Without include_enis: count populated, payload empty.
	noEnis, err := agg.Build(context.Background(), &dashcenterv1.GetTopologyRequest{IncludeEnis: false})
	if err != nil {
		t.Fatalf("Build no-enis: %v", err)
	}
	dpu := noEnis.Appliances[0].Dpus[0]
	if dpu.EniCount != 2 {
		t.Errorf("EniCount = %d; want 2", dpu.EniCount)
	}
	if len(dpu.Enis) != 0 {
		t.Errorf("Enis = %d; want 0 (excluded)", len(dpu.Enis))
	}

	// With include_enis: payload present and sorted.
	withEnis, err := agg.Build(context.Background(), &dashcenterv1.GetTopologyRequest{IncludeEnis: true})
	if err != nil {
		t.Fatalf("Build with-enis: %v", err)
	}
	dpu = withEnis.Appliances[0].Dpus[0]
	if dpu.EniCount != 2 || len(dpu.Enis) != 2 {
		t.Errorf("EniCount/len = %d/%d; want 2/2", dpu.EniCount, len(dpu.Enis))
	}
	if dpu.Enis[0].Name != "eni-a-1" {
		t.Errorf("ENI order wrong: %v", dpu.Enis[0].Name)
	}
}

func TestBuild_ObjectCountsPerNamespace(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t)
	putVnet(t, st, "default", "v1", 1)
	putVnet(t, st, "default", "v2", 2)
	putAcl(t, st, "default", "a1")
	putVnet(t, st, "edge", "v3", 3)

	agg := newAgg(t, st, inv, "dashd-1")
	resp, err := agg.Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	d := resp.Objects["default"]
	if d == nil || d.Vnets != 2 || d.AclPolicies != 1 {
		t.Errorf("default counts wrong: %+v", d)
	}
	e := resp.Objects["edge"]
	if e == nil || e.Vnets != 1 {
		t.Errorf("edge counts wrong: %+v", e)
	}
}

func TestBuild_DeterministicOrdering(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t,
		inventory.DpuEntry{ID: "dpu-zzz", Endpoint: "h:1", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"rack": "z"}},
		inventory.DpuEntry{ID: "dpu-aaa", Endpoint: "h:2", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"rack": "a"}},
	)
	agg := newAgg(t, st, inv, "dashd-1")
	r1, _ := agg.Build(context.Background(), nil)
	r2, _ := agg.Build(context.Background(), nil)

	if r1.Appliances[0].Id != "a" || r1.Appliances[1].Id != "z" {
		t.Errorf("Appliances not sorted: %v", r1.Appliances)
	}
	// Same names across calls (timestamps differ but appliance/DPU
	// ordering must be byte-stable).
	for i := range r1.Appliances {
		if r1.Appliances[i].Id != r2.Appliances[i].Id {
			t.Errorf("Appliances[%d] differ across calls: %v vs %v", i, r1.Appliances[i], r2.Appliances[i])
		}
	}
}

func TestBuild_NoLeader_Unhealthy(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t)
	agg := newAgg(t, st, inv, "" /* no leader */)

	resp, err := agg.Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if resp.Cluster.Healthy {
		t.Error("expected unhealthy when no leader")
	}
	if resp.Cluster.LeaderId != "" {
		t.Errorf("LeaderId = %q; want empty", resp.Cluster.LeaderId)
	}
	if resp.Cluster.Nodes[0].IsLeader {
		t.Error("no node should be leader when leader_id is empty")
	}
}

func TestSummarize_CountsCordoned(t *testing.T) {
	st := openFileStore(t)
	inv := newInv(t,
		inventory.DpuEntry{ID: "dpu-up", Endpoint: "h:1", State: dashcenterv1.DpuState_DPU_STATE_UP, Labels: map[string]string{"rack": "r"}},
		inventory.DpuEntry{ID: "dpu-cord", Endpoint: "h:2", State: dashcenterv1.DpuState_DPU_STATE_UP, Cordoned: true, Labels: map[string]string{"rack": "r"}},
	)
	agg := newAgg(t, st, inv, "dashd-1")
	resp, _ := agg.Build(context.Background(), nil)
	if resp.Summary.HealthyDpus != 1 || resp.Summary.CordonedDpus != 1 {
		t.Errorf("summary = %+v; want healthy=1 cordoned=1", resp.Summary)
	}
}
