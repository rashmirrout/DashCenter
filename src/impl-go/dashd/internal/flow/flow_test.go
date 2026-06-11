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

// seedFleet builds a small but realistic desired state for the trace
// tests: 1 vnet, 2 ENIs, 2 mappings, 1 route policy (vnet → tenant + drop
// default), 1 inbound ACL policy (allow 443/tcp, deny everything else).
func seedFleet(t *testing.T) store.DesiredStore {
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
	put("eni", "eni-web-01", &dashcenterv1.EniSpec{
		Name:        "eni-web-01",
		VnetName:    "tenant-v",
		MacAddress:  "aa:bb:cc:00:00:01",
		UnderlayIp:  "10.0.0.11",
		AdminState:  "up",
		PlacementHintDpuIds: []string{"dpu-1"},
	})
	put("eni", "eni-web-02", &dashcenterv1.EniSpec{
		Name:        "eni-web-02",
		VnetName:    "tenant-v",
		MacAddress:  "aa:bb:cc:00:00:02",
		UnderlayIp:  "10.0.0.12",
		AdminState:  "up",
		PlacementHintDpuIds: []string{"dpu-1"},
	})
	put("vnet_mapping", "map-01", &dashcenterv1.VnetMappingSpec{
		VnetName:   "tenant-v",
		IpAddress:  "192.168.10.10",
		UnderlayIp: "10.0.0.11",
		MacAddress: "aa:bb:cc:00:00:01",
		Action:     "vnet_encap",
	})
	put("route_policy", "rp-default", &dashcenterv1.RoutePolicySpec{
		Name:     "rp-default",
		EniNames: []string{"eni-web-01"},
		Routes: []*dashcenterv1.RouteSpec{
			{Prefix: "192.168.10.0/24", NextHopType: "vnet", NextHopTarget: "tenant-v", Metric: 10},
			{Prefix: "0.0.0.0/0", NextHopType: "drop", Metric: 1000},
		},
	})
	put("acl_policy", "acl-inbound-01", &dashcenterv1.AclPolicySpec{
		Name:     "acl-inbound-01",
		Stage:    "inbound",
		EniNames: []string{"eni-web-01"},
		Rules: []*dashcenterv1.AclRuleSpec{
			{Priority: 100, Action: "allow", SrcPrefixes: []string{"0.0.0.0/0"}, DstPorts: []string{"443"}, Protocols: []string{"tcp"}},
			{Priority: 1000, Action: "deny", SrcPrefixes: []string{"0.0.0.0/0"}},
		},
	})
	return st
}

// PE-G1 test #1: permit verdict (allow rule wins).
func TestTraceFlow_AllowVerdict(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		DpuId: "dpu-1",
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-web-01",
			SrcIp:     "203.0.113.10",
			DstIp:     "192.168.10.10",
			SrcPort:   55555,
			DstPort:   443,
			Protocol:  "tcp",
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_ENCAP {
		t.Errorf("verdict=%v want=VERDICT_ENCAP; trace=%v", res.GetVerdict(), res.GetTrace())
	}
	if res.GetMatchedAclRule() == nil || res.GetMatchedAclRule().GetAction() != "allow" {
		t.Errorf("matched ACL=%v want allow rule 100", res.GetMatchedAclRule())
	}
	if res.GetMatchedRoute() == nil || res.GetMatchedRoute().GetPrefix() != "192.168.10.0/24" {
		t.Errorf("matched route=%v want /24 vnet hop", res.GetMatchedRoute())
	}
	if res.GetMatchedVnetMapping() == nil || res.GetMatchedVnetMapping().GetIpAddress() != "192.168.10.10" {
		t.Errorf("matched vnet_mapping=%v want 192.168.10.10", res.GetMatchedVnetMapping())
	}
}

