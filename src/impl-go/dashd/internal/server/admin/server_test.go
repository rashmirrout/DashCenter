package admin

import (
"context"
"encoding/json"
"io"
"net/http"
"net/http/httptest"
"strings"
"testing"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

type adminFixture struct {
ts    *httptest.Server
inv   *inventory.Inventory
store *filstore.FileStore
obs   *model.ObsCache
}

func setupAdmin(t *testing.T) *adminFixture {
t.Helper()
dir := t.TempDir()
fs, err := filstore.Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
t.Cleanup(func() { _ = fs.Close() })

inv := inventory.New()
obs := model.NewObsCache()
srv := New(inv, fs, obs, nil)

ts := httptest.NewServer(srv.srv.Handler)
t.Cleanup(ts.Close)
return &adminFixture{ts: ts, inv: inv, store: fs, obs: obs}
}

func (f *adminFixture) get(t *testing.T, path string) (int, []byte) {
t.Helper()
resp, err := http.Get(f.ts.URL + path)
if err != nil {
t.Fatalf("GET %s: %v", path, err)
}
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)
return resp.StatusCode, body
}

func (f *adminFixture) post(t *testing.T, path string) (int, []byte) {
t.Helper()
resp, err := http.Post(f.ts.URL+path, "application/json", strings.NewReader(""))
if err != nil {
t.Fatalf("POST %s: %v", path, err)
}
defer resp.Body.Close()
body, _ := io.ReadAll(resp.Body)
return resp.StatusCode, body
}

func (f *adminFixture) seedVnetEni(t *testing.T, dpuID string) {
t.Helper()
ctx := context.Background()
vnet := &dashcenterv1.VnetSpec{Name: "v1", Vni: 1000}
mustPut(t, f.store, ctx, "vnet", "v1", vnet)
eni := &dashcenterv1.EniSpec{
Name:                "e1",
VnetName:            "v1",
MacAddress:          "00:11:22:33:44:55",
UnderlayIp:          "10.0.0.1",
AdminState:          "enabled",
PlacementHintDpuIds: []string{dpuID},
}
mustPut(t, f.store, ctx, "eni", "e1", eni)
}

func mustPut(t *testing.T, st *filstore.FileStore, ctx context.Context, kind, name string, spec any) {
t.Helper()
key := store.ObjectKey{Namespace: store.DefaultNamespace, Kind: kind, Name: name}
if _, err := st.Put(ctx, key, spec, 0); err != nil {
t.Fatalf("seed %s/%s: %v", kind, name, err)
}
}

// --- health ---

func TestAdmin_Health_AllUp_StatusOk(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
f.inv.SetState("dpu-0", dashcenterv1.DpuState_DPU_STATE_UP)

code, body := f.get(t, "/admin/health")
if code != 200 {
t.Fatalf("status=%d", code)
}
var out map[string]any
_ = json.Unmarshal(body, &out)
if out["status"] != "ok" {
t.Errorf("status=%v want ok", out["status"])
}
}

func TestAdmin_Health_OneDown_StatusDegraded(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
// state defaults to REGISTERING

code, body := f.get(t, "/admin/health")
if code != 200 {
t.Fatalf("status=%d", code)
}
var out map[string]any
_ = json.Unmarshal(body, &out)
if out["status"] != "degraded" {
t.Errorf("status=%v want degraded", out["status"])
}
}

// --- inventory / leader ---

func TestAdmin_Inventory_ListsAll(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-a", Endpoint: "x"})
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-b", Endpoint: "y"})

code, body := f.get(t, "/admin/inventory")
if code != 200 {
t.Fatalf("status=%d", code)
}
if !strings.Contains(string(body), "dpu-a") || !strings.Contains(string(body), "dpu-b") {
t.Errorf("inventory body missing entries: %s", body)
}
}

func TestAdmin_Leader_AlwaysTrue(t *testing.T) {
f := setupAdmin(t)
code, body := f.get(t, "/admin/leader")
if code != 200 {
t.Fatalf("status=%d", code)
}
var out map[string]bool
_ = json.Unmarshal(body, &out)
if !out["leader"] {
t.Error("leader=false")
}
}

