package aggregation

import (
"context"
"encoding/json"
"fmt"
"io"
"log/slog"
"net/http"
"sync"
"time"

"github.com/go-chi/chi/v5"
"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

// Aggregator provides HTTP handlers for BFF aggregation endpoints.
// Each handler fans out to multiple dashd APIs in parallel, merges
// results, and returns a pre-computed view model.
type Aggregator struct {
cfg        *config.Config
logger     *slog.Logger
httpClient *http.Client
sf         singleFlightGroup
}

// singleFlightGroup prevents duplicate concurrent requests to dashd.
// Uses a simple sync.Map of in-flight channels.
type singleFlightGroup struct {
mu   sync.Mutex
calls map[string]*call
}

type call struct {
wg  sync.WaitGroup
val []byte
err error
}

func (g *singleFlightGroup) Do(key string, fn func() ([]byte, error)) ([]byte, error) {
g.mu.Lock()
if g.calls == nil {
g.calls = make(map[string]*call)
}
if c, ok := g.calls[key]; ok {
g.mu.Unlock()
c.wg.Wait()
return c.val, c.err
}
c := &call{}
c.wg.Add(1)
g.calls[key] = c
g.mu.Unlock()

c.val, c.err = fn()
c.wg.Done()

g.mu.Lock()
delete(g.calls, key)
g.mu.Unlock()

return c.val, c.err
}

// New creates an Aggregator.
func New(cfg *config.Config, logger *slog.Logger) *Aggregator {
return &Aggregator{
cfg:    cfg,
logger: logger,
httpClient: &http.Client{
Timeout: cfg.ProxyTimeout,
},
}
}

// fetchJSON performs a GET request to dashd and returns the raw body.
func (a *Aggregator) fetchJSON(ctx context.Context, baseURL, path string) ([]byte, error) {
url := baseURL + path
req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
if err != nil {
return nil, fmt.Errorf("build request %s: %w", url, err)
}

resp, err := a.httpClient.Do(req)
if err != nil {
return nil, fmt.Errorf("fetch %s: %w", url, err)
}
defer resp.Body.Close()

body, err := io.ReadAll(resp.Body)
if err != nil {
return nil, fmt.Errorf("read %s: %w", url, err)
}

if resp.StatusCode != http.StatusOK {
return nil, fmt.Errorf("%s returned %d: %s", url, resp.StatusCode, string(body[:min(len(body), 200)]))
}

return body, nil
}

func min(a, b int) int {
if a < b {
return a
}
return b
}

// FleetSummary handles GET /api/console/fleet/summary.
// Fan-out: /admin/health + /v1/default/vnets + /v1/default/enis
func (a *Aggregator) FleetSummary(w http.ResponseWriter, r *http.Request) {
ctx := r.Context()

type healthResp struct {
Status   string `json:"status"`
Leader   bool   `json:"leader"`
LeaderID string `json:"leader_id"`
Dpus     []struct {
ID       string `json:"id"`
State    string `json:"state"`
LastSeen string `json:"last_seen"`
} `json:"dpus"`
}

type listResp struct {
Items []json.RawMessage `json:"items"`
}

var (
health healthResp
vnets  listResp
enis   listResp
errs   [3]error
wg     sync.WaitGroup
)

wg.Add(3)

go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/health")
if err != nil {
errs[0] = err
return
}
errs[0] = json.Unmarshal(data, &health)
}()

go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/default/vnets")
if err != nil {
errs[1] = err
return
}
errs[1] = json.Unmarshal(data, &vnets)
}()

go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/default/enis")
if err != nil {
errs[2] = err
return
}
errs[2] = json.Unmarshal(data, &enis)
}()

wg.Wait()

// Check for critical errors (health is required; vnets/enis may be empty)
if errs[0] != nil {
a.logger.Error("fleet summary: health fetch failed", "error", errs[0])
writeError(w, http.StatusBadGateway, "failed to fetch fleet health: "+errs[0].Error())
return
}