// PE-G1 test #2: deny verdict (deny rule matches first).
func TestTraceFlow_DenyVerdict(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		DpuId: "dpu-1",
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-web-01",
			SrcIp:     "203.0.113.10",
			DstIp:     "192.168.10.10",
			SrcPort:   55555,
			DstPort:   22, // SSH — no allow rule matches → deny catch-all triggers
			Protocol:  "tcp",
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_DROP_ACL {
		t.Errorf("verdict=%v want=VERDICT_DROP_ACL; trace=%v", res.GetVerdict(), res.GetTrace())
	}
	if res.GetMatchedAclRule() == nil || res.GetMatchedAclRule().GetPriority() != 1000 {
		t.Errorf("matched ACL=%v want priority 1000 deny", res.GetMatchedAclRule())
	}
}

// PE-G1 test #3: no-match default ACL → continues to route → route
// returns drop → verdict is DROP_NO_ROUTE.
func TestTraceFlow_NoRouteFallthroughDrop(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		DpuId: "dpu-1",
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-web-01",
			SrcIp:     "10.0.0.99",
			DstIp:     "172.16.0.1", // matches default 0.0.0.0/0 → next_hop=drop
			SrcPort:   12345,
			DstPort:   443,
			Protocol:  "tcp",
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_ROUTE {
		t.Errorf("verdict=%v want=VERDICT_DROP_NO_ROUTE; trace=%v", res.GetVerdict(), res.GetTrace())
	}
	if res.GetMatchedRoute() == nil || res.GetMatchedRoute().GetNextHopType() != "drop" {
		t.Errorf("matched route=%v want default 0.0.0.0/0 drop", res.GetMatchedRoute())
	}
}

// PE-G1 test #4: ENI exists but no route policy bound → DROP_NO_ROUTE.
func TestTraceFlow_NoRouteForEni(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		DpuId: "dpu-1",
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-web-02", // no route policy bound to this ENI
			SrcIp:     "203.0.113.10",
			DstIp:     "192.168.10.10",
			SrcPort:   55555,
			DstPort:   443,
			Protocol:  "tcp",
		},
	})
	if err != nil {
		t.Fatalf("TraceFlow: %v", err)
	}
	if res.GetVerdict() != dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_ROUTE {
		t.Errorf("verdict=%v want=VERDICT_DROP_NO_ROUTE; trace=%v", res.GetVerdict(), res.GetTrace())
	}
}

// PE-G1 test #5: invalid args.
func TestTraceFlow_InvalidArgs(t *testing.T) {
	eng := New(nil, nil, nil, nil)
	cases := []struct {
		name string
		req  *dashcenterv1.TraceFlowRequest
	}{
		{"nil req", nil},
		{"nil flow", &dashcenterv1.TraceFlowRequest{}},
		{"missing eni", &dashcenterv1.TraceFlowRequest{Flow: &dashcenterv1.FlowDescriptor{Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND}}},
		{"unspecified direction", &dashcenterv1.TraceFlowRequest{Flow: &dashcenterv1.FlowDescriptor{EniName: "e1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := eng.TraceFlow(context.Background(), tc.req)
			if !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("want ErrInvalidArgument, got %v", err)
			}
		})
	}
}

// PE-G1 test #6: ENI not in declared state → ErrNotFound.
func TestTraceFlow_EniNotFound(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)
	_, err := eng.TraceFlow(context.Background(), &dashcenterv1.TraceFlowRequest{
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "nope",
		},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// ExplainMatch returns per-candidate reasons. PE-1 spec test.
func TestExplainMatch_AclCandidates(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)
	res, err := eng.ExplainMatch(context.Background(), &dashcenterv1.MatchRequest{
		Subject: dashcenterv1.MatchRequest_SUBJECT_ACL,
		Flow: &dashcenterv1.FlowDescriptor{
			Direction: dashcenterv1.FlowDescriptor_DIRECTION_INBOUND,
			EniName:   "eni-web-01",
			SrcIp:     "203.0.113.10",
			DstIp:     "192.168.10.10",
			DstPort:   443,
			Protocol:  "tcp",
		},
	})
	if err != nil {
		t.Fatalf("ExplainMatch: %v", err)
	}
	if len(res.GetCandidates()) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(res.GetCandidates()))
	}
	// Priority 100 (allow) must match dst_port=443; priority 1000 (deny)
	// also matches (catch-all) but the first allow terminates selection.
	if !res.GetCandidates()[0].GetMatched() || res.GetSelectedCandidateId() == "" {
		t.Errorf("want first candidate matched + selected; got %+v / sel=%q",
			res.GetCandidates()[0], res.GetSelectedCandidateId())
	}
	if !strings.Contains(res.GetSelectedCandidateId(), "/100") {
		t.Errorf("selected_id=%q want suffix /100", res.GetSelectedCandidateId())
	}
}

