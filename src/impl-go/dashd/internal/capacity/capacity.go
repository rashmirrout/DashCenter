// Package capacity implements pre-write admission control for dashd.
//
// PB-1's job: reject every Put* operation that would push a DPU past
// the hard limits the DPU itself advertises (DpuCapacityLimits). The
// tracker keeps an in-memory per-DPU usage counter, derived from the
// desired store, and the service layer (internal/service) consults it
// before every store.Put.
//
// What we count per DPU (PB-1 scope):
//
//	enis           — count of EniSpecs placed on this DPU
//	acl_rules      — sum of len(AclPolicySpec.rules) across every ACL
//	                 policy whose eni_names[] include any ENI on this DPU
//	vnet_mappings  — count of VnetMappingSpecs (these are namespace-scoped,
//	                 placement is fleet-wide today, so the per-DPU count
//	                 equals the total. PC will tighten placement.)
//
// Out of scope for PB-1 (deferred to PB-2 / PB-3):
//
//	max_routes / max_route_groups / max_route_rules  — route-policy counting
//	max_active_flows / max_pps / max_bps             — live counters, not
//	                                                    desired-state derived
//	max_meters / max_meter_rules                     — Phase 2 PD signals
//	SimulateApply (no-write preview)                 — PB-2
//	Schema/capability gating (e.g. ServiceTunnel)    — PB-3
//
// Placement model:
//
//   - ENIs land on the DPU(s) in spec.placement_hint_dpu_ids[]. When the
//     hint is empty, today we conservatively count the ENI against every
//     DPU in the inventory (the placement engine has not yet decided
//     where it will go). PC will tighten this to "the engine's chosen
//     DPU set" once the placement decision is recorded.
//
//   - ACL policies and route policies inherit their per-DPU placement
//     from the ENIs they reference. An ACL policy bound to ENIs e1, e2
//     contributes its rules to every DPU hosting any of {e1, e2}.
//
// Concurrency:
//
//   - The tracker is safe for concurrent reads. Writes (Apply / Remove)
//     take an exclusive lock; they are called from the service layer
//     which already holds the per-key serialization implied by
//     store.DesiredStore.Put.
//
//   - Recount(ctx) snapshots the entire store under lock. Use it on
//     startup (after store.Open returns) and on any out-of-band event
//     that may have desynced the counters (e.g. PA-1b etcd Watch
//     compaction-resync — store emits EventResync).
package capacity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// ErrResourceExhausted is returned when a Put would push a DPU past its
// advertised limit. The error message always includes (dpu, dimension,
// limit, current, requested) so the operator can act without digging
// through logs. Callers wrap it with the service-layer's
// ErrResourceExhausted sentinel.
var ErrResourceExhausted = errors.New("capacity: resource exhausted")

// Inventory is the subset of *inventory.Inventory the tracker needs.
// Defined as an interface so tests can provide a fake without booting
// the full inventory machinery.
type Inventory interface {
	Get(id string) (inventory.DpuEntry, error)
	List() []inventory.DpuEntry
}

// usage holds the per-DPU counters the tracker maintains.
type usage struct {
	enis         int64
	aclRules     int64
	vnetMappings int64
}

// Tracker maintains per-DPU usage counters and enforces admission.
// Safe for concurrent use.
type Tracker struct {
	inv Inventory

	mu sync.RWMutex
	// byDPU[dpuID] → current usage on that DPU.
	byDPU map[string]*usage
	// eniDPUs[namespace][eniName] → the DPU ids that host this ENI.
	// Used by Apply/Remove for ACL policies (which reference ENIs by
	// name) to know which DPU(s) to attribute rules to.
	eniDPUs map[string]map[string][]string
	// vnetMappingPresence[namespace][mappingKey] tracks which mappings
	// we've already counted, so a Put of an existing mapping is a no-op
	// instead of double-counting.
	vnetMappingPresence map[string]map[string]struct{}
}

// NewTracker constructs an empty tracker. Call Recount before serving
// admission to populate the counters from the existing desired state.
func NewTracker(inv Inventory) *Tracker {
	return &Tracker{
		inv:                 inv,
		byDPU:               map[string]*usage{},
		eniDPUs:             map[string]map[string][]string{},
		vnetMappingPresence: map[string]map[string]struct{}{},
	}
}