// --- desired / observed validation ---

func TestAdmin_Desired_MissingKind_400(t *testing.T) {
f := setupAdmin(t)
code, _ := f.get(t, "/admin/desired")
if code != 400 {
t.Errorf("status=%d want 400", code)
}
}

func TestAdmin_Desired_WithKind_ReturnsItems(t *testing.T) {
f := setupAdmin(t)
ctx := context.Background()
mustPut(t, f.store, ctx, "vnet", "v1", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})

code, body := f.get(t, "/admin/desired?kind=vnet")
if code != 200 {
t.Fatalf("status=%d body=%s", code, body)
}
if !strings.Contains(string(body), "v1") {
t.Errorf("v1 missing from response: %s", body)
}
}

func TestAdmin_Observed_MissingDpu_400(t *testing.T) {
f := setupAdmin(t)
code, _ := f.get(t, "/admin/observed")
if code != 400 {
t.Errorf("status=%d want 400", code)
}
}

func TestAdmin_Observed_ReturnsCacheEntries(t *testing.T) {
f := setupAdmin(t)
f.obs.Set("dpu-0", &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"},
})

code, body := f.get(t, "/admin/observed?dpu=dpu-0")
if code != 200 {
t.Fatalf("status=%d", code)
}
if !strings.Contains(string(body), "OBJECT_KIND_VNET") {
t.Errorf("response missing VNET kind: %s", body)
}
}

// --- drift ---

// 1. Empty everything → 200 with empty items.
func TestAdmin_Drift_EmptyEverything_NoItems(t *testing.T) {
f := setupAdmin(t)
code, body := f.get(t, "/admin/drift")
if code != 200 {
t.Fatalf("status=%d", code)
}
var out map[string]any
_ = json.Unmarshal(body, &out)
items, _ := out["items"].([]any)
if len(items) != 0 {
t.Errorf("items=%d want 0", len(items))
}
}

// 2. Desired only (no observed) → drift contains add ops.
func TestAdmin_Drift_DesiredOnly_ReportsAdds(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
f.seedVnetEni(t, "dpu-0")

code, body := f.get(t, "/admin/drift")
if code != 200 {
t.Fatalf("status=%d body=%s", code, body)
}
var out struct {
Items []struct {
DpuID string   `json:"dpu_id"`
Op    string   `json:"op"`
Kind  string   `json:"kind"`
Key   []string `json:"key"`
} `json:"items"`
Summary map[string]int `json:"summary"`
}
if err := json.Unmarshal(body, &out); err != nil {
t.Fatalf("decode: %v", err)
}
if len(out.Items) == 0 {
t.Fatal("expected at least one add item")
}
sawAddVnet, sawAddEni := false, false
for _, it := range out.Items {
if it.DpuID != "dpu-0" || it.Op != "add" {
continue
}
if it.Kind == "OBJECT_KIND_VNET" {
sawAddVnet = true
}
if it.Kind == "OBJECT_KIND_ENI" {
sawAddEni = true
}
}
if !sawAddVnet || !sawAddEni {
t.Errorf("missing add ops: vnet=%v eni=%v", sawAddVnet, sawAddEni)
}
}

// 3. Observed only (no desired) → drift contains remove ops.
func TestAdmin_Drift_ObservedOnly_ReportsRemoves(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
f.obs.Set("dpu-0", &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"stale"},
})

code, body := f.get(t, "/admin/drift")
if code != 200 {
t.Fatalf("status=%d body=%s", code, body)
}
var out struct {
Items []struct {
Op   string   `json:"op"`
Key  []string `json:"key"`
} `json:"items"`
}
_ = json.Unmarshal(body, &out)
removeFound := false
for _, it := range out.Items {
if it.Op == "remove" && len(it.Key) > 0 && it.Key[0] == "stale" {
removeFound = true
}
}
if !removeFound {
t.Errorf("expected remove op for stale; body=%s", body)
}
}

