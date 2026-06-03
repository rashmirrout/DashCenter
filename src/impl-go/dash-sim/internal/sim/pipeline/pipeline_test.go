package pipeline_test

import (
	"testing"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dash_acl_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_group"
	dash_acl_in "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_in"
	dash_acl_out "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_out"
	dash_acl_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/acl_rule"
	dash_eni "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni"
	dash_eni_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/eni_route"
	dash_route "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route"
	dash_route_group "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_group"
	dash_route_rule "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_rule"
	dash_route_type "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/route_type"
	dash_types "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/types"
	dash_vnet "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet"
	dash_vnet_mapping "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dash/vnet_mapping"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/pipeline"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"google.golang.org/protobuf/proto"
)

// ipv4ToFixed32 packs a.b.c.d into the network-byte-order uint32 expected by
// the upstream IpAddress message.
func ipv4ToFixed32(a, b, c, d byte) uint32 {
	return uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24
}

func ipAddr(a, b, c, d byte) *dash_types.IpAddress {
	return &dash_types.IpAddress{Ip: &dash_types.IpAddress_Ipv4{Ipv4: ipv4ToFixed32(a, b, c, d)}}
}

func ipPrefix(a, b, c, d byte, bits int) *dash_types.IpPrefix {
	mask := ^uint32(0) << (32 - bits)
	maskBytes := [4]byte{byte(mask >> 24), byte(mask >> 16), byte(mask >> 8), byte(mask)}
	return &dash_types.IpPrefix{
		Ip:   ipAddr(a, b, c, d),
		Mask: &dash_types.IpAddress{Ip: &dash_types.IpAddress_Ipv4{Ipv4: uint32(maskBytes[0]) | uint32(maskBytes[1])<<8 | uint32(maskBytes[2])<<16 | uint32(maskBytes[3])<<24}},
	}
}

func mustApply(t *testing.T, store *model.Store, kind dashapi.ObjectKind, key []string, msg proto.Message) {
	t.Helper()
	obj, err := kinds.WrapObject(kind, key, msg)
	if err != nil {
		t.Fatalf("wrap %v %v: %v", kind, key, err)
	}
	if _, _, err := store.Apply(obj); err != nil {
		t.Fatalf("apply %v %v: %v", kind, key, err)
	}
}

func newFixture(t *testing.T) (*pipeline.Engine, *model.Store) {
	t.Helper()
	bus := events.New()
	store := model.New(bus)
	ctrs := counters.New()
	eng := pipeline.New(store, ctrs)

	// VNETs
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-prod"},
		&dash_vnet.Vnet{Vni: 1001})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_VNET, []string{"vnet-stage"},
		&dash_vnet.Vnet{Vni: 1002})

	// ENI eni-001 (enabled), eni-002 (disabled to test admin_state)
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-001"},
		&dash_eni.Eni{
			EniId:      "11111111-1111-1111-1111-111111111111",
			MacAddress: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55},
			Vnet:       "vnet-prod",
			AdminState: dash_eni.State_STATE_ENABLED,
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ENI, []string{"eni-002"},
		&dash_eni.Eni{
			EniId:      "22222222-2222-2222-2222-222222222222",
			MacAddress: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x66},
			Vnet:       "vnet-prod",
			AdminState: dash_eni.State_STATE_DISABLED,
		})

	// ACL groups + rules — outbound deny 10.99.0.0/16, permit-all otherwise.
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, []string{"acl-out-stage1"},
		&dash_acl_group.AclGroup{IpVersion: dash_types.IpVersion_IP_VERSION_IPV4})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-out-stage1", "10"},
		&dash_acl_rule.AclRule{
			Priority:    10,
			Action:      dash_acl_rule.Action_ACTION_DENY,
			Terminating: true,
			DstAddr:     []*dash_types.IpPrefix{ipPrefix(10, 99, 0, 0, 16)},
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-out-stage1", "100"},
		&dash_acl_rule.AclRule{
			Priority:    100,
			Action:      dash_acl_rule.Action_ACTION_PERMIT,
			Terminating: true,
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_OUT, []string{"eni-001", "1"},
		&dash_acl_out.AclOut{V4AclGroupId: "acl-out-stage1"})

	// Route group + routes for eni-001
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ROUTE_GROUP, []string{"rg-prod"},
		&dash_route_group.RouteGroup{Version: "v1"})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.1.0.0/16"},
		&dash_route.Route{
			RoutingType: dash_route_type.RoutingType_ROUTING_TYPE_VNET,
			RoutingTypeData: &dash_route.Route_Vnet{Vnet: "vnet-stage"},
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.2.0.0/16"},
		&dash_route.Route{
			RoutingType: dash_route_type.RoutingType_ROUTING_TYPE_DROP,
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ROUTE, []string{"rg-prod", "10.3.0.0/16"},
		&dash_route.Route{
			RoutingType: dash_route_type.RoutingType_ROUTING_TYPE_DIRECT,
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ENI_ROUTE, []string{"eni-001"},
		&dash_eni_route.EniRoute{GroupId: "rg-prod"})

	// vnet_mapping for vnet-stage 10.1.0.10 -> underlay 100.64.0.10
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_VNET_MAPPING, []string{"vnet-stage", "10.1.0.10"},
		&dash_vnet_mapping.VnetMapping{
			UnderlayIp:  ipAddr(100, 64, 0, 10),
			RoutingType: dash_route_type.RoutingType_ROUTING_TYPE_VNET,
		})

	// Inbound: route_rule for eni-001 vni=1001 prefix 0.0.0.0/0 priority 100, decap action.
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ROUTE_RULE, []string{"eni-001", "1001", "0.0.0.0/0", "100"},
		&dash_route_rule.RouteRule{
			ActionType: dash_route_type.ActionType_ACTION_TYPE_DECAP,
			Vnet:       proto.String("vnet-prod"),
		})

	// ACL_IN for eni-001 stage 1 — permit everything.
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_GROUP, []string{"acl-in-stage1"},
		&dash_acl_group.AclGroup{IpVersion: dash_types.IpVersion_IP_VERSION_IPV4})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_RULE, []string{"acl-in-stage1", "100"},
		&dash_acl_rule.AclRule{
			Priority: 100, Action: dash_acl_rule.Action_ACTION_PERMIT, Terminating: true,
		})
	mustApply(t, store, dashapi.ObjectKind_OBJECT_KIND_ACL_IN, []string{"eni-001", "1"},
		&dash_acl_in.AclIn{V4AclGroupId: "acl-in-stage1"})

	return eng, store
}

