// PE-G6 gRPC ClusterService handler. Thin adapter over service.ClusterService.
package grpcserver

import (
	"context"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/grpc"
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

// WatchTopology streams TopologyEvents:
//
//  1. Immediately sends a SNAPSHOT event (the current topology).
//  2. Subscribes to the broadcaster + relays every event.
//  3. Returns cleanly on ctx cancel (client disconnect) or broadcaster
//     close.
func (h *clusterHandler) WatchTopology(req *dashcenterv1.WatchTopologyRequest, stream grpc.ServerStreamingServer[dashcenterv1.TopologyEvent]) error {
	ctx := stream.Context()

	// 1. Initial snapshot.
	snap, err := h.svc.GetTopology(ctx, &dashcenterv1.GetTopologyRequest{IncludeEnis: req.GetIncludeEnis()})
	if err != nil {
		return serviceErrToStatus(err)
	}
	if err := stream.Send(&dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
		Ts:   timestamppb.New(time.Now()),
		Body: &dashcenterv1.TopologyEvent_Snapshot{Snapshot: snap},
	}); err != nil {
		return err
	}

	// 2. Subscribe + drain.
	ch, cancel := h.svc.Subscribe()
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				return nil // broadcaster closed
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}
