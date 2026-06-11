// PE-1 gRPC DiagnosticsService handler. Thin adapter over
// service.DiagnosticsService — the same pattern HaService and
// MigrationService follow.
package grpcserver

import (
	"context"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/grpc"
)

type diagnosticsHandler struct {
	dashcenterv1.UnimplementedDiagnosticsServiceServer
	diag service.DiagnosticsService
}

func registerDiagnostics(gs *grpc.Server, diag service.DiagnosticsService) {
	if diag == nil {
		return
	}
	dashcenterv1.RegisterDiagnosticsServiceServer(gs, &diagnosticsHandler{diag: diag})
}

func (h *diagnosticsHandler) TraceFlow(ctx context.Context, req *dashcenterv1.TraceFlowRequest) (*dashcenterv1.FlowTraceResult, error) {
	res, err := h.diag.TraceFlow(ctx, req)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return res, nil
}

func (h *diagnosticsHandler) ExplainMatch(ctx context.Context, req *dashcenterv1.MatchRequest) (*dashcenterv1.MatchExplanation, error) {
	res, err := h.diag.ExplainMatch(ctx, req)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return res, nil
}

func (h *diagnosticsHandler) ExplainDrift(ctx context.Context, req *dashcenterv1.DriftExplainRequest) (*dashcenterv1.DriftExplanation, error) {
	res, err := h.diag.ExplainDrift(ctx, req)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return res, nil
}

// GetAclHitStats: proto says streaming. Adapter pulls the full slice
// from the service layer (PE-1 scope is "small fleet, paginated future
// work") and fans it into individual stream sends.
func (h *diagnosticsHandler) GetAclHitStats(req *dashcenterv1.AclStatsRequest, stream grpc.ServerStreamingServer[dashcenterv1.AclStatsPerDpu]) error {
	rows, err := h.diag.GetAclHitStats(stream.Context(), req)
	if err != nil {
		return serviceErrToStatus(err)
	}
	for _, row := range rows {
		if err := stream.Send(row); err != nil {
			return err
		}
	}
	return nil
}

func (h *diagnosticsHandler) TriggerResimulation(ctx context.Context, req *dashcenterv1.ResimRequest) (*dashcenterv1.Ack, error) {
	ack, err := h.diag.TriggerResimulation(ctx, req)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ack, nil
}
