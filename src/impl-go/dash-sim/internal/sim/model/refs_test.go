// refs_test.go — unit tests for referential integrity validation
// in model.Store.Apply(). Covers all FK families + the StrictRefs
// toggle + the checkRefs helper.

package model

import (
	"strings"
	"testing"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_group"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_acl_out "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_out"
	dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_eni_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni_route"
	dash_ha_scope "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope"
	dash_ha_scope_config "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope_config"
	dash_ha_scope_state "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope_state"
	dash_ha_set "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set"
	dash_ha_set_config "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set_config"
	dash_ha_set_state "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set_state"
	dash_meter_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/meter_rule"
	dash_meter_policy "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/meter_policy"
	dash_outbound_port_map "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/outbound_port_map"
	dash_outbound_port_map_range "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/outbound_port_map_range"
	dash_qos "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/qos"
	dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
	dash_route_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_group"
	dash_route_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_rule"
	dash_routing_appliance "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/routing_appliance"
	dash_tag "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/tag"
	dash_tunnel "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/tunnel"
	dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"google.golang.org/protobuf/proto"
)

func newTestStore() *Store {
	return New(events.New())
}

// apply is a helper that builds an *Object and calls store.Apply.
func apply(t *testing.T, s *Store, kind dashapi.ObjectKind, key []string, payload proto.Message) {
	t.Helper()
	obj := &dashapi.Object{Kind: kind, Key: key}
	switch kind {
	case dashapi.ObjectKind_OBJECT_KIND_VNET:
		obj.Payload = &dashapi.Object_Vnet{Vnet: payload.(*dash_vnet.Vnet)}
	case dashapi.ObjectKind_OBJECT_KIND_QOS:
		obj.Payload = &dashapi.Object_Qos{Qos: payload.(*dash_qos.Qos)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP:
		obj.Payload = &dashapi.Object_AclGroup{AclGroup: payload.(*dash_acl_group.AclGroup)}
	case dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP:
		obj.Payload = &dashapi.Object_RouteGroup{RouteGroup: payload.(*dash_route_group.RouteGroup)}
	case dashapi.ObjectKind_OBJECT_KIND_ROUTING_APPLIANCE:
		obj.Payload = &dashapi.Object_RoutingAppliance{RoutingAppliance: payload.(*dash_routing_appliance.RoutingAppliance)}
	case dashapi.ObjectKind_OBJECT_KIND_PREFIX_TAG:
		obj.Payload = &dashapi.Object_PrefixTag{PrefixTag: payload.(*dash_tag.PrefixTag)}
	case dashapi.ObjectKind_OBJECT_KIND_TUNNEL:
		obj.Payload = &dashapi.Object_Tunnel{Tunnel: payload.(*dash_tunnel.Tunnel)}
	case dashapi.ObjectKind_OBJECT_KIND_METER_POLICY:
		obj.Payload = &dashapi.Object_MeterPolicy{MeterPolicy: payload.(*dash_meter_policy.MeterPolicy)}
	case dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP:
		obj.Payload = &dashapi.Object_OutboundPortMap{OutboundPortMap: payload.(*dash_outbound_port_map.OutboundPortMap)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SET:
		obj.Payload = &dashapi.Object_HaSet{HaSet: payload.(*dash_ha_set.HaSet)}
	case dashapi.ObjectKind_OBJECT_KIND_ENI:
		obj.Payload = &dashapi.Object_Eni{Eni: payload.(*dash_eni.Eni)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_RULE:
		obj.Payload = &dashapi.Object_AclRule{AclRule: payload.(*dash_acl_rule.AclRule)}
	case dashapi.ObjectKind_OBJECT_KIND_ROUTE:
		obj.Payload = &dashapi.Object_Route{Route: payload.(*dash_route.Route)}
	case dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING:
		obj.Payload = &dashapi.Object_VnetMapping{VnetMapping: payload.(*dash_vnet_mapping.VnetMapping)}
	case dashapi.ObjectKind_OBJECT_KIND_METER_RULE:
		obj.Payload = &dashapi.Object_MeterRule{MeterRule: payload.(*dash_meter_rule.MeterRule)}
	case dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE:
		obj.Payload = &dashapi.Object_OutboundPortMapRange{OutboundPortMapRange: payload.(*dash_outbound_port_map_range.OutboundPortMapRange)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE:
		obj.Payload = &dashapi.Object_HaScope{HaScope: payload.(*dash_ha_scope.HaScope)}
	case dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE:
		obj.Payload = &dashapi.Object_EniRoute{EniRoute: payload.(*dash_eni_route.EniRoute)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_IN:
		obj.Payload = &dashapi.Object_AclIn{AclIn: payload.(*dash_acl_in.AclIn)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_OUT:
		obj.Payload = &dashapi.Object_AclOut{AclOut: payload.(*dash_acl_out.AclOut)}
	case dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE:
		obj.Payload = &dashapi.Object_RouteRule{RouteRule: payload.(*dash_route_rule.RouteRule)}
	case dashapi.ObjectKind_OBJECT_KIND_METER:
		obj.Payload = &dashapi.Object_Meter{Meter: payload.(*dash_meter_rule.Meter)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SET_CONFIG:
		obj.Payload = &dashapi.Object_HaSetConfig{HaSetConfig: payload.(*dash_ha_set_config.HaSetConfig)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SET_STATE:
		obj.Payload = &dashapi.Object_HaSetState{HaSetState: payload.(*dash_ha_set_state.HaSetState)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG:
		obj.Payload = &dashapi.Object_HaScopeConfig{HaScopeConfig: payload.(*dash_ha_scope_config.HaScopeConfig)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE:
		obj.Payload = &dashapi.Object_HaScopeState{HaScopeState: payload.(*dash_ha_scope_state.HaScopeState)}
	default:
		t.Fatalf("apply: unsupported kind %v in test helper", kind)
	}
	_, _, err := s.Apply(obj)
	if err != nil {
		t.Fatalf("apply %v %v: %v", kind, key, err)
	}
}

func mustFail(t *testing.T, s *Store, kind dashapi.ObjectKind, key []string, payload proto.Message, wantSubstr string) {
	t.Helper()
	obj := &dashapi.Object{Kind: kind, Key: key}
	// Reuse the same switch as apply but check for error
	switch kind {
	case dashapi.ObjectKind_OBJECT_KIND_ENI:
		obj.Payload = &dashapi.Object_Eni{Eni: payload.(*dash_eni.Eni)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_RULE:
		obj.Payload = &dashapi.Object_AclRule{AclRule: payload.(*dash_acl_rule.AclRule)}
	case dashapi.ObjectKind_OBJECT_KIND_ROUTE:
		obj.Payload = &dashapi.Object_Route{Route: payload.(*dash_route.Route)}
	case dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING:
		obj.Payload = &dashapi.Object_VnetMapping{VnetMapping: payload.(*dash_vnet_mapping.VnetMapping)}
	case dashapi.ObjectKind_OBJECT_KIND_METER_RULE:
		obj.Payload = &dashapi.Object_MeterRule{MeterRule: payload.(*dash_meter_rule.MeterRule)}
	case dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE:
		obj.Payload = &dashapi.Object_OutboundPortMapRange{OutboundPortMapRange: payload.(*dash_outbound_port_map_range.OutboundPortMapRange)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE:
		obj.Payload = &dashapi.Object_HaScope{HaScope: payload.(*dash_ha_scope.HaScope)}
	case dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE:
		obj.Payload = &dashapi.Object_EniRoute{EniRoute: payload.(*dash_eni_route.EniRoute)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_IN:
		obj.Payload = &dashapi.Object_AclIn{AclIn: payload.(*dash_acl_in.AclIn)}
	case dashapi.ObjectKind_OBJECT_KIND_ACL_OUT:
		obj.Payload = &dashapi.Object_AclOut{AclOut: payload.(*dash_acl_out.AclOut)}
	case dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE:
		obj.Payload = &dashapi.Object_RouteRule{RouteRule: payload.(*dash_route_rule.RouteRule)}
	case dashapi.ObjectKind_OBJECT_KIND_METER:
		obj.Payload = &dashapi.Object_Meter{Meter: payload.(*dash_meter_rule.Meter)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SET_CONFIG:
		obj.Payload = &dashapi.Object_HaSetConfig{HaSetConfig: payload.(*dash_ha_set_config.HaSetConfig)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SET_STATE:
		obj.Payload = &dashapi.Object_HaSetState{HaSetState: payload.(*dash_ha_set_state.HaSetState)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG:
		obj.Payload = &dashapi.Object_HaScopeConfig{HaScopeConfig: payload.(*dash_ha_scope_config.HaScopeConfig)}
	case dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE:
		obj.Payload = &dashapi.Object_HaScopeState{HaScopeState: payload.(*dash_ha_scope_state.HaScopeState)}
	default:
		t.Fatalf("mustFail: unsupported kind %v in test helper", kind)
	}
	_, _, err := s.Apply(obj)
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error %q does not contain %q", err.Error(), wantSubstr)
	}
}

// seed creates all Tier 0 root objects so Tier 1/2 tests can reference them.
func seed(t *testing.T, s *Store) {
	t.Helper()
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-blue"}, &dash_vnet.Vnet{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_QOS, []string{"qos-default"}, &dash_qos.Qos{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, []string{"acl-grp-1"}, &dash_acl_group.AclGroup{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP, []string{"rg-prod"}, &dash_route_group.RouteGroup{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTING_APPLIANCE, []string{"ra-1"}, &dash_routing_appliance.RoutingAppliance{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_PREFIX_TAG, []string{"tag-corp"}, &dash_tag.PrefixTag{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_TUNNEL, []string{"tun-1"}, &dash_tunnel.Tunnel{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_METER_POLICY, []string{"mp-1"}, &dash_meter_policy.MeterPolicy{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP, []string{"opm-1"}, &dash_outbound_port_map.OutboundPortMap{})
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SET, []string{"ha-set-1"}, &dash_ha_set.HaSet{})
	// Tier 1: ENI (needed by Tier 2 tests)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"}, &dash_eni.Eni{Vnet: "vnet-blue"})
	// Tier 1: HA scope (needed by ha_scope_config/state)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE, []string{"hs-1"}, &dash_ha_scope.HaScope{HaSetId: "ha-set-1"})
}

// ── Tier 1 tests: each references only Tier 0 ─────────────────────────

func TestRefs_ENI_MissingVnet(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-x"},
		&dash_eni.Eni{Vnet: "no-such-vnet"}, "vnet")
}

func TestRefs_ENI_MissingQos(t *testing.T) {
	s := newTestStore()
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"v1"}, &dash_vnet.Vnet{})
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-x"},
		&dash_eni.Eni{Vnet: "v1", Qos: "no-such-qos"}, "qos")
}

func TestRefs_ENI_AllRefsOK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-ok"},
		&dash_eni.Eni{Vnet: "vnet-blue", Qos: "qos-default"})
}

func TestRefs_AclRule_MissingGroup(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"no-group", "100"},
		&dash_acl_rule.AclRule{}, "acl_group")
}

func TestRefs_AclRule_MissingSrcTag(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-grp-1", "100"},
		&dash_acl_rule.AclRule{SrcTag: []string{"no-tag"}}, "prefix_tag")
}

func TestRefs_AclRule_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-grp-1", "100"},
		&dash_acl_rule.AclRule{SrcTag: []string{"tag-corp"}})
}

func TestRefs_Route_MissingRouteGroup(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"no-group", "10.0.0.0/8"},
		&dash_route.Route{}, "route_group")
}

func TestRefs_Route_MissingVnet(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{RoutingTypeData: &dash_route.Route_Vnet{Vnet: "no-vnet"}}, "vnet")
}

