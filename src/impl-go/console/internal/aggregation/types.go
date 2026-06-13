// Package aggregation implements BFF aggregation endpoints that
// fan out to multiple dashd APIs, merge results, and return
// pre-computed view models for the SPA.
package aggregation

import "time"

// FleetSummary is the response for GET /api/console/fleet/summary.
// Merges data from /admin/health + /admin/inventory + /v1/*/vnets + /v1/*/enis.
type FleetSummary struct {
Timestamp      time.Time          `json:"timestamp"`
ClusterHealthy bool               `json:"cluster_healthy"`
LeaderNode     string             `json:"leader_node"`
IsLeader       bool               `json:"is_leader"`
DpuCount       int                `json:"dpu_count"`
DpusByState    map[string]int     `json:"dpus_by_state"`
EniCount       int                `json:"eni_count"`
VnetCount      int                `json:"vnet_count"`
DriftedDpus    []string           `json:"drifted_dpus,omitempty"`
OfflineDpus    []string           `json:"offline_dpus,omitempty"`
Dpus           []DpuSummary       `json:"dpus"`
}

// DpuSummary is a per-DPU entry in FleetSummary.
type DpuSummary struct {
ID       string `json:"id"`
State    string `json:"state"`
LastSeen string `json:"last_seen"`
}

// DpuDetail is the response for GET /api/console/dpu/{dpuId}/detail.
// Merges data from /admin/health + /admin/drift + /admin/observed +
// /v1/*/enis + /v1/*/acl-policies + /v1/*/route-policies.
type DpuDetail struct {
ID         string      `json:"id"`
State      string      `json:"state"`
LastSeen   string      `json:"last_seen"`
DriftCount int         `json:"drift_count"`
DriftItems []DriftItem `json:"drift_items,omitempty"`
Enis       []EniInfo   `json:"enis"`
}

// DriftItem is a single declared-vs-observed mismatch.
type DriftItem struct {
DpuID string `json:"dpu_id"`
Op    string `json:"op"`
Kind  string `json:"kind"`
Key   string `json:"key"`
}

// EniInfo is a per-ENI summary within a DPU or Vnet detail.
type EniInfo struct {
Name       string `json:"name"`
VnetName   string `json:"vnet_name"`
MacAddress string `json:"mac_address,omitempty"`
UnderlayIP string `json:"underlay_ip,omitempty"`
AdminState string `json:"admin_state,omitempty"`
}

// TopologyGraph is the response for GET /api/console/topology.
// Pre-computed React Flow-compatible graph data.
type TopologyGraph struct {
Nodes []TopologyNode `json:"nodes"`
Edges []TopologyEdge `json:"edges"`
}

// TopologyNode represents a DPU, ENI, or Vnet in the topology graph.
type TopologyNode struct {
ID       string         `json:"id"`
Type     string         `json:"type"` // "dpu" | "eni" | "vnet"
Label    string         `json:"label"`
State    string         `json:"state,omitempty"`
ParentID string         `json:"parent_id,omitempty"`
Data     map[string]any `json:"data,omitempty"`
}

// TopologyEdge represents a connection between nodes.
type TopologyEdge struct {
ID     string `json:"id"`
Source string `json:"source"`
Target string `json:"target"`
Type   string `json:"type,omitempty"` // "placement" | "vnet_membership"
Label  string `json:"label,omitempty"`
}

// VnetDetail is the response for GET /api/console/vnet/{name}/detail.
type VnetDetail struct {
Name      string    `json:"name"`
VNI       int       `json:"vni"`
Namespace string    `json:"namespace"`
EniCount  int       `json:"eni_count"`
Enis      []EniInfo `json:"enis"`
}

// VnetCanvasData is the response for GET /api/console/vnet/{name}/canvas.
// Pre-computed canvas model for the dual-plane Vnet visualization.
type VnetCanvasData struct {
Vnet    VnetCanvasVnet    `json:"vnet"`
Dpus    []CanvasDpu       `json:"dpus"`
Tunnels []CanvasTunnel    `json:"tunnels"`
Enis    []CanvasEni       `json:"enis"`
}

// VnetCanvasVnet is the Vnet summary within the canvas data.
type VnetCanvasVnet struct {
Name      string `json:"name"`
VNI       int    `json:"vni"`
Namespace string `json:"namespace"`
EniCount  int    `json:"eni_count"`
}

// CanvasDpu represents a DPU in the Vnet canvas.
type CanvasDpu struct {
ID         string   `json:"id"`
State      string   `json:"state"`
UnderlayIP string   `json:"underlay_ip"`
EniIDs     []string `json:"eni_ids"`
}

