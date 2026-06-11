// Package migration implements PC-G4..G6 ENI live migration for dashd.
//
// The migration coordinator manages persistent 10-phase sessions:
//
//	ADMISSION → SNAPSHOT → PREPARE → SYNC → READY → CUTOVER →
//	DRAIN → COMMIT → FINALIZE → COMPLETED
//
// plus the synthetic ROLLBACK and ABORTED terminals.
//
// Locked decisions for PC-G4..G6:
//
//   * Sessions are PERSISTENT — they live in `store.DesiredStore` under
//     kind=`"migration_session"`. PA's etcd backend means a restart-
//     recovery (PC-G6) is just `List + LoadSessions` on boot. PD will
//     add a dedicated namespace + retention.
//
//   * Each AdvanceMigrationPhase call requires `expected_generation`
//     matching the live session's generation; mismatched advances
//     return ErrGenerationMismatch. This is optimistic concurrency on
//     the session object — two operators racing on the same session
//     can't both succeed.
//
//   * AdvanceMigrationPhase enforces "next phase = current_phase + 1"
//     exactly. Skipping phases is FAILED_PRECONDITION. The wire enum
//     ordinals are CONTRACTUAL — operators / dashctl compare them
//     numerically to gauge progress.
//
//   * Rollback (PC-G5) is permitted from any phase BEFORE COMMIT. It
//     drives the session through synthetic ROLLBACK while the
//     `CutoverEffect.Undo*` chain runs (newest-first), then the
//     session terminates in ABORTED with `failure_reason` set.
//     Post-COMMIT rollback is forbidden — the source DPU has already
//     released its state.
//
//   * CUTOVER is the single phase that mutates the desired store (we
//     rewrite each ENI's `placement_hint_dpu_ids` from source → target
//     via the service-layer PutEni so capacity/schema/cordon admission
//     all fire on the target). Other phases are pure orchestrator-
//     state machinery + injectable southbound effects (`CutoverEffect`
//     interface, mirrored on the HA orchestrator's Pusher pattern).
//
//   * Strategies: NEW_FLOWS_FIRST_DRAIN (default), FULL_REHOME,
//     MAINTENANCE_FAST, CANARY_SPLIT. The state machine is identical
//     across strategies for PC-G4..G6; per-strategy semantics live in
//     the injected CutoverEffect (which knows whether SYNC blocks on
//     flow-sync completion, whether DRAIN waits for flow aging, etc.).
//
// Out of scope for PC-G4..G6 (each deferred with a documented seam):
//
//   * Bundle export/import (streaming RPCs) — separate gRPC complexity;
//     stubs return Unimplemented. PD wires it once the audit log is in.
//   * Cross-cluster bundle import — depends on the bundle format
//     spec which is still being finalised in DASH upstream.
//   * Saga-backed multi-ENI batch rollback inside one session — current
//     implementation rolls back ENI-by-ENI in reverse-apply order; the
//     PC-G8 saga package will be reused for this in PD.
package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Errors surfaced by the coordinator.
var (
	ErrNotFound             = errors.New("migration: session not found")
	ErrInvalidArgument      = errors.New("migration: invalid argument")
	ErrGenerationMismatch   = errors.New("migration: generation mismatch")
	ErrInvalidTransition    = errors.New("migration: invalid phase transition")
	ErrCommitted            = errors.New("migration: session is past COMMIT; rollback not permitted")
	ErrTerminal             = errors.New("migration: session is terminal")
)

