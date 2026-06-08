package grpcserver

import (
"context"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"google.golang.org/grpc"
"google.golang.org/grpc/codes"
"google.golang.org/grpc/status"
)

// --- Observability gRPC types (hand-written, matching observability.proto) ---

// DpuStatusRequest mirrors the proto message.
type DpuStatusRequest struct {
DpuIds     []string `protobuf:"bytes,1,rep,name=dpu_ids,json=dpuIds,proto3" json:"dpu_ids,omitempty"`
DeltasOnly bool     `protobuf:"varint,2,opt,name=deltas_only,json=deltasOnly,proto3" json:"deltas_only,omitempty"`
}

func (x *DpuStatusRequest) Reset()         {}
func (x *DpuStatusRequest) String() string { return "DpuStatusRequest" }
func (x *DpuStatusRequest) ProtoMessage()  {}

// DpuStatusResponse wraps a single status report for unary response.
type DpuStatusResponse struct {
Dpus []service.DpuStatus `protobuf:"bytes,1,rep,name=dpus,proto3" json:"dpus,omitempty"`
}

func (x *DpuStatusResponse) Reset()         {}
func (x *DpuStatusResponse) String() string { return "DpuStatusResponse" }
func (x *DpuStatusResponse) ProtoMessage()  {}

// DriftRequest mirrors the proto message.
type DriftRequest struct {
DpuIds []string `protobuf:"bytes,2,rep,name=dpu_ids,json=dpuIds,proto3" json:"dpu_ids,omitempty"`
}

func (x *DriftRequest) Reset()         {}
func (x *DriftRequest) String() string { return "DriftRequest" }
func (x *DriftRequest) ProtoMessage()  {}

// DriftResponse wraps drift items.
type DriftResponse struct {
Items []service.DriftItem `protobuf:"bytes,3,rep,name=items,proto3" json:"items,omitempty"`
}

func (x *DriftResponse) Reset()         {}
func (x *DriftResponse) String() string { return "DriftResponse" }
func (x *DriftResponse) ProtoMessage()  {}

// --- Observability handler ---

type observabilityHandler struct {
obs service.ObservabilityService
}

func registerObservability(gs *grpc.Server, obs service.ObservabilityService) {
h := &observabilityHandler{obs: obs}
gs.RegisterService(&observabilityServiceDesc, h)
}

func (h *observabilityHandler) GetDpuStatus(ctx context.Context, req *DpuStatusRequest) (*DpuStatusResponse, error) {
statuses, err := h.obs.GetDpuStatus(ctx, req.DpuIds)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &DpuStatusResponse{Dpus: statuses}, nil
}

func (h *observabilityHandler) GetDrift(ctx context.Context, req *DriftRequest) (*DriftResponse, error) {
dpuID := ""
if len(req.DpuIds) > 0 {
dpuID = req.DpuIds[0]
}
items, err := h.obs.GetDrift(ctx, dpuID)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &DriftResponse{Items: items}, nil
}

// Stubs for Phase 2 RPCs.
type emptyRequest struct{}
func (x *emptyRequest) Reset()         {}
func (x *emptyRequest) String() string { return "emptyRequest" }
func (x *emptyRequest) ProtoMessage()  {}

type emptyResponse struct{}
func (x *emptyResponse) Reset()         {}
func (x *emptyResponse) String() string { return "emptyResponse" }
func (x *emptyResponse) ProtoMessage()  {}

func unimplemented(_ any, ctx context.Context, _ *emptyRequest) (*emptyResponse, error) {
return nil, status.Errorf(codes.Unimplemented, "not yet implemented")
}

// --- Service Descriptor ---

var observabilityServiceDesc = grpc.ServiceDesc{
ServiceName: "dashcenter.v1.ObservabilityService",
HandlerType: (*observabilityHandler)(nil),
Methods: []grpc.MethodDesc{
{MethodName: "GetDpuStatus", Handler: wrapUnary[DpuStatusRequest, DpuStatusResponse](func(h any, ctx context.Context, req *DpuStatusRequest) (*DpuStatusResponse, error) {
return h.(*observabilityHandler).GetDpuStatus(ctx, req)
})},
{MethodName: "GetDrift", Handler: wrapUnary[DriftRequest, DriftResponse](func(h any, ctx context.Context, req *DriftRequest) (*DriftResponse, error) {
return h.(*observabilityHandler).GetDrift(ctx, req)
})},
},
Streams:  []grpc.StreamDesc{},
Metadata: "dashcenter/v1/observability.proto",
}