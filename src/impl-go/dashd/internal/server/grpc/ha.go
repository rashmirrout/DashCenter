// PC-G1..G3 gRPC HaService handler. Thin adapter over service.HaService.
package grpcserver

import (
	"context"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type haHandler struct {
	dashcenterv1.UnimplementedHaServiceServer
	ha service.HaService
}

// registerHa wires the HaServiceServer into the gRPC server. Skipped
// when ha is nil (legacy test wiring); the embedded
// UnimplementedHaServiceServer then keeps the RPCs returning
// codes.Unimplemented which matches Phase 1 behaviour.
func registerHa(gs *grpc.Server, ha service.HaService) {
	if ha == nil {
		return
	}
	dashcenterv1.RegisterHaServiceServer(gs, &haHandler{ha: ha})
}

func (h *haHandler) GetHaSetState(ctx context.Context, req *dashcenterv1.HaSetStateRequest) (*dashcenterv1.HaSetStatus, error) {
	v, err := h.ha.GetHaSetState(ctx, req.GetNamespace(), req.GetHaSetName())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	out := &dashcenterv1.HaSetStatus{
		Namespace: v.Namespace, Name: v.Name, Mode: v.Mode, VirtualIp: v.VirtualIP, FlowSync: v.FlowSync,
	}
	for _, m := range v.Members {
		out.Members = append(out.Members, &dashcenterv1.HaSetMember{
			DpuId: m.DpuID, Role: m.Role, Reason: m.Reason,
		})
	}
	return out, nil
}

func (h *haHandler) GetHaScopeState(ctx context.Context, req *dashcenterv1.HaScopeStateRequest) (*dashcenterv1.HaScopeStatus, error) {
	rows, err := h.ha.GetHaScopeState(ctx, req.GetNamespace(), req.GetVdpuId(), req.GetHaScopeId())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	if len(rows) == 0 {
		return nil, status.Errorf(codes.NotFound, "no HA scope match")
	}
	// The proto-level call expects ONE HaScopeStatus; we return the
	// current role-holder (or the first row when none holds the role).
	for _, r := range rows {
		if r.IsRoleHolder {
			return haScopeViewToProto(r), nil
		}
	}
	return haScopeViewToProto(rows[0]), nil
}

func haScopeViewToProto(v *service.HaScopeView) *dashcenterv1.HaScopeStatus {
	return &dashcenterv1.HaScopeStatus{
		Namespace: v.Namespace, VdpuId: v.VdpuID, HaScopeId: v.ScopeID, DpuId: v.DpuID,
		Role: v.Role, IsRoleHolder: v.IsRoleHolder, FlowSync: v.FlowSync, Reason: v.Reason,
	}
}

func (h *haHandler) TriggerSwitchover(req *dashcenterv1.SwitchoverRequest, stream grpc.ServerStreamingServer[dashcenterv1.HaScopeStatus]) error {
	ch, err := h.ha.TriggerSwitchover(stream.Context(), req.GetNamespace(), req.GetHaSetName(), req.GetTargetDpuId(), req.GetReason())
	if err != nil {
		return serviceErrToStatus(err)
	}
	for s := range ch {
		ss := s
		if err := stream.Send(&ss); err != nil {
			return err
		}
	}
	return nil
}

func (h *haHandler) TriggerFailover(req *dashcenterv1.FailoverRequest, stream grpc.ServerStreamingServer[dashcenterv1.HaScopeStatus]) error {
	ch, err := h.ha.TriggerFailover(stream.Context(), req.GetNamespace(), req.GetHaSetName(), req.GetFailedDpuId(), req.GetTargetDpuId(), req.GetReason())
	if err != nil {
		return serviceErrToStatus(err)
	}
	for s := range ch {
		ss := s
		if err := stream.Send(&ss); err != nil {
			return err
		}
	}
	return nil
}

func (h *haHandler) WatchHaEvents(req *dashcenterv1.HaEventFilter, stream grpc.ServerStreamingServer[dashcenterv1.HaEvent]) error {
	ch, cancel, err := h.ha.WatchHaEvents(service.HaEventFilter{
		Namespaces: req.GetNamespaces(), HaSetNames: req.GetHaSetNames(), Types: req.GetTypes(),
	})
	if err != nil {
		return serviceErrToStatus(err)
	}
	defer cancel()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case e, ok := <-ch:
			if !ok {
				return nil
			}
			ee := e
			if err := stream.Send(&ee); err != nil {
				return err
			}
		}
	}
}

func (h *haHandler) GetFlowSyncStats(ctx context.Context, req *dashcenterv1.FlowSyncStatsRequest) (*dashcenterv1.FlowSyncStats, error) {
	v, err := h.ha.GetFlowSyncStats(ctx, req.GetNamespace(), req.GetHaSetName())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	// FlowSyncStats has open scalar fields; we surface state via a
	// detail tag (the proto carries opaque counters that PE will fill
	// with real DASH telemetry).
	return &dashcenterv1.FlowSyncStats{Namespace: v.Namespace, HaSetName: v.HaSetName}, nil
}