// CutoverEffect is the southbound abstraction the coordinator uses
// during phase advance. Production wires `LivePutEffect` (rewrites
// each ENI's placement_hint at CUTOVER via service.PutEni so capacity
// + schema + cordon admission all fire); tests inject a fake to assert
// call sequencing. Same injection-seam pattern as orchestrator.Pusher
// from PC-G1..G3.
type CutoverEffect interface {
	// PrepareTarget is called at PREPARE. Pre-stage policy on the
	// target DPU so cutover is a cheap flip. Today this is a no-op
	// (capacity admission already happens at PutEni time); PE wires
	// real DASH southbound here.
	PrepareTarget(ctx context.Context, plan dashcenterv1.MigrationPlan) error

	// SyncFlows is called at SYNC. Strategy-specific; production
	// today returns nil (sim doesn't carry flow state). PE wires real
	// flow-sync RPCs against the dash-sim once the sim grows them.
	SyncFlows(ctx context.Context, plan dashcenterv1.MigrationPlan) error

	// Cutover is called at CUTOVER. The single phase that mutates the
	// desired store: rewrites each ENI's placement_hint to the target
	// DPU via the service-layer PutEni (so capacity + schema + cordon
	// admission fire on the destination). Returns a list of (eni,
	// oldHints) snapshots so Undo can restore them on rollback.
	Cutover(ctx context.Context, plan dashcenterv1.MigrationPlan) (Snapshot, error)

	// DrainSource is called at DRAIN. Strategy-specific; today a
	// no-op (sim has no slow-path drain).
	DrainSource(ctx context.Context, plan dashcenterv1.MigrationPlan) error

	// UndoCutover restores the pre-cutover ENI placements. Called by
	// rollback when the session is at or past CUTOVER but not yet
	// COMMIT. Best-effort: per-ENI failures are logged and the rollback
	// continues with the remaining ENIs.
	UndoCutover(ctx context.Context, plan dashcenterv1.MigrationPlan, snap Snapshot) error
}

// Snapshot captures pre-cutover ENI placement state so UndoCutover can
// restore it.
type Snapshot struct {
	// PerEni maps ENI name → previous placement_hint_dpu_ids.
	PerEni map[string][]string `json:"per_eni,omitempty"`
}

// Session is the in-memory + persisted shape of one live migration.
// Mirrors the wire MigrationSession but uses Go-native maps so we
// can JSON-encode for the desired store without proto marshalling.
type Session struct {
	ID             string                          `json:"session_id"`
	Generation     uint64                          `json:"generation"`
	Plan           dashcenterv1.MigrationPlan      `json:"plan"`
	Phase          dashcenterv1.MigrationPhase     `json:"phase"`
	PhaseStartedAt map[int32]string                `json:"phase_started_at,omitempty"`
	DetailJSON     string                          `json:"detail_json,omitempty"`
	FailureReason  string                          `json:"failure_reason,omitempty"`
	CreatedAt      time.Time                       `json:"created_at"`
	UpdatedAt      time.Time                       `json:"updated_at"`
	// Snapshot is the pre-cutover ENI placement, captured by
	// CutoverEffect.Cutover so UndoCutover can restore it. Persisted
	// so a dashd restart mid-rollback can resume from etcd.
	Snapshot Snapshot `json:"snapshot,omitempty"`
}

// Store key for a session. Sessions live in a synthetic namespace
// "_migrations" so they never collide with operator-applied specs.
// Underscore-prefix is reserved per the dashcenter.v1 namespace
// validator and is filesystem-safe on Windows + Linux.
const (
	sessionKind = "migration_session"
	sessionNS   = "_migrations"
)

func sessionKey(id string) store.ObjectKey {
	return store.ObjectKey{Namespace: sessionNS, Kind: sessionKind, Name: id}
}

// Coordinator owns the session catalogue and drives state transitions.
// Construct with New; wire into service.NewControlPlane via a new arg.
//
// Thread-safe: every public method takes the mu lock for its critical
// section and releases BEFORE invoking the CutoverEffect (which may
// block on network IO).
type Coordinator struct {
	st     store.DesiredStore
	effect CutoverEffect

	mu       sync.Mutex
	sessions map[string]*Session
	bcast    *Broadcaster
	clock    func() time.Time
}