// Build response
summary := FleetSummary{
Timestamp:      time.Now().UTC(),
ClusterHealthy: health.Status == "ok",
LeaderNode:     health.LeaderID,
IsLeader:       health.Leader,
DpuCount:       len(health.Dpus),
DpusByState:    make(map[string]int),
EniCount:       len(enis.Items),
VnetCount:      len(vnets.Items),
Dpus:           make([]DpuSummary, 0, len(health.Dpus)),
}

for _, dpu := range health.Dpus {
summary.DpusByState[dpu.State]++
summary.Dpus = append(summary.Dpus, DpuSummary{
ID:       dpu.ID,
State:    dpu.State,
LastSeen: dpu.LastSeen,
})

// Track offline DPUs
if dpu.State != "DPU_STATE_UP" && dpu.State != "DPU_STATE_REGISTERING" {
summary.OfflineDpus = append(summary.OfflineDpus, dpu.ID)
}
}

writeJSON(w, http.StatusOK, summary)
}

// DpuDetail handles GET /api/console/dpu/{dpuId}/detail.
func (a *Aggregator) DpuDetail(w http.ResponseWriter, r *http.Request) {
dpuID := chi.URLParam(r, "dpuId")
if dpuID == "" {
writeError(w, http.StatusBadRequest, "dpuId path parameter is required")
return
}

ctx := r.Context()

type healthResp struct {
Dpus []struct {
ID       string `json:"id"`
State    string `json:"state"`
LastSeen string `json:"last_seen"`
} `json:"dpus"`
}

type driftResp struct {
Items []struct {
DpuID string `json:"dpu_id"`
Op    string `json:"op"`
Kind  string `json:"kind"`
Key   []string `json:"key"`
} `json:"items"`
}

var (
health healthResp
drift  driftResp
errs   [2]error
wg     sync.WaitGroup
)

wg.Add(2)

go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/health")
if err != nil { errs[0] = err; return }
errs[0] = json.Unmarshal(data, &health)
}()

go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/drift?dpu="+dpuID)
if err != nil { errs[1] = err; return }
errs[1] = json.Unmarshal(data, &drift)
}()

wg.Wait()

if errs[0] != nil {
writeError(w, http.StatusBadGateway, "failed to fetch health: "+errs[0].Error())
return
}

detail := DpuDetail{ID: dpuID}

for _, dpu := range health.Dpus {
if dpu.ID == dpuID {
detail.State = dpu.State
detail.LastSeen = dpu.LastSeen
break
}
}

if errs[1] == nil {
for _, item := range drift.Items {
key := ""
if len(item.Key) > 0 { key = item.Key[0] }
detail.DriftItems = append(detail.DriftItems, DriftItem{
DpuID: item.DpuID, Op: item.Op,
Kind: item.Kind, Key: key,
})
}
detail.DriftCount = len(detail.DriftItems)
}

writeJSON(w, http.StatusOK, detail)
}

// Topology handles GET /api/console/topology.
func (a *Aggregator) Topology(w http.ResponseWriter, r *http.Request) {
ctx := r.Context()

type healthResp struct {
Dpus []struct {
ID    string `json:"id"`
State string `json:"state"`
} `json:"dpus"`
}

data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/health")
if err != nil {
writeError(w, http.StatusBadGateway, "failed to fetch health: "+err.Error())
return
}

var health healthResp
if err := json.Unmarshal(data, &health); err != nil {
writeError(w, http.StatusInternalServerError, "parse health: "+err.Error())
return
}

graph := TopologyGraph{
Nodes: make([]TopologyNode, 0, len(health.Dpus)),
Edges: make([]TopologyEdge, 0),
}

for _, dpu := range health.Dpus {
graph.Nodes = append(graph.Nodes, TopologyNode{
ID:    "dpu-" + dpu.ID,
Type:  "dpu",
Label: dpu.ID,
State: dpu.State,
})
}

writeJSON(w, http.StatusOK, graph)
}

