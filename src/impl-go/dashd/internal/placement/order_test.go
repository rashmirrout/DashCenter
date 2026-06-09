package placement

import (
"testing"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
)

// 1. Every registered ObjectKind has Tier in 1..5 (none == 99)
func TestAllKindsCovered(t *testing.T) {
for _, info := range kinds.All {
tier := Tier(info.Kind)
if tier == 99 {
t.Errorf("kind %s (%d) not covered by Tier()", info.Name, info.Kind)
}
if tier < 1 || tier > 5 {
t.Errorf("kind %s: tier %d out of range 1-5", info.Name, tier)
}
}
}

// 2. OrderForApply: [HaSet, Vnet, Eni] → [Vnet, Eni, HaSet]
func TestOrderForApply(t *testing.T) {
objs := []*dashapiv1.Object{
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_HA_SET, Key: []string{"h1"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_ENI, Key: []string{"e1"}},
}
ordered := OrderForApply(objs)
if ordered[0].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_VNET {
t.Errorf("expected Vnet first, got %v", ordered[0].GetKind())
}
if ordered[1].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_ENI {
t.Errorf("expected Eni second, got %v", ordered[1].GetKind())
}
if ordered[2].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_HA_SET {
t.Errorf("expected HaSet third, got %v", ordered[2].GetKind())
}
}

// 3. OrderForDelete is reverse
func TestOrderForDelete(t *testing.T) {
objs := []*dashapiv1.Object{
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_ENI, Key: []string{"e1"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_HA_SET, Key: []string{"h1"}},
}
ordered := OrderForDelete(objs)
if ordered[0].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_HA_SET {
t.Errorf("expected HaSet first in delete order, got %v", ordered[0].GetKind())
}
if ordered[2].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_VNET {
t.Errorf("expected Vnet last in delete order, got %v", ordered[2].GetKind())
}
}

// 4. Input slice unmutated after call
func TestOrderDoesNotMutateInput(t *testing.T) {
objs := []*dashapiv1.Object{
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_HA_SET, Key: []string{"h1"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"v1"}},
}
original0 := objs[0].GetKind()
_ = OrderForApply(objs)
if objs[0].GetKind() != original0 {
t.Error("OrderForApply mutated the input slice")
}
}

// 5. Stable within tier
func TestOrderStableWithinTier(t *testing.T) {
objs := []*dashapiv1.Object{
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"z"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"a"}},
{Kind: dashapiv1.ObjectKind_OBJECT_KIND_VNET, Key: []string{"m"}},
}
ordered := OrderForApply(objs)
// Same tier → original order preserved (SliceStable).
if ordered[0].Key[0] != "z" || ordered[1].Key[0] != "a" || ordered[2].Key[0] != "m" {
t.Errorf("within-tier order not stable: %v %v %v",
ordered[0].Key[0], ordered[1].Key[0], ordered[2].Key[0])
}
}