// New constructs a Coordinator backed by `st` for session persistence.
// If effect is nil, a NoOpEffect is used (matches PC-G4..G6 sim
// behaviour today; PE wires real DASH southbound). Hydrates persisted
// sessions from `st` at construction time so a dashd restart picks up
// in-flight migrations (PC-G6).
func New(ctx context.Context, st store.DesiredStore, effect CutoverEffect) (*Coordinator, error) {
	if st == nil {
		return nil, errors.New("migration: nil store")
	}
	if effect == nil {
		effect = &NoOpEffect{}
	}
	c := &Coordinator{
		st:       st,
		effect:   effect,
		sessions: map[string]*Session{},
		bcast:    NewBroadcaster(),
		clock:    func() time.Time { return time.Now() },
	}
	if err := c.hydrate(ctx); err != nil {
		return nil, fmt.Errorf("migration: hydrate: %w", err)
	}
	return c, nil
}

// hydrate loads all persisted sessions from the desired store. Called
// once at startup; idempotent.
func (c *Coordinator) hydrate(ctx context.Context) error {
	rows, err := c.st.List(ctx, sessionNS, sessionKind)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range rows {
		s := &Session{}
		if err := json.Unmarshal(r.Data, s); err != nil {
			// Skip corrupted rows — log via the operator's audit log
			// once PD adds one; for now we silently drop.
			continue
		}
		c.sessions[s.ID] = s
	}
	return nil
}

// Broadcaster exposes the per-coordinator event fan-out (used by
// StreamMigrationSession). PC-G6 reuses orchestrator-style fan-out.
func (c *Coordinator) Broadcaster() *Broadcaster { return c.bcast }

// --- Public API ----------------------------------------------------------

// CreatePlan validates plan inputs and produces a MigrationPlan with a
// fresh plan_id + warnings. Does NOT persist anything — the plan is a
// transient validation artifact until StartSession.
func (c *Coordinator) CreatePlan(_ context.Context, ns string, req *dashcenterv1.CreateMigrationPlanRequest) (*dashcenterv1.MigrationPlan, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request is nil", ErrInvalidArgument)
	}
	if len(req.GetEniNames()) == 0 {
		return nil, fmt.Errorf("%w: eni_names is empty", ErrInvalidArgument)
	}
	if req.GetSourceDpuId() == "" {
		return nil, fmt.Errorf("%w: source_dpu_id is required", ErrInvalidArgument)
	}
	if req.GetTargetDpuId() == "" {
		return nil, fmt.Errorf("%w: target_dpu_id is required (auto-placement deferred to PD)", ErrInvalidArgument)
	}
	if req.GetSourceDpuId() == req.GetTargetDpuId() {
		return nil, fmt.Errorf("%w: source_dpu_id == target_dpu_id", ErrInvalidArgument)
	}
	strategy := req.GetStrategy()
	if strategy == dashcenterv1.MigrationStrategy_MIGRATION_STRATEGY_UNSPECIFIED {
		strategy = dashcenterv1.MigrationStrategy_MIGRATION_STRATEGY_NEW_FLOWS_FIRST_DRAIN
	}
	if ns == "" {
		ns = req.GetNamespace()
	}
	if ns == "" {
		ns = "default"
	}
	plan := &dashcenterv1.MigrationPlan{
		PlanId:                  newSessionID("plan"),
		Namespace:               ns,
		EniNames:                append([]string(nil), req.GetEniNames()...),
		SourceDpuId:             req.GetSourceDpuId(),
		TargetDpuId:             req.GetTargetDpuId(),
		Strategy:                strategy,
		MaxSyncDurationSeconds:  req.GetMaxSyncDurationSeconds(),
		MaxTotalDurationSeconds: req.GetMaxTotalDurationSeconds(),
	}
	return plan, nil
}

