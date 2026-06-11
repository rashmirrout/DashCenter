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