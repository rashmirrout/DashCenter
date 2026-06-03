// Package kinds is the single source of truth for the 29 DASH object kinds
// exposed by the DashApi service. It maps an ObjectKind to:
//
//   - the zero-valued upstream proto.Message it carries,
//   - the upstream key-message field names (purely informational),
//   - how to unpack/pack the typed payload from/to a dashapi.Object.
//
// Every per-kind switch in the codebase lives HERE so that adding a new kind
// is a one-place change.
package kinds

import (
	"fmt"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_group"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_acl_out "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_out"
	dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
	dash_appliance "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/appliance"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_eni_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni_route"
	dash_ha_scope "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope"
	dash_ha_scope_config "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope_config"
	dash_ha_scope_state "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_scope_state"
	dash_ha_set "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set"
	dash_ha_set_config "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set_config"
	dash_ha_set_state "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set_state"
	dash_meter_policy "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/meter_policy"
	dash_meter_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/meter_rule"
	dash_outbound_port_map "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/outbound_port_map"
	dash_outbound_port_map_range "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/outbound_port_map_range"
	dash_pa_validation "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/pa_validation"
	dash_qos "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/qos"
	dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
	dash_route_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_group"
	dash_route_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_rule"
	dash_route_type "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_type"
	dash_routing_appliance "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/routing_appliance"
	dash_tag "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/tag"
	dash_tunnel "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/tunnel"
	dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	"google.golang.org/protobuf/proto"
)

// Info describes one kind.
type Info struct {
	Kind     dashapi.ObjectKind
	Name     string   // short, lower_snake (matches CLI)
	KeyParts []string // field names of upstream KeyMessage, in order
	NewZero  func() proto.Message
	Pack     func(*dashapi.Object, proto.Message)        // sets payload oneof
	Unpack   func(*dashapi.Object) (proto.Message, bool) // extracts payload
}

// TableName returns the canonical SONiC APP_DB table prefix for this kind,
// e.g. "DASH_VNET_TABLE", "DASH_VNET_MAPPING_TABLE". Real DASH orchagent
// keys are `<TableName>:<joined-key>`.
func (i Info) TableName() string {
	return "DASH_" + strings.ToUpper(i.Name) + "_TABLE"
}

