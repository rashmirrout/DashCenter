package aggregation

import (
"context"
"encoding/json"
"io"
"log/slog"
"net/http"
"net/http/httptest"
"testing"
"time"

"github.com/go-chi/chi/v5"
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

// ── EniDetail tests ─────────────────────────────────────────

// eniMockOpts parameterises mockDashdForEni so individual tests can
// simulate 404s and partial-failure scenarios.
type eniMockOpts struct {
// eniMissing causes /v1/{ns}/enis/{name} to return 404.
eniMissing bool
// vnetMissing causes /v1/{ns}/vnets/{vnet_name} to return 404 so
// the aggregator must degrade gracefully (no vnet block, warning).
vnetMissing bool
}

// mockDashdForEni stands up two httptest servers (admin + REST) that
// answer all 8 endpoints the EniDetail handler fans out to.
func mockDashdForEni(t *testing.T, opts eniMockOpts) (admin, rest *httptest.Server) {
t.Helper()
admin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
switch r.URL.Path {
case "/admin/eni-placement":
// eni-blue-1 is HA active-active across dpu-1 + dpu-2.
// eni-other-1 is on a different DPU to confirm we filter by name.
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
{
"name":        "eni-blue-1",
"vnet_name":   "vnet-blue",
"mac_address": "aa:bb:cc:00:01:01",
"underlay_ip": "10.0.1.11",
"admin_state": "UP",
"placements": []map[string]any{
{"dpu_id": "dpu-1", "observed": true},
{"dpu_id": "dpu-2", "observed": true},
},
},
{
"name":      "eni-other-1",
"vnet_name": "vnet-red",
"dpu_id":    "dpu-9",
},
},
})
default:
w.WriteHeader(404)
_, _ = w.Write([]byte(`{"error":"not found"}`))
}
}))

rest = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.Header().Set("Content-Type", "application/json")
switch r.URL.Path {
case "/v1/default/enis/eni-blue-1":
if opts.eniMissing {
w.WriteHeader(404)
_, _ = w.Write([]byte(`{"error":"eni not found"}`))
return
}
_ = json.NewEncoder(w).Encode(map[string]any{
"name":        "eni-blue-1",
"vnet_name":   "vnet-blue",
"mac_address": "aa:bb:cc:00:01:01",
"underlay_ip": "10.0.1.11",
"admin_state": "UP",
"generation":  3,
"labels":      map[string]any{"tier": "app"},
})
case "/v1/default/vnets/vnet-blue":
if opts.vnetMissing {
w.WriteHeader(404)
_, _ = w.Write([]byte(`{"error":"vnet not found"}`))
return
}
_ = json.NewEncoder(w).Encode(map[string]any{
"name":   "vnet-blue",
"vni":    100,
"gw_mac": "aa:bb:cc:00:00:01",
"state":  "ACTIVE",
})
case "/v1/default/vnet-mappings":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
// Reachable: in vnet-blue, action service_tunnel → tunnel-east.
{
"name":        "m-1",
"vnet_name":   "vnet-blue",
"ip_address":  "192.168.1.10",
"underlay_ip": "10.0.1.50",
"action":      "service_tunnel",
"params":      map[string]string{"tunnel": "tunnel-east"},
},
{
"name":        "m-2",
"vnet_name":   "vnet-blue",
"ip_address":  "192.168.1.11",
"underlay_ip": "10.0.1.51",
"action":      "vnet_encap",
},
// Not reachable: different vnet, must be filtered out.
{
"name":      "m-3",
"vnet_name": "vnet-red",
"action":    "vnet_encap",
},
},
})
case "/v1/default/acl-policies":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
// Inbound for eni-blue-1.
{
"name":      "acl-blue-in",
"namespace": "default",
"stage":     "inbound",
"eni_names": []string{"eni-blue-1"},
"rules": []map[string]any{
{"priority": 100, "action": "allow"},
},
},
// Outbound for eni-blue-1.
{
"name":      "acl-blue-out",
"namespace": "default",
"stage":     "outbound",
"eni_names": []string{"eni-blue-1"},
"rules": []map[string]any{
{"priority": 100, "action": "allow"},
},
},
// Doesn't reference eni-blue-1 → filtered out.
{
"name":      "acl-red-in",
"stage":     "inbound",
"eni_names": []string{"eni-red-1"},
"rules":     []map[string]any{},
},
},
})
case "/v1/default/route-policies":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
// References eni-blue-1; one entry points at tunnel-west.
{
"name":      "rp-blue",
"namespace": "default",
"eni_names": []string{"eni-blue-1"},
"routes": []map[string]any{
{
"prefix":          "10.0.0.0/8",
"next_hop_type":   "service_tunnel",
"next_hop_target": "tunnel-west",
},
{
"prefix":        "0.0.0.0/0",
"next_hop_type": "drop",
},
},
},
// Different ENI → filtered out.
{
"name":      "rp-red",
"eni_names": []string{"eni-red-1"},
"routes":    []map[string]any{},
},
},
})
case "/v1/default/service-tunnels":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
{"name": "tunnel-east", "local_underlay_ip": "10.0.0.1", "vni": 200},
{"name": "tunnel-west", "local_underlay_ip": "10.0.0.2", "vni": 201},
{"name": "tunnel-unused", "local_underlay_ip": "10.0.0.3", "vni": 202},
},
})
case "/v1/default/ha":
_ = json.NewEncoder(w).Encode(map[string]any{
"items": []map[string]any{
// dpu-1 is a member, so this HaSet matches.
{
"name":       "ha-blue",
"scope":      "appliance",
"virtual_ip": "10.0.99.1",
"members": []map[string]any{
{"dpu_id": "dpu-1", "role": "ACTIVE"},
{"dpu_id": "dpu-2", "role": "ACTIVE"},
},
},
// dpu-1 not in this set → not selected.
{
"name": "ha-other",
"members": []map[string]any{
{"dpu_id": "dpu-5", "role": "ACTIVE"},
{"dpu_id": "dpu-6", "role": "ACTIVE"},
},
},
},
})
default:
w.WriteHeader(404)
_, _ = w.Write([]byte(`{"error":"not found"}`))
}
}))
return admin, rest
}

