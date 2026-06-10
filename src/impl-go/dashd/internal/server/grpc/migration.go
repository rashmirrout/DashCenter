// PC-G4..G6 gRPC MigrationService handler. Thin adapter over
// service.MigrationService. ExportMigrationBundle / ImportMigrationBundle
// stay Unimplemented for PC-G4..G6 (deferred to PE per the locked
// scope note in internal/migration/migration.go).
package grpcserver

import (
	"context"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/migration"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type migrationHandler struct {
	dashcenterv1.UnimplementedMigrationServiceServer
	mig service.MigrationService
}

func registerMigration(gs *grpc.Server, mig service.MigrationService) {
	if mig == nil {
		return
	}
	dashcenterv1.RegisterMigrationServiceServer(gs, &migrationHandler{mig: mig})
}

func (h *migrationHandler) CreateMigrationPlan(ctx context.Context, req *dashcenterv1.CreateMigrationPlanRequest) (*dashcenterv1.MigrationPlan, error) {
	p, err := h.mig.CreatePlan(ctx, req.GetNamespace(), req)
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return p, nil
}

func (h *migrationHandler) ValidateMigrationPlan(ctx context.Context, req *dashcenterv1.ValidateMigrationPlanRequest) (*dashcenterv1.MigrationPlan, error) {
	p, err := h.mig.ValidatePlan(ctx, req.GetPlan())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return p, nil
}

func (h *migrationHandler) StartMigrationSession(ctx context.Context, req *dashcenterv1.StartMigrationSessionRequest) (*dashcenterv1.MigrationSession, error) {
	s, err := h.mig.StartSession(ctx, req.GetPlan())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return sessionToProto(s), nil
}

func (h *migrationHandler) AdvanceMigrationPhase(ctx context.Context, req *dashcenterv1.AdvanceMigrationPhaseRequest) (*dashcenterv1.MigrationSession, error) {
	s, err := h.mig.AdvancePhase(ctx, req.GetSessionId(), req.GetExpectedGeneration(), req.GetToPhase(), req.GetReason())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return sessionToProto(s), nil
}

func (h *migrationHandler) RollbackMigration(ctx context.Context, req *dashcenterv1.MigrationActionRequest) (*dashcenterv1.MigrationSession, error) {
	s, err := h.mig.Rollback(ctx, req.GetSessionId(), req.GetExpectedGeneration(), req.GetReason())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return sessionToProto(s), nil
}

func (h *migrationHandler) AbortMigration(ctx context.Context, req *dashcenterv1.MigrationActionRequest) (*dashcenterv1.MigrationSession, error) {
	s, err := h.mig.Abort(ctx, req.GetSessionId(), req.GetExpectedGeneration(), req.GetReason())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return sessionToProto(s), nil
}

func (h *migrationHandler) CommitMigration(ctx context.Context, req *dashcenterv1.MigrationActionRequest) (*dashcenterv1.MigrationSession, error) {
	s, err := h.mig.Commit(ctx, req.GetSessionId(), req.GetExpectedGeneration(), req.GetReason())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return sessionToProto(s), nil
}

func (h *migrationHandler) GetMigrationSession(ctx context.Context, req *dashcenterv1.GetMigrationSessionRequest) (*dashcenterv1.MigrationSession, error) {
	s, err := h.mig.Get(ctx, req.GetSessionId())
	if err != nil {
		return nil, serviceErrToStatus(err)
	}
	return sessionToProto(s), nil
}

func (h *migrationHandler) ListMigrationSessions(req *dashcenterv1.ListMigrationSessionsRequest, stream grpc.ServerStreamingServer[dashcenterv1.MigrationSession]) error {
	phases := make([]dashcenterv1.MigrationPhase, 0, len(req.GetPhases()))
	phases = append(phases, req.GetPhases()...)
	rows := h.mig.List(stream.Context(), &migration.ListFilter{
		Namespaces:      req.GetNamespaces(),
		SourceDPUIDs:    req.GetSourceDpuIds(),
		TargetDPUIDs:    req.GetTargetDpuIds(),
		Phases:          phases,
		IncludeTerminal: req.GetIncludeTerminal(),
	})
	for _, s := range rows {
		if err := stream.Send(sessionToProto(s)); err != nil {
			return err
		}
	}
	return nil
}

func (h *migrationHandler) StreamMigrationSession(req *dashcenterv1.StreamMigrationSessionRequest, stream grpc.ServerStreamingServer[dashcenterv1.MigrationSession]) error {
	id := req.GetSessionId()
	if !req.GetDeltasOnly() {
		// Send the current snapshot first.
		if s, err := h.mig.Get(stream.Context(), id); err == nil {
			if err := stream.Send(sessionToProto(s)); err != nil {
				return err
			}
		}
	}
	ch, cancel, err := h.mig.StreamSession(id)
	if err != nil {
		return serviceErrToStatus(err)
	}
	defer cancel()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case s, ok := <-ch:
			if !ok {
				return nil
			}
			if err := stream.Send(sessionToProto(&s)); err != nil {
				return err
			}
		}
	}
}

func sessionToProto(s *migration.Session) *dashcenterv1.MigrationSession {
	if s == nil {
		return nil
	}
	out := &dashcenterv1.MigrationSession{
		SessionId:      s.ID,
		Generation:     s.Generation,
		Plan:           &s.Plan,
		Phase:          s.Phase,
		PhaseStartedAt: make(map[int32]string, len(s.PhaseStartedAt)),
		DetailJson:     s.DetailJSON,
		FailureReason:  s.FailureReason,
	}
	for k, v := range s.PhaseStartedAt {
		out.PhaseStartedAt[k] = v
	}
	if !s.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(s.CreatedAt)
	} else {
		out.CreatedAt = timestamppb.New(time.Time{})
	}
	if !s.UpdatedAt.IsZero() {
		out.UpdatedAt = timestamppb.New(s.UpdatedAt)
	} else {
		out.UpdatedAt = timestamppb.New(time.Time{})
	}
	return out
}