// ValidatePlan re-runs the constructor checks against a supplied plan.
// Returns the plan unchanged on success.
func (c *Coordinator) ValidatePlan(ctx context.Context, plan *dashcenterv1.MigrationPlan) (*dashcenterv1.MigrationPlan, error) {
	if plan == nil {
		return nil, fmt.Errorf("%w: plan is nil", ErrInvalidArgument)
	}
	// Pass through CreatePlan's validation by reconstructing the
	// request shape. Discard the new plan_id — caller wants its own
	// plan back.
	_, err := c.CreatePlan(ctx, plan.GetNamespace(), &dashcenterv1.CreateMigrationPlanRequest{
		Namespace: plan.GetNamespace(), EniNames: plan.GetEniNames(),
		SourceDpuId: plan.GetSourceDpuId(), TargetDpuId: plan.GetTargetDpuId(),
		Strategy: plan.GetStrategy(),
	})
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// StartSession registers a new session at MIGRATION_PHASE_ADMISSION
// and persists it to the desired store.
func (c *Coordinator) StartSession(ctx context.Context, plan *dashcenterv1.MigrationPlan) (*Session, error) {
	if _, err := c.ValidatePlan(ctx, plan); err != nil {
		return nil, err
	}
	now := c.clock()
	s := &Session{
		ID:             newSessionID("mig"),
		Generation:     1,
		Plan:           *plan,
		Phase:          dashcenterv1.MigrationPhase_MIGRATION_PHASE_ADMISSION,
		PhaseStartedAt: map[int32]string{int32(dashcenterv1.MigrationPhase_MIGRATION_PHASE_ADMISSION): now.UTC().Format(time.RFC3339Nano)},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := c.persist(ctx, s); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.sessions[s.ID] = s
	c.mu.Unlock()
	c.bcast.Publish(*s)
	return cloneSession(s), nil
}

// Advance moves the session to `next`, which MUST equal current + 1.
// expected_generation must match the live session's generation (CAS).
// Phases that have side-effects (PREPARE/SYNC/CUTOVER/DRAIN) invoke
// the corresponding CutoverEffect method; effect failures abort the
// advance and the session phase is unchanged (operator can retry).
func (c *Coordinator) Advance(ctx context.Context, sessionID string, expectedGen uint64, next dashcenterv1.MigrationPhase, _ string) (*Session, error) {
	c.mu.Lock()
	s, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sessionID)
	}
	if s.Generation != expectedGen {
		gen := s.Generation
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: expected gen=%d, current gen=%d", ErrGenerationMismatch, expectedGen, gen)
	}
	if isTerminal(s.Phase) {
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: %s is in %s", ErrTerminal, sessionID, s.Phase)
	}
	if int(next) != int(s.Phase)+1 {
		current := s.Phase
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: cannot advance from %s to %s (must be %s)", ErrInvalidTransition, current, next, current+1)
	}
	planCopy := s.Plan
	c.mu.Unlock()

	// Run side effects OUTSIDE the lock so other readers see the
	// pre-advance phase while we work.
	var (
		snap Snapshot
		err  error
	)
	switch next {
	case dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE:
		err = c.effect.PrepareTarget(ctx, planCopy)
	case dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC:
		err = c.effect.SyncFlows(ctx, planCopy)
	case dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER:
		snap, err = c.effect.Cutover(ctx, planCopy)
	case dashcenterv1.MigrationPhase_MIGRATION_PHASE_DRAIN:
		err = c.effect.DrainSource(ctx, planCopy)
	}
	if err != nil {
		return nil, fmt.Errorf("migration: advance to %s: %w", next, err)
	}

	// Commit the phase change under lock.
	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check generation under lock — a concurrent caller could have
	// raced past us.
	if s.Generation != expectedGen {
		return nil, fmt.Errorf("%w: raced", ErrGenerationMismatch)
	}
	s.Phase = next
	s.Generation++
	s.UpdatedAt = c.clock()
	if s.PhaseStartedAt == nil {
		s.PhaseStartedAt = map[int32]string{}
	}
	s.PhaseStartedAt[int32(next)] = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if next == dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER {
		s.Snapshot = snap
	}
	if err := c.persistLocked(ctx, s); err != nil {
		return nil, err
	}
	c.bcast.Publish(*s)
	return cloneSession(s), nil
}

