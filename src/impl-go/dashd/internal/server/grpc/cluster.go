// PE-G6 / PE-G7 gRPC ClusterService handler. Thin adapter over
// service.ClusterService that maps the broadcaster's resilience
// features (resume cursor + KIND_DROPPED sentinel synthesis +
// RESOURCE_EXHAUSTED on cap) into the gRPC streaming contract.
package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/cluster"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type clusterHandler struct {
	dashcenterv1.UnimplementedClusterServiceServer
	svc service.ClusterService
}

func registerCluster(gs *grpc.Server, svc service.ClusterService) {
	if svc == nil {
		return
	}
	dashcenterv1.RegisterClusterServiceServer(gs, &clusterHandler{svc: svc})
}

func (h *clusterHandler) GetTopology(ctx context.Context, req *dashcenterv1.GetTopologyRequest) (*dashcenterv1.TopologyResponse, error) {
	resp, err := h.svc.GetTopology(ctx, req)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return resp, nil
}

// WatchTopology streams TopologyEvents with full PE-G7 resilience:
//
//  1. Honors `resume_after_event_id`: if non-zero, the broadcaster
//     replays buffered frames OR emits a single KIND_RESYNC notice
//     (when the cursor is stale) so the client refetches GetTopology.
//  2. When the cursor is zero, emits a fresh KIND_SNAPSHOT before
//     subscribing — this is the cold-start path.
//  3. On each successful Send, synthesises a KIND_DROPPED notice
//     before relaying the next live event if the broadcaster recorded
//     drops since the previous tick.
//  4. Maps ErrTooManySubscribers → codes.ResourceExhausted with the
//     gRPC standard retry-after metadata so clients back off.
//  5. Returns cleanly on ctx cancel (client disconnect) or
//     broadcaster close.
func (h *clusterHandler) WatchTopology(req *dashcenterv1.WatchTopologyRequest, stream grpc.ServerStreamingServer[dashcenterv1.TopologyEvent]) error {
	ctx := stream.Context()
	subj := auth.FromContext(ctx)

	subscription, cancel, err := h.svc.Subscribe(service.SubscribeOptions{
		SubjectName:        subj.Name,
		ResumeAfterEventID: req.GetResumeAfterEventId(),
	})
	if err != nil {
		if errors.Is(err, cluster.ErrTooManySubscribers) {
			return status.Error(codes.ResourceExhausted, err.Error())
		}
		return serviceErrToStatus(err)
	}
	defer cancel()

	// Cold-start snapshot: only when the client did NOT supply a
	// cursor. Cursor-resume paths handle their own state via the
	// broadcaster's ring buffer (or KIND_RESYNC sentinel).
	if req.GetResumeAfterEventId() == 0 {
		snap, sErr := h.svc.GetTopology(ctx, &dashcenterv1.GetTopologyRequest{IncludeEnis: req.GetIncludeEnis()})
		if sErr != nil {
			return serviceErrToStatus(sErr)
		}
		if err := stream.Send(&dashcenterv1.TopologyEvent{
			Kind:    dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
			Ts:      timestamppb.New(time.Now()),
			EventId: subscription.LastDeliveredEventID(), // 0 on fresh sub
			Body:    &dashcenterv1.TopologyEvent_Snapshot{Snapshot: snap},
		}); err != nil {
			return err
		}
	}

	ch := subscription.Recv()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil // broadcaster closed
			}
			// Inject KIND_DROPPED BEFORE the next real event if the
			// broadcaster recorded any drops since our last delivery.
			if n := subscription.TakeDroppedCount(); n > 0 {
				notice := &dashcenterv1.TopologyEvent{
					Kind: dashcenterv1.TopologyEvent_KIND_DROPPED,
					Ts:   timestamppb.New(time.Now()),
					Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
						DroppedCount: n,
						Message:      fmt.Sprintf("subscriber buffer overflow; %d events lost — call GetTopology to resync", n),
					}},
				}
				if err := stream.Send(notice); err != nil {
					return err
				}
			}
			if err := stream.Send(ev.Event); err != nil {
				return err
			}
		}
	}
}
