// aggregator.go — pure function from (registry, inventory, store, elector)
// to a dashcenter.v1.TopologyResponse. No IO outside the store.List
// calls in countObjects; everything else is in-memory.
//
// Determinism: every list (Cluster.Nodes, Appliances, Appliances[i].Dpus,
// Appliances[i].Dpus[k].Enis, Zones) is sorted by stable key so two
// back-to-back calls on identical inputs return byte-identical bodies.
// This matters for diff-based clients (e.g. Prometheus exporters).
package cluster

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// LeaderObserver is the minimal slice of an elector that the aggregator
// needs. Implemented by *leader.EtcdElector and *leader.NoneElector.
type LeaderObserver interface {
	LeaderID() string
}

// constLeader is the trivial LeaderObserver for tests and single-node
// installs.
type constLeader struct{ id string }

func (c constLeader) LeaderID() string { return c.id }

// NewConstLeader is exported so tests / single-node main.go can wire a
// fixed leader id.
func NewConstLeader(id string) LeaderObserver { return constLeader{id: id} }

// InventoryView is the slice of inventory.Inventory the aggregator needs.
// Defined here so tests can inject a fake; *inventory.Inventory satisfies
// it naturally.
type InventoryView interface {
	List() []inventory.DpuEntry
}

// EniPlacementSource resolves "which ENIs live on which DPU" from the
// already-loaded *placement.DesiredSpecs. It exists as a function value
// rather than a struct method so test code can supply deterministic
// fixtures without rebuilding a full DesiredSpecs.
type EniPlacementSource func(specs *placement.DesiredSpecs) map[string][]*dashcenterv1.EniTopInfo

// DefaultEniPlacementSource returns a function that derives per-DPU
// ENI lists from EniSpec.PlacementHintDpuIds — the same field
// placement.ResolveAll uses to spread ENIs across the fleet.
//
// When include is false the returned EniTopInfo slices are nil
// (eni_count is still computed and populated elsewhere).
func DefaultEniPlacementSource(include bool) EniPlacementSource {
	return func(specs *placement.DesiredSpecs) map[string][]*dashcenterv1.EniTopInfo {
		out := map[string][]*dashcenterv1.EniTopInfo{}
		if specs == nil {
			return out
		}
		for name, eni := range specs.Enis {
			if eni == nil {
				continue
			}
			dpus := eni.PlacementHintDpuIds
			if len(dpus) == 0 {
				dpus = []string{"unassigned"}
			}
			for _, dpuID := range dpus {
				if include {
					out[dpuID] = append(out[dpuID], &dashcenterv1.EniTopInfo{
						Name:       name,
						VnetName:   eni.VnetName,
						MacAddress: eni.MacAddress,
						AdminState: eni.AdminState,
					})
				} else {
					// When ENIs are excluded from the response we still
					// need the per-DPU count, so we record a 1-byte
					// placeholder. The handler uses the count, not the
					// payload.
					out[dpuID] = append(out[dpuID], nil)
				}
			}
		}
		// Deterministic ordering: sort each per-DPU slice by name. nil
		// entries (count-only mode) stay grouped but the count is the
		// same either way.
		for _, list := range out {
			sort.SliceStable(list, func(i, j int) bool {
				if list[i] == nil || list[j] == nil {
					return false
				}
				return list[i].Name < list[j].Name
			})
		}
		return out
	}
}

// AggregatorConfig binds the aggregator to its dependencies. All fields
// are required except Namespaces (defaults to ["default"]) and
// EniPlacement (defaults to DefaultEniPlacementSource).
type AggregatorConfig struct {
	Registry      *Registry
	Inventory     InventoryView
	Store         store.DesiredStore
	Elector       LeaderObserver
	Version       string   // dashd version published into cluster.nodes[*].version
	BuildSHA      string   // optional git SHA
	NodeID        string   // self
	Namespaces    []string // namespaces to enumerate for the per-namespace object counts
	IncludeAlways bool     // if true, include ENI payloads regardless of request.IncludeEnis
}

