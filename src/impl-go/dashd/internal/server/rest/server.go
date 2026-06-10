// Package rest implements the HTTP REST gateway for the dashcenter.v1 API.
// This is a thin adapter: all business logic lives in internal/service/.
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
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/operations"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Server is the REST HTTP server.
type Server struct {
srv *http.Server
}

// New creates a REST server wired to the shared service layer. ha may
// be nil — in that case the /v1/ha/* routes return 503; existing
// callers wire it explicitly post-PC-G1.
func New(cp service.ControlPlaneService, obs service.ObservabilityService, ha service.HaService) *Server {
h := &handler{cp: cp, obs: obs, ha: ha}
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
cp  service.ControlPlaneService
obs service.ObservabilityService
ha  service.HaService // may be nil; /v1/ha/* returns 503 when so
}

// urlKindToStoreKind maps plural URL path segments to singular store kind names.
var urlKindToStoreKind = map[string]string{
"vnets":            "vnet",
"enis":             "eni",
"vnet-mappings":    "vnet_mapping",
"acl-policies":     "acl_policy",
"route-policies":   "route_policy",
"ha-sets":          "ha_set",
"service-tunnels":  "service_tunnel",
}

func (h *handler) router() http.Handler {
mux := http.NewServeMux()

// Inventory (no namespace).
mux.HandleFunc("PUT /v1/inventory", h.putInventory)
mux.HandleFunc("GET /v1/inventory", h.getInventory)

// RegisterDpu (PB-3): advertise DpuCapacityLimits + DpuCapabilities for
// a previously-registered DPU. Body shape mirrors service.DpuRegistration.
mux.HandleFunc("POST /v1/inventory/{id}/register", h.registerDpu)

// Cordon / Uncordon (PC-1). Body: {"reason": "..."} — reason is
// recorded in the operations audit ring for forensics. Idempotent.
mux.HandleFunc("POST /v1/inventory/{id}/cordon", h.cordonDpu)
mux.HandleFunc("POST /v1/inventory/{id}/uncordon", h.uncordonDpu)
mux.HandleFunc("GET /v1/inventory/cordoned", h.listCordoned)

// Drain (PC-G7). Body: {"reason":"...","parallelism":4}. Cordons
// the DPU then rehomes every ENI to a least-loaded uncordoned
// destination. Returns the full DrainResult envelope; status code
// is 200 when every ENI migrated, 207 (Multi-Status) when some
// failed (the source remains cordoned for retry).
mux.HandleFunc("POST /v1/inventory/{id}/drain", h.drainDpu)

// Namespace-scoped spec routes (with optional {ns} prefix, fallback to "default").
// Pattern: /v1/{ns}/{plural_kind}/{name}
mux.HandleFunc("PUT /v1/{ns}/vnets/{name}", h.putVnet)
mux.HandleFunc("PUT /v1/{ns}/enis/{name}", h.putEni)
mux.HandleFunc("PUT /v1/{ns}/vnet-mappings/{name}", h.putVnetMapping)
mux.HandleFunc("PUT /v1/{ns}/acl-policies/{name}", h.putAclPolicy)
mux.HandleFunc("PUT /v1/{ns}/route-policies/{name}", h.putRoutePolicy)
mux.HandleFunc("PUT /v1/{ns}/ha-sets/{name}", h.putHaSet)
mux.HandleFunc("PUT /v1/{ns}/service-tunnels/{name}", h.putServiceTunnel)

// Generic Get/List/Delete for all spec kinds (namespace-scoped).
mux.HandleFunc("GET /v1/{ns}/{kind}/{name}", h.get)
mux.HandleFunc("GET /v1/{ns}/{kind}", h.list)
mux.HandleFunc("DELETE /v1/{ns}/{kind}/{name}", h.delete)

// Backward-compat: non-namespace routes (defaults to "default" ns).
mux.HandleFunc("PUT /v1/vnets/{name}", h.putVnetDefault)
mux.HandleFunc("GET /v1/vnets/{name}", h.getDefault)
mux.HandleFunc("GET /v1/vnets", h.listDefault)
mux.HandleFunc("PUT /v1/enis/{name}", h.putEniDefault)
mux.HandleFunc("GET /v1/enis/{name}", h.getEniDefault)
mux.HandleFunc("GET /v1/enis", h.listEnisDefault)
mux.HandleFunc("DELETE /v1/{kind}/{name}", h.deleteDefault)

// Reconcile.
mux.HandleFunc("POST /v1/reconcile", h.reconcile)

// SimulateApply (PB-2): dry-run admission. Body is JSON of
// service.SimulateOp list under {"ops": [...]}. Always returns 200
// (the would_succeed field carries the verdict); only request-shape
// errors (empty body, bad json) return 400. This matches kubectl's
// `--dry-run=server` UX where the server is reachable but the request
// would fail validation — still a successful round-trip.
mux.HandleFunc("POST /v1/simulate", h.simulate)

// ApplyBatch (PC-8): atomic multi-spec write. Body is JSON of
// service.BatchOp list under {"ops": [...]}. Returns 200 with the
// service.BatchResult envelope on commit; 207 (Multi-Status) with
// the same envelope on partial rollback so operators can distinguish
// success from clean-failure from dirty-failure at the HTTP layer.
mux.HandleFunc("POST /v1/apply-batch", h.applyBatch)

// HA orchestration (PC-G1..G3).
mux.HandleFunc("GET /v1/ha", h.listHa)
mux.HandleFunc("GET /v1/ha/{ns}/{name}", h.getHa)
mux.HandleFunc("POST /v1/ha/{ns}/{name}/switchover", h.haSwitchover)
mux.HandleFunc("POST /v1/ha/{ns}/{name}/failover", h.haFailover)
mux.HandleFunc("GET /v1/ha/events", h.haWatchEvents)
mux.HandleFunc("GET /v1/ha/flow-sync-stats", h.haFlowSyncStats)

return mux
}

