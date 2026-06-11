// Package schema implements PB-3 capability + schema gating for dashd.
//
// PB-3's job: reject Put* operations whose spec kind or features
// require DPU capabilities that the target DPU(s) have NOT advertised
// in their DpuCapabilities. PB-1 covered "fits the box" (capacity);
// PB-3 covers "the box knows how to do this" (capability).
//
// Locked decisions:
//
//   * Capabilities are advertised by the DPU at RegisterDpu time and
//     stored on the inventory entry as `Capabilities *DpuCapabilities`.
//     nil capabilities == "not yet known" → allow with a log warning
//     (MC-3 forward-compat contract). This matches PB-1's nil-Limits
//     contract so a half-bootstrapped fleet doesn't silently reject
//     every Put.
//
//   * Kind-level gates today: `service_tunnel` → caps.ServiceTunnel.
//     Spec-level gates today: ENI with IPv6 underlay → caps.IPv6.
//     Future-proofed via a small `requirements` table — add a row to
//     extend without touching call sites.
//
//   * Placement model mirrors capacity:
//
//       - ENIs check the DPUs in spec.placement_hint_dpu_ids[]. Empty
//         hint fans out to every registered DPU (fail-conservative).
//
//       - ServiceTunnel is fleet-wide today (no placement field), so
//         the gate must hold on EVERY registered DPU. PC will tighten
//         this once the placement engine records per-spec DPU targets.
//
// Out of scope for PB-3:
//
//   * Schema-version negotiation across multi-version fleets — `caps.
//     DashApiSchemaVersion` is read but only compared by exact match;
//     proper semver constraint matching lands with PD.
//   * Capability-aware placement (steer ENI away from non-capable
//     DPUs). PC's placement engine consumes this gate to decide.
package schema