func TestRefs_Route_MissingAppliance(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{RoutingTypeData: &dash_route.Route_Appliance{Appliance: "no-app"}}, "routing_appliance")
}

func TestRefs_Route_MissingTunnel(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{Tunnel: strPtr("no-tun")}, "tunnel")
}

func TestRefs_Route_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{RoutingTypeData: &dash_route.Route_Vnet{Vnet: "vnet-blue"}})
}

func TestRefs_VnetMapping_MissingVnet(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"no-vnet", "10.0.0.1"},
		&dash_vnet_mapping.VnetMapping{}, "vnet")
}

func TestRefs_VnetMapping_MissingTunnel(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-blue", "10.0.0.1"},
		&dash_vnet_mapping.VnetMapping{Tunnel: strPtr("no-tun")}, "tunnel")
}

func TestRefs_VnetMapping_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-blue", "10.0.0.1"},
		&dash_vnet_mapping.VnetMapping{})
}

func TestRefs_MeterRule_MissingPolicy(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_METER_RULE, []string{"no-policy", "1"},
		&dash_meter_rule.MeterRule{}, "meter_policy")
}

func TestRefs_MeterRule_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_METER_RULE, []string{"mp-1", "1"},
		&dash_meter_rule.MeterRule{})
}

func TestRefs_OPMRange_MissingPortMap(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE, []string{"no-map", "1000", "2000"},
		&dash_outbound_port_map_range.OutboundPortMapRange{}, "outbound_port_map")
}

