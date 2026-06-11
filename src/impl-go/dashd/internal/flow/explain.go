// ExplainMatch returns per-candidate reasoning for a single subject
// (ACL / Route / VnetMapping) decision. Shares the matcher helpers
// with TraceFlow so both diagnostics surface identical reasons.
package flow

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
)

// ExplainMatch returns the candidate list (with per-candidate match
// outcome + reason) and the id of the selected winner.
//
// Subject selection:
//
//	SUBJECT_ACL          → every AclRule across every AclPolicy bound to
//	                       the ENI, ordered by priority asc; candidate_id
//	                       is "acl/{policy}/{priority}".
//	SUBJECT_ROUTE        → every RouteSpec across every RoutePolicy
//	                       bound to the ENI whose prefix contains dst_ip;
//	                       candidate_id is "route/{policy}/{prefix}".
//	SUBJECT_VNET_MAPPING → every VnetMapping whose vnet_name matches
//	                       the ENI's vnet; candidate_id is
//	                       "vnet_mapping/{name}".
//
// The selected_candidate_id is empty when no candidate matched (and the
// candidates slice still carries per-row reasons explaining why).
func (e *Engine) ExplainMatch(ctx context.Context, req *dashcenterv1.MatchRequest) (*dashcenterv1.MatchExplanation, error) {
	if req == nil {
		return nil, invArgf("request is nil")
	}
	flow := req.GetFlow()
	if flow == nil {
		return nil, invArgf("flow descriptor is required")
	}
	if flow.GetEniName() == "" {
		return nil, invArgf("flow.eni_name is required")
	}
	if req.GetSubject() == dashcenterv1.MatchRequest_SUBJECT_UNSPECIFIED {
		return nil, invArgf("subject is required (ACL, ROUTE, or VNET_MAPPING)")
	}

	specs, err := e.loadView(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := specs.Enis[flow.GetEniName()]; !ok {
		return nil, notFoundf("eni %q does not exist", flow.GetEniName())
	}

	res := &dashcenterv1.MatchExplanation{ComputedAt: nowTS()}
	switch req.GetSubject() {
	case dashcenterv1.MatchRequest_SUBJECT_ACL:
		explainAcl(res, specs, flow)
	case dashcenterv1.MatchRequest_SUBJECT_ROUTE:
		explainRoute(res, specs, flow)
	case dashcenterv1.MatchRequest_SUBJECT_VNET_MAPPING:
		explainVnetMapping(res, specs, flow)
	}
	return res, nil
}

func explainAcl(res *dashcenterv1.MatchExplanation, specs *placement.DesiredSpecs, flow *dashcenterv1.FlowDescriptor) {
	type rulePtr struct {
		policy *dashcenterv1.AclPolicySpec
		rule   *dashcenterv1.AclRuleSpec
	}
	policies := aclPoliciesForEni(specs, flow.GetEniName(), stageForDirection(flow.GetDirection()))
	all := make([]rulePtr, 0)
	for _, p := range policies {
		for _, r := range p.GetRules() {
			all = append(all, rulePtr{policy: p, rule: r})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].rule.GetPriority() != all[j].rule.GetPriority() {
			return all[i].rule.GetPriority() < all[j].rule.GetPriority()
		}
		return all[i].policy.GetName() < all[j].policy.GetName()
	})

	var selectedID string
	for _, rp := range all {
		matched, reason := matchAclRule(rp.rule, flow)
		cand := &dashcenterv1.MatchCandidate{
			CandidateId: fmt.Sprintf("acl/%s/%d", rp.policy.GetName(), rp.rule.GetPriority()),
			Matched:     matched,
			Reason:      reason,
			Priority:    rp.rule.GetPriority(),
		}
		res.Candidates = append(res.Candidates, cand)
		if matched && selectedID == "" {
			act := strings.ToLower(strings.TrimSpace(rp.rule.GetAction()))
			// First matching rule that is a *terminal* action wins.
			// allow_and_continue does NOT set selected — the chain
			// continues evaluating.
			if act == aclActionAllow || act == aclActionDeny {
				selectedID = cand.CandidateId
			}
		}
	}
	res.SelectedCandidateId = selectedID
}

