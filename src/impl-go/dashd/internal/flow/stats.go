// GetAclHitStats streams per-(dpu, namespace, policy, stage, rule)
// counter rows. PE-1 ships with NilHitStats by default, which reports
// every rule as "never observed" (hits=0, bytes=0, last_hit_at=nil) —
// that's exactly the right answer for dead-rule detection until PD-G5
// wires the live counter store.
package flow

import (
	"context"
	"sort"
	"strings"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/placement"
)

// GetAclHitStats walks every AclPolicy in the desired state, filters
// by the request's dpu / namespace / policy-name scopes (empty list =
// no filter on that axis), and returns one row per (policy, stage)
// containing per-rule counters drawn from HitStatsSource.
//
// The result is unary (one slice) rather than a true server-stream
// because the service-layer interface is unary and the gRPC adapter
// fans the slice into N stream sends. Keeps the diagnostics logic
// testable without grpc.ServerStream plumbing.
//
// When req.ZeroHitsOnly is true, only rules with hits == 0 across the
// returned DPUs are included — useful for the "find unused ACL rules"
// audit flow.
func (e *Engine) GetAclHitStats(ctx context.Context, req *dashcenterv1.AclStatsRequest) ([]*dashcenterv1.AclStatsPerDpu, error) {
	if req == nil {
		return nil, invArgf("request is nil")
	}
	specs, err := e.loadView(ctx)
	if err != nil {
		return nil, err
	}

	dpuFilter := stringSet(req.GetDpuIds())
	nsFilter := stringSet(req.GetNamespaces())
	policyFilter := stringSet(req.GetPolicyNames())

	// Determine the DPU list to fan over. If the caller specified one,
	// honour it verbatim; otherwise use every DPU bound to any policy
	// in scope (the union of placement_hint_dpu_ids referenced via the
	// rules' eni_names → eni.placement_hint_dpu_ids).
	dpus := dpuFilter
	if len(dpus) == 0 {
		dpus = inferDpus(specs, nsFilter, policyFilter)
	}

	// Sort for determinism.
	dpuList := make([]string, 0, len(dpus))
	for d := range dpus {
		dpuList = append(dpuList, d)
	}
	sort.Strings(dpuList)

	// Sort policies by name.
	polNames := make([]string, 0, len(specs.AclPolicies))
	for n := range specs.AclPolicies {
		polNames = append(polNames, n)
	}
	sort.Strings(polNames)

	out := make([]*dashcenterv1.AclStatsPerDpu, 0)
	for _, dpu := range dpuList {
		for _, polName := range polNames {
			pol := specs.AclPolicies[polName]
			ns := pol.GetNamespace()
			if ns == "" {
				ns = "default"
			}
			if len(nsFilter) > 0 && !nsFilter[ns] {
				continue
			}
			if len(policyFilter) > 0 && !policyFilter[polName] {
				continue
			}

			rows := make([]*dashcenterv1.AclRuleHit, 0, len(pol.GetRules()))
			anyHit := false
			for _, r := range pol.GetRules() {
				hits, bytes, lastNs, _ := e.hits.AclHits(dpu, ns, polName, pol.GetStage(), r.GetPriority())
				if hits > 0 {
					anyHit = true
				}
				rows = append(rows, &dashcenterv1.AclRuleHit{
					Priority:  r.GetPriority(),
					Action:    r.GetAction(),
					Hits:      hits,
					Bytes:     bytes,
					LastHitAt: tsFromUnixNanos(lastNs),
				})
			}
			if req.GetZeroHitsOnly() && anyHit {
				// Drop the whole policy row when any rule has hits —
				// the operator asked specifically for dead-only policies.
				continue
			}
			if len(rows) == 0 {
				continue
			}
			out = append(out, &dashcenterv1.AclStatsPerDpu{
				DpuId:      dpu,
				Namespace:  ns,
				PolicyName: polName,
				Stage:      strings.ToLower(strings.TrimSpace(pol.GetStage())),
				Rules:      rows,
				SampledAt:  nowTS(),
			})
		}
	}
	return out, nil
}

// stringSet returns a lowercase-trimmed set; nil/empty input yields
// the empty set (filter disabled).
func stringSet(in []string) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out[s] = true
		}
	}
	return out
}

// inferDpus returns the union of placement_hint_dpu_ids across every
// ENI bound to any policy in scope. Falls back to the empty set —
// callers should treat that as "no DPUs in scope; emit nothing".
func inferDpus(specs *placement.DesiredSpecs, nsFilter, policyFilter map[string]bool) map[string]bool {
	dpus := make(map[string]bool)
	for polName, pol := range specs.AclPolicies {
		ns := pol.GetNamespace()
		if ns == "" {
			ns = "default"
		}
		if len(nsFilter) > 0 && !nsFilter[ns] {
			continue
		}
		if len(policyFilter) > 0 && !policyFilter[polName] {
			continue
		}
		for _, eniName := range pol.GetEniNames() {
			eni, ok := specs.Enis[eniName]
			if !ok {
				continue
			}
			for _, dpu := range eni.GetPlacementHintDpuIds() {
				if dpu != "" {
					dpus[dpu] = true
				}
			}
		}
	}
	return dpus
}
