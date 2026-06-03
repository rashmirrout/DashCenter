// Package pipeline is the behavioural DASH packet processor.
//
// It walks the upstream DASH pipeline as documented at
// https://github.com/sonic-net/DASH:
//
//   Outbound (VM -> network):
//     1. ENI lookup by Packet.eni (must exist, admin_state must be ENABLED).
//     2. ACL_OUT stage 1..5: for each stage, look up acl_out keyed by
//        (eni, stage); load the bound acl_group; evaluate acl_rule rows in
//        priority order (lower = higher prio). First match decides
//        ALLOW/DENY; if terminating, stop the stage; else continue. A DENY
//        terminates the pipeline with DROP.
//     3. Route lookup: eni_route.group_id -> route table rows for that
//        group, longest-prefix-match on dst_ip.
//     4. Route action:
//          - DROP                  -> DROP
//          - DIRECT                -> FORWARD as-is
//          - VNET / VNET_DIRECT    -> vnet_mapping lookup by (route.vnet,
//                                     dst_ip) gives underlay_ip + VNI ->
//                                     ENCAP.
//          - SERVICETUNNEL         -> ENCAP using route.service_tunnel.
//          - APPLIANCE             -> ENCAP via routing_appliance.
//
//   Inbound (network -> VM):
//     1. ENI lookup by destination MAC. If caller supplies eni, prefer it.
//     2. route_rule lookup keyed by (eni, vni, src_ip prefix or tag).
//        Matches in priority order.
//     3. action_type: DECAP/MAPDECAP -> continue; DROP -> DROP.
//     4. ACL_IN stage 1..5: same shape as ACL_OUT.
//     5. FORWARD to the matched ENI.
//
// All decisions also increment counters: the matched ENI sees a packet
// in/out tick; on ENCAP, the vnet_mapping key gets one too.
package pipeline

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_group"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_acl_out "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_out"
	dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_eni_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni_route"
	dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
	dash_route_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_rule"
	dash_routing_appliance "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/routing_appliance"
	dash_route_type "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_type"
	dash_types "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/types"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"google.golang.org/protobuf/proto"
)

// Engine runs SimulatePacket against a Store.
type Engine struct {
	Store    *model.Store
	Counters *counters.Registry
}

// New returns an Engine wired to a store + counter registry.
func New(store *model.Store, ctrs *counters.Registry) *Engine {
	return &Engine{Store: store, Counters: ctrs}
}

// Evaluate runs one packet through the pipeline and returns the Decision.
// On any non-DROP outcome it also ticks counters for the matched ENI and,
// for ENCAP outcomes, the matched vnet_mapping.
func (e *Engine) Evaluate(pkt *dashapi.Packet, trace bool) *dashapi.Decision {
	d := &dashapi.Decision{}
	tr := traceCtx{enabled: trace, out: &d.Trace}
	if pkt == nil {
		return drop(d, tr, "nil packet")
	}

	tr.log("input dir=%s eni=%q vni=%d src=%s:%d->%s:%d proto=%d len=%d",
		pkt.GetDirection(), pkt.GetEni(), pkt.GetVni(),
		pkt.GetSrcIp(), pkt.GetSrcPort(), pkt.GetDstIp(), pkt.GetDstPort(),
		pkt.GetProtocol(), pkt.GetLengthBytes())

	switch pkt.GetDirection() {
	case dashapi.Packet_DIRECTION_OUTBOUND:
		return e.outbound(pkt, d, tr)
	case dashapi.Packet_DIRECTION_INBOUND:
		return e.inbound(pkt, d, tr)
	default:
		return drop(d, tr, "direction unspecified")
	}
}

// -----------------------------------------------------------------------------
// Outbound
// -----------------------------------------------------------------------------

