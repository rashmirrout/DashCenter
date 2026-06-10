// Package orchestrator implements PC-G1..G3 HA orchestration for dashd.
//
// The orchestrator holds an in-memory model of every HA set dashd knows
// about (registered from PutHaSet specs in the desired store) and
// per-member roles: ACTIVE, STANDBY, DEAD, or any of the SWITCHING_*
// transitional states from the upstream DASH 10-state machine.
//
// Locked decisions for PC-G1..G3 (HA orchestration slice):
//
//   * Orchestrator state is the SOURCE OF TRUTH for "what role does
//     this member hold right now?" — dashctl reads it via GetHaSetState
//     / GetHaScopeState. Capacity / drain / scheduling all consume the
//     same model. The DASH southbound RPCs that would actually program
//     the DPU sims to flip their flow-sync role are stubbed through an
//     injectable `Pusher` interface — production wires this to a real
//     dashapi.v1 client when the sim grows the HA scope endpoints (PE).
//     PC-G1..G3 tests inject a fake Pusher to assert call sequencing.
//
//   * Switchover (planned, PC-G1) walks:
//       ACTIVE   --drain-->  SWITCHING_TO_STANDBY  --ack-->  STANDBY
//       STANDBY  --promote--> SWITCHING_TO_ACTIVE   --ack-->  ACTIVE
//     The drain phase calls Pusher.DrainOldActive(ctx, dpuID). If that
//     fails or times out, the switchover aborts and the orchestrator
//     restores both members to their pre-switchover roles (no split-
//     brain risk: we never promoted the target).
//
//   * Failover (unplanned, PC-G2) walks:
//       ACTIVE   --declared-dead-->  DEAD          (NO Pusher call)
//       STANDBY  --promote-->        SWITCHING_TO_ACTIVE --ack--> ACTIVE
//     The orchestrator MUST NOT contact the presumed-dead old-active.
//     We enforce this by NOT calling Pusher.DrainOldActive on the
//     failover path — tests assert the Pusher's drain count stays at
//     zero across a failover.
//
//   * WatchHaEvents (PC-G3) is fed by a `Broadcaster` fan-out. Every
//     role transition emits an HaEvent (ROLE_CHANGED) plus per-phase
//     SWITCHOVER_STARTED / SWITCHOVER_COMPLETED (or FAILOVER_*).
//     Subscribers get a bounded per-subscription buffer (default 32);
//     a slow subscriber that fills its buffer is silently dropped —
//     dashd never blocks the orchestrator on a stuck WatchHaEvents
//     client.
//
//   * Concurrency: every public method takes a mu lock for the state
//     model and releases BEFORE invoking Pusher methods (which may
//     block on network IO). This means another caller can observe an
//     "intermediate" SWITCHING_* state mid-drain — that's the correct
//     behaviour (the proto exposes the umbrella TRANSITIONING enum for
//     exactly this).
//
// Out of scope for PC-G1..G3 (each is documented in the tracker):
//   * Real DPU HA scope dispatch (depends on sim work; PE).
//   * Persistent orchestrator state across dashd restart (PD).
//   * Split-brain detection (PD; requires DASH telemetry stream).
//   * Multi-HA-set saga rollback (covered today by PC-G8 ApplyBatch).
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// ErrNotFound is returned when the named HA set is unknown to the
// orchestrator (no PutHaSet applied, or it was deleted).
var ErrNotFound = errors.New("orchestrator: ha set not found")

// ErrInvalidTransition is returned when a switchover / failover is
// requested against a state that does not permit it (e.g. switchover
// when there is no active member).
var ErrInvalidTransition = errors.New("orchestrator: invalid HA transition")

// Pusher is the southbound abstraction the orchestrator uses to drive
// the actual DPU role flips. Production wires a real dashapi.v1
// client; tests inject a fake to assert call sequencing (PC-G2
// specifically asserts that failover does NOT invoke DrainOldActive).
//
// Methods are expected to be synchronous from the orchestrator's view:
// they return when the DPU has acknowledged the role change (or after
// a configurable timeout, which the implementation honours).
type Pusher interface {
	// DrainOldActive tells the current-active DPU to stop accepting
	// new flows and bleed existing ones to the standby. Returns nil
	// on success; orchestrator aborts the switchover on error.
	// Only called from TriggerSwitchover — TriggerFailover bypasses
	// this method by contract (PC-G2).
	DrainOldActive(ctx context.Context, dpuID, vdpuID, scopeID string) error

	// PromoteToActive tells the target DPU to take the ACTIVE role.
	// Returns nil on success; orchestrator marks the target ACTIVE
	// on success or restores prior roles on error.
	PromoteToActive(ctx context.Context, dpuID, vdpuID, scopeID string) error

	// DemoteToStandby is called on the old-active after a successful
	// drain so it transitions cleanly to STANDBY. Skipped during
	// failover. Errors are logged but do not undo the promotion that
	// already succeeded (we'd rather have one ACTIVE + one in-limbo
	// member than zero ACTIVE members).
	DemoteToStandby(ctx context.Context, dpuID, vdpuID, scopeID string) error
}

