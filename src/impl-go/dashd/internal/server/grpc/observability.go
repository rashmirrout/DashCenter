// Package grpcserver: ObservabilityService RPC adapter.
//
// Mirrors the design of control_plane.go: a thin adapter that translates
// generated dashcenter.v1.ObservabilityService RPCs into transport-agnostic
// calls on service.ObservabilityService, mapping internal types to proto
// envelopes for the wire.
//
// Phase 1 implements the two RPCs the operator actually needs today:
//   * GetDpuStatus  (server-streaming) — one DpuStatusReport per DPU
//   * GetDrift      (unary)            — DriftReport for one or all DPUs
//
// GetFlowStats, GetFlowList, GetCounters, WatchEvents, GetAuditLog are
// inherited from UnimplementedObservabilityServiceServer (codes.Unimplemented)
// and will be wired in Phase 2 milestones PD (audit/counters) and PE
// (diagnostics).
package grpcserver

import (
"context"
"time"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"google.golang.org/grpc"
"google.golang.org/protobuf/types/known/timestamppb"
)

// observabilityHandler implements the generated ObservabilityServiceServer.
// Embedding the Unimplemented base future-proofs us: new RPCs added to the
// proto won't break compilation, they'll just return codes.Unimplemented.
type observabilityHandler struct {
dashcenterv1.UnimplementedObservabilityServiceServer
obs service.ObservabilityService
}

// registerObservability wires the handler into the gRPC server using the
// generated RegisterObservabilityServiceServer — this gives us the correct
// proto-v2 codec and the validated ObservabilityService_ServiceDesc.
func registerObservability(gs *grpc.Server, obs service.ObservabilityService) {
dashcenterv1.RegisterObservabilityServiceServer(gs, &observabilityHandler{obs: obs})
}

// GetDpuStatus is a server-streaming RPC. Per the proto, it emits one
// DpuStatusReport per DPU in the requested set (or every DPU if DpuIds is
// empty), then closes the stream. The Phase 1 implementation is snapshot-only:
// we read the inventory once and stream one report per entry. Phase 2 PD will
// add the "+ deltas" continuous-stream mode by hooking the inventory's
// observation channel.
func (h *observabilityHandler) GetDpuStatus(req *dashcenterv1.DpuStatusRequest, stream dashcenterv1.ObservabilityService_GetDpuStatusServer) error {
ctx := stream.Context()
statuses, err := h.obs.GetDpuStatus(ctx, req.GetDpuIds())
if err != nil {
return serviceErrToStatus(err)
}
reportedAt := timestamppb.New(time.Now().UTC())
for _, s := range statuses {
report := &dashcenterv1.DpuStatusReport{
Identity: &dashcenterv1.DpuIdentity{
DpuId:  s.ID,
Labels: s.Labels,
},
State:       s.State,
StateReason: "",
ReportedAt:  reportedAt,
}
if err := stream.Send(report); err != nil {
return err // network-side; gRPC framework will propagate.
}
}
return nil
}

// GetDrift is a unary RPC that returns a DriftReport for the requested scope.
// The proto accepts an optional list of dpu_ids; Phase 1 treats the first ID
// as the target DPU (or returns an empty report when none is supplied,
// matching the service-layer contract).
func (h *observabilityHandler) GetDrift(ctx context.Context, req *dashcenterv1.DriftRequest) (*dashcenterv1.DriftReport, error) {
dpuID := ""
if ids := req.GetDpuIds(); len(ids) > 0 {
dpuID = ids[0]
}
items, err := h.obs.GetDrift(ctx, dpuID)
if err != nil {
return nil, serviceErrToStatus(err)
}
report := &dashcenterv1.DriftReport{
ComputedAt: timestamppb.New(time.Now().UTC()),
ItemsTotal: int32(len(items)),
Items:      make([]*dashcenterv1.DriftItem, 0, len(items)),
}
for _, it := range items {
report.Items = append(report.Items, &dashcenterv1.DriftItem{
Kind:   driftOpToKind(it.Op),
Target: &dashcenterv1.NameRef{Kind: it.Kind, Name: ""},
DpuId:  it.DpuID,
Detail: it.Detail,
})
}
return report, nil
}

// driftOpToKind maps the service-layer Op string ("apply" | "delete") to the
// proto DriftItem_Kind enum. The mapping is:
//   - "apply"  → DECLARED_NOT_OBSERVED  (dashd wants it, DPU doesn't have it)
//   - "delete" → OBSERVED_NOT_DECLARED  (DPU has it, dashd doesn't)
//   - other    → UNSPECIFIED
// Field-mismatch drift (FIELD_MISMATCH) is a Phase 2 enhancement; Phase 1
// drift is presence-only.
func driftOpToKind(op string) dashcenterv1.DriftItem_Kind {
switch op {
case "apply":
return dashcenterv1.DriftItem_DRIFT_KIND_DECLARED_NOT_OBSERVED
case "delete":
return dashcenterv1.DriftItem_DRIFT_KIND_OBSERVED_NOT_DECLARED
default:
return dashcenterv1.DriftItem_DRIFT_KIND_UNSPECIFIED
}
}