func (e *Engine) outbound(pkt *dashapi.Packet, d *dashapi.Decision, tr traceCtx) *dashapi.Decision {
	eniKey := pkt.GetEni()
	if eniKey == "" {
		return drop(d, tr, "outbound: packet.eni is required")
	}
	eni, err := loadEni(e.Store, eniKey)
	if err != nil {
		return drop(d, tr, fmt.Sprintf("eni %q: %v", eniKey, err))
	}
	if eni.GetAdminState() != dash_eni.State_STATE_ENABLED {
		return drop(d, tr, fmt.Sprintf("eni %q admin_state=%s", eniKey, eni.GetAdminState()))
	}
	tr.log("outbound: eni=%q admin_state=ENABLED vnet=%s", eniKey, eni.GetVnet())

	// ACL_OUT 1..5
	if blocked, stage, prio := e.evalACL(pkt, eniKey, false, tr); blocked {
		d.MatchedAclStage = stage
		d.MatchedAclPriority = prio
		return drop(d, tr, fmt.Sprintf("acl_out stage=%d priority=%d deny", stage, prio))
	}

	// Route lookup via eni_route -> route_group -> route prefix match.
	groupID, err := loadEniRouteGroup(e.Store, eniKey)
	if err != nil {
		return drop(d, tr, fmt.Sprintf("eni_route %q: %v", eniKey, err))
	}
	tr.log("outbound: route_group=%q", groupID)
	route, prefix, err := e.lookupRoute(groupID, pkt.GetDstIp(), tr)
	if err != nil {
		return drop(d, tr, fmt.Sprintf("route lookup: %v", err))
	}
	d.MatchedRoutePrefix = prefix
	d.OutRoutingType = strings.TrimPrefix(route.GetRoutingType().String(), "ROUTING_TYPE_")

	// Apply route action.
	switch route.GetRoutingType() {
	case dash_route_type.RoutingType_ROUTING_TYPE_DROP:
		return drop(d, tr, "route action=DROP")
	case dash_route_type.RoutingType_ROUTING_TYPE_DIRECT:
		return forward(d, tr, eniKey, "direct")
	case dash_route_type.RoutingType_ROUTING_TYPE_VNET, dash_route_type.RoutingType_ROUTING_TYPE_VNET_DIRECT, dash_route_type.RoutingType_ROUTING_TYPE_VNET_ENCAP:
		vnetName := routeVnet(route)
		if vnetName == "" {
			return drop(d, tr, "route routing_type=VNET but no vnet name in payload")
		}
		mapping, err := e.lookupVnetMapping(vnetName, pkt.GetDstIp(), tr)
		if err != nil {
			return drop(d, tr, fmt.Sprintf("vnet_mapping %s/%s: %v", vnetName, pkt.GetDstIp(), err))
		}
		underlay := ipAddressString(mapping.GetUnderlayIp())
		vni := vnetVNI(e.Store, vnetName, mapping.GetUseDstVni())
		e.tickEncap(eniKey, vnetName, pkt.GetDstIp(), pkt.GetLengthBytes())
		return encap(d, tr, eniKey, underlay, vni)
	case dash_route_type.RoutingType_ROUTING_TYPE_SERVICETUNNEL:
		st := route.GetServiceTunnel()
		if st == nil {
			return drop(d, tr, "route routing_type=SERVICETUNNEL but no service_tunnel payload")
		}
		underlay := ipAddressString(st.GetUnderlayDip())
		e.tickEncap(eniKey, "service-tunnel", pkt.GetDstIp(), pkt.GetLengthBytes())
		return encap(d, tr, eniKey, underlay, 0)
	case dash_route_type.RoutingType_ROUTING_TYPE_APPLIANCE:
		ap := route.GetAppliance()
		if ap == "" {
			return drop(d, tr, "route routing_type=APPLIANCE but no appliance ref")
		}
		appl, err := loadRoutingAppliance(e.Store, ap)
		if err != nil {
			return drop(d, tr, fmt.Sprintf("routing_appliance %q: %v", ap, err))
		}
		underlay := ""
		if len(appl.GetAddresses()) > 0 {
			underlay = ipAddressString(appl.GetAddresses()[0])
		}
		e.tickEncap(eniKey, "appliance:"+ap, pkt.GetDstIp(), pkt.GetLengthBytes())
		return encap(d, tr, eniKey, underlay, appl.GetVni())
	default:
		return drop(d, tr, fmt.Sprintf("route routing_type=%s unsupported", route.GetRoutingType()))
	}
}

// -----------------------------------------------------------------------------
// Inbound
// -----------------------------------------------------------------------------

