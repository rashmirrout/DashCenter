// Package service provides the transport-agnostic business logic layer.
// Both REST and gRPC handlers are thin adapters that delegate to this layer.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/namespace"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/operations"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/schema"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// ErrInvalidArgument indicates a validation failure.
var ErrInvalidArgument = errors.New("invalid argument")

// ErrResourceExhausted indicates a per-DPU capacity limit would be
// exceeded by accepting this write. Maps to HTTP 429 / gRPC
// RESOURCE_EXHAUSTED. Locked decision D4 (impl-phases.md): admission is
// hard-fail when limits are advertised; nil limits allow writes through
// (capacity tracker logs a warning).
var ErrResourceExhausted = errors.New("resource exhausted")

// ErrFailedPrecondition indicates a capability or schema gate rejected
// the write (PB-3). Maps to HTTP 412 / gRPC FAILED_PRECONDITION. The
// underlying schema-package error carries (dpu, kind, reason) so the
// operator can act without reading dashd logs.
var ErrFailedPrecondition = errors.New("failed precondition")

// PutResult is the response from any Put* operation.
type PutResult struct {
	Accepted   bool  `json:"accepted"`
	Generation int64 `json:"generation"`
}

// StoredItem is the response from Get/List operations.
type StoredItem struct {
	Kind       string          `json:"kind"`
	Name       string          `json:"name"`
	Namespace  string          `json:"namespace"`
	Generation int64           `json:"generation"`
	Spec       json.RawMessage `json:"spec"`
}

// DriftItem represents a single declared-vs-observed discrepancy.
type DriftItem struct {
	Kind   string `json:"kind"`
	DpuID  string `json:"dpu_id"`
	Op     string `json:"op"`
	Detail string `json:"detail,omitempty"`
}

// DpuStatus is the runtime view of a DPU.
type DpuStatus struct {
	ID       string                  `json:"id"`
	Endpoint string                  `json:"endpoint"`
	State    dashcenterv1.DpuState   `json:"state"`
	Labels   map[string]string       `json:"labels,omitempty"`
}

// ControlPlaneService defines the transport-agnostic API for all mutating
// and read operations on the dashcenter.v1 data model.
type ControlPlaneService interface {
	PutVnet(ctx context.Context, ns string, spec *dashcenterv1.VnetSpec) (*PutResult, error)
	PutEni(ctx context.Context, ns string, spec *dashcenterv1.EniSpec) (*PutResult, error)
	PutVnetMapping(ctx context.Context, ns string, spec *dashcenterv1.VnetMappingSpec) (*PutResult, error)
	PutAclPolicy(ctx context.Context, ns string, spec *dashcenterv1.AclPolicySpec) (*PutResult, error)
	PutRoutePolicy(ctx context.Context, ns string, spec *dashcenterv1.RoutePolicySpec) (*PutResult, error)
	PutHaSet(ctx context.Context, ns string, spec *dashcenterv1.HaSetSpec) (*PutResult, error)
	PutServiceTunnel(ctx context.Context, ns string, spec *dashcenterv1.ServiceTunnelSpec) (*PutResult, error)

	PutInventory(ctx context.Context, dpus []DpuInput) error
	GetInventory(ctx context.Context) ([]DpuStatus, error)

	// RegisterDpu (PB-3) attaches advertised DpuCapacityLimits and
	// DpuCapabilities to a DPU that was previously added via
	// PutInventory. This is the capability-discovery seam: until a
	// DPU calls RegisterDpu the schema gate is permissive (nil caps
	// == "not yet known", MC-3 contract). The DPU must already exist
	// in inventory; this RPC does not create one (use PutInventory
	// for that).
	RegisterDpu(ctx context.Context, reg DpuRegistration) error

	// CordonDpu (PC-1) flags the DPU as ineligible for new fleet-wide
	// ENI placements. Existing ENIs stay put; operators must invoke
	// DrainDpu (PC-7) to evacuate. An explicit placement_hint that
	// names a cordoned DPU is rejected by PutEni with
	// ErrFailedPrecondition. Idempotent. Errors: ErrInvalidArgument
	// (empty id), ErrNotFound (no such DPU).
	CordonDpu(ctx context.Context, dpuID, reason string) error
	UncordonDpu(ctx context.Context, dpuID, reason string) error
	ListCordonedDpus(ctx context.Context) []string

	// ApplyBatch (PC-8) commits a list of Put/Delete ops atomically.
	// Either every op lands in the desired store, or none do — the
	// saga coordinator rolls back any partially-applied ops on the
	// first failure (reverse order). Returns the saga.Result envelope
	// describing the outcome; the error is non-nil iff the batch was
	// not committed.
	//
	// PC-8 wires the existing per-kind Put / Delete machinery through
	// the saga; capacity + schema admission still run per op (so a
	// batch that would exceed capacity is rejected at op-i, then the
	// previous i-1 are rolled back).
	ApplyBatch(ctx context.Context, ops []BatchOp) (*BatchResult, error)

	Delete(ctx context.Context, ns, kind, name string) error
	Get(ctx context.Context, ns, kind, name string) (*StoredItem, error)
	List(ctx context.Context, ns, kind string) ([]*StoredItem, error)

	Reconcile(ctx context.Context) error

	// SimulateApply (PB-2) is a read-only dry-run: it accepts a batch of
	// proposed Put/Delete operations and returns per-DPU capacity deltas
	// plus a flat list of validation/admission errors. The store and
	// tracker are not mutated; safe to call concurrently with live
	// admission traffic.
	SimulateApply(ctx context.Context, ops []SimulateOp) (*SimulateResult, error)
}

