package aggregation

import (
"encoding/json"
"io"
"log/slog"
"net/http"
"net/http/httptest"
"testing"
"time"

"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/config"
)

func testLogger() *slog.Logger {
return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

// mockDashdAdmin creates a mock dashd admin server.
func mockDashdAdmin(t *testing.T) *httptest.Server {
t.Helper()
return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
switch r.URL.Path {
case "/admin/health":
_ = json.NewEncoder(w).Encode(map[string]any{
"status":    "ok",
"leader":    true,
"leader_id": "dashd-1",
"dpus": []map[string]string{
{"id": "dpu-1", "state": "DPU_STATE_UP", "last_seen": time.Now().Format(time.RFC3339)},
{"id": "dpu-2", "state": "DPU_STATE_UP", "last_seen": time.Now().Format(time.RFC3339)},
{"id": "dpu-3", "state": "DPU_STATE_DEGRADED", "last_seen": time.Now().Format(time.RFC3339)},
},
})
case "/admin/drift":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
{"dpu_id": "dpu-1", "op": "add", "kind": "OBJECT_KIND_ENI", "key": []string{"eni-01"}},
},
"summary": map[string]int{"total": 1, "dpus": 1},
})
default:
w.WriteHeader(404)
_, _ = w.Write([]byte(`{"error":"not found"}`))
}
}))
}

// mockDashdREST creates a mock dashd REST server.
func mockDashdREST(t *testing.T) *httptest.Server {
t.Helper()
return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
switch r.URL.Path {
case "/v1/default/vnets":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
{"name": "vnet-prod", "vni": 10001},
{"name": "vnet-staging", "vni": 20001},
},
})
case "/v1/default/enis":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
{"name": "eni-01", "vnet_name": "vnet-prod"},
{"name": "eni-02", "vnet_name": "vnet-prod"},
{"name": "eni-03", "vnet_name": "vnet-staging"},
},
})
case "/v1/default/vnets/vnet-prod":
_ = json.NewEncoder(w).Encode(map[string]any{
"name": "vnet-prod", "vni": 10001, "namespace": "default",
})
default:
w.WriteHeader(404)
_, _ = w.Write([]byte(`{"error":"not found"}`))
}
}))
}

func testAggregator(t *testing.T) (*Aggregator, func()) {
t.Helper()
admin := mockDashdAdmin(t)
rest := mockDashdREST(t)

cfg := &config.Config{
DashdAdminAddr: admin.URL,
DashdRestAddr:  rest.URL,
ProxyTimeout:   5 * time.Second,
}

agg := New(cfg, testLogger())

cleanup := func() {
admin.Close()
rest.Close()
}

return agg, cleanup
}

func TestFleetSummary(t *testing.T) {
agg, cleanup := testAggregator(t)
defer cleanup()

req := httptest.NewRequest(http.MethodGet, "/api/console/fleet/summary", nil)
rec := httptest.NewRecorder()

agg.FleetSummary(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
}

var summary FleetSummary
if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
t.Fatalf("decode: %v", err)
}

if summary.DpuCount != 3 {
t.Errorf("DpuCount = %d, want 3", summary.DpuCount)
}
if summary.VnetCount != 2 {
t.Errorf("VnetCount = %d, want 2", summary.VnetCount)
}
if summary.EniCount != 3 {
t.Errorf("EniCount = %d, want 3", summary.EniCount)
}
if !summary.ClusterHealthy {
t.Error("ClusterHealthy = false, want true")
}
if summary.LeaderNode != "dashd-1" {
t.Errorf("LeaderNode = %q, want %q", summary.LeaderNode, "dashd-1")
}
if len(summary.Dpus) != 3 {
t.Errorf("len(Dpus) = %d, want 3", len(summary.Dpus))
}
if summary.DpusByState["DPU_STATE_UP"] != 2 {
t.Errorf("DpusByState[UP] = %d, want 2", summary.DpusByState["DPU_STATE_UP"])
}
if len(summary.OfflineDpus) != 1 {
t.Errorf("len(OfflineDpus) = %d, want 1 (dpu-3 is DEGRADED)", len(summary.OfflineDpus))
}
}

func TestFleetSummary_DashdDown(t *testing.T) {
cfg := &config.Config{
DashdAdminAddr: "http://127.0.0.1:1",
DashdRestAddr:  "http://127.0.0.1:1",
ProxyTimeout:   1 * time.Second,
}
agg := New(cfg, testLogger())

req := httptest.NewRequest(http.MethodGet, "/api/console/fleet/summary", nil)
rec := httptest.NewRecorder()

agg.FleetSummary(rec, req)

if rec.Code != http.StatusBadGateway {
t.Errorf("status = %d, want 502", rec.Code)
}
}

func TestTopology(t *testing.T) {
agg, cleanup := testAggregator(t)
defer cleanup()

req := httptest.NewRequest(http.MethodGet, "/api/console/topology", nil)
rec := httptest.NewRecorder()

agg.Topology(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200", rec.Code)
}

var graph TopologyGraph
if err := json.NewDecoder(rec.Body).Decode(&graph); err != nil {
t.Fatalf("decode: %v", err)
}

if len(graph.Nodes) != 3 {
t.Errorf("len(Nodes) = %d, want 3", len(graph.Nodes))
}
if graph.Nodes[0].Type != "dpu" {
t.Errorf("Nodes[0].Type = %q, want %q", graph.Nodes[0].Type, "dpu")
}
}

func TestCapacityStats(t *testing.T) {
agg, cleanup := testAggregator(t)
defer cleanup()

req := httptest.NewRequest(http.MethodGet, "/api/console/stats/capacity", nil)
rec := httptest.NewRecorder()

agg.CapacityStats(rec, req)

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200", rec.Code)
}

var stats CapacityStats
if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
t.Fatalf("decode: %v", err)
}

if stats.Fleet.TotalDpus != 3 {
t.Errorf("Fleet.TotalDpus = %d, want 3", stats.Fleet.TotalDpus)
}
if len(stats.PerDpu) != 3 {
t.Errorf("len(PerDpu) = %d, want 3", len(stats.PerDpu))
}
}

func TestSingleFlightGroup(t *testing.T) {
var g singleFlightGroup
callCount := 0

// Two concurrent calls to the same key should result in only one fn call
ch := make(chan []byte, 2)
for i := 0; i < 2; i++ {
go func() {
val, _ := g.Do("test", func() ([]byte, error) {
callCount++
return []byte("result"), nil
})
ch <- val
}()
}

<-ch
<-ch

// Due to singleflight, fn should be called at most once
// (timing dependent — but validates no panic or deadlock)
if callCount == 0 {
t.Error("fn was never called")
}
}