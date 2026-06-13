// PE-G6 REST handlers for ClusterService. Mirror dashcenter.v1
// request/response shapes verbatim so `dashctl topology` and any HTTP
// client speak the same JSON.
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
// First event = SNAPSHOT (full TopologyResponse), subsequent events =
// typed deltas (peer add/remove, leader change, DPU state change).
//
// Each line is emitted as `event: <kind>\ndata: <json>\n\n` per the
// HTML5 EventSource spec. Optional ?include_enis=true.
func (h *handler) watchClusterTopology(w http.ResponseWriter, r *http.Request) {
	if !h.requireCluster(w) {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("response writer does not support streaming"))
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)

	includeEnis := r.URL.Query().Get("include_enis") == "true"

	// Initial SNAPSHOT.
	snap, err := h.cluster.GetTopology(r.Context(), &dashcenterv1.GetTopologyRequest{IncludeEnis: includeEnis})
	if err != nil {
		writeSSE(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}
	snapEv := &dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
		Body: &dashcenterv1.TopologyEvent_Snapshot{Snapshot: snap},
	}
	if err := writeSSEProto(w, flusher, "snapshot", snapEv); err != nil {
		return
	}

	// Subscribe + drain.
	ch, cancel := h.cluster.Subscribe()
	defer cancel()

	// Keep-alive ticker so proxies don't close the stream during long
	// quiet periods.
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			// SSE comment line; ignored by EventSource clients but
			// keeps the connection warm.
			_, _ = fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return // broadcaster closed
			}
			if err := writeSSEProto(w, flusher, sseEventName(ev.Kind), ev); err != nil {
				return
			}
		}
	}
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
	}
	return "unknown"
}