// DpuInput is the input for PutInventory.
type DpuInput struct {
	ID       string            `json:"id"`
	Endpoint string            `json:"endpoint"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// DpuRegistration is the input for RegisterDpu (PB-3). At least one of
// Limits or Capabilities should be non-nil — a registration with both
// nil is rejected to avoid silently clearing previously-advertised
// values via a misconfigured client.
type DpuRegistration struct {
	ID           string                          `json:"id"`
	Limits       *dashcenterv1.DpuCapacityLimits `json:"limits,omitempty"`
	Capabilities *dashcenterv1.DpuCapabilities   `json:"capabilities,omitempty"`
}

// controlPlaneService is the default implementation.
type controlPlaneService struct {
	store store.DesiredStore
	inv   *inventory.Inventory
	rec   *reconciler.Reconciler
	nsv   *namespace.Validator
	// cap is the per-DPU capacity tracker. nil means no admission
	// gates (legacy / test). When non-nil, PutEni/PutVnetMapping/
	// PutAclPolicy consult it before persisting and on success update
	// its internal counters; Delete decrements them.
	cap *capacity.Tracker
	// gate is the capability/schema admission checker (PB-3). nil
	// disables all capability checks (legacy / test). When non-nil,
	// PutServiceTunnel / PutHaSet consult CheckKind, and PutEni /
	// PutVnetMapping / PutRoutePolicy / PutServiceTunnel consult
	// CheckSpec for IPv6 underlay gating.
	gate *schema.Gate
	// ops is the cordon / drain manager (PC-1). nil disables cordon
	// admission (legacy tests). When non-nil, PutEni rejects an
	// explicit placement_hint that names a cordoned DPU — capacity's
	// fleet-wide fallback already skips cordoned DPUs, so the only
	// way to land an ENI on one is via explicit hint, and we want
	// that to fail loudly.
	ops *operations.Manager
}

// NewControlPlane creates a new ControlPlaneService. The capacity
// tracker and schema gate are optional — pass nil to disable admission
// gates (used by existing unit tests; production wires both).
func NewControlPlane(st store.DesiredStore, inv *inventory.Inventory, rec *reconciler.Reconciler, cap *capacity.Tracker, gate *schema.Gate, ops *operations.Manager) ControlPlaneService {
	return &controlPlaneService{store: st, inv: inv, rec: rec, nsv: namespace.NewValidator(st), cap: cap, gate: gate, ops: ops}
}

// validKinds is the set of spec kinds the service layer recognizes.
var validKinds = map[string]bool{
	"vnet":           true,
	"eni":            true,
	"vnet_mapping":   true,
	"acl_policy":     true,
	"route_policy":   true,
	"ha_set":         true,
	"service_tunnel": true,
}

// specForKind returns an empty spec of the correct type for deserialization.
func specForKind(kind string) (any, bool) {
	switch kind {
	case "vnet":
		return &dashcenterv1.VnetSpec{}, true
	case "eni":
		return &dashcenterv1.EniSpec{}, true
	case "vnet_mapping":
		return &dashcenterv1.VnetMappingSpec{}, true
	case "acl_policy":
		return &dashcenterv1.AclPolicySpec{}, true
	case "route_policy":
		return &dashcenterv1.RoutePolicySpec{}, true
	case "ha_set":
		return &dashcenterv1.HaSetSpec{}, true
	case "service_tunnel":
		return &dashcenterv1.ServiceTunnelSpec{}, true
	default:
		return nil, false
	}
}

func resolveNS(ns string) string {
	if ns == "" {
		return store.DefaultNamespace
	}
	return ns
}

// --- Put operations ---

func (s *controlPlaneService) PutVnet(ctx context.Context, ns string, spec *dashcenterv1.VnetSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckVnet(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "vnet", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutEni(ctx context.Context, ns string, spec *dashcenterv1.EniSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckEni(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.ops != nil {
		for _, hint := range spec.GetPlacementHintDpuIds() {
			if s.ops.IsCordoned(hint) {
				return nil, fmt.Errorf("%w: placement_hint dpu=%s is cordoned (uncordon first or pick another DPU)",
					ErrFailedPrecondition, hint)
			}
		}
	}
	if s.gate != nil {
		if err := s.gate.CheckSpec(spec.GetPlacementHintDpuIds(), "eni", spec); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
		}
	}
	if s.cap != nil {
		if err := s.cap.CheckEni(ns, spec); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrResourceExhausted, err)
		}
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "eni", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	if s.cap != nil {
		s.cap.ApplyEni(ns, spec)
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutVnetMapping(ctx context.Context, ns string, spec *dashcenterv1.VnetMappingSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	// VnetMapping uses "vnet_name-ip_address" as the key if no separate name field.
	name := spec.GetVnetName()
	if name == "" {
		return nil, fmt.Errorf("%w: vnet_name is required", ErrInvalidArgument)
	}
	if spec.GetIpAddress() != "" {
		name = name + "-" + spec.GetIpAddress()
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckVnetMapping(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.gate != nil {
		if err := s.gate.CheckSpec(nil, "vnet_mapping", spec); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
		}
	}
	if s.cap != nil {
		if err := s.cap.CheckVnetMapping(ns, spec); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrResourceExhausted, err)
		}
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "vnet_mapping", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	if s.cap != nil {
		s.cap.ApplyVnetMapping(ns, spec)
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutAclPolicy(ctx context.Context, ns string, spec *dashcenterv1.AclPolicySpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckAclPolicy(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	// For updates we need the previous rule count so the tracker only
	// admits the delta (not the full new rule count on top of old).
	oldRuleCount := int64(0)
	if s.cap != nil {
		if sp, err := s.store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: "acl_policy", Name: name}); err == nil {
			var old dashcenterv1.AclPolicySpec
			if json.Unmarshal(sp.Data, &old) == nil {
				oldRuleCount = int64(len(old.GetRules()))
			}
		}
		if err := s.cap.CheckAclPolicy(ns, spec, oldRuleCount); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrResourceExhausted, err)
		}
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "acl_policy", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	if s.cap != nil {
		s.cap.ApplyAclPolicy(ns, spec, oldRuleCount)
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutRoutePolicy(ctx context.Context, ns string, spec *dashcenterv1.RoutePolicySpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckRoutePolicy(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.gate != nil {
		if err := s.gate.CheckSpec(nil, "route_policy", spec); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
		}
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "route_policy", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutHaSet(ctx context.Context, ns string, spec *dashcenterv1.HaSetSpec) (*PutResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
	}
	name := spec.GetName()
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	if err := s.nsv.CheckHaSet(ctx, ns, spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
	}
	if s.gate != nil {
		// HA must hold on every member DPU listed in the set; an empty
		// member list falls back to fleet-wide (the validator already
		// rejects an HA set with no members, so this is defensive).
		if err := s.gate.CheckKind(spec.GetMemberDpuIds(), "ha_set"); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
		}
	}
	gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "ha_set", Name: name}, spec, int64(spec.GetExpectedGeneration()))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

func (s *controlPlaneService) PutServiceTunnel(ctx context.Context, ns string, spec *dashcenterv1.ServiceTunnelSpec) (*PutResult, error) {
if spec == nil {
return nil, fmt.Errorf("%w: spec is nil", ErrInvalidArgument)
}
name := spec.Name
if name == "" {
return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
}
ns = resolveNS(ns)
if err := s.nsv.CheckServiceTunnel(ctx, ns, spec); err != nil {
	return nil, fmt.Errorf("%w: %w", ErrInvalidArgument, err)
}
if s.gate != nil {
	// ServiceTunnel is fleet-wide today (no placement field), so the
	// kind gate must hold on EVERY registered DPU — if any single DPU
	// lacks caps.service_tunnel we reject (PB-G3). The spec gate
	// additionally enforces IPv6 underlay constraints (PB-G4 friend).
	if err := s.gate.CheckKind(nil, "service_tunnel"); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
	}
	if err := s.gate.CheckSpec(nil, "service_tunnel", spec); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFailedPrecondition, err)
	}
}
gen, err := s.store.Put(ctx, store.ObjectKey{Namespace: ns, Kind: "service_tunnel", Name: name}, spec, int64(spec.ExpectedGeneration))
	if err != nil {
		return nil, err
	}
	return &PutResult{Accepted: true, Generation: gen}, nil
}

// --- Inventory operations ---

func (s *controlPlaneService) PutInventory(ctx context.Context, dpus []DpuInput) error {
	for _, d := range dpus {
		if d.ID == "" {
			return fmt.Errorf("%w: dpu id is required", ErrInvalidArgument)
		}
		if d.Endpoint == "" {
			return fmt.Errorf("%w: dpu endpoint is required for %s", ErrInvalidArgument, d.ID)
		}
		if err := s.inv.Register(inventory.DpuEntry{
			ID: d.ID, Endpoint: d.Endpoint, Labels: d.Labels,
		}); err != nil {
			return fmt.Errorf("register %s: %w", d.ID, err)
		}
	}
	return nil
}

func (s *controlPlaneService) RegisterDpu(ctx context.Context, reg DpuRegistration) error {
	if reg.ID == "" {
		return fmt.Errorf("%w: dpu id is required", ErrInvalidArgument)
	}
	if reg.Limits == nil && reg.Capabilities == nil {
		return fmt.Errorf("%w: at least one of limits or capabilities must be set", ErrInvalidArgument)
	}
	if _, err := s.inv.Get(reg.ID); err != nil {
		return fmt.Errorf("%w: dpu %q is not registered (call PutInventory first)", ErrInvalidArgument, reg.ID)
	}
	if reg.Limits != nil {
		if err := s.inv.SetLimits(reg.ID, reg.Limits); err != nil {
			return fmt.Errorf("register %s: %w", reg.ID, err)
		}
	}
	if reg.Capabilities != nil {
		if err := s.inv.SetCapabilities(reg.ID, reg.Capabilities); err != nil {
			return fmt.Errorf("register %s: %w", reg.ID, err)
		}
	}
	return nil
}

func (s *controlPlaneService) GetInventory(ctx context.Context) ([]DpuStatus, error) {
	entries := s.inv.List()
	result := make([]DpuStatus, len(entries))
	for i, e := range entries {
		result[i] = DpuStatus{
			ID:       e.ID,
			Endpoint: e.Endpoint,
			State:    e.State,
			Labels:   e.Labels,
		}
	}
	return result, nil
}

// --- Cordon / Uncordon (PC-1) -----------------------------------------

func (s *controlPlaneService) CordonDpu(ctx context.Context, dpuID, reason string) error {
	if dpuID == "" {
		return fmt.Errorf("%w: dpu id is required", ErrInvalidArgument)
	}
	if s.ops == nil {
		return fmt.Errorf("%w: operations manager not configured", ErrInvalidArgument)
	}
	if err := s.ops.Cordon(dpuID, reason); err != nil {
		if errors.Is(err, operations.ErrNotFound) {
			return fmt.Errorf("%w: dpu %q not in inventory", ErrInvalidArgument, dpuID)
		}
		return err
	}
	return nil
}

func (s *controlPlaneService) UncordonDpu(ctx context.Context, dpuID, reason string) error {
	if dpuID == "" {
		return fmt.Errorf("%w: dpu id is required", ErrInvalidArgument)
	}
	if s.ops == nil {
		return fmt.Errorf("%w: operations manager not configured", ErrInvalidArgument)
	}
	if err := s.ops.Uncordon(dpuID, reason); err != nil {
		if errors.Is(err, operations.ErrNotFound) {
			return fmt.Errorf("%w: dpu %q not in inventory", ErrInvalidArgument, dpuID)
		}
		return err
	}
	return nil
}

func (s *controlPlaneService) ListCordonedDpus(ctx context.Context) []string {
	if s.ops == nil {
		return nil
	}
	return s.ops.ListCordoned()
}

// --- CRUD operations ---

func (s *controlPlaneService) Delete(ctx context.Context, ns, kind, name string) error {
	if kind == "" {
		return fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	// Read the spec before deletion so we can decrement capacity
	// counters correctly (we need fields like placement hints / rule
	// counts that the bare key doesn't carry).
	var before *store.StoredSpec
	if s.cap != nil {
		if sp, err := s.store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name}); err == nil {
			before = sp
		}
	}
	if err := s.store.Delete(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name}); err != nil {
		return err
	}
	if s.cap != nil && before != nil {
		switch kind {
		case "eni":
			s.cap.RemoveEni(ns, name)
		case "vnet_mapping":
			var spec dashcenterv1.VnetMappingSpec
			if json.Unmarshal(before.Data, &spec) == nil {
				key := spec.GetVnetName()
				if spec.GetIpAddress() != "" {
					key = key + "-" + spec.GetIpAddress()
				}
				s.cap.RemoveVnetMapping(ns, key)
			}
		case "acl_policy":
			var spec dashcenterv1.AclPolicySpec
			if json.Unmarshal(before.Data, &spec) == nil {
				s.cap.RemoveAclPolicy(ns, &spec)
			}
		}
	}
	return nil
}

func (s *controlPlaneService) Get(ctx context.Context, ns, kind, name string) (*StoredItem, error) {
	if kind == "" {
		return nil, fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	sp, err := s.store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name})
	if err != nil {
		return nil, err
	}
	return &StoredItem{
		Kind:       sp.Key.Kind,
		Name:       sp.Key.Name,
		Namespace:  sp.Key.Namespace,
		Generation: sp.Generation,
		Spec:       json.RawMessage(sp.Data),
	}, nil
}

func (s *controlPlaneService) List(ctx context.Context, ns, kind string) ([]*StoredItem, error) {
	if kind == "" {
		return nil, fmt.Errorf("%w: kind is required", ErrInvalidArgument)
	}
	ns = resolveNS(ns)
	specs, err := s.store.List(ctx, ns, kind)
	if err != nil {
		return nil, err
	}
	items := make([]*StoredItem, len(specs))
	for i, sp := range specs {
		items[i] = &StoredItem{
			Kind:       sp.Key.Kind,
			Name:       sp.Key.Name,
			Namespace:  sp.Key.Namespace,
			Generation: sp.Generation,
			Spec:       json.RawMessage(sp.Data),
		}
	}
	return items, nil
}

// --- Reconcile ---

func (s *controlPlaneService) Reconcile(ctx context.Context) error {
	if s.rec != nil {
		s.rec.ForceReconcile()
	}
	return nil
}

// --- SimulateApply (PB-2) ---

// SimulateOp is one operation in a SimulateApply batch.
//
// Action is "put" or "delete". For "put", exactly one of the spec
// pointers must be non-nil and Kind must match. For "delete", Name +
// Kind are required (Spec is ignored). Namespace defaults to "default"
// if empty.
type SimulateOp struct {
	Action          string                          `json:"action"`
	Namespace       string                          `json:"namespace,omitempty"`
	Kind            string                          `json:"kind"`
	Name            string                          `json:"name,omitempty"`
	EniSpec         *dashcenterv1.EniSpec           `json:"eni,omitempty"`
	VnetMappingSpec *dashcenterv1.VnetMappingSpec   `json:"vnet_mapping,omitempty"`
	AclPolicySpec   *dashcenterv1.AclPolicySpec     `json:"acl_policy,omitempty"`
}

// SimulateDpuImpact is the per-DPU row of a SimulateResult.
type SimulateDpuImpact struct {
	DpuID             string `json:"dpu_id"`
	DeltaEnis         int64  `json:"delta_enis"`
	DeltaVnetMappings int64  `json:"delta_vnet_mappings"`
	DeltaAclRules     int64  `json:"delta_acl_rules"`
	ExceedsCapacity   bool   `json:"exceeds_capacity,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

// SimulateResult is what SimulateApply returns: an aggregate verdict, a
// flat list of validation/admission errors, and one row per affected
// DPU.
type SimulateResult struct {
	WouldSucceed     bool                 `json:"would_succeed"`
	ValidationErrors []string             `json:"validation_errors,omitempty"`
	PerDpuImpact     []*SimulateDpuImpact `json:"per_dpu_impact,omitempty"`
}

// SimulateApply runs a read-only admission check over a batch of
// proposed ops. Returns ErrInvalidArgument when ops is empty or
// malformed (these are upfront input errors — distinct from the
// per-op admission errors carried inside SimulateResult).
func (s *controlPlaneService) SimulateApply(ctx context.Context, ops []SimulateOp) (*SimulateResult, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: ops list is empty", ErrInvalidArgument)
	}
	if s.cap == nil {
		// No capacity tracker configured (legacy test wiring). Return
		// a successful no-op result rather than 500-ing: clients that
		// rely on SimulateApply for would-succeed feedback degrade
		// gracefully.
		return &SimulateResult{WouldSucceed: true}, nil
	}

	// Translate to capacity.SimOp + run pre-flight validation.
	simOps := make([]capacity.SimOp, 0, len(ops))
	var preErrs []string
	for i, op := range ops {
		switch op.Action {
		case "put", "delete":
		default:
			preErrs = append(preErrs, fmt.Sprintf("op[%d]: unknown action %q (want put|delete)", i, op.Action))
			continue
		}
		switch op.Kind {
		case "eni", "vnet_mapping", "acl_policy":
		default:
			preErrs = append(preErrs, fmt.Sprintf("op[%d]: PB-2 supports kind eni|vnet_mapping|acl_policy, got %q", i, op.Kind))
			continue
		}
		ns := resolveNS(op.Namespace)

		so := capacity.SimOp{Action: op.Action, Namespace: ns, Kind: op.Kind, Name: op.Name}
		if op.Action == "put" {
			switch op.Kind {
			case "eni":
				if op.EniSpec == nil {
					preErrs = append(preErrs, fmt.Sprintf("op[%d]: put eni: eni spec is nil", i))
					continue
				}
				so.Spec = op.EniSpec
				if so.Name == "" {
					so.Name = op.EniSpec.GetName()
				}
			case "vnet_mapping":
				if op.VnetMappingSpec == nil {
					preErrs = append(preErrs, fmt.Sprintf("op[%d]: put vnet_mapping: spec is nil", i))
					continue
				}
				so.Spec = op.VnetMappingSpec
			case "acl_policy":
				if op.AclPolicySpec == nil {
					preErrs = append(preErrs, fmt.Sprintf("op[%d]: put acl_policy: spec is nil", i))
					continue
				}
				so.Spec = op.AclPolicySpec
				if so.Name == "" {
					so.Name = op.AclPolicySpec.GetName()
				}
			}
		} else if op.Name == "" {
			preErrs = append(preErrs, fmt.Sprintf("op[%d]: delete %s: name is required", i, op.Kind))
			continue
		}
		simOps = append(simOps, so)
	}

	if len(preErrs) > 0 {
		return &SimulateResult{WouldSucceed: false, ValidationErrors: preErrs}, nil
	}

	r := s.cap.Simulate(simOps)
	out := &SimulateResult{WouldSucceed: r.WouldSucceed}
	for _, e := range r.Errors {
		out.ValidationErrors = append(out.ValidationErrors, fmt.Sprintf("op[%d]: %s", e.Op, e.Reason))
	}
	for _, row := range r.PerDPU {
		out.PerDpuImpact = append(out.PerDpuImpact, &SimulateDpuImpact{
			DpuID:             row.DpuID,
			DeltaEnis:         row.DeltaEnis,
			DeltaVnetMappings: row.DeltaVnetMappings,
			DeltaAclRules:     row.DeltaAclRules,
			ExceedsCapacity:   row.ExceedsCapacity,
			Reason:            row.Reason,
		})
	}
	return out, nil
}
// --- ApplyBatch (PC-8) -------------------------------------------------

