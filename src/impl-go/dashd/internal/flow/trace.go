// TraceFlow simulates the DASH pipeline for a synthetic packet against
// the current desired-state policy on one DPU, returning the verdict +
// a per-stage trace. PE-G1 quality gate.
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

// TraceFlow runs the ACL + route + vnet-mapping pipeline for the
// supplied flow descriptor and returns the verdict.
//
// Pipeline order (PE-G1 spec):
//
//	1. ACL chain    →  every AclPolicy bound to req.flow.eni_name with
//	                   stage matching req.flow.direction, evaluated
//	                   lowest-priority-number first (priority 1 wins
//	                   over priority 100). allow_and_continue falls
//	                   through to the next rule; allow / deny terminate.
//	2. Route        →  longest-prefix match on req.flow.dst_ip across
//	                   every RoutePolicy bound to the ENI; metric breaks
//	                   ties. The next_hop_type decides the verdict:
//	                   direct → ALLOW, drop → DROP, service_tunnel →
//	                   ENCAP, vnet → continue to stage 3.
//	3. VnetMapping  →  lookup on (route.next_hop_target, dst_ip) → the
//	                   underlay encap target. Missing entry →
//	                   DROP_NO_MAPPING.
func (e *Engine) TraceFlow(ctx context.Context, req *dashcenterv1.TraceFlowRequest) (*dashcenterv1.FlowTraceResult, error) {
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
	dir := flow.GetDirection()
	if dir != dashcenterv1.FlowDescriptor_DIRECTION_INBOUND &&
		dir != dashcenterv1.FlowDescriptor_DIRECTION_OUTBOUND {
		return nil, invArgf("flow.direction must be INBOUND or OUTBOUND")
	}

	specs, err := e.loadView(ctx)
	if err != nil {
		return nil, err
	}
	eni, ok := specs.Enis[flow.GetEniName()]
	if !ok {
		return nil, notFoundf("eni %q does not exist", flow.GetEniName())
	}

	res := &dashcenterv1.FlowTraceResult{
		Verdict:    dashcenterv1.FlowTraceResult_VERDICT_UNSPECIFIED,
		ComputedAt: nowTS(),
	}
	traceLines := make([]string, 0, 8)
	addTrace := func(format string, args ...any) {
		if req.GetVerdictOnly() {
			return
		}
		traceLines = append(traceLines, fmt.Sprintf(format, args...))
	}

	addTrace("INPUT: dir=%s eni=%s src=%s:%d dst=%s:%d proto=%s vni=%s",
		dirName(dir), flow.GetEniName(),
		flow.GetSrcIp(), flow.GetSrcPort(),
		flow.GetDstIp(), flow.GetDstPort(),
		flow.GetProtocol(), flow.GetVni())

	// Stage 1: ACL.
	stage := stageForDirection(dir)
	acls := aclPoliciesForEni(specs, eni.GetName(), stage)
	addTrace("ACL %s: %d candidate policies", stage, len(acls))
	matched, action := evalAclChain(acls, flow, addTrace)
	if matched != nil {
		res.MatchedAclRule = &dashcenterv1.MatchedAclRule{
			PolicyName: matched.policyName,
			Priority:   matched.priority,
			Action:     action,
		}
	}
	if action == aclActionDeny {
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_DROP_ACL
		res.Trace = traceLines
		return res, nil
	}

	// Stage 2: route lookup.
	addTrace("ROUTE: looking up dst=%s on eni=%s", flow.GetDstIp(), eni.GetName())
	route := bestRoute(specs, eni.GetName(), flow.GetDstIp(), addTrace)
	if route == nil {
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_ROUTE
		res.Trace = traceLines
		return res, nil
	}
	res.MatchedRoute = &dashcenterv1.MatchedRoute{
		PolicyName:    route.policyName,
		Prefix:        route.prefix,
		NextHopType:   route.nextHopType,
		NextHopTarget: route.nextHopTarget,
	}

	switch route.nextHopType {
	case "drop":
		addTrace("ROUTE: next_hop=drop → policy drop")
		// proto enum has no DROP_POLICY value; DROP_NO_ROUTE is the
		// closest semantic match (the trace makes the real cause clear).
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_ROUTE
		res.Trace = traceLines
		return res, nil
	case "direct":
		addTrace("ROUTE: next_hop=direct (no encap) → ALLOW")
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_ALLOW
		res.Trace = traceLines
		return res, nil
	case "service_tunnel":
		addTrace("ROUTE: next_hop=service_tunnel target=%s → ENCAP", route.nextHopTarget)
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_ENCAP
		res.Trace = traceLines
		return res, nil
	case "vnet":
		// Continue to vnet-mapping lookup.
	default:
		addTrace("ROUTE: unknown next_hop_type=%q → DROP_INVALID", route.nextHopType)
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_DROP_INVALID
		res.Trace = traceLines
		return res, nil
	}

	// Stage 3: vnet-mapping.
	addTrace("VNET_MAPPING: looking up %s in vnet=%s", flow.GetDstIp(), route.nextHopTarget)
	mapping := lookupVnetMapping(specs, route.nextHopTarget, flow.GetDstIp())
	if mapping == nil {
		addTrace("VNET_MAPPING: no entry for %s in vnet=%s → DROP_NO_MAPPING",
			flow.GetDstIp(), route.nextHopTarget)
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_MAPPING
		res.Trace = traceLines
		return res, nil
	}
	addTrace("VNET_MAPPING: %s → underlay=%s mac=%s action=%s",
		mapping.GetIpAddress(), mapping.GetUnderlayIp(), mapping.GetMacAddress(), mapping.GetAction())
	res.MatchedVnetMapping = &dashcenterv1.MatchedVnetMapping{
		VnetName:  mapping.GetVnetName(),
		IpAddress: mapping.GetIpAddress(),
		Action:    mapping.GetAction(),
	}
	switch mapping.GetAction() {
	case "drop":
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_DROP_NO_MAPPING
	default:
		// vnet_encap (the default), service_tunnel, or any new future
		// action all forward the packet — surface as ENCAP.
		res.Verdict = dashcenterv1.FlowTraceResult_VERDICT_ENCAP
	}
	res.Trace = traceLines
	return res, nil
}

