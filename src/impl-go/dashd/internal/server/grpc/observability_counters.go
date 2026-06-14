// observability_counters.go — PE-3c / PD-G5 gRPC handler for
// ObservabilityService.GetCounters.
//
// Mirrors ClusterService.WatchTopology (cluster.go) exactly:
//
//   1. If req.resume_after_event_id == 0 emit a fresh per-DPU snapshot
//      first (KIND_SNAPSHOT frames), then subscribe for live updates.
//   2. If resume_after_event_id > 0 skip the snapshot and let the
//      broadcaster's ring replay (or KIND_RESYNC sentinel) drive the
//      catch-up.
//   3. Synthesise KIND_DROPPED before relaying the next live event if
//      the broadcaster recorded drops since the previous tick.
//   4. Map ErrTooManySubscribers → codes.ResourceExhausted.
//   5. Return cleanly on ctx cancel (client disconnect) or
//      broadcaster close.
//
// The per-DPU filter (req.dpu_ids) is applied two places: snapshot
// (limit store.List to those DPUs) and live (passed into
// broadcaster.SubscribeOptions for fan-out-time filtering). Both must
// be in sync — if a client asks for [dpu-a] they get a snapshot for
// dpu-a only AND only live updates for dpu-a.

package grpcserver

import (
	"errors"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/observability/broadcaster"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CounterReader is the read surface of counters.Store the handler
// needs for the cold-start snapshot. Modelled as an interface so
// tests can inject a fake without dragging the counters package +
// inventory deps in.
type CounterReader interface {
	// ListReports returns one (dpu_id, CounterReport) tuple per
	// known DPU, sorted by dpu_id for stable test output. Implemented
	// by counters.Store via the small adapter in main.go.
	ListReports() []DpuCounterEntry
	// GetReport returns the cached report for a specific DPU.
	GetReport(dpuID string) (*dashcenterv1.CounterReport, bool)
}

// DpuCounterEntry is the snapshot payload returned by
// CounterReader.ListReports. Defined locally to keep grpc package
// free of a hard counters import.
type DpuCounterEntry struct {
	DpuID  string
	Report *dashcenterv1.CounterReport
}

// SetCounterWiring late-injects the broadcaster + reader the
// GetCounters handler depends on. Both nil = handler returns
// codes.FailedPrecondition (matches the admin.SetCountersWiring
// pattern from PE-3b).
//
// MUST be called before grpc.Server.Serve.
func (h *observabilityHandler) SetCounterWiring(bcast *broadcaster.Broadcaster, reader CounterReader) {
	h.cntBcast = bcast
	h.cntReader = reader
}

// GetCounters streams CounterEvent frames to the client. See file
// header comment for the resilience contract.
func (h *observabilityHandler) GetCounters(req *dashcenterv1.CounterRequest, stream grpc.ServerStreamingServer[dashcenterv1.CounterEvent]) error {
	if h.cntBcast == nil || h.cntReader == nil {
		return status.Error(codes.FailedPrecondition, "counters pipeline not wired")
	}
	ctx := stream.Context()
	subj := auth.FromContext(ctx)

	sub, cancel, err := h.cntBcast.Subscribe(broadcaster.SubscribeOptions{
		SubjectName:        subj.Name,
		DpuIDs:             req.GetDpuIds(),
		ResumeAfterEventID: req.GetResumeAfterEventId(),
	})
	if err != nil {
		if errors.Is(err, broadcaster.ErrTooManySubscribers) {
			return status.Error(codes.ResourceExhausted, err.Error())
		}
		return status.Error(codes.Internal, err.Error())
	}
	defer cancel()

	// Cold-start snapshot: only when no cursor. Cursor-resume paths
	// rely on the broadcaster's ring (or KIND_RESYNC sentinel).
	if req.GetResumeAfterEventId() == 0 {
		entries := h.cntReader.ListReports()
		filter := dpuFilterSet(req.GetDpuIds())
		for _, e := range entries {
			if filter != nil {
				if _, ok := filter[e.DpuID]; !ok {
					continue
				}
			}
			snap := &dashcenterv1.CounterEvent{
				Kind: dashcenterv1.CounterEvent_KIND_SNAPSHOT,
				Ts:   timestamppb.New(time.Now()),
				// event_id stays 0 for snapshots — they are not part
				// of the monotonic delta sequence and clients MUST
				// NOT use them as a Last-Event-ID cursor.
				Body: &dashcenterv1.CounterEvent_Report{Report: e.Report},
			}
			if err := stream.Send(snap); err != nil {
				return err
			}
		}
	}

	// Follow loop. follow=false drains anything currently pending in
	// the subscription channel within a short window and exits; the
	// snapshot phase above is the "current state" for that mode.
	if !req.GetFollow() {
		// Drain pending events for ~50ms then exit. This catches any
		// poll-round Put that landed concurrently with our snapshot
		// walk so the one-shot client doesn't strictly need to
		// reconcile with a subsequent follow stream.
		deadline := time.NewTimer(50 * time.Millisecond)
		defer deadline.Stop()
		ch := sub.Recv()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-deadline.C:
				return nil
			case ev, ok := <-ch:
				if !ok {
					return nil
				}
				if err := stream.Send(ev.Event); err != nil {
					return err
				}
			}
		}
	}

	ch := sub.Recv()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			// Inject KIND_DROPPED before the next real event if the
			// broadcaster recorded any drops since the last tick.
			if n := sub.TakeDroppedCount(); n > 0 {
				if err := stream.Send(broadcaster.NewDroppedNotice(n)); err != nil {
					return err
				}
			}
			if err := stream.Send(ev.Event); err != nil {
				return err
			}
		}
	}
}

// dpuFilterSet returns a membership set for ids (nil if no ids). The
// snapshot phase uses this to skip non-matching DPUs.
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