// Aggregator builds TopologyResponse envelopes. Stateless except for
// the AggregatorConfig — safe to share across concurrent RPC handlers.
type Aggregator struct {
	cfg AggregatorConfig
}

// NewAggregator validates the config and returns the aggregator.
func NewAggregator(cfg AggregatorConfig) (*Aggregator, error) {
	if cfg.Registry == nil {
		return nil, fmt.Errorf("cluster.NewAggregator: Registry is required")
	}
	if cfg.Inventory == nil {
		return nil, fmt.Errorf("cluster.NewAggregator: Inventory is required")
	}
	if cfg.NodeID == "" {
		return nil, fmt.Errorf("cluster.NewAggregator: NodeID is required")
	}
	if cfg.Elector == nil {
		cfg.Elector = constLeader{}
	}
	if len(cfg.Namespaces) == 0 {
		cfg.Namespaces = []string{store.DefaultNamespace}
	}
	return &Aggregator{cfg: cfg}, nil
}

// Build assembles the TopologyResponse. Returns nil + wrapped error if
// LoadDesiredSpecs fails; all other failures degrade gracefully (e.g.
// inventory empty, registry self-only).
func (a *Aggregator) Build(ctx context.Context, req *dashcenterv1.GetTopologyRequest) (*dashcenterv1.TopologyResponse, error) {
	if req == nil {
		req = &dashcenterv1.GetTopologyRequest{}
	}
	includeEnis := req.IncludeEnis || a.cfg.IncludeAlways

	peers := a.cfg.Registry.Snapshot()
	dpus := a.cfg.Inventory.List()
	specs, err := placement.LoadDesiredSpecs(ctx, a.cfg.Store)
	if err != nil {
		return nil, fmt.Errorf("cluster.Build: load desired specs: %w", err)
	}

	eniByDpu := DefaultEniPlacementSource(includeEnis)(specs)

	cluster := buildClusterInfo(peers, a.cfg.Elector.LeaderID(), a.cfg.NodeID)
	appliances := groupByAppliance(dpus, eniByDpu, includeEnis)
	zones := summarizeZones(appliances)
	summary := summarize(peers, appliances)

	objects, err := a.countObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("cluster.Build: count objects: %w", err)
	}

	return &dashcenterv1.TopologyResponse{
		ComputedAt: timestamppb.Now(),
		Cluster:    cluster,
		Appliances: appliances,
		Zones:      zones,
		Summary:    summary,
		Objects:    objects,
	}, nil
}

// ── pure helpers (no IO, fully testable) ────────────────────────────────

func buildClusterInfo(peers []PeerInfo, leaderID, selfID string) *dashcenterv1.ClusterInfo {
	out := &dashcenterv1.ClusterInfo{
		Healthy:   true,
		LeaderId:  leaderID,
		NodeCount: int32(len(peers)),
		Nodes:     make([]*dashcenterv1.ClusterNodeInfo, 0, len(peers)),
	}
	for _, p := range peers {
		node := &dashcenterv1.ClusterNodeInfo{
			NodeId:    p.NodeID,
			RestAddr:  p.RESTAddr,
			GrpcAddr:  p.GRPCAddr,
			AdminAddr: p.AdminAddr,
			Version:   p.Version,
			BuildSha:  p.BuildSHA,
			IsLeader:  p.NodeID == leaderID && leaderID != "",
			Labels:    cloneLabels(p.Labels),
		}
		if !p.StartedAt.IsZero() {
			node.StartedAt = timestamppb.New(p.StartedAt)
		}
		out.Nodes = append(out.Nodes, node)
	}
	// Deterministic node ordering.
	sort.SliceStable(out.Nodes, func(i, j int) bool { return out.Nodes[i].NodeId < out.Nodes[j].NodeId })

	// Healthy when there's a leader AND at least one node. Operators
	// alert on this single bool; tighter health bits live elsewhere.
	if leaderID == "" || len(peers) == 0 {
		out.Healthy = false
	}
	return out
}

