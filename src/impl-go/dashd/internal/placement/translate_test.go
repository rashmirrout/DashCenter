package placement

import (
"testing"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
"google.golang.org/protobuf/proto"
)

// 1. TranslateVnet all fields appear
func TestTranslateVnet(t *testing.T) {
obj, err := TranslateVnet("v1", &dashcenterv1.VnetSpec{Vni: 100})
if err != nil {
t.Fatalf("TranslateVnet: %v", err)
}
if obj.GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_VNET {
t.Errorf("expected VNET kind, got %v", obj.GetKind())
}
if obj.Key[0] != "v1" {
t.Errorf("expected key [v1], got %v", obj.Key)
}
}

// 2. TranslateVnet missing name → error
func TestTranslateVnetMissingName(t *testing.T) {
_, err := TranslateVnet("", &dashcenterv1.VnetSpec{Vni: 100})
if err == nil {
t.Error("expected error for empty name")
}
}

// 3. TranslateEni no RouteGroupRefs → 1 object
func TestTranslateEniSingle(t *testing.T) {
objs, err := TranslateEni("e1", &dashcenterv1.EniSpec{MacAddress: "aa:bb:cc:dd:ee:ff"})
if err != nil {
t.Fatalf("TranslateEni: %v", err)
}
if len(objs) != 1 {
t.Errorf("expected 1 object, got %d", len(objs))
}
if objs[0].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_ENI {
t.Errorf("expected ENI kind, got %v", objs[0].GetKind())
}
}

// 4. TranslateEni missing MAC → error
func TestTranslateEniMissingMac(t *testing.T) {
_, err := TranslateEni("e1", &dashcenterv1.EniSpec{})
if err == nil {
t.Error("expected error for missing mac_address")
}
}

// 5. TranslateVnetMapping key = [Vnet, IpAddress]
func TestTranslateVnetMapping(t *testing.T) {
obj, err := TranslateVnetMapping("vm1", &dashcenterv1.VnetMappingSpec{
VnetName: "v1", IpAddress: "10.0.0.1", MacAddress: "aa:bb:cc:dd:ee:ff",
})
if err != nil {
t.Fatalf("TranslateVnetMapping: %v", err)
}
if len(obj.Key) != 2 || obj.Key[0] != "v1" || obj.Key[1] != "10.0.0.1" {
t.Errorf("expected key [v1, 10.0.0.1], got %v", obj.Key)
}
}

// 6. TranslateAclPolicy N rules → 1+N objects
func TestTranslateAclPolicy(t *testing.T) {
objs, err := TranslateAclPolicy("acl1", &dashcenterv1.AclPolicySpec{
Name: "acl1",
Rules: []*dashcenterv1.AclRuleSpec{
{Priority: 100, Action: "allow"},
{Priority: 200, Action: "deny"},
},
})
if err != nil {
t.Fatalf("TranslateAclPolicy: %v", err)
}
if len(objs) != 3 { // 1 group + 2 rules
t.Errorf("expected 3 objects, got %d", len(objs))
}
if objs[0].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_ACL_GROUP {
t.Errorf("first should be AclGroup, got %v", objs[0].GetKind())
}
}

// 7. TranslateRoutePolicy emits routes
func TestTranslateRoutePolicy(t *testing.T) {
objs, err := TranslateRoutePolicy("rp1", &dashcenterv1.RoutePolicySpec{
Name: "rp1",
Routes: []*dashcenterv1.RouteSpec{
{Prefix: "10.0.0.0/8"},
{Prefix: "172.16.0.0/12"},
},
})
if err != nil {
t.Fatalf("TranslateRoutePolicy: %v", err)
}
if len(objs) != 3 { // 1 group + 2 routes
t.Errorf("expected 3 objects, got %d", len(objs))
}
}

// 8. TranslateHaSet emits HaSet + HaSetConfig
func TestTranslateHaSet(t *testing.T) {
objs, err := TranslateHaSet("ha1", &dashcenterv1.HaSetSpec{Name: "ha1"})
if err != nil {
t.Fatalf("TranslateHaSet: %v", err)
}
if len(objs) != 2 {
t.Fatalf("expected 2 objects, got %d", len(objs))
}
if objs[0].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_HA_SET {
t.Errorf("first should be HaSet, got %v", objs[0].GetKind())
}
if objs[1].GetKind() != dashapiv1.ObjectKind_OBJECT_KIND_HA_SET_CONFIG {
t.Errorf("second should be HaSetConfig, got %v", objs[1].GetKind())
}
}

// 9. Round-trip: Translate → PayloadOf → proto.Equal
func TestTranslateVnetRoundTrip(t *testing.T) {
obj, _ := TranslateVnet("v1", &dashcenterv1.VnetSpec{Vni: 42})
payload, err := kinds.PayloadOf(obj)
if err != nil {
t.Fatalf("PayloadOf: %v", err)
}

// Create a second object with same spec.
obj2, _ := TranslateVnet("v1", &dashcenterv1.VnetSpec{Vni: 42})
payload2, _ := kinds.PayloadOf(obj2)

if !proto.Equal(payload, payload2) {
t.Error("round-trip payloads not equal")
}
}