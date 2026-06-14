// counters_wiring.go — adapter glue between the generated
// ObservabilityServiceClient and the observability.CountersClient
// interface the hub expects. Lives in server (not in observability)
// to keep the observability package free of a hard dependency on the
// generated proto types beyond the message types.

package server

import (
	"context"

	"github.com/rashmirrout/DashCenter/src/impl-go/console/internal/observability"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/grpc"
)

// observabilityClientAdapter wraps the generated client to satisfy
// observability.CountersClient.
type observabilityClientAdapter struct {
	cli dashcenterv1.ObservabilityServiceClient
}

func (a *observabilityClientAdapter) GetCounters(ctx context.Context, req *dashcenterv1.CounterRequest) (observability.CounterStream, error) {
	s, err := a.cli.GetCounters(ctx, req)
	if err != nil {
		return nil, err
	}
	return &grpcCounterStream{s: s}, nil
}

// grpcCounterStream adapts grpc.ServerStreamingClient to the simpler
// CounterStream interface.
type grpcCounterStream struct {
	s grpc.ServerStreamingClient[dashcenterv1.CounterEvent]
}

func (g *grpcCounterStream) Recv() (*dashcenterv1.CounterEvent, error) {
	return g.s.Recv()
}