// BatchOp is one element of an ApplyBatch request. Shape mirrors
// SimulateOp but covers all dashcenter.v1 kinds (admission gates run
// per op as the saga executor walks them).
type BatchOp struct {
Action            string                          `json:"action"` // "put" | "delete"
Namespace         string                          `json:"namespace,omitempty"`
Kind              string                          `json:"kind"`
Name              string                          `json:"name,omitempty"`
VnetSpec          *dashcenterv1.VnetSpec          `json:"vnet,omitempty"`
EniSpec           *dashcenterv1.EniSpec           `json:"eni,omitempty"`
VnetMappingSpec   *dashcenterv1.VnetMappingSpec   `json:"vnet_mapping,omitempty"`
AclPolicySpec     *dashcenterv1.AclPolicySpec     `json:"acl_policy,omitempty"`
RoutePolicySpec   *dashcenterv1.RoutePolicySpec   `json:"route_policy,omitempty"`
HaSetSpec         *dashcenterv1.HaSetSpec         `json:"ha_set,omitempty"`
ServiceTunnelSpec *dashcenterv1.ServiceTunnelSpec `json:"service_tunnel,omitempty"`
}

// BatchResult is the wire-shape envelope returned by ApplyBatch.
type BatchResult struct {
Committed    bool                  `json:"committed"`
OpsTotal     int                   `json:"ops_total"`
OpsCommitted int                   `json:"ops_committed"`
FailedIndex  int                   `json:"failed_index,omitempty"`
FailedError  string                `json:"failed_error,omitempty"`
Compensated  int                   `json:"compensated,omitempty"`
CompFailures []sagaCompFailureJSON `json:"comp_failures,omitempty"`
}

