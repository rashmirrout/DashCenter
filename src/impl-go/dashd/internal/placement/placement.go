package placement

import (
"log/slog"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// DesiredSpecs holds the complete set of desired specs loaded from the store.
type DesiredSpecs struct {
Vnets         map[string]*dashcenterv1.VnetSpec
Enis          map[string]*dashcenterv1.EniSpec
VnetMappings  map[string]*dashcenterv1.VnetMappingSpec
AclPolicies   map[string]*dashcenterv1.AclPolicySpec
RoutePolicies map[string]*dashcenterv1.RoutePolicySpec
HaSets        map[string]*dashcenterv1.HaSetSpec
}

// Resolve returns the complete set of dashapi.v1 Objects that should exist on
// dpuID given the desired specs and inventory.
// PURE: no I/O, no goroutines, no global state. Freshly allocated output.
func Resolve(dpuID string, specs *DesiredSpecs, inv *inventory.Inventory) []*dashapiv1.Object {
if specs == nil {
return nil
}

var out []*dashapiv1.Object

// 1. Find ENIs on this DPU → build vnetsOnDpu set.
vnetsOnDpu := make(map[string]struct{})
aclGroupsOnDpu := make(map[string]struct{})
routeGroupsOnDpu := make(map[string]struct{})

for name, eni := range specs.Enis {
if !eniOnDpu(eni, dpuID) {
continue
}
vnetsOnDpu[eni.GetVnetName()] = struct{}{}

objs, err := TranslateEni(name, eni)
if err != nil {
slog.Warn("placement: translate eni failed", "name", name, "error", err)
continue
}
out = append(out, objs...)

// Collect referenced ACL/route groups from this ENI.
// Phase 1: AclPolicySpec.EniNames and RoutePolicySpec.EniNames
// are checked below.
}

// Scan ACL/Route policies to find which reference ENIs on this DPU.
for _, acl := range specs.AclPolicies {
for _, eniName := range acl.GetEniNames() {
if eni, ok := specs.Enis[eniName]; ok && eniOnDpu(eni, dpuID) {
aclGroupsOnDpu[acl.GetName()] = struct{}{}
break
}
}
}
for _, rp := range specs.RoutePolicies {
for _, eniName := range rp.GetEniNames() {
if eni, ok := specs.Enis[eniName]; ok && eniOnDpu(eni, dpuID) {
routeGroupsOnDpu[rp.GetName()] = struct{}{}
break
}
}
}

// 2. VNETs that have ENIs on this DPU.
for name, vnet := range specs.Vnets {
if _, ok := vnetsOnDpu[name]; !ok {
continue
}
obj, err := TranslateVnet(name, vnet)
if err != nil {
slog.Warn("placement: translate vnet failed", "name", name, "error", err)
continue
}
out = append(out, obj)
}

// 3. VnetMappings for VNETs on this DPU.
for name, vm := range specs.VnetMappings {
if _, ok := vnetsOnDpu[vm.GetVnetName()]; !ok {
continue
}
obj, err := TranslateVnetMapping(name, vm)
if err != nil {
slog.Warn("placement: translate vnet_mapping failed", "name", name, "error", err)
continue
}
out = append(out, obj)
}

// 4. AclPolicies for groups referenced by ENIs on this DPU.
for name, acl := range specs.AclPolicies {
if _, ok := aclGroupsOnDpu[name]; !ok {
continue
}
objs, err := TranslateAclPolicy(name, acl)
if err != nil {
slog.Warn("placement: translate acl_policy failed", "name", name, "error", err)
continue
}
out = append(out, objs...)
}

// 5. RoutePolicies for groups referenced by ENIs on this DPU.
for name, rp := range specs.RoutePolicies {
if _, ok := routeGroupsOnDpu[name]; !ok {
continue
}
objs, err := TranslateRoutePolicy(name, rp)
if err != nil {
slog.Warn("placement: translate route_policy failed", "name", name, "error", err)
continue
}
out = append(out, objs...)
}

// 6. HaSets where this DPU is a member.
for name, hs := range specs.HaSets {
if !dpuInSlice(dpuID, hs.GetMemberDpuIds()) {
continue
}
objs, err := TranslateHaSet(name, hs)
if err != nil {
slog.Warn("placement: translate ha_set failed", "name", name, "error", err)
continue
}
out = append(out, objs...)
}

return out
}

// ResolveAll runs Resolve for every DPU in the inventory.
func ResolveAll(specs *DesiredSpecs, inv *inventory.Inventory) map[string][]*dashapiv1.Object {
result := make(map[string][]*dashapiv1.Object)
for _, e := range inv.List() {
objs := Resolve(e.ID, specs, inv)
result[e.ID] = objs
}
return result
}

// AffectedDpus returns DPU IDs whose Resolve output may have changed
// when the spec at (kind, name) changes.
// Unknown kind → conservative fallback: all DPUs.
func AffectedDpus(kind, name string, specs *DesiredSpecs, inv *inventory.Inventory) []string {
if specs == nil {
return allDpuIDs(inv)
}

switch kind {
case "eni":
eni, ok := specs.Enis[name]
if !ok {
return allDpuIDs(inv)
}
return dpusForEni(eni, inv)

case "vnet":
// All DPUs hosting an ENI in this VNET.
return dpusWithVnet(name, specs, inv)

case "vnet_mapping":
vm, ok := specs.VnetMappings[name]
if !ok {
return allDpuIDs(inv)
}
return dpusWithVnet(vm.GetVnetName(), specs, inv)

case "acl_policy":
acl, ok := specs.AclPolicies[name]
if !ok {
return allDpuIDs(inv)
}
return dpusForPolicy(acl.GetEniNames(), specs, inv)

case "route_policy":
rp, ok := specs.RoutePolicies[name]
if !ok {
return allDpuIDs(inv)
}
return dpusForPolicy(rp.GetEniNames(), specs, inv)

case "ha_set":
hs, ok := specs.HaSets[name]
if !ok {
return allDpuIDs(inv)
}
return hs.GetMemberDpuIds()

default:
return allDpuIDs(inv)
}
}

// --- helpers ---

func eniOnDpu(eni *dashcenterv1.EniSpec, dpuID string) bool {
hints := eni.GetPlacementHintDpuIds()
if len(hints) == 0 {
return false
}
for _, h := range hints {
if h == dpuID {
return true
}
}
return false
}

func dpuInSlice(dpuID string, ids []string) bool {
for _, id := range ids {
if id == dpuID {
return true
}
}
return false
}

func allDpuIDs(inv *inventory.Inventory) []string {
entries := inv.List()
ids := make([]string, len(entries))
for i, e := range entries {
ids[i] = e.ID
}
return ids
}

func dpusForEni(eni *dashcenterv1.EniSpec, inv *inventory.Inventory) []string {
hints := eni.GetPlacementHintDpuIds()
if len(hints) == 0 {
return allDpuIDs(inv)
}
return hints
}

func dpusWithVnet(vnetName string, specs *DesiredSpecs, inv *inventory.Inventory) []string {
seen := make(map[string]struct{})
for _, eni := range specs.Enis {
if eni.GetVnetName() == vnetName {
for _, h := range eni.GetPlacementHintDpuIds() {
seen[h] = struct{}{}
}
}
}
result := make([]string, 0, len(seen))
for id := range seen {
result = append(result, id)
}
if len(result) == 0 {
return allDpuIDs(inv)
}
return result
}

func dpusForPolicy(eniNames []string, specs *DesiredSpecs, inv *inventory.Inventory) []string {
seen := make(map[string]struct{})
for _, eniName := range eniNames {
eni, ok := specs.Enis[eniName]
if !ok {
continue
}
for _, h := range eni.GetPlacementHintDpuIds() {
seen[h] = struct{}{}
}
}
result := make([]string, 0, len(seen))
for id := range seen {
result = append(result, id)
}
if len(result) == 0 {
return allDpuIDs(inv)
}
return result
}