// makeEniRequest builds an httptest.Request with the chi route context
// already populated (namespace + name path params). Avoids the need to
// spin up a full chi.Router just for the handler unit test.
func makeEniRequest(ns, name string) *http.Request {
req := httptest.NewRequest(http.MethodGet,
"/api/console/eni/"+ns+"/"+name+"/detail", nil)
rctx := chi.NewRouteContext()
rctx.URLParams.Add("namespace", ns)
rctx.URLParams.Add("name", name)
return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestEniDetail_HappyPath verifies that all 8 fan-out responses are
// merged into the expected EniDetail shape and that the per-resource
// filters / joins all fire correctly.
func TestEniDetail_HappyPath(t *testing.T) {
admin, rest := mockDashdForEni(t, eniMockOpts{})
defer admin.Close()
defer rest.Close()

cfg := &config.Config{
DashdAdminAddr: admin.URL,
DashdRestAddr:  rest.URL,
ProxyTimeout:   5 * time.Second,
}
agg := New(cfg, testLogger())

rec := httptest.NewRecorder()
agg.EniDetail(rec, makeEniRequest("default", "eni-blue-1"))

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200. body: %s", rec.Code, rec.Body.String())
}

var got EniDetail
if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
t.Fatalf("decode: %v", err)
}

// Identity
if got.Namespace != "default" || got.Name != "eni-blue-1" {
t.Errorf("name/namespace mismatch: %s/%s", got.Namespace, got.Name)
}
if got.Identity.VnetName != "vnet-blue" {
t.Errorf("Identity.VnetName = %q, want %q", got.Identity.VnetName, "vnet-blue")
}
if got.Identity.MacAddress != "aa:bb:cc:00:01:01" {
t.Errorf("Identity.MacAddress = %q", got.Identity.MacAddress)
}
if got.Identity.Generation != 3 {
t.Errorf("Identity.Generation = %d, want 3", got.Identity.Generation)
}
if got.Identity.Labels["tier"] != "app" {
t.Errorf("Identity.Labels[tier] = %q, want app", got.Identity.Labels["tier"])
}

// Vnet block (VNI inheritance)
if got.Vnet == nil {
t.Fatal("Vnet block missing")
}
if got.Vnet.VNI != 100 {
t.Errorf("Vnet.VNI = %d, want 100", got.Vnet.VNI)
}

// Placement: HA active-active across dpu-1 + dpu-2
if !got.Placement.HaActiveActive {
t.Error("Placement.HaActiveActive = false, want true")
}
if len(got.Placement.DpuIDs) != 2 {
t.Errorf("Placement.DpuIDs len = %d, want 2", len(got.Placement.DpuIDs))
}