// PE-G2 test: GetAclHitStats with zero_hits_only=true returns the
// policy when the NilHitStats source reports zero hits for every rule.
func TestGetAclHitStats_ZeroFilter(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil) // default = NilHitStats → every rule is "never observed"
	rows, err := eng.GetAclHitStats(context.Background(), &dashcenterv1.AclStatsRequest{
		ZeroHitsOnly: true,
	})
	if err != nil {
		t.Fatalf("GetAclHitStats: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("want at least one stats row for the seeded acl policy, got 0")
	}
	// Every rule is "never observed" → every Hits == 0 + LastHitAt == nil.
	for _, row := range rows {
		for _, r := range row.GetRules() {
			if r.GetHits() != 0 {
				t.Errorf("rule prio=%d Hits=%d want 0", r.GetPriority(), r.GetHits())
			}
			if r.GetLastHitAt() != nil {
				t.Errorf("rule prio=%d LastHitAt=%v want nil (never hit)", r.GetPriority(), r.GetLastHitAt())
			}
		}
	}
}

// GetAclHitStats filter behaviour: a synthetic HitStatsSource that
// reports hits for priority 100 should hide the policy when
// zero_hits_only=true.
func TestGetAclHitStats_NonZeroHidden(t *testing.T) {
	st := seedFleet(t)
	hits := &fakeHits{m: map[uint32]int64{100: 42}}
	eng := New(st, nil, hits, nil)

	allRows, err := eng.GetAclHitStats(context.Background(), &dashcenterv1.AclStatsRequest{})
	if err != nil {
		t.Fatalf("GetAclHitStats (all): %v", err)
	}
	if len(allRows) == 0 {
		t.Fatal("want >=1 row in non-filtered query")
	}

	zeroRows, err := eng.GetAclHitStats(context.Background(), &dashcenterv1.AclStatsRequest{ZeroHitsOnly: true})
	if err != nil {
		t.Fatalf("GetAclHitStats (zero only): %v", err)
	}
	if len(zeroRows) != 0 {
		t.Errorf("want 0 rows when zero_hits_only=true and prio 100 has hits, got %d", len(zeroRows))
	}
}

type fakeHits struct{ m map[uint32]int64 }

func (f *fakeHits) AclHits(_, _, _, _ string, prio uint32) (int64, int64, int64, bool) {
	if h, ok := f.m[prio]; ok {
		return h, h * 1500, 1_700_000_000_000_000_000, true
	}
	return 0, 0, 0, false
}

// PE-G1 test #7: ExplainDrift narrative for present + absent targets.
func TestExplainDrift_PresentAndAbsent(t *testing.T) {
	st := seedFleet(t)
	eng := New(st, nil, nil, nil)

	// Present: existing vnet.
	res, err := eng.ExplainDrift(context.Background(), &dashcenterv1.DriftExplainRequest{
		Target: &dashcenterv1.NameRef{Kind: "vnet", Name: "tenant-v"},
		DpuId:  "dpu-1",
	})
	if err != nil {
		t.Fatalf("ExplainDrift (present): %v", err)
	}
	if res.GetSuggested() != dashcenterv1.DriftExplanation_REMEDIATION_RECONCILE {
		t.Errorf("present: suggested=%v want RECONCILE", res.GetSuggested())
	}
	if !strings.Contains(res.GetRationale(), "exists in declared state") {
		t.Errorf("present rationale=%q want 'exists in declared state'", res.GetRationale())
	}

	// Absent: non-existent vnet.
	res, err = eng.ExplainDrift(context.Background(), &dashcenterv1.DriftExplainRequest{
		Target: &dashcenterv1.NameRef{Kind: "vnet", Name: "ghost-vnet"},
		DpuId:  "dpu-1",
	})
	if err != nil {
		t.Fatalf("ExplainDrift (absent): %v", err)
	}
	if res.GetSuggested() != dashcenterv1.DriftExplanation_REMEDIATION_MANUAL {
		t.Errorf("absent: suggested=%v want MANUAL", res.GetSuggested())
	}
}