// CanvasTunnel represents a tunnel in the Vnet canvas.
type CanvasTunnel struct {
ID              string `json:"id"`
Name            string `json:"name"`
SrcDpuID        string `json:"src_dpu_id"`
DstDpuID        string `json:"dst_dpu_id"`
LocalUnderlayIP string `json:"local_underlay_ip"`
RemoteUnderlayIP string `json:"remote_underlay_ip"`
VNI             int    `json:"vni"`
State           string `json:"state"`
}

// CanvasEni represents an ENI in the Vnet canvas.
type CanvasEni struct {
ID         string `json:"id"`
Name       string `json:"name"`
MacAddress string `json:"mac_address"`
UnderlayIP string `json:"underlay_ip"`
DpuID      string `json:"dpu_id"`
AdminState string `json:"admin_state"`
}

// ── Service Topology types ──────────────────────────────────

// ServiceTopologyResponse is the response for GET /api/console/service-topology.
// Merges cluster health fan-out + inventory + eni-placement into a
// hierarchical view: cluster → appliances → DPUs → ENIs.
type ServiceTopologyResponse struct {
	Timestamp  time.Time        `json:"timestamp"`
	Cluster    ClusterInfo      `json:"cluster"`
	Appliances []ApplianceInfo  `json:"appliances"`
	Zones      []ZoneInfo       `json:"zones"`
	Summary    TopologySummary  `json:"summary"`
}

// ClusterInfo describes the dashd controller cluster.
type ClusterInfo struct {
	Healthy   bool              `json:"healthy"`
	LeaderID  string            `json:"leader_id"`
	NodeCount int               `json:"node_count"`
	Nodes     []ClusterNodeInfo `json:"nodes"`
}

// ClusterNodeInfo is a single dashd controller node.
type ClusterNodeInfo struct {
	Addr       string `json:"addr"`
	NodeID     string `json:"node_id"`
	Status     string `json:"status"`     // "ok" | "degraded" | "unreachable"
	IsLeader   bool   `json:"is_leader"`
	LeaderID   string `json:"leader_id"`
	DpuCount   int    `json:"dpu_count"`
	Latency    string `json:"latency_ms"` // round-trip fetch time
}

// ApplianceInfo groups DPUs that share the same appliance (rack label).
type ApplianceInfo struct {
	ID     string       `json:"id"`
	Zone   string       `json:"zone,omitempty"`
	Tier   string       `json:"tier,omitempty"`
	Dpus   []DpuTopInfo `json:"dpus"`
}

// DpuTopInfo is a per-DPU entry within an appliance for the topology view.
type DpuTopInfo struct {
	ID         string        `json:"id"`
	Slot       int           `json:"slot"`
	State      string        `json:"state"`
	LastSeen   string        `json:"last_seen,omitempty"`
	EniCount   int           `json:"eni_count"`
	Enis       []EniTopInfo  `json:"enis,omitempty"`
}

// EniTopInfo is a per-ENI entry within a DPU for the topology view.
type EniTopInfo struct {
	Name       string `json:"name"`
	VnetName   string `json:"vnet_name,omitempty"`
	MacAddress string `json:"mac_address,omitempty"`
	AdminState string `json:"admin_state,omitempty"`
}

// ZoneInfo aggregates counts per availability zone.
type ZoneInfo struct {
	Zone           string `json:"zone"`
	ApplianceCount int    `json:"appliance_count"`
	DpuCount       int    `json:"dpu_count"`
	EniCount       int    `json:"eni_count"`
}

// TopologySummary provides fleet-wide rollup numbers.
type TopologySummary struct {
	TotalNodes      int `json:"total_nodes"`
	TotalAppliances int `json:"total_appliances"`
	TotalDpus       int `json:"total_dpus"`
	TotalEnis       int `json:"total_enis"`
	HealthyDpus     int `json:"healthy_dpus"`
	DegradedDpus    int `json:"degraded_dpus"`
	OfflineDpus     int `json:"offline_dpus"`
}

// CapacityStats is the response for GET /api/console/stats/capacity.
type CapacityStats struct {
Timestamp time.Time       `json:"timestamp"`
Fleet     FleetCapacity   `json:"fleet"`
PerDpu    []DpuCapacity   `json:"per_dpu"`
}

// FleetCapacity is the aggregated fleet-level capacity.
type FleetCapacity struct {
TotalDpus       int `json:"total_dpus"`
TotalEnis       int `json:"total_enis"`
TotalRoutes     int `json:"total_routes"`
TotalAclRules   int `json:"total_acl_rules"`
TotalFlows      int `json:"total_flows"`
}

// DpuCapacity is per-DPU capacity usage.
type DpuCapacity struct {
ID         string  `json:"id"`
State      string  `json:"state"`
EnisUsed   int     `json:"enis_used"`
EnisMax    int     `json:"enis_max"`
EnisPct    float64 `json:"enis_pct"`
RoutesUsed int     `json:"routes_used"`
RoutesMax  int     `json:"routes_max"`
RoutesPct  float64 `json:"routes_pct"`
}

