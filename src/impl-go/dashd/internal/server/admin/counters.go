// PE-3b admin endpoints for the counter polling pipeline.
//
//   GET  /admin/counters[?dpu=ID]          dump cached reports
//   POST /admin/counters/poll-interval     {"interval":"3s"} runtime knob
//   POST /admin/counters/enable            {"enabled":true|false}
//
// These run on the same unauthenticated admin port as every other
// /admin/* endpoint (trusted-management-network model). They use the
// late-wired counters.Store + counters.Poller injected by main.go via
// SetCountersWiring \u2014 nil means "counter pipeline disabled at boot"
// and every endpoint returns 503.
package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	dccnt "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/counters"
)

// CountersStore is the read view of the counter cache that the admin
// endpoints depend on. Defined here as an interface so tests can
// substitute a stub without dragging in the real Store; the real
// *counters.Store satisfies it.
type CountersStore interface {
	Get(dpuID string) (*dccnt.Entry, bool)
	List() []*dccnt.Entry
}

// CountersPoller is the read+control surface of the polling loop the
// admin endpoints expose. Real *counters.Poller satisfies it.
type CountersPoller interface {
	Enabled() bool
	Interval() time.Duration
	SetEnabled(bool)
	SetInterval(time.Duration)
}

// SetCountersWiring injects the per-DPU counter cache + poller control
// surface. Either argument may be nil (e.g. counters disabled at boot
// with no store created) \u2014 the endpoints return 503 in that case.
// Idempotent + safe to call before Serve.
func (s *Server) SetCountersWiring(store CountersStore, poller CountersPoller) {
	s.cntStore = store
	s.cntPoller = poller
	if s.handler != nil {
		s.handler.cntStore = store
		s.handler.cntPoller = poller
	}
}

// countersList serves GET /admin/counters[?dpu=ID].
//
// Without ?dpu: returns every cached entry sorted by dpu_id.
// With ?dpu=ID: returns the single entry or 404.
func (h *handler) countersList(w http.ResponseWriter, r *http.Request) {
	if h.cntStore == nil {
		writeErr(w, http.StatusServiceUnavailable, "counters pipeline not wired")
		return
	}
	if dpuID := r.URL.Query().Get("dpu"); dpuID != "" {
		entry, ok := h.cntStore.Get(dpuID)
		if !ok {
			writeErr(w, http.StatusNotFound, "no counter report cached for dpu "+dpuID)
			return
		}
		writeJSON(w, http.StatusOK, marshalEntry(entry))
		return
	}
	entries := h.cntStore.List()
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, marshalEntry(e))
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// countersPollInterval serves POST /admin/counters/poll-interval.
//
// Body: {"interval": "3s"} (time.ParseDuration syntax). Values below
// counters.MinInterval are clamped by the poller; this handler only
// rejects empty + unparseable input.
func (h *handler) countersPollInterval(w http.ResponseWriter, r *http.Request) {
	if h.cntPoller == nil {
		writeErr(w, http.StatusServiceUnavailable, "counters pipeline not wired")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var req struct {
		Interval string `json:"interval"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "decode: "+err.Error())
		return
	}
	if req.Interval == "" {
		writeErr(w, http.StatusBadRequest, "interval is required")
		return
	}
	dur, err := time.ParseDuration(req.Interval)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "parse interval: "+err.Error())
		return
	}
	if dur <= 0 {
		writeErr(w, http.StatusBadRequest, "interval must be > 0")
		return
	}
	h.cntPoller.SetInterval(dur)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"interval": h.cntPoller.Interval().String(),
	})
}

// countersEnable serves POST /admin/counters/enable.
//
// Body: {"enabled": true|false}. Returns the new state.
func (h *handler) countersEnable(w http.ResponseWriter, r *http.Request) {
	if h.cntPoller == nil {
		writeErr(w, http.StatusServiceUnavailable, "counters pipeline not wired")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<10))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "decode: "+err.Error())
		return
	}
	if req.Enabled == nil {
		writeErr(w, http.StatusBadRequest, "enabled is required (bool)")
		return
	}
	h.cntPoller.SetEnabled(*req.Enabled)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"enabled": h.cntPoller.Enabled(),
	})
}

// marshalEntry converts a counters.Entry to a JSON-friendly shape that
// also surfaces the per-ENI + per-VNET sub-rollups. The CounterReport
// is rendered via protojson so the operator sees stable field names
// matching the proto schema.
func marshalEntry(e *dccnt.Entry) map[string]any {
	opts := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

	main := map[string]any{}
	if raw, err := opts.Marshal(e.Report); err == nil {
		_ = json.Unmarshal(raw, &main)
	}

	perEni := map[string]map[string]any{}
	for k, v := range e.PerEni {
		m := map[string]any{}
		if raw, err := opts.Marshal(v); err == nil {
			_ = json.Unmarshal(raw, &m)
		}
		perEni[k] = m
	}
	perVnet := map[string]map[string]any{}
	for k, v := range e.PerVnet {
		m := map[string]any{}
		if raw, err := opts.Marshal(v); err == nil {
			_ = json.Unmarshal(raw, &m)
		}
		perVnet[k] = m
	}

	return map[string]any{
		"dpu_id":    e.DpuID,
		"update_at": e.UpdateAt.UTC().Format(time.RFC3339Nano),
		"report":    main,
		"per_eni":   perEni,
		"per_vnet":  perVnet,
	}
}
