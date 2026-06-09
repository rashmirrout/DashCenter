package placement

import (
"testing"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

func setupInv(ids ...string) *inventory.Inventory {
inv := inventory.New()
for _, id := range ids {
inv.Register(inventory.DpuEntry{ID: id, Endpoint: "localhost:50051"})
}
return inv
}

func emptySpecs() *DesiredSpecs {
return &DesiredSpecs{
Vnets:         make(map[string]*dashcenterv1.VnetSpec),
Enis:          make(map[string]*dashcenterv1.EniSpec),
VnetMappings:  make(map[string]*dashcenterv1.VnetMappingSpec),
AclPolicies:   make(map[string]*dashcenterv1.AclPolicySpec),
RoutePolicies: make(map[string]*dashcenterv1.RoutePolicySpec),
HaSets:        make(map[string]*dashcenterv1.HaSetSpec),
}
}

// 1. Empty specs → empty result
func TestResolveEmptySpecs(t *testing.T) {
inv := setupInv("dpu-0")
objs := Resolve("dpu-0", emptySpecs(), inv)
if len(objs) != 0 {
t.Errorf("expected 0 objects, got %d", len(objs))
}
}

// 2. Single ENI on dpu-0: Resolve("dpu-0") = [Vnet, Eni]; Resolve("dpu-1") = []
func TestResolveSingleEni(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1")
specs := emptySpecs()
specs.Vnets["vnet-1"] = &dashcenterv1.VnetSpec{Name: "vnet-1", Vni: 100}
specs.Enis["eni-1"] = &dashcenterv1.EniSpec{
Name:                "eni-1",
VnetName:            "vnet-1",
MacAddress:          "00:11:22:33:44:55",
PlacementHintDpuIds: []string{"dpu-0"},
}

objs0 := Resolve("dpu-0", specs, inv)
objs1 := Resolve("dpu-1", specs, inv)

if len(objs0) < 2 {
t.Errorf("dpu-0 should have ≥2 objects (Vnet+Eni), got %d", len(objs0))
}
if len(objs1) != 0 {
t.Errorf("dpu-1 should have 0 objects, got %d", len(objs1))
}

// Verify object kinds.
hasVnet, hasEni := false, false
for _, o := range objs0 {
if o.GetKind() == dashapiv1.ObjectKind_OBJECT_KIND_VNET {
hasVnet = true
}
if o.GetKind() == dashapiv1.ObjectKind_OBJECT_KIND_ENI {
hasEni = true
}
}
if !hasVnet || !hasEni {
t.Errorf("expected both Vnet and Eni, got hasVnet=%v hasEni=%v", hasVnet, hasEni)
}
}

// 3. VnetMapping follows ENI placement
func TestResolveVnetMappingFollows(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1")
specs := emptySpecs()
specs.Vnets["vnet-1"] = &dashcenterv1.VnetSpec{Name: "vnet-1", Vni: 100}
specs.Enis["eni-1"] = &dashcenterv1.EniSpec{
Name: "eni-1", VnetName: "vnet-1", MacAddress: "aa:bb:cc:dd:ee:ff",
PlacementHintDpuIds: []string{"dpu-0"},
}
specs.VnetMappings["vm-1"] = &dashcenterv1.VnetMappingSpec{
VnetName: "vnet-1", IpAddress: "10.0.0.1", MacAddress: "aa:bb:cc:dd:ee:ff",
}

objs0 := Resolve("dpu-0", specs, inv)
objs1 := Resolve("dpu-1", specs, inv)

hasMapping := false
for _, o := range objs0 {
if o.GetKind() == dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING {
hasMapping = true
}
}
if !hasMapping {
t.Error("dpu-0 should have VnetMapping")
}
for _, o := range objs1 {
if o.GetKind() == dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING {
t.Error("dpu-1 should not have VnetMapping")
}
}
}

// 4. AclPolicy follows ENI
func TestResolveAclFollowsEni(t *testing.T) {
inv := setupInv("dpu-0")
specs := emptySpecs()
specs.Enis["eni-1"] = &dashcenterv1.EniSpec{
Name: "eni-1", VnetName: "v", MacAddress: "aa:bb:cc:dd:ee:ff",
PlacementHintDpuIds: []string{"dpu-0"},
}
specs.AclPolicies["acl-1"] = &dashcenterv1.AclPolicySpec{
Name: "acl-1", EniNames: []string{"eni-1"},
Rules: []*dashcenterv1.AclRuleSpec{{Priority: 100, Action: "allow"}},
}

objs := Resolve("dpu-0", specs, inv)
hasAclGroup := false
for _, o := range objs {
if o.GetKind() == dashapiv1.ObjectKind_OBJECT_KIND_ACL_GROUP {
hasAclGroup = true
}
}
if !hasAclGroup {
t.Error("expected AclGroup on dpu-0")
}
}

// 5. HaSet spans MemberDpuIds
func TestResolveHaSet(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1", "dpu-2")
specs := emptySpecs()
specs.HaSets["ha-1"] = &dashcenterv1.HaSetSpec{
Name: "ha-1", MemberDpuIds: []string{"dpu-0", "dpu-1"},
}

objs0 := Resolve("dpu-0", specs, inv)
objs1 := Resolve("dpu-1", specs, inv)
objs2 := Resolve("dpu-2", specs, inv)

if len(objs0) != 2 { // HaSet + HaSetConfig
t.Errorf("dpu-0 expected 2 HA objects, got %d", len(objs0))
}
if len(objs1) != 2 {
t.Errorf("dpu-1 expected 2 HA objects, got %d", len(objs1))
}
if len(objs2) != 0 {
t.Errorf("dpu-2 expected 0 objects, got %d", len(objs2))
}
}

// 6. AffectedDpus for ENI → single DPU
func TestAffectedDpusEni(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1")
specs := emptySpecs()
specs.Enis["eni-1"] = &dashcenterv1.EniSpec{
Name: "eni-1", PlacementHintDpuIds: []string{"dpu-0"},
}

affected := AffectedDpus("eni", "eni-1", specs, inv)
if len(affected) != 1 || affected[0] != "dpu-0" {
t.Errorf("expected [dpu-0], got %v", affected)
}
}

// 7. AffectedDpus for VNET → DPUs hosting ENIs in that VNET
func TestAffectedDpusVnet(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1")
specs := emptySpecs()
specs.Enis["eni-1"] = &dashcenterv1.EniSpec{
Name: "eni-1", VnetName: "vnet-1", PlacementHintDpuIds: []string{"dpu-0"},
}

affected := AffectedDpus("vnet", "vnet-1", specs, inv)
if len(affected) != 1 || affected[0] != "dpu-0" {
t.Errorf("expected [dpu-0], got %v", affected)
}
}

// 8. AffectedDpus for unknown kind → all DPUs
func TestAffectedDpusUnknownKind(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1")
specs := emptySpecs()

affected := AffectedDpus("unknown_kind", "x", specs, inv)
if len(affected) != 2 {
t.Errorf("expected 2 (all DPUs), got %d", len(affected))
}
}

// 9. Nil specs → Resolve returns nil
func TestResolveNilSpecs(t *testing.T) {
inv := setupInv("dpu-0")
objs := Resolve("dpu-0", nil, inv)
if objs != nil {
t.Errorf("expected nil, got %v", objs)
}
}

// 10. ResolveAll returns map for all DPUs
func TestResolveAll(t *testing.T) {
inv := setupInv("dpu-0", "dpu-1")
specs := emptySpecs()
specs.Vnets["v1"] = &dashcenterv1.VnetSpec{Name: "v1", Vni: 100}
specs.Enis["e1"] = &dashcenterv1.EniSpec{
Name: "e1", VnetName: "v1", MacAddress: "aa:bb:cc:dd:ee:ff",
PlacementHintDpuIds: []string{"dpu-0"},
}

all := ResolveAll(specs, inv)
if len(all) != 2 {
t.Errorf("expected 2 DPUs in result, got %d", len(all))
}
if len(all["dpu-0"]) < 2 {
t.Errorf("dpu-0 should have objects, got %d", len(all["dpu-0"]))
}
if len(all["dpu-1"]) != 0 {
t.Errorf("dpu-1 should have 0 objects, got %d", len(all["dpu-1"]))
}
}