// sagaCompFailureJSON mirrors saga.ItemError for wire output without
// importing internal/saga at the wire layer (keep the type surface
// thin).
type sagaCompFailureJSON struct {
Index  int    `json:"index"`
Kind   string `json:"kind"`
Name   string `json:"name"`
Action string `json:"action"`
Error  string `json:"error"`
}

// sagaServiceExecutor adapts the service-layer Put / Delete machinery
// (which runs admission gates and updates capacity counters) into a
// saga.Executor. We deliberately go through the public service methods
// rather than the raw store so capacity + schema admission run per op
// inside the batch — a batch that would exceed MaxEnis on the 7th op
// rolls back the first 6 instead of silently committing them.
type sagaServiceExecutor struct {
svc *controlPlaneService
// nsResolved is filled in lazily; each op may carry its own
// namespace.
}

func (e *sagaServiceExecutor) SnapshotPrior(ctx context.Context, op sagaOpAdapter) ([]byte, error) {
sp, err := e.svc.store.Get(ctx, store.ObjectKey{Namespace: op.ns, Kind: op.kind, Name: op.name})
if errors.Is(err, store.ErrNotFound) {
return nil, nil
}
if err != nil {
return nil, err
}
return append([]byte(nil), sp.Data...), nil
}

func (e *sagaServiceExecutor) Execute(ctx context.Context, op sagaOpAdapter) error {
if op.action == "delete" {
return e.svc.Delete(ctx, op.ns, op.kind, op.name)
}
// PUT — dispatch to the right typed handler so admission gates run.
switch op.kind {
case "vnet":
_, err := e.svc.PutVnet(ctx, op.ns, op.vnet)
return err
case "eni":
_, err := e.svc.PutEni(ctx, op.ns, op.eni)
return err
case "vnet_mapping":
_, err := e.svc.PutVnetMapping(ctx, op.ns, op.vnetMapping)
return err
case "acl_policy":
_, err := e.svc.PutAclPolicy(ctx, op.ns, op.aclPolicy)
return err
case "route_policy":
_, err := e.svc.PutRoutePolicy(ctx, op.ns, op.routePolicy)
return err
case "ha_set":
_, err := e.svc.PutHaSet(ctx, op.ns, op.haSet)
return err
case "service_tunnel":
_, err := e.svc.PutServiceTunnel(ctx, op.ns, op.serviceTunnel)
return err
}
return fmt.Errorf("%w: unknown kind %q in batch", ErrInvalidArgument, op.kind)
}

