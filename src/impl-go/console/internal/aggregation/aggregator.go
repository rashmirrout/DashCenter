package aggregation

import (
"context"
"encoding/json"
"fmt"
"io"
"log/slog"
"net/http"
"strings"
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

// EniDetail handles GET /api/console/eni/{namespace}/{name}/detail.
//
// Fan-out (8 parallel goroutines, then a 9th serial fetch for the
// parent vnet once we know its name):
//
//	1. GET /v1/{ns}/enis/{name}        → identity (FATAL on 404)
//	2. GET /admin/eni-placement        → placement on DPUs (HA-aware)
//	3. GET /v1/{ns}/vnet-mappings      → all mappings, filtered later
//	4. GET /v1/{ns}/acl-policies       → all ACLs, filtered + split by stage
//	5. GET /v1/{ns}/route-policies     → all routes, filtered later
//	6. GET /v1/{ns}/service-tunnels    → all tunnels, kept for ref resolution
//	7. GET /v1/{ns}/ha                 → HaSets, attached if placement DPU is a member
//	8. GET /v1/{ns}/vnets/{vnet_name}  → parent Vnet + VNI (deferred until 1 returns)
//
// Only the ENI fetch is fatal (404 → 404, other errors → 502). Every
// other call is best-effort: on failure the corresponding array stays
// empty and a warning is appended so the UI can show partial data.
func (a *Aggregator) EniDetail(w http.ResponseWriter, r *http.Request) {
ns := chi.URLParam(r, "namespace")
name := chi.URLParam(r, "name")
if ns == "" || name == "" {
writeError(w, http.StatusBadRequest, "namespace and name path parameters are required")
return
}

ctx := r.Context()

type listResp struct {
Items []map[string]any `json:"items"`
}

var (
eniRaw       map[string]any
placementAll listResp
mappingsAll  listResp
aclsAll      listResp
routesAll    listResp
tunnelsAll   listResp
haSetsAll    listResp

eniErr, placementErr, mappingsErr error
aclsErr, routesErr, tunnelsErr, haSetsErr error
)

var wg sync.WaitGroup
wg.Add(7)

// 1. ENI identity (the only fatal fetch). Cannot fetch the vnet here
// because vnet_name is unknown until this returns — vnet is deferred
// to after wg.Wait().
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/"+ns+"/enis/"+name)
if err != nil {
eniErr = err
return
}
eniErr = json.Unmarshal(data, &eniRaw)
}()

// 2. Placement (admin endpoint).
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdAdminAddr, "/admin/eni-placement")
if err != nil {
placementErr = err
return
}
placementErr = json.Unmarshal(data, &placementAll)
}()

// 3. VnetMappings (full list, filter later).
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/"+ns+"/vnet-mappings")
if err != nil {
mappingsErr = err
return
}
mappingsErr = json.Unmarshal(data, &mappingsAll)
}()

// 4. ACL policies.
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/"+ns+"/acl-policies")
if err != nil {
aclsErr = err
return
}
aclsErr = json.Unmarshal(data, &aclsAll)
}()

// 5. Route policies.
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/"+ns+"/route-policies")
if err != nil {
routesErr = err
return
}
routesErr = json.Unmarshal(data, &routesAll)
}()

// 6. Service tunnels.
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/"+ns+"/service-tunnels")
if err != nil {
tunnelsErr = err
return
}
tunnelsErr = json.Unmarshal(data, &tunnelsAll)
}()

// 7. HA sets.
go func() {
defer wg.Done()
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr, "/v1/"+ns+"/ha")
if err != nil {
haSetsErr = err
return
}
haSetsErr = json.Unmarshal(data, &haSetsAll)
}()

wg.Wait()

// ── Fatal: ENI fetch failure ────────────────────────────────
if eniErr != nil {
// dashd returns 404 as a non-OK status which fetchJSON wraps
// as an error containing "returned 404". Surface as 404 so
// React Router can render a not-found state.
if strings.Contains(eniErr.Error(), "returned 404") {
writeError(w, http.StatusNotFound, "eni not found: "+ns+"/"+name)
return
}
a.logger.Error("eni detail: identity fetch failed",
"ns", ns, "name", name, "error", eniErr)
writeError(w, http.StatusBadGateway, "failed to fetch eni: "+eniErr.Error())
return
}