// ── EniDetail (Phase A) ─────────────────────────────────────
//
// EniDetail is the response for
//   GET /api/console/eni/{namespace}/{name}/detail.
//
// It is a comprehensive, pre-joined view of a single ENI built by
// fanning out 8 calls to dashd in parallel and merging the results.
// All cross-resource joins live server-side so the SPA renders a
// single fetch into a "single comprehensive page" for debugging.
//
// Source endpoints fanned out by EniDetail():
//   1. GET /v1/{ns}/enis/{name}              → Identity / 404 short-circuit
//   2. GET /admin/eni-placement              → DPU placement (HA-aware)
//   3. GET /v1/{ns}/vnets/{vnet_name}        → Parent Vnet + VNI
//   4. GET /v1/{ns}/vnet-mappings            → Filter to this ENI's vnet
//   5. GET /v1/{ns}/acl-policies             → Filter eni_names + split by stage
//   6. GET /v1/{ns}/route-policies           → Filter eni_names
//   7. GET /v1/{ns}/service-tunnels          → Resolve refs from routes/mappings
//   8. GET /v1/{ns}/ha                       → Membership of placement DPU(s)
//
// All fields use snake_case for JSON to stay consistent with the
// rest of the dashw BFF responses.
type EniDetail struct {
Namespace string                `json:"namespace"`
Name      string                `json:"name"`
Identity  EniIdentity           `json:"identity"`
Vnet      *VnetSummary          `json:"vnet,omitempty"`
Placement EniPlacementSummary   `json:"placement"`
HaSet     *HaSetSummary         `json:"ha_set,omitempty"`

VnetMappingsReachable []map[string]any `json:"vnet_mappings_reachable"`
AclsInbound           []map[string]any `json:"acls_inbound"`
AclsOutbound          []map[string]any `json:"acls_outbound"`
RoutePolicies         []map[string]any `json:"route_policies"`
ServiceTunnels        []map[string]any `json:"service_tunnels"`

Counters EniDetailCounters `json:"counters"`
Warnings []string          `json:"warnings,omitempty"`
}

// EniIdentity is the ENI's own spec, projected to the fields the
// UI cares about. We keep raw `labels` for the chip cluster and
// `generation` so the UI can detect stale data after writes.
type EniIdentity struct {
VnetName   string            `json:"vnet_name"`
MacAddress string            `json:"mac_address,omitempty"`
UnderlayIP string            `json:"underlay_ip,omitempty"`
AdminState string            `json:"admin_state,omitempty"`
Generation int64             `json:"generation,omitempty"`
Labels     map[string]string `json:"labels,omitempty"`
}

// VnetSummary is the parent-Vnet projection, with VNI prominently
// surfaced so the UI can render it as a first-class chip. The
// concepts doc (docs/concepts/dashd-configuration-concepts.md)
// explains why VNI lives on the Vnet and is inherited by every
// ENI in it — this struct is the wire-level expression of that
// inheritance.
type VnetSummary struct {
Name  string `json:"name"`
VNI   int    `json:"vni"`
GwMac string `json:"gw_mac,omitempty"`
State string `json:"state,omitempty"`
}

// EniPlacementSummary describes where this ENI currently lives.
// HaActiveActive is true iff DpuIDs has more than one entry
// (i.e. the ENI is present on two DPUs simultaneously per HA).
type EniPlacementSummary struct {
DpuIDs         []string             `json:"dpu_ids"`
HaActiveActive bool                 `json:"ha_active_active"`
Slots          []EniPlacementSlot   `json:"slots,omitempty"`
}

// EniPlacementSlot mirrors the per-slot record from
// /admin/eni-placement so the UI can show observed vs declared.
type EniPlacementSlot struct {
DpuID    string `json:"dpu_id"`
Observed bool   `json:"observed"`
}

// HaSetSummary is attached when at least one placement DPU is a
// member of an HaSet. The MemberDpuIDs slice is the union of all
// member DPUs across matching sets (in practice there is at most
// one match per ENI).
type HaSetSummary struct {
Name          string            `json:"name"`
Scope         string            `json:"scope,omitempty"`
VirtualIP     string            `json:"virtual_ip,omitempty"`
MemberDpuIDs  []string          `json:"member_dpu_ids"`
MembersByRole map[string]string `json:"members_by_role,omitempty"`
}

// EniDetailCounters provides the at-a-glance numbers that the
// Overview tab renders without having to count arrays itself.
type EniDetailCounters struct {
AclInbound   int `json:"acl_inbound"`
AclOutbound  int `json:"acl_outbound"`
Routes       int `json:"routes"`
Mappings     int `json:"mappings"`
Tunnels      int `json:"tunnels"`
Placements   int `json:"placements"`
RuleHits     int `json:"rule_hits,omitempty"` // reserved for Phase B counters
}