func (e *sagaServiceExecutor) Compensate(ctx context.Context, op sagaOpAdapter, prior []byte) error {
key := store.ObjectKey{Namespace: op.ns, Kind: op.kind, Name: op.name}
if prior == nil {
// Op created the key — reverse with raw store.Delete.
if err := e.svc.store.Delete(ctx, key); err != nil && !errors.Is(err, store.ErrNotFound) {
return err
}
// Best-effort capacity counter cleanup for kinds we know about.
e.maybeRemoveFromCapacity(op.kind, op.ns, op.name, nil)
return nil
}
// Op overwrote an existing payload — restore.
var raw json.RawMessage = prior
if _, err := e.svc.store.Put(ctx, key, raw, 0); err != nil {
return err
}
return nil
}

// maybeRemoveFromCapacity invokes the capacity Remove* methods for
// kinds the tracker counts. Best-effort: if the spec can't be parsed
// we skip; this is rollback, the worst case is a counter that's too
// high until the next Recount.
func (e *sagaServiceExecutor) maybeRemoveFromCapacity(kind, ns, name string, _ []byte) {
if e.svc.cap == nil {
return
}
switch kind {
case "eni":
e.svc.cap.RemoveEni(ns, name)
}
}

// sagaOpAdapter is the typed shape `sagaServiceExecutor` walks. We
// keep it private — callers build BatchOp on the wire and we translate
// here.
type sagaOpAdapter struct {
action        string
ns            string
kind          string
name          string
vnet          *dashcenterv1.VnetSpec
eni           *dashcenterv1.EniSpec
vnetMapping   *dashcenterv1.VnetMappingSpec
aclPolicy     *dashcenterv1.AclPolicySpec
routePolicy   *dashcenterv1.RoutePolicySpec
haSet         *dashcenterv1.HaSetSpec
serviceTunnel *dashcenterv1.ServiceTunnelSpec
}

