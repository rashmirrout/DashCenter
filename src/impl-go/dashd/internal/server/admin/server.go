// Package admin implements the admin HTTP server for health, drift,
// ENI placement, and force-reconcile endpoints. Everything here is
// read-only except the explicit POST /admin/reconcile.
//
// All operator-facing data shaping lives here so the placement and
// dispatch packages remain pure / IO-free.
package admin

import (
"context"
"encoding/json"
"errors"
"log/slog"
"net/http"
"sort"
"time"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Server is the admin HTTP server.
type Server struct {
	srv        *http.Server
	clusterSvc service.ClusterService // PE-G2; set later via SetClusterService
	cntStore   CountersStore          // PE-3b; set later via SetCountersWiring
	cntPoller  CountersPoller         // PE-3b; set later via SetCountersWiring
	handler    *handler                // back-ref so SetClusterService can wire it through
}

// LeaderObserver is the minimal slice of an elector that the admin
// handler needs to report leadership state on /admin/health and
// /admin/leader. Implemented by *leader.EtcdElector and *leader.NoneElector.
type LeaderObserver interface {
	IsLeader() bool
	LeaderID() string
}

// noLeaderObserver is the always-leader fallback used when no elector is
// supplied (preserves PA-0 single-node behaviour for callers that haven't
// migrated to NewWithElector yet).
type noLeaderObserver struct{}

func (noLeaderObserver) IsLeader() bool    { return true }
func (noLeaderObserver) LeaderID() string { return "" }

// New creates an admin server. Equivalent to NewWithElector with a
// stub elector that always reports leader=true (PA-0 single-node).
func New(inv *inventory.Inventory, st store.DesiredStore, obs *model.ObsCache, rec *reconciler.Reconciler) *Server {
	return NewWithElector(inv, st, obs, rec, noLeaderObserver{})
}

// NewWithElector creates an admin server that reports live leadership
// state from the supplied LeaderObserver on /admin/health and
// /admin/leader. Multi-node controller deployments (PA-3+) MUST use this
// constructor so followers report leader=false.
func NewWithElector(inv *inventory.Inventory, st store.DesiredStore, obs *model.ObsCache, rec *reconciler.Reconciler, el LeaderObserver) *Server {
	if el == nil {
		el = noLeaderObserver{}
	}
	h := &handler{inv: inv, store: st, obs: obs, rec: rec, elector: el}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/health", h.health)
	mux.HandleFunc("GET /admin/leader", h.leader)
	mux.HandleFunc("GET /admin/inventory", h.inventoryList)
	mux.HandleFunc("GET /admin/desired", h.desired)
	mux.HandleFunc("GET /admin/observed", h.observed)
	mux.HandleFunc("GET /admin/drift", h.drift)
	mux.HandleFunc("GET /admin/eni-placement", h.eniPlacement)
	mux.HandleFunc("POST /admin/reconcile", h.reconcile)
	mux.HandleFunc("GET /admin/topology", h.topology)
	mux.HandleFunc("GET /admin/counters", h.countersList)
	mux.HandleFunc("POST /admin/counters/poll-interval", h.countersPollInterval)
	mux.HandleFunc("POST /admin/counters/enable", h.countersEnable)
	return &Server{
		srv: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		handler: h,
	}
}

// Serve starts listening on addr.
func (s *Server) Serve(addr string) error {
s.srv.Addr = addr
slog.Info("admin: listening", "addr", addr)
err := s.srv.ListenAndServe()
if errors.Is(err, http.ErrServerClosed) {
return nil
}
return err
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
_ = s.srv.Shutdown(ctx)
}

type handler struct {
inv     *inventory.Inventory
store   store.DesiredStore
obs     *model.ObsCache
rec     *reconciler.Reconciler
elector LeaderObserver
clusterSvc service.ClusterService // PE-G2; nil until main.go calls Server.SetClusterService
cntStore   CountersStore          // PE-3b; nil until SetCountersWiring
cntPoller  CountersPoller         // PE-3b; nil until SetCountersWiring
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
type dpuInfo struct {
ID       string `json:"id"`
State    string `json:"state"`
LastSeen string `json:"last_seen"`
}

leaderID := h.elector.LeaderID()
isLeader := h.elector.IsLeader()
out := struct {
Status   string    `json:"status"`
Leader   bool      `json:"leader"`
LeaderID string    `json:"leader_id,omitempty"`
Dpus     []dpuInfo `json:"dpus"`
}{Status: "ok", Leader: isLeader, LeaderID: leaderID}

allOk := true
for _, e := range h.inv.List() {
out.Dpus = append(out.Dpus, dpuInfo{
ID:       e.ID,
State:    e.State.String(),
LastSeen: e.LastSeen.Format(time.RFC3339),
})
if e.State != dashcenterv1.DpuState_DPU_STATE_UP {
allOk = false
}
}
if !allOk {
out.Status = "degraded"
}
writeJSON(w, 200, out)
}

func (h *handler) leader(w http.ResponseWriter, r *http.Request) {
writeJSON(w, 200, map[string]any{"leader": h.elector.IsLeader(), "leader_id": h.elector.LeaderID()})
}

func (h *handler) inventoryList(w http.ResponseWriter, r *http.Request) {
entries := h.inv.List()
writeJSON(w, 200, map[string]any{"dpus": entries})
}

func (h *handler) desired(w http.ResponseWriter, r *http.Request) {
kind := r.URL.Query().Get("kind")
if kind == "" {
writeErr(w, 400, "kind query parameter is required")
return
}
specs, err := h.store.List(r.Context(), store.DefaultNamespace, kind)
if err != nil {
writeErr(w, 500, err.Error())
return
}
items := make([]map[string]any, len(specs))
for i, sp := range specs {
items[i] = map[string]any{
"name": sp.Key.Name, "generation": sp.Generation,
"spec": json.RawMessage(sp.Data),
}
}
writeJSON(w, 200, map[string]any{"items": items})
}

func (h *handler) observed(w http.ResponseWriter, r *http.Request) {
dpuID := r.URL.Query().Get("dpu")
if dpuID == "" {
writeErr(w, 400, "dpu query parameter is required")
return
}
m := h.obs.GetDpu(dpuID)
items := make([]map[string]any, 0, len(m))
for _, obj := range m {
items = append(items, map[string]any{
"kind": obj.GetKind().String(),
"key":  obj.GetKey(),
})
}
writeJSON(w, 200, map[string]any{"items": items})
}

// drift computes the live declared-vs-observed delta for every DPU
// (or the single ?dpu= query parameter, if supplied) and returns one
// JSON item per (dpu, op, kind, key) triple. Op is "add", "update",
// or "remove" — matching the worker's vocabulary.
//
// This endpoint is read-only: it does NOT mutate state, and it does
// NOT trigger a reconcile.
func (h *handler) drift(w http.ResponseWriter, r *http.Request) {
specs, err := placement.LoadDesiredSpecs(r.Context(), h.store)
if err != nil {
writeErr(w, 500, "load desired: "+err.Error())
return
}

// Filter to one DPU if requested; otherwise scan all.
dpuFilter := r.URL.Query().Get("dpu")
dpuIDs := h.dpuList(dpuFilter)

type driftItem struct {
DpuID string   `json:"dpu_id"`
Op    string   `json:"op"`
Kind  string   `json:"kind"`
Key   []string `json:"key"`
}

var items []driftItem
for _, id := range dpuIDs {
desired := placement.Resolve(id, specs, h.inv)
diff := h.obs.Diff(id, desired)

appendOps := func(op string, objs []*dashapiv1.Object) {
for _, o := range objs {
items = append(items, driftItem{
DpuID: id, Op: op,
Kind: o.GetKind().String(),
Key:  o.GetKey(),
})
}
}
appendOps("add", diff.Add)
appendOps("update", diff.Update)
appendOps("remove", diff.Remove)
}

// Stable order: (dpu, op, kind, joined-key) for reproducible output.
sort.SliceStable(items, func(i, j int) bool {
if items[i].DpuID != items[j].DpuID {
return items[i].DpuID < items[j].DpuID
}
if items[i].Op != items[j].Op {
return items[i].Op < items[j].Op
}
if items[i].Kind != items[j].Kind {
return items[i].Kind < items[j].Kind
}
return joinKey(items[i].Key) < joinKey(items[j].Key)
})

writeJSON(w, 200, map[string]any{
"items": items,
"summary": map[string]int{
"total":  len(items),
"dpus":   len(dpuIDs),
},
})
}

// eniPlacement returns one item per ENI showing which DPUs it is
// placed on (the placement hint) and whether each DPU agrees the ENI
// is observed. This is the "where does my ENI live?" diagnostic.
//
// Query parameters:
//   - ?vnet=<name>  — restrict to ENIs in this VNET.
//   - ?eni=<name>   — restrict to a single ENI.
func (h *handler) eniPlacement(w http.ResponseWriter, r *http.Request) {
specs, err := placement.LoadDesiredSpecs(r.Context(), h.store)
if err != nil {
writeErr(w, 500, "load desired: "+err.Error())
return
}

vnetFilter := r.URL.Query().Get("vnet")
eniFilter := r.URL.Query().Get("eni")

type dpuPlacement struct {
DpuID    string `json:"dpu_id"`
Observed bool   `json:"observed"`
}

type eniItem struct {
Name      string         `json:"name"`
VnetName  string         `json:"vnet_name"`
MAC       string         `json:"mac_address,omitempty"`
UnderlayIp string        `json:"underlay_ip,omitempty"`
AdminState string        `json:"admin_state,omitempty"`
Placements []dpuPlacement `json:"placements"`
}

var items []eniItem
names := make([]string, 0, len(specs.Enis))
for n := range specs.Enis {
names = append(names, n)
}
sort.Strings(names)

for _, name := range names {
eni := specs.Enis[name]
if eniFilter != "" && name != eniFilter {
continue
}
if vnetFilter != "" && eni.GetVnetName() != vnetFilter {
continue
}

placements := make([]dpuPlacement, 0, len(eni.GetPlacementHintDpuIds()))
for _, dpuID := range eni.GetPlacementHintDpuIds() {
observed := h.obs.GetDpu(dpuID)
hit := false
for _, obj := range observed {
if obj.GetKind() == dashapiv1.ObjectKind_OBJECT_KIND_ENI &&
len(obj.GetKey()) == 1 && obj.GetKey()[0] == name {
hit = true
break
}
}
placements = append(placements, dpuPlacement{DpuID: dpuID, Observed: hit})
}

items = append(items, eniItem{
Name:       name,
VnetName:   eni.GetVnetName(),
MAC:        eni.GetMacAddress(),
UnderlayIp: eni.GetUnderlayIp(),
AdminState: eni.GetAdminState(),
Placements: placements,
})
}

writeJSON(w, 200, map[string]any{
"items": items,
"count": len(items),
})
}

func (h *handler) reconcile(w http.ResponseWriter, r *http.Request) {
if h.rec != nil {
h.rec.ForceReconcile()
}
writeJSON(w, 200, map[string]any{"ok": true})
}

// dpuList returns the DPU IDs to scan: a single-element slice if
// `filter` is non-empty (and the DPU exists), otherwise every DPU.
func (h *handler) dpuList(filter string) []string {
all := h.inv.List()
if filter != "" {
for _, e := range all {
if e.ID == filter {
return []string{filter}
}
}
return nil
}
ids := make([]string, len(all))
for i, e := range all {
ids[i] = e.ID
}
sort.Strings(ids)
return ids
}

// joinKey is local to admin so we don't have to export model.innerKey.
// Same semantics: ":"-joined string, stable for sort.
func joinKey(parts []string) string {
out := ""
for i, p := range parts {
if i > 0 {
out += ":"
}
out += p
}
return out
}

func writeJSON(w http.ResponseWriter, status int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}