func TestRefs_HaScope_MissingHaSet(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE, []string{"hs-x"},
		&dash_ha_scope.HaScope{HaSetId: "no-set"}, "ha_set")
}

func TestRefs_HaScope_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE, []string{"hs-new"},
		&dash_ha_scope.HaScope{HaSetId: "ha-set-1"})
}

// ── Tier 2 tests: reference Tier 0 + Tier 1 ──────────────────────────

func TestRefs_EniRoute_MissingENI(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, []string{"no-eni"},
		&dash_eni_route.EniRoute{GroupId: "rg-prod"}, "eni")
}

func TestRefs_EniRoute_MissingRouteGroup(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, []string{"eni-001"},
		&dash_eni_route.EniRoute{GroupId: "no-rg"}, "route_group")
}

func TestRefs_EniRoute_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, []string{"eni-001"},
		&dash_eni_route.EniRoute{GroupId: "rg-prod"})
}

func TestRefs_AclIn_MissingENI(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"no-eni", "1"},
		&dash_acl_in.AclIn{V4AclGroupId: "acl-grp-1"}, "eni")
}

func TestRefs_AclIn_MissingGroup(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"eni-001", "1"},
		&dash_acl_in.AclIn{V4AclGroupId: "no-grp"}, "acl_group")
}