// PE-G1 test #8: TriggerResimulation propagates scope to the Resimulator.
func TestTriggerResimulation_HappyPath(t *testing.T) {
	resim := &NopResimulator{NextTxnID: "txn-xyz"}
	eng := New(nil, nil, nil, resim)
	ack, err := eng.TriggerResimulation(context.Background(), &dashcenterv1.ResimRequest{
		DpuIds:       []string{"dpu-1", "dpu-2"},
		EniNames:     []string{"eni-web-01"},
		Namespace:    "default",
		DropAllFlows: true,
	})
	if err != nil {
		t.Fatalf("TriggerResimulation: %v", err)
	}
	if ack.GetTxnId() != "txn-xyz" {
		t.Errorf("txn_id=%q want txn-xyz", ack.GetTxnId())
	}
	if len(resim.LastDpus) != 2 || !resim.LastDropAll {
		t.Errorf("Resimulator not called with expected scope; got dpus=%v dropAll=%v", resim.LastDpus, resim.LastDropAll)
	}
}

// PE-G1 test #9: TriggerResimulation rejects empty scope.
func TestTriggerResimulation_EmptyScopeRejected(t *testing.T) {
	eng := New(nil, nil, nil, nil)
	_, err := eng.TriggerResimulation(context.Background(), &dashcenterv1.ResimRequest{
		Namespace: "default",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

// Matcher unit tests — keep the helpers honest.

func TestIpInPrefix(t *testing.T) {
	cases := []struct {
		ip       string
		prefixes []string
		want     bool
	}{
		{"10.0.0.1", []string{"10.0.0.0/24"}, true},
		{"10.0.1.1", []string{"10.0.0.0/24"}, false},
		{"203.0.113.5", []string{"203.0.113.0/24", "198.51.100.0/24"}, true},
		{"10.0.0.1", nil, true}, // empty list = any
		{"not-an-ip", []string{"10.0.0.0/24"}, false},
		{"10.0.0.1", []string{"bogus-prefix"}, false},
	}
	for _, tc := range cases {
		got, _, _ := ipInPrefix(tc.ip, tc.prefixes)
		if got != tc.want {
			t.Errorf("ipInPrefix(%q, %v) = %v; want %v", tc.ip, tc.prefixes, got, tc.want)
		}
	}
}

func TestPortMatches(t *testing.T) {
	cases := []struct {
		port  uint32
		specs []string
		want  bool
	}{
		{443, []string{"443"}, true},
		{443, []string{"80", "443"}, true},
		{1500, []string{"1000-2000"}, true},
		{2001, []string{"1000-2000"}, false},
		{80, nil, true}, // empty = any
		{80, []string{"not-a-port"}, false},
		{80, []string{"100-50"}, false}, // reversed range
	}
	for _, tc := range cases {
		got, _ := portMatches(tc.port, tc.specs)
		if got != tc.want {
			t.Errorf("portMatches(%d, %v) = %v; want %v", tc.port, tc.specs, got, tc.want)
		}
	}
}

func TestProtoMatches(t *testing.T) {
	cases := []struct {
		proto string
		specs []string
		want  bool
	}{
		{"tcp", []string{"tcp"}, true},
		{"tcp", []string{"6"}, true},      // numeric ↔ name
		{"6", []string{"tcp"}, true},      // reverse
		{"udp", []string{"tcp"}, false},
		{"icmpv6", []string{"58"}, true},
		{"icmp", nil, true}, // empty = any
		{"tcp", []string{"unknown"}, false},
	}
	for _, tc := range cases {
		got, _ := protoMatches(tc.proto, tc.specs)
		if got != tc.want {
			t.Errorf("protoMatches(%q, %v) = %v; want %v", tc.proto, tc.specs, got, tc.want)
		}
	}
}