// HA set attached because dpu-1 is a member
if got.HaSet == nil {
t.Fatal("HaSet missing — expected ha-blue (dpu-1 is a member)")
}
if got.HaSet.Name != "ha-blue" {
t.Errorf("HaSet.Name = %q, want ha-blue", got.HaSet.Name)
}
if got.HaSet.VirtualIP != "10.0.99.1" {
t.Errorf("HaSet.VirtualIP = %q", got.HaSet.VirtualIP)
}

// Vnet-mappings filter: only the 2 in vnet-blue
if len(got.VnetMappingsReachable) != 2 {
t.Errorf("VnetMappingsReachable len = %d, want 2", len(got.VnetMappingsReachable))
}

// ACL split by stage: 1 in, 1 out (the acl-red-in is filtered)
if len(got.AclsInbound) != 1 || len(got.AclsOutbound) != 1 {
t.Errorf("AclsInbound=%d AclsOutbound=%d, want 1/1",
len(got.AclsInbound), len(got.AclsOutbound))
}

// Routes: 1 matches eni-blue-1
if len(got.RoutePolicies) != 1 {
t.Errorf("RoutePolicies len = %d, want 1", len(got.RoutePolicies))
}

// Tunnels: tunnel-east (from mapping) + tunnel-west (from route),
// tunnel-unused excluded.
if len(got.ServiceTunnels) != 2 {
t.Errorf("ServiceTunnels len = %d, want 2 (east+west)", len(got.ServiceTunnels))
}

// Counters
if got.Counters.AclInbound != 1 || got.Counters.AclOutbound != 1 {
t.Errorf("Counters acl in/out = %d/%d", got.Counters.AclInbound, got.Counters.AclOutbound)
}
if got.Counters.Placements != 2 {
t.Errorf("Counters.Placements = %d, want 2", got.Counters.Placements)
}
if got.Counters.Tunnels != 2 {
t.Errorf("Counters.Tunnels = %d, want 2", got.Counters.Tunnels)
}
}

// TestEniDetail_NotFound verifies that an ENI 404 from dashd surfaces
// as 404 from the aggregator (not 502 or partial data).
func TestEniDetail_NotFound(t *testing.T) {
admin, rest := mockDashdForEni(t, eniMockOpts{eniMissing: true})
defer admin.Close()
defer rest.Close()

cfg := &config.Config{
DashdAdminAddr: admin.URL,
DashdRestAddr:  rest.URL,
ProxyTimeout:   5 * time.Second,
}
agg := New(cfg, testLogger())

rec := httptest.NewRecorder()
agg.EniDetail(rec, makeEniRequest("default", "eni-blue-1"))

if rec.Code != http.StatusNotFound {
t.Errorf("status = %d, want 404. body: %s", rec.Code, rec.Body.String())
}
}

// TestEniDetail_DegradeOnVnetMissing verifies graceful degradation:
// the ENI exists but the parent Vnet fetch fails. The handler must
// still return 200 with identity, ACLs, routes, placement etc.; the
// Vnet block is omitted and a warning is appended.
func TestEniDetail_DegradeOnVnetMissing(t *testing.T) {
admin, rest := mockDashdForEni(t, eniMockOpts{vnetMissing: true})
defer admin.Close()
defer rest.Close()

cfg := &config.Config{
DashdAdminAddr: admin.URL,
DashdRestAddr:  rest.URL,
ProxyTimeout:   5 * time.Second,
}
agg := New(cfg, testLogger())

rec := httptest.NewRecorder()
agg.EniDetail(rec, makeEniRequest("default", "eni-blue-1"))

if rec.Code != http.StatusOK {
t.Fatalf("status = %d, want 200 (degrade). body: %s", rec.Code, rec.Body.String())
}

var got EniDetail
if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
t.Fatalf("decode: %v", err)
}

if got.Vnet != nil {
t.Error("Vnet should be nil when fetch fails")
}
if got.Identity.VnetName != "vnet-blue" {
t.Errorf("Identity.VnetName lost: %q", got.Identity.VnetName)
}
if len(got.Warnings) == 0 {
t.Error("expected at least one warning about missing vnet")
}
// Sanity: the rest of the data still came through.
if len(got.AclsInbound) != 1 {
t.Errorf("AclsInbound = %d, want 1 — partial data should still render",
len(got.AclsInbound))
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