// --- Put handlers (namespace-scoped) ---

func (h *handler) putVnet(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
name := r.PathValue("name")
spec := &dashcenterv1.VnetSpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutVnet(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putEni(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
name := r.PathValue("name")
spec := &dashcenterv1.EniSpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutEni(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putVnetMapping(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
spec := &dashcenterv1.VnetMappingSpec{}
if !readSpec(w, r, spec) {
return
}
res, err := h.cp.PutVnetMapping(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putAclPolicy(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
name := r.PathValue("name")
spec := &dashcenterv1.AclPolicySpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutAclPolicy(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putRoutePolicy(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
name := r.PathValue("name")
spec := &dashcenterv1.RoutePolicySpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutRoutePolicy(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putHaSet(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
name := r.PathValue("name")
spec := &dashcenterv1.HaSetSpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutHaSet(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putServiceTunnel(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
name := r.PathValue("name")
spec := &dashcenterv1.ServiceTunnelSpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutServiceTunnel(r.Context(), ns, spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

// --- Backward-compat handlers (default namespace) ---

func (h *handler) putVnetDefault(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
spec := &dashcenterv1.VnetSpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutVnet(r.Context(), "", spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) putEniDefault(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
spec := &dashcenterv1.EniSpec{}
if !readSpec(w, r, spec) {
return
}
if spec.Name == "" {
spec.Name = name
}
res, err := h.cp.PutEni(r.Context(), "", spec)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, res)
}

func (h *handler) getDefault(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
// The first path segment after /v1/ is the plural kind.
kind := "vnet" // caller must route correctly
item, err := h.cp.Get(r.Context(), "", kind, name)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, item)
}

func (h *handler) getEniDefault(w http.ResponseWriter, r *http.Request) {
name := r.PathValue("name")
item, err := h.cp.Get(r.Context(), "", "eni", name)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, item)
}

func (h *handler) listDefault(w http.ResponseWriter, r *http.Request) {
items, err := h.cp.List(r.Context(), "", "vnet")
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"items": items})
}

func (h *handler) listEnisDefault(w http.ResponseWriter, r *http.Request) {
items, err := h.cp.List(r.Context(), "", "eni")
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"items": items})
}

func (h *handler) deleteDefault(w http.ResponseWriter, r *http.Request) {
rawKind := r.PathValue("kind")
kind := resolveKind(rawKind)
name := r.PathValue("name")
if err := h.cp.Delete(r.Context(), "", kind, name); err != nil {
handleServiceErr(w, err)
return
}
w.WriteHeader(204)
}

// --- Namespace-scoped CRUD ---

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
rawKind := r.PathValue("kind")
kind := resolveKind(rawKind)
name := r.PathValue("name")
item, err := h.cp.Get(r.Context(), ns, kind, name)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, item)
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
rawKind := r.PathValue("kind")
kind := resolveKind(rawKind)
items, err := h.cp.List(r.Context(), ns, kind)
if err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"items": items})
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
ns := r.PathValue("ns")
rawKind := r.PathValue("kind")
kind := resolveKind(rawKind)
name := r.PathValue("name")
if err := h.cp.Delete(r.Context(), ns, kind, name); err != nil {
handleServiceErr(w, err)
return
}
w.WriteHeader(204)
}

// --- Inventory ---

func (h *handler) putInventory(w http.ResponseWriter, r *http.Request) {
body, err := io.ReadAll(r.Body)
if err != nil {
writeErr(w, 400, err)
return
}
var req struct {
Dpus []service.DpuInput `json:"dpus"`
}
if err := json.Unmarshal(body, &req); err != nil {
writeErr(w, 400, err)
return
}
if err := h.cp.PutInventory(r.Context(), req.Dpus); err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"accepted": true})
}

// --- Cordon / Uncordon (PC-1) -----------------------------------------

// reasonBody is the optional body shape for cordon / uncordon. We accept
// either an empty body or {"reason":"..."}.
type reasonBody struct {
	Reason string `json:"reason,omitempty"`
}

func (h *handler) cordonDpu(w http.ResponseWriter, r *http.Request) {
	h.cordonImpl(w, r, true)
}

func (h *handler) uncordonDpu(w http.ResponseWriter, r *http.Request) {
	h.cordonImpl(w, r, false)
}

func (h *handler) cordonImpl(w http.ResponseWriter, r *http.Request, cordoned bool) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, 400, errors.New("path: dpu id is required"))
		return
	}
	var req reasonBody
	if body, err := io.ReadAll(r.Body); err == nil && len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(w, 400, fmt.Errorf("parse body: %w", err))
			return
		}
	}
	var err error
	if cordoned {
		err = h.cp.CordonDpu(r.Context(), id, req.Reason)
	} else {
		err = h.cp.UncordonDpu(r.Context(), id, req.Reason)
	}
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"accepted": true, "id": id, "cordoned": cordoned})
}

