// sentinels.go — small constructors for the four CounterEvent sentinel
// kinds. Centralising them here keeps the wire shape consistent across
// every producer (broadcaster internals + handler per-subscriber synth)
// and gives tests one place to assert sentinel field semantics.
//
// CounterEvent enum review:
//
//   KIND_KEEPALIVE      — periodic no-op, body = Notice{message:"keepalive"}
//   KIND_DROPPED        — per-subscriber buffer overflow; body = Notice{dropped_count, message}
//   KIND_RATE_LIMITED   — broadcaster-wide suppression notice; body = Notice{suppressed_count, message}
//   KIND_RESYNC         — cursor stale; body = Notice{current_event_id, message}
//
// KIND_DROPPED is intentionally NOT constructed in this file — the
// broadcaster never owns the dropped count; it's a per-subscriber
// atomic that the handler reads (Subscription.TakeDroppedCount) and
// then synthesises a notice via newDroppedNotice at send time.

package broadcaster

import (
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// newKeepaliveNotice constructs a KIND_KEEPALIVE event. Ts is stamped
// by publishImmediate; here it's left nil for the caller's clock.
func newKeepaliveNotice() *dashcenterv1.CounterEvent {
	return &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_KEEPALIVE,
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
			Message: "keepalive",
		}},
	}
}

// newRateLimitedNotice carries the broadcaster's count of suppressed
// events in the last window so operators alerting on stream lag can
// react.
func newRateLimitedNotice(suppressed uint64, ratePerSec float64) *dashcenterv1.CounterEvent {
	return &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_RATE_LIMITED,
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
			SuppressedCount: suppressed,
			Message:         fmt.Sprintf("counter broadcaster suppressed %d events in the last window (rate=%g/s)", suppressed, ratePerSec),
		}},
	}
}

// newResyncNotice tells a subscriber its resume cursor is unusable
// (predates the ring OR is from a previous process). Carries the
// current event_id so the client can compute the gap.
func newResyncNotice(currentEventID uint64, msg string) *dashcenterv1.CounterEvent {
	return &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_RESYNC,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
			Message:        msg,
			CurrentEventId: currentEventID,
		}},
	}
}

// NewDroppedNotice is called by handlers from the gRPC/REST adapters
// when Subscription.TakeDroppedCount returns a non-zero count. Exposed
// (capital N) because it's part of the public consumer API; the
// other three sentinels are emitted from inside the broadcaster.
func NewDroppedNotice(droppedCount uint64) *dashcenterv1.CounterEvent {
	return &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_DROPPED,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
			DroppedCount: droppedCount,
			Message:      fmt.Sprintf("subscriber buffer overflow; %d events lost — refetch snapshot to resync", droppedCount),
		}},
	}
}