// ── Build identity (probe both top-level and spec shapes) ───
identity := EniIdentity{
VnetName:   stringField(eniRaw, "vnet_name"),
MacAddress: stringField(eniRaw, "mac_address"),
UnderlayIP: stringField(eniRaw, "underlay_ip"),
AdminState: stringField(eniRaw, "admin_state"),
Generation: int64Field(eniRaw, "generation"),
Labels:     stringMapField(eniRaw, "labels"),
}
if identity.VnetName == "" {
if spec, ok := eniRaw["spec"].(map[string]any); ok {
identity.VnetName = stringField(spec, "vnet_name")
identity.MacAddress = stringField(spec, "mac_address")
identity.UnderlayIP = stringField(spec, "underlay_ip")
identity.AdminState = stringField(spec, "admin_state")
if identity.Labels == nil {
identity.Labels = stringMapField(spec, "labels")
}
}
}

var warnings []string

// ── Deferred: fetch parent vnet now that we know vnet_name ──
var vnetSummary *VnetSummary
if identity.VnetName != "" {
data, err := a.fetchJSON(ctx, a.cfg.DashdRestAddr,
"/v1/"+ns+"/vnets/"+identity.VnetName)
if err != nil {
warnings = append(warnings, "vnet fetch failed: "+err.Error())
a.logger.Warn("eni detail: vnet fetch failed",
"vnet", identity.VnetName, "error", err)
} else {
var vnetRaw map[string]any
if err := json.Unmarshal(data, &vnetRaw); err != nil {
warnings = append(warnings, "vnet parse failed: "+err.Error())
} else {
vnetSummary = buildVnetSummary(vnetRaw)
}
}
}

// ── Build placement (with HA-active-active detection) ───────
placement := buildPlacementSummary(placementAll.Items, name)
if placementErr != nil {
warnings = append(warnings, "placement fetch failed: "+placementErr.Error())
}

// ── Filter ACLs by eni_names + split by stage ───────────────
aclsIn, aclsOut, aclWarn := filterAclsForEni(aclsAll.Items, name, ns)
if aclsErr != nil {
warnings = append(warnings, "acl-policies fetch failed: "+aclsErr.Error())
}
warnings = append(warnings, aclWarn...)

// ── Filter routes by eni_names ──────────────────────────────
myRoutes, routeWarn := filterRoutesForEni(routesAll.Items, name, ns)
if routesErr != nil {
warnings = append(warnings, "route-policies fetch failed: "+routesErr.Error())
}
warnings = append(warnings, routeWarn...)

// ── Filter vnet-mappings to this ENI's vnet ─────────────────
var myMappings []map[string]any
if identity.VnetName != "" {
for _, m := range mappingsAll.Items {
if mappingVnetName(m) == identity.VnetName {
myMappings = append(myMappings, m)
}
}
}
if mappingsErr != nil {
warnings = append(warnings, "vnet-mappings fetch failed: "+mappingsErr.Error())
}

// ── Derive overlay IP from this ENI's matching vnet-mapping ─
// dashd's data model puts the overlay address on the VnetMapping,
// not the ENI. Join on (underlay_ip + mac_address) so the UI can
// surface the ENI's overlay IP without a second round-trip.
if identity.UnderlayIP != "" && identity.MacAddress != "" {
identity.OverlayIP = overlayIPFromMappings(
myMappings, identity.UnderlayIP, identity.MacAddress)
}

// ── Resolve referenced service tunnels ──────────────────────
referencedTunnels := referencedTunnelNames(myRoutes, myMappings)
var myTunnels []map[string]any
for _, t := range tunnelsAll.Items {
tn := resourceName(t)
if _, ok := referencedTunnels[tn]; ok {
myTunnels = append(myTunnels, t)
}
}
if tunnelsErr != nil {
warnings = append(warnings, "service-tunnels fetch failed: "+tunnelsErr.Error())
}

// ── Attach HaSet if any placement DPU is a member ───────────
haSet := findHaSetForDpus(haSetsAll.Items, placement.DpuIDs)
if haSetsErr != nil {
warnings = append(warnings, "ha-sets fetch failed: "+haSetsErr.Error())
}