func explainRoute(res *dashcenterv1.MatchExplanation, specs *placement.DesiredSpecs, flow *dashcenterv1.FlowDescriptor) {
	dst, err := netip.ParseAddr(strings.TrimSpace(flow.GetDstIp()))
	if err != nil {
		res.Candidates = append(res.Candidates, &dashcenterv1.MatchCandidate{
			CandidateId: "route/-",
			Matched:     false,
			Reason:      fmt.Sprintf("dst_ip %q invalid: %v", flow.GetDstIp(), err),
		})
		return
	}
	type cand struct {
		policy *dashcenterv1.RoutePolicySpec
		route  *dashcenterv1.RouteSpec
		bits   int
		hit    bool
	}
	all := []cand{}
	for _, rp := range specs.RoutePolicies {
		bound := false
		for _, n := range rp.GetEniNames() {
			if n == flow.GetEniName() {
				bound = true
				break
			}
		}
		if !bound {
			continue
		}
		for _, r := range rp.GetRoutes() {
			c := cand{policy: rp, route: r, bits: -1, hit: false}
			pf, err := netip.ParsePrefix(strings.TrimSpace(r.GetPrefix()))
			if err == nil {
				c.bits = pf.Bits()
				c.hit = pf.Contains(dst)
			}
			all = append(all, c)
		}
	}
	// Order: matched first (longest-prefix first within matched, then
	// metric asc), then non-matched in declared order.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].hit != all[j].hit {
			return all[i].hit
		}
		if all[i].hit && all[j].hit {
			if all[i].bits != all[j].bits {
				return all[i].bits > all[j].bits
			}
			return all[i].route.GetMetric() < all[j].route.GetMetric()
		}
		return all[i].policy.GetName()+all[i].route.GetPrefix() <
			all[j].policy.GetName()+all[j].route.GetPrefix()
	})

	for i, c := range all {
		reason := ""
		if c.hit {
			reason = fmt.Sprintf("%s ⊇ %s (len=%d, metric=%d, next_hop=%s/%s)",
				c.route.GetPrefix(), dst.String(), c.bits, c.route.GetMetric(),
				c.route.GetNextHopType(), c.route.GetNextHopTarget())
		} else if c.bits < 0 {
			reason = fmt.Sprintf("prefix %q invalid", c.route.GetPrefix())
		} else {
			reason = fmt.Sprintf("%s ⊅ %s", c.route.GetPrefix(), dst.String())
		}
		mc := &dashcenterv1.MatchCandidate{
			CandidateId: fmt.Sprintf("route/%s/%s", c.policy.GetName(), c.route.GetPrefix()),
			Matched:     c.hit,
			Reason:      reason,
			Priority:    uint32(c.bits),
		}
		res.Candidates = append(res.Candidates, mc)
		if i == 0 && c.hit && res.SelectedCandidateId == "" {
			res.SelectedCandidateId = mc.CandidateId
		}
	}
}

func explainVnetMapping(res *dashcenterv1.MatchExplanation, specs *placement.DesiredSpecs, flow *dashcenterv1.FlowDescriptor) {
	eni := specs.Enis[flow.GetEniName()]
	vnetName := ""
	if eni != nil {
		vnetName = eni.GetVnetName()
	}
	// Sort mappings by name so iteration is deterministic.
	names := make([]string, 0, len(specs.VnetMappings))
	for n := range specs.VnetMappings {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		m := specs.VnetMappings[n]
		matched := false
		reason := ""
		switch {
		case m.GetVnetName() != vnetName:
			reason = fmt.Sprintf("vnet=%q != eni.vnet=%q", m.GetVnetName(), vnetName)
		case strings.TrimSpace(m.GetIpAddress()) != strings.TrimSpace(flow.GetDstIp()):
			reason = fmt.Sprintf("ip_address=%q != dst_ip=%q", m.GetIpAddress(), flow.GetDstIp())
		default:
			matched = true
			reason = fmt.Sprintf("vnet=%q ip_address=%q → underlay=%s mac=%s action=%s",
				m.GetVnetName(), m.GetIpAddress(),
				m.GetUnderlayIp(), m.GetMacAddress(), m.GetAction())
		}
		cand := &dashcenterv1.MatchCandidate{
			CandidateId: fmt.Sprintf("vnet_mapping/%s", n),
			Matched:     matched,
			Reason:      reason,
		}
		res.Candidates = append(res.Candidates, cand)
		if matched && res.SelectedCandidateId == "" {
			res.SelectedCandidateId = cand.CandidateId
		}
	}
}