// Recount rebuilds the in-memory counters from the desired store.
// Idempotent. Call on startup and after a store.EventResync.
func (t *Tracker) Recount(ctx context.Context, st store.DesiredStore) error {
	// Snapshot all spec kinds we care about.
	dpus := t.inv.List()
	if len(dpus) == 0 {
		// No DPUs registered yet → nothing to count. The first
		// inventory.Register will be followed by a Recount.
		t.mu.Lock()
		t.byDPU = map[string]*usage{}
		t.eniDPUs = map[string]map[string][]string{}
		t.vnetMappingPresence = map[string]map[string]struct{}{}
		t.mu.Unlock()
		return nil
	}

	// Collect all ENIs first because ACL/route policies reference them
	// by name and we need the ENI → DPU mapping resolved before
	// attributing rule counts.
	allEnis := map[string]map[string]*dashcenterv1.EniSpec{} // ns → name → spec
	allMappings := map[string]map[string]*dashcenterv1.VnetMappingSpec{}
	allAcls := map[string]map[string]*dashcenterv1.AclPolicySpec{}

	// We list per namespace. The store doesn't expose "all namespaces"
	// — we iterate over every namespace ever seen for these kinds via a
	// known set ("default" for Phase 1; multi-tenant comes with PA-5).
	// Recount is best-effort: an empty list for an unknown namespace is
	// not an error, it just means "no specs to count there".
	for _, ns := range t.knownNamespaces() {
		if err := ctx.Err(); err != nil {
			return err
		}
		enis, err := st.List(ctx, ns, "eni")
		if err != nil {
			return fmt.Errorf("capacity: recount list eni in %s: %w", ns, err)
		}
		for _, sp := range enis {
			e := &dashcenterv1.EniSpec{}
			if err := unmarshalSpec(sp.Data, e); err != nil {
				continue
			}
			if allEnis[ns] == nil {
				allEnis[ns] = map[string]*dashcenterv1.EniSpec{}
			}
			allEnis[ns][sp.Key.Name] = e
		}

		mappings, err := st.List(ctx, ns, "vnet_mapping")
		if err != nil {
			return fmt.Errorf("capacity: recount list vnet_mapping in %s: %w", ns, err)
		}
		for _, sp := range mappings {
			m := &dashcenterv1.VnetMappingSpec{}
			if err := unmarshalSpec(sp.Data, m); err != nil {
				continue
			}
			if allMappings[ns] == nil {
				allMappings[ns] = map[string]*dashcenterv1.VnetMappingSpec{}
			}
			allMappings[ns][sp.Key.Name] = m
		}

		acls, err := st.List(ctx, ns, "acl_policy")
		if err != nil {
			return fmt.Errorf("capacity: recount list acl_policy in %s: %w", ns, err)
		}
		for _, sp := range acls {
			a := &dashcenterv1.AclPolicySpec{}
			if err := unmarshalSpec(sp.Data, a); err != nil {
				continue
			}
			if allAcls[ns] == nil {
				allAcls[ns] = map[string]*dashcenterv1.AclPolicySpec{}
			}
			allAcls[ns][sp.Key.Name] = a
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.byDPU = map[string]*usage{}
	t.eniDPUs = map[string]map[string][]string{}
	t.vnetMappingPresence = map[string]map[string]struct{}{}

	// Seed every known DPU with a zero usage so List has a stable shape.
	for _, d := range dpus {
		t.byDPU[d.ID] = &usage{}
	}

	// ENIs.
	for ns, enis := range allEnis {
		t.eniDPUs[ns] = map[string][]string{}
		for name, e := range enis {
			targets := t.placementForEni(e, dpus)
			t.eniDPUs[ns][name] = targets
			for _, dpuID := range targets {
				t.byDPU[dpuID].enis++
			}
		}
	}

	// VnetMappings — fleet-wide for Phase 1.
	for ns, mappings := range allMappings {
		if t.vnetMappingPresence[ns] == nil {
			t.vnetMappingPresence[ns] = map[string]struct{}{}
		}
		for _, m := range mappings {
			key := mappingNameOf(m)
			if key == "" {
				continue
			}
			t.vnetMappingPresence[ns][key] = struct{}{}
			for _, d := range dpus {
				t.byDPU[d.ID].vnetMappings++
			}
		}
	}

	// ACL rules. Sum the rule count of each policy onto every DPU
	// hosting any of its referenced ENIs.
	for ns, acls := range allAcls {
		for _, a := range acls {
			ruleCount := int64(len(a.GetRules()))
			if ruleCount == 0 {
				continue
			}
			dpuSet := map[string]struct{}{}
			for _, eni := range a.GetEniNames() {
				if e, ok := t.eniDPUs[ns][eni]; ok {
					for _, dpu := range e {
						dpuSet[dpu] = struct{}{}
					}
				}
			}
			for dpu := range dpuSet {
				if u := t.byDPU[dpu]; u != nil {
					u.aclRules += ruleCount
				}
			}
		}
	}

	return nil
}

// CheckEni admits an EniSpec write. Returns nil on success or
// ErrResourceExhausted (wrapped with detail) if any target DPU is at
// max_enis.
//
// For PB-1 we conservatively count an empty placement_hint against ALL
// DPUs in the inventory — the placement engine has not yet decided
// where this ENI will go, so the safe admission is "block if it
// wouldn't fit on every candidate". PC's placement engine will let us
// tighten this.
func (t *Tracker) CheckEni(ns string, spec *dashcenterv1.EniSpec) error {
	if spec == nil {
		return nil
	}
	targets := t.placementForEni(spec, t.inv.List())

	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, dpuID := range targets {
		entry, err := t.inv.Get(dpuID)
		if err != nil {
			// Unknown DPU in the placement hint — fail-closed so an
			// operator typo doesn't quietly accept the spec.
			return fmt.Errorf("%w: eni placement target %q is not a registered DPU",
				ErrResourceExhausted, dpuID)
		}
		// If the DPU hasn't advertised limits yet, we can't admission-check
		// against it — allow the write but the operator should see no
		// limits in /admin/health.
		if entry.Limits == nil || entry.Limits.MaxEnis <= 0 {
			continue
		}
		current := int64(0)
		if u := t.byDPU[dpuID]; u != nil {
			current = u.enis
		}
		// Is this ENI already counted? If we're updating an existing
		// ENI on the same DPU, the count doesn't change. We approximate
		// by checking whether the spec name appears in eniDPUs[ns].
		if existing, ok := t.eniDPUs[ns][spec.GetName()]; ok {
			if contains(existing, dpuID) {
				// Already counted — no admission delta.
				continue
			}
		}
		if current+1 > entry.Limits.MaxEnis {
			return fmt.Errorf("%w: dpu=%s dimension=max_enis limit=%d current=%d requested=+1",
				ErrResourceExhausted, dpuID, entry.Limits.MaxEnis, current)
		}
	}
	return nil
}

// CheckVnetMapping admits a VnetMappingSpec. PB-1 treats mappings as
// fleet-wide (one per DPU). Checks every DPU's max_vnet_mappings.
func (t *Tracker) CheckVnetMapping(ns string, spec *dashcenterv1.VnetMappingSpec) error {
	if spec == nil {
		return nil
	}
	mappingKey := mappingNameOf(spec)

	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, d := range t.inv.List() {
		if d.Limits == nil || d.Limits.MaxVnetMappings <= 0 {
			continue
		}
		current := int64(0)
		if u := t.byDPU[d.ID]; u != nil {
			current = u.vnetMappings
		}
		// Updating an existing mapping → no delta. We approximate by
		// checking the byDPU count: PB-1 doesn't track per-mapping
		// per-DPU presence (fleet-wide model), so an update of an
		// existing mapping is a no-delta operation and a new mapping
		// is +1.
		if t.mappingExists(ns, mappingKey) {
			continue
		}
		if current+1 > d.Limits.MaxVnetMappings {
			return fmt.Errorf("%w: dpu=%s dimension=max_vnet_mappings limit=%d current=%d requested=+1",
				ErrResourceExhausted, d.ID, d.Limits.MaxVnetMappings, current)
		}
	}
	return nil
}

// CheckAclPolicy admits an AclPolicySpec. For each DPU hosting any of
// the referenced ENIs, sums the rule-count delta against
// max_acl_rules_per_group. Updating an existing policy contributes the
// delta (new_rules - old_rules) only.
func (t *Tracker) CheckAclPolicy(ns string, spec *dashcenterv1.AclPolicySpec, oldRuleCount int64) error {
	if spec == nil {
		return nil
	}
	newRuleCount := int64(len(spec.GetRules()))
	delta := newRuleCount - oldRuleCount
	if delta <= 0 {
		// Shrinking or staying the same → no admission concern.
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	// Collect target DPUs from the referenced ENIs.
	dpuSet := map[string]struct{}{}
	for _, eni := range spec.GetEniNames() {
		if hosts, ok := t.eniDPUs[ns][eni]; ok {
			for _, d := range hosts {
				dpuSet[d] = struct{}{}
			}
		}
	}

	for dpuID := range dpuSet {
		entry, err := t.inv.Get(dpuID)
		if err != nil {
			continue
		}
		if entry.Limits == nil || entry.Limits.MaxAclRulesPerGroup <= 0 {
			continue
		}
		current := int64(0)
		if u := t.byDPU[dpuID]; u != nil {
			current = u.aclRules
		}
		if current+delta > entry.Limits.MaxAclRulesPerGroup {
			return fmt.Errorf("%w: dpu=%s dimension=max_acl_rules_per_group limit=%d current=%d requested=+%d",
				ErrResourceExhausted, dpuID, entry.Limits.MaxAclRulesPerGroup, current, delta)
		}
	}
	return nil
}

// ApplyEni records a successful Put for an EniSpec. Idempotent for
// re-applies on the same DPU set (no double-counting).
func (t *Tracker) ApplyEni(ns string, spec *dashcenterv1.EniSpec) {
	if spec == nil {
		return
	}
	targets := t.placementForEni(spec, t.inv.List())

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.eniDPUs[ns] == nil {
		t.eniDPUs[ns] = map[string][]string{}
	}
	previous := t.eniDPUs[ns][spec.GetName()]

	// Decrement removed targets.
	for _, p := range previous {
		if !contains(targets, p) {
			if u := t.byDPU[p]; u != nil && u.enis > 0 {
				u.enis--
			}
		}
	}
	// Increment new targets.
	for _, n := range targets {
		if !contains(previous, n) {
			if t.byDPU[n] == nil {
				t.byDPU[n] = &usage{}
			}
			t.byDPU[n].enis++
		}
	}
	t.eniDPUs[ns][spec.GetName()] = targets
}

// RemoveEni records a successful Delete.
func (t *Tracker) RemoveEni(ns, name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.eniDPUs[ns] == nil {
		return
	}
	targets := t.eniDPUs[ns][name]
	for _, p := range targets {
		if u := t.byDPU[p]; u != nil && u.enis > 0 {
			u.enis--
		}
	}
	delete(t.eniDPUs[ns], name)
}

// ApplyVnetMapping records a Put. Idempotent: a Put for an existing
// mapping is a no-op (PB-1 tracks count, not per-DPU presence).
func (t *Tracker) ApplyVnetMapping(ns string, spec *dashcenterv1.VnetMappingSpec) {
	if spec == nil {
		return
	}
	key := mappingNameOf(spec)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.mappingExistsLocked(ns, key) {
		return
	}
	t.markMappingLocked(ns, key)
	for _, d := range t.inv.List() {
		if t.byDPU[d.ID] == nil {
			t.byDPU[d.ID] = &usage{}
		}
		t.byDPU[d.ID].vnetMappings++
	}
}

// RemoveVnetMapping records a successful Delete.
func (t *Tracker) RemoveVnetMapping(ns, key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.mappingExistsLocked(ns, key) {
		return
	}
	t.unmarkMappingLocked(ns, key)
	for _, d := range t.inv.List() {
		if u := t.byDPU[d.ID]; u != nil && u.vnetMappings > 0 {
			u.vnetMappings--
		}
	}
}

// ApplyAclPolicy records a Put. Pass the OLD rule-count so we apply the
// correct delta (update with fewer rules → decrement).
func (t *Tracker) ApplyAclPolicy(ns string, spec *dashcenterv1.AclPolicySpec, oldRuleCount int64) {
	if spec == nil {
		return
	}
	newRuleCount := int64(len(spec.GetRules()))
	delta := newRuleCount - oldRuleCount

	t.mu.Lock()
	defer t.mu.Unlock()

	dpuSet := map[string]struct{}{}
	for _, eni := range spec.GetEniNames() {
		if hosts, ok := t.eniDPUs[ns][eni]; ok {
			for _, d := range hosts {
				dpuSet[d] = struct{}{}
			}
		}
	}
	for dpuID := range dpuSet {
		if t.byDPU[dpuID] == nil {
			t.byDPU[dpuID] = &usage{}
		}
		t.byDPU[dpuID].aclRules += delta
		if t.byDPU[dpuID].aclRules < 0 {
			// Defensive — shouldn't happen if ApplyAclPolicy is always
			// preceded by a matching RemoveAclPolicy. Clamp at zero
			// so we don't underflow.
			t.byDPU[dpuID].aclRules = 0
		}
	}
}

// RemoveAclPolicy records a Delete. ruleCount is the count from the
// last-known version of the spec (callers fetch it before issuing the
// delete).
func (t *Tracker) RemoveAclPolicy(ns string, spec *dashcenterv1.AclPolicySpec) {
	if spec == nil {
		return
	}
	ruleCount := int64(len(spec.GetRules()))
	if ruleCount == 0 {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	dpuSet := map[string]struct{}{}
	for _, eni := range spec.GetEniNames() {
		if hosts, ok := t.eniDPUs[ns][eni]; ok {
			for _, d := range hosts {
				dpuSet[d] = struct{}{}
			}
		}
	}
	for dpuID := range dpuSet {
		if u := t.byDPU[dpuID]; u != nil {
			u.aclRules -= ruleCount
			if u.aclRules < 0 {
				u.aclRules = 0
			}
		}
	}
}

// SnapshotForDPU returns a copy of the counters for one DPU. Empty
// struct (all zeros) for an unknown DPU. Used by /admin/health and by
// PB-2's SimulateApply preview.
func (t *Tracker) SnapshotForDPU(dpuID string) (enis, aclRules, vnetMappings int64) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if u := t.byDPU[dpuID]; u != nil {
		return u.enis, u.aclRules, u.vnetMappings
	}
	return 0, 0, 0
}

// --- internal helpers -------------------------------------------------

// placementForEni returns the DPU ids an ENI should be counted against.
// When placement_hint_dpu_ids is non-empty, that's the answer (operators
// are explicit — we honour their decision even for cordoned DPUs; the
// service layer enforces the cordon ban for explicit-hint Puts and
// surfaces a clear error). When empty, returns every NON-CORDONED DPU
// in the inventory (PC-1: cordon excludes from fleet-wide fallback).
func (t *Tracker) placementForEni(spec *dashcenterv1.EniSpec, allDPUs []inventory.DpuEntry) []string {
	if spec == nil {
		return nil
	}
	hints := spec.GetPlacementHintDpuIds()
	if len(hints) > 0 {
		out := make([]string, 0, len(hints))
		for _, h := range hints {
			out = append(out, h)
		}
		return out
	}
	out := make([]string, 0, len(allDPUs))
	for _, d := range allDPUs {
		if d.Cordoned {
			continue
		}
		out = append(out, d.ID)
	}
	return out
}

// mappingNameOf returns the canonical store key for a VnetMappingSpec.
// Mirrors the service-layer logic (vnet_name + "-" + ip_address).
func mappingNameOf(spec *dashcenterv1.VnetMappingSpec) string {
	if spec == nil {
		return ""
	}
	name := spec.GetVnetName()
	if spec.GetIpAddress() != "" {
		name = name + "-" + spec.GetIpAddress()
	}
	return name
}

// vnetMappingPresence and the helper functions below let CheckVnetMapping
// / ApplyVnetMapping distinguish "new mapping" from "update of an
// existing mapping" without scanning the desired store on every Put.

func (t *Tracker) mappingExists(ns, key string) bool {
	if t.vnetMappingPresence[ns] == nil {
		return false
	}
	_, ok := t.vnetMappingPresence[ns][key]
	return ok
}

func (t *Tracker) mappingExistsLocked(ns, key string) bool {
	return t.mappingExists(ns, key)
}

func (t *Tracker) markMappingLocked(ns, key string) {
	if t.vnetMappingPresence[ns] == nil {
		t.vnetMappingPresence[ns] = map[string]struct{}{}
	}
	t.vnetMappingPresence[ns][key] = struct{}{}
}

func (t *Tracker) unmarkMappingLocked(ns, key string) {
	if t.vnetMappingPresence[ns] != nil {
		delete(t.vnetMappingPresence[ns], key)
	}
}

// knownNamespaces enumerates the namespaces we should scan during
// Recount. Phase 1 + PA-5 keep "default" as the canonical namespace;
// once PD adds dynamic namespace registration this becomes a real
// lookup against inventory metadata.
func (t *Tracker) knownNamespaces() []string {
	return []string{store.DefaultNamespace}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// unmarshalSpec decodes a stored spec's JSON bytes into the target
// proto message. We intentionally use encoding/json (not protojson)
// because the file + etcd stores serialise via encoding/json — keep the
// codec symmetric.
func unmarshalSpec(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}

// -- PB-2: SimulateApply support --------------------------------------
//
// Simulate runs the same admission math that CheckEni / CheckVnetMapping /
// CheckAclPolicy would run, but for a *batch* of proposed operations
// and WITHOUT mutating tracker state. It returns one PerDPUDelta entry
// per affected DPU plus a flat list of validation errors. The service
// layer turns this into a SimulateApplyResult on the wire.
//
// The implementation works against a transient overlay over the live
// counters so the order of operations within the batch is honoured (a
// Put-Put-Delete sequence on the same key cancels out, etc.). Live
// counters are read under RLock once at the start; the overlay is local
// to the call.

// SimOp is one proposed operation in a Simulate batch.
type SimOp struct {
	Action    string // "put" | "delete"
	Namespace string
	Kind      string // eni | vnet_mapping | acl_policy
	// Spec is the proto spec for "put"; ignored for "delete".
	Spec any
	// Name is required for "delete"; for "put" it is derived from the spec.
	Name string
}

// SimError is one validation/admission failure produced by Simulate.
// Strings only — this is operator-facing dry-run output and must not
// leak internal structs.
type SimError struct {
	Op     int    // index in the input []SimOp
	Reason string // human-readable, includes (dpu, dimension, limit, current, requested)
}

// PerDPUDelta is the net change in usage counters for a DPU after
// applying the entire batch. Negative deltas mean "frees capacity".
type PerDPUDelta struct {
	DpuID             string
	DeltaEnis         int64
	DeltaVnetMappings int64
	DeltaAclRules     int64
	// ExceedsCapacity is true if applying the batch would push at least
	// one dimension on this DPU past its advertised limit. Reason
	// carries the first such breach (operator can iterate one fix at a
	// time).
	ExceedsCapacity bool
	Reason          string
}

// SimulateResult is the full output of Simulate.
type SimulateResult struct {
	WouldSucceed bool
	Errors       []SimError
	PerDPU       []PerDPUDelta
}

// Simulate runs a batch of proposed operations against an overlay and
// returns the per-DPU deltas + admission verdict. Does not mutate the
// tracker. Safe to call concurrently with live admission traffic.
func (t *Tracker) Simulate(ops []SimOp) SimulateResult {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Overlay state: deep-copy only the slices we'll mutate.
	type overlayUsage struct{ enis, aclRules, vnetMappings int64 }
	overlay := map[string]*overlayUsage{}
	for id, u := range t.byDPU {
		overlay[id] = &overlayUsage{u.enis, u.aclRules, u.vnetMappings}
	}
	// Make sure every DPU known to the inventory appears in the overlay
	// (so a fresh DPU with zero usage gets a row in PerDPU).
	for _, d := range t.inv.List() {
		if _, ok := overlay[d.ID]; !ok {
			overlay[d.ID] = &overlayUsage{}
		}
	}
	// eniDPUs overlay: shallow-copy the per-ns maps then deep-copy the
	// per-name slice when we touch it.
	overlayEni := map[string]map[string][]string{}
	for ns, m := range t.eniDPUs {
		dst := make(map[string][]string, len(m))
		for k, v := range m {
			cp := make([]string, len(v))
			copy(cp, v)
			dst[k] = cp
		}
		overlayEni[ns] = dst
	}
	// vnetMapping presence overlay.
	overlayMap := map[string]map[string]struct{}{}
	for ns, m := range t.vnetMappingPresence {
		dst := make(map[string]struct{}, len(m))
		for k := range m {
			dst[k] = struct{}{}
		}
		overlayMap[ns] = dst
	}
	// Acl rule counts overlay (per ns/name → rules currently charged).
	// Recount doesn't keep this map; we rebuild it lazily as we see ACL
	// ops within the batch. Live values are derived from byDPU which is
	// already in `overlay`.
	overlayAclRules := map[string]map[string]int64{}
	currentAclRules := func(ns, name string) int64 {
		if m, ok := overlayAclRules[ns]; ok {
			if v, ok := m[name]; ok {
				return v
			}
		}
		// Live tracker doesn't keep per-policy rule counts, so for ops
		// against a live policy the caller must pass the previous count
		// via an explicit DELETE-then-PUT pair in the batch. This is
		// how dashctl's --diff flow will be wired (PB-3).
		return 0
	}
	setAclRules := func(ns, name string, v int64) {
		if overlayAclRules[ns] == nil {
			overlayAclRules[ns] = map[string]int64{}
		}
		overlayAclRules[ns][name] = v
	}

	dpus := t.inv.List()
	dpuByID := make(map[string]inventory.DpuEntry, len(dpus))
	for _, d := range dpus {
		dpuByID[d.ID] = d
	}

	var errs []SimError
	for i, op := range ops {
		switch op.Action {
		case "put":
			switch op.Kind {
			case "eni":
				spec, ok := op.Spec.(*dashcenterv1.EniSpec)
				if !ok || spec == nil {
					errs = append(errs, SimError{Op: i, Reason: "put eni: spec is nil or wrong type"})
					continue
				}
				ns := op.Namespace
				targets := t.placementForEni(spec, dpus)
				prev := overlayEni[ns][spec.GetName()]
				// Capacity check: count net new placements only.
				for _, dpuID := range targets {
					entry, ok := dpuByID[dpuID]
					if !ok {
						errs = append(errs, SimError{Op: i, Reason: fmt.Sprintf("put eni %q: placement target %q is not a registered DPU", spec.GetName(), dpuID)})
						continue
					}
					if entry.Limits == nil || entry.Limits.MaxEnis <= 0 {
						continue
					}
					if contains(prev, dpuID) {
						continue
					}
					u := overlay[dpuID]
					if u.enis+1 > entry.Limits.MaxEnis {
						errs = append(errs, SimError{
							Op: i,
							Reason: fmt.Sprintf("dpu=%s dimension=max_enis limit=%d current=%d requested=+1",
								dpuID, entry.Limits.MaxEnis, u.enis),
						})
						continue
					}
				}
				// Apply to overlay.
				for _, p := range prev {
					if !contains(targets, p) {
						if u := overlay[p]; u != nil && u.enis > 0 {
							u.enis--
						}
					}
				}
				for _, n := range targets {
					if !contains(prev, n) {
						if overlay[n] == nil {
							overlay[n] = &overlayUsage{}
						}
						overlay[n].enis++
					}
				}
				if overlayEni[ns] == nil {
					overlayEni[ns] = map[string][]string{}
				}
				overlayEni[ns][spec.GetName()] = append([]string{}, targets...)

			case "vnet_mapping":
				spec, ok := op.Spec.(*dashcenterv1.VnetMappingSpec)
				if !ok || spec == nil {
					errs = append(errs, SimError{Op: i, Reason: "put vnet_mapping: spec is nil or wrong type"})
					continue
				}
				ns := op.Namespace
				key := mappingNameOf(spec)
				if key == "" {
					errs = append(errs, SimError{Op: i, Reason: "put vnet_mapping: empty key (vnet_name + ip_address)"})
					continue
				}
				exists := false
				if m, ok := overlayMap[ns]; ok {
					_, exists = m[key]
				}
				if !exists {
					// Admission: charge against every DPU.
					for _, d := range dpus {
						if d.Limits == nil || d.Limits.MaxVnetMappings <= 0 {
							continue
						}
						u := overlay[d.ID]
						if u.vnetMappings+1 > d.Limits.MaxVnetMappings {
							errs = append(errs, SimError{
								Op: i,
								Reason: fmt.Sprintf("dpu=%s dimension=max_vnet_mappings limit=%d current=%d requested=+1",
									d.ID, d.Limits.MaxVnetMappings, u.vnetMappings),
							})
						}
					}
					if overlayMap[ns] == nil {
						overlayMap[ns] = map[string]struct{}{}
					}
					overlayMap[ns][key] = struct{}{}
					for _, d := range dpus {
						if overlay[d.ID] == nil {
							overlay[d.ID] = &overlayUsage{}
						}
						overlay[d.ID].vnetMappings++
					}
				}

			case "acl_policy":
				spec, ok := op.Spec.(*dashcenterv1.AclPolicySpec)
				if !ok || spec == nil {
					errs = append(errs, SimError{Op: i, Reason: "put acl_policy: spec is nil or wrong type"})
					continue
				}
				ns := op.Namespace
				newRules := int64(len(spec.GetRules()))
				oldRules := currentAclRules(ns, spec.GetName())
				delta := newRules - oldRules
				// Resolve DPU set from referenced ENIs (use overlay).
				dpuSet := map[string]struct{}{}
				for _, eni := range spec.GetEniNames() {
					if hosts, ok := overlayEni[ns][eni]; ok {
						for _, d := range hosts {
							dpuSet[d] = struct{}{}
						}
					}
				}
				if delta > 0 {
					for dpuID := range dpuSet {
						entry, ok := dpuByID[dpuID]
						if !ok {
							continue
						}
						if entry.Limits == nil || entry.Limits.MaxAclRulesPerGroup <= 0 {
							continue
						}
						u := overlay[dpuID]
						if u.aclRules+delta > entry.Limits.MaxAclRulesPerGroup {
							errs = append(errs, SimError{
								Op: i,
								Reason: fmt.Sprintf("dpu=%s dimension=max_acl_rules_per_group limit=%d current=%d requested=+%d",
									dpuID, entry.Limits.MaxAclRulesPerGroup, u.aclRules, delta),
							})
						}
					}
				}
				// Apply delta to overlay regardless (so the per-dpu
				// numbers in the result reflect the requested state,
				// even when capacity is exceeded — operators want to
				// see how far over the limit they are).
				for dpuID := range dpuSet {
					if overlay[dpuID] == nil {
						overlay[dpuID] = &overlayUsage{}
					}
					overlay[dpuID].aclRules += delta
					if overlay[dpuID].aclRules < 0 {
						overlay[dpuID].aclRules = 0
					}
				}
				setAclRules(ns, spec.GetName(), newRules)

			default:
				errs = append(errs, SimError{Op: i, Reason: fmt.Sprintf("put: unsupported kind %q (PB-2 supports eni|vnet_mapping|acl_policy)", op.Kind)})
			}

		case "delete":
			if op.Name == "" {
				errs = append(errs, SimError{Op: i, Reason: "delete: name is required"})
				continue
			}
			ns := op.Namespace
			switch op.Kind {
			case "eni":
				prev := overlayEni[ns][op.Name]
				for _, p := range prev {
					if u := overlay[p]; u != nil && u.enis > 0 {
						u.enis--
					}
				}
				if overlayEni[ns] != nil {
					delete(overlayEni[ns], op.Name)
				}
			case "vnet_mapping":
				// Op.Name carries the canonical key for deletes.
				exists := false
				if m, ok := overlayMap[ns]; ok {
					_, exists = m[op.Name]
				}
				if exists {
					delete(overlayMap[ns], op.Name)
					for _, d := range dpus {
						if u := overlay[d.ID]; u != nil && u.vnetMappings > 0 {
							u.vnetMappings--
						}
					}
				}
			case "acl_policy":
				// Without the prior spec we don't know which ENIs were
				// referenced. For PB-2 we keep delete a best-effort
				// no-op against the overlay counters; PB-3's diff-aware
				// dry-run will fetch the prior spec before issuing the
				// simulated delete.
				setAclRules(ns, op.Name, 0)
			default:
				errs = append(errs, SimError{Op: i, Reason: fmt.Sprintf("delete: unsupported kind %q", op.Kind)})
			}
		default:
			errs = append(errs, SimError{Op: i, Reason: fmt.Sprintf("unknown action %q (want put|delete)", op.Action)})
		}
	}

	// Build per-DPU diff against the snapshot at entry.
	perDPU := make([]PerDPUDelta, 0, len(overlay))
	for id, after := range overlay {
		before := &usage{}
		if u := t.byDPU[id]; u != nil {
			before = u
		}
		entry, hasLimits := dpuByID[id]
		row := PerDPUDelta{
			DpuID:             id,
			DeltaEnis:         after.enis - before.enis,
			DeltaVnetMappings: after.vnetMappings - before.vnetMappings,
			DeltaAclRules:     after.aclRules - before.aclRules,
		}
		// Surface a per-row capacity flag — duplicates info already in
		// Errors[] but lets a table renderer light up the offending row
		// without iterating the error list.
		if hasLimits && entry.Limits != nil {
			if entry.Limits.MaxEnis > 0 && after.enis > entry.Limits.MaxEnis {
				row.ExceedsCapacity = true
				row.Reason = fmt.Sprintf("max_enis: %d > %d", after.enis, entry.Limits.MaxEnis)
			}
			if entry.Limits.MaxVnetMappings > 0 && after.vnetMappings > entry.Limits.MaxVnetMappings {
				row.ExceedsCapacity = true
				if row.Reason == "" {
					row.Reason = fmt.Sprintf("max_vnet_mappings: %d > %d", after.vnetMappings, entry.Limits.MaxVnetMappings)
				}
			}
			if entry.Limits.MaxAclRulesPerGroup > 0 && after.aclRules > entry.Limits.MaxAclRulesPerGroup {
				row.ExceedsCapacity = true
				if row.Reason == "" {
					row.Reason = fmt.Sprintf("max_acl_rules_per_group: %d > %d", after.aclRules, entry.Limits.MaxAclRulesPerGroup)
				}
			}
		}
		// Only emit rows that have non-zero deltas OR that are flagged
		// — this keeps output noise-free when the batch touches 2 DPUs
		// in a 100-DPU fleet.
		if row.DeltaEnis != 0 || row.DeltaVnetMappings != 0 || row.DeltaAclRules != 0 || row.ExceedsCapacity {
			perDPU = append(perDPU, row)
		}
	}

	return SimulateResult{
		WouldSucceed: len(errs) == 0,
		Errors:       errs,
		PerDPU:       perDPU,
	}
}