// VnetDetail handles GET /api/console/vnet/{vnetName}/detail.
func (a *Aggregator) VnetDetail(w http.ResponseWriter, r *http.Request) {
vnetName := chi.URLParam(r, "vnetName")
if vnetName == "" {
writeError(w, http.StatusBadRequest, "vnetName path parameter is required")
return
}

ctx := r.Context()

// Fetch vnet spec
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/default/vnets/"+vnetName)
if err != nil {
writeError(w, http.StatusBadGateway, "failed to fetch vnet: "+err.Error())
return
}

var vnet struct {
Name string `json:"name"`
VNI  int    `json:"vni"`
Namespace string `json:"namespace"`
}
if err := json.Unmarshal(data, &vnet); err != nil {
writeError(w, http.StatusInternalServerError, "parse vnet: "+err.Error())
return
}

detail := VnetDetail{
Name:      vnet.Name,
VNI:       vnet.VNI,
Namespace: vnet.Namespace,
}

writeJSON(w, http.StatusOK, detail)
}

// CapacityStats handles GET /api/console/stats/capacity.
func (a *Aggregator) CapacityStats(w http.ResponseWriter, r *http.Request) {
ctx := r.Context()

type healthResp struct {
Dpus []struct {
ID    string `json:"id"`
State string `json:"state"`
} `json:"dpus"`
}

data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/health")
if err != nil {
writeError(w, http.StatusBadGateway, "failed to fetch health: "+err.Error())
return
}

var health healthResp
if err := json.Unmarshal(data, &health); err != nil {
writeError(w, http.StatusInternalServerError, "parse health: "+err.Error())
return
}

stats := CapacityStats{
Timestamp: time.Now().UTC(),
Fleet: FleetCapacity{
TotalDpus: len(health.Dpus),
},
PerDpu: make([]DpuCapacity, 0, len(health.Dpus)),
}

for _, dpu := range health.Dpus {
stats.PerDpu = append(stats.PerDpu, DpuCapacity{
ID:    dpu.ID,
State: dpu.State,
})
}

writeJSON(w, http.StatusOK, stats)
}

// ServiceTopology handles GET /api/console/service-topology.
// Fan-out: cluster health (all nodes) + inventory + eni-placement.
func (a *Aggregator) ServiceTopology(w http.ResponseWriter, r *http.Request) {
ctx := r.Context()

type nodeHealthResp struct {
Status   string `json:"status"`
Leader   bool   `json:"leader"`
LeaderID string `json:"leader_id"`
Dpus     []struct {
ID       string `json:"id"`
State    string `json:"state"`
LastSeen string `json:"last_seen"`
} `json:"dpus"`
}

type inventoryItem struct {
Identity struct {
DpuID       string `json:"dpu_id"`
ApplianceID string `json:"appliance_id"`
Slot        int    `json:"slot"`
} `json:"identity"`
Zone   string            `json:"zone"`
Tier   string            `json:"tier"`
Labels map[string]string `json:"labels"`
}

type eniPlacementItem struct {
Name       string `json:"name"`
VnetName   string `json:"vnet_name"`
MacAddress string `json:"mac_address"`
AdminState string `json:"admin_state"`
DpuID      string `json:"dpu_id"`
Placements []struct {
DpuID string `json:"dpu_id"`
} `json:"placements"`
}

addrs := a.cfg.DashdClusterAddrs
nodeCount := len(addrs)

// --- Fan-out: cluster health per node ---
type nodeResult struct {
addr    string
health  nodeHealthResp
latency time.Duration
err     error
}

nodeResults := make([]nodeResult, nodeCount)
var wg sync.WaitGroup

// Fan out to each cluster node
for i, addr := range addrs {
wg.Add(1)
go func(idx int, nodeAddr string) {
defer wg.Done()
start := time.Now()
data, err := a.fetchJSON(ctx, nodeAddr, "/admin/health")
elapsed := time.Since(start)
nodeResults[idx].addr = nodeAddr
nodeResults[idx].latency = elapsed
if err != nil {
nodeResults[idx].err = err
return
}
nodeResults[idx].err = json.Unmarshal(data, &nodeResults[idx].health)
}(i, addr)
}

// Fan out: inventory
var inventory []inventoryItem
var inventoryErr error
wg.Add(1)
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/inventory")
if err != nil {
inventoryErr = err
return
}
var resp struct {
Items []inventoryItem `json:"items"`
}
if err := json.Unmarshal(data, &resp); err != nil {
// Try as raw array
inventoryErr = json.Unmarshal(data, &inventory)
return
}
inventory = resp.Items
}()

