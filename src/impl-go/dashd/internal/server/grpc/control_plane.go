// Package grpcserver: ControlPlane RPC adapter.
//
// This file is a thin adapter layer that translates between the wire-level
// generated gRPC ControlPlane service (gen/go/dashcenter/v1) and the
// transport-agnostic ControlPlaneService in internal/service. All business
// logic, validation, and persistence lives in the service layer; this file
// only does request demux, error→status code mapping, and result envelope
// construction.
package grpcserver

import (
	"context"
	"encoding/json"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// controlPlaneHandler implements the generated ControlPlaneServer interface.
// It embeds UnimplementedControlPlaneServer so streaming RPCs and Phase 2
// methods automatically return codes.Unimplemented; we only override the
// methods that have real Phase 1 backing logic.
type controlPlaneHandler struct {
	dashcenterv1.UnimplementedControlPlaneServer
	cp service.ControlPlaneService
}

// registerControlPlane installs the handler on the gRPC server using the
// generated RegisterControlPlaneServer entry point. This is the supported
// path — it wires up the proper proto-v2 codec and the validated
// ControlPlane_ServiceDesc from control_plane_grpc.pb.go.
func registerControlPlane(gs *grpc.Server, cp service.ControlPlaneService) {
	dashcenterv1.RegisterControlPlaneServer(gs, &controlPlaneHandler{cp: cp})
}

// ackFor produces a uniform Ack envelope for successful Put operations.
// The generated Ack message does not have Accepted/Generation fields (those
// were Phase 1 shortcuts in the previous hand-written stubs); the canonical
// success carrier is TxnId, which we populate from the store generation so
// clients can correlate Put → Get round-trips.
func ackFor(res *service.PutResult) *dashcenterv1.Ack {
	if res == nil {
		return &dashcenterv1.Ack{}
	}
	return &dashcenterv1.Ack{TxnId: fmt.Sprintf("g%d", res.Generation)}
}

// --- Per-kind Put handlers -----------------------------------------------------
// Each handler extracts the namespace from the spec (the proto carries it as a
// first-class field on every Put), delegates to the service layer, and maps
// errors to gRPC status codes.

func (h *controlPlaneHandler) PutVnet(ctx context.Context, spec *dashcenterv1.VnetSpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutVnet(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

func (h *controlPlaneHandler) PutEni(ctx context.Context, spec *dashcenterv1.EniSpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutEni(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

func (h *controlPlaneHandler) PutVnetMapping(ctx context.Context, spec *dashcenterv1.VnetMappingSpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutVnetMapping(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

func (h *controlPlaneHandler) PutAclPolicy(ctx context.Context, spec *dashcenterv1.AclPolicySpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutAclPolicy(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

func (h *controlPlaneHandler) PutRoutePolicy(ctx context.Context, spec *dashcenterv1.RoutePolicySpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutRoutePolicy(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

func (h *controlPlaneHandler) PutHaSet(ctx context.Context, spec *dashcenterv1.HaSetSpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutHaSet(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

func (h *controlPlaneHandler) PutServiceTunnel(ctx context.Context, spec *dashcenterv1.ServiceTunnelSpec) (*dashcenterv1.Ack, error) {
	res, err := h.cp.PutServiceTunnel(ctx, spec.GetNamespace(), spec)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return ackFor(res), nil
}

// --- Get / Delete / Reconcile --------------------------------------------------

func (h *controlPlaneHandler) Delete(ctx context.Context, ref *dashcenterv1.NameRef) (*dashcenterv1.Ack, error) {
	if err := h.cp.Delete(ctx, ref.GetNamespace(), ref.GetKind(), ref.GetName()); err != nil {
		return nil, serviceErrToStatus(err)
	}
	return &dashcenterv1.Ack{}, nil
}

func (h *controlPlaneHandler) Get(ctx context.Context, ref *dashcenterv1.NameRef) (*dashcenterv1.PolicyObject, error) {
	item, err := h.cp.Get(ctx, ref.GetNamespace(), ref.GetKind(), ref.GetName())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return storedItemToPolicyObject(item)
}

func (h *controlPlaneHandler) Reconcile(ctx context.Context, _ *dashcenterv1.ReconcileRequest) (*dashcenterv1.Ack, error) {
	if err := h.cp.Reconcile(ctx); err != nil {
		return nil, serviceErrToStatus(err)
	}
	return &dashcenterv1.Ack{}, nil
}

// storedItemToPolicyObject converts a service.StoredItem (transport-agnostic)
// into a wire PolicyObject. The generated PolicyObject has an `object` oneof
// across the 7 kinds, so we use the typed PolicyObject_* setters rather than
// flat field assignment. Unknown kinds return Internal (the service layer
// should never produce one — it validates kinds at Put time).
func storedItemToPolicyObject(item *service.StoredItem) (*dashcenterv1.PolicyObject, error) {
	if item == nil {
		return nil, status.Errorf(codes.Internal, "nil stored item")
	}
	po := &dashcenterv1.PolicyObject{
		Generation: uint64(item.Generation),
	}
	switch item.Kind {
	case "vnet":
		spec := &dashcenterv1.VnetSpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal vnet: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_Vnet{Vnet: spec}
	case "eni":
		spec := &dashcenterv1.EniSpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal eni: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_Eni{Eni: spec}
	case "vnet_mapping":
		spec := &dashcenterv1.VnetMappingSpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal vnet_mapping: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_VnetMapping{VnetMapping: spec}
	case "acl_policy":
		spec := &dashcenterv1.AclPolicySpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal acl_policy: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_AclPolicy{AclPolicy: spec}
	case "route_policy":
		spec := &dashcenterv1.RoutePolicySpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal route_policy: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_RoutePolicy{RoutePolicy: spec}
	case "ha_set":
		spec := &dashcenterv1.HaSetSpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal ha_set: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_HaSet{HaSet: spec}
	case "service_tunnel":
		spec := &dashcenterv1.ServiceTunnelSpec{}
		if err := json.Unmarshal(item.Spec, spec); err != nil {
			return nil, status.Errorf(codes.Internal, "unmarshal service_tunnel: %v", err)
		}
		po.Object = &dashcenterv1.PolicyObject_ServiceTunnel{ServiceTunnel: spec}
	default:
		return nil, status.Errorf(codes.Internal, "unknown kind: %s", item.Kind)
	}
	return po, nil
}

// PutInventory / DeregisterDpu are NOT overridden here — the embedded
// UnimplementedControlPlaneServer returns codes.Unimplemented for them
// automatically, which is the correct Phase 1 behavior. They are wired
// to Phase 2 milestones (PC operations).

// RegisterDpu (PB-3) attaches advertised DpuCapacityLimits +
// DpuCapabilities to a previously-registered DPU. Used by DPU agents'
// phone-home flow and by the bootstrap container in deploy/dashctl-fleet.
func (h *controlPlaneHandler) RegisterDpu(ctx context.Context, reg *dashcenterv1.DpuRegistration) (*dashcenterv1.Ack, error) {
	if reg == nil || reg.GetIdentity() == nil {
		return nil, status.Errorf(codes.InvalidArgument, "identity is required")
	}
	input := service.DpuRegistration{
		ID:           reg.GetIdentity().GetDpuId(),
		Limits:       reg.GetLimits(),
		Capabilities: reg.GetCapabilities(),
	}
	if err := h.cp.RegisterDpu(ctx, input); err != nil {
		return nil, serviceErrToStatus(err)
	}
	return &dashcenterv1.Ack{TxnId: "register:" + input.ID}, nil
}

// SimulateApply (PB-2) is the gRPC dry-run admission endpoint. The proto
// is unary single-op (one PolicyApplyRequest carrying one PolicyObject),
// so we translate to a one-element service.SimulateOp batch and project
// the result back onto the wire SimulateApplyResult shape. For
// multi-op batches, clients should use the REST POST /v1/simulate
// endpoint which carries an ops[] array.
func (h *controlPlaneHandler) SimulateApply(ctx context.Context, req *dashcenterv1.PolicyApplyRequest) (*dashcenterv1.SimulateApplyResult, error) {
	if req == nil {
		return nil, status.Errorf(codes.InvalidArgument, "request is nil")
	}
	op, err := simulateOpFromProto(req)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	res, err := h.cp.SimulateApply(ctx, []service.SimulateOp{op})
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	out := &dashcenterv1.SimulateApplyResult{
		WouldSucceed:     res.WouldSucceed,
		ValidationErrors: res.ValidationErrors,
	}
	for _, row := range res.PerDpuImpact {
		out.PerDpuImpact = append(out.PerDpuImpact, &dashcenterv1.SimulatedDpuImpact{
			DpuId:                 row.DpuID,
			DeltaEnis:             row.DeltaEnis,
			DeltaVnetMappings:     row.DeltaVnetMappings,
			DeltaAclRules:         row.DeltaAclRules,
			ExceedsCapacity:       row.ExceedsCapacity,
			CapacityFailureReason: row.Reason,
		})
	}
	return out, nil
}

// simulateOpFromProto translates a PolicyApplyRequest into the
// service-layer SimulateOp representation. PB-2 supports
// eni/vnet_mapping/acl_policy only; other kinds (vnet/route/ha/tunnel)
// return InvalidArgument until PB-3 broadens admission gating.
func simulateOpFromProto(req *dashcenterv1.PolicyApplyRequest) (service.SimulateOp, error) {
	op := service.SimulateOp{}
	switch req.GetAction() {
	case dashcenterv1.PolicyApplyRequest_ACTION_PUT:
		op.Action = "put"
	case dashcenterv1.PolicyApplyRequest_ACTION_DELETE:
		op.Action = "delete"
	default:
		return op, fmt.Errorf("action must be ACTION_PUT or ACTION_DELETE")
	}
	obj := req.GetObject()
	if obj == nil {
		return op, fmt.Errorf("object is nil")
	}
	switch x := obj.GetObject().(type) {
	case *dashcenterv1.PolicyObject_Eni:
		op.Kind = "eni"
		op.EniSpec = x.Eni
		op.Namespace = x.Eni.GetNamespace()
		op.Name = x.Eni.GetName()
	case *dashcenterv1.PolicyObject_VnetMapping:
		op.Kind = "vnet_mapping"
		op.VnetMappingSpec = x.VnetMapping
		op.Namespace = x.VnetMapping.GetNamespace()
		if op.Action == "delete" {
			name := x.VnetMapping.GetVnetName()
			if ip := x.VnetMapping.GetIpAddress(); ip != "" {
				name = name + "-" + ip
			}
			op.Name = name
		}
	case *dashcenterv1.PolicyObject_AclPolicy:
		op.Kind = "acl_policy"
		op.AclPolicySpec = x.AclPolicy
		op.Namespace = x.AclPolicy.GetNamespace()
		op.Name = x.AclPolicy.GetName()
	default:
		return op, fmt.Errorf("PB-2 SimulateApply supports eni|vnet_mapping|acl_policy only")
	}
	return op, nil
}