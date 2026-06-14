// handler.go — HTTP handlers for the counter Hub.
//
// Routes (registered by router.go):
//   GET /api/console/counters[?dpu=...]        snapshot (one-shot JSON envelope)
//   GET /api/console/counters/stream[?dpu=...] SSE long-lived stream
//   GET /api/console/counters/_stats           debug Stats JSON
//
// SSE format mirrors dashd's counter SSE handler exactly: each frame
// carries `event: <kind>`, `id: <event_id>`, and `data: <protojson>`.
// Source/via are injected by the Hub before the frame reaches here.

package observability

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HTTPHandler bundles the snapshot + stream + stats handlers backed
// by a single Hub. Wire from main.go with NewHTTPHandler(hub).
type HTTPHandler struct {
	hub *Hub
}

// NewHTTPHandler returns a handler wired to hub. hub MUST already be
// Start()ed.
func NewHTTPHandler(hub *Hub) *HTTPHandler {
	return &HTTPHandler{hub: hub}
}

// Snapshot serves GET /api/console/counters.
//
// The hub does not cache snapshots (counter data is intentionally
// short-lived). Instead we drain the most-recent ring entries and
// emit the latest per dpu_id. If no entries are cached, the response
// is `{"reports":[]}` (the operator should wait for the poller to
// fill the cache; PE-3c does NOT proactively fetch on demand).
func (h *HTTPHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	ids := r.URL.Query()["dpu"]
	filter := dpuFilterSet(ids)

	// Snapshot from ring: take the latest report per dpu_id.
	reports := h.hub.LatestPerDpu()
	out := make([]json.RawMessage, 0, len(reports))
	for _, rep := range reports {
		if filter != nil {
			if _, ok := filter[rep.GetDpuId()]; !ok {
				continue
			}
		}
		js, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}.Marshal(rep)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, fmt.Sprintf("encode %s: %v", rep.GetDpuId(), err))
			return
		}
		out = append(out, js)
	}
	body, _ := json.Marshal(map[string]any{"reports": out})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// SSE serves GET /api/console/counters/stream. Same shape as the
// cluster SSE handler.
func (h *HTTPHandler) SSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "response writer does not support streaming")
		return
	}
	cursor := parseLastEventID(r)
	ids := r.URL.Query()["dpu"]
	clientIP := clientIPFor(r)

	watcher, cancel, err := h.hub.Subscribe(SubscribeOptions{
		ClientID:           clientIP,
		DpuIDs:             ids,
		ResumeAfterEventID: cursor,
	})
	if err != nil {
		if errors.Is(err, ErrTooManyWatchers) {
			w.Header().Set("Retry-After", "30")
			writeJSONErr(w, http.StatusTooManyRequests, err.Error())
			return
		}
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Cold-start snapshot from ring.
	if cursor == 0 {
		filter := dpuFilterSet(ids)
		for _, rep := range h.hub.LatestPerDpu() {
			if filter != nil {
				if _, ok := filter[rep.GetDpuId()]; !ok {
					continue
				}
			}
			snap := &dashcenterv1.CounterEvent{
				Kind: dashcenterv1.CounterEvent_KIND_SNAPSHOT,
				Ts:   timestamppb.Now(),
				Body: &dashcenterv1.CounterEvent_Report{Report: rep},
			}
			if frame, err := h.hub.buildFrame(snap); err == nil {
				if err := writeSSEFrame(w, flusher, frame); err != nil {
					return
				}
			}
		}
	}

	ch := watcher.Recv()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if n := watcher.TakeDroppedCount(); n > 0 {
				notice := &dashcenterv1.CounterEvent{
					Kind: dashcenterv1.CounterEvent_KIND_DROPPED,
					Ts:   timestamppb.Now(),
					Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
						DroppedCount: n,
						Message:      fmt.Sprintf("hub buffer overflow; %d events lost", n),
					}},
				}
				if frame, err := h.hub.buildFrame(notice); err == nil {
					if err := writeSSEFrame(w, flusher, frame); err != nil {
						return
					}
				}
			}
			if err := writeSSEFrame(w, flusher, ev); err != nil {
				return
			}
		}
	}
}

// AdminStats serves GET /api/console/counters/_stats. Operator debug
// surface — exposes the Hub's internal counters as JSON.
func (h *HTTPHandler) AdminStats(w http.ResponseWriter, r *http.Request) {
	body, _ := json.Marshal(h.hub.Stats())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// ── helpers ─────────────────────────────────────────────────────────────

// dpuFilterSet is the local copy used by Snapshot+SSE.
func dpuFilterSet(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseLastEventID(r *http.Request) uint64 {
	if hdr := r.Header.Get("Last-Event-ID"); hdr != "" {
		if v, err := strconv.ParseUint(hdr, 10, 64); err == nil {
			return v
		}
	}
	if q := r.URL.Query().Get("last_event_id"); q != "" {
		if v, err := strconv.ParseUint(q, 10, 64); err == nil {
			return v
		}
	}
	return 0
}

func clientIPFor(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := indexComma(v); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	// Strip port from RemoteAddr.
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}

func indexComma(s string) int {
	for i, c := range s {
		if c == ',' {
			return i
		}
	}
	return -1
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`, msg)
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	body, _ := json.Marshal(payload)
	if event != "" {
		_, _ = fmt.Fprintf(w, "event: %s\n", event)
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
	flusher.Flush()
}

func writeSSEFrame(w http.ResponseWriter, flusher http.Flusher, f *Frame) error {
	if f == nil || f.Event == nil {
		return nil
	}
	if name := sseEventName(f.Event.GetKind()); name != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
			return err
		}
	}
	if id := f.Event.GetEventId(); id > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", id); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", f.JSON); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func sseEventName(k dashcenterv1.CounterEvent_Kind) string {
	switch k {
	case dashcenterv1.CounterEvent_KIND_SNAPSHOT:
		return "snapshot"
	case dashcenterv1.CounterEvent_KIND_REPORT:
		return "report"
	case dashcenterv1.CounterEvent_KIND_KEEPALIVE:
		return "keepalive"
	case dashcenterv1.CounterEvent_KIND_DROPPED:
		return "dropped"
	case dashcenterv1.CounterEvent_KIND_RATE_LIMITED:
		return "rate_limited"
	case dashcenterv1.CounterEvent_KIND_RESYNC:
		return "resync"
	}
	return "unknown"
}

// LatestPerDpu walks the ring and returns one CounterReport per
// unique dpu_id (the most-recent). Used by snapshot endpoints and
// cold-start of SSE.
//
// Defined as a Hub method (cross-package callers don't need it).
// Live in handler.go to keep hub.go focused on stream + fan-out.
var _ = time.Second // keep "time" referenced