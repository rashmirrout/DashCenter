package placement

import (
"encoding/binary"
"fmt"
"net"
"strconv"

dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
dash_acl_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_group"
dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
dash_ha_set "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set"
dash_ha_set_config "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/ha_set_config"
dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
dash_route_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_group"
dash_types "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/types"
dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
)

// parseIpAddress converts a string IP (e.g. "10.0.0.1") to a dashapi IpAddress.
// Returns nil if the string is empty.
func parseIpAddress(s string) *dash_types.IpAddress {
	if s == "" {
		return nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil
	}
if v4 := ip.To4(); v4 != nil {
return &dash_types.IpAddress{
Ip: &dash_types.IpAddress_Ipv4{Ipv4: binary.BigEndian.Uint32(v4)},
}
}
return &dash_types.IpAddress{
Ip: &dash_types.IpAddress_Ipv6{Ipv6: []byte(ip.To16())},
}
}

// parseAdminState maps a string admin state to the dash ENI state enum.
func parseAdminState(s string) dash_eni.State {
	switch s {
	case "enabled", "ENABLED", "STATE_ENABLED":
		return dash_eni.State_STATE_ENABLED
	case "disabled", "DISABLED", "STATE_DISABLED":
		return dash_eni.State_STATE_DISABLED
	default:
		return dash_eni.State_STATE_UNSPECIFIED
	}
}

// parseMac converts a MAC address string to bytes.
// Returns nil if the string is empty or unparseable.
func parseMac(s string) []byte {
	if s == "" {
		return nil
	}
	hw, err := net.ParseMAC(s)
	if err != nil {
		// Fall back to raw bytes.
		return []byte(s)
	}
	return []byte(hw)
}

// TranslateVnet converts a VnetSpec to a dashapi Vnet object.
func TranslateVnet(name string, s *dashcenterv1.VnetSpec) (*dashapiv1.Object, error) {
	if name == "" {
		return nil, fmt.Errorf("translate vnet: missing name")
	}
	payload := &dash_vnet.Vnet{
		Vni: s.GetVni(),
	}
	return kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_VNET, []string{name}, payload)
}

// TranslateEni converts an EniSpec to 1 or 2 dashapi objects (Eni + optional EniRoute).
func TranslateEni(name string, s *dashcenterv1.EniSpec) ([]*dashapiv1.Object, error) {
	if s.MacAddress == "" {
		return nil, fmt.Errorf("translate eni: missing mac_address")
	}
	if name == "" {
		return nil, fmt.Errorf("translate eni: missing name")
	}

	eniPayload := &dash_eni.Eni{
		MacAddress: parseMac(s.MacAddress),
		Vnet:       s.VnetName,
		UnderlayIp: parseIpAddress(s.UnderlayIp),
		AdminState: parseAdminState(s.AdminState),
	}
	eniObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_ENI, []string{name}, eniPayload)
	if err != nil {
		return nil, err
	}

	result := []*dashapiv1.Object{eniObj}

	// If the ENI references route groups, emit an EniRoute.
	// Phase 1: simplified — we don't have explicit RouteGroupRefs on EniSpec,
	// but the plan indicates we should support this when present.
	// For now, return just the ENI object.

	return result, nil
}

// TranslateVnetMapping converts a VnetMappingSpec to a dashapi VnetMapping.
func TranslateVnetMapping(name string, s *dashcenterv1.VnetMappingSpec) (*dashapiv1.Object, error) {
	if s.VnetName == "" {
		return nil, fmt.Errorf("translate vnet_mapping: missing vnet_name")
	}
	if s.IpAddress == "" {
		return nil, fmt.Errorf("translate vnet_mapping: missing ip_address")
	}
	payload := &dash_vnet_mapping.VnetMapping{
		MacAddress: parseMac(s.MacAddress),
		UnderlayIp: parseIpAddress(s.UnderlayIp),
	}
	key := []string{s.VnetName, s.IpAddress}
	return kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, key, payload)
}

// TranslateAclPolicy converts an AclPolicySpec to 1+N dashapi objects
// (AclGroup + N AclRules).
func TranslateAclPolicy(name string, s *dashcenterv1.AclPolicySpec) ([]*dashapiv1.Object, error) {
	if name == "" {
		return nil, fmt.Errorf("translate acl_policy: missing name")
	}

	groupPayload := &dash_acl_group.AclGroup{}
	groupObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_ACL_GROUP, []string{name}, groupPayload)
	if err != nil {
		return nil, err
	}

	result := []*dashapiv1.Object{groupObj}

	for _, rule := range s.Rules {
		rulePayload := &dash_acl_rule.AclRule{
			Priority: rule.Priority,
		}
		ruleKey := []string{name, strconv.Itoa(int(rule.Priority))}
		ruleObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_ACL_RULE, ruleKey, rulePayload)
		if err != nil {
			return nil, err
		}
		result = append(result, ruleObj)
	}

	return result, nil
}

// TranslateRoutePolicy converts a RoutePolicySpec to 1+N dashapi objects
// (RouteGroup + N Routes).
func TranslateRoutePolicy(name string, s *dashcenterv1.RoutePolicySpec) ([]*dashapiv1.Object, error) {
	if name == "" {
		return nil, fmt.Errorf("translate route_policy: missing name")
	}

	groupPayload := &dash_route_group.RouteGroup{}
	groupObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_ROUTE_GROUP, []string{name}, groupPayload)
	if err != nil {
		return nil, err
	}

	result := []*dashapiv1.Object{groupObj}

	for _, r := range s.Routes {
		routePayload := &dash_route.Route{}
		routeKey := []string{name, r.Prefix}
		routeObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_ROUTE, routeKey, routePayload)
		if err != nil {
			return nil, err
		}
		result = append(result, routeObj)
	}

	return result, nil
}

// TranslateHaSet converts an HaSetSpec to 2 dashapi objects (HaSet + HaSetConfig).
func TranslateHaSet(name string, s *dashcenterv1.HaSetSpec) ([]*dashapiv1.Object, error) {
	if name == "" {
		return nil, fmt.Errorf("translate ha_set: missing name")
	}

	haSetPayload := &dash_ha_set.HaSet{
		LocalIp: parseIpAddress(s.VirtualIp),
	}
	haSetObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_HA_SET, []string{name}, haSetPayload)
	if err != nil {
		return nil, err
	}

	haConfigPayload := &dash_ha_set_config.HaSetConfig{}
	haConfigObj, err := kinds.WrapObject(dashapiv1.ObjectKind_OBJECT_KIND_HA_SET_CONFIG, []string{name}, haConfigPayload)
	if err != nil {
		return nil, err
	}

	return []*dashapiv1.Object{haSetObj, haConfigObj}, nil
}