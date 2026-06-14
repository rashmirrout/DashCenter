package flow

import (
	"context"
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	storefile "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// seedRichFleet adds variants the baseline seedFleet doesn't cover:
// - outbound ACL with allow_and_continue cascade
// - direct + service_tunnel + multi-metric routes
// - same prefix at metric 10 and metric 100 (tie-break)
func seedRichFleet(t *testing.T) store.DesiredStore {
	t.Helper()
	st, err := storefile.Open(t.TempDir())
	if err != nil {
		t.Fatalf("storefile.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	put := func(kind, name string, spec any) {
		t.Helper()
		_, err := st.Put(context.Background(), store.ObjectKey{
			Namespace: store.DefaultNamespace,
			Kind:      kind,
			Name:      name,
		}, spec, 0)
		if err != nil {
			t.Fatalf("seed %s/%s: %v", kind, name, err)
		}
	}

	put("vnet", "tenant-v", &dashcenterv1.VnetSpec{Name: "tenant-v", Vni: 100})
	put("eni", "eni-x", &dashcenterv1.EniSpec{
		Name:                "eni-x",
		VnetName:            "tenant-v",
		MacAddress:          "aa:bb:cc:00:00:0a",
		UnderlayIp:          "10.0.0.20",
		AdminState:          "up",
		PlacementHintDpuIds: []string{"dpu-1"},
	})
	put("vnet_mapping", "map-x", &dashcenterv1.VnetMappingSpec{
		VnetName:   "tenant-v",
		IpAddress:  "192.168.1.1",
		UnderlayIp: "10.0.0.20",
		MacAddress: "aa:bb:cc:00:00:0a",
		Action:     "vnet_encap",
	})
	// Route policy with FOUR next-hop variants:
	//   /32 direct  → ALLOW
	//   /30 service_tunnel → ENCAP
	//   /24 vnet     → ENCAP (via vnet_mapping)
	//   default drop → DROP_NO_ROUTE
	// Plus a same-prefix multi-metric pair to exercise metric tie-break.
	put("route_policy", "rp-x", &dashcenterv1.RoutePolicySpec{
		Name:     "rp-x",
		EniNames: []string{"eni-x"},
		Routes: []*dashcenterv1.RouteSpec{
			{Prefix: "192.168.1.1/32", NextHopType: "direct", Metric: 5},
			{Prefix: "192.168.1.0/30", NextHopType: "service_tunnel", NextHopTarget: "st-egress", Metric: 10},
			{Prefix: "192.168.1.0/24", NextHopType: "vnet", NextHopTarget: "tenant-v", Metric: 10},
			{Prefix: "10.10.0.0/16", NextHopType: "vnet", NextHopTarget: "tenant-v", Metric: 10},
			{Prefix: "10.10.0.0/16", NextHopType: "vnet", NextHopTarget: "tenant-v", Metric: 100},
			{Prefix: "0.0.0.0/0", NextHopType: "drop", Metric: 1000},
		},
	})
	// Outbound ACL with allow_and_continue cascade then default allow.
	put("acl_policy", "acl-outbound-cascade", &dashcenterv1.AclPolicySpec{
		Name:     "acl-outbound-cascade",
		Stage:    "outbound",
		EniNames: []string{"eni-x"},
		Rules: []*dashcenterv1.AclRuleSpec{
			{Priority: 10, Action: "allow_and_continue", DstPrefixes: []string{"10.0.0.0/8"}, Protocols: []string{"tcp"}},
			{Priority: 20, Action: "allow_and_continue", DstPrefixes: []string{"10.0.0.0/8"}, Protocols: []string{"tcp"}},
			{Priority: 999, Action: "deny", DstPrefixes: []string{"192.0.2.0/24"}},
		},
	})
	return st
}

// PE-G1 extra #1: direct next-hop → ALLOW (no encap).
func TestTraceFlow_DirectAllow(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			SrcIp:     "203.0.113.1",
			DstIp:     "192.168.1.1", // exact /32 match → direct
			Protocol:  "tcp",
			DstPort:   80,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_ALLOW {
		t.Errorf("verdict=%v want=VERDICT_ALLOW; trace=%v", res.GetVerdict(), res.GetTrace())
	}
	if res.GetMatchedRoute().GetPrefix() != "192.168.1.1/32" {
		t.Errorf("matched route=%v want /32", res.GetMatchedRoute())
	}
}

// PE-G1 extra #2: service_tunnel next-hop → ENCAP.
func TestTraceFlow_ServiceTunnelEncap(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			SrcIp:     "203.0.113.1",
			DstIp:     "192.168.1.2", // matches /30 service_tunnel (longer than /24)
			Protocol:  "tcp",
			DstPort:   80,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_ENCAP {
		t.Errorf("verdict=%v want=VERDICT_ENCAP; trace=%v", res.GetVerdict(), res.GetTrace())
	}
	if res.GetMatchedRoute().GetNextHopType() != "service_tunnel" {
		t.Errorf("next_hop_type=%q want service_tunnel", res.GetMatchedRoute().GetNextHopType())
	}
}

// PE-G1 extra #3: route → vnet but no matching VnetMapping → DROP_NO_MAPPING.
func TestTraceFlow_NoMappingDrop(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			SrcIp:     "203.0.113.1",
			DstIp:     "192.168.1.99", // /24 vnet route hits, but no mapping for .99
			Protocol:  "tcp",
			DstPort:   80,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_MAPPING {
		t.Errorf("verdict=%v want=VERDICT_DROP_NO_MAPPING; trace=%v", res.GetVerdict(), res.GetTrace())
	}
}

// PE-G1 extra #4: metric tie-break — same prefix, lower metric wins.
func TestTraceFlow_MetricTieBreak(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			SrcIp:     "203.0.113.1",
			DstIp:     "10.10.5.5", // matches /16 at metric 10 AND metric 100
			Protocol:  "tcp",
			DstPort:   80,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetMatchedRoute() == nil {
		t.Fatal("matched route nil")
	}
	// We expect the metric=10 route (no DROP_NO_MAPPING because vnet
	// route → mapping miss → drop; we only check the metric here).
	// Trace line carries the metric — assert via substring.
	hasMetric10 := false
	for _, line := range res.GetTrace() {
		if strings.Contains(line, "metric=10") {
			hasMetric10 = true
			break
		}
	}
	if !hasMetric10 {
		t.Errorf("trace should mention metric=10 winner; got %v", res.GetTrace())
	}
}

// PE-G1 extra #5: verdict_only suppresses trace lines.
func TestTraceFlow_VerdictOnly(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		VerdictOnly: true,
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			SrcIp:     "203.0.113.1",
			DstIp:     "192.168.1.1",
			Protocol:  "tcp",
			DstPort:   80,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if len(res.GetTrace()) != 0 {
		t.Errorf("verdict_only=true should suppress trace; got %v", res.GetTrace())
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_ALLOW {
		t.Errorf("verdict=%v want VERDICT_ALLOW", res.GetVerdict())
	}
}

// PE-G1 extra #6: outbound ACL cascade — allow_and_continue rules
// don't terminate; deny rule for a different prefix doesn't match, so
// the final verdict is allow.
func TestTraceFlow_OutboundAllowAndContinueChain(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_OUTBOUND,
			EniName:   "eni-x",
			SrcIp:     "10.0.0.1",
			DstIp:     "10.10.0.1",
			Protocol:  "tcp",
			DstPort:   443,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	// outbound ACL matched 2x allow_and_continue + the deny on
	// 192.0.2.0/24 didn't match → final ACL verdict is allow → route
	// + mapping path runs (10.10/16 → vnet → no mapping → DROP_NO_MAPPING).
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_MAPPING {
		t.Errorf("verdict=%v want=VERDICT_DROP_NO_MAPPING; trace=%v", res.GetVerdict(), res.GetTrace())
	}
	if res.GetMatchedAclRule() == nil || res.GetMatchedAclRule().GetAction() != "allow" {
		t.Errorf("matched ACL=%v want allow (last allow_and_continue resolves to allow)", res.GetMatchedAclRule())
	}
}

// PE-G1 extra #7: outbound ACL deny match.
func TestTraceFlow_OutboundDeny(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_OUTBOUND,
			EniName:   "eni-x",
			SrcIp:     "10.0.0.1",
			DstIp:     "192.0.2.10",
			Protocol:  "tcp",
			DstPort:   80,
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_DROP_ACL {
		t.Errorf("verdict=%v want=VERDICT_DROP_ACL; trace=%v", res.GetVerdict(), res.GetTrace())
	}
}

// ExplainMatch routes — exercises the explainRoute path.
func TestExplainMatch_RouteCandidates(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.ExplainMatch(context.Background(), &dashcenterv1.MatchRequest{
		Subject: dashcenterv1.MatchRequest_SUBJECT_ROUTE,
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			DstIp:     "192.168.1.1",
		},
	})
	if err != nil {
		t.Fatalf("ExplainMatch: %v", err)
	}
	if len(res.GetCandidates()) == 0 {
		t.Fatal("want >=1 candidate")
	}
	if res.GetSelectedCandidateId() == "" {
		t.Error("want a selected route candidate")
	}
	if !strings.Contains(res.GetSelectedCandidateId(), "/32") {
		t.Errorf("selected_id=%q want suffix /32 (longest-prefix winner)", res.GetSelectedCandidateId())
	}
}

// ExplainMatch routes — invalid dst_ip returns single error candidate.
func TestExplainMatch_RouteBadDst(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.ExplainMatch(context.Background(), &dashcenterv1.MatchRequest{
		Subject: dashcenterv1.MatchRequest_SUBJECT_ROUTE,
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			DstIp:     "not-an-ip",
		},
	})
	if err != nil {
		t.Fatalf("ExplainMatch: %v", err)
	}
	if len(res.GetCandidates()) != 1 {
		t.Errorf("want exactly 1 candidate (the error sentinel), got %d", len(res.GetCandidates()))
	}
	if res.GetCandidates()[0].GetMatched() {
		t.Errorf("sentinel candidate should have matched=false")
	}
}

// ExplainMatch VnetMapping — exercises the explainVnetMapping path.
func TestExplainMatch_VnetMappingCandidates(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.ExplainMatch(context.Background(), &dashcenterv1.MatchRequest{
		Subject: dashcenterv1.MatchRequest_SUBJECT_VNET_MAPPING,
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-x",
			DstIp:     "192.168.1.1",
		},
	})
	if err != nil {
		t.Fatalf("ExplainMatch: %v", err)
	}
	if res.GetSelectedCandidateId() == "" {
		t.Error("want selected vnet_mapping candidate")
	}
	if !strings.Contains(res.GetSelectedCandidateId(), "map-x") {
		t.Errorf("selected_id=%q want suffix map-x", res.GetSelectedCandidateId())
	}
}

