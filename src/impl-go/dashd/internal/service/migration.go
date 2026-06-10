// PC-G4..G6 MigrationService surface. Adapts the migration.Coordinator
// into the transport-agnostic service interface that REST and gRPC
// handlers delegate to.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/migration"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// MigrationService is the transport-agnostic ENI live migration API.
type MigrationService interface {
	CreatePlan(ctx context.Context, ns string, req *dashcenterv1.CreateMigrationPlanRequest) (*dashcenterv1.MigrationPlan, error)
	ValidatePlan(ctx context.Context, plan *dashcenterv1.MigrationPlan) (*dashcenterv1.MigrationPlan, error)
	StartSession(ctx context.Context, plan *dashcenterv1.MigrationPlan) (*migration.Session, error)
	AdvancePhase(ctx context.Context, sessionID string, expectedGen uint64, next dashcenterv1.MigrationPhase, reason string) (*migration.Session, error)
	Rollback(ctx context.Context, sessionID string, expectedGen uint64, reason string) (*migration.Session, error)
	Abort(ctx context.Context, sessionID string, expectedGen uint64, reason string) (*migration.Session, error)
	Commit(ctx context.Context, sessionID string, expectedGen uint64, reason string) (*migration.Session, error)
	Get(ctx context.Context, sessionID string) (*migration.Session, error)
	List(ctx context.Context, filter *migration.ListFilter) []*migration.Session

	// StreamSession returns a channel of session updates + a cancel
	// function. Pass sessionID="" for all sessions.
	StreamSession(sessionID string) (<-chan migration.Session, func(), error)
}

// migrationService implements MigrationService against a Coordinator.
type migrationService struct {
	coord *migration.Coordinator
}

// NewMigration returns the MigrationService impl. Panics if coord is
// nil — production always wires one; tests that don't want migration
// should not call this.
func NewMigration(coord *migration.Coordinator) MigrationService {
	if coord == nil {
		panic("service.NewMigration: coordinator is nil")
	}
	return &migrationService{coord: coord}
}

func (m *migrationService) CreatePlan(ctx context.Context, ns string, req *dashcenterv1.CreateMigrationPlanRequest) (*dashcenterv1.MigrationPlan, error) {
	plan, err := m.coord.CreatePlan(ctx, ns, req)
	return plan, mapMigErr(err)
}
func (m *migrationService) ValidatePlan(ctx context.Context, plan *dashcenterv1.MigrationPlan) (*dashcenterv1.MigrationPlan, error) {
	p, err := m.coord.ValidatePlan(ctx, plan)
	return p, mapMigErr(err)
}
func (m *migrationService) StartSession(ctx context.Context, plan *dashcenterv1.MigrationPlan) (*migration.Session, error) {
	s, err := m.coord.StartSession(ctx, plan)
	return s, mapMigErr(err)
}
func (m *migrationService) AdvancePhase(ctx context.Context, id string, gen uint64, next dashcenterv1.MigrationPhase, reason string) (*migration.Session, error) {
	s, err := m.coord.Advance(ctx, id, gen, next, reason)
	return s, mapMigErr(err)
}
func (m *migrationService) Rollback(ctx context.Context, id string, gen uint64, reason string) (*migration.Session, error) {
	s, err := m.coord.Rollback(ctx, id, gen, reason)
	return s, mapMigErr(err)
}
func (m *migrationService) Abort(ctx context.Context, id string, gen uint64, reason string) (*migration.Session, error) {
	s, err := m.coord.Abort(ctx, id, gen, reason)
	return s, mapMigErr(err)
}
func (m *migrationService) Commit(ctx context.Context, id string, gen uint64, reason string) (*migration.Session, error) {
	s, err := m.coord.Commit(ctx, id, gen, reason)
	return s, mapMigErr(err)
}
func (m *migrationService) Get(ctx context.Context, id string) (*migration.Session, error) {
	s, err := m.coord.Get(ctx, id)
	return s, mapMigErr(err)
}
func (m *migrationService) List(ctx context.Context, f *migration.ListFilter) []*migration.Session {
	return m.coord.List(ctx, f)
}
func (m *migrationService) StreamSession(id string) (<-chan migration.Session, func(), error) {
	ch, cancel := m.coord.Broadcaster().Subscribe(id)
	return ch, cancel, nil
}

// mapMigErr translates internal migration sentinels to service-layer
// sentinels so REST/gRPC error mapping is consistent.
func mapMigErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, migration.ErrNotFound):
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	case errors.Is(err, migration.ErrInvalidArgument):
		return fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	case errors.Is(err, migration.ErrGenerationMismatch):
		return fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
	case errors.Is(err, migration.ErrInvalidTransition):
		return fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
	case errors.Is(err, migration.ErrCommitted):
		return fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
	case errors.Is(err, migration.ErrTerminal):
		return fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
	}
	return err
}

// --- service-backed EniRehomer ---------------------------------------

// ServiceEniRehomer satisfies migration.EniRehomer by going through the
// service-layer PutEni so capacity + schema + cordon admission run on
// each rehome. This is the production wiring; tests that want raw store
// access use migration.DirectStoreRehomer.
type ServiceEniRehomer struct {
	Svc   ControlPlaneService
	Store store.DesiredStore
}

func (r *ServiceEniRehomer) Rehome(ctx context.Context, ns, name, dst string) ([]string, error) {
	sp, err := r.Store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: "eni", Name: name})
	if err != nil {
		return nil, err
	}
	spec := &dashcenterv1.EniSpec{}
	if err := json.Unmarshal(sp.Data, spec); err != nil {
		return nil, fmt.Errorf("rehome: unmarshal eni %s/%s: %w", ns, name, err)
	}
	prior := append([]string(nil), spec.GetPlacementHintDpuIds()...)
	spec.PlacementHintDpuIds = []string{dst}
	spec.ExpectedGeneration = uint64(sp.Generation)
	if _, err := r.Svc.PutEni(ctx, ns, spec); err != nil {
		return nil, err
	}
	return prior, nil
}

func (r *ServiceEniRehomer) Restore(ctx context.Context, ns, name string, prior []string) error {
	sp, err := r.Store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: "eni", Name: name})
	if err != nil {
		return err
	}
	spec := &dashcenterv1.EniSpec{}
	if err := json.Unmarshal(sp.Data, spec); err != nil {
		return fmt.Errorf("restore: unmarshal eni %s/%s: %w", ns, name, err)
	}
	spec.PlacementHintDpuIds = append([]string(nil), prior...)
	spec.ExpectedGeneration = uint64(sp.Generation)
	_, err = r.Svc.PutEni(ctx, ns, spec)
	return err
}
