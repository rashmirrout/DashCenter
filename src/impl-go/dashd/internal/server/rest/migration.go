// PC-G4..G6 REST surface for migration sessions. Mirrors the gRPC
// MigrationServiceServer; StreamMigrationSession is SSE-streamed,
// same shape as /v1/ha/events.
package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/migration"
)

func (h *handler) requireMig(w http.ResponseWriter) bool {
	if h.mig == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("migration coordinator not configured"))
		return false
	}
	return true
}

// migCreatePlan: POST /v1/migrations/plans
// Body: CreateMigrationPlanRequest JSON.
func (h *handler) migCreatePlan(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	var req dashcenterv1.CreateMigrationPlanRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	plan, err := h.mig.CreatePlan(r.Context(), req.GetNamespace(), &req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, planJSON(plan))
}

func (h *handler) migValidatePlan(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	var body struct {
		Plan *dashcenterv1.MigrationPlan `json:"plan"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	p, err := h.mig.ValidatePlan(r.Context(), body.Plan)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, planJSON(p))
}

// migStartSession: POST /v1/migrations/sessions  body: {"plan": {...}}
func (h *handler) migStartSession(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	var body struct {
		Plan *dashcenterv1.MigrationPlan `json:"plan"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	s, err := h.mig.StartSession(r.Context(), body.Plan)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

// migAdvance: POST /v1/migrations/sessions/{id}/advance
// Body: {"expected_generation": N, "to_phase": "MIGRATION_PHASE_SNAPSHOT", "reason": "..."}
func (h *handler) migAdvance(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, 400, errors.New("path: session id is required"))
		return
	}
	var body struct {
		ExpectedGeneration uint64 `json:"expected_generation"`
		ToPhase            any    `json:"to_phase"` // number or enum string
		Reason             string `json:"reason,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	phase, err := parsePhase(body.ToPhase)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s, err := h.mig.AdvancePhase(r.Context(), id, body.ExpectedGeneration, phase, body.Reason)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

func (h *handler) migRollback(w http.ResponseWriter, r *http.Request) {
	h.migAction(w, r, h.mig.Rollback)
}
func (h *handler) migAbort(w http.ResponseWriter, r *http.Request) {
	h.migAction(w, r, h.mig.Abort)
}
func (h *handler) migCommit(w http.ResponseWriter, r *http.Request) {
	h.migAction(w, r, h.mig.Commit)
}

// migAction is the shared body for the three CAS-protected lifecycle
// RPCs (Rollback / Abort / Commit). Body shape is identical:
//   {"expected_generation": N, "reason": "..."}
func (h *handler) migAction(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id string, gen uint64, reason string) (*migration.Session, error)) {
	if !h.requireMig(w) {
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, 400, errors.New("path: session id is required"))
		return
	}
	var body struct {
		ExpectedGeneration uint64 `json:"expected_generation"`
		Reason             string `json:"reason,omitempty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, 400, err)
		return
	}
	s, err := fn(r.Context(), id, body.ExpectedGeneration, body.Reason)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

func (h *handler) migGetSession(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	id := r.PathValue("id")
	s, err := h.mig.Get(r.Context(), id)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

func (h *handler) migListSessions(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	q := r.URL.Query()
	filter := &migration.ListFilter{}
	if v := q.Get("namespace"); v != "" {
		filter.Namespaces = []string{v}
	}
	if v := q.Get("source_dpu_id"); v != "" {
		filter.SourceDPUIDs = []string{v}
	}
	if v := q.Get("target_dpu_id"); v != "" {
		filter.TargetDPUIDs = []string{v}
	}
	if v := q.Get("include_terminal"); v == "true" {
		filter.IncludeTerminal = true
	}
	rows := h.mig.List(r.Context(), filter)
	writeJSON(w, 200, map[string]any{"sessions": rows})
}

// migStreamSession: GET /v1/migrations/sessions/{id}/stream → SSE
func (h *handler) migStreamSession(w http.ResponseWriter, r *http.Request) {
	if !h.requireMig(w) {
		return
	}
	id := r.PathValue("id")
	// Send snapshot first.
	if s, err := h.mig.Get(r.Context(), id); err == nil {
		// Continue to SSE setup.
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeErr(w, http.StatusNotImplemented, errors.New("response writer does not support flushing for SSE"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		b, _ := json.Marshal(s)
		fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
		ch, cancel, _ := h.mig.StreamSession(id)
		defer cancel()
		for {
			select {
			case <-r.Context().Done():
				return
			case update, ok := <-ch:
				if !ok {
					return
				}
				b, _ := json.Marshal(update)
				fmt.Fprintf(w, "data: %s\n\n", string(b))
				flusher.Flush()
			}
		}
	} else {
		handleServiceErr(w, err)
	}
}

// --- helpers ---------------------------------------------------------

// decodeJSON parses request JSON; empty body is OK and produces a
// zero-value target.
func decodeJSON(r *http.Request, dst any) error {
	if r.ContentLength == 0 {
		return nil
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("parse body: %w", err)
	}
	return nil
}

// parsePhase accepts either an enum string ("MIGRATION_PHASE_SNAPSHOT")
// or a numeric ordinal.
func parsePhase(v any) (dashcenterv1.MigrationPhase, error) {
	switch x := v.(type) {
	case string:
		if val, ok := dashcenterv1.MigrationPhase_value[x]; ok {
			return dashcenterv1.MigrationPhase(val), nil
		}
		// Allow numeric string ("3") too.
		if n, err := strconv.Atoi(x); err == nil {
			return dashcenterv1.MigrationPhase(n), nil
		}
		return 0, fmt.Errorf("invalid to_phase %q (use enum name or ordinal)", x)
	case float64:
		return dashcenterv1.MigrationPhase(int(x)), nil
	case nil:
		return 0, errors.New("to_phase is required")
	}
	return 0, fmt.Errorf("invalid to_phase type %T", v)
}

func planJSON(p *dashcenterv1.MigrationPlan) map[string]any {
	if p == nil {
		return nil
	}
	return map[string]any{
		"plan_id":                    p.GetPlanId(),
		"namespace":                  p.GetNamespace(),
		"eni_names":                  p.GetEniNames(),
		"source_dpu_id":              p.GetSourceDpuId(),
		"target_dpu_id":              p.GetTargetDpuId(),
		"strategy":                   p.GetStrategy().String(),
		"max_sync_duration_seconds":  p.GetMaxSyncDurationSeconds(),
		"max_total_duration_seconds": p.GetMaxTotalDurationSeconds(),
		"warnings":                   p.GetWarnings(),
	}
}