func TestRefs_AclIn_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"eni-001", "1"},
		&dash_acl_in.AclIn{V4AclGroupId: "acl-grp-1"})
}

func TestRefs_AclOut_MissingENI(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, []string{"no-eni", "1"},
		&dash_acl_out.AclOut{V4AclGroupId: "acl-grp-1"}, "eni")
}

func TestRefs_AclOut_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, []string{"eni-001", "1"},
		&dash_acl_out.AclOut{V4AclGroupId: "acl-grp-1"})
}

func TestRefs_RouteRule_MissingENI(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, []string{"no-eni", "1001", "10.0.0.0/8", "100"},
		&dash_route_rule.RouteRule{}, "eni")
}

func TestRefs_RouteRule_MissingVnet(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, []string{"eni-001", "1001", "10.0.0.0/8", "100"},
		&dash_route_rule.RouteRule{Vnet: strPtr("no-vnet")}, "vnet")
}

func TestRefs_RouteRule_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, []string{"eni-001", "1001", "10.0.0.0/8", "100"},
		&dash_route_rule.RouteRule{Vnet: strPtr("vnet-blue")})
}

func TestRefs_Meter_MissingENI(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_METER, []string{"no-eni", "1"},
		&dash_meter_rule.Meter{}, "eni")
}

func TestRefs_Meter_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_METER, []string{"eni-001", "1"},
		&dash_meter_rule.Meter{})
}