// ── Counters ────────────────────────────────────────────────
counters := EniDetailCounters{
AclInbound:  len(aclsIn),
AclOutbound: len(aclsOut),
Routes:      len(myRoutes),
Mappings:    len(myMappings),
Tunnels:     len(myTunnels),
Placements:  len(placement.DpuIDs),
}

resp := EniDetail{
Namespace:             ns,
Name:                  name,
Identity:              identity,
Vnet:                  vnetSummary,
Placement:             placement,
HaSet:                 haSet,
VnetMappingsReachable: nonNil(myMappings),
AclsInbound:           nonNil(aclsIn),
AclsOutbound:          nonNil(aclsOut),
RoutePolicies:         nonNil(myRoutes),
ServiceTunnels:        nonNil(myTunnels),
Counters:              counters,
Warnings:              warnings,
}

writeJSON(w, http.StatusOK, resp)
}

// ── EniDetail helpers (pure, no I/O) ───────────────────────

// stringField extracts a string field from a JSON-decoded map, returning
// "" when the key is missing or not a string.
func stringField(m map[string]any, key string) string {
if v, ok := m[key].(string); ok {
return v
}
return ""
}

// int64Field extracts a numeric field as int64. JSON numbers decode into
// float64 by default; we coerce safely.
func int64Field(m map[string]any, key string) int64 {
switch v := m[key].(type) {
case float64:
return int64(v)
case int64:
return v
case int:
return int64(v)
}
return 0
}

// stringMapField extracts a map[string]string from a JSON-decoded value.
// Returns nil when missing so json `omitempty` drops the key.
func stringMapField(m map[string]any, key string) map[string]string {
raw, ok := m[key].(map[string]any)
if !ok || len(raw) == 0 {
return nil
}
out := make(map[string]string, len(raw))
for k, v := range raw {
if s, ok := v.(string); ok {
out[k] = s
}
}
if len(out) == 0 {
return nil
}
return out
}

// stringSliceField extracts a []string from a JSON-decoded slice of any.
func stringSliceField(m map[string]any, key string) []string {
raw, ok := m[key].([]any)
if !ok {
return nil
}
out := make([]string, 0, len(raw))
for _, v := range raw {
if s, ok := v.(string); ok {
out = append(out, s)
}
}
return out
}

// resourceName returns the bare name of a dashd resource regardless of
// whether the wire shape is { "name": "..." } or
// { "metadata": { "name": "..." } }.
func resourceName(r map[string]any) string {
if s, ok := r["name"].(string); ok && s != "" {
return s
}
if md, ok := r["metadata"].(map[string]any); ok {
if s, ok := md["name"].(string); ok {
return s
}
}
return ""
}

// resourceNamespace returns the namespace, handling both top-level and
// metadata-nested shapes.
func resourceNamespace(r map[string]any) string {
if s, ok := r["namespace"].(string); ok && s != "" {
return s
}
if md, ok := r["metadata"].(map[string]any); ok {
if s, ok := md["namespace"].(string); ok {
return s
}
}
return ""
}

// buildVnetSummary projects a Vnet wire object into the VnetSummary
// the UI consumes. Handles both top-level and spec-nested shapes.
func buildVnetSummary(raw map[string]any) *VnetSummary {
if raw == nil {
return nil
}
src := raw
if spec, ok := raw["spec"].(map[string]any); ok {
src = spec
}
return &VnetSummary{
Name:  firstNonEmpty(stringField(raw, "name"), stringField(src, "name")),
VNI:   int(int64Field(src, "vni")),
GwMac: stringField(src, "gw_mac"),
State: stringField(src, "state"),
}
}

// firstNonEmpty returns the first non-empty string from its arguments.
func firstNonEmpty(vals ...string) string {
for _, v := range vals {
if v != "" {
return v
}
}
return ""
}

