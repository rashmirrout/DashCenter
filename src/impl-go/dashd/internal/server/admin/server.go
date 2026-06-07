// Package admin implements the admin HTTP server for health, drift, and
// force-reconcile endpoints.
package admin

import (
"context"
"encoding/json"
"errors"
"log/slog"
"net/http"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Server is the admin HTTP server.
type Server struct {
srv *http.Server
}

// New creates an admin server.
func New(inv *inventory.Inventory, st store.DesiredStore, obs *model.ObsCache, rec *reconciler.Reconciler) *Server {
h := &handler{inv: inv, store: st, obs: obs, rec: rec}
mux := http.NewServeMux()
mux.HandleFunc("GET /admin/health", h.health)
mux.HandleFunc("GET /admin/leader", h.leader)
mux.HandleFunc("GET /admin/inventory", h.inventoryList)
mux.HandleFunc("GET /admin/desired", h.desired)
mux.HandleFunc("GET /admin/observed", h.observed)
mux.HandleFunc("GET /admin/drift", h.drift)
mux.HandleFunc("POST /admin/reconcile", h.reconcile)
return &Server{srv: &http.Server{
Handler:           mux,
ReadHeaderTimeout: 5 * time.Second,
}}
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
inv   *inventory.Inventory
store store.DesiredStore
obs   *model.ObsCache
rec   *reconciler.Reconciler
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
type dpuInfo struct {
ID       string `json:"id"`
State    string `json:"state"`
LastSeen string `json:"last_seen"`
}

out := struct {
Status string    `json:"status"`
Leader bool      `json:"leader"`
Dpus   []dpuInfo `json:"dpus"`
}{Status: "ok", Leader: true}

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
writeJSON(w, 200, map[string]bool{"leader": true})
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

func (h *handler) drift(w http.ResponseWriter, r *http.Request) {
// Phase 1 stub: drift computation requires full placement + diff.
// For now, returns empty items.
writeJSON(w, 200, map[string]any{"items": []any{}})
}

func (h *handler) reconcile(w http.ResponseWriter, r *http.Request) {
if h.rec != nil {
h.rec.ForceReconcile()
}
writeJSON(w, 200, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
json.NewEncoder(w).Encode(map[string]string{"error": msg})
}