// 4. ?dpu= filter restricts to one DPU.
func TestAdmin_Drift_DpuFilter_ScopesOutput(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-1", Endpoint: "y"})
f.seedVnetEni(t, "dpu-0") // ENI on dpu-0 only

code, body := f.get(t, "/admin/drift?dpu=dpu-1")
if code != 200 {
t.Fatalf("status=%d", code)
}
var out struct {
Items []struct {
DpuID string `json:"dpu_id"`
} `json:"items"`
Summary map[string]int `json:"summary"`
}
_ = json.Unmarshal(body, &out)
for _, it := range out.Items {
if it.DpuID != "dpu-1" {
t.Errorf("filter leaked dpu-id=%s", it.DpuID)
}
}
if out.Summary["dpus"] != 1 {
t.Errorf("summary.dpus=%d want 1", out.Summary["dpus"])
}
}

// 5. Unknown ?dpu= filter → empty items but 200.
func TestAdmin_Drift_UnknownDpuFilter_EmptyItems(t *testing.T) {
f := setupAdmin(t)
code, body := f.get(t, "/admin/drift?dpu=ghost")
if code != 200 {
t.Fatalf("status=%d", code)
}
var out struct {
Items []any `json:"items"`
}
_ = json.Unmarshal(body, &out)
if len(out.Items) != 0 {
t.Errorf("items=%d want 0", len(out.Items))
}
}

// 6. Store error → 500 with error envelope.
func TestAdmin_Drift_StoreError_500(t *testing.T) {
f := setupAdmin(t)
_ = f.store.Close() // future List returns ErrClosed

code, body := f.get(t, "/admin/drift")
if code != 500 {
t.Fatalf("status=%d body=%s want 500", code, body)
}
if !strings.Contains(string(body), "error") {
t.Errorf("missing error envelope: %s", body)
}
}

// --- eni-placement ---

// 7. Empty store → 200 with no items.
func TestAdmin_EniPlacement_Empty_NoItems(t *testing.T) {
f := setupAdmin(t)
code, body := f.get(t, "/admin/eni-placement")
if code != 200 {
t.Fatalf("status=%d body=%s", code, body)
}
if !strings.Contains(string(body), `"count":0`) {
t.Errorf("expected count:0, got %s", body)
}
}

// 8. Seeded ENI → returned with placements; observed=false (cache empty).
func TestAdmin_EniPlacement_ReturnsEniWithUnobservedFlag(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
f.seedVnetEni(t, "dpu-0")

code, body := f.get(t, "/admin/eni-placement")
if code != 200 {
t.Fatalf("status=%d body=%s", code, body)
}
var out struct {
Items []struct {
Name       string `json:"name"`
VnetName   string `json:"vnet_name"`
Placements []struct {
DpuID    string `json:"dpu_id"`
Observed bool   `json:"observed"`
} `json:"placements"`
} `json:"items"`
}
if err := json.Unmarshal(body, &out); err != nil {
t.Fatalf("decode: %v body=%s", err, body)
}
if len(out.Items) != 1 {
t.Fatalf("items=%d want 1", len(out.Items))
}
got := out.Items[0]
if got.Name != "e1" || got.VnetName != "v1" {
t.Errorf("unexpected item: %+v", got)
}
if len(got.Placements) != 1 ||
got.Placements[0].DpuID != "dpu-0" ||
got.Placements[0].Observed {
t.Errorf("unexpected placements: %+v", got.Placements)
}
}

// 9. Seeded ENI + observed entry → placements report observed=true.
func TestAdmin_EniPlacement_ObservedFlag_True(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
f.seedVnetEni(t, "dpu-0")
f.obs.Set("dpu-0", &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_ENI, Key: []string{"e1"},
})

code, body := f.get(t, "/admin/eni-placement")
if code != 200 {
t.Fatalf("status=%d", code)
}
if !strings.Contains(string(body), `"observed":true`) {
t.Errorf("expected observed:true; body=%s", body)
}
}