func (s *controlPlaneService) ApplyBatch(ctx context.Context, batchOps []BatchOp) (*BatchResult, error) {
if len(batchOps) == 0 {
return nil, fmt.Errorf("%w: ops list is empty", ErrInvalidArgument)
}
adapters := make([]sagaOpAdapter, 0, len(batchOps))
for i, o := range batchOps {
if o.Action != "put" && o.Action != "delete" {
return nil, fmt.Errorf("%w: op[%d] action must be put|delete (got %q)", ErrInvalidArgument, i, o.Action)
}
a := sagaOpAdapter{action: o.Action, ns: resolveNS(o.Namespace), kind: o.Kind, name: o.Name}
switch o.Kind {
case "vnet":
a.vnet = o.VnetSpec
if a.vnet != nil && a.name == "" {
a.name = a.vnet.GetName()
}
case "eni":
a.eni = o.EniSpec
if a.eni != nil && a.name == "" {
a.name = a.eni.GetName()
}
case "vnet_mapping":
a.vnetMapping = o.VnetMappingSpec
if a.vnetMapping != nil && a.name == "" {
n := a.vnetMapping.GetVnetName()
if ip := a.vnetMapping.GetIpAddress(); ip != "" {
n = n + "-" + ip
}
a.name = n
}
case "acl_policy":
a.aclPolicy = o.AclPolicySpec
if a.aclPolicy != nil && a.name == "" {
a.name = a.aclPolicy.GetName()
}
case "route_policy":
a.routePolicy = o.RoutePolicySpec
if a.routePolicy != nil && a.name == "" {
a.name = a.routePolicy.GetName()
}
case "ha_set":
a.haSet = o.HaSetSpec
if a.haSet != nil && a.name == "" {
a.name = a.haSet.GetName()
}
case "service_tunnel":
a.serviceTunnel = o.ServiceTunnelSpec
if a.serviceTunnel != nil && a.name == "" {
a.name = a.serviceTunnel.GetName()
}
default:
return nil, fmt.Errorf("%w: op[%d] unknown kind %q", ErrInvalidArgument, i, o.Kind)
}
if a.action == "delete" && a.name == "" {
return nil, fmt.Errorf("%w: op[%d] delete requires name", ErrInvalidArgument, i)
}
adapters = append(adapters, a)
}

ex := &sagaServiceExecutor{svc: s}
// We need a generic saga.Run that accepts our typed adapter; the
// saga package's Op uses any-payload. Run our own minimal serial
// loop here so we don't have to round-trip through saga.Op (we get
// the same semantics: forward-pass, reverse-order rollback,
// compensation-failure surfacing).
res := &BatchResult{OpsTotal: len(adapters)}
priors := make([][]byte, 0, len(adapters))
for i, op := range adapters {
if err := ctx.Err(); err != nil {
res.FailedIndex = i
res.FailedError = err.Error()
return s.rollbackBatch(ex, adapters, priors, res), fmt.Errorf("apply_batch op[%d]: %w", i, err)
}
prior, err := ex.SnapshotPrior(ctx, op)
if err != nil {
res.FailedIndex = i
res.FailedError = "snapshot prior: " + err.Error()
return s.rollbackBatch(ex, adapters, priors, res), fmt.Errorf("apply_batch op[%d] snapshot: %w", i, err)
}
if err := ex.Execute(ctx, op); err != nil {
res.FailedIndex = i
res.FailedError = err.Error()
return s.rollbackBatch(ex, adapters, priors, res), fmt.Errorf("apply_batch op[%d]: %w", i, err)
}
priors = append(priors, prior)
}
res.Committed = true
res.OpsCommitted = len(adapters)
return res, nil
}

func (s *controlPlaneService) rollbackBatch(ex *sagaServiceExecutor, ops []sagaOpAdapter, priors [][]byte, res *BatchResult) *BatchResult {
compCtx := context.Background()
for i := len(priors) - 1; i >= 0; i-- {
op := ops[i]
if err := ex.Compensate(compCtx, op, priors[i]); err != nil {
res.CompFailures = append(res.CompFailures, sagaCompFailureJSON{
Index: i, Kind: op.kind, Name: op.name, Action: op.action, Error: err.Error(),
})
continue
}
res.Compensated++
}
return res
}