// --- Outbound cases ---

func TestOutbound_EncapViaVnetMapping(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_OUTBOUND,
		Eni:       "eni-001",
		SrcIp:     "10.0.0.1", DstIp: "10.1.0.10",
		Protocol: 6, SrcPort: 1024, DstPort: 80, LengthBytes: 64,
	}, true)
	if d.GetAction() != dashapi.Decision_ACTION_ENCAP {
		t.Fatalf("want ENCAP, got %s (reason=%s trace=%v)", d.GetAction(), d.GetReason(), d.GetTrace())
	}
	if d.GetOutUnderlayIp() != "100.64.0.10" {
		t.Errorf("underlay want 100.64.0.10 got %s", d.GetOutUnderlayIp())
	}
	if d.GetMatchedRoutePrefix() != "10.1.0.0/16" {
		t.Errorf("matched prefix want 10.1.0.0/16 got %s", d.GetMatchedRoutePrefix())
	}
}

func TestOutbound_RouteDrop(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_OUTBOUND, Eni: "eni-001",
		SrcIp: "10.0.0.1", DstIp: "10.2.0.5", Protocol: 6,
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_DROP {
		t.Fatalf("want DROP via DROP route, got %s", d.GetAction())
	}
}

func TestOutbound_RouteDirect(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_OUTBOUND, Eni: "eni-001",
		SrcIp: "10.0.0.1", DstIp: "10.3.0.5", Protocol: 6,
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_FORWARD {
		t.Fatalf("want FORWARD via DIRECT route, got %s (reason=%s)", d.GetAction(), d.GetReason())
	}
}

func TestOutbound_AclDeny(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_OUTBOUND, Eni: "eni-001",
		SrcIp: "10.0.0.1", DstIp: "10.99.0.50",
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_DROP {
		t.Fatalf("want DROP via ACL deny, got %s", d.GetAction())
	}
	if d.GetMatchedAclPriority() != 10 || d.GetMatchedAclStage() != 1 {
		t.Errorf("matched acl stage=%d prio=%d (want stage=1 prio=10)",
			d.GetMatchedAclStage(), d.GetMatchedAclPriority())
	}
}

func TestOutbound_DisabledEni(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_OUTBOUND, Eni: "eni-002",
		DstIp: "10.1.0.10",
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_DROP {
		t.Fatalf("want DROP for disabled ENI, got %s", d.GetAction())
	}
}

func TestOutbound_NoRouteMatch(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_OUTBOUND, Eni: "eni-001",
		SrcIp: "10.0.0.1", DstIp: "172.16.0.1",
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_DROP {
		t.Fatalf("want DROP for missing route, got %s", d.GetAction())
	}
}

// --- Inbound cases ---

func TestInbound_DeliverViaRouteRuleAndAclPermit(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_INBOUND,
		Eni:       "eni-001",
		Vni:       1001,
		SrcIp:     "10.99.0.1", DstIp: "10.0.0.4",
	}, true)
	if d.GetAction() != dashapi.Decision_ACTION_FORWARD {
		t.Fatalf("want FORWARD inbound deliver, got %s (reason=%s trace=%v)",
			d.GetAction(), d.GetReason(), d.GetTrace())
	}
	if d.GetOutEni() != "eni-001" {
		t.Errorf("out_eni want eni-001 got %s", d.GetOutEni())
	}
}

func TestInbound_NoRouteRule_Drops(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_INBOUND,
		Eni:       "eni-001",
		Vni:       9999, // no rule for this VNI
		SrcIp:     "10.99.0.1", DstIp: "10.0.0.4",
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_DROP {
		t.Fatalf("want DROP no route_rule, got %s", d.GetAction())
	}
}

func TestInbound_ResolveByMac(t *testing.T) {
	eng, _ := newFixture(t)
	d := eng.Evaluate(&dashapi.Packet{
		Direction: dashapi.Packet_DIRECTION_INBOUND,
		Vni:       1001,
		DstMac:    "00:11:22:33:44:55",
		SrcIp:     "10.99.0.1", DstIp: "10.0.0.4",
	}, false)
	if d.GetAction() != dashapi.Decision_ACTION_FORWARD {
		t.Fatalf("want FORWARD via mac lookup, got %s", d.GetAction())
	}
}