// buildPlacementSummary walks the eni-placement list and projects all
// entries that match eniName into an EniPlacementSummary. Supports
// both single-DPU (legacy `dpu_id`) and multi-DPU HA (`placements[]`).
func buildPlacementSummary(items []map[string]any, eniName string) EniPlacementSummary {
out := EniPlacementSummary{}
seen := make(map[string]bool)

for _, p := range items {
// The placement entry may use `name` (current) or `eni_name` (legacy).
n := stringField(p, "name")
if n == "" {
n = stringField(p, "eni_name")
}
if n != eniName {
continue
}

// Single-DPU shape
if d := stringField(p, "dpu_id"); d != "" && !seen[d] {
out.DpuIDs = append(out.DpuIDs, d)
out.Slots = append(out.Slots, EniPlacementSlot{DpuID: d, Observed: true})
seen[d] = true
}
// Multi-DPU (HA) shape
if slots, ok := p["placements"].([]any); ok {
for _, s := range slots {
sm, ok := s.(map[string]any)
if !ok {
continue
}
d := stringField(sm, "dpu_id")
if d == "" || seen[d] {
continue
}
observed := true
if o, ok := sm["observed"].(bool); ok {
observed = o
}
out.DpuIDs = append(out.DpuIDs, d)
out.Slots = append(out.Slots, EniPlacementSlot{DpuID: d, Observed: observed})
seen[d] = true
}
}
}

out.HaActiveActive = len(out.DpuIDs) > 1
return out
}

// filterAclsForEni walks acl-policies and returns two slices: those
// whose eni_names contains eniName for the inbound stage and for the
// outbound stage. Warnings are emitted for cross-namespace references
// (rare but possible — dashd doesn't enforce same-namespace today).
func filterAclsForEni(items []map[string]any, eniName, ns string) (in, out []map[string]any, warns []string) {
for _, acl := range items {
// eni_names may live at the top level or inside spec.
names := stringSliceField(acl, "eni_names")
if len(names) == 0 {
if spec, ok := acl["spec"].(map[string]any); ok {
names = stringSliceField(spec, "eni_names")
}
}
matches := false
for _, n := range names {
if n == eniName {
matches = true
break
}
}
if !matches {
continue
}

// Cross-namespace sanity warning (best-effort; doesn't block).
if aclNs := resourceNamespace(acl); aclNs != "" && aclNs != ns {
warns = append(warns, "acl-policy "+resourceName(acl)+
" lives in namespace "+aclNs+" but references this eni — unsupported")
}

stage := stringField(acl, "stage")
if stage == "" {
if spec, ok := acl["spec"].(map[string]any); ok {
stage = stringField(spec, "stage")
}
}
switch stage {
case "inbound":
in = append(in, acl)
case "outbound":
out = append(out, acl)
default:
// Unknown stage — surface under inbound and warn (legacy data).
warns = append(warns, "acl-policy "+resourceName(acl)+
" has unknown stage "+stage+", showing under inbound")
in = append(in, acl)
}
}
return in, out, warns
}

// filterRoutesForEni walks route-policies and returns those whose
// eni_names contains eniName. Cross-namespace refs warned.
func filterRoutesForEni(items []map[string]any, eniName, ns string) (out []map[string]any, warns []string) {
for _, rp := range items {
names := stringSliceField(rp, "eni_names")
if len(names) == 0 {
if spec, ok := rp["spec"].(map[string]any); ok {
names = stringSliceField(spec, "eni_names")
}
}
matches := false
for _, n := range names {
if n == eniName {
matches = true
break
}
}
if !matches {
continue
}
if rpNs := resourceNamespace(rp); rpNs != "" && rpNs != ns {
warns = append(warns, "route-policy "+resourceName(rp)+
" lives in namespace "+rpNs+" but references this eni — unsupported")
}
out = append(out, rp)
}
return out, warns
}

// mappingVnetName returns the vnet_name of a vnet-mapping, handling
// both top-level and spec-nested shapes.
func mappingVnetName(m map[string]any) string {
if v := stringField(m, "vnet_name"); v != "" {
return v
}
if spec, ok := m["spec"].(map[string]any); ok {
return stringField(spec, "vnet_name")
}
return ""
}

