// Package namespace enforces multi-tenant isolation invariants on every
// spec write.
//
// Phase 1 already stores specs under a namespace-scoped key
// (<state_dir>/<namespace>/<kind>/<name>), so the on-disk layout never
// needs migration. PA-5 adds the BEHAVIOURAL enforcement:
//
//  1. Spec-namespace consistency.  If a spec has a non-empty
//     `metadata.namespace` field, it MUST match the URL/parameter
//     namespace the caller used. This catches "I PUT to /v1/ns-a/eni/x
//     but the YAML says namespace: ns-b" — without enforcement, dashd
//     would silently honour the URL and discard the spec-side hint.
//
//  2. Cross-namespace references.  A spec in namespace ns-a MUST NOT
//     reference an object in namespace ns-b. The references we know
//     about today:
//
//        EniSpec.vnet_name              → Vnet (same namespace)
//        VnetMappingSpec.vnet_name      → Vnet (same namespace)
//        AclPolicySpec.eni_names[]      → Eni  (same namespace)
//        RoutePolicySpec.eni_names[]    → Eni  (same namespace)
//        RoutePolicySpec.routes[i].next_hop_target  (when type=vnet)
//                                       → Vnet (same namespace)
//
//     The validator does a store.Get for every referenced object and
//     rejects with ErrCrossNamespace if it's missing OR in a different
//     namespace. (Missing-in-this-namespace and present-elsewhere are
//     the same error from the caller's perspective: "the target you
//     name is not reachable from this namespace".)
//
// What this package does NOT do today:
//   - It does not enforce a namespace allow-list (PD; tenant-scoped RBAC).
//   - It does not delete-cascade when a referenced object is removed
//     (referential integrity on Delete is a Phase-3 enhancement).
//   - It does not validate `service_tunnel` targets or HA-set DPU IDs
//     (DPUs are operator-owned, global, not tenant-scoped).
//
// Plumbing:
//
//	validator := namespace.NewValidator(store)
//	if err := validator.CheckEni(ctx, ns, spec); err != nil { ... }
//
// The validator is read-only; safe to share across goroutines.
package namespace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// Sentinel errors. Callers SHOULD wrap these with the per-operation error
// so the underlying classifier (REST → 400, gRPC → INVALID_ARGUMENT) maps
// correctly.
var (
	// ErrSpecNamespaceMismatch is returned when the spec's own
	// `namespace` field is set and disagrees with the URL/parameter
	// namespace used by the caller.
	ErrSpecNamespaceMismatch = errors.New("namespace: spec.namespace disagrees with operation namespace")

	// ErrCrossNamespace is returned when a spec references an object
	// that lives in a different namespace (or does not exist in the
	// caller's namespace).
	ErrCrossNamespace = errors.New("namespace: cross-namespace reference rejected")

	// ErrDanglingReference is returned when a spec references an
	// object that does not exist (inventory DPU, service_tunnel, etc.).
	ErrDanglingReference = errors.New("referential integrity: dangling reference")

	// ErrHasDependents is returned when deleting an object that is
	// still referenced by other objects.
	ErrHasDependents = errors.New("referential integrity: object has dependents")
)

// Validator checks namespace invariants against a store. It is read-only;
// it never mutates state. Construct one per dashd process and share
// across goroutines.
type Validator struct {
	store store.DesiredStore
	inv   *inventory.Inventory // may be nil — skips DPU existence checks
}

// NewValidator constructs a Validator backed by the given store.
// The store is used only for read-side reference checks (store.Get).
func NewValidator(st store.DesiredStore) *Validator {
	return &Validator{store: st}
}

// WithInventory sets the inventory reference for DPU existence checks.
// Returns the Validator for chaining.
func (v *Validator) WithInventory(inv *inventory.Inventory) *Validator {
	v.inv = inv
	return v
}

// CheckSpecNamespace verifies that a spec's own namespace field (if
// non-empty) matches the operation namespace. Returns nil when:
//   - specNS is empty (caller didn't supply one in the body), OR
//   - specNS equals opNS.
//
// The intent is to honour caller-supplied URL namespace as the
// authoritative source while still rejecting obvious mistakes where
// the YAML body says one thing and the URL says another.
func (v *Validator) CheckSpecNamespace(opNS, specNS string) error {
	if specNS != "" && specNS != opNS {
		return fmt.Errorf("%w: operation namespace=%q, spec.namespace=%q",
			ErrSpecNamespaceMismatch, opNS, specNS)
	}
	return nil
}

// CheckVnet validates a VnetSpec. Vnets have no cross-references, so the
// only check is the spec-namespace consistency. Included for API
// completeness (every Put* hands off to a Check* in the service layer).
func (v *Validator) CheckVnet(_ context.Context, ns string, spec *dashcenterv1.VnetSpec) error {
	if spec == nil {
		return nil
	}
	return v.CheckSpecNamespace(ns, spec.GetNamespace())
}