// NoOpPusher is the zero-value Pusher used by tests that don't care
// about southbound effects and by production until PE wires the real
// dashapi.v1 client. It records call counts for test assertions.
type NoOpPusher struct {
	mu             sync.Mutex
	DrainCalls     int
	PromoteCalls   int
	DemoteCalls    int
	DrainErr       error // injectable failure for switchover-abort tests
	PromoteErr     error
	DrainDelay     time.Duration // injectable delay for cancellation tests
	LastDrainDpuID string
}

func (n *NoOpPusher) DrainOldActive(ctx context.Context, dpuID, _, _ string) error {
	n.mu.Lock()
	n.DrainCalls++
	n.LastDrainDpuID = dpuID
	delay := n.DrainDelay
	err := n.DrainErr
	n.mu.Unlock()
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}
func (n *NoOpPusher) PromoteToActive(_ context.Context, _, _, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.PromoteCalls++
	return n.PromoteErr
}
func (n *NoOpPusher) DemoteToStandby(_ context.Context, _, _, _ string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.DemoteCalls++
	return nil
}

// Member tracks one DPU's role within an HA set.
type Member struct {
	DpuID            string
	Role             dashcenterv1.HaScopeRole
	LastRoleChangeAt time.Time
	Reason           string
}

// Set is the orchestrator's in-memory view of one HA set. Mirrors
// HaSetSpec + adds the live per-member role.
type Set struct {
	Namespace string
	Name      string
	Mode      string // "active_active" | "active_standby"
	VirtualIP string
	// VdpuID / ScopeID are synthesized from (namespace, name) for
	// PC-G1..G3; PE will pull the real upstream identifiers from the
	// DPU's DASH telemetry.
	VdpuID    string
	ScopeID   string
	FlowSync  dashcenterv1.FlowSyncState
	Members   []Member
}

// clone returns a deep copy safe to return to callers without exposing
// the internal map.
func (s Set) clone() Set {
	out := s
	out.Members = make([]Member, len(s.Members))
	copy(out.Members, s.Members)
	return out
}

// Orchestrator owns the HA state model + the event broadcaster.
// Thread-safe; multiple gRPC streams can read state and subscribe to
// events concurrently with mutating switchover/failover operations.
type Orchestrator struct {
	pusher Pusher

	mu    sync.Mutex
	sets  map[string]*Set // key = "ns/name"
	bcast *Broadcaster
}

// New constructs an Orchestrator. Pass NoOpPusher{} for the
// stub-southbound case (matches today's sim, and what production
// uses until PE).
func New(pusher Pusher) *Orchestrator {
	if pusher == nil {
		pusher = &NoOpPusher{}
	}
	return &Orchestrator{
		pusher: pusher,
		sets:   map[string]*Set{},
		bcast:  NewBroadcaster(),
	}
}

// Broadcaster exposes the per-orchestrator event fan-out.
func (o *Orchestrator) Broadcaster() *Broadcaster { return o.bcast }

func key(ns, name string) string { return ns + "/" + name }