// --- shared helpers (also used by ExplainMatch) ----------------------

// AclAction string constants — kept in this package because the dashd
// AclPolicySpec carries `action` as a free-form string. Any new
// supported action goes here so TraceFlow and ExplainMatch stay in sync.
const (
	aclActionAllow            = "allow"
	aclActionDeny             = "deny"
	aclActionAllowAndContinue = "allow_and_continue"
)

// aclMatch records one matched rule.
type aclMatch struct {
	policyName string
	priority   uint32
	rule       *dashcenterv1.AclRuleSpec
}

// routeMatch records one selected route entry.
type routeMatch struct {
	policyName    string
	prefix        string
	nextHopType   string
	nextHopTarget string
	metric        uint32
	prefixLen     int // for longest-prefix comparison
}

// dirName returns the human label used in the trace output.
func dirName(d dashcenterv1.FlowDescriptor_Direction) string {
	switch d {
	case dashcenterv1.FlowDescriptor_DIRECTION_INBOUND:
		return "INBOUND"
	case dashcenterv1.FlowDescriptor_DIRECTION_OUTBOUND:
		return "OUTBOUND"
	}
	return "UNKNOWN"
}

// stageForDirection maps the proto direction onto the AclPolicy.stage tag.
func stageForDirection(d dashcenterv1.FlowDescriptor_Direction) string {
	if d == dashcenterv1.FlowDescriptor_DIRECTION_OUTBOUND {
		return "outbound"
	}
	return "inbound"
}

// aclPoliciesForEni returns every AclPolicy bound to eniName with the
// requested stage, sorted by name so iteration is deterministic. Rules
// inside each policy retain their declared order; the chain evaluator
// applies priority ordering across policies + rules combined.
func aclPoliciesForEni(specs *placement.DesiredSpecs, eniName, stage string) []*dashcenterv1.AclPolicySpec {
	out := []*dashcenterv1.AclPolicySpec{}
	for _, p := range specs.AclPolicies {
		if !strings.EqualFold(p.GetStage(), stage) {
			continue
		}
		for _, n := range p.GetEniNames() {
			if n == eniName {
				out = append(out, p)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].GetName() < out[j].GetName()
	})
	return out
}

// evalAclChain runs every rule across every supplied policy in
// priority order (lowest priority number wins). Semantics:
//
//   - First matching `deny` rule       → returns (match, "deny").
//   - First matching `allow` rule      → returns (match, "allow").
//   - Matching `allow_and_continue`    → recorded and traced, then
//     evaluation continues with the next-priority rule.
//   - Fall-through (no matching rule)  → returns (nil, "allow") because
//     dashd's default action when no ACL bound matches is to allow
//     (DASH spec: ACL is a deny-list overlay; absence of a deny is allow).
//
// addTrace is called once per evaluated rule so operators can see the
// full reasoning chain in the trace output.
func evalAclChain(policies []*dashcenterv1.AclPolicySpec, flow *dashcenterv1.FlowDescriptor, addTrace func(string, ...any)) (*aclMatch, string) {
	type rulePtr struct {
		policy *dashcenterv1.AclPolicySpec
		rule   *dashcenterv1.AclRuleSpec
	}
	all := make([]rulePtr, 0)
	for _, p := range policies {
		for _, r := range p.GetRules() {
			all = append(all, rulePtr{policy: p, rule: r})
		}
	}
	// Sort by priority asc — smaller priority is more specific.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].rule.GetPriority() != all[j].rule.GetPriority() {
			return all[i].rule.GetPriority() < all[j].rule.GetPriority()
		}
		// Deterministic tie-break: by policy name.
		return all[i].policy.GetName() < all[j].policy.GetName()
	})

	var lastContinue *aclMatch
	for _, rp := range all {
		matched, reason := matchAclRule(rp.rule, flow)
		if !matched {
			addTrace("ACL skip: policy=%s priority=%d action=%s reason=%s",
				rp.policy.GetName(), rp.rule.GetPriority(), rp.rule.GetAction(), reason)
			continue
		}
		m := &aclMatch{policyName: rp.policy.GetName(), priority: rp.rule.GetPriority(), rule: rp.rule}
		action := strings.ToLower(strings.TrimSpace(rp.rule.GetAction()))
		switch action {
		case aclActionDeny:
			addTrace("ACL DENY: policy=%s priority=%d reason=%s",
				rp.policy.GetName(), rp.rule.GetPriority(), reason)
			return m, aclActionDeny
		case aclActionAllow:
			addTrace("ACL ALLOW: policy=%s priority=%d reason=%s",
				rp.policy.GetName(), rp.rule.GetPriority(), reason)
			return m, aclActionAllow
		case aclActionAllowAndContinue, "":
			addTrace("ACL ALLOW_AND_CONTINUE: policy=%s priority=%d reason=%s",
				rp.policy.GetName(), rp.rule.GetPriority(), reason)
			lastContinue = m
			continue
		default:
			addTrace("ACL unknown action %q (policy=%s priority=%d) — skipping",
				rp.rule.GetAction(), rp.policy.GetName(), rp.rule.GetPriority())
			continue
		}
	}
	if lastContinue != nil {
		return lastContinue, aclActionAllow
	}
	addTrace("ACL: no rule matched → default allow")
	return nil, aclActionAllow
}

