// client.go — the gRPC ClusterClient interface + a real implementation
// backed by dashcenter.v1.ClusterServiceClient. Kept behind an
// interface so hub_test.go can swap in a fake that returns scripted
// frames without spinning up a real dashd.
package cluster

import (
	"context"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// ClusterStream is the receive end of an upstream WatchTopology.
// Mirrors the relevant slice of grpc.ServerStreamingClient so tests
// can implement it with a channel.
type ClusterStream interface {
	Recv() (*dashcenterv1.TopologyEvent, error)
}

// ClusterClient is the slice of the generated gRPC client the hub
// uses. Defined here so tests can inject a fake.
type ClusterClient interface {
	GetTopology(ctx context.Context, includeEnis bool) (*dashcenterv1.TopologyResponse, error)
	WatchTopology(ctx context.Context, resumeAfter uint64, includeEnis bool) (ClusterStream, error)
}

// grpcClusterClient adapts dashcenterv1.ClusterServiceClient to the
// ClusterClient interface.
type grpcClusterClient struct {
	c dashcenterv1.ClusterServiceClient
}

// NewGRPCClient wraps the generated client. Pass the conn from
// DialClusterService.
func NewGRPCClient(c dashcenterv1.ClusterServiceClient) ClusterClient {
	return &grpcClusterClient{c: c}
}

func (g *grpcClusterClient) GetTopology(ctx context.Context, includeEnis bool) (*dashcenterv1.TopologyResponse, error) {
	return g.c.GetTopology(ctx, &dashcenterv1.GetTopologyRequest{IncludeEnis: includeEnis})
}

func (g *grpcClusterClient) WatchTopology(ctx context.Context, resumeAfter uint64, includeEnis bool) (ClusterStream, error) {
	s, err := g.c.WatchTopology(ctx, &dashcenterv1.WatchTopologyRequest{
		IncludeEnis:        includeEnis,
		ResumeAfterEventId: resumeAfter,
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}