// SyncFromSpec registers (or updates) an HA set in the orchestrator
// from the PutHaSet wire spec. Idempotent: re-applying a spec preserves
// any existing roles for members that are still in member_dpu_ids;
// members removed from the spec are dropped from state; new members
// start in STANDBY.
//
// Wired into service.PutHaSet so the orchestrator always has a model
// of every applied HA set.
func (o *Orchestrator) SyncFromSpec(spec *dashcenterv1.HaSetSpec) {
	if spec == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()

	ns := spec.GetNamespace()
	if ns == "" {
		ns = "default"
	}
	name := spec.GetName()
	k := key(ns, name)
	now := time.Now()

	existing := o.sets[k]
	newMembers := make([]Member, 0, len(spec.GetMemberDpuIds()))
	for i, dpuID := range spec.GetMemberDpuIds() {
		role := dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY
		reason := "registered from spec"
		lastChange := now
		if existing != nil {
			for _, m := range existing.Members {
				if m.DpuID == dpuID {
					role = m.Role
					reason = m.Reason
					lastChange = m.LastRoleChangeAt
					break
				}
			}
		}
		// First member of a new set is auto-promoted to ACTIVE so a
		// switchover has somewhere to flip FROM. Without this seed,
		// every new HA set sits in all-STANDBY limbo until an operator
		// manually triggers a promote, which is friction we don't need.
		if existing == nil && i == 0 {
			role = dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE
			reason = "auto-promoted (first member of new set)"
		}
		newMembers = append(newMembers, Member{
			DpuID: dpuID, Role: role, Reason: reason, LastRoleChangeAt: lastChange,
		})
	}

	set := &Set{
		Namespace: ns,
		Name:      name,
		Mode:      spec.GetMode(),
		VirtualIP: spec.GetVirtualIp(),
		VdpuID:    name + "-vdpu",
		ScopeID:   name + "-scope",
		FlowSync:  dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_NONE,
		Members:   newMembers,
	}
	if len(newMembers) >= 2 {
		// Two or more members → flow sync is required. We optimistically
		// start in SYNCED; a real DASH telemetry stream would flip this
		// to INITIATING/SYNCING during a switchover.
		set.FlowSync = dashcenterv1.FlowSyncState_FLOW_SYNC_STATE_SYNCED
	}
	o.sets[k] = set
}

// Remove drops an HA set from the orchestrator (called by service.Delete
// when an HaSet spec is deleted from the store).
func (o *Orchestrator) Remove(ns, name string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.sets, key(ns, name))
}

// GetSet returns a snapshot of one HA set, or ErrNotFound.
func (o *Orchestrator) GetSet(ns, name string) (Set, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	set, ok := o.sets[key(ns, name)]
	if !ok {
		return Set{}, fmt.Errorf("%w: %s/%s", ErrNotFound, ns, name)
	}
	return set.clone(), nil
}

