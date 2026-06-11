// PE-1 DiagnosticsService surface. Keeps internal/service free of
// proto streaming concerns — the gRPC handler in internal/server/grpc
// adapts these to the wire RPCs.
package service

import (
	"context"
	"errors"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/flow"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// DiagnosticsService is the transport-agnostic surface for PE-1
// diagnostics RPCs. dashd's gRPC and REST adapters delegate here;
// tests can drive it directly without spinning up a server.
type DiagnosticsService interface {
	TraceFlow(ctx context.Context, req *dashcenterv1.TraceFlowRequest) (*dashcenterv1.FlowTraceResult, error)
	ExplainMatch(ctx context.Context, req *dashcenterv1.MatchRequest) (*dashcenterv1.MatchExplanation, error)
	ExplainDrift(ctx context.Context, req *dashcenterv1.DriftExplainRequest) (*dashcenterv1.DriftExplanation, error)
	GetAclHitStats(ctx context.Context, req *dashcenterv1.AclStatsRequest) ([]*dashcenterv1.AclStatsPerDpu, error)
	TriggerResimulation(ctx context.Context, req *dashcenterv1.ResimRequest) (*dashcenterv1.Ack, error)
}

// diagnosticsService wraps flow.Engine and maps flow.* sentinel errors
// onto the service-layer sentinels (ErrInvalidArgument / NotFound) so
// REST + gRPC adapters use the existing handleServiceErr / serviceErrToStatus
// helpers without flow-package leakage.
type diagnosticsService struct {
	engine *flow.Engine
}

// NewDiagnostics returns the production DiagnosticsService bound to
// the supplied flow.Engine. Panics when engine is nil — main.go must
// always wire one (tests that don't need diagnostics simply don't
// register the service).
func NewDiagnostics(engine *flow.Engine) DiagnosticsService {
	if engine == nil {
		panic("service.NewDiagnostics: engine is nil")
	}
	return &diagnosticsService{engine: engine}
}

func (d *diagnosticsService) TraceFlow(ctx context.Context, req *dashcenterv1.TraceFlowRequest) (*dashcenterv1.FlowTraceResult, error) {
	res, err := d.engine.TraceFlow(ctx, req)
	return res, mapFlowErr(err)
}

func (d *diagnosticsService) ExplainMatch(ctx context.Context, req *dashcenterv1.MatchRequest) (*dashcenterv1.MatchExplanation, error) {
	res, err := d.engine.ExplainMatch(ctx, req)
	return res, mapFlowErr(err)
}

func (d *diagnosticsService) ExplainDrift(ctx context.Context, req *dashcenterv1.DriftExplainRequest) (*dashcenterv1.DriftExplanation, error) {
	res, err := d.engine.ExplainDrift(ctx, req)
	return res, mapFlowErr(err)
}

func (d *diagnosticsService) GetAclHitStats(ctx context.Context, req *dashcenterv1.AclStatsRequest) ([]*dashcenterv1.AclStatsPerDpu, error) {
	rows, err := d.engine.GetAclHitStats(ctx, req)
	return rows, mapFlowErr(err)
}

func (d *diagnosticsService) TriggerResimulation(ctx context.Context, req *dashcenterv1.ResimRequest) (*dashcenterv1.Ack, error) {
	ack, err := d.engine.TriggerResimulation(ctx, req)
	return ack, mapFlowErr(err)
}

// mapFlowErr translates flow-package sentinels to service / store
// sentinels so the existing transport-layer error mappers
// (handleServiceErr / serviceErrToStatus) translate them to the right
// HTTP / gRPC codes without needing flow-package awareness.
func mapFlowErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, flow.ErrInvalidArgument) {
		return errors.Join(ErrInvalidArgument, err)
	}
	if errors.Is(err, flow.ErrNotFound) {
		return errors.Join(store.ErrNotFound, err)
	}
	return err
}