// overlayIPFromMappings walks the already-filtered vnet-mappings and
// returns the overlay IP (mapping.ip_address) for the entry whose
// (underlay_ip + mac_address) tuple matches the ENI. Returns "" when
// no mapping matches — the UI is expected to show a blank/placeholder
// in that case rather than fail the whole detail render.
//
// Mappings are searched in spec-nested first then top-level so the
// helper works with both wire shapes.
func overlayIPFromMappings(mappings []map[string]any, underlayIP, mac string) string {
if underlayIP == "" || mac == "" {
return ""
}
wantMac := strings.ToLower(mac)
for _, m := range mappings {
src := m
if spec, ok := m["spec"].(map[string]any); ok {
src = spec
}
mUnderlay := stringField(src, "underlay_ip")
if mUnderlay == "" {
mUnderlay = stringField(m, "underlay_ip")
}
mMac := stringField(src, "mac_address")
if mMac == "" {
mMac = stringField(m, "mac_address")
}
if mUnderlay != underlayIP {
continue
}
if strings.ToLower(mMac) != wantMac {
continue
}
// dashd uses `ip_address`; legacy `overlay_ip` alias also accepted.
if ip := stringField(src, "ip_address"); ip != "" {
return ip
}
if ip := stringField(m, "ip_address"); ip != "" {
return ip
}
if ip := stringField(src, "overlay_ip"); ip != "" {
return ip
}
if ip := stringField(m, "overlay_ip"); ip != "" {
return ip
}
}
return ""
}

// referencedTunnelNames walks the ENI's routes and mappings and collects
// the set of service-tunnel names they point at. Used to filter the full
// tunnel list down to those actually reachable from this ENI.
func referencedTunnelNames(routes, mappings []map[string]any) map[string]struct{} {
out := make(map[string]struct{})
add := func(name string) {
if name != "" {
out[name] = struct{}{}
}
}

for _, rp := range routes {
src := rp
if spec, ok := rp["spec"].(map[string]any); ok {
src = spec
}
// dashd uses `routes` (current) — fall back to `rules`.
entries, ok := src["routes"].([]any)
if !ok {
entries, _ = src["rules"].([]any)
}
for _, e := range entries {
em, ok := e.(map[string]any)
if !ok {
continue
}
if stringField(em, "next_hop_type") == "service_tunnel" {
add(stringField(em, "next_hop_target"))
}
if members, ok := em["ecmp_members"].([]any); ok {
for _, mAny := range members {
mm, ok := mAny.(map[string]any)
if !ok {
continue
}
if stringField(mm, "next_hop_type") == "service_tunnel" {
add(stringField(mm, "next_hop_target"))
}
}
}
}
}

for _, mp := range mappings {
src := mp
if spec, ok := mp["spec"].(map[string]any); ok {
src = spec
}
if stringField(src, "action") == "service_tunnel" {
if params, ok := src["params"].(map[string]any); ok {
add(stringField(params, "tunnel"))
}
}
}

return out
}

// findHaSetForDpus returns the first HaSet that has at least one member
// in dpuIDs, or nil. We also stamp MemberDpuIDs / MembersByRole so the
// UI can render the full set with peer chips.
func findHaSetForDpus(items []map[string]any, dpuIDs []string) *HaSetSummary {
if len(dpuIDs) == 0 || len(items) == 0 {
return nil
}
inSet := make(map[string]bool, len(dpuIDs))
for _, d := range dpuIDs {
inSet[d] = true
}

for _, h := range items {
src := h
if spec, ok := h["spec"].(map[string]any); ok {
src = spec
}
members, ok := src["members"].([]any)
if !ok {
continue
}

var memberIDs []string
roleMap := make(map[string]string)
hit := false
for _, mAny := range members {
mm, ok := mAny.(map[string]any)
if !ok {
continue
}
id := stringField(mm, "dpu_id")
if id == "" {
continue
}
memberIDs = append(memberIDs, id)
role := stringField(mm, "role")
if role != "" {
roleMap[id] = role
}
if inSet[id] {
hit = true
}
}
if !hit {
continue
}

if len(roleMap) == 0 {
roleMap = nil
}
return &HaSetSummary{
Name:          resourceName(h),
Scope:         stringField(src, "scope"),
VirtualIP:     stringField(src, "virtual_ip"),
MemberDpuIDs:  memberIDs,
MembersByRole: roleMap,
}
}
return nil
}

// nonNil returns the input slice if non-nil, else an empty slice so
// JSON serialization emits `[]` instead of `null`. Keeps the TS type
// strict (no `| null` everywhere).
func nonNil(s []map[string]any) []map[string]any {
if s == nil {
return []map[string]any{}
}
return s
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