// CheckEni validates an EniSpec:
//   - spec-namespace consistency (CheckSpecNamespace);
//   - EniSpec.vnet_name MUST exist as a Vnet in the same namespace
//     (unless empty — an ENI without a Vnet is a Phase-2 admission
//     concern, not a namespace concern).
func (v *Validator) CheckEni(ctx context.Context, ns string, spec *dashcenterv1.EniSpec) error {
	if spec == nil {
		return nil
	}
	if err := v.CheckSpecNamespace(ns, spec.GetNamespace()); err != nil {
		return err
	}
	if vn := spec.GetVnetName(); vn != "" {
		if err := v.refExists(ctx, ns, "vnet", vn); err != nil {
			return fmt.Errorf("eni.vnet_name=%q: %w", vn, err)
		}
	}
	return nil
}

// CheckVnetMapping validates a VnetMappingSpec:
//   - spec-namespace consistency;
//   - VnetMappingSpec.vnet_name MUST exist as a Vnet in the same
//     namespace.
func (v *Validator) CheckVnetMapping(ctx context.Context, ns string, spec *dashcenterv1.VnetMappingSpec) error {
	if spec == nil {
		return nil
	}
	if err := v.CheckSpecNamespace(ns, spec.GetNamespace()); err != nil {
		return err
	}
	if vn := spec.GetVnetName(); vn != "" {
		if err := v.refExists(ctx, ns, "vnet", vn); err != nil {
			return fmt.Errorf("vnet_mapping.vnet_name=%q: %w", vn, err)
		}
	}
	return nil
}

// CheckAclPolicy validates an AclPolicySpec:
//   - spec-namespace consistency;
//   - every eni_names[i] MUST exist as an Eni in the same namespace.
//
// An ACL policy that binds zero ENIs is allowed (operator may stage a
// policy before attaching it to ENIs).
func (v *Validator) CheckAclPolicy(ctx context.Context, ns string, spec *dashcenterv1.AclPolicySpec) error {
	if spec == nil {
		return nil
	}
	if err := v.CheckSpecNamespace(ns, spec.GetNamespace()); err != nil {
		return err
	}
	for i, eni := range spec.GetEniNames() {
		if eni == "" {
			continue
		}
		if err := v.refExists(ctx, ns, "eni", eni); err != nil {
			return fmt.Errorf("acl_policy.eni_names[%d]=%q: %w", i, eni, err)
		}
	}
	return nil
}

// CheckRoutePolicy validates a RoutePolicySpec:
//   - spec-namespace consistency;
//   - every eni_names[i] MUST exist as an Eni in the same namespace;
//   - every routes[i].next_hop_target MUST exist as a Vnet in the
//     same namespace when next_hop_type == "vnet". Other next-hop
//     types (service_tunnel, direct, drop) are out of scope here:
//     service_tunnel targets are checked by PB schema-gating; direct
//     and drop have no target.
func (v *Validator) CheckRoutePolicy(ctx context.Context, ns string, spec *dashcenterv1.RoutePolicySpec) error {
	if spec == nil {
		return nil
	}
	if err := v.CheckSpecNamespace(ns, spec.GetNamespace()); err != nil {
		return err
	}
	for i, eni := range spec.GetEniNames() {
		if eni == "" {
			continue
		}
		if err := v.refExists(ctx, ns, "eni", eni); err != nil {
			return fmt.Errorf("route_policy.eni_names[%d]=%q: %w", i, eni, err)
		}
	}
	for i, r := range spec.GetRoutes() {
		if r == nil {
			continue
		}
		if r.GetNextHopType() == "vnet" && r.GetNextHopTarget() != "" {
			if err := v.refExists(ctx, ns, "vnet", r.GetNextHopTarget()); err != nil {
				return fmt.Errorf("route_policy.routes[%d].next_hop_target=%q: %w",
					i, r.GetNextHopTarget(), err)
			}
		}
		if r.GetNextHopType() == "service_tunnel" && r.GetNextHopTarget() != "" {
			if err := v.refExists(ctx, ns, "service_tunnel", r.GetNextHopTarget()); err != nil {
				return fmt.Errorf("route_policy.routes[%d].next_hop_target=%q (type=service_tunnel): %w",
					i, r.GetNextHopTarget(), err)
			}
		}
	}
	return nil
}

// CheckHaSet validates an HaSetSpec:
//   - spec-namespace consistency;
//   - every member_dpu_ids[i] MUST exist in the DPU inventory.
func (v *Validator) CheckHaSet(_ context.Context, ns string, spec *dashcenterv1.HaSetSpec) error {
	if spec == nil {
		return nil
	}
	if err := v.CheckSpecNamespace(ns, spec.GetNamespace()); err != nil {
		return err
	}
	if v.inv != nil {
		for i, dpuID := range spec.GetMemberDpuIds() {
			if dpuID == "" {
				continue
			}
			if _, err := v.inv.Get(dpuID); err != nil {
				return fmt.Errorf("%w: ha_set.member_dpu_ids[%d]=%q not found in inventory",
					ErrDanglingReference, i, dpuID)
			}
		}
	}
	return nil
}

