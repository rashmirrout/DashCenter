// PE-1 REST handlers for the Diagnostics surface. Body shapes mirror
// the proto request messages so the same JSON works for `dashctl
// diag *` and curl. Errors are mapped to HTTP status by the shared
// handleServiceErr helper; 503 when diagnostics aren't wired.
package rest

import (
	"errors"
	"net/http"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func (h *handler) requireDiag(w http.ResponseWriter) bool {
	if h.diag == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("diagnostics service not configured"))
		return false
	}
	return true
}

// POST /v1/diagnostics/trace-flow — body: TraceFlowRequest JSON.
func (h *handler) diagTraceFlow(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiag(w) {
		return
	}
	var req dashcenterv1.TraceFlowRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := h.diag.TraceFlow(r.Context(), &req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// POST /v1/diagnostics/explain-match — body: MatchRequest JSON.
func (h *handler) diagExplainMatch(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiag(w) {
		return
	}
	var req dashcenterv1.MatchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := h.diag.ExplainMatch(r.Context(), &req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// POST /v1/diagnostics/explain-drift — body: DriftExplainRequest JSON.
func (h *handler) diagExplainDrift(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiag(w) {
		return
	}
	var req dashcenterv1.DriftExplainRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	res, err := h.diag.ExplainDrift(r.Context(), &req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

// POST /v1/diagnostics/acl-hit-stats — body: AclStatsRequest JSON.
// Returns a JSON array of AclStatsPerDpu so curl-style clients can
// jq over it; the proto wire form would be a server-stream, which
// REST doesn't support natively without SSE.
func (h *handler) diagAclHitStats(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiag(w) {
		return
	}
	var req dashcenterv1.AclStatsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.diag.GetAclHitStats(r.Context(), &req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	if rows == nil {
		rows = []*dashcenterv1.AclStatsPerDpu{}
	}
	writeJSON(w, 200, map[string]any{"items": rows})
}

// POST /v1/diagnostics/trigger-resimulation — body: ResimRequest JSON.
func (h *handler) diagTriggerResim(w http.ResponseWriter, r *http.Request) {
	if !h.requireDiag(w) {
		return
	}
	var req dashcenterv1.ResimRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ack, err := h.diag.TriggerResimulation(r.Context(), &req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, ack)
}
