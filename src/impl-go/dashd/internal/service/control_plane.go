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
}

// NewControlPlane creates a new ControlPlaneService. The capacity
// tracker and schema gate are optional — pass nil to disable admission
// gates (used by existing unit tests; production wires both).
func NewControlPlane(st store.DesiredStore, inv *inventory.Inventory, rec *reconciler.Reconciler, cap *capacity.Tracker, gate *schema.Gate) ControlPlaneService {
	return &controlPlaneService{store: st, inv: inv, rec: rec, nsv: namespace.NewValidator(st), cap: cap, gate: gate}
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