// ListSets returns snapshots of every known HA set, sorted by
// (namespace, name).
func (o *Orchestrator) ListSets() []Set {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Set, 0, len(o.sets))
	for _, s := range o.sets {
		out = append(out, s.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// pickTarget returns the dpuID that should become ACTIVE if targetDpuID
// is empty (highest-priority STANDBY = first STANDBY in declared order).
// Returns "" if no eligible target exists.
func pickTarget(set *Set, requested string) string {
	if requested != "" {
		return requested
	}
	for _, m := range set.Members {
		if m.Role == dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY {
			return m.DpuID
		}
	}
	return ""
}

// findMember returns a pointer to the named member within the set, or
// nil. Set's lock must be held by the caller.
func findMember(set *Set, dpuID string) *Member {
	for i := range set.Members {
		if set.Members[i].DpuID == dpuID {
			return &set.Members[i]
		}
	}
	return nil
}

// snapshot returns a clone for events / streaming reads.
func snapshot(set *Set) Set { return set.clone() }

// TriggerSwitchover performs a planned drain-first role flip from the
// current ACTIVE to the target STANDBY (or picked highest-priority
// STANDBY if targetDpuID is empty).
//
// Returns the channel of HaScopeStatus updates streamed during the
// transition. The channel is closed when the switchover terminates
// (success or failure). The accompanying error is non-nil iff the
// switchover could not start (unknown HA set, no eligible target).
// In-flight failures are surfaced via a final HaScopeStatus row with
// the role rolled back to its pre-switchover value.
func (o *Orchestrator) TriggerSwitchover(ctx context.Context, ns, name, targetDpuID, reason string) (<-chan dashcenterv1.HaScopeStatus, error) {
	o.mu.Lock()
	set, ok := o.sets[key(ns, name)]
	if !ok {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, ns, name)
	}
	// Find the current ACTIVE.
	var active *Member
	for i := range set.Members {
		if set.Members[i].Role == dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE {
			active = &set.Members[i]
			break
		}
	}
	if active == nil {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: %s/%s has no ACTIVE member", ErrInvalidTransition, ns, name)
	}
	targetID := pickTarget(set, targetDpuID)
	if targetID == "" {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: %s/%s has no STANDBY target", ErrInvalidTransition, ns, name)
	}
	if targetID == active.DpuID {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: target %s is already ACTIVE", ErrInvalidTransition, targetID)
	}
	target := findMember(set, targetID)
	if target == nil {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: target %s is not in HA set %s/%s", ErrInvalidTransition, targetID, ns, name)
	}

	// Capture the pre-flip roles so we can roll back if the drain fails.
	priorActiveRole := active.Role
	priorTargetRole := target.Role
	activeID := active.DpuID
	vdpu, scope := set.VdpuID, set.ScopeID

	// Flip ACTIVE -> SWITCHING_TO_STANDBY immediately under lock so a
	// concurrent reader sees the transitional state.
	o.setRoleLocked(set, activeID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_STANDBY, reason)
	o.setRoleLocked(set, targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_ACTIVE, reason)
	o.bcast.Publish(haEvent(dashcenterv1.HaEvent_TYPE_SWITCHOVER_STARTED, ns, name, activeID, priorActiveRole, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_STANDBY))
	o.mu.Unlock()

	out := make(chan dashcenterv1.HaScopeStatus, 8)
	go func() {
		defer close(out)

		emit := func(dpuID string, role dashcenterv1.HaScopeRole, why string) {
			out <- statusFor(ns, vdpu, scope, dpuID, role, why)
		}
		emit(activeID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_STANDBY, "drain started")
		emit(targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_ACTIVE, "promotion staged")

		// 1. Drain the old active.
		if err := o.pusher.DrainOldActive(ctx, activeID, vdpu, scope); err != nil {
			o.rollbackSwitchover(ns, name, activeID, targetID, priorActiveRole, priorTargetRole, "drain failed: "+err.Error())
			emit(activeID, priorActiveRole, "switchover rolled back: drain failed")
			emit(targetID, priorTargetRole, "switchover rolled back: drain failed")
			return
		}

		// 2. Promote the new active.
		if err := o.pusher.PromoteToActive(ctx, targetID, vdpu, scope); err != nil {
			o.rollbackSwitchover(ns, name, activeID, targetID, priorActiveRole, priorTargetRole, "promote failed: "+err.Error())
			emit(activeID, priorActiveRole, "switchover rolled back: promote failed")
			emit(targetID, priorTargetRole, "switchover rolled back: promote failed")
			return
		}

		// 3. Demote the old active to STANDBY.
		_ = o.pusher.DemoteToStandby(ctx, activeID, vdpu, scope)

		o.mu.Lock()
		o.setRoleLocked(set, activeID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY, "switchover complete")
		o.setRoleLocked(set, targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE, "switchover complete")
		o.mu.Unlock()
		o.bcast.Publish(haEvent(dashcenterv1.HaEvent_TYPE_SWITCHOVER_COMPLETED, ns, name, targetID, priorTargetRole, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE))
		emit(activeID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY, "switchover complete")
		emit(targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE, "switchover complete")
	}()
	return out, nil
}

// TriggerFailover performs an immediate role flip without contacting
// the presumed-dead old-active. PC-G2: the Pusher.DrainOldActive
// method is NEVER invoked on this path.
//
// failedDpuID must be a known member of the HA set; absent that, the
// orchestrator returns ErrInvalidTransition (we will not declare an
// unknown DPU dead and start flipping roles).
func (o *Orchestrator) TriggerFailover(ctx context.Context, ns, name, failedDpuID, targetDpuID, reason string) (<-chan dashcenterv1.HaScopeStatus, error) {
	o.mu.Lock()
	set, ok := o.sets[key(ns, name)]
	if !ok {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, ns, name)
	}
	failed := findMember(set, failedDpuID)
	if failed == nil {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: failed_dpu_id %s is not in HA set %s/%s", ErrInvalidTransition, failedDpuID, ns, name)
	}
	// Pick a target — any STANDBY other than the failed one.
	targetID := targetDpuID
	if targetID == "" {
		for _, m := range set.Members {
			if m.DpuID == failedDpuID {
				continue
			}
			if m.Role == dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_STANDBY {
				targetID = m.DpuID
				break
			}
		}
	}
	if targetID == "" || targetID == failedDpuID {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: no eligible STANDBY target for failover in %s/%s", ErrInvalidTransition, ns, name)
	}
	target := findMember(set, targetID)
	if target == nil {
		o.mu.Unlock()
		return nil, fmt.Errorf("%w: target %s is not in HA set %s/%s", ErrInvalidTransition, targetID, ns, name)
	}
	priorTargetRole := target.Role
	priorFailedRole := failed.Role
	vdpu, scope := set.VdpuID, set.ScopeID

	// Immediately mark the failed member DEAD and the target SWITCHING_TO_ACTIVE.
	// PC-G2: NO call to Pusher.DrainOldActive — the whole point of
	// failover is that the old-active is unreachable.
	o.setRoleLocked(set, failedDpuID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_DEAD, "declared dead by failover: "+reason)
	o.setRoleLocked(set, targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_ACTIVE, reason)
	o.bcast.Publish(haEvent(dashcenterv1.HaEvent_TYPE_FAILOVER_STARTED, ns, name, failedDpuID, priorFailedRole, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_DEAD))
	o.mu.Unlock()

	out := make(chan dashcenterv1.HaScopeStatus, 8)
	go func() {
		defer close(out)
		out <- statusFor(ns, vdpu, scope, failedDpuID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_DEAD, "declared dead")
		out <- statusFor(ns, vdpu, scope, targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_SWITCHING_TO_ACTIVE, "promotion staged")
		if err := o.pusher.PromoteToActive(ctx, targetID, vdpu, scope); err != nil {
			// Roll back the target (failed member stays DEAD — the operator
			// asserted it; we don't second-guess that decision).
			o.mu.Lock()
			o.setRoleLocked(set, targetID, priorTargetRole, "promote failed: "+err.Error())
			o.mu.Unlock()
			out <- statusFor(ns, vdpu, scope, targetID, priorTargetRole, "failover rolled back: promote failed")
			return
		}
		o.mu.Lock()
		o.setRoleLocked(set, targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE, "failover complete")
		o.mu.Unlock()
		o.bcast.Publish(haEvent(dashcenterv1.HaEvent_TYPE_FAILOVER_COMPLETED, ns, name, targetID, priorTargetRole, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE))
		out <- statusFor(ns, vdpu, scope, targetID, dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE, "failover complete")
	}()
	return out, nil
}