func (e *Engine) inbound(pkt *dashapi.Packet, d *dashapi.Decision, tr traceCtx) *dashapi.Decision {
	eniKey := pkt.GetEni()
	if eniKey == "" {
		var err error
		eniKey, err = e.resolveEniByMac(pkt.GetDstMac())
		if err != nil {
			return drop(d, tr, fmt.Sprintf("inbound: ENI lookup by dst_mac=%q: %v", pkt.GetDstMac(), err))
		}
		tr.log("inbound: resolved eni=%q via dst_mac=%s", eniKey, pkt.GetDstMac())
	}
	eni, err := loadEni(e.Store, eniKey)
	if err != nil {
		return drop(d, tr, fmt.Sprintf("eni %q: %v", eniKey, err))
	}
	if eni.GetAdminState() != dash_eni.State_STATE_ENABLED {
		return drop(d, tr, fmt.Sprintf("eni %q admin_state=%s", eniKey, eni.GetAdminState()))
	}

	// route_rule lookup.
	rule, err := e.lookupRouteRule(eniKey, pkt.GetVni(), pkt.GetSrcIp(), tr)
	if err != nil {
		return drop(d, tr, fmt.Sprintf("route_rule (eni=%s vni=%d): %v", eniKey, pkt.GetVni(), err))
	}
	tr.log("inbound: matched route_rule action_type=%s", rule.GetActionType())
	if rule.GetActionType() == dash_route_type.ActionType_ACTION_TYPE_DROP {
		return drop(d, tr, "route_rule action_type=DROP")
	}

	// ACL_IN 1..5
	if blocked, stage, prio := e.evalACL(pkt, eniKey, true, tr); blocked {
		d.MatchedAclStage = stage
		d.MatchedAclPriority = prio
		return drop(d, tr, fmt.Sprintf("acl_in stage=%d priority=%d deny", stage, prio))
	}

	return forward(d, tr, eniKey, "inbound deliver")
}

// -----------------------------------------------------------------------------
// ACL evaluation (shared between in/out)
// -----------------------------------------------------------------------------

// evalACL returns blocked, stage, priority.
func (e *Engine) evalACL(pkt *dashapi.Packet, eniKey string, inbound bool, tr traceCtx) (bool, uint32, uint32) {
	for stage := uint32(1); stage <= 5; stage++ {
		groupID := e.aclGroupForStage(eniKey, stage, isIPv4(pkt.GetSrcIp(), pkt.GetDstIp()), inbound)
		if groupID == "" {
			continue
		}
		tr.log("acl stage=%d group=%q", stage, groupID)
		_, err := loadAclGroup(e.Store, groupID)
		if err != nil {
			tr.log("  acl_group %q missing; skip", groupID)
			continue
		}
		rules, err := e.aclRulesFor(groupID)
		if err != nil || len(rules) == 0 {
			tr.log("  no acl_rules")
			continue
		}
		for _, r := range rules {
			if !aclRuleMatches(r.rule, pkt) {
				continue
			}
			tr.log("  matched rule priority=%d action=%s terminating=%v",
				r.rule.GetPriority(), r.rule.GetAction(), r.rule.GetTerminating())
			if r.rule.GetAction() == dash_acl_rule.Action_ACTION_DENY {
				return true, stage, r.rule.GetPriority()
			}
			if r.rule.GetTerminating() {
				return false, stage, r.rule.GetPriority()
			}
		}
	}
	return false, 0, 0
}

func (e *Engine) aclGroupForStage(eniKey string, stage uint32, isV4, inbound bool) string {
	kind := dashapi.ObjectKind_OBJECT_KIND_ACL_OUT
	if inbound {
		kind = dashapi.ObjectKind_OBJECT_KIND_ACL_IN
	}
	obj, err := e.Store.Get(kind, []string{eniKey, fmt.Sprintf("%d", stage)})
	if err != nil {
		return ""
	}
	payload, _ := kinds.PayloadOf(obj)
	if inbound {
		bind := payload.(*dash_acl_in.AclIn)
		if isV4 {
			return bind.GetV4AclGroupId()
		}
		return bind.GetV6AclGroupId()
	}
	bind := payload.(*dash_acl_out.AclOut)
	if isV4 {
		return bind.GetV4AclGroupId()
	}
	return bind.GetV6AclGroupId()
}

