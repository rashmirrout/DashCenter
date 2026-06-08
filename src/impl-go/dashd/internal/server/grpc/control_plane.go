package grpcserver

import (
"context"
"encoding/json"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"google.golang.org/grpc"
"google.golang.org/grpc/codes"
"google.golang.org/grpc/status"
)

// controlPlaneHandler implements the ControlPlane gRPC service.
type controlPlaneHandler struct {
cp service.ControlPlaneService
}

func registerControlPlane(gs *grpc.Server, cp service.ControlPlaneService) {
h := &controlPlaneHandler{cp: cp}
gs.RegisterService(&controlPlaneServiceDesc, h)
}

// --- RPC Handlers ---

func (h *controlPlaneHandler) PutVnet(ctx context.Context, spec *dashcenterv1.VnetSpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutVnet(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) PutEni(ctx context.Context, spec *dashcenterv1.EniSpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutEni(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) PutVnetMapping(ctx context.Context, spec *dashcenterv1.VnetMappingSpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutVnetMapping(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) PutAclPolicy(ctx context.Context, spec *dashcenterv1.AclPolicySpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutAclPolicy(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) PutRoutePolicy(ctx context.Context, spec *dashcenterv1.RoutePolicySpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutRoutePolicy(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) PutHaSet(ctx context.Context, spec *dashcenterv1.HaSetSpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutHaSet(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) PutServiceTunnel(ctx context.Context, spec *dashcenterv1.ServiceTunnelSpec) (*dashcenterv1.Ack, error) {
ns := spec.Namespace
res, err := h.cp.PutServiceTunnel(ctx, ns, spec)
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true, Generation: res.Generation}, nil
}

func (h *controlPlaneHandler) Delete(ctx context.Context, ref *dashcenterv1.NameRef) (*dashcenterv1.Ack, error) {
err := h.cp.Delete(ctx, ref.GetNamespace(), ref.GetKind(), ref.GetName())
if err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true}, nil
}

func (h *controlPlaneHandler) Get(ctx context.Context, ref *dashcenterv1.NameRef) (*dashcenterv1.PolicyObject, error) {
item, err := h.cp.Get(ctx, ref.GetNamespace(), ref.GetKind(), ref.GetName())
if err != nil {
return nil, serviceErrToStatus(err)
}
return storedItemToPolicyObject(item)
}

func (h *controlPlaneHandler) Reconcile(ctx context.Context, req *dashcenterv1.ReconcileRequest) (*dashcenterv1.Ack, error) {
if err := h.cp.Reconcile(ctx); err != nil {
return nil, serviceErrToStatus(err)
}
return &dashcenterv1.Ack{Accepted: true}, nil
}

// Stubs for Phase 2 RPCs.
func (h *controlPlaneHandler) PutInventory(ctx context.Context, req *dashcenterv1.PutInventoryRequest) (*dashcenterv1.Ack, error) {
return nil, status.Errorf(codes.Unimplemented, "PutInventory via gRPC not yet implemented; use REST PUT /v1/inventory")
}

func (h *controlPlaneHandler) RegisterDpu(ctx context.Context, req *dashcenterv1.DpuRegistration) (*dashcenterv1.Ack, error) {
return nil, status.Errorf(codes.Unimplemented, "RegisterDpu not yet implemented")
}

func (h *controlPlaneHandler) DeregisterDpu(ctx context.Context, ref *dashcenterv1.NameRef) (*dashcenterv1.Ack, error) {
return nil, status.Errorf(codes.Unimplemented, "DeregisterDpu not yet implemented")
}

func (h *controlPlaneHandler) SimulateApply(ctx context.Context, req *dashcenterv1.PolicyApplyRequest) (*dashcenterv1.SimulateApplyResult, error) {
return nil, status.Errorf(codes.Unimplemented, "SimulateApply not yet implemented")
}

// storedItemToPolicyObject converts a service.StoredItem to a PolicyObject.
func storedItemToPolicyObject(item *service.StoredItem) (*dashcenterv1.PolicyObject, error) {
po := &dashcenterv1.PolicyObject{
Generation: uint64(item.Generation),
}
// Deserialize the spec based on kind using flat struct fields.
switch item.Kind {
case "vnet":
spec := &dashcenterv1.VnetSpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal vnet: %v", err)
}
po.Vnet = spec
case "eni":
spec := &dashcenterv1.EniSpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal eni: %v", err)
}
po.Eni = spec
case "acl_policy":
spec := &dashcenterv1.AclPolicySpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal acl_policy: %v", err)
}
po.AclPolicy = spec
case "route_policy":
spec := &dashcenterv1.RoutePolicySpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal route_policy: %v", err)
}
po.RoutePolicy = spec
case "vnet_mapping":
spec := &dashcenterv1.VnetMappingSpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal vnet_mapping: %v", err)
}
po.VnetMapping = spec
case "ha_set":
spec := &dashcenterv1.HaSetSpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal ha_set: %v", err)
}
po.HaSet = spec
case "service_tunnel":
spec := &dashcenterv1.ServiceTunnelSpec{}
if err := json.Unmarshal(item.Spec, spec); err != nil {
return nil, status.Errorf(codes.Internal, "unmarshal service_tunnel: %v", err)
}
po.ServiceTunnel = spec
default:
return nil, status.Errorf(codes.Internal, "unknown kind: %s", item.Kind)
}
return po, nil
}