// setRoleLocked mutates one member's role + emits the ROLE_CHANGED
// event on the broadcaster. Caller must hold o.mu.
func (o *Orchestrator) setRoleLocked(set *Set, dpuID string, role dashcenterv1.HaScopeRole, reason string) {
	m := findMember(set, dpuID)
	if m == nil {
		return
	}
	if m.Role == role {
		return
	}
	prior := m.Role
	m.Role = role
	m.Reason = reason
	m.LastRoleChangeAt = time.Now()
	o.bcast.Publish(haEvent(dashcenterv1.HaEvent_TYPE_ROLE_CHANGED, set.Namespace, set.Name, dpuID, prior, role))
}

// rollbackSwitchover restores prior roles on both members. Holds o.mu
// internally so the caller MUST NOT hold it (we only call this from
// the goroutine post-Unlock).
func (o *Orchestrator) rollbackSwitchover(ns, name, activeID, targetID string, priorActive, priorTarget dashcenterv1.HaScopeRole, reason string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	set, ok := o.sets[key(ns, name)]
	if !ok {
		return
	}
	o.setRoleLocked(set, activeID, priorActive, reason)
	o.setRoleLocked(set, targetID, priorTarget, reason)
}

// statusFor builds an HaScopeStatus wire row.
func statusFor(ns, vdpu, scope, dpuID string, role dashcenterv1.HaScopeRole, reason string) dashcenterv1.HaScopeStatus {
	return dashcenterv1.HaScopeStatus{
		Namespace:    ns,
		VdpuId:       vdpu,
		HaScopeId:    scope,
		DpuId:        dpuID,
		Role:         role,
		IsRoleHolder: role == dashcenterv1.HaScopeRole_HA_SCOPE_ROLE_ACTIVE,
		Reason:       reason,
	}
}

// haEvent builds an HaEvent for the broadcaster.
func haEvent(t dashcenterv1.HaEvent_Type, ns, name, dpuID string, prior, next dashcenterv1.HaScopeRole) dashcenterv1.HaEvent {
	return dashcenterv1.HaEvent{
		Type:         t,
		Namespace:    ns,
		HaSetName:    name,
		DpuId:        dpuID,
		PreviousRole: prior,
		NewRole:      next,
	}
}
