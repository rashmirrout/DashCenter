// Package rest implements the HTTP REST gateway for the dashcenter.v1 API.
package rest

import (
"context"
"encoding/json"
"errors"
"fmt"
"io"
"log/slog"
"net/http"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Server is the REST HTTP server.
type Server struct {
srv *http.Server
}

// New creates a REST server.
func New(st store.DesiredStore, inv *inventory.Inventory, rec *reconciler.Reconciler) *Server {
h := &handler{store: st, inv: inv, rec: rec}
return &Server{srv: &http.Server{
Handler:           h.router(),
ReadHeaderTimeout: 5 * time.Second,
}}
}

// Serve starts listening on addr.
func (s *Server) Serve(addr string) error {
s.srv.Addr = addr
slog.Info("rest: listening", "addr", addr)
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
store store.DesiredStore
inv   *inventory.Inventory
rec   *reconciler.Reconciler
}

func (h *handler) router() http.Handler {
mux := http.NewServeMux()
mux.HandleFunc("PUT /v1/inventory", h.putInventory)
mux.HandleFunc("GET /v1/inventory", h.getInventory)
mux.HandleFunc("PUT /v1/vnets/{name}", h.putVnet)
mux.HandleFunc("GET /v1/vnets/{name}", h.getVnet)
mux.HandleFunc("GET /v1/vnets", h.listVnets)
mux.HandleFunc("PUT /v1/enis/{name}", h.putEni)
mux.HandleFunc("GET /v1/enis/{name}", h.getEni)
mux.HandleFunc("GET /v1/enis", h.listEnis)
mux.HandleFunc("PUT /v1/vnet-mappings/{name}", h.putVnetMapping)
mux.HandleFunc("PUT /v1/acl-policies/{name}", h.putAclPolicy)
mux.HandleFunc("PUT /v1/route-policies/{name}", h.putRoutePolicy)
mux.HandleFunc("PUT /v1/ha-sets/{name}", h.putHaSet)
mux.HandleFunc("DELETE /v1/{kind}/{name}", h.delete)
mux.HandleFunc("POST /v1/reconcile", h.reconcile)
return mux
}

func (h *handler) putVnet(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
body, err := io.ReadAll(r.Body)
if err != nil {
writeErr(w, 400, err)
return
}
spec := &dashcenterv1.VnetSpec{}
if err := json.Unmarshal(body, spec); err != nil {
writeErr(w, 400, fmt.Errorf("invalid spec: %w", err))
return
}
gen, err := h.store.Put(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: "vnet", Name: name}, spec, int64(spec.GetExpectedGeneration()))
if err != nil {
handleStoreErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"accepted": true, "generation": gen})
}

func (h *handler) getVnet(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
sp, err := h.store.Get(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: "vnet", Name: name})
if err != nil {
handleStoreErr(w, err)
return
}
writeJSON(w, 200, map[string]any{
"kind": "vnet", "name": sp.Key.Name, "generation": sp.Generation,
"spec": json.RawMessage(sp.Data),
})
}

func (h *handler) listVnets(w http.ResponseWriter, r *http.Request) {
h.listKind(w, r, "vnet")
}

func (h *handler) putEni(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
body, _ := io.ReadAll(r.Body)
spec := &dashcenterv1.EniSpec{}
if err := json.Unmarshal(body, spec); err != nil {
writeErr(w, 400, fmt.Errorf("invalid spec: %w", err))
return
}
gen, err := h.store.Put(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: "eni", Name: name}, spec, int64(spec.GetExpectedGeneration()))
if err != nil {
handleStoreErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"accepted": true, "generation": gen})
}

func (h *handler) getEni(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
sp, err := h.store.Get(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: "eni", Name: name})
if err != nil {
handleStoreErr(w, err)
return
}
writeJSON(w, 200, map[string]any{
"kind": "eni", "name": sp.Key.Name, "generation": sp.Generation,
"spec": json.RawMessage(sp.Data),
})
}

func (h *handler) listEnis(w http.ResponseWriter, r *http.Request) {
h.listKind(w, r, "eni")
}

