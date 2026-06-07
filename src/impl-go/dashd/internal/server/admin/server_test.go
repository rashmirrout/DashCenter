package admin

import (
"encoding/json"
"net/http"
"net/http/httptest"
"testing"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func setupAdmin(t *testing.T) (*httptest.Server, *inventory.Inventory) {
t.Helper()
dir := t.TempDir()
fs, _ := filstore.Open(dir)
t.Cleanup(func() { fs.Close() })
inv := inventory.New()
obs := model.NewObsCache()
srv := New(inv, fs, obs, nil)
return httptest.NewServer(srv.srv.Handler), inv
}

// 1. All UP → status: ok
func TestHealthAllUp(t *testing.T) {
ts, inv := setupAdmin(t)
defer ts.Close()

inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
inv.SetState("dpu-0", dashcenterv1.DpuState_DPU_STATE_UP)

resp, _ := http.Get(ts.URL + "/admin/health")
if resp.StatusCode != 200 {
t.Fatalf("expected 200, got %d", resp.StatusCode)
}
var body map[string]any
json.NewDecoder(resp.Body).Decode(&body)
if body["status"] != "ok" {
t.Errorf("expected status=ok, got %v", body["status"])
}
}

// 2. One not UP → status: degraded
func TestHealthDegraded(t *testing.T) {
ts, inv := setupAdmin(t)
defer ts.Close()

inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
// State is REGISTERING (not UP).

resp, _ := http.Get(ts.URL + "/admin/health")
var body map[string]any
json.NewDecoder(resp.Body).Decode(&body)
if body["status"] != "degraded" {
t.Errorf("expected status=degraded, got %v", body["status"])
}
}

// 3. Inventory lists all DPUs
func TestAdminInventory(t *testing.T) {
ts, inv := setupAdmin(t)
defer ts.Close()

inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
inv.Register(inventory.DpuEntry{ID: "dpu-1", Endpoint: "y"})

resp, _ := http.Get(ts.URL + "/admin/inventory")
if resp.StatusCode != 200 {
t.Fatalf("expected 200, got %d", resp.StatusCode)
}
}

// 4. Desired without kind → 400
func TestDesiredWithoutKind(t *testing.T) {
ts, _ := setupAdmin(t)
defer ts.Close()

resp, _ := http.Get(ts.URL + "/admin/desired")
if resp.StatusCode != 400 {
t.Errorf("expected 400, got %d", resp.StatusCode)
}
}

// 5. Observed without dpu → 400
func TestObservedWithoutDpu(t *testing.T) {
ts, _ := setupAdmin(t)
defer ts.Close()

resp, _ := http.Get(ts.URL + "/admin/observed")
if resp.StatusCode != 400 {
t.Errorf("expected 400, got %d", resp.StatusCode)
}
}

// 6. Drift returns empty items
func TestDriftEmpty(t *testing.T) {
ts, _ := setupAdmin(t)
defer ts.Close()

resp, _ := http.Get(ts.URL + "/admin/drift")
if resp.StatusCode != 200 {
t.Fatalf("expected 200, got %d", resp.StatusCode)
}
var body map[string]any
json.NewDecoder(resp.Body).Decode(&body)
items := body["items"].([]any)
if len(items) != 0 {
t.Errorf("expected 0 drift items, got %d", len(items))
}
}

// 7. Reconcile returns ok
func TestAdminReconcile(t *testing.T) {
ts, _ := setupAdmin(t)
defer ts.Close()

resp, _ := http.Post(ts.URL+"/admin/reconcile", "application/json", nil)
if resp.StatusCode != 200 {
t.Errorf("expected 200, got %d", resp.StatusCode)
}
}