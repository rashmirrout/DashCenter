// Package server implements the dashapi.v1.DashApi gRPC service backed by
// model.Store, events.Bus, faults.Injector and counters.Registry.
package server

import (
	"context"
	"errors"
	"strings"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/pipeline"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements dashapi.DashApiServer.
type Server struct {
	dashapi.UnimplementedDashApiServer

	store    *model.Store
	bus      *events.Bus
	faults   *faults.Injector
	counters *counters.Registry
	engine   *pipeline.Engine
}

// New constructs a Server. All deps must be non-nil.
func New(store *model.Store, bus *events.Bus, faultInj *faults.Injector, ctrs *counters.Registry) *Server {
	return &Server{
		store: store, bus: bus, faults: faultInj, counters: ctrs,
		engine: pipeline.New(store, ctrs),
	}
}

func nowNs() int64 { return time.Now().UnixNano() }

func ack(txn string, err error) *dashapi.Ack {
	if err != nil {
		return &dashapi.Ack{TxnId: txn, Accepted: false, Error: err.Error(), ServerTsNs: nowNs()}
	}
	return &dashapi.Ack{TxnId: txn, Accepted: true, ServerTsNs: nowNs()}
}

func errToStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, model.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	default:
		return status.Error(codes.InvalidArgument, err.Error())
	}
}

// Apply implements DashApi.Apply.
func (s *Server) Apply(_ context.Context, req *dashapi.ApplyRequest) (*dashapi.Ack, error) {
	if err := s.faults.Apply("Apply"); err != nil {
		return ack("", err), nil
	}
	tx, _, err := s.store.Apply(req.GetObject())
	return ack(tx, err), nil
}

// Delete implements DashApi.Delete.
func (s *Server) Delete(_ context.Context, req *dashapi.DeleteRequest) (*dashapi.Ack, error) {
	if err := s.faults.Apply("Delete"); err != nil {
		return ack("", err), nil
	}
	tx, err := s.store.Delete(req.GetKind(), req.GetKey())
	if err == nil {
		s.counters.Forget(counters.JoinKey(req.GetKey()))
	}
	return ack(tx, err), nil
}

// Get implements DashApi.Get.
func (s *Server) Get(_ context.Context, req *dashapi.GetRequest) (*dashapi.GetResponse, error) {
	if err := s.faults.Apply("Get"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	obj, err := s.store.Get(req.GetKind(), req.GetKey())
	if err != nil {
		return nil, errToStatus(err)
	}
	return &dashapi.GetResponse{Object: obj, ServerTsNs: nowNs()}, nil
}

// List implements DashApi.List.
func (s *Server) List(req *dashapi.ListRequest, stream dashapi.DashApi_ListServer) error {
	if err := s.faults.Apply("List"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	items, err := s.store.List(req.GetKind(), req.GetKeyPrefix())
	if err != nil {
		return errToStatus(err)
	}
	limit := int(req.GetLimit())
	for i, obj := range items {
		if limit > 0 && i >= limit {
			break
		}
		if err := stream.Send(&dashapi.ListItem{Object: obj}); err != nil {
			return err
		}
	}
	return nil
}

// Subscribe implements DashApi.Subscribe.
func (s *Server) Subscribe(req *dashapi.SubscribeRequest, stream dashapi.DashApi_SubscribeServer) error {
	if err := s.faults.Apply("Subscribe"); err != nil {
		return status.Error(codes.Unavailable, err.Error())
	}
	sub := s.bus.Subscribe(req.GetKinds())
	defer sub.Close()

	if req.GetSnapshotFirst() {
		for _, ev := range s.store.SnapshotEvents(req.GetKinds()) {
			if err := stream.Send(ev); err != nil {
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

// GetCounters implements DashApi.GetCounters.
func (s *Server) GetCounters(_ context.Context, req *dashapi.CountersRequest) (*dashapi.CountersResponse, error) {
	if err := s.faults.Apply("GetCounters"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	key := strings.Join(req.GetKey(), ":")
	return &dashapi.CountersResponse{
		Counters:   s.counters.Snapshot(key),
		ServerTsNs: nowNs(),
	}, nil
}

// SimulatePacket implements DashApi.SimulatePacket.
func (s *Server) SimulatePacket(_ context.Context, req *dashapi.SimulatePacketRequest) (*dashapi.SimulatePacketResponse, error) {
	if err := s.faults.Apply("SimulatePacket"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	d := s.engine.Evaluate(req.GetPacket(), req.GetTrace())
	return &dashapi.SimulatePacketResponse{Decision: d, ServerTsNs: nowNs()}, nil
}
