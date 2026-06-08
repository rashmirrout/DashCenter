//go:build integration

package integration

import (
"encoding/json"
"strings"
"testing"
"time"
)

// 1. Daemon starts with no specs and converges to drift=empty.
func TestIntegration_DaemonStartsClean(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

// Wait for the worker's first reconcile pass to declare in-sync.
h.waitConverged(t, 30*time.Second)

code, body := httpDo(t, "GET", h.adminURL+"/admin/health", "")
if code != 200 || !strings.Contains(string(body), `"status"`) {
t.Fatalf("health unexpected: %d %s", code, body)
}
}

// 2. PUT VNet via REST converges (sim observed reflects it).
func TestIntegration_PutVnet_Converges_REST(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

code, body := httpDo(t, "PUT", h.restURL+"/v1/vnets/v1",
`{"name":"v1","vni":100}`)
if code >= 400 {
t.Fatalf("PUT vnet: %d %s", code, body)
}
h.waitConverged(t, 30*time.Second)

// Read back via REST GET.
code, body = httpDo(t, "GET", h.restURL+"/v1/vnets/v1", "")
if code != 200 {
t.Fatalf("GET vnet: %d %s", code, body)
}
if !strings.Contains(string(body), `"v1"`) {
t.Errorf("response missing v1: %s", body)
}
}

// 3. PUT ENI via REST converges and is visible in /admin/eni-placement.
func TestIntegration_PutEni_Converges_REST(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

httpDo(t, "PUT", h.restURL+"/v1/vnets/v1", `{"name":"v1","vni":100}`)
httpDo(t, "PUT", h.restURL+"/v1/enis/e1",
`{"name":"e1","vnet_name":"v1","mac_address":"aa:bb:cc:dd:ee:01",`+
`"underlay_ip":"10.1.1.1","admin_state":"enabled",`+
`"placement_hint_dpu_ids":["`+h.dpuID+`"]}`)

h.waitConverged(t, 30*time.Second)

code, body := httpDo(t, "GET", h.adminURL+"/admin/eni-placement?eni=e1", "")
if code != 200 {
t.Fatalf("eni-placement: %d %s", code, body)
}
if !strings.Contains(string(body), `"e1"`) {
t.Errorf("e1 not in placement: %s", body)
}
// Observed flag should flip to true once subscribe pump receives the
// CREATED event from dash-sim.
if !strings.Contains(string(body), `"observed":true`) {
t.Errorf("e1 not observed yet: %s", body)
}
}

// 4. Edit an ENI (change MAC) and reconverge.
func TestIntegration_EditEni_Reconverges(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

httpDo(t, "PUT", h.restURL+"/v1/vnets/v1", `{"name":"v1","vni":100}`)
httpDo(t, "PUT", h.restURL+"/v1/enis/e1",
`{"name":"e1","vnet_name":"v1","mac_address":"aa:bb:cc:dd:ee:01",`+
`"underlay_ip":"10.1.1.1","admin_state":"enabled",`+
`"placement_hint_dpu_ids":["`+h.dpuID+`"]}`)
h.waitConverged(t, 30*time.Second)

// Update MAC.
code, body := httpDo(t, "PUT", h.restURL+"/v1/enis/e1",
`{"name":"e1","vnet_name":"v1","mac_address":"aa:bb:cc:dd:ee:99",`+
`"underlay_ip":"10.1.1.1","admin_state":"enabled",`+
`"placement_hint_dpu_ids":["`+h.dpuID+`"]}`)
if code >= 400 {
t.Fatalf("PUT eni update: %d %s", code, body)
}
h.waitConverged(t, 30*time.Second)
}

// 5. DELETE an ENI and verify reconciliation removes it from the sim.
func TestIntegration_DeleteEni_Reconciles(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

httpDo(t, "PUT", h.restURL+"/v1/vnets/v1", `{"name":"v1","vni":100}`)
httpDo(t, "PUT", h.restURL+"/v1/enis/e1",
`{"name":"e1","vnet_name":"v1","mac_address":"aa:bb:cc:dd:ee:01",`+
`"underlay_ip":"10.1.1.1","admin_state":"enabled",`+
`"placement_hint_dpu_ids":["`+h.dpuID+`"]}`)
h.waitConverged(t, 30*time.Second)

code, body := httpDo(t, "DELETE", h.restURL+"/v1/enis/e1", "")
if code >= 400 {
t.Fatalf("DELETE eni: %d %s", code, body)
}
h.waitConverged(t, 30*time.Second)

// GET should 404 now.
code, _ = httpDo(t, "GET", h.restURL+"/v1/enis/e1", "")
if code != 404 {
t.Errorf("GET deleted eni: code=%d want 404", code)
}
}

// 6. Persisted state survives a daemon restart (file-store backed).
func TestIntegration_RestartPersistsState(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

httpDo(t, "PUT", h.restURL+"/v1/vnets/v1", `{"name":"v1","vni":100}`)
h.waitConverged(t, 30*time.Second)

// Kill dashd; restart it pointing at the same state dir.
killProc(h.dashd)
h.dashd = nil
h.startDashd(t)
if err := waitHTTP(h.adminURL+"/admin/health", 30*time.Second); err != nil {
t.Fatalf("dashd restart unhealthy: %v", err)
}

// VNet must still be listed via REST.
code, body := httpDo(t, "GET", h.restURL+"/v1/vnets", "")
if code != 200 {
t.Fatalf("GET vnets after restart: %d %s", code, body)
}
if !strings.Contains(string(body), `"v1"`) {
t.Errorf("v1 missing after restart: %s", body)
}
}

// 7. Force reconcile endpoint returns 200.
func TestIntegration_ForceReconcile_OK(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

code, body := httpDo(t, "POST", h.adminURL+"/admin/reconcile", "")
if code != 200 {
t.Fatalf("force reconcile: %d %s", code, body)
}
}

// 8. /admin/drift JSON has the expected envelope.
func TestIntegration_DriftEnvelope_Shape(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

code, body := httpDo(t, "GET", h.adminURL+"/admin/drift", "")
if code != 200 {
t.Fatalf("drift: %d %s", code, body)
}

var out struct {
Items   []any          `json:"items"`
Summary map[string]int `json:"summary"`
}
if err := json.Unmarshal(body, &out); err != nil {
t.Fatalf("decode drift: %v body=%s", err, body)
}
if out.Summary == nil {
t.Errorf("missing summary block: %s", body)
}
}

// 9. /admin/eni-placement returns count:0 on an empty store.
func TestIntegration_EniPlacement_EmptyStore(t *testing.T) {
skipIfNoToolchain(t)
h := startHarness(t)

code, body := httpDo(t, "GET", h.adminURL+"/admin/eni-placement", "")
if code != 200 {
t.Fatalf("eni-placement: %d %s", code, body)
}
if !strings.Contains(string(body), `"count":0`) {
t.Errorf("expected count:0; got %s", body)
}
}