// Fan out: eni-placement
var eniPlacements []eniPlacementItem
var eniErr error
wg.Add(1)
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/eni-placement")
if err != nil {
eniErr = err
return
}
var resp struct {
Items []eniPlacementItem `json:"items"`
}
if err := json.Unmarshal(data, &resp); err != nil {
// Try as raw array
eniErr = json.Unmarshal(data, &eniPlacements)
return
}
eniPlacements = resp.Items
}()

wg.Wait()

// --- Build cluster info ---
cluster := ClusterInfo{
Healthy:   true,
NodeCount: nodeCount,
Nodes:     make([]ClusterNodeInfo, 0, nodeCount),
}

// Collect all DPU states from the first successful node
dpuStateMap := make(map[string]string)    // dpuID → state
dpuLastSeen := make(map[string]string)    // dpuID → last_seen

// Use leader election: the leader_id that appears most wins
leaderVotes := make(map[string]int)

for i, nr := range nodeResults {
node := ClusterNodeInfo{
Addr:    nr.addr,
NodeID:  fmt.Sprintf("node-%d", i+1),
Latency: fmt.Sprintf("%.1f", float64(nr.latency.Microseconds())/1000.0),
}

if nr.err != nil {
node.Status = "unreachable"
cluster.Healthy = false
a.logger.Warn("service-topology: node unreachable",
"addr", nr.addr, "error", nr.err)
} else {
node.Status = nr.health.Status
node.IsLeader = nr.health.Leader
node.LeaderID = nr.health.LeaderID
node.DpuCount = len(nr.health.Dpus)

if nr.health.LeaderID != "" {
leaderVotes[nr.health.LeaderID]++
}

// Collect DPU states from this node
for _, dpu := range nr.health.Dpus {
dpuStateMap[dpu.ID] = dpu.State
dpuLastSeen[dpu.ID] = dpu.LastSeen
}
}

cluster.Nodes = append(cluster.Nodes, node)
}

// Determine leader — most-voted leader_id
bestLeader := ""
bestVotes := 0
for lid, votes := range leaderVotes {
if votes > bestVotes {
bestLeader = lid
bestVotes = votes
}
}
cluster.LeaderID = bestLeader

// Mark which node is the real leader based on consensus
for i := range cluster.Nodes {
if cluster.Nodes[i].LeaderID == bestLeader && cluster.Nodes[i].IsLeader {
// This node claims to be leader AND its leader_id matches consensus
cluster.Nodes[i].IsLeader = true
} else {
cluster.Nodes[i].IsLeader = false
}
}

// --- Build inventory lookup ---
type invEntry struct {
applianceID string
slot        int
zone        string
tier        string
}
invMap := make(map[string]invEntry) // dpuID → invEntry
if inventoryErr != nil {
a.logger.Warn("service-topology: inventory fetch failed", "error", inventoryErr)
} else {
for _, item := range inventory {
invMap[item.Identity.DpuID] = invEntry{
applianceID: item.Identity.ApplianceID,
slot:        item.Identity.Slot,
zone:        item.Zone,
tier:        item.Tier,
}
// Also check labels for rack/zone/tier if top-level fields are empty
if invMap[item.Identity.DpuID].applianceID == "" && item.Labels != nil {
e := invMap[item.Identity.DpuID]
if v, ok := item.Labels["rack"]; ok {
e.applianceID = v
}
if v, ok := item.Labels["zone"]; ok {
e.zone = v
}
if v, ok := item.Labels["tier"]; ok {
e.tier = v
}
invMap[item.Identity.DpuID] = e
}
}
}

