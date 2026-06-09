package placement

import (
"testing"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// buildSpecs synthesises a DesiredSpecs with `dpuCount` DPUs and
// `enisPerDpu` ENIs per DPU. Every ENI is in one shared VNET, anchored
// to one VnetMapping. Useful for benchmarking placement at realistic
// scale (10 DPUs × 500 ENIs ≈ 5000 ENIs total).
func buildSpecs(dpuCount, enisPerDpu int) (*DesiredSpecs, *inventory.Inventory) {
inv := inventory.New()
specs := &DesiredSpecs{
Vnets:         map[string]*dashcenterv1.VnetSpec{},
Enis:          map[string]*dashcenterv1.EniSpec{},
VnetMappings:  map[string]*dashcenterv1.VnetMappingSpec{},
AclPolicies:   map[string]*dashcenterv1.AclPolicySpec{},
RoutePolicies: map[string]*dashcenterv1.RoutePolicySpec{},
HaSets:        map[string]*dashcenterv1.HaSetSpec{},
}

specs.Vnets["v1"] = &dashcenterv1.VnetSpec{Name: "v1", Vni: 1000}
specs.VnetMappings["vm1"] = &dashcenterv1.VnetMappingSpec{
VnetName: "v1", IpAddress: "10.0.0.99", MacAddress: "00:00:00:00:00:99",
}

for d := 0; d < dpuCount; d++ {
dpuID := "dpu-" + itoa(d)
_ = inv.Register(inventory.DpuEntry{ID: dpuID, Endpoint: "ep-" + itoa(d)})
for e := 0; e < enisPerDpu; e++ {
name := "eni-" + itoa(d) + "-" + itoa(e)
specs.Enis[name] = &dashcenterv1.EniSpec{
Name:                name,
VnetName:            "v1",
MacAddress:          "00:00:00:00:00:01",
UnderlayIp:          "10.0." + itoa(d) + "." + itoa(e&0xff),
AdminState:          "enabled",
PlacementHintDpuIds: []string{dpuID},
}
}
}
return specs, inv
}

// itoa avoids importing fmt in hot benchmark paths.
func itoa(i int) string {
if i == 0 {
return "0"
}
var buf [10]byte
n := 0
neg := i < 0
if neg {
i = -i
}
for i > 0 {
buf[n] = byte('0' + i%10)
n++
i /= 10
}
out := make([]byte, 0, n+1)
if neg {
out = append(out, '-')
}
for j := n - 1; j >= 0; j-- {
out = append(out, buf[j])
}
return string(out)
}

// BenchmarkResolve_Small targets the typical case: 1 DPU, 50 ENIs.
func BenchmarkResolve_Small(b *testing.B) {
specs, inv := buildSpecs(1, 50)
b.ResetTimer()
for i := 0; i < b.N; i++ {
_ = Resolve("dpu-0", specs, inv)
}
}

// BenchmarkResolve_Medium: 10 DPUs, 100 ENIs each = 1000 ENIs total,
// but Resolve runs for a single DPU at a time.
func BenchmarkResolve_Medium(b *testing.B) {
specs, inv := buildSpecs(10, 100)
b.ResetTimer()
for i := 0; i < b.N; i++ {
_ = Resolve("dpu-5", specs, inv)
}
}

// BenchmarkResolve_Large: 50 DPUs × 200 ENIs = 10k ENIs. Resolve
// scans the entire ENI map per call so we expect linear scaling here.
func BenchmarkResolve_Large(b *testing.B) {
specs, inv := buildSpecs(50, 200)
b.ResetTimer()
for i := 0; i < b.N; i++ {
_ = Resolve("dpu-25", specs, inv)
}
}

// BenchmarkResolveAll_Medium: ResolveAll fans out across all DPUs.
// Worst case is O(dpus × enis).
func BenchmarkResolveAll_Medium(b *testing.B) {
specs, inv := buildSpecs(10, 100)
b.ResetTimer()
for i := 0; i < b.N; i++ {
_ = ResolveAll(specs, inv)
}
}

// BenchmarkAffectedDpus_EniChange tests the dirty-tracking hot path.
func BenchmarkAffectedDpus_EniChange(b *testing.B) {
specs, inv := buildSpecs(10, 100)
b.ResetTimer()
for i := 0; i < b.N; i++ {
_ = AffectedDpus("eni", "eni-5-50", specs, inv)
}
}