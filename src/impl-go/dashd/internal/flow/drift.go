// ExplainDrift returns a field-by-field narrative for one (NameRef,
// DPU) target, comparing declared (desired) state with observed
// (cached) state. Used by `dashctl drift explain` to answer "what
// changed and how should I respond?"
package flow

import (
	"context"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
)

// ExplainDrift returns the field diffs + a suggested remediation.
//
// PE-G1 scope: dashd already has DriftItem narrative at the spec
// level (the admin endpoint returns adds/updates/removes per DPU). For
// PE-1 we ship a structural explanation: presence/absence in declared
// state plus a one-of-3 remediation hint (RECONCILE / IMPORT_OBSERVED
// / MANUAL). Field-level diff payloads land in a follow-up once the
// obs cache exposes typed payload accessors (today it stores opaque
// dashapi.Object instances whose field shape differs from the
// dashcenter.v1 spec).
func (e *Engine) ExplainDrift(ctx context.Context, req *dashcenterv1.DriftExplainRequest) (*dashcenterv1.DriftExplanation, error) {
	if req == nil {
		return nil, invArgf("request is nil")
	}
	target := req.GetTarget()
	if target == nil {
		return nil, invArgf("target (NameRef) is required")
	}
	if target.GetName() == "" || target.GetKind() == "" {
		return nil, invArgf("target.name and target.kind are required")
	}
	if req.GetDpuId() == "" {
		return nil, invArgf("dpu_id is required")
	}

	specs, err := e.loadView(ctx)
	if err != nil {
		return nil, err
	}

	out := &dashcenterv1.DriftExplanation{
		Target:    target,
		DpuId:     req.GetDpuId(),
		Suggested: dashcenterv1.DriftExplanation_REMEDIATION_RECONCILE,
	}

	if !declaredExists(specs, target.GetKind(), target.GetName()) {
		out.Suggested = dashcenterv1.DriftExplanation_REMEDIATION_MANUAL
		out.FieldDiffs = append(out.FieldDiffs, &dashcenterv1.FieldDiff{
			Field:    "presence",
			Declared: "absent",
			Observed: "(query /admin/observed?dpu=<id> to see what the DPU still holds)",
		})
		out.Rationale = fmt.Sprintf(
			"declared %s/%s not found in desired state — DPU %q may still hold an observed copy; recommend MANUAL review (operator must decide whether to import-as-declared or delete on DPU)",
			target.GetKind(), target.GetName(), req.GetDpuId())
		return out, nil
	}

	out.FieldDiffs = append(out.FieldDiffs, &dashcenterv1.FieldDiff{
		Field:    "presence",
		Declared: "present",
		Observed: "(see /admin/drift for live add/update/remove vs DPU)",
	})
	out.Rationale = fmt.Sprintf(
		"%s/%s exists in declared state. To resolve drift, RECONCILE will push declared → DPU %q. Use IMPORT_OBSERVED only when the DPU is authoritative (rare; manual confirmation recommended).",
		target.GetKind(), target.GetName(), req.GetDpuId())
	return out, nil
}

// declaredExists reports whether the (kind, name) tuple has a
// corresponding entry in the loaded DesiredSpecs. Unknown kinds
// return false (consistent with placement: unknown kinds are silently
// ignored).
func declaredExists(specs *placement.DesiredSpecs, kind, name string) bool {
	if specs == nil {
		return false
	}
	switch kind {
	case "vnet", "Vnet":
		_, ok := specs.Vnets[name]
		return ok
	case "eni", "Eni":
		_, ok := specs.Enis[name]
		return ok
	case "vnet_mapping", "VnetMapping":
		_, ok := specs.VnetMappings[name]
		return ok
	case "acl_policy", "AclPolicy":
		_, ok := specs.AclPolicies[name]
		return ok
	case "route_policy", "RoutePolicy":
		_, ok := specs.RoutePolicies[name]
		return ok
	case "ha_set", "HaSet":
		_, ok := specs.HaSets[name]
		return ok
	}
	return false
}