// ── StrictRefs toggle ─────────────────────────────────────────────────

func TestRefs_StrictRefsOff_AcceptsDanglingRef(t *testing.T) {
	s := newTestStore()
	s.SetStrictRefs(false)
	// Should succeed even though vnet "ghost" doesn't exist
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-ok"},
		&dash_eni.Eni{Vnet: "ghost"})
}

func TestRefs_StrictRefsOn_RejectsDanglingRef(t *testing.T) {
	s := newTestStore()
	// StrictRefs is true by default
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-x"},
		&dash_eni.Eni{Vnet: "ghost"}, "vnet")
}

// ── Tier 0 (roots) always succeed ─────────────────────────────────────

func TestRefs_Tier0_VnetNoFKCheck(t *testing.T) {
	s := newTestStore()
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"v1"}, &dash_vnet.Vnet{})
}

func TestRefs_Tier0_AclGroupNoFKCheck(t *testing.T) {
	s := newTestStore()
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, []string{"g1"}, &dash_acl_group.AclGroup{})
}

// ── Error message quality ─────────────────────────────────────────────

func TestRefs_ErrorContainsKindAndRefName(t *testing.T) {
	s := newTestStore()
	_, _, err := s.Apply(&dashapi.Object{
		Kind:    dashapi.ObjectKind_OBJECT_KIND_ENI,
		Key:     []string{"eni-err"},
		Payload: &dashapi.Object_Eni{Eni: &dash_eni.Eni{Vnet: "missing-vnet-xyz"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "missing-vnet-xyz") {
		t.Errorf("error should name the missing ref: %s", msg)
	}
	if !strings.Contains(msg, "vnet") {
		t.Errorf("error should name the ref kind: %s", msg)
	}
	if !strings.Contains(msg, "create it first") {
		t.Errorf("error should suggest the fix: %s", msg)
	}
}

// ── HA_SET_CONFIG / HA_SET_STATE (Tier 1) ─────────────────────────────

func TestRefs_HaSetConfig_MissingHaSet(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SET_CONFIG, []string{"no-set"},
		&dash_ha_set_config.HaSetConfig{}, "ha_set")
}

func TestRefs_HaSetConfig_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SET_CONFIG, []string{"ha-set-1"},
		&dash_ha_set_config.HaSetConfig{})
}

func TestRefs_HaSetState_MissingHaSet(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SET_STATE, []string{"no-set"},
		&dash_ha_set_state.HaSetState{}, "ha_set")
}

func TestRefs_HaSetState_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SET_STATE, []string{"ha-set-1"},
		&dash_ha_set_state.HaSetState{})
}

// ── HA_SCOPE_CONFIG (Tier 2) — key = [vdpu_id, ha_scope_id] ──────────

func TestRefs_HaScopeConfig_MissingHaScope(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, []string{"vdpu-1", "no-scope"},
		&dash_ha_scope_config.HaScopeConfig{HaSetId: "ha-set-1"}, "ha_scope")
}

func TestRefs_HaScopeConfig_MissingHaSet(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, []string{"vdpu-1", "hs-1"},
		&dash_ha_scope_config.HaScopeConfig{HaSetId: "no-set"}, "ha_set")
}

func TestRefs_HaScopeConfig_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, []string{"vdpu-1", "hs-1"},
		&dash_ha_scope_config.HaScopeConfig{HaSetId: "ha-set-1"})
}

// ── HA_SCOPE_STATE (Tier 2) — key = [ha_scope_id] ────────────────────

func TestRefs_HaScopeState_MissingHaScope(t *testing.T) {
	s := newTestStore()
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE, []string{"no-scope"},
		&dash_ha_scope_state.HaScopeState{}, "ha_scope")
}

func TestRefs_HaScopeState_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE, []string{"hs-1"},
		&dash_ha_scope_state.HaScopeState{})
}

// ── Additional coverage: secondary FK fields ──────────────────────────

