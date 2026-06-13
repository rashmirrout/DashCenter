// PE-G6 ClusterService transport-agnostic surface. The gRPC handler in
// internal/server/grpc and the REST handler in internal/server/rest
// both delegate here; tests can drive the interface directly.
//
// PE-G7 additions:
//   - Subscribe accepts SubscribeOptions carrying ResumeAfterEventID
//     (for last-event-id resume) + SubjectName (for per-tenant caps).
//   - Returns *cluster.Subscription so handlers can query
//     TakeDroppedCount() + LastDeliveredEventID() for KIND_DROPPED
//     sentinel synthesis.
package service

import (
	"context"
	"errors"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/cluster"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SubscribeOptions narrows a watcher's behaviour. Mirrors the proto
// WatchTopologyRequest shape so transport adapters can copy fields 1:1.
type SubscribeOptions struct {
	// SubjectName is the auth.Subject.Name of the caller — empty if
	// auth.mode=none. Used by the broadcaster's per-tenant cap.
	SubjectName string

	// ResumeAfterEventID, when non-zero, causes the broadcaster to
	// replay events with id > ResumeAfterEventID instead of starting
	// fresh. See cluster.Broadcaster.Subscribe for resync semantics.
	ResumeAfterEventID uint64
}

// ClusterService is the transport-agnostic surface for ClusterService
// RPCs. Implementations MUST be safe for concurrent use; the production
// wiring constructs exactly one.
type ClusterService interface {
	// GetTopology returns the current snapshot. Pure read, sub-ms.
	GetTopology(ctx context.Context, req *dashcenterv1.GetTopologyRequest) (*dashcenterv1.TopologyResponse, error)

	// Subscribe returns a *cluster.Subscription handle + a cancel
	// function that MUST be called to release resources. Returns
	// cluster.ErrTooManySubscribers when caps are exhausted (transport
	// adapters map this to gRPC RESOURCE_EXHAUSTED / HTTP 429).
	Subscribe(opts SubscribeOptions) (*cluster.Subscription, func(), error)
}

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

func (c *clusterService) Subscribe(opts SubscribeOptions) (*cluster.Subscription, func(), error) {
	return c.bcst.Subscribe(cluster.SubscribeOptions{
		SubjectName:        opts.SubjectName,
		ResumeAfterEventID: opts.ResumeAfterEventID,
	})
}
