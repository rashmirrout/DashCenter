// refs.go — declarative foreign-key validation for the DASH object
// store. Every FK relationship across the 29 object kinds is encoded
// in the fkRules table. Store.Apply() calls checkRefs() before
// writing when StrictRefs is true.
//
// Design: each rule describes ONE FK relationship:
//
//	fkRule{
//	    Kind:    the kind being Applied,
//	    Field:   human-readable field name (for error messages),
//	    RefKind: the kind that must already exist,
//	    Extract: func(obj, key) → []string of ref keys to check,
//	}
//
// Extract receives both the proto payload and the object key parts so
// it can handle key-embedded FKs (e.g., acl_rule's group_id is in the
// key, not the payload).
//
// When Extract returns an empty slice, the rule is satisfied (the FK
// is optional and not set). When it returns non-empty strings, each
// must exist as a key of RefKind in the store.

package model

import (
	"fmt"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_acl_out "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_out"
	dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_eni_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni_route"
	dash_ha_scope "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope"
	dash_ha_scope_config "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope_config"
	dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
	dash_route_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_rule"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	"google.golang.org/protobuf/proto"
)

// fkRule describes one foreign-key relationship.
type fkRule struct {
	Kind    dashapi.ObjectKind
	Field   string                                              // human-readable (for error messages)
	RefKind dashapi.ObjectKind                                  // the kind that must exist
	Extract func(payload proto.Message, key []string) []string  // returns ref keys to check; empty = skip
}

