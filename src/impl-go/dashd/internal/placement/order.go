// Package placement implements the pure placement function that translates
// dashcenter.v1 fleet specs into per-DPU dashapi.v1 Objects, plus dependency
// ordering for Apply/Delete.
package placement

import (
"sort"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
)

// Tier returns the dependency tier (1-5) for the given ObjectKind.
// Lower tiers are applied first, deleted last. Unknown kinds → 99.
func Tier(kind dashapiv1.ObjectKind) int {
switch kind {
// Tier 1: infrastructure objects
case dashapiv1.ObjectKind_OBJECT_KIND_APPLIANCE,
dashapiv1.ObjectKind_OBJECT_KIND_VNET,
dashapiv1.ObjectKind_OBJECT_KIND_ROUTE_TYPE,
dashapiv1.ObjectKind_OBJECT_KIND_PREFIX_TAG,
dashapiv1.ObjectKind_OBJECT_KIND_TUNNEL,
dashapiv1.ObjectKind_OBJECT_KIND_QOS,
dashapiv1.ObjectKind_OBJECT_KIND_METER_POLICY,
dashapiv1.ObjectKind_OBJECT_KIND_ROUTE_GROUP,
dashapiv1.ObjectKind_OBJECT_KIND_ACL_GROUP:
return 1

// Tier 2: rules/members that reference tier-1 groups
case dashapiv1.ObjectKind_OBJECT_KIND_METER_RULE,
dashapiv1.ObjectKind_OBJECT_KIND_ROUTE,
dashapiv1.ObjectKind_OBJECT_KIND_ACL_RULE,
dashapiv1.ObjectKind_OBJECT_KIND_ROUTING_APPLIANCE,
dashapiv1.ObjectKind_OBJECT_KIND_PA_VALIDATION:
return 2

// Tier 3: ENIs
case dashapiv1.ObjectKind_OBJECT_KIND_ENI,
dashapiv1.ObjectKind_OBJECT_KIND_ENI_ROUTE:
return 3

// Tier 4: per-ENI bindings
case dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING,
dashapiv1.ObjectKind_OBJECT_KIND_ACL_IN,
dashapiv1.ObjectKind_OBJECT_KIND_ACL_OUT,
dashapiv1.ObjectKind_OBJECT_KIND_ROUTE_RULE,
dashapiv1.ObjectKind_OBJECT_KIND_METER:
return 4

// Tier 5: HA and remaining
case dashapiv1.ObjectKind_OBJECT_KIND_HA_SET,
dashapiv1.ObjectKind_OBJECT_KIND_HA_SET_CONFIG,
dashapiv1.ObjectKind_OBJECT_KIND_HA_SET_STATE,
dashapiv1.ObjectKind_OBJECT_KIND_HA_SCOPE,
dashapiv1.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG,
dashapiv1.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE,
dashapiv1.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP,
dashapiv1.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE:
return 5

default:
return 99
}
}

// OrderForApply returns objects sorted by ascending tier (tier 1 first).
// The input slice is not mutated; a new slice is returned.
// sort.SliceStable keeps within-tier order stable.
func OrderForApply(objects []*dashapiv1.Object) []*dashapiv1.Object {
out := make([]*dashapiv1.Object, len(objects))
copy(out, objects)
sort.SliceStable(out, func(i, j int) bool {
return Tier(out[i].GetKind()) < Tier(out[j].GetKind())
})
return out
}

// OrderForDelete returns objects sorted by descending tier (tier 5 first).
// The input slice is not mutated; a new slice is returned.
func OrderForDelete(objects []*dashapiv1.Object) []*dashapiv1.Object {
out := make([]*dashapiv1.Object, len(objects))
copy(out, objects)
sort.SliceStable(out, func(i, j int) bool {
return Tier(out[i].GetKind()) > Tier(out[j].GetKind())
})
return out
}