// observability_counters.go — PE-3c / PD-G5 REST/SSE surface for the
// counter streaming pipeline.
//
// Routes:
//   GET /v1/observability/counters[?dpu=ID]   one-shot snapshot (JSON array)
//   GET /v1/observability/counters/stream     SSE long-lived stream
//
// The SSE handler mirrors WatchTopology semantics from cluster.go:
//   * Honours Last-Event-ID header AND ?last_event_id= query param.
//   * Emits per-DPU KIND_SNAPSHOT frames first (no cursor); cursor
//     paths get ring replay + KIND_RESYNC sentinel from the broadcaster.
//   * Synthesises KIND_DROPPED before the next live event when the
//     broadcaster recorded drops.
//   * Maps ErrTooManySubscribers → 429 + Retry-After: 30.
//
// The handler is auth-gated by the same middleware as every other REST
// route; main.go's NewWithOptions chain layers auth + audit around the
// router (see server.go). No per-handler auth glue needed here.

package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/observability/broadcaster"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CounterReader is the read surface of counters.Store the handler
// needs for the cold-start snapshot. Mirrors the gRPC handler's
// CounterReader (defined in grpc/observability_counters.go).
type CounterReader interface {
	ListReports() []DpuCounterEntry
	GetReport(dpuID string) (*dashcenterv1.CounterReport, bool)
}

// DpuCounterEntry is the snapshot payload returned by
// CounterReader.ListReports.
type DpuCounterEntry struct {
	DpuID  string
	Report *dashcenterv1.CounterReport
}

// requireCountersWired returns true and is safe to proceed; otherwise
// writes a 503 and returns false. Mirrors requireCluster.
func (h *handler) requireCountersWired(w http.ResponseWriter) bool {
	if h.cntBcast == nil || h.cntReader == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("counter pipeline not wired"))
		return false
	}
	return true
}

// GET /v1/observability/counters — one-shot snapshot. Filters by
// ?dpu= (repeatable). Returns a JSON envelope:
//
//   {"reports":[<CounterReport>, ...]}
//
// Each report is encoded with protojson (UseProtoNames=true) for
// consistency with the SSE stream and the gRPC wire format.
func (h *handler) getCountersSnapshot(w http.ResponseWriter, r *http.Request) {
	if !h.requireCountersWired(w) {
		return
	}
	ids := r.URL.Query()["dpu"]
	filter := dpuFilterSet(ids)
	entries := h.cntReader.ListReports()

	reportJSONs := make([]json.RawMessage, 0, len(entries))
	for _, e := range entries {
		if filter != nil {
			if _, ok := filter[e.DpuID]; !ok {
				continue
			}
		}
		js, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}.Marshal(e.Report)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, fmt.Errorf("encode report %s: %w", e.DpuID, err))
			return
		}
		reportJSONs = append(reportJSONs, js)
	}

	out, err := json.Marshal(map[string]any{"reports": reportJSONs})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// GET /v1/observability/counters/stream — SSE long-lived stream.
func (h *handler) streamCounters(w http.ResponseWriter, r *http.Request) {
	if !h.requireCountersWired(w) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, errors.New("response writer does not support streaming"))
		return
	}
	cursor := parseLastEventID(r)
	ids := r.URL.Query()["dpu"]
	filter := dpuFilterSet(ids)
	subj := auth.FromContext(r.Context())

	sub, cancel, err := h.cntBcast.Subscribe(broadcaster.SubscribeOptions{
		SubjectName:        subj.Name,
		DpuIDs:             ids,
		ResumeAfterEventID: cursor,
	})
	if err != nil {
		if errors.Is(err, broadcaster.ErrTooManySubscribers) {
			w.Header().Set("Retry-After", "30")
			writeErr(w, http.StatusTooManyRequests, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	defer cancel()

	// Headers + status — set AFTER Subscribe so a 429 path can write
	// a proper JSON error rather than an empty stream.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Cold-start: emit per-DPU snapshot when no cursor.
	if cursor == 0 {
		for _, e := range h.cntReader.ListReports() {
			if filter != nil {
				if _, ok := filter[e.DpuID]; !ok {
					continue
				}
			}
			snap := &dashcenterv1.CounterEvent{
				Kind: dashcenterv1.CounterEvent_KIND_SNAPSHOT,
				Ts:   timestamppb.New(time.Now()),
				Body: &dashcenterv1.CounterEvent_Report{Report: e.Report},
			}
			if err := writeSSECounterProto(w, flusher, "snapshot", snap); err != nil {
				return
			}
		}
	}

	ch := sub.Recv()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return // broadcaster closed
			}
			if n := sub.TakeDroppedCount(); n > 0 {
				if err := writeSSECounterProto(w, flusher, "dropped", broadcaster.NewDroppedNotice(n)); err != nil {
					return
				}
			}
			if err := writeSSECounterFrame(w, flusher, ev); err != nil {
				return
			}
		}
	}
}

// writeSSECounterFrame writes a broadcaster Frame using its pre-
// marshalled JSON bytes (marshal-once contract preserved).
func writeSSECounterFrame(w http.ResponseWriter, flusher http.Flusher, f *broadcaster.Frame) error {
	if f == nil || f.Event == nil {
		return nil
	}
	name := counterSSEEventName(f.Event.GetKind())
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

// writeSSECounterProto is the synthetic-event variant used for handler-
// emitted sentinels (snapshot, dropped) that haven't been through the
// broadcaster's marshal-once path.
func writeSSECounterProto(w http.ResponseWriter, flusher http.Flusher, event string, ev *dashcenterv1.CounterEvent) error {
	return writeSSEProto(w, flusher, event, ev)
}

// counterSSEEventName maps a CounterEvent kind to the SSE `event:` line.
func counterSSEEventName(k dashcenterv1.CounterEvent_Kind) string {
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

// dpuFilterSet de-duplicates the ?dpu= query values (empty entries
// degrade to "no filter").
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