// fkRules is the complete FK registry for all 29 DASH object kinds.
// Tier 0 kinds (vnet, qos, acl_group, route_group, etc.) have no
// entries — they reference nothing.
var fkRules = []fkRule{
	// ── ENI (Tier 1) ─────────────────────────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Field: "vnet",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_VNET,
		Extract: func(p proto.Message, _ []string) []string {
			if e, ok := p.(*dash_eni.Eni); ok && e.GetVnet() != "" {
				return []string{e.GetVnet()}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Field: "qos",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_QOS,
		Extract: func(p proto.Message, _ []string) []string {
			if e, ok := p.(*dash_eni.Eni); ok && e.GetQos() != "" {
				return []string{e.GetQos()}
			}
			return nil
		},
	},

	// ── ACL_RULE (Tier 1) — key[0]=group_id ──────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, Field: "key.group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, Field: "src_tag",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_PREFIX_TAG,
		Extract: func(p proto.Message, _ []string) []string {
			if r, ok := p.(*dash_acl_rule.AclRule); ok {
				return nonEmpty(r.GetSrcTag())
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, Field: "dst_tag",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_PREFIX_TAG,
		Extract: func(p proto.Message, _ []string) []string {
			if r, ok := p.(*dash_acl_rule.AclRule); ok {
				return nonEmpty(r.GetDstTag())
			}
			return nil
		},
	},

	// ── ROUTE (Tier 1) — key[0]=group_id ─────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE, Field: "key.group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE, Field: "vnet",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_VNET,
		Extract: func(p proto.Message, _ []string) []string {
			r, ok := p.(*dash_route.Route)
			if !ok {
				return nil
			}
			// Route has multiple vnet references via oneof action
			var refs []string
			if v := r.GetVnet(); v != "" {
				refs = append(refs, v)
			}
			if vd := r.GetVnetDirect(); vd != nil && vd.GetVnet() != "" {
				refs = append(refs, vd.GetVnet())
			}
			return refs
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE, Field: "appliance",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ROUTING_APPLIANCE,
		Extract: func(p proto.Message, _ []string) []string {
			if r, ok := p.(*dash_route.Route); ok && r.GetAppliance() != "" {
				return []string{r.GetAppliance()}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE, Field: "tunnel",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_TUNNEL,
		Extract: func(p proto.Message, _ []string) []string {
			if r, ok := p.(*dash_route.Route); ok && r.GetTunnel() != "" {
				return []string{r.GetTunnel()}
			}
			return nil
		},
	},

	// ── VNET_MAPPING (Tier 1) — key[0]=vnet ──────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, Field: "key.vnet",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_VNET,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, Field: "tunnel",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_TUNNEL,
		Extract: func(p proto.Message, _ []string) []string {
			if m, ok := p.(*dash_vnet_mapping.VnetMapping); ok && m.GetTunnel() != "" {
				return []string{m.GetTunnel()}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, Field: "port_map",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP,
		Extract: func(p proto.Message, _ []string) []string {
			if m, ok := p.(*dash_vnet_mapping.VnetMapping); ok && m.GetPortMap() != "" {
				return []string{m.GetPortMap()}
			}
			return nil
		},
	},

	// ── METER_RULE (Tier 1) — key[0]=meter_policy_id ─────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_METER_RULE, Field: "key.meter_policy_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_METER_POLICY,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},

	// ── OUTBOUND_PORT_MAP_RANGE (Tier 1) — key[0]=map_id ─
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE, Field: "key.map_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},

	// ── HA_SCOPE (Tier 1) ────────────────────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE, Field: "ha_set_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_HA_SET,
		Extract: func(p proto.Message, _ []string) []string {
			if s, ok := p.(*dash_ha_scope.HaScope); ok && s.GetHaSetId() != "" {
				return []string{s.GetHaSetId()}
			}
			return nil
		},
	},

	// ── HA_SET_CONFIG (Tier 1) — key[0]=ha_set_id ────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SET_CONFIG, Field: "key.ha_set_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_HA_SET,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},

	// ── HA_SET_STATE (Tier 1) — key[0]=ha_set_id ─────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SET_STATE, Field: "key.ha_set_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_HA_SET,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},

	// ── ENI_ROUTE (Tier 2) — key[0]=eni ──────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, Field: "key.eni",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, Field: "group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP,
		Extract: func(p proto.Message, _ []string) []string {
			if er, ok := p.(*dash_eni_route.EniRoute); ok && er.GetGroupId() != "" {
				return []string{er.GetGroupId()}
			}
			return nil
		},
	},

	// ── ACL_IN (Tier 2) — key[0]=eni ─────────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_IN, Field: "key.eni",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_IN, Field: "v4_acl_group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP,
		Extract: func(p proto.Message, _ []string) []string {
			if a, ok := p.(*dash_acl_in.AclIn); ok && a.GetV4AclGroupId() != "" {
				return []string{a.GetV4AclGroupId()}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_IN, Field: "v6_acl_group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP,
		Extract: func(p proto.Message, _ []string) []string {
			if a, ok := p.(*dash_acl_in.AclIn); ok && a.GetV6AclGroupId() != "" {
				return []string{a.GetV6AclGroupId()}
			}
			return nil
		},
	},

	// ── ACL_OUT (Tier 2) — key[0]=eni ────────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, Field: "key.eni",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, Field: "v4_acl_group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP,
		Extract: func(p proto.Message, _ []string) []string {
			if a, ok := p.(*dash_acl_out.AclOut); ok && a.GetV4AclGroupId() != "" {
				return []string{a.GetV4AclGroupId()}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, Field: "v6_acl_group_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP,
		Extract: func(p proto.Message, _ []string) []string {
			if a, ok := p.(*dash_acl_out.AclOut); ok && a.GetV6AclGroupId() != "" {
				return []string{a.GetV6AclGroupId()}
			}
			return nil
		},
	},

	// ── ROUTE_RULE (Tier 2) — key[0]=eni ─────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, Field: "key.eni",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, Field: "vnet",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_VNET,
		Extract: func(p proto.Message, _ []string) []string {
			if rr, ok := p.(*dash_route_rule.RouteRule); ok && rr.GetVnet() != "" {
				return []string{rr.GetVnet()}
			}
			return nil
		},
	},

	// ── METER (Tier 2) — key[0]=eni ──────────────────────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_METER, Field: "key.eni",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_ENI,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},

	// ── HA_SCOPE_CONFIG (Tier 2) — key[1]=ha_scope_id ────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, Field: "key.ha_scope_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 1 && key[1] != "" {
				return []string{key[1]}
			}
			return nil
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, Field: "ha_set_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_HA_SET,
		Extract: func(p proto.Message, _ []string) []string {
			if sc, ok := p.(*dash_ha_scope_config.HaScopeConfig); ok && sc.GetHaSetId() != "" {
				return []string{sc.GetHaSetId()}
			}
			return nil
		},
	},

	// ── HA_SCOPE_STATE (Tier 2) — key[0]=ha_scope_id ─────
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE, Field: "key.ha_scope_id",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE,
		Extract: func(_ proto.Message, key []string) []string {
			if len(key) > 0 && key[0] != "" {
				return []string{key[0]}
			}
			return nil
		},
	},
}

// checkRefs validates that every FK referenced by the object exists in the
// store. Must be called under s.mu.RLock (the caller already holds the lock
// in Apply). Returns nil if all references are satisfied.
func (s *Store) checkRefs(kind dashapi.ObjectKind, payload proto.Message, key []string) error {
	for i := range fkRules {
		r := &fkRules[i]
		if r.Kind != kind {
			continue
		}
		refs := r.Extract(payload, key)
		for _, ref := range refs {
			if ref == "" {
				continue
			}
			tbl := s.tables[r.RefKind]
			if tbl == nil || tbl[ref] == nil {
				kindName := kindNameOf(kind)
				refKindName := kindNameOf(r.RefKind)
				return fmt.Errorf("referential integrity: %s references %s %q (field %s) which does not exist; create it first",
					kindName, refKindName, ref, r.Field)
			}
		}
	}
	return nil
}

// nonEmpty filters a string slice to non-empty values.
func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// kindNameOf returns the human-readable kind name for error messages.
func kindNameOf(k dashapi.ObjectKind) string {
	return strings.ToLower(strings.TrimPrefix(k.String(), "OBJECT_KIND_"))
}