func (h *handler) listCordoned(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"dpus": h.cp.ListCordonedDpus(r.Context())})
}

// drainDpu (PC-G7) cordons the DPU then rehomes every ENI to a
// least-loaded uncordoned destination. Body shape (JSON, all fields
// optional):
//
//	{
//	  "reason": "rolling reboot",
//	  "parallelism": 4
//	}
//
// Response is the operations.DrainResult envelope. Status code:
//   200 — every ENI migrated; source is cordoned and empty
//   207 — some ENIs failed to migrate; source remains cordoned for
//         retry. Inspect result.failed[] for per-ENI reasons.
func (h *handler) drainDpu(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, 400, errors.New("path: dpu id is required"))
		return
	}
	var req struct {
		Reason      string `json:"reason,omitempty"`
		Parallelism int    `json:"parallelism,omitempty"`
	}
	if body, err := io.ReadAll(r.Body); err == nil && len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			writeErr(w, 400, fmt.Errorf("parse body: %w", err))
			return
		}
	}
	res, err := h.cp.DrainDpu(r.Context(), id, operations.DrainOpts{
		Parallelism: req.Parallelism,
		Reason:      req.Reason,
	})
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	status := http.StatusOK
	if len(res.Failed) > 0 {
		status = http.StatusMultiStatus
	}
	writeJSON(w, status, res)
}

// registerDpu (PB-3) accepts a DpuRegistration body and forwards to the
// service layer. Body shape (JSON):
//
//	{
//	  "limits": { "max_enis": 100, ... },
//	  "capabilities": { "ipv6": true, "service_tunnel": false, ... }
//	}
//
// The {id} path parameter overrides any "id" in the body. At least one
// of limits or capabilities must be set — empty bodies are rejected to
// avoid silently clearing previously-advertised values.
func (h *handler) registerDpu(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, 400, errors.New("path: dpu id is required"))
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if len(body) == 0 {
		writeErr(w, 400, errors.New("empty body; expected {\"limits\":..., \"capabilities\":...}"))
		return
	}
	var req service.DpuRegistration
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	req.ID = id
	if err := h.cp.RegisterDpu(r.Context(), req); err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"accepted": true, "id": id})
}