// Rollback drives the session through synthetic ROLLBACK to ABORTED.
// Rejected if the session is at or past COMMIT.
func (c *Coordinator) Rollback(ctx context.Context, sessionID string, expectedGen uint64, reason string) (*Session, error) {
	c.mu.Lock()
	s, ok := c.sessions[sessionID]
	if !ok {
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sessionID)
	}
	if s.Generation != expectedGen {
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: expected gen=%d, current gen=%d", ErrGenerationMismatch, expectedGen, s.Generation)
	}
	if isTerminal(s.Phase) {
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrTerminal, sessionID)
	}
	if int(s.Phase) >= int(dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMMIT) {
		c.mu.Unlock()
		return nil, fmt.Errorf("%w: phase=%s", ErrCommitted, s.Phase)
	}
	// Snapshot the data we need before releasing the lock.
	planCopy := s.Plan
	snapCopy := s.Snapshot
	currentPhase := s.Phase
	s.Phase = dashcenterv1.MigrationPhase_MIGRATION_PHASE_ROLLBACK
	s.Generation++
	s.UpdatedAt = c.clock()
	if s.PhaseStartedAt == nil {
		s.PhaseStartedAt = map[int32]string{}
	}
	s.PhaseStartedAt[int32(dashcenterv1.MigrationPhase_MIGRATION_PHASE_ROLLBACK)] = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	s.FailureReason = reason
	if err := c.persistLocked(ctx, s); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	c.bcast.Publish(*s)
	c.mu.Unlock()

	// Run UndoCutover if we already cut over.
	if int(currentPhase) >= int(dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER) {
		if err := c.effect.UndoCutover(ctx, planCopy, snapCopy); err != nil {
			// Mark ABORTED with the undo failure detail; operator must
			// reconcile by hand.
			c.mu.Lock()
			s.FailureReason = fmt.Sprintf("%s; undo-cutover failed: %s", reason, err.Error())
			s.Phase = dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED
			s.Generation++
			s.UpdatedAt = c.clock()
			s.PhaseStartedAt[int32(s.Phase)] = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
			_ = c.persistLocked(ctx, s)
			c.bcast.Publish(*s)
			c.mu.Unlock()
			return nil, fmt.Errorf("migration: rollback: undo cutover: %w", err)
		}
	}

	// Transition ROLLBACK → ABORTED.
	c.mu.Lock()
	defer c.mu.Unlock()
	s.Phase = dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED
	s.Generation++
	s.UpdatedAt = c.clock()
	s.PhaseStartedAt[int32(s.Phase)] = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	if err := c.persistLocked(ctx, s); err != nil {
		return nil, err
	}
	c.bcast.Publish(*s)
	return cloneSession(s), nil
}

// Abort forces an immediate transition to ABORTED. Does NOT call
// UndoCutover (use Rollback for that). Operator must clean up by hand.
// Allowed from any non-terminal phase.
func (c *Coordinator) Abort(ctx context.Context, sessionID string, expectedGen uint64, reason string) (*Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sessionID)
	}
	if s.Generation != expectedGen {
		return nil, fmt.Errorf("%w: expected gen=%d, current gen=%d", ErrGenerationMismatch, expectedGen, s.Generation)
	}
	if isTerminal(s.Phase) {
		return nil, fmt.Errorf("%w: %s", ErrTerminal, sessionID)
	}
	s.Phase = dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED
	s.Generation++
	s.UpdatedAt = c.clock()
	if s.PhaseStartedAt == nil {
		s.PhaseStartedAt = map[int32]string{}
	}
	s.PhaseStartedAt[int32(s.Phase)] = s.UpdatedAt.UTC().Format(time.RFC3339Nano)
	s.FailureReason = reason
	if err := c.persistLocked(ctx, s); err != nil {
		return nil, err
	}
	c.bcast.Publish(*s)
	return cloneSession(s), nil
}