type aclRuleRow struct {
	num  uint32
	rule *dash_acl_rule.AclRule
}

func (e *Engine) aclRulesFor(groupID string) ([]aclRuleRow, error) {
	items, err := e.Store.List(dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, groupID+":")
	if err != nil {
		return nil, err
	}
	var out []aclRuleRow
	for _, obj := range items {
		if len(obj.GetKey()) < 2 || obj.GetKey()[0] != groupID {
			continue
		}
		payload, _ := kinds.PayloadOf(obj)
		rule := payload.(*dash_acl_rule.AclRule)
		num := parseUint32(obj.GetKey()[1])
		out = append(out, aclRuleRow{num: num, rule: rule})
	}
	sort.Slice(out, func(i, j int) bool {
		// Lower priority value = higher precedence per upstream comment.
		if out[i].rule.GetPriority() != out[j].rule.GetPriority() {
			return out[i].rule.GetPriority() < out[j].rule.GetPriority()
		}
		return out[i].num < out[j].num
	})
	return out, nil
}

// aclRuleMatches evaluates 5-tuple matching. Empty repeated fields = match all.
func aclRuleMatches(r *dash_acl_rule.AclRule, pkt *dashapi.Packet) bool {
	// Protocol
	if len(r.GetProtocol()) > 0 {
		hit := false
		for _, p := range r.GetProtocol() {
			if p == pkt.GetProtocol() {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	// Src/Dst IP prefix lists
	if len(r.GetSrcAddr()) > 0 && !anyPrefixMatches(r.GetSrcAddr(), pkt.GetSrcIp()) {
		return false
	}
	if len(r.GetDstAddr()) > 0 && !anyPrefixMatches(r.GetDstAddr(), pkt.GetDstIp()) {
		return false
	}
	if len(r.GetSrcPort()) > 0 && !anyPortMatches(r.GetSrcPort(), pkt.GetSrcPort()) {
		return false
	}
	if len(r.GetDstPort()) > 0 && !anyPortMatches(r.GetDstPort(), pkt.GetDstPort()) {
		return false
	}
	return true
}

// -----------------------------------------------------------------------------
// Route lookups
// -----------------------------------------------------------------------------

func loadEniRouteGroup(store *model.Store, eniKey string) (string, error) {
	obj, err := store.Get(dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, []string{eniKey})
	if err != nil {
		return "", err
	}
	payload, _ := kinds.PayloadOf(obj)
	return payload.(*dash_eni_route.EniRoute).GetGroupId(), nil
}

func (e *Engine) lookupRoute(groupID, dstIP string, tr traceCtx) (*dash_route.Route, string, error) {
	items, err := e.Store.List(dashapi.ObjectKind_OBJECT_KIND_ROUTE, groupID+":")
	if err != nil {
		return nil, "", err
	}
	dst, err := netip.ParseAddr(dstIP)
	if err != nil {
		return nil, "", fmt.Errorf("invalid dst_ip %q: %w", dstIP, err)
	}

	type cand struct {
		bits   int
		prefix string
		route  *dash_route.Route
	}
	var matches []cand
	for _, obj := range items {
		if len(obj.GetKey()) < 2 || obj.GetKey()[0] != groupID {
			continue
		}
		prefixStr := obj.GetKey()[1]
		pfx, err := netip.ParsePrefix(prefixStr)
		if err != nil {
			tr.log("  route key=%s invalid prefix; skip", prefixStr)
			continue
		}
		if !pfx.Contains(dst) {
			continue
		}
		payload, _ := kinds.PayloadOf(obj)
		matches = append(matches, cand{bits: pfx.Bits(), prefix: prefixStr, route: payload.(*dash_route.Route)})
	}
	if len(matches) == 0 {
		return nil, "", fmt.Errorf("no route matches dst=%s in group=%s", dstIP, groupID)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].bits > matches[j].bits })
	tr.log("route LPM dst=%s -> prefix=%s", dstIP, matches[0].prefix)
	return matches[0].route, matches[0].prefix, nil
}

func (e *Engine) lookupRouteRule(eniKey string, vni uint32, srcIP string, tr traceCtx) (*dash_route_rule.RouteRule, error) {
	prefix := fmt.Sprintf("%s:%d:", eniKey, vni)
	items, err := e.Store.List(dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, prefix)
	if err != nil {
		return nil, err
	}
	src, err := netip.ParseAddr(srcIP)
	srcValid := err == nil

	type cand struct {
		prio uint32
		rule *dash_route_rule.RouteRule
	}
	var matches []cand
	for _, obj := range items {
		key := obj.GetKey()
		if len(key) < 4 {
			continue
		}
		if key[0] != eniKey || parseUint32(key[1]) != vni {
			continue
		}
		// key[2] is prefix or tag; we match prefix only here.
		pfxStr := key[2]
		if srcValid && strings.Contains(pfxStr, "/") {
			pfx, err := netip.ParsePrefix(pfxStr)
			if err != nil || !pfx.Contains(src) {
				continue
			}
		}
		payload, _ := kinds.PayloadOf(obj)
		matches = append(matches, cand{prio: parseUint32(key[3]), rule: payload.(*dash_route_rule.RouteRule)})
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no route_rule matches")
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].prio < matches[j].prio })
	tr.log("route_rule matched priority=%d", matches[0].prio)
	return matches[0].rule, nil
}

