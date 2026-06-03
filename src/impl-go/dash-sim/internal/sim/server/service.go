// Package server implements the dashsim.v1.DashSim gRPC service backed by
// internal/sim/model.Store and internal/sim/events.Bus.
//
// Every handler:
//
//  1. Calls faults.Apply(rpcName) — for fault-injection by /admin/faults.
//  2. Delegates to the model package.
//  3. Returns an Ack with the txn_id assigned by the model (matches the
//     txn_id delivered on Subscribe).
//
// Validation errors come back through Ack{accepted:false, error:msg}; this is
// consistent with how a real DPU agent surfaces SAI rejection codes.
package server

import (
	"context"
	"errors"
	"time"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements dashsimv1.DashSimServer.
type Server struct {
	dashsimv1.UnimplementedDashSimServer

	store    *model.Store
	bus      *events.Bus
	faults   *faults.Injector
	counters *counters.Registry
}

// New constructs a Server. All four dependencies must be non-nil.
func New(store *model.Store, bus *events.Bus, faults *faults.Injector, counters *counters.Registry) *Server {
	return &Server{
		store:    store,
		bus:      bus,
		faults:   faults,
		counters: counters,
	}
}

func nowNs() int64 { return time.Now().UnixNano() }

func ack(txn string, err error) *dashsimv1.Ack {
	if err != nil {
		return &dashsimv1.Ack{TxnId: txn, Accepted: false, Error: err.Error(), ServerTsNs: nowNs()}
	}
	return &dashsimv1.Ack{TxnId: txn, Accepted: true, ServerTsNs: nowNs()}
}

func errToStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, model.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		return status.Error(codes.InvalidArgument, err.Error())
	}
}

// -----------------------------------------------------------------------------
// VNETs
// -----------------------------------------------------------------------------

func (s *Server) CreateVnet(_ context.Context, in *dashsimv1.Vnet) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("CreateVnet"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.CreateVnet(in)
	return ack(tx, err), nil
}

