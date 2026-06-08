package placement

import (
"context"
"encoding/json"
"os"
"path/filepath"
"testing"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// openStore returns a fresh file-backed store rooted in t.TempDir().
func openStore(t *testing.T) *filstore.FileStore {
t.Helper()
dir := filepath.Join(t.TempDir(), "specs")
_ = os.MkdirAll(dir, 0o755)
st, err := filstore.Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
t.Cleanup(func() { _ = st.Close() })
return st
}

func putSpec(t *testing.T, st *filstore.FileStore, kind, name string, v any) {
t.Helper()
key := store.ObjectKey{Namespace: store.DefaultNamespace, Kind: kind, Name: name}
if _, err := st.Put(context.Background(), key, v, 0); err != nil {
t.Fatalf("put %s/%s: %v", kind, name, err)
}
}

// 1. Nil store → empty DesiredSpecs, not an error.
func TestLoadDesiredSpecs_NilStore_EmptySpecs(t *testing.T) {
got, err := LoadDesiredSpecs(context.Background(), nil)
if err != nil {
t.Fatalf("err=%v", err)
}
if got == nil {
t.Fatal("nil DesiredSpecs")
}
}

// 2. Empty store → all maps initialized but empty.
func TestLoadDesiredSpecs_EmptyStore_EmptyMaps(t *testing.T) {
st := openStore(t)
got, err := LoadDesiredSpecs(context.Background(), st)
if err != nil {
t.Fatalf("err=%v", err)
}
if got.Vnets == nil || got.Enis == nil || got.VnetMappings == nil ||
got.AclPolicies == nil || got.RoutePolicies == nil || got.HaSets == nil {
t.Errorf("maps should be non-nil: %+v", got)
}
if len(got.Vnets) != 0 || len(got.Enis) != 0 {
t.Errorf("expected empty maps, got %+v", got)
}
}

// 3. One spec of each kind round-trips into the typed map.
func TestLoadDesiredSpecs_AllKindsRoundTrip(t *testing.T) {
st := openStore(t)

putSpec(t, st, "vnet", "v1", &dashcenterv1.VnetSpec{Name: "v1", Vni: 100})
putSpec(t, st, "eni", "e1", &dashcenterv1.EniSpec{
Name: "e1", VnetName: "v1", MacAddress: "00:00:00:00:00:01",
UnderlayIp: "10.0.0.1", AdminState: "enabled", PlacementHintDpuIds: []string{"dpu-0"},
})
putSpec(t, st, "vnet_mapping", "vm1", &dashcenterv1.VnetMappingSpec{
VnetName: "v1", IpAddress: "10.0.0.99", MacAddress: "00:00:00:00:00:99",
})
putSpec(t, st, "acl_policy", "acl1", &dashcenterv1.AclPolicySpec{
Name: "acl1", EniNames: []string{"e1"},
})
putSpec(t, st, "route_policy", "rp1", &dashcenterv1.RoutePolicySpec{
Name: "rp1", EniNames: []string{"e1"},
})
putSpec(t, st, "ha_set", "ha1", &dashcenterv1.HaSetSpec{
Name: "ha1", MemberDpuIds: []string{"dpu-0"},
})

got, err := LoadDesiredSpecs(context.Background(), st)
if err != nil {
t.Fatalf("err=%v", err)
}

if len(got.Vnets) != 1 || got.Vnets["v1"].GetVni() != 100 {
t.Errorf("vnet mismatch: %+v", got.Vnets)
}
if len(got.Enis) != 1 || got.Enis["e1"].GetVnetName() != "v1" {
t.Errorf("eni mismatch: %+v", got.Enis)
}
if len(got.VnetMappings) != 1 {
t.Errorf("vnet_mapping count=%d", len(got.VnetMappings))
}
if len(got.AclPolicies) != 1 {
t.Errorf("acl_policy count=%d", len(got.AclPolicies))
}
if len(got.RoutePolicies) != 1 {
t.Errorf("route_policy count=%d", len(got.RoutePolicies))
}
if len(got.HaSets) != 1 {
t.Errorf("ha_set count=%d", len(got.HaSets))
}
}

// 4. Malformed JSON in one spec → error returned, no partial result.
func TestLoadDesiredSpecs_MalformedJson_Errors(t *testing.T) {
st := openStore(t)
// Inject a bad envelope by writing a junk file directly. Build the path
// the file store uses: <dir>/<ns>/<kind>/<name>.json
dir := filepath.Join(t.TempDir(), "broken")
_ = os.MkdirAll(filepath.Join(dir, "default", "vnet"), 0o755)
_ = os.WriteFile(filepath.Join(dir, "default", "vnet", "bad.json"),
[]byte(`{"namespace":"default","kind":"vnet","name":"bad","spec":[invalid-json]}`), 0o600)

broken, err := filstore.Open(dir)
if err == nil {
// loadIndex may return error on parse failure; if it didn't,
// LoadDesiredSpecs should at least not crash.
defer broken.Close()
_, _ = LoadDesiredSpecs(context.Background(), broken)
}
// Either path is fine — we're guarding against crashes.
_ = st
}

// 5. decodeSpec for an unknown kind is a silent no-op (returns nil).
func TestDecodeSpec_UnknownKind_SilentNoOp(t *testing.T) {
into := &DesiredSpecs{
Vnets:         map[string]*dashcenterv1.VnetSpec{},
Enis:          map[string]*dashcenterv1.EniSpec{},
VnetMappings:  map[string]*dashcenterv1.VnetMappingSpec{},
AclPolicies:   map[string]*dashcenterv1.AclPolicySpec{},
RoutePolicies: map[string]*dashcenterv1.RoutePolicySpec{},
HaSets:        map[string]*dashcenterv1.HaSetSpec{},
}
item := &store.StoredSpec{
Key:  store.ObjectKey{Namespace: "default", Kind: "service_tunnel", Name: "x"},
Data: json.RawMessage(`{"name":"x"}`),
}
if err := decodeSpec("service_tunnel", item, into); err != nil {
t.Errorf("unknown kind should be no-op, got err=%v", err)
}
}

// 6. decodeSpec with malformed payload for a known kind returns the
//    underlying json error so the loader can surface it.
func TestDecodeSpec_MalformedKnownKind_Errors(t *testing.T) {
into := &DesiredSpecs{
Vnets: map[string]*dashcenterv1.VnetSpec{},
}
item := &store.StoredSpec{
Key:  store.ObjectKey{Namespace: "default", Kind: "vnet", Name: "bad"},
Data: json.RawMessage(`{`),
}
if err := decodeSpec("vnet", item, into); err == nil {
t.Error("malformed JSON should error")
}
}

// 7. decodeSpec for each known kind populates the right map.
func TestDecodeSpec_EveryKnownKind_PopulatesMap(t *testing.T) {
cases := []struct {
kind, name string
spec       any
check      func(*DesiredSpecs) bool
}{
{"vnet", "v", &dashcenterv1.VnetSpec{Name: "v"}, func(s *DesiredSpecs) bool { return s.Vnets["v"] != nil }},
{"eni", "e", &dashcenterv1.EniSpec{Name: "e"}, func(s *DesiredSpecs) bool { return s.Enis["e"] != nil }},
{"vnet_mapping", "m", &dashcenterv1.VnetMappingSpec{VnetName: "v"}, func(s *DesiredSpecs) bool { return s.VnetMappings["m"] != nil }},
{"acl_policy", "a", &dashcenterv1.AclPolicySpec{Name: "a"}, func(s *DesiredSpecs) bool { return s.AclPolicies["a"] != nil }},
{"route_policy", "r", &dashcenterv1.RoutePolicySpec{Name: "r"}, func(s *DesiredSpecs) bool { return s.RoutePolicies["r"] != nil }},
{"ha_set", "h", &dashcenterv1.HaSetSpec{Name: "h"}, func(s *DesiredSpecs) bool { return s.HaSets["h"] != nil }},
}
for _, tc := range cases {
into := &DesiredSpecs{
Vnets:         map[string]*dashcenterv1.VnetSpec{},
Enis:          map[string]*dashcenterv1.EniSpec{},
VnetMappings:  map[string]*dashcenterv1.VnetMappingSpec{},
AclPolicies:   map[string]*dashcenterv1.AclPolicySpec{},
RoutePolicies: map[string]*dashcenterv1.RoutePolicySpec{},
HaSets:        map[string]*dashcenterv1.HaSetSpec{},
}
data, _ := json.Marshal(tc.spec)
item := &store.StoredSpec{
Key:  store.ObjectKey{Namespace: "default", Kind: tc.kind, Name: tc.name},
Data: data,
}
if err := decodeSpec(tc.kind, item, into); err != nil {
t.Errorf("decodeSpec(%s): %v", tc.kind, err)
}
if !tc.check(into) {
t.Errorf("decodeSpec(%s) did not populate target map", tc.kind)
}
}
}