// matchAclRule evaluates one rule against the flow descriptor. Returns
// (matched, reason) where reason is a single human-readable sentence
// suitable for the trace output.
func matchAclRule(rule *dashcenterv1.AclRuleSpec, flow *dashcenterv1.FlowDescriptor) (bool, string) {
	if ok, _, why := ipInPrefix(flow.GetSrcIp(), rule.GetSrcPrefixes()); !ok {
		return false, "src: " + why
	}
	if ok, _, why := ipInPrefix(flow.GetDstIp(), rule.GetDstPrefixes()); !ok {
		return false, "dst: " + why
	}
	if ok, why := portMatches(flow.GetSrcPort(), rule.GetSrcPorts()); !ok {
		return false, "src_port: " + why
	}
	if ok, why := portMatches(flow.GetDstPort(), rule.GetDstPorts()); !ok {
		return false, "dst_port: " + why
	}
	if ok, why := protoMatches(flow.GetProtocol(), rule.GetProtocols()); !ok {
		return false, "proto: " + why
	}
	return true, "all fields matched"
}

// bestRoute walks every RoutePolicy bound to eniName, finds every
// route whose prefix contains dstIP, and returns the (longest-prefix,
// lowest-metric) winner. Returns nil when no route matches.
func bestRoute(specs *placement.DesiredSpecs, eniName, dstIP string, addTrace func(string, ...any)) *routeMatch {
	dst, err := netip.ParseAddr(strings.TrimSpace(dstIP))
	if err != nil {
		addTrace("ROUTE: invalid dst=%q (%v) → no route", dstIP, err)
		return nil
	}
	var best *routeMatch
	for _, rp := range specs.RoutePolicies {
		bound := false
		for _, n := range rp.GetEniNames() {
			if n == eniName {
				bound = true
				break
			}
		}
		if !bound {
			continue
		}
		for _, r := range rp.GetRoutes() {
			pf, err := netip.ParsePrefix(strings.TrimSpace(r.GetPrefix()))
			if err != nil || !pf.Contains(dst) {
				continue
			}
			cand := &routeMatch{
				policyName:    rp.GetName(),
				prefix:        r.GetPrefix(),
				nextHopType:   r.GetNextHopType(),
				nextHopTarget: r.GetNextHopTarget(),
				metric:        r.GetMetric(),
				prefixLen:     pf.Bits(),
			}
			if best == nil || isBetterRoute(cand, best) {
				best = cand
			}
		}
	}
	if best == nil {
		addTrace("ROUTE: no policy contains %s for eni=%s", dst.String(), eniName)
		return nil
	}
	addTrace("ROUTE: best match policy=%s prefix=%s next_hop=%s/%s metric=%d (len=%d)",
		best.policyName, best.prefix, best.nextHopType, best.nextHopTarget, best.metric, best.prefixLen)
	return best
}

// isBetterRoute applies the DASH route tie-break: longest prefix first,
// then lowest metric. Returns true when cand wins.
func isBetterRoute(cand, cur *routeMatch) bool {
	if cand.prefixLen != cur.prefixLen {
		return cand.prefixLen > cur.prefixLen
	}
	return cand.metric < cur.metric
}

// lookupVnetMapping finds the VnetMapping for (vnetName, overlayIP).
// Returns nil when no entry exists. dashd persists mappings keyed by
// name, not (vnet, ip), so we scan; the manifest sizes (~30 mappings
// in the reference fleet) make this trivially fast.
func lookupVnetMapping(specs *placement.DesiredSpecs, vnetName, overlayIP string) *dashcenterv1.VnetMappingSpec {
	for _, m := range specs.VnetMappings {
		if m.GetVnetName() != vnetName {
			continue
		}
		if strings.TrimSpace(m.GetIpAddress()) == strings.TrimSpace(overlayIP) {
			return m
		}
	}
	return nil
}