// Commit is a convenience that walks FINALIZE → COMPLETED in one call
// from COMMIT. Returns an error if not currently at COMMIT.
func (c *Coordinator) Commit(ctx context.Context, sessionID string, expectedGen uint64, _ string) (*Session, error) {
	// FINALIZE
	s, err := c.Advance(ctx, sessionID, expectedGen, dashcenterv1.MigrationPhase_MIGRATION_PHASE_FINALIZE, "commit: finalize")
	if err != nil {
		return nil, err
	}
	// COMPLETED
	return c.Advance(ctx, sessionID, s.Generation, dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMPLETED, "commit: completed")
}

// Get returns a clone of the named session.
func (c *Coordinator) Get(_ context.Context, sessionID string) (*Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, sessionID)
	}
	return cloneSession(s), nil
}

// List returns clones of every session matching the filter. nil filter
// returns all (including terminals).
type ListFilter struct {
	Namespaces      []string
	SourceDPUIDs    []string
	TargetDPUIDs    []string
	Phases          []dashcenterv1.MigrationPhase
	IncludeTerminal bool
}

func (c *Coordinator) List(_ context.Context, f *ListFilter) []*Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*Session, 0, len(c.sessions))
	for _, s := range c.sessions {
		if f != nil && !f.matches(s) {
			continue
		}
		if f != nil && !f.IncludeTerminal && isTerminal(s.Phase) {
			continue
		}
		out = append(out, cloneSession(s))
	}
	return out
}

func (f *ListFilter) matches(s *Session) bool {
	if len(f.Namespaces) > 0 && !contains(f.Namespaces, s.Plan.GetNamespace()) {
		return false
	}
	if len(f.SourceDPUIDs) > 0 && !contains(f.SourceDPUIDs, s.Plan.GetSourceDpuId()) {
		return false
	}
	if len(f.TargetDPUIDs) > 0 && !contains(f.TargetDPUIDs, s.Plan.GetTargetDpuId()) {
		return false
	}
	if len(f.Phases) > 0 {
		ok := false
		for _, p := range f.Phases {
			if p == s.Phase {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// --- internal helpers --------------------------------------------------

func (c *Coordinator) persist(ctx context.Context, s *Session) error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if _, err := c.st.Put(ctx, sessionKey(s.ID), json.RawMessage(data), 0); err != nil {
		return err
	}
	return nil
}

// persistLocked is persist() but assumes caller holds c.mu.
func (c *Coordinator) persistLocked(ctx context.Context, s *Session) error {
	return c.persist(ctx, s)
}

func isTerminal(p dashcenterv1.MigrationPhase) bool {
	return p == dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMPLETED ||
		p == dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED
}

func cloneSession(s *Session) *Session {
	out := *s
	if s.PhaseStartedAt != nil {
		out.PhaseStartedAt = make(map[int32]string, len(s.PhaseStartedAt))
		for k, v := range s.PhaseStartedAt {
			out.PhaseStartedAt[k] = v
		}
	}
	if s.Snapshot.PerEni != nil {
		cp := make(map[string][]string, len(s.Snapshot.PerEni))
		for k, v := range s.Snapshot.PerEni {
			cp[k] = append([]string(nil), v...)
		}
		out.Snapshot.PerEni = cp
	}
	// Plan is a proto with pointer-y fields; shallow copy is fine for
	// our read-only consumers.
	return &out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// newSessionID returns a short, sortable id like "mig-1718000000.123-abcd".
// We don't pull uuid just for this — time + a random nibble is enough
// for human readability and uniqueness within a single dashd.
func newSessionID(prefix string) string {
	n := time.Now().UnixNano()
	return fmt.Sprintf("%s-%d-%04x", prefix, n, (n^(n>>13))&0xffff)
}
