// PE-G6 / PE-G7 REST handlers for ClusterService.
//
// Endpoints (REST :8443):
//
//   GET /v1/cluster/topology                  unary snapshot (?include_enis)
//   GET /v1/cluster/topology/watch            SSE stream
//
// The SSE handler mirrors the gRPC WatchTopology semantics: honors
// Last-Event-ID for cursor resume, synthesises KIND_DROPPED on
// per-subscriber overflow, surfaces ErrTooManySubscribers as
// HTTP 429 + Retry-After, and uses the SAME broadcaster instance as
// the gRPC handler so per-tenant caps are enforced uniformly.
//
// Browsers SHOULD NOT hit dashd directly in production — see
// docs/dashd-features/topology-streaming-design.md (dashw is the
// multiplexer). This handler exists so:
//   - `dashctl topology --follow` can use the simple REST + EventSource
//     contract instead of speaking gRPC, and
//   - operators can curl the SSE stream during incident response.
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/cluster"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (h *handler) requireCluster(w http.ResponseWriter) bool {
	if h.cluster == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("cluster service not configured"))
		return false
	}
	return true
}

// GET /v1/cluster/topology — optional ?include_enis=true.
func (h *handler) getClusterTopology(w http.ResponseWriter, r *http.Request) {
	if !h.requireCluster(w) {
		return
	}
	req := &dashcenterv1.GetTopologyRequest{
		IncludeEnis: r.URL.Query().Get("include_enis") == "true",
	}
	resp, err := h.cluster.GetTopology(r.Context(), req)
	if err != nil {
		handleServiceErr(w, err)
		return
	}
	writeProtoJSON(w, http.StatusOK, resp)
}

// GET /v1/cluster/topology/watch — Server-Sent Events stream.
//
// Cursor resume: clients may set the `Last-Event-ID` request header
// (per the EventSource spec) OR the `?last_event_id=N` query
// parameter to ask the server to replay events with id > N. When the
// cursor is stale the server emits a single KIND_RESYNC event and
// the client MUST refetch GET /v1/cluster/topology before relying on
// further deltas.
//
// Sentinels (clients should handle):
//   - event: snapshot       — full TopologyResponse (cold start OR after RESYNC)
//   - event: keepalive      — periodic no-op (safe to ignore)
//   - event: dropped        — Notice.dropped_count; you missed events; resync
//   - event: rate_limited   — Notice.suppressed_count; informational
//   - event: resync         — refetch GetTopology; discard local state
//
// Caps: ErrTooManySubscribers → HTTP 429 + Retry-After: 30.
func (h *handler) watchClusterTopology(w http.ResponseWriter, r *http.Request) {
	if !h.requireCluster(w) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("response writer does not support streaming"))
		return
	}

	includeEnis := r.URL.Query().Get("include_enis") == "true"
	cursor := parseLastEventID(r)
	subj := auth.FromContext(r.Context())

	subscription, cancel, err := h.cluster.Subscribe(service.SubscribeOptions{
		SubjectName:        subj.Name,
		ResumeAfterEventID: cursor,
	})
	if err != nil {
		if errors.Is(err, cluster.ErrTooManySubscribers) {
			w.Header().Set("Retry-After", "30")
			writeErr(w, http.StatusTooManyRequests, err)
			return
		}
		handleServiceErr(w, err)
		return
	}
	defer cancel()

	// Headers + status — set AFTER Subscribe so a 429 path can write a
	// proper JSON error rather than an empty stream.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)

	// Cold-start snapshot: only when no cursor. Cursor paths get ring
	// replay (or KIND_RESYNC) from the broadcaster directly.
	if cursor == 0 {
		snap, sErr := h.cluster.GetTopology(r.Context(), &dashcenterv1.GetTopologyRequest{IncludeEnis: includeEnis})
		if sErr != nil {
			writeSSE(w, flusher, "error", map[string]string{"error": sErr.Error()})
			return
		}
		snapEv := &dashcenterv1.TopologyEvent{
			Kind: dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
			Ts:   timestamppb.New(time.Now()),
			Body: &dashcenterv1.TopologyEvent_Snapshot{Snapshot: snap},
		}
		if err := writeSSEProto(w, flusher, "snapshot", snapEv); err != nil {
			return
		}
	}

	ch := subscription.Recv()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return // broadcaster closed
			}
			// Synthesise KIND_DROPPED notice if the broadcaster
			// recorded any drops since our last successful send.
			if n := subscription.TakeDroppedCount(); n > 0 {
				notice := &dashcenterv1.TopologyEvent{
					Kind: dashcenterv1.TopologyEvent_KIND_DROPPED,
					Ts:   timestamppb.New(time.Now()),
					Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
						DroppedCount: n,
						Message:      fmt.Sprintf("subscriber buffer overflow; %d events lost — call GetTopology to resync", n),
					}},
				}
				if err := writeSSEProto(w, flusher, "dropped", notice); err != nil {
					return
				}
			}
			if err := writeSSEFrame(w, flusher, ev); err != nil {
				return
			}
		}
	}
}

// parseLastEventID accepts the EventSource standard header OR a query
// param. Header takes precedence (it's the one EventSource auto-sends
// on reconnect). 0 = no resume cursor.
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

// writeSSEFrame writes a broadcaster Frame using the pre-marshalled
// JSON bytes (marshal-once-send-many) plus the standard EventSource
// `id:` line so reconnect cursor flows for free.
func writeSSEFrame(w http.ResponseWriter, flusher http.Flusher, f *cluster.Frame) error {
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

// writeProtoJSON serialises a proto.Message using protojson so field
// names and oneof values are encoded in their canonical wire JSON shape.
func writeProtoJSON(w http.ResponseWriter, status int, msg proto.Message) {
	out, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(msg)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("encode response: %w", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(out)
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

func writeSSEProto(w http.ResponseWriter, flusher http.Flusher, event string, msg proto.Message) error {
	body, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return err
	}
	if event != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", body); err != nil {
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
