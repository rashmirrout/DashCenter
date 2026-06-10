// PC-G1..G3 REST surface for the HA orchestrator.
//
// Streaming RPCs (TriggerSwitchover, TriggerFailover, WatchHaEvents) are
// surfaced as Server-Sent Events (SSE) over HTTP, which is the kubectl-
// aligned answer for REST clients that can't speak gRPC. Each event is a
// single line:
//
//   data: {<json>}\n\n
//
// Clients close the connection (or get ctx-cancelled by the proxy) to
// unsubscribe.
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
)

func (h *handler) requireHa(w http.ResponseWriter) bool {
	if h.ha == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("HA orchestrator not configured"))
		return false
	}
	return true
}

func (h *handler) listHa(w http.ResponseWriter, r *http.Request) {
	if !h.requireHa(w) {
		return
	}
	writeJSON(w, 200, map[string]any{"ha_sets": h.ha.ListHaSets(r.Context())})
}

func (h *handler) getHa(w http.ResponseWriter, r *http.Request) {
	if !h.requireHa(w) {
		return
	}
	ns := r.PathValue("ns")
	name := r.PathValue("name")
	v, err := h.ha.GetHaSetState(r.Context(), ns, name)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (h *handler) haFlowSyncStats(w http.ResponseWriter, r *http.Request) {
	if !h.requireHa(w) {
		return
	}
	ns := r.URL.Query().Get("namespace")
	name := r.URL.Query().Get("ha_set_name")
	v, err := h.ha.GetFlowSyncStats(r.Context(), ns, name)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, v)
}

// haSwitchover body: {"target_dpu_id":"...", "reason":"..."}
func (h *handler) haSwitchover(w http.ResponseWriter, r *http.Request) {
	if !h.requireHa(w) {
		return
	}
	ns := r.PathValue("ns")
	name := r.PathValue("name")
	var req struct {
		TargetDpuID string `json:"target_dpu_id,omitempty"`
		Reason      string `json:"reason,omitempty"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, fmt.Errorf("parse body: %w", err))
			return
		}
	}
	ch, err := h.ha.TriggerSwitchover(r.Context(), ns, name, req.TargetDpuID, req.Reason)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	streamHaStatus(w, r, ch)
}

// haFailover body: {"failed_dpu_id":"...", "target_dpu_id":"...", "reason":"..."}
func (h *handler) haFailover(w http.ResponseWriter, r *http.Request) {
	if !h.requireHa(w) {
		return
	}
	ns := r.PathValue("ns")
	name := r.PathValue("name")
	var req struct {
		FailedDpuID string `json:"failed_dpu_id"`
		TargetDpuID string `json:"target_dpu_id,omitempty"`
		Reason      string `json:"reason,omitempty"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, 400, fmt.Errorf("parse body: %w", err))
			return
		}
	}
	if req.FailedDpuID == "" {
		writeErr(w, 400, errors.New("failed_dpu_id is required"))
		return
	}
	ch, err := h.ha.TriggerFailover(r.Context(), ns, name, req.FailedDpuID, req.TargetDpuID, req.Reason)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	streamHaStatus(w, r, ch)
}

// streamHaStatus writes each HaScopeStatus row to the SSE response.
// Closes when the orchestrator channel closes or ctx is cancelled.
func streamHaStatus(w http.ResponseWriter, r *http.Request, ch <-chan dashcenterv1.HaScopeStatus) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Plain JSON array fallback for clients without SSE flush.
		var rows []map[string]any
		for s := range ch {
			rows = append(rows, haScopeStatusJSON(s))
		}
		writeJSON(w, 200, map[string]any{"rows": rows})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case <-r.Context().Done():
			return
		case s, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(haScopeStatusJSON(s))
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}
	}
}

// haWatchEvents SSE-streams HaEvent records from the broadcaster.
// Filters: ?namespace=&ha_set_name=&type=
func (h *handler) haWatchEvents(w http.ResponseWriter, r *http.Request) {
	if !h.requireHa(w) {
		return
	}
	q := r.URL.Query()
	filter := service.HaEventFilter{}
	if v := q.Get("namespace"); v != "" {
		filter.Namespaces = []string{v}
	}
	if v := q.Get("ha_set_name"); v != "" {
		filter.HaSetNames = []string{v}
	}
	ch, cancel, err := h.ha.WatchHaEvents(filter)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	defer cancel()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusNotImplemented, errors.New("response writer does not support flushing for SSE"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(haEventJSON(e))
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			flusher.Flush()
		}
	}
}

func haScopeStatusJSON(s dashcenterv1.HaScopeStatus) map[string]any {
	return map[string]any{
		"namespace":      s.GetNamespace(),
		"vdpu_id":        s.GetVdpuId(),
		"ha_scope_id":    s.GetHaScopeId(),
		"dpu_id":         s.GetDpuId(),
		"role":           s.GetRole().String(),
		"is_role_holder": s.GetIsRoleHolder(),
		"flow_sync":      s.GetFlowSync().String(),
		"reason":         s.GetReason(),
	}
}

func haEventJSON(e dashcenterv1.HaEvent) map[string]any {
	return map[string]any{
		"type":          e.GetType().String(),
		"namespace":     e.GetNamespace(),
		"ha_set_name":   e.GetHaSetName(),
		"dpu_id":        e.GetDpuId(),
		"previous_role": e.GetPreviousRole().String(),
		"new_role":      e.GetNewRole().String(),
		"detail_json":   e.GetDetailJson(),
	}
}
