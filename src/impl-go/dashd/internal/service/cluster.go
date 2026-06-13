// PE-G6 ClusterService transport-agnostic surface. The gRPC handler in
// internal/server/grpc and the REST handler in internal/server/rest
// both delegate here; tests can drive the interface directly.
package service

import (
	"context"
	"errors"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/cluster"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ClusterService is the transport-agnostic surface for ClusterService
// RPCs. Implementations MUST be safe for concurrent use; the production
// wiring constructs exactly one.
type ClusterService interface {
	GetTopology(ctx context.Context, req *dashcenterv1.GetTopologyRequest) (*dashcenterv1.TopologyResponse, error)

	// Subscribe returns a channel that delivers TopologyEvents and a
	// cancel function that MUST be called to release resources.
	// Implementations buffer per-subscriber; events are dropped
	// silently when the buffer is full.
	Subscribe() (<-chan *dashcenterv1.TopologyEvent, func())
}

// clusterService wraps the cluster.Aggregator + cluster.Broadcaster and
// owns the producer wiring (registry OnChange, inventory Subscribe,
// elector polling) that converts internal state changes into broadcast
// events.
type clusterService struct {
	agg  *cluster.Aggregator
	bcst *cluster.Broadcaster
}

// NewCluster wraps an aggregator + broadcaster into the service
// interface. The producer wiring (Registry/Inventory/Elector
// subscriptions) is the caller's responsibility — main.go installs the
// callbacks at startup so this constructor stays pure.
func NewCluster(agg *cluster.Aggregator, bcst *cluster.Broadcaster) ClusterService {
	if agg == nil {
		panic("service.NewCluster: aggregator is nil")
	}
	if bcst == nil {
		panic("service.NewCluster: broadcaster is nil")
	}
	return &clusterService{agg: agg, bcst: bcst}
}

func (c *clusterService) GetTopology(ctx context.Context, req *dashcenterv1.GetTopologyRequest) (*dashcenterv1.TopologyResponse, error) {
	if req == nil {
		req = &dashcenterv1.GetTopologyRequest{}
	}
	resp, err := c.agg.Build(ctx, req)
	if err != nil {
		// Distinguish ctx cancel (the gRPC layer maps that to canceled
		// natively) from genuine internal errors.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, err
	}
	if resp.ComputedAt == nil {
		resp.ComputedAt = timestamppb.New(time.Now())
	}
	return resp, nil
}

func (c *clusterService) Subscribe() (<-chan *dashcenterv1.TopologyEvent, func()) {
	return c.bcst.Subscribe()
}