func TestRefs_OPMRange_OK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE, []string{"opm-1", "1000", "2000"},
		&dash_outbound_port_map_range.OutboundPortMapRange{})
}

func TestRefs_VnetMapping_MissingPortMap(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-blue", "10.0.0.1"},
		&dash_vnet_mapping.VnetMapping{PortMap: strPtr("no-pm")}, "outbound_port_map")
}

func TestRefs_VnetMapping_AllRefsOK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-blue", "10.0.0.1"},
		&dash_vnet_mapping.VnetMapping{Tunnel: strPtr("tun-1"), PortMap: strPtr("opm-1")})
}

func TestRefs_AclRule_MissingDstTag(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-grp-1", "100"},
		&dash_acl_rule.AclRule{DstTag: []string{"no-tag"}}, "prefix_tag")
}

func TestRefs_AclRule_BothTagsOK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-grp-1", "100"},
		&dash_acl_rule.AclRule{SrcTag: []string{"tag-corp"}, DstTag: []string{"tag-corp"}})
}

func TestRefs_Route_VnetDirectRef(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{RoutingTypeData: &dash_route.Route_VnetDirect{
			VnetDirect: &dash_route.VnetDirect{Vnet: "vnet-blue"},
		}})
}

func TestRefs_Route_VnetDirectMissing(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{RoutingTypeData: &dash_route.Route_VnetDirect{
			VnetDirect: &dash_route.VnetDirect{Vnet: "no-vnet"},
		}}, "vnet")
}

func TestRefs_AclIn_MissingV6Group(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"eni-001", "1"},
		&dash_acl_in.AclIn{V6AclGroupId: "no-grp"}, "acl_group")
}

func TestRefs_AclIn_AllGroupsOK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"eni-001", "1"},
		&dash_acl_in.AclIn{V4AclGroupId: "acl-grp-1", V6AclGroupId: "acl-grp-1"})
}

func TestRefs_AclOut_MissingV6Group(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	mustFail(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, []string{"eni-001", "1"},
		&dash_acl_out.AclOut{V6AclGroupId: "no-grp"}, "acl_group")
}

func TestRefs_AclOut_AllGroupsOK(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, []string{"eni-001", "1"},
		&dash_acl_out.AclOut{V4AclGroupId: "acl-grp-1", V6AclGroupId: "acl-grp-1"})
}

// ── Edge case: optional FK field is empty string (skip branch) ────────

func TestRefs_ENI_OptionalQosEmpty_NoError(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	// Qos="" means "no qos reference" — must not trigger FK check
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-no-qos"},
		&dash_eni.Eni{Vnet: "vnet-blue", Qos: ""})
}

func TestRefs_Route_OptionalTunnelEmpty_NoError(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	// Tunnel=nil means no tunnel ref — should pass
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.0.0.0/8"},
		&dash_route.Route{})
}

func TestRefs_HaScopeConfig_OptionalHaSetEmpty_NoError(t *testing.T) {
	s := newTestStore()
	seed(t, s)
	// HaSetId="" means no ha_set ref — skip FK check for that field
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, []string{"vdpu-1", "hs-1"},
		&dash_ha_scope_config.HaScopeConfig{HaSetId: ""})
}

// ── Edge case: Extract returns slice with empty string (defensive) ────

func TestRefs_CheckRefs_EmptyRefInSliceIsSkipped(t *testing.T) {
	// Temporarily append a rule whose Extract returns [""] to cover
	// the defensive `ref == ""` → continue branch in checkRefs.
	orig := fkRules
	defer func() { fkRules = orig }()
	fkRules = append(fkRules, fkRule{
		Kind:    dashapi.ObjectKind_OBJECT_KIND_VNET,
		Field:   "test_empty",
		RefKind: dashapi.ObjectKind_OBJECT_KIND_QOS,
		Extract: func(_ proto.Message, _ []string) []string {
			return []string{""}
		},
	})
	s := newTestStore()
	// Vnet must still pass despite the rule returning [""]
	apply(t, s, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"v1"}, &dash_vnet.Vnet{})
}

func strPtr(s string) *string { return &s }