func cloneLabels(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// groupByAppliance builds the appliances list from the DPU inventory.
// Uses label "rack" as the appliance id (falling back to "unassigned").
// Slot is parsed from the "slot" label (best-effort).
func groupByAppliance(dpus []inventory.DpuEntry, eniByDpu map[string][]*dashcenterv1.EniTopInfo, includeEnis bool) []*dashcenterv1.ApplianceInfo {
	type appBuilder struct {
		id   string
		zone string
		tier string
		dpus []*dashcenterv1.DpuTopInfo
	}
	apps := map[string]*appBuilder{}

	for _, d := range dpus {
		appID := labelOr(d.Labels, "rack", "unassigned")
		slot, _ := strconv.Atoi(labelOr(d.Labels, "slot", "0"))
		zone := labelOr(d.Labels, "zone", "")
		tier := labelOr(d.Labels, "tier", "")

		dpuTop := &dashcenterv1.DpuTopInfo{
			Id:       d.ID,
			Slot:     int32(slot),
			State:    d.State.String(),
			Cordoned: d.Cordoned,
		}
		if !d.LastSeen.IsZero() {
			dpuTop.LastSeen = timestamppb.New(d.LastSeen)
		}
		enis := eniByDpu[d.ID]
		dpuTop.EniCount = int32(len(enis))
		if includeEnis {
			// Strip nil placeholders the count-only path leaves behind.
			filtered := enis[:0]
			for _, e := range enis {
				if e != nil {
					filtered = append(filtered, e)
				}
			}
			dpuTop.Enis = filtered
		}

		ab, ok := apps[appID]
		if !ok {
			ab = &appBuilder{id: appID, zone: zone, tier: tier}
			apps[appID] = ab
		} else {
			// First-non-empty wins for appliance-level labels.
			if ab.zone == "" {
				ab.zone = zone
			}
			if ab.tier == "" {
				ab.tier = tier
			}
		}
		ab.dpus = append(ab.dpus, dpuTop)
	}

	out := make([]*dashcenterv1.ApplianceInfo, 0, len(apps))
	for _, ab := range apps {
		sort.SliceStable(ab.dpus, func(i, j int) bool { return ab.dpus[i].Id < ab.dpus[j].Id })
		out = append(out, &dashcenterv1.ApplianceInfo{
			Id:   ab.id,
			Zone: ab.zone,
			Tier: ab.tier,
			Dpus: ab.dpus,
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return out
}

func labelOr(labels map[string]string, key, defaultVal string) string {
	if v, ok := labels[key]; ok && v != "" {
		return v
	}
	return defaultVal
}

func summarizeZones(apps []*dashcenterv1.ApplianceInfo) []*dashcenterv1.ZoneInfo {
	zm := map[string]*dashcenterv1.ZoneInfo{}
	for _, ap := range apps {
		zone := ap.Zone
		if zone == "" {
			zone = "unknown"
		}
		z, ok := zm[zone]
		if !ok {
			z = &dashcenterv1.ZoneInfo{Zone: zone}
			zm[zone] = z
		}
		z.ApplianceCount++
		z.DpuCount += int32(len(ap.Dpus))
		for _, d := range ap.Dpus {
			z.EniCount += d.EniCount
		}
	}
	out := make([]*dashcenterv1.ZoneInfo, 0, len(zm))
	for _, z := range zm {
		out = append(out, z)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Zone < out[j].Zone })
	return out
}

func summarize(peers []PeerInfo, apps []*dashcenterv1.ApplianceInfo) *dashcenterv1.TopologySummary {
	s := &dashcenterv1.TopologySummary{
		TotalNodes:      int32(len(peers)),
		TotalAppliances: int32(len(apps)),
	}
	const up = "DPU_STATE_UP"
	const down = "DPU_STATE_FAILED"
	const cord = "DPU_STATE_CORDONED"
	_ = cord
	for _, ap := range apps {
		s.TotalDpus += int32(len(ap.Dpus))
		for _, d := range ap.Dpus {
			s.TotalEnis += d.EniCount
			switch {
			case d.Cordoned:
				s.CordonedDpus++
			case d.State == up:
				s.HealthyDpus++
			case d.State == down:
				s.OfflineDpus++
			default:
				s.DegradedDpus++
			}
		}
	}
	return s
}

// countObjects sweeps the store across the configured namespaces and
// returns per-namespace per-kind counts. Errors from individual List
// calls degrade to zero counts for that (ns,kind) tuple — we don't fail
// the whole topology for a single missing prefix.
func (a *Aggregator) countObjects(ctx context.Context) (map[string]*dashcenterv1.NamespaceObjectCounts, error) {
	if a.cfg.Store == nil {
		return map[string]*dashcenterv1.NamespaceObjectCounts{}, nil
	}
	out := make(map[string]*dashcenterv1.NamespaceObjectCounts, len(a.cfg.Namespaces))
	for _, ns := range a.cfg.Namespaces {
		c := &dashcenterv1.NamespaceObjectCounts{}
		c.Vnets = listCount(ctx, a.cfg.Store, ns, "vnet")
		c.Enis = listCount(ctx, a.cfg.Store, ns, "eni")
		c.VnetMappings = listCount(ctx, a.cfg.Store, ns, "vnet_mapping")
		c.AclPolicies = listCount(ctx, a.cfg.Store, ns, "acl_policy")
		c.RoutePolicies = listCount(ctx, a.cfg.Store, ns, "route_policy")
		c.HaSets = listCount(ctx, a.cfg.Store, ns, "ha_set")
		c.ServiceTunnels = listCount(ctx, a.cfg.Store, ns, "service_tunnel")
		out[ns] = c
	}
	return out, nil
}

func listCount(ctx context.Context, st store.DesiredStore, ns, kind string) int32 {
	items, err := st.List(ctx, ns, kind)
	if err != nil {
		// Log via slog up at the caller? Keep this helper IO-pure for
		// readability; the missing count is the operator-visible
		// degradation signal.
		return 0
	}
	return int32(len(items))
}

// canonicalLeaderEvent emits a LEADER_CHANGED event payload. Helper so
// the broadcaster doesn't have to import timestamppb.
func canonicalLeaderEvent(oldID, newID string, ts time.Time) *dashcenterv1.TopologyEvent {
	return &dashcenterv1.TopologyEvent{
		Kind:        dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED,
		Ts:          timestamppb.New(ts),
		OldLeaderId: oldID,
		NewLeaderId: newID,
	}
}

// peerToNodeInfo translates a registry PeerInfo to the proto wire type.
// Exported via lower-case helper because the broadcaster uses it for
// per-peer events.
func peerToNodeInfo(p PeerInfo, leaderID string) *dashcenterv1.ClusterNodeInfo {
	out := &dashcenterv1.ClusterNodeInfo{
		NodeId:    p.NodeID,
		RestAddr:  p.RESTAddr,
		GrpcAddr:  p.GRPCAddr,
		AdminAddr: p.AdminAddr,
		Version:   p.Version,
		BuildSha:  p.BuildSHA,
		IsLeader:  p.NodeID == leaderID && leaderID != "",
		Labels:    cloneLabels(p.Labels),
	}
	if !p.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(p.StartedAt)
	}
	return out
}

// dpuEntryToTop translates an inventory entry into the proto wire type.
// Used by the broadcaster's DPU_* events.
func dpuEntryToTop(d inventory.DpuEntry) *dashcenterv1.DpuTopInfo {
	out := &dashcenterv1.DpuTopInfo{
		Id:       d.ID,
		Slot:     0,
		State:    d.State.String(),
		Cordoned: d.Cordoned,
	}
	if s, err := strconv.Atoi(strings.TrimSpace(d.Labels["slot"])); err == nil {
		out.Slot = int32(s)
	}
	if !d.LastSeen.IsZero() {
		out.LastSeen = timestamppb.New(d.LastSeen)
	}
	return out
}