func (e *Engine) lookupVnetMapping(vnet, ip string, tr traceCtx) (*dash_vnet_mapping.VnetMapping, error) {
	obj, err := e.Store.Get(dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{vnet, ip})
	if err != nil {
		return nil, err
	}
	tr.log("vnet_mapping hit vnet=%s ip=%s", vnet, ip)
	payload, _ := kinds.PayloadOf(obj)
	return payload.(*dash_vnet_mapping.VnetMapping), nil
}

// -----------------------------------------------------------------------------
// Loaders / helpers
// -----------------------------------------------------------------------------

func loadEni(store *model.Store, eniKey string) (*dash_eni.Eni, error) {
	obj, err := store.Get(dashapi.ObjectKind_OBJECT_KIND_ENI, []string{eniKey})
	if err != nil {
		return nil, err
	}
	payload, _ := kinds.PayloadOf(obj)
	return payload.(*dash_eni.Eni), nil
}

func loadAclGroup(store *model.Store, groupID string) (*dash_acl_group.AclGroup, error) {
	obj, err := store.Get(dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, []string{groupID})
	if err != nil {
		return nil, err
	}
	payload, _ := kinds.PayloadOf(obj)
	return payload.(*dash_acl_group.AclGroup), nil
}

func loadRoutingAppliance(store *model.Store, id string) (*dash_routing_appliance.RoutingAppliance, error) {
	obj, err := store.Get(dashapi.ObjectKind_OBJECT_KIND_ROUTING_APPLIANCE, []string{id})
	if err != nil {
		return nil, err
	}
	payload, _ := kinds.PayloadOf(obj)
	return payload.(*dash_routing_appliance.RoutingAppliance), nil
}

// resolveEniByMac scans all ENIs for one with matching mac_address (string
// formatted "aa:bb:cc:dd:ee:ff").
func (e *Engine) resolveEniByMac(macStr string) (string, error) {
	items, err := e.Store.List(dashapi.ObjectKind_OBJECT_KIND_ENI, "")
	if err != nil {
		return "", err
	}
	want := strings.ToLower(strings.TrimSpace(macStr))
	for _, obj := range items {
		payload, _ := kinds.PayloadOf(obj)
		eni := payload.(*dash_eni.Eni)
		have := formatMac(eni.GetMacAddress())
		if strings.EqualFold(have, want) {
			return obj.GetKey()[0], nil
		}
	}
	return "", fmt.Errorf("no eni with mac=%s", want)
}

func routeVnet(r *dash_route.Route) string {
	if v := r.GetVnet(); v != "" {
		return v
	}
	if vd := r.GetVnetDirect(); vd != nil {
		return vd.GetVnet()
	}
	return ""
}

func vnetVNI(_ *model.Store, _ string, useDst bool) uint32 {
	// Without source ENI vnet context here we just propagate dst-vnet hint;
	// a real DPU resolves the actual VNI. Returns 0 (caller decides default).
	_ = useDst
	return 0
}