// ExplainMatch input validation.
func TestExplainMatch_InvalidArgs(t *testing.T) {
	eng := New(nil, nil, nil, nil)
	cases := []struct {
		name string
		req  *dashcenterv1.MatchRequest
	}{
		{"nil req", nil},
		{"nil flow", &dashcenterv1.MatchRequest{Subject: dashcenterv1.MatchRequest_SUBJECT_ACL}},
		{"missing eni", &dashcenterv1.MatchRequest{Subject: dashcenterv1.MatchRequest_SUBJECT_ACL, Flow: &dashcenterv1.FlowDescriptor{}}},
		{"unspecified subject", &dashcenterv1.MatchRequest{Flow: &dashcenterv1.FlowDescriptor{EniName: "e1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eng.ExplainMatch(context.Background(), tc.req)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// ExplainDrift input validation.
func TestExplainDrift_InvalidArgs(t *testing.T) {
	eng := New(nil, nil, nil, nil)
	cases := []struct {
		name string
		req  *dashcenterv1.DriftExplainRequest
	}{
		{"nil req", nil},
		{"nil target", &dashcenterv1.DriftExplainRequest{DpuId: "d1"}},
		{"empty name", &dashcenterv1.DriftExplainRequest{Target: &dashcenterv1.NameRef{Kind: "vnet"}, DpuId: "d1"}},
		{"empty kind", &dashcenterv1.DriftExplainRequest{Target: &dashcenterv1.NameRef{Name: "x"}, DpuId: "d1"}},
		{"missing dpu", &dashcenterv1.DriftExplainRequest{Target: &dashcenterv1.NameRef{Kind: "vnet", Name: "x"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eng.ExplainDrift(context.Background(), tc.req)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// ExplainDrift across all known kinds.
func TestExplainDrift_AllKinds(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	cases := []struct {
		kind, name string
		wantFound  bool
	}{
		{"vnet", "tenant-v", true},
		{"eni", "eni-x", true},
		{"vnet_mapping", "map-x", true},
		{"acl_policy", "acl-outbound-cascade", true},
		{"route_policy", "rp-x", true},
		{"ha_set", "no-such", false},
		{"unknown_kind", "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.name, func(t *testing.T) {
			res, err := eng.ExplainDrift(context.Background(), &dashcenterv1.DriftExplainRequest{
				Target: &dashcenterv1.NameRef{Kind: tc.kind, Name: tc.name},
				DpuId:  "dpu-1",
			})
			if err != nil {
				t.Fatalf("ExplainDrift: %v", err)
			}
			gotFound := res.GetSuggested() == dashcenterv1.DriftExplanation_REMEDIATION_RECONCILE
			if gotFound != tc.wantFound {
				t.Errorf("found=%v want=%v (suggested=%v)", gotFound, tc.wantFound, res.GetSuggested())
			}
		})
	}
}

// GetAclHitStats invalid arg.
func TestGetAclHitStats_NilReq(t *testing.T) {
	eng := New(nil, nil, nil, nil)
	_, err := eng.GetAclHitStats(context.Background(), nil)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

// GetAclHitStats with explicit dpu_ids + policy_names filter.
func TestGetAclHitStats_ExplicitFilters(t *testing.T) {
	st := seedRichFleet(t)
	eng := New(st, nil, nil, nil)
	rows, err := eng.GetAclHitStats(context.Background(), &dashcenterv1.AclStatsRequest{
		DpuIds:      []string{"dpu-1"},
		Namespaces:  []string{"default"},
		PolicyNames: []string{"acl-outbound-cascade"},
	})
	if err != nil {
		t.Fatalf("GetAclHitStats: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 row (filtered), got %d", len(rows))
	}
	if rows[0].GetPolicyName() != "acl-outbound-cascade" {
		t.Errorf("policy=%q want acl-outbound-cascade", rows[0].GetPolicyName())
	}
	if rows[0].GetStage() != "outbound" {
		t.Errorf("stage=%q want outbound", rows[0].GetStage())
	}
}

// Resimulator NextErr surfaces as a non-nil error.
func TestTriggerResimulation_PropagatesError(t *testing.T) {
	resim := &NopResimulator{NextErr: errors.New("boom")}
	eng := New(nil, nil, nil, resim)
	_, err := eng.TriggerResimulation(context.Background(), &dashcenterv1.ResimRequest{
		EniNames: []string{"e1"},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("want propagated boom error, got %v", err)
	}
}

// NopResimulator without NextTxnID generates a synthetic id.
func TestNopResimulator_SyntheticTxnID(t *testing.T) {
	resim := &NopResimulator{}
	got, err := resim.Resimulate(context.Background(), []string{"d1"}, nil, "ns-z", false)
	if err != nil {
		t.Fatalf("Resimulate: %v", err)
	}
	if got != "resim-ns-z" {
		t.Errorf("txn=%q want resim-ns-z", got)
	}
	if resim.LastNS != "ns-z" {
		t.Errorf("LastNS=%q want ns-z", resim.LastNS)
	}
}

// dirName covers UNKNOWN as well (defensive default).
func TestDirNameUnknown(t *testing.T) {
	if got := dirName(dashcenterv1.FlowDescriptor_DIRECTION_UNSPECIFIED); got != "UNKNOWN" {
		t.Errorf("dirName(UNSPECIFIED)=%q want UNKNOWN", got)
	}
}