import (
	"errors"
	"fmt"
	"strings"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// ErrFailedPrecondition is the sentinel returned when a spec kind or
// feature is rejected by the gate. Callers (service layer) wrap it
// with their own service-level sentinel that maps to HTTP 412 /
// codes.FailedPrecondition at the wire.
var ErrFailedPrecondition = errors.New("schema: failed precondition")

// Inventory is the subset of *inventory.Inventory the gate needs. Same
// shape as capacity.Inventory so tests can supply a single fake.
type Inventory interface {
	Get(id string) (inventory.DpuEntry, error)
	List() []inventory.DpuEntry
}

// Gate is a thread-safe capability/schema admission checker. Construct
// with NewGate; pass to service.NewControlPlane.
type Gate struct {
	inv Inventory
}

// NewGate returns a Gate wired to the given inventory. The gate holds
// no internal state of its own — every check is a fresh read against
// the inventory's current snapshot.
func NewGate(inv Inventory) *Gate {
	return &Gate{inv: inv}
}

// CheckKind admits a kind-level write. PB-3 enforces:
//
//   - "service_tunnel" requires caps.ServiceTunnel == true on every
//     DPU in the target set.
//   - All other kinds always pass (capacity already covers them).
//
// If targets is empty, the kind must be supported by every registered
// DPU (the fleet-wide fallback). nil caps on any target is treated as
// "not yet advertised" and allowed through (matches PB-1 MC-3).
func (g *Gate) CheckKind(targets []string, kind string) error {
	if g == nil {
		return nil
	}
	dpus := g.resolveTargets(targets)
	for _, d := range dpus {
		// nil caps → DPU hasn't advertised yet; we don't know whether
		// it supports this kind. Allow through (matches the nil-Limits
		// contract in capacity/PB-1).
		if d.Capabilities == nil {
			continue
		}
		switch kind {
		case "service_tunnel":
			if !d.Capabilities.ServiceTunnel {
				return fmt.Errorf("%w: dpu=%s kind=service_tunnel reason=caps.service_tunnel=false",
					ErrFailedPrecondition, d.ID)
			}
		case "ha_set":
			// HA requires at least one of the two HA modes.
			if !d.Capabilities.HaActiveActive && !d.Capabilities.HaActiveStandby {
				return fmt.Errorf("%w: dpu=%s kind=ha_set reason=caps.ha_active_active=false caps.ha_active_standby=false",
					ErrFailedPrecondition, d.ID)
			}
		}
	}
	return nil
}

// CheckSpec admits a spec-level write. PB-3 enforces:
//
//   - ENI with an IPv6 underlay address requires caps.IPv6 == true on
//     every placement target.
//   - VnetMapping with an IPv6 underlay address requires caps.IPv6.
//   - RoutePolicy with an IPv6 prefix in any route requires caps.IPv6
//     on every host-DPU.
//
// Unsupported kinds (no spec-level requirements yet) are a no-op.
func (g *Gate) CheckSpec(targets []string, kind string, spec any) error {
	if g == nil {
		return nil
	}
	switch kind {
	case "eni":
		s, ok := spec.(*dashcenterv1.EniSpec)
		if !ok || s == nil {
			return nil
		}
		if !isIPv6Address(s.GetUnderlayIp()) {
			return nil
		}
		return g.requireIPv6(targets, "eni", s.GetName(), "underlay_ip")
	case "vnet_mapping":
		s, ok := spec.(*dashcenterv1.VnetMappingSpec)
		if !ok || s == nil {
			return nil
		}
		if !isIPv6Address(s.GetUnderlayIp()) && !isIPv6Address(s.GetIpAddress()) {
			return nil
		}
		return g.requireIPv6(targets, "vnet_mapping", s.GetVnetName(), "underlay_ip|ip_address")
	case "route_policy":
		s, ok := spec.(*dashcenterv1.RoutePolicySpec)
		if !ok || s == nil {
			return nil
		}
		for _, r := range s.GetRoutes() {
			if isIPv6Prefix(r.GetPrefix()) {
				return g.requireIPv6(targets, "route_policy", s.GetName(), "prefix="+r.GetPrefix())
			}
		}
	case "service_tunnel":
		s, ok := spec.(*dashcenterv1.ServiceTunnelSpec)
		if !ok || s == nil {
			return nil
		}
		if isIPv6Address(s.GetLocalUnderlayIp()) || isIPv6Address(s.GetRemoteUnderlayIp()) {
			return g.requireIPv6(targets, "service_tunnel", s.GetName(), "underlay_ip")
		}
	}
	return nil
}

// SchemaVersionFor returns the DPU's advertised dash_api_schema_version,
// or "" when not yet known. Phase 1 only surfaces this for diagnostics;
// PD adds proper constraint matching.
func (g *Gate) SchemaVersionFor(dpuID string) string {
	if g == nil {
		return ""
	}
	entry, err := g.inv.Get(dpuID)
	if err != nil || entry.Capabilities == nil {
		return ""
	}
	return entry.Capabilities.DashApiSchemaVersion
}

// --- internal helpers -------------------------------------------------

func (g *Gate) requireIPv6(targets []string, kind, name, fieldHint string) error {
	for _, d := range g.resolveTargets(targets) {
		if d.Capabilities == nil {
			continue
		}
		if !d.Capabilities.Ipv6 {
			return fmt.Errorf("%w: dpu=%s kind=%s name=%s reason=ipv6 required (%s) but caps.ipv6=false",
				ErrFailedPrecondition, d.ID, kind, name, fieldHint)
		}
	}
	return nil
}

// resolveTargets returns the inventory entries for the given DPU ids,
// or every registered DPU when targets is empty. Unknown ids are
// silently dropped — capacity already fails-closed on unknown
// placement hints, so the gate doesn't need to duplicate that check.
func (g *Gate) resolveTargets(targets []string) []inventory.DpuEntry {
	if len(targets) == 0 {
		return g.inv.List()
	}
	out := make([]inventory.DpuEntry, 0, len(targets))
	for _, id := range targets {
		entry, err := g.inv.Get(id)
		if err == nil {
			out = append(out, entry)
		}
	}
	return out
}

// isIPv6Address returns true for an address literal that contains a
// colon (the v6 separator). Conservatively false for empty input —
// dashd never asks the gate whether "" is v6.
//
// We intentionally avoid net/netip.ParseAddr because the gate must run
// on partially-validated specs (caller validation happens later in
// dispatch). This heuristic is wire-protocol-correct: every v4 literal
// contains only dots, every v6 literal contains at least one colon.
func isIPv6Address(s string) bool {
	if s == "" {
		return false
	}
	return strings.Contains(s, ":")
}

// isIPv6Prefix returns true for a CIDR whose host part is an IPv6
// literal (i.e., contains a colon before the '/').
func isIPv6Prefix(s string) bool {
	if s == "" {
		return false
	}
	if i := strings.Index(s, "/"); i >= 0 {
		return isIPv6Address(s[:i])
	}
	return isIPv6Address(s)
}