func (s *Server) DeleteVnet(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("DeleteVnet"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.DeleteVnet(in.GetId())
	return ack(tx, err), nil
}

func (s *Server) GetVnet(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.VnetResponse, error) {
	if err := s.faults.Apply("GetVnet"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	v, err := s.store.GetVnet(in.GetId())
	if err != nil {
		return nil, errToStatus(err)
	}
	return &dashsimv1.VnetResponse{Item: v, ServerTsNs: nowNs()}, nil
}

func (s *Server) ListVnets(_ *dashsimv1.ListRequest, stream dashsimv1.DashSim_ListVnetsServer) error {
	if err := s.faults.Apply("ListVnets"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for _, v := range s.store.ListVnets() {
		if err := stream.Send(&dashsimv1.VnetResponse{Item: v, ServerTsNs: nowNs()}); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// ENIs
// -----------------------------------------------------------------------------

func (s *Server) CreateEni(_ context.Context, in *dashsimv1.Eni) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("CreateEni"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.CreateEni(in)
	return ack(tx, err), nil
}

func (s *Server) UpdateEni(_ context.Context, in *dashsimv1.Eni) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("UpdateEni"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.UpdateEni(in)
	return ack(tx, err), nil
}

func (s *Server) DeleteEni(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("DeleteEni"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.DeleteEni(in.GetId())
	if err == nil {
		s.counters.Forget(in.GetId())
	}
	return ack(tx, err), nil
}

func (s *Server) GetEni(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.EniResponse, error) {
	if err := s.faults.Apply("GetEni"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	e, err := s.store.GetEni(in.GetId())
	if err != nil {
		return nil, errToStatus(err)
	}
	return &dashsimv1.EniResponse{Item: e, ServerTsNs: nowNs()}, nil
}

func (s *Server) ListEnis(_ *dashsimv1.ListRequest, stream dashsimv1.DashSim_ListEnisServer) error {
	if err := s.faults.Apply("ListEnis"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for _, e := range s.store.ListEnis() {
		if err := stream.Send(&dashsimv1.EniResponse{Item: e, ServerTsNs: nowNs()}); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// VNET mappings
// -----------------------------------------------------------------------------

func (s *Server) AddVnetMapping(_ context.Context, in *dashsimv1.VnetMapping) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("AddVnetMapping"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.AddVnetMapping(in)
	return ack(tx, err), nil
}

func (s *Server) DeleteVnetMapping(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("DeleteVnetMapping"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.DeleteVnetMapping(in.GetId())
	return ack(tx, err), nil
}

func (s *Server) ListVnetMappings(_ *dashsimv1.ListRequest, stream dashsimv1.DashSim_ListVnetMappingsServer) error {
	if err := s.faults.Apply("ListVnetMappings"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for _, m := range s.store.ListVnetMappings() {
		if err := stream.Send(&dashsimv1.VnetMappingResponse{Item: m, ServerTsNs: nowNs()}); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Routes
// -----------------------------------------------------------------------------

func (s *Server) AddRoute(_ context.Context, in *dashsimv1.Route) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("AddRoute"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.AddRoute(in)
	return ack(tx, err), nil
}

func (s *Server) DeleteRoute(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("DeleteRoute"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.DeleteRoute(in.GetId())
	return ack(tx, err), nil
}

func (s *Server) ListRoutes(_ *dashsimv1.ListRequest, stream dashsimv1.DashSim_ListRoutesServer) error {
	if err := s.faults.Apply("ListRoutes"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for _, r := range s.store.ListRoutes() {
		if err := stream.Send(&dashsimv1.RouteResponse{Item: r, ServerTsNs: nowNs()}); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// ACL
// -----------------------------------------------------------------------------

func (s *Server) AddAclGroup(_ context.Context, in *dashsimv1.AclGroup) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("AddAclGroup"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.AddAclGroup(in)
	return ack(tx, err), nil
}

func (s *Server) DeleteAclGroup(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("DeleteAclGroup"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.DeleteAclGroup(in.GetId())
	return ack(tx, err), nil
}

func (s *Server) ListAclGroups(_ *dashsimv1.ListRequest, stream dashsimv1.DashSim_ListAclGroupsServer) error {
	if err := s.faults.Apply("ListAclGroups"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for _, g := range s.store.ListAclGroups() {
		if err := stream.Send(&dashsimv1.AclGroupResponse{Item: g, ServerTsNs: nowNs()}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) AddAclRule(_ context.Context, in *dashsimv1.AclRule) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("AddAclRule"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.AddAclRule(in)
	return ack(tx, err), nil
}

func (s *Server) DeleteAclRule(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.Ack, error) {
	if err := s.faults.Apply("DeleteAclRule"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.DeleteAclRule(in.GetId())
	return ack(tx, err), nil
}

func (s *Server) ListAclRules(_ *dashsimv1.ListRequest, stream dashsimv1.DashSim_ListAclRulesServer) error {
	if err := s.faults.Apply("ListAclRules"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	for _, r := range s.store.ListAclRules() {
		if err := stream.Send(&dashsimv1.AclRuleResponse{Item: r, ServerTsNs: nowNs()}); err != nil {
			return err
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Subscribe (snapshot + live)
// -----------------------------------------------------------------------------

func (s *Server) Subscribe(req *dashsimv1.SubscribeRequest, stream dashsimv1.DashSim_SubscribeServer) error {
	sub := s.bus.Subscribe(req.GetKinds())
	defer sub.Close()

	wantKind := func(k dashsimv1.ObjectKind) bool {
		if len(req.GetKinds()) == 0 {
			return true
		}
		for _, x := range req.GetKinds() {
			if x == k {
				return true
			}
		}
		return false
	}

	if req.GetSnapshotFirst() {
		snap := s.store.Snapshot()
		for _, v := range snap.Vnets {
			if !wantKind(dashsimv1.ObjectKind_OBJECT_KIND_VNET) {
				continue
			}
			if err := stream.Send(&dashsimv1.Event{
				Type:       dashsimv1.EventType_EVENT_TYPE_SNAPSHOT,
				Kind:       dashsimv1.ObjectKind_OBJECT_KIND_VNET,
				Id:         v.Id,
				Payload:    &dashsimv1.Event_Vnet{Vnet: v},
				ServerTsNs: nowNs(),
			}); err != nil {
				return err
			}
		}
		for _, e := range snap.Enis {
			if !wantKind(dashsimv1.ObjectKind_OBJECT_KIND_ENI) {
				continue
			}
			if err := stream.Send(&dashsimv1.Event{
				Type:       dashsimv1.EventType_EVENT_TYPE_SNAPSHOT,
				Kind:       dashsimv1.ObjectKind_OBJECT_KIND_ENI,
				Id:         e.Id,
				Payload:    &dashsimv1.Event_Eni{Eni: e},
				ServerTsNs: nowNs(),
			}); err != nil {
				return err
			}
		}
		for _, g := range snap.AclGroups {
			if !wantKind(dashsimv1.ObjectKind_OBJECT_KIND_ACL_GROUP) {
				continue
			}
			if err := stream.Send(&dashsimv1.Event{
				Type:       dashsimv1.EventType_EVENT_TYPE_SNAPSHOT,
				Kind:       dashsimv1.ObjectKind_OBJECT_KIND_ACL_GROUP,
				Id:         g.Id,
				Payload:    &dashsimv1.Event_AclGroup{AclGroup: g},
				ServerTsNs: nowNs(),
			}); err != nil {
				return err
			}
		}
		for _, r := range snap.AclRules {
			if !wantKind(dashsimv1.ObjectKind_OBJECT_KIND_ACL_RULE) {
				continue
			}
			if err := stream.Send(&dashsimv1.Event{
				Type:       dashsimv1.EventType_EVENT_TYPE_SNAPSHOT,
				Kind:       dashsimv1.ObjectKind_OBJECT_KIND_ACL_RULE,
				Id:         r.Id,
				Payload:    &dashsimv1.Event_AclRule{AclRule: r},
				ServerTsNs: nowNs(),
			}); err != nil {
				return err
			}
		}
		for _, r := range snap.Routes {
			if !wantKind(dashsimv1.ObjectKind_OBJECT_KIND_ROUTE) {
				continue
			}
			if err := stream.Send(&dashsimv1.Event{
				Type:       dashsimv1.EventType_EVENT_TYPE_SNAPSHOT,
				Kind:       dashsimv1.ObjectKind_OBJECT_KIND_ROUTE,
				Id:         r.Id,
				Payload:    &dashsimv1.Event_Route{Route: r},
				ServerTsNs: nowNs(),
			}); err != nil {
				return err
			}
		}
		for _, m := range snap.VnetMappings {
			if !wantKind(dashsimv1.ObjectKind_OBJECT_KIND_VNET_MAPPING) {
				continue
			}
			if err := stream.Send(&dashsimv1.Event{
				Type:       dashsimv1.EventType_EVENT_TYPE_SNAPSHOT,
				Kind:       dashsimv1.ObjectKind_OBJECT_KIND_VNET_MAPPING,
				Id:         m.Id,
				Payload:    &dashsimv1.Event_VnetMapping{VnetMapping: m},
				ServerTsNs: nowNs(),
			}); err != nil {
				return err
			}
		}
	}

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-sub.C:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		}
	}
}

// -----------------------------------------------------------------------------
// Counters
// -----------------------------------------------------------------------------

func (s *Server) GetCounters(_ context.Context, in *dashsimv1.KeyRequest) (*dashsimv1.CountersResponse, error) {
	if err := s.faults.Apply("GetCounters"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	return &dashsimv1.CountersResponse{
		Counters:   s.counters.Snapshot(in.GetId()),
		ServerTsNs: nowNs(),
	}, nil
}