// --- Build ENI lookup: dpuID → []EniTopInfo ---
eniByDpu := make(map[string][]EniTopInfo)
if eniErr != nil {
a.logger.Warn("service-topology: eni-placement fetch failed", "error", eniErr)
} else {
for _, ep := range eniPlacements {
dpuID := ep.DpuID
if dpuID == "" && len(ep.Placements) > 0 {
dpuID = ep.Placements[0].DpuID
}
if dpuID == "" {
continue
}
eniByDpu[dpuID] = append(eniByDpu[dpuID], EniTopInfo{
Name:       ep.Name,
VnetName:   ep.VnetName,
MacAddress: ep.MacAddress,
AdminState: ep.AdminState,
})
}
}

// --- Group DPUs into appliances ---
type appBuilder struct {
zone string
tier string
dpus map[string]*DpuTopInfo
}
appMap := make(map[string]*appBuilder) // applianceID → builder

for dpuID, state := range dpuStateMap {
inv, hasInv := invMap[dpuID]
appID := "unassigned"
slot := 0
zone := ""
tier := ""
if hasInv {
appID = inv.applianceID
if appID == "" {
appID = "unassigned"
}
slot = inv.slot
zone = inv.zone
tier = inv.tier
}

ab, ok := appMap[appID]
if !ok {
ab = &appBuilder{zone: zone, tier: tier, dpus: make(map[string]*DpuTopInfo)}
appMap[appID] = ab
}

enis := eniByDpu[dpuID]
ab.dpus[dpuID] = &DpuTopInfo{
ID:       dpuID,
Slot:     slot,
State:    state,
LastSeen: dpuLastSeen[dpuID],
EniCount: len(enis),
Enis:     enis,
}
}

// Convert to sorted slice
appliances := make([]ApplianceInfo, 0, len(appMap))
for appID, ab := range appMap {
dpus := make([]DpuTopInfo, 0, len(ab.dpus))
for _, d := range ab.dpus {
dpus = append(dpus, *d)
}
appliances = append(appliances, ApplianceInfo{
ID:   appID,
Zone: ab.zone,
Tier: ab.tier,
Dpus: dpus,
})
}

// --- Build zone summary ---
zoneMap := make(map[string]*ZoneInfo)
for _, app := range appliances {
z := app.Zone
if z == "" {
z = "unknown"
}
zi, ok := zoneMap[z]
if !ok {
zi = &ZoneInfo{Zone: z}
zoneMap[z] = zi
}
zi.ApplianceCount++
zi.DpuCount += len(app.Dpus)
for _, d := range app.Dpus {
zi.EniCount += d.EniCount
}
}
zones := make([]ZoneInfo, 0, len(zoneMap))
for _, zi := range zoneMap {
zones = append(zones, *zi)
}

// --- Build summary ---
totalEnis := 0
healthyDpus := 0
degradedDpus := 0
offlineDpus := 0
for _, state := range dpuStateMap {
switch state {
case "DPU_STATE_UP":
healthyDpus++
case "DPU_STATE_REGISTERING", "DPU_STATE_RECONCILING":
degradedDpus++
default:
offlineDpus++
}
}
for _, enis := range eniByDpu {
totalEnis += len(enis)
}

resp := ServiceTopologyResponse{
Timestamp:  time.Now().UTC(),
Cluster:    cluster,
Appliances: appliances,
Zones:      zones,
Summary: TopologySummary{
TotalNodes:      nodeCount,
TotalAppliances: len(appliances),
TotalDpus:       len(dpuStateMap),
TotalEnis:       totalEnis,
HealthyDpus:     healthyDpus,
DegradedDpus:    degradedDpus,
OfflineDpus:     offlineDpus,
},
}

writeJSON(w, http.StatusOK, resp)
}

// writeJSON marshals v as JSON and writes it.
func writeJSON(w http.ResponseWriter, status int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}