// --- Hand-written gRPC Service Descriptor ---
// This replaces the generated control_plane_grpc.pb.go that protoc-gen-go-grpc
// would produce. It registers all unary ControlPlane RPCs.

var controlPlaneServiceDesc = grpc.ServiceDesc{
ServiceName: "dashcenter.v1.ControlPlane",
HandlerType: (*controlPlaneHandler)(nil),
Methods: []grpc.MethodDesc{
{MethodName: "PutVnet", Handler: wrapUnary[dashcenterv1.VnetSpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.VnetSpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutVnet(ctx, req)
})},
{MethodName: "PutEni", Handler: wrapUnary[dashcenterv1.EniSpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.EniSpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutEni(ctx, req)
})},
{MethodName: "PutVnetMapping", Handler: wrapUnary[dashcenterv1.VnetMappingSpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.VnetMappingSpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutVnetMapping(ctx, req)
})},
{MethodName: "PutAclPolicy", Handler: wrapUnary[dashcenterv1.AclPolicySpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.AclPolicySpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutAclPolicy(ctx, req)
})},
{MethodName: "PutRoutePolicy", Handler: wrapUnary[dashcenterv1.RoutePolicySpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.RoutePolicySpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutRoutePolicy(ctx, req)
})},
{MethodName: "PutHaSet", Handler: wrapUnary[dashcenterv1.HaSetSpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.HaSetSpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutHaSet(ctx, req)
})},
{MethodName: "PutServiceTunnel", Handler: wrapUnary[dashcenterv1.ServiceTunnelSpec, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.ServiceTunnelSpec) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutServiceTunnel(ctx, req)
})},
{MethodName: "Delete", Handler: wrapUnary[dashcenterv1.NameRef, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.NameRef) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).Delete(ctx, req)
})},
{MethodName: "Get", Handler: wrapUnary[dashcenterv1.NameRef, dashcenterv1.PolicyObject](func(h any, ctx context.Context, req *dashcenterv1.NameRef) (*dashcenterv1.PolicyObject, error) {
return h.(*controlPlaneHandler).Get(ctx, req)
})},
{MethodName: "Reconcile", Handler: wrapUnary[dashcenterv1.ReconcileRequest, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.ReconcileRequest) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).Reconcile(ctx, req)
})},
{MethodName: "PutInventory", Handler: wrapUnary[dashcenterv1.PutInventoryRequest, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.PutInventoryRequest) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).PutInventory(ctx, req)
})},
{MethodName: "RegisterDpu", Handler: wrapUnary[dashcenterv1.DpuRegistration, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.DpuRegistration) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).RegisterDpu(ctx, req)
})},
{MethodName: "DeregisterDpu", Handler: wrapUnary[dashcenterv1.NameRef, dashcenterv1.Ack](func(h any, ctx context.Context, req *dashcenterv1.NameRef) (*dashcenterv1.Ack, error) {
return h.(*controlPlaneHandler).DeregisterDpu(ctx, req)
})},
{MethodName: "SimulateApply", Handler: wrapUnary[dashcenterv1.PolicyApplyRequest, dashcenterv1.SimulateApplyResult](func(h any, ctx context.Context, req *dashcenterv1.PolicyApplyRequest) (*dashcenterv1.SimulateApplyResult, error) {
return h.(*controlPlaneHandler).SimulateApply(ctx, req)
})},
},
Streams:  []grpc.StreamDesc{},
Metadata: "dashcenter/v1/control_plane.proto",
}

// wrapUnary produces a grpc.methodHandler that decodes the request, calls the
// typed handler function, and returns the response. This is the pattern used
// by protoc-gen-go-grpc generated code.
func wrapUnary[Req any, Resp any](fn func(srv any, ctx context.Context, req *Req) (*Resp, error)) func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
in := new(Req)
if err := dec(in); err != nil {
return nil, err
}
if interceptor == nil {
return fn(srv, ctx, in)
}
info := &grpc.UnaryServerInfo{
Server:     srv,
FullMethod: "", // filled by grpc framework
}
handler := func(ctx context.Context, req any) (any, error) {
return fn(srv, ctx, req.(*Req))
}
return interceptor(ctx, in, info, handler)
}
}