// All is the registry of supported kinds, ordered by ObjectKind value.
var All = []Info{
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_APPLIANCE, Name: "appliance",
		KeyParts: []string{"appliance_id"},
		NewZero:  func() proto.Message { return &dash_appliance.Appliance{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Appliance{Appliance: m.(*dash_appliance.Appliance)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Appliance)
			if !ok || x == nil {
				return nil, false
			}
			return x.Appliance, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET, Name: "vnet",
		KeyParts: []string{"vnet_name"},
		NewZero:  func() proto.Message { return &dash_vnet.Vnet{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Vnet{Vnet: m.(*dash_vnet.Vnet)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Vnet)
			if !ok || x == nil {
				return nil, false
			}
			return x.Vnet, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI, Name: "eni",
		KeyParts: []string{"eni"},
		NewZero:  func() proto.Message { return &dash_eni.Eni{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Eni{Eni: m.(*dash_eni.Eni)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Eni)
			if !ok || x == nil {
				return nil, false
			}
			return x.Eni, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, Name: "eni_route",
		KeyParts: []string{"eni"},
		NewZero:  func() proto.Message { return &dash_eni_route.EniRoute{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_EniRoute{EniRoute: m.(*dash_eni_route.EniRoute)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_EniRoute)
			if !ok || x == nil {
				return nil, false
			}
			return x.EniRoute, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, Name: "acl_group",
		KeyParts: []string{"group_id"},
		NewZero:  func() proto.Message { return &dash_acl_group.AclGroup{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_AclGroup{AclGroup: m.(*dash_acl_group.AclGroup)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_AclGroup)
			if !ok || x == nil {
				return nil, false
			}
			return x.AclGroup, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, Name: "acl_rule",
		KeyParts: []string{"group_id", "rule_num"},
		NewZero:  func() proto.Message { return &dash_acl_rule.AclRule{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_AclRule{AclRule: m.(*dash_acl_rule.AclRule)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_AclRule)
			if !ok || x == nil {
				return nil, false
			}
			return x.AclRule, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_IN, Name: "acl_in",
		KeyParts: []string{"eni", "stage"},
		NewZero:  func() proto.Message { return &dash_acl_in.AclIn{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_AclIn{AclIn: m.(*dash_acl_in.AclIn)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_AclIn)
			if !ok || x == nil {
				return nil, false
			}
			return x.AclIn, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, Name: "acl_out",
		KeyParts: []string{"eni", "stage"},
		NewZero:  func() proto.Message { return &dash_acl_out.AclOut{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_AclOut{AclOut: m.(*dash_acl_out.AclOut)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_AclOut)
			if !ok || x == nil {
				return nil, false
			}
			return x.AclOut, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE, Name: "route",
		KeyParts: []string{"group_id", "prefix"},
		NewZero:  func() proto.Message { return &dash_route.Route{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Route{Route: m.(*dash_route.Route)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Route)
			if !ok || x == nil {
				return nil, false
			}
			return x.Route, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP, Name: "route_group",
		KeyParts: []string{"group_id"},
		NewZero:  func() proto.Message { return &dash_route_group.RouteGroup{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_RouteGroup{RouteGroup: m.(*dash_route_group.RouteGroup)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_RouteGroup)
			if !ok || x == nil {
				return nil, false
			}
			return x.RouteGroup, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, Name: "route_rule",
		KeyParts: []string{"eni", "vni", "prefix_or_tag", "priority"},
		NewZero:  func() proto.Message { return &dash_route_rule.RouteRule{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_RouteRule{RouteRule: m.(*dash_route_rule.RouteRule)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_RouteRule)
			if !ok || x == nil {
				return nil, false
			}
			return x.RouteRule, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTE_TYPE, Name: "route_type",
		KeyParts: []string{"routing_type"},
		NewZero:  func() proto.Message { return &dash_route_type.RouteType{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_RouteType{RouteType: m.(*dash_route_type.RouteType)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_RouteType)
			if !ok || x == nil {
				return nil, false
			}
			return x.RouteType, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_ROUTING_APPLIANCE, Name: "routing_appliance",
		KeyParts: []string{"appliance_id"},
		NewZero:  func() proto.Message { return &dash_routing_appliance.RoutingAppliance{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_RoutingAppliance{RoutingAppliance: m.(*dash_routing_appliance.RoutingAppliance)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_RoutingAppliance)
			if !ok || x == nil {
				return nil, false
			}
			return x.RoutingAppliance, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_PREFIX_TAG, Name: "prefix_tag",
		KeyParts: []string{"tag_name"},
		NewZero:  func() proto.Message { return &dash_tag.PrefixTag{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_PrefixTag{PrefixTag: m.(*dash_tag.PrefixTag)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_PrefixTag)
			if !ok || x == nil {
				return nil, false
			}
			return x.PrefixTag, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, Name: "vnet_mapping",
		KeyParts: []string{"vnet", "ip_address"},
		NewZero:  func() proto.Message { return &dash_vnet_mapping.VnetMapping{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_VnetMapping{VnetMapping: m.(*dash_vnet_mapping.VnetMapping)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_VnetMapping)
			if !ok || x == nil {
				return nil, false
			}
			return x.VnetMapping, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_TUNNEL, Name: "tunnel",
		KeyParts: []string{"tunnel_name"},
		NewZero:  func() proto.Message { return &dash_tunnel.Tunnel{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Tunnel{Tunnel: m.(*dash_tunnel.Tunnel)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Tunnel)
			if !ok || x == nil {
				return nil, false
			}
			return x.Tunnel, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_PA_VALIDATION, Name: "pa_validation",
		KeyParts: []string{"vni"},
		NewZero:  func() proto.Message { return &dash_pa_validation.PaValidation{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_PaValidation{PaValidation: m.(*dash_pa_validation.PaValidation)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_PaValidation)
			if !ok || x == nil {
				return nil, false
			}
			return x.PaValidation, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_QOS, Name: "qos",
		KeyParts: []string{"qos_name"},
		NewZero:  func() proto.Message { return &dash_qos.Qos{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Qos{Qos: m.(*dash_qos.Qos)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Qos)
			if !ok || x == nil {
				return nil, false
			}
			return x.Qos, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_METER, Name: "meter",
		KeyParts: []string{"eni", "metering_class_id"},
		NewZero:  func() proto.Message { return &dash_meter_rule.Meter{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_Meter{Meter: m.(*dash_meter_rule.Meter)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_Meter)
			if !ok || x == nil {
				return nil, false
			}
			return x.Meter, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_METER_POLICY, Name: "meter_policy",
		KeyParts: []string{"meter_policy_id"},
		NewZero:  func() proto.Message { return &dash_meter_policy.MeterPolicy{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_MeterPolicy{MeterPolicy: m.(*dash_meter_policy.MeterPolicy)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_MeterPolicy)
			if !ok || x == nil {
				return nil, false
			}
			return x.MeterPolicy, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_METER_RULE, Name: "meter_rule",
		KeyParts: []string{"meter_policy_id", "rule_num"},
		NewZero:  func() proto.Message { return &dash_meter_rule.MeterRule{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_MeterRule{MeterRule: m.(*dash_meter_rule.MeterRule)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_MeterRule)
			if !ok || x == nil {
				return nil, false
			}
			return x.MeterRule, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP, Name: "outbound_port_map",
		KeyParts: []string{"map_id"},
		NewZero:  func() proto.Message { return &dash_outbound_port_map.OutboundPortMap{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_OutboundPortMap{OutboundPortMap: m.(*dash_outbound_port_map.OutboundPortMap)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_OutboundPortMap)
			if !ok || x == nil {
				return nil, false
			}
			return x.OutboundPortMap, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE, Name: "outbound_port_map_range",
		KeyParts: []string{"map_id", "start_port", "end_port"},
		NewZero:  func() proto.Message { return &dash_outbound_port_map_range.OutboundPortMapRange{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_OutboundPortMapRange{OutboundPortMapRange: m.(*dash_outbound_port_map_range.OutboundPortMapRange)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_OutboundPortMapRange)
			if !ok || x == nil {
				return nil, false
			}
			return x.OutboundPortMapRange, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE, Name: "ha_scope",
		KeyParts: []string{"ha_scope_id"},
		NewZero:  func() proto.Message { return &dash_ha_scope.HaScope{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_HaScope{HaScope: m.(*dash_ha_scope.HaScope)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_HaScope)
			if !ok || x == nil {
				return nil, false
			}
			return x.HaScope, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_CONFIG, Name: "ha_scope_config",
		KeyParts: []string{"vdpu_id", "ha_scope_id"},
		NewZero:  func() proto.Message { return &dash_ha_scope_config.HaScopeConfig{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_HaScopeConfig{HaScopeConfig: m.(*dash_ha_scope_config.HaScopeConfig)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_HaScopeConfig)
			if !ok || x == nil {
				return nil, false
			}
			return x.HaScopeConfig, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SCOPE_STATE, Name: "ha_scope_state",
		KeyParts: []string{"ha_scope_id"},
		NewZero:  func() proto.Message { return &dash_ha_scope_state.HaScopeState{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_HaScopeState{HaScopeState: m.(*dash_ha_scope_state.HaScopeState)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_HaScopeState)
			if !ok || x == nil {
				return nil, false
			}
			return x.HaScopeState, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SET, Name: "ha_set",
		KeyParts: []string{"ha_set_id"},
		NewZero:  func() proto.Message { return &dash_ha_set.HaSet{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_HaSet{HaSet: m.(*dash_ha_set.HaSet)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_HaSet)
			if !ok || x == nil {
				return nil, false
			}
			return x.HaSet, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SET_CONFIG, Name: "ha_set_config",
		KeyParts: []string{"ha_set_id"},
		NewZero:  func() proto.Message { return &dash_ha_set_config.HaSetConfig{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_HaSetConfig{HaSetConfig: m.(*dash_ha_set_config.HaSetConfig)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_HaSetConfig)
			if !ok || x == nil {
				return nil, false
			}
			return x.HaSetConfig, true
		},
	},
	{
		Kind: dashapi.ObjectKind_OBJECT_KIND_HA_SET_STATE, Name: "ha_set_state",
		KeyParts: []string{"ha_set_id"},
		NewZero:  func() proto.Message { return &dash_ha_set_state.HaSetState{} },
		Pack: func(o *dashapi.Object, m proto.Message) {
			o.Payload = &dashapi.Object_HaSetState{HaSetState: m.(*dash_ha_set_state.HaSetState)}
		},
		Unpack: func(o *dashapi.Object) (proto.Message, bool) {
			x, ok := o.Payload.(*dashapi.Object_HaSetState)
			if !ok || x == nil {
				return nil, false
			}
			return x.HaSetState, true
		},
	},
}

var byKind = func() map[dashapi.ObjectKind]Info {
	m := make(map[dashapi.ObjectKind]Info, len(All))
	for _, info := range All {
		m[info.Kind] = info
	}
	return m
}()

var byName = func() map[string]Info {
	m := make(map[string]Info, len(All))
	for _, info := range All {
		m[info.Name] = info
	}
	return m
}()

// Lookup returns the Info for the given kind.
func Lookup(kind dashapi.ObjectKind) (Info, error) {
	info, ok := byKind[kind]
	if !ok {
		return Info{}, fmt.Errorf("kinds: unknown ObjectKind %v", kind)
	}
	return info, nil
}

// LookupByName returns the Info for the given short name (e.g. "vnet_mapping").
func LookupByName(name string) (Info, error) {
	info, ok := byName[name]
	if !ok {
		return Info{}, fmt.Errorf("kinds: unknown name %q", name)
	}
	return info, nil
}

// Names returns every registered short name, in enum order.
func Names() []string {
	out := make([]string, 0, len(All))
	for _, info := range All {
		out = append(out, info.Name)
	}
	return out
}

// PayloadOf returns the typed proto message carried by o.
func PayloadOf(o *dashapi.Object) (proto.Message, error) {
	if o == nil {
		return nil, fmt.Errorf("kinds: nil Object")
	}
	info, err := Lookup(o.GetKind())
	if err != nil {
		return nil, err
	}
	m, ok := info.Unpack(o)
	if !ok {
		return nil, fmt.Errorf("kinds: Object kind=%v has no payload", o.GetKind())
	}
	return m, nil
}

// WrapObject constructs an *Object with kind+key+typed payload.
func WrapObject(kind dashapi.ObjectKind, key []string, m proto.Message) (*dashapi.Object, error) {
	info, err := Lookup(kind)
	if err != nil {
		return nil, err
	}
	o := &dashapi.Object{Kind: kind, Key: append([]string(nil), key...)}
	info.Pack(o, m)
	return o, nil
}