func ipAddressString(a *dash_types.IpAddress) string {
	if a == nil {
		return ""
	}
	if v6 := a.GetIpv6(); len(v6) == 16 {
		addr, _ := netip.AddrFromSlice(v6)
		return addr.String()
	}
	v4 := a.GetIpv4()
	if v4 == 0 {
		return ""
	}
	// fixed32 in network byte order.
	b := [4]byte{byte(v4), byte(v4 >> 8), byte(v4 >> 16), byte(v4 >> 24)}
	return netip.AddrFrom4(b).String()
}

func formatMac(b []byte) string {
	if len(b) != 6 {
		return ""
	}
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

func isIPv4(strs ...string) bool {
	for _, s := range strs {
		if s == "" {
			continue
		}
		addr, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		return addr.Is4() || addr.Is4In6()
	}
	return true // default v4
}

func anyPrefixMatches(prefixes []*dash_types.IpPrefix, ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	for _, p := range prefixes {
		// upstream IpPrefix is {ip, mask}. Build a netip.Prefix.
		ipPart := ipAddressString(p.GetIp())
		maskPart := ipAddressString(p.GetMask())
		bits := maskToBits(maskPart, addr.Is4())
		base, err := netip.ParseAddr(ipPart)
		if err != nil {
			continue
		}
		px := netip.PrefixFrom(base, bits)
		if px.Contains(addr) {
			return true
		}
	}
	return false
}

func maskToBits(mask string, isV4 bool) int {
	addr, err := netip.ParseAddr(mask)
	if err != nil {
		if isV4 {
			return 32
		}
		return 128
	}
	if isV4 {
		bs := addr.As4()
		n := 0
		for _, b := range bs {
			for i := 7; i >= 0; i-- {
				if b&(1<<i) != 0 {
					n++
				} else {
					return n
				}
			}
		}
		return n
	}
	bs := addr.As16()
	n := 0
	for _, b := range bs {
		for i := 7; i >= 0; i-- {
			if b&(1<<i) != 0 {
				n++
			} else {
				return n
			}
		}
	}
	return n
}

func anyPortMatches(ranges []*dash_types.ValueOrRange, port uint32) bool {
	for _, r := range ranges {
		if v := r.GetValue(); v != 0 && v == port {
			return true
		}
		if rng := r.GetRange(); rng != nil {
			if port >= rng.GetMin() && port <= rng.GetMax() {
				return true
			}
		}
	}
	return false
}

func parseUint32(s string) uint32 {
	var n uint32
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + uint32(c-'0')
	}
	return n
}

// -----------------------------------------------------------------------------
// trace + decision constructors
// -----------------------------------------------------------------------------

type traceCtx struct {
	enabled bool
	out     *[]string
}

func (t traceCtx) log(format string, args ...interface{}) {
	if !t.enabled {
		return
	}
	*t.out = append(*t.out, fmt.Sprintf(format, args...))
}

func drop(d *dashapi.Decision, tr traceCtx, reason string) *dashapi.Decision {
	d.Action = dashapi.Decision_ACTION_DROP
	d.Reason = reason
	tr.log("DROP: %s", reason)
	return d
}

func forward(d *dashapi.Decision, tr traceCtx, eni, reason string) *dashapi.Decision {
	d.Action = dashapi.Decision_ACTION_FORWARD
	d.OutEni = eni
	if d.Reason == "" {
		d.Reason = reason
	}
	tr.log("FORWARD eni=%s (%s)", eni, reason)
	return d
}

func encap(d *dashapi.Decision, tr traceCtx, eni, underlay string, vni uint32) *dashapi.Decision {
	d.Action = dashapi.Decision_ACTION_ENCAP
	d.OutEni = eni
	d.OutUnderlayIp = underlay
	d.OutVni = vni
	if d.Reason == "" {
		d.Reason = "encap"
	}
	tr.log("ENCAP eni=%s underlay=%s vni=%d", eni, underlay, vni)
	return d
}

// tickEncap increments counters for both the ENI and the vnet_mapping target.
func (e *Engine) tickEncap(eniKey, vnet, dstIP string, _ uint32) {
	if e.Counters == nil {
		return
	}
	e.Counters.Tick(eniKey)
	if vnet != "" && dstIP != "" {
		e.Counters.Tick(vnet + ":" + dstIP)
	}
}

// Compile-time sanity.
var _ proto.Message = (*dashapi.Packet)(nil)
