package placement

import (
"context"
"encoding/json"
"fmt"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// allKinds enumerates every spec kind dashd persists. Adding a new kind
// requires a one-line addition here plus the corresponding map population
// in LoadDesiredSpecs.
var allKinds = []string{
"vnet", "eni", "vnet_mapping", "acl_policy", "route_policy", "ha_set",
}

// LoadDesiredSpecs reads every persisted spec from the DesiredStore
// (across all kinds and the default namespace) and assembles them into
// a *DesiredSpecs ready for Resolve / ResolveAll.
//
// Errors from individual List calls are wrapped and returned; a single
// malformed spec aborts the entire load to avoid producing an
// inconsistent placement view.
//
// Currently scans only the default namespace; multi-namespace support
// will be added when the store gains namespace enumeration.
func LoadDesiredSpecs(ctx context.Context, st store.DesiredStore) (*DesiredSpecs, error) {
if st == nil {
return &DesiredSpecs{}, nil
}

specs := &DesiredSpecs{
Vnets:         map[string]*dashcenterv1.VnetSpec{},
Enis:          map[string]*dashcenterv1.EniSpec{},
VnetMappings:  map[string]*dashcenterv1.VnetMappingSpec{},
AclPolicies:   map[string]*dashcenterv1.AclPolicySpec{},
RoutePolicies: map[string]*dashcenterv1.RoutePolicySpec{},
HaSets:        map[string]*dashcenterv1.HaSetSpec{},
}

ns := store.DefaultNamespace
for _, kind := range allKinds {
items, err := st.List(ctx, ns, kind)
if err != nil {
return nil, fmt.Errorf("placement: list %s: %w", kind, err)
}
for _, item := range items {
if err := decodeSpec(kind, item, specs); err != nil {
return nil, fmt.Errorf("placement: decode %s/%s: %w",
kind, item.Key.Name, err)
}
}
}

return specs, nil
}

// decodeSpec unmarshals one StoredSpec's payload into the typed spec map
// inside DesiredSpecs. Field name semantics: the file store serializes
// via encoding/json on the dashcenter.v1 proto-generated structs, so we
// decode with the same encoder to round-trip the PascalCase JSON keys.
func decodeSpec(kind string, item *store.StoredSpec, into *DesiredSpecs) error {
switch kind {
case "vnet":
v := &dashcenterv1.VnetSpec{}
if err := json.Unmarshal(item.Data, v); err != nil {
return err
}
into.Vnets[item.Key.Name] = v
case "eni":
v := &dashcenterv1.EniSpec{}
if err := json.Unmarshal(item.Data, v); err != nil {
return err
}
into.Enis[item.Key.Name] = v
case "vnet_mapping":
v := &dashcenterv1.VnetMappingSpec{}
if err := json.Unmarshal(item.Data, v); err != nil {
return err
}
into.VnetMappings[item.Key.Name] = v
case "acl_policy":
v := &dashcenterv1.AclPolicySpec{}
if err := json.Unmarshal(item.Data, v); err != nil {
return err
}
into.AclPolicies[item.Key.Name] = v
case "route_policy":
v := &dashcenterv1.RoutePolicySpec{}
if err := json.Unmarshal(item.Data, v); err != nil {
return err
}
into.RoutePolicies[item.Key.Name] = v
case "ha_set":
v := &dashcenterv1.HaSetSpec{}
if err := json.Unmarshal(item.Data, v); err != nil {
return err
}
into.HaSets[item.Key.Name] = v
default:
// Silently skip unknown kinds — they're not part of placement.
return nil
}
return nil
}