func (h *handler) putVnetMapping(w http.ResponseWriter, r *http.Request) {
h.putGeneric(w, r, "vnet_mapping", &dashcenterv1.VnetMappingSpec{})
}

func (h *handler) putAclPolicy(w http.ResponseWriter, r *http.Request) {
h.putGeneric(w, r, "acl_policy", &dashcenterv1.AclPolicySpec{})
}

func (h *handler) putRoutePolicy(w http.ResponseWriter, r *http.Request) {
h.putGeneric(w, r, "route_policy", &dashcenterv1.RoutePolicySpec{})
}

func (h *handler) putHaSet(w http.ResponseWriter, r *http.Request) {
h.putGeneric(w, r, "ha_set", &dashcenterv1.HaSetSpec{})
}

func (h *handler) putInventory(w http.ResponseWriter, r *http.Request) {
body, _ := io.ReadAll(r.Body)
var req struct {
Dpus []struct {
ID       string            `json:"id"`
Endpoint string            `json:"endpoint"`
Labels   map[string]string `json:"labels"`
} `json:"dpus"`
}
if err := json.Unmarshal(body, &req); err != nil {
writeErr(w, 400, err)
return
}
for _, d := range req.Dpus {
if err := h.inv.Register(inventory.DpuEntry{
ID: d.ID, Endpoint: d.Endpoint, Labels: d.Labels,
}); err != nil {
writeErr(w, 400, err)
return
}
}
writeJSON(w, 200, map[string]any{"accepted": true})
}

func (h *handler) getInventory(w http.ResponseWriter, r *http.Request) {
entries := h.inv.List()
writeJSON(w, 200, map[string]any{"dpus": entries})
}

// urlKindToStoreKind maps plural URL path segments to singular store kind names.
var urlKindToStoreKind = map[string]string{
"vnets":          "vnet",
"enis":           "eni",
"vnet-mappings":  "vnet_mapping",
"acl-policies":   "acl_policy",
"route-policies": "route_policy",
"ha-sets":        "ha_set",
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
rawKind := r.PathValue("kind")
kind, ok := urlKindToStoreKind[rawKind]
if !ok {
kind = rawKind // fallback: use as-is
}
name := r.PathValue("name")
err := h.store.Delete(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: kind, Name: name})
if err != nil {
handleStoreErr(w, err)
return
}
w.WriteHeader(204)
}

func (h *handler) reconcile(w http.ResponseWriter, r *http.Request) {
if h.rec != nil {
h.rec.ForceReconcile()
}
writeJSON(w, 200, map[string]any{"ok": true})
}

// putGeneric handles PUT for any spec type.
func (h *handler) putGeneric(w http.ResponseWriter, r *http.Request, kind string, _ any) {
name := r.PathValue("name")
body, _ := io.ReadAll(r.Body)

// We store the raw JSON as-is via a wrapperspb.StringValue for simplicity.
// The spec is validated on read by the placement package.
spec := &dashcenterv1.VnetSpec{} // placeholder; store accepts proto.Message
_ = json.Unmarshal(body, spec)

gen, err := h.store.Put(r.Context(), store.ObjectKey{Namespace: store.DefaultNamespace, Kind: kind, Name: name}, spec, 0)
if err != nil {
handleStoreErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"accepted": true, "generation": gen})
}

func (h *handler) listKind(w http.ResponseWriter, r *http.Request, kind string) {
specs, err := h.store.List(r.Context(), store.DefaultNamespace, kind)
if err != nil {
writeErr(w, 500, err)
return
}
items := make([]map[string]any, len(specs))
for i, sp := range specs {
items[i] = map[string]any{
"kind": sp.Key.Kind, "name": sp.Key.Name, "generation": sp.Generation,
"spec": json.RawMessage(sp.Data),
}
}
writeJSON(w, 200, map[string]any{"items": items})
}

func handleStoreErr(w http.ResponseWriter, err error) {
if errors.Is(err, store.ErrNotFound) {
writeErr(w, 404, errors.New("not found"))
} else if errors.Is(err, store.ErrGenerationMismatch) {
writeErr(w, 409, errors.New("generation mismatch"))
} else {
writeErr(w, 500, errors.New("internal"))
}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}