package model

import (
"sync"
"testing"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
)

func vnetObj(name string, vni uint32) *dashapiv1.Object {
obj, _ := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{name}, &dash_vnet.Vnet{Vni: vni})
return obj
}

func eniObj(name string) *dashapiv1.Object {
// Use a minimal ENI object.
obj := &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_ENI,
Key:  []string{name},
}
return obj
}

// 1. Set then GetDpu returns object
func TestSetThenGet(t *testing.T) {
c := NewObsCache()
obj := vnetObj("v1", 100)
c.Set("dpu-0", obj)
m := c.GetDpu("dpu-0")
if len(m) != 1 {
t.Errorf("expected 1 entry, got %d", len(m))
}
}

// 2. Set overwrites — second Set for same key wins
func TestSetOverwrites(t *testing.T) {
c := NewObsCache()
c.Set("dpu-0", vnetObj("v1", 100))
c.Set("dpu-0", vnetObj("v1", 200))
m := c.GetDpu("dpu-0")
if len(m) != 1 {
t.Errorf("expected 1 entry after overwrite, got %d", len(m))
}
}

// 3. Delete removes
func TestDelete(t *testing.T) {
c := NewObsCache()
c.Set("dpu-0", vnetObj("v1", 100))
c.Delete("dpu-0", dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v1"})
m := c.GetDpu("dpu-0")
if len(m) != 0 {
t.Errorf("expected 0 entries after delete, got %d", len(m))
}
}

// 4. Delete unknown key — no panic
func TestDeleteUnknown(t *testing.T) {
c := NewObsCache()
// Should not panic.
c.Delete("dpu-0", dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"nope"})
}

// 5. ClearDpu empties cache
func TestClearDpu(t *testing.T) {
c := NewObsCache()
c.Set("dpu-0", vnetObj("v1", 100))
c.Set("dpu-0", vnetObj("v2", 200))
c.ClearDpu("dpu-0")
m := c.GetDpu("dpu-0")
if len(m) != 0 {
t.Errorf("expected 0 entries after ClearDpu, got %d", len(m))
}
}

// 6. GetDpu returns defensive copy
func TestGetDpuDefensiveCopy(t *testing.T) {
c := NewObsCache()
c.Set("dpu-0", vnetObj("v1", 100))
m := c.GetDpu("dpu-0")
// Mutate returned map.
for k := range m {
delete(m, k)
}
// Original should be unaffected.
m2 := c.GetDpu("dpu-0")
if len(m2) != 1 {
t.Errorf("GetDpu mutation affected cache; expected 1, got %d", len(m2))
}
}

// 7. Diff(empty desired, empty observed) → empty
func TestDiffBothEmpty(t *testing.T) {
c := NewObsCache()
d := c.Diff("dpu-0", nil)
if !d.IsEmpty() {
t.Errorf("expected empty diff, got total=%d", d.Total())
}
}

// 8. Diff(desired=3, observed=0) → Add has 3
func TestDiffAllAdd(t *testing.T) {
c := NewObsCache()
desired := []*dashapiv1.Object{
vnetObj("v1", 100),
vnetObj("v2", 200),
vnetObj("v3", 300),
}
d := c.Diff("dpu-0", desired)
if len(d.Add) != 3 {
t.Errorf("expected 3 adds, got %d", len(d.Add))
}
if len(d.Update) != 0 || len(d.Remove) != 0 {
t.Errorf("expected no updates/removes")
}
}

// 9. Diff(desired=0, observed=3) → Remove has 3
func TestDiffAllRemove(t *testing.T) {
c := NewObsCache()
c.Set("dpu-0", vnetObj("v1", 100))
c.Set("dpu-0", vnetObj("v2", 200))
c.Set("dpu-0", vnetObj("v3", 300))
d := c.Diff("dpu-0", nil)
if len(d.Remove) != 3 {
t.Errorf("expected 3 removes, got %d", len(d.Remove))
}
}

// 10. Diff same payload → IsEmpty
func TestDiffSamePayload(t *testing.T) {
c := NewObsCache()
obj := vnetObj("v1", 100)
c.Set("dpu-0", obj)
d := c.Diff("dpu-0", []*dashapiv1.Object{vnetObj("v1", 100)})
if !d.IsEmpty() {
t.Errorf("expected empty diff for same payload, got total=%d", d.Total())
}
}

// 11. Diff different payload → Update
func TestDiffDifferentPayload(t *testing.T) {
c := NewObsCache()
c.Set("dpu-0", vnetObj("v1", 100))
d := c.Diff("dpu-0", []*dashapiv1.Object{vnetObj("v1", 999)})
if len(d.Update) != 1 {
t.Errorf("expected 1 update, got %d", len(d.Update))
}
}

// 12. Diff stable order
func TestDiffStableOrder(t *testing.T) {
c := NewObsCache()
desired := []*dashapiv1.Object{
vnetObj("z", 1),
vnetObj("a", 2),
vnetObj("m", 3),
}
d1 := c.Diff("dpu-0", desired)
d2 := c.Diff("dpu-0", desired)
if len(d1.Add) != len(d2.Add) {
t.Fatal("diff lengths differ")
}
for i := range d1.Add {
k1 := innerKey(d1.Add[i].GetKind(), d1.Add[i].GetKey())
k2 := innerKey(d2.Add[i].GetKind(), d2.Add[i].GetKey())
if k1 != k2 {
t.Errorf("diff not stable at index %d: %s != %s", i, k1, k2)
}
}
}

// 13. Concurrent Set/Get/Delete
func TestConcurrentOps(t *testing.T) {
c := NewObsCache()
var wg sync.WaitGroup
for i := 0; i < 20; i++ {
wg.Add(1)
go func(n int) {
defer wg.Done()
for j := 0; j < 100; j++ {
obj := vnetObj("v1", uint32(n*100+j))
c.Set("dpu-0", obj)
c.GetDpu("dpu-0")
c.Delete("dpu-0", dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{"v1"})
}
}(i)
}
wg.Wait()
// No panic = success.
}

// 14. KeyOf deep-copies key
func TestKeyOfDeepCopy(t *testing.T) {
obj := &dashapiv1.Object{
Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET,
Key:  []string{"original"},
}
k := KeyOf("dpu-0", obj)
k.Key[0] = "mutated"
if obj.Key[0] != "original" {
t.Error("KeyOf did not deep-copy key — source was mutated")
}
}