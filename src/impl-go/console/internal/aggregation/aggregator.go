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