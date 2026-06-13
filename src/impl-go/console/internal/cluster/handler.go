// handler.go — HTTP handlers for the topology-v2 surface in dashw.
//
// Endpoints (registered by server/router.go):
//
//   GET /api/console/topology-v2                  unary snapshot (?include_enis)
//   GET /api/console/topology-v2/stream           SSE stream
//   GET /api/console/topology-v2/ws               WebSocket stream
//
// Both stream transports serve the SAME pre-marshalled frames from
// the Hub. Per-IP + global caps surface as HTTP 429 + Retry-After: 30.
// Last-Event-ID (SSE) / ?last_event_id= (WS) flows seamlessly because
// the hub already exposes resume-cursor replay on Subscribe.
package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/coder/websocket"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HTTPHandler bundles the three handlers + the hub they share. Wire
// it from router.go with NewHTTPHandler(hub, logger).
type HTTPHandler struct {
	hub *Hub
}

// NewHTTPHandler returns the handler bundle.
func NewHTTPHandler(hub *Hub) *HTTPHandler {
	return &HTTPHandler{hub: hub}
}

// GetSnapshot serves GET /api/console/topology-v2 — unary, cached.
func (h *HTTPHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	includeEnis := r.URL.Query().Get("include_enis") == "true"
	resp, err := h.hub.GetTopology(r.Context(), includeEnis)
	if err != nil {
		writeJSONErr(w, http.StatusBadGateway, "snapshot fetch failed: "+err.Error())
		return
	}
	out, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(resp)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "encode: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// SSE serves GET /api/console/topology-v2/stream as Server-Sent Events.
//
// Headers honored:
//   - Last-Event-ID            (EventSource standard for reconnect)
//   - X-Real-IP / X-Forwarded-For (when behind a reverse proxy)
//
// Query params:
//   - ?include_enis=true       (passed through to snapshot)
//   - ?last_event_id=N         (alternative to Last-Event-ID header)
//
// Caps: HTTP 429 + Retry-After: 30 on ErrTooManyWatchers.
func (h *HTTPHandler) SSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "response writer does not support streaming")
		return
	}

	includeEnis := r.URL.Query().Get("include_enis") == "true"
	cursor := parseLastEventID(r)
	clientIP := clientIPFor(r)
	subject := bearerSubject(r)

	watcher, cancel, err := h.hub.Subscribe(SubscribeOptions{
		ClientID:           clientIP,
		SubjectName:        subject,
		ResumeAfterEventID: cursor,
		IncludeEnis:        includeEnis,
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

	// Cold-start snapshot if no cursor.
	if cursor == 0 {
		snap, sErr := h.hub.GetTopology(r.Context(), includeEnis)
		if sErr != nil {
			writeSSE(w, flusher, "error", map[string]string{"error": sErr.Error()})
			return
		}
		ev := &dashcenterv1.TopologyEvent{
			Kind: dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
			Ts:   timestamppb.Now(),
			Body: &dashcenterv1.TopologyEvent_Snapshot{Snapshot: snap},
		}
		if frame, ferr := buildFrame(ev); ferr == nil {
			if err := writeSSEFrame(w, flusher, frame); err != nil {
				return
			}
		}
	}

	ch := watcher.Recv()
	idleTimer := time.NewTimer(h.hub.cfg.IdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-idleTimer.C:
			// Defend against abandoned tabs: close streams that have
			// been idle past the IdleTimeout. The client will
			// reconnect with Last-Event-ID and pick up cleanly.
			return

		case ev, ok := <-ch:
			if !ok {
				return
			}
			if h.hub.cfg.IdleTimeout > 0 {
				idleTimer.Reset(h.hub.cfg.IdleTimeout)
			}
			// Synthesise KIND_DROPPED if the hub dropped any events
			// for this watcher.
			if n := watcher.TakeDroppedCount(); n > 0 {
				notice := dropNotice(n)
				if frame, ferr := buildFrame(notice); ferr == nil {
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

// WebSocket serves GET /api/console/topology-v2/ws as WebSocket.
// Same data, same frames as SSE — exists because some corporate
// proxies strip text/event-stream and WebSocket survives those paths.
func (h *HTTPHandler) WebSocket(w http.ResponseWriter, r *http.Request) {
	includeEnis := r.URL.Query().Get("include_enis") == "true"
	cursor := parseLastEventID(r)
	clientIP := clientIPFor(r)
	subject := bearerSubject(r)

	watcher, cancel, err := h.hub.Subscribe(SubscribeOptions{
		ClientID:           clientIP,
		SubjectName:        subject,
		ResumeAfterEventID: cursor,
		IncludeEnis:        includeEnis,
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

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // origin check is the BFF's CORS layer's job
	})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Cold-start snapshot.
	if cursor == 0 {
		snap, sErr := h.hub.GetTopology(r.Context(), includeEnis)
		if sErr == nil {
			ev := &dashcenterv1.TopologyEvent{
				Kind: dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
				Ts:   timestamppb.Now(),
				Body: &dashcenterv1.TopologyEvent_Snapshot{Snapshot: snap},
			}
			if frame, ferr := buildFrame(ev); ferr == nil {
				_ = conn.Write(r.Context(), websocket.MessageText, frame.JSON)
			}
		}
	}

	ch := watcher.Recv()
	idleTimer := time.NewTimer(h.hub.cfg.IdleTimeout)
	defer idleTimer.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-idleTimer.C:
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if h.hub.cfg.IdleTimeout > 0 {
				idleTimer.Reset(h.hub.cfg.IdleTimeout)
			}
			if n := watcher.TakeDroppedCount(); n > 0 {
				notice := dropNotice(n)
				if frame, ferr := buildFrame(notice); ferr == nil {
					_ = conn.Write(r.Context(), websocket.MessageText, frame.JSON)
				}
			}
			writeCtx, wcancel := context.WithTimeout(r.Context(), 10*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, ev.JSON)
			wcancel()
			if err != nil {
				return
			}
		}
	}
}

// AdminStats serves GET /api/console/topology-v2/_stats — operator
// visibility into the hub's health, drop counts, and per-IP fairness.
func (h *HTTPHandler) AdminStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(h.hub.Stats())
}

// ── helpers ──────────────────────────────────────────────────────────────

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
	// Trust X-Real-IP first (set by realIPMiddleware), then
	// X-Forwarded-For first hop, fall back to RemoteAddr.
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// Take the first IP (closest to client).
		if i := indexComma(v); i > 0 {
			return v[:i]
		}
		return v
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func indexComma(s string) int {
	for i, c := range s {
		if c == ',' {
			return i
		}
	}
	return -1
}

func bearerSubject(r *http.Request) string {
	// Best-effort: derive a per-subject key from the Authorization
	// header so dashd's per-subject cap (on the upstream stream) is
	// stable across reconnects. For tokens we use the 8-char prefix
	// the dashd audit code already exposes — same shape, same key.
	hdr := r.Header.Get("Authorization")
	if len(hdr) < 8 || hdr[:7] != "Bearer " {
		return ""
	}
	tok := hdr[7:]
	if len(tok) > 8 {
		return "bearer:" + tok[:8]
	}
	return "bearer:" + tok
}

func dropNotice(n uint64) *dashcenterv1.TopologyEvent {
	return &dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_DROPPED,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
			DroppedCount: n,
			Message:      fmt.Sprintf("dashw hub overflow; %d events lost — re-fetch GetTopology", n),
		}},
	}
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
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
	name := sseEventName(f.Event.GetKind())
	if name != "" {
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

func sseEventName(k dashcenterv1.TopologyEvent_Kind) string {
	switch k {
	case dashcenterv1.TopologyEvent_KIND_SNAPSHOT:
		return "snapshot"
	case dashcenterv1.TopologyEvent_KIND_PEER_ADDED:
		return "peer_added"
	case dashcenterv1.TopologyEvent_KIND_PEER_REMOVED:
		return "peer_removed"
	case dashcenterv1.TopologyEvent_KIND_PEER_UPDATED:
		return "peer_updated"
	case dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED:
		return "leader_changed"
	case dashcenterv1.TopologyEvent_KIND_DPU_STATE:
		return "dpu_state"
	case dashcenterv1.TopologyEvent_KIND_DPU_ADDED:
		return "dpu_added"
	case dashcenterv1.TopologyEvent_KIND_DPU_REMOVED:
		return "dpu_removed"
	case dashcenterv1.TopologyEvent_KIND_KEEPALIVE:
		return "keepalive"
	case dashcenterv1.TopologyEvent_KIND_DROPPED:
		return "dropped"
	case dashcenterv1.TopologyEvent_KIND_RATE_LIMITED:
		return "rate_limited"
	case dashcenterv1.TopologyEvent_KIND_RESYNC:
		return "resync"
	}
	return "unknown"
}