func (h *handler) getInventory(w http.ResponseWriter, r *http.Request) {
statuses, err := h.cp.GetInventory(r.Context())
if err != nil {
handleServiceErr(w, err)
return
}
// Serialize DpuState as its enum NAME (e.g. "DPU_STATE_UP") so REST
// clients don't have to know the proto enum number. Mirrors the shape
// used by /admin/inventory and /admin/health.
type dpuOut struct {
ID       string            `json:"id"`
Endpoint string            `json:"endpoint,omitempty"`
State    string            `json:"state"`
Labels   map[string]string `json:"labels,omitempty"`
}
out := make([]dpuOut, 0, len(statuses))
for _, s := range statuses {
out = append(out, dpuOut{
ID: s.ID, Endpoint: s.Endpoint,
State:  s.State.String(),
Labels: s.Labels,
})
}
writeJSON(w, 200, map[string]any{"dpus": out})
}

// --- Reconcile ---

func (h *handler) reconcile(w http.ResponseWriter, r *http.Request) {
if err := h.cp.Reconcile(r.Context()); err != nil {
handleServiceErr(w, err)
return
}
writeJSON(w, 200, map[string]any{"ok": true})
}

// --- SimulateApply (PB-2) ---

func (h *handler) simulate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ops []service.SimulateOp `json:"ops"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if len(body) == 0 {
		writeErr(w, 400, errors.New("empty body; expected {\"ops\": [...]}"))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, fmt.Errorf("parse body: %w", err))
		return
	}
	res, err := h.cp.SimulateApply(r.Context(), req.Ops)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// --- ApplyBatch (PC-8) ---

func (h *handler) applyBatch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ops []service.BatchOp `json:"ops"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if len(body) == 0 {
		writeErr(w, 400, errors.New("empty body; expected {\"ops\": [...]}"))
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, 400, fmt.Errorf("parse body: %w", err))
		return
	}
	res, batchErr := h.cp.ApplyBatch(r.Context(), req.Ops)
	if res == nil {
		// Shape-error from the service layer (e.g. unknown kind) —
		// surface the standard service error path so it gets mapped
		// to 400 / 412 / 429 as appropriate.
		handleServiceErr(w, batchErr)
		return
	}
	// Always return the full envelope. Status code:
	//   200 — committed
	//   207 (Multi-Status) — rolled back cleanly
	//   500 — rolled back BUT some compensations failed (dirty)
	status := http.StatusOK
	if !res.Committed {
		status = http.StatusMultiStatus
		if len(res.CompFailures) > 0 {
			status = http.StatusInternalServerError
		}
	}
	writeJSON(w, status, res)
}

// --- Helpers ---

func resolveKind(plural string) string {
if kind, ok := urlKindToStoreKind[plural]; ok {
return kind
}
return plural
}

func readSpec(w http.ResponseWriter, r *http.Request, out any) bool {
body, err := io.ReadAll(r.Body)
if err != nil {
writeErr(w, 400, err)
return false
}
if err := json.Unmarshal(body, out); err != nil {
writeErr(w, 400, fmt.Errorf("invalid spec: %w", err))
return false
}
return true
}

func handleServiceErr(w http.ResponseWriter, err error) {
if errors.Is(err, store.ErrNotFound) {
writeErr(w, 404, errors.New("not found"))
return
}
if errors.Is(err, store.ErrGenerationMismatch) {
writeErr(w, 409, errors.New("generation mismatch"))
return
}
if errors.Is(err, service.ErrInvalidArgument) {
writeErr(w, 400, err)
return
}
if errors.Is(err, service.ErrResourceExhausted) {
writeErr(w, 429, err)
return
}
if errors.Is(err, service.ErrFailedPrecondition) {
writeErr(w, 412, err)
return
}
// Unclassified errors are still 500, but log the underlying reason
// (so operators have a fighting chance to diagnose), and include a
// truncated copy of the message in the response body. This avoids the
// silent "internal" response that hid the real cause.
slog.Error("rest: internal error returned to client", "error", err.Error())
msg := err.Error()
if len(msg) > 240 {
msg = msg[:240] + "…"
}
writeErr(w, 500, errors.New("internal: "+msg))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, err error) {
w.Header().Set("Content-Type", "application/json")
w.WriteHeader(status)
_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}