// CheckServiceTunnel validates a ServiceTunnelSpec. Like HA sets,
// service tunnels reference physical/topology values rather than
// tenant-scoped objects.
func (v *Validator) CheckServiceTunnel(_ context.Context, ns string, spec *dashcenterv1.ServiceTunnelSpec) error {
	if spec == nil {
		return nil
	}
	return v.CheckSpecNamespace(ns, spec.GetNamespace())
}

// refExists returns nil when (ns, kind, name) names an existing object,
// or ErrCrossNamespace otherwise. We treat "not found in this
// namespace" identically to "found in a different namespace" because
// the caller's perspective is the same: the reference is unreachable.
func (v *Validator) refExists(ctx context.Context, ns, kind, name string) error {
	_, err := v.store.Get(ctx, store.ObjectKey{Namespace: ns, Kind: kind, Name: name})
	if err == nil {
		return nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w (referenced %s/%s/%s not found in this namespace)",
			ErrCrossNamespace, ns, kind, name)
	}
	// Some other store error — propagate as-is so the caller can
	// distinguish "validator broke" from "validator rejected".
	return fmt.Errorf("namespace: store lookup failed: %w", err)
}

// CheckDelete validates that no other object references the target
// before deletion. Returns ErrHasDependents if dependents exist.
// force=true bypasses the check (emergency operations).
func (v *Validator) CheckDelete(ctx context.Context, ns, kind, name string, force bool) error {
	if force {
		return nil
	}
	switch kind {
	case "vnet":
		return v.checkVnetDependents(ctx, ns, name)
	case "eni":
		return v.checkEniDependents(ctx, ns, name)
	case "service_tunnel":
		return v.checkServiceTunnelDependents(ctx, ns, name)
	}
	return nil
}

// checkVnetDependents scans for ENIs and VnetMappings that reference
// the given vnet.
func (v *Validator) checkVnetDependents(ctx context.Context, ns, vnetName string) error {
	// Check ENIs referencing this vnet
	enis, err := v.store.List(ctx, ns, "eni")
	if err != nil {
		return nil // store error — don't block deletion
	}
	for _, sp := range enis {
		var spec dashcenterv1.EniSpec
		if json.Unmarshal(sp.Data, &spec) == nil && spec.GetVnetName() == vnetName {
			return fmt.Errorf("%w: cannot delete vnet %q — eni %q still references it",
				ErrHasDependents, vnetName, sp.Key.Name)
		}
	}
	// Check VnetMappings referencing this vnet
	mappings, err := v.store.List(ctx, ns, "vnet_mapping")
	if err != nil {
		return nil
	}
	for _, sp := range mappings {
		var spec dashcenterv1.VnetMappingSpec
		if json.Unmarshal(sp.Data, &spec) == nil && spec.GetVnetName() == vnetName {
			return fmt.Errorf("%w: cannot delete vnet %q — vnet_mapping %q still references it",
				ErrHasDependents, vnetName, sp.Key.Name)
		}
	}
	return nil
}

// checkEniDependents scans for AclPolicies and RoutePolicies that
// reference the given ENI.
func (v *Validator) checkEniDependents(ctx context.Context, ns, eniName string) error {
	acls, err := v.store.List(ctx, ns, "acl_policy")
	if err != nil {
		return nil
	}
	for _, sp := range acls {
		var spec dashcenterv1.AclPolicySpec
		if json.Unmarshal(sp.Data, &spec) == nil {
			for _, en := range spec.GetEniNames() {
				if en == eniName {
					return fmt.Errorf("%w: cannot delete eni %q — acl_policy %q still references it",
						ErrHasDependents, eniName, sp.Key.Name)
				}
			}
		}
	}
	routes, err := v.store.List(ctx, ns, "route_policy")
	if err != nil {
		return nil
	}
	for _, sp := range routes {
		var spec dashcenterv1.RoutePolicySpec
		if json.Unmarshal(sp.Data, &spec) == nil {
			for _, en := range spec.GetEniNames() {
				if en == eniName {
					return fmt.Errorf("%w: cannot delete eni %q — route_policy %q still references it",
						ErrHasDependents, eniName, sp.Key.Name)
				}
			}
		}
	}
	return nil
}

// checkServiceTunnelDependents scans for RoutePolicies that reference
// the given service tunnel.
func (v *Validator) checkServiceTunnelDependents(ctx context.Context, ns, tunnelName string) error {
	routes, err := v.store.List(ctx, ns, "route_policy")
	if err != nil {
		return nil
	}
	for _, sp := range routes {
		var spec dashcenterv1.RoutePolicySpec
		if json.Unmarshal(sp.Data, &spec) == nil {
			for _, r := range spec.GetRoutes() {
				if r != nil && r.GetNextHopType() == "service_tunnel" && r.GetNextHopTarget() == tunnelName {
					return fmt.Errorf("%w: cannot delete service_tunnel %q — route_policy %q still references it",
						ErrHasDependents, tunnelName, sp.Key.Name)
				}
			}
		}
	}
	return nil
}