// 10. ?vnet= filter scopes results.
func TestAdmin_EniPlacement_VnetFilter_Scopes(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
ctx := context.Background()
mustPut(t, f.store, ctx, "vnet", "vA", &dashcenterv1.VnetSpec{Name: "vA", Vni: 1})
mustPut(t, f.store, ctx, "vnet", "vB", &dashcenterv1.VnetSpec{Name: "vB", Vni: 2})
mustPut(t, f.store, ctx, "eni", "eA", &dashcenterv1.EniSpec{
Name: "eA", VnetName: "vA", MacAddress: "00:00:00:00:00:01",
UnderlayIp: "10.0.0.1", PlacementHintDpuIds: []string{"dpu-0"},
})
mustPut(t, f.store, ctx, "eni", "eB", &dashcenterv1.EniSpec{
Name: "eB", VnetName: "vB", MacAddress: "00:00:00:00:00:02",
UnderlayIp: "10.0.0.2", PlacementHintDpuIds: []string{"dpu-0"},
})

code, body := f.get(t, "/admin/eni-placement?vnet=vA")
if code != 200 {
t.Fatalf("status=%d", code)
}
if !strings.Contains(string(body), "eA") || strings.Contains(string(body), "eB") {
t.Errorf("vnet filter wrong: %s", body)
}
}

// 11. ?eni= filter selects a single ENI.
func TestAdmin_EniPlacement_EniFilter_SelectsOne(t *testing.T) {
f := setupAdmin(t)
_ = f.inv.Register(inventory.DpuEntry{ID: "dpu-0", Endpoint: "x"})
ctx := context.Background()
mustPut(t, f.store, ctx, "vnet", "v1", &dashcenterv1.VnetSpec{Name: "v1", Vni: 1})
mustPut(t, f.store, ctx, "eni", "eA", &dashcenterv1.EniSpec{
Name: "eA", VnetName: "v1", MacAddress: "00:00:00:00:00:01",
UnderlayIp: "10.0.0.1", PlacementHintDpuIds: []string{"dpu-0"},
})
mustPut(t, f.store, ctx, "eni", "eB", &dashcenterv1.EniSpec{
Name: "eB", VnetName: "v1", MacAddress: "00:00:00:00:00:02",
UnderlayIp: "10.0.0.2", PlacementHintDpuIds: []string{"dpu-0"},
})

_, body := f.get(t, "/admin/eni-placement?eni=eB")
if !strings.Contains(string(body), "eB") || strings.Contains(string(body), `"eA"`) {
t.Errorf("eni filter wrong: %s", body)
}
}

// 12. Store error on eni-placement → 500.
func TestAdmin_EniPlacement_StoreError_500(t *testing.T) {
f := setupAdmin(t)
_ = f.store.Close()

code, _ := f.get(t, "/admin/eni-placement")
if code != 500 {
t.Errorf("status=%d want 500", code)
}
}

// --- reconcile (rec is nil → still 200) ---

func TestAdmin_Reconcile_NoRec_Returns200(t *testing.T) {
f := setupAdmin(t)
code, body := f.post(t, "/admin/reconcile")
if code != 200 {
t.Fatalf("status=%d body=%s", code, body)
}
if !strings.Contains(string(body), `"ok":true`) {
t.Errorf("body=%s", body)
}
}

// --- helper: joinKey ---

func TestJoinKey_FormatsParts(t *testing.T) {
cases := []struct {
in   []string
want string
}{
{nil, ""},
{[]string{}, ""},
{[]string{"a"}, "a"},
{[]string{"a", "b"}, "a:b"},
{[]string{"x", "y", "z"}, "x:y:z"},
}
for _, tc := range cases {
got := joinKey(tc.in)
if got != tc.want {
t.Errorf("joinKey(%v)=%q want %q", tc.in, got, tc.want)
}
}
}

// --- lifecycle: Stop / Serve return cleanly ---

func TestAdminServer_StopWithoutServe_NoPanic(t *testing.T) {
inv := inventory.New()
obs := model.NewObsCache()
srv := New(inv, nil, obs, nil)
srv.Stop()
}