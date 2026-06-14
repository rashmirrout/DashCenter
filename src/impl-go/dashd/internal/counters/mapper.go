// Package counters implements the dashd northbound counter pipeline
// (PE-3b / PE-G9). It ingests the generic per-DPU rollup emitted by
// the sim's dashapi.v1.DashApi.GetDpuCounters RPC, translates it into
// the typed dashcenter.v1.CounterReport that operator tooling
// consumes, and caches the most-recent report per DPU.
//
// Option B in the design spec — the SIM stays domain-agnostic and
// emits a generic shape; dashd is the translator that knows about
// drop reasons, encap counters, HA counters, etc. The mapper here is
// a pure function; it never touches the network and never blocks. The
// poller (poller.go) and store (store.go) layer the live polling and
// caching on top.
//
// The mapper is intentionally conservative:
//
//   - Generic Bucket.PacketsIn/PacketsOut → vxlan_decap/vxlan_encap.
//     Today's sim treats every passed packet as a VXLAN-encapped DASH
//     overlay packet, so the mapping is 1:1. Future scope (#1 in the
//     design doc) adds a typed sim-side hint; until then the mapping
//     is "decap == inbound packets, encap == outbound packets".
//
//   - Generic Bucket.Drops → drop_acl_in. This is a simplification —
//     the sim doesn't differentiate drop reasons yet. PE-3a future
//     scope #5 (per-key drop classification) will let us split into
//     drop_acl_in/out, drop_route_miss, drop_vnet_mapping_miss, etc.
//
//   - flow_table_size = total enumerated scope count. Gives operators
//     a visible "the DPU has work" signal even before we have real
//     flow counters; gets replaced when PE-3c lands true flow stats.
//
//   - sampled_at: prefers the sim-reported sampled_at_ns over wall
//     clock so the same poll round produces consistent timestamps
//     across DPUs. Falls back to time.Now() if the sim omits it.
//
// Mapping decisions live in ONE function so the entire Option B
// translator is reviewable in a single screen. New mappings MUST add
// an explicit comment naming the dashcenter counter field + the sim
// source path, and SHOULD be additive (never delete an existing
// mapping without a Future-Scope row in the design doc).
package counters

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// MapReport converts the generic dashapi DpuCountersResponse from a
// DPU agent (or dash-sim) into a typed dashcenter.v1.CounterReport
// keyed by dpuID. The returned report carries the DPU id supplied to
// the function (NOT the sim's device_id) so that dashd's caller-side
// identifier is authoritative — sims can be misconfigured but dashd's
// inventory is the source of truth.
//
// nil-tolerant: a nil src is treated as an empty response and yields
// an empty report (still annotated with dpuID + now). This matches the
// poller's contract that a single bad poll must not crash the cache.
func MapReport(dpuID string, src *dashapiv1.DpuCountersResponse) *dashcenterv1.CounterReport {
	sampled := time.Now().UTC()
	if src != nil && src.GetSampledAtNs() > 0 {
		sampled = time.Unix(0, src.GetSampledAtNs()).UTC()
	}

	report := &dashcenterv1.CounterReport{
		DpuId:     dpuID,
		SampledAt: timestamppb.New(sampled),
	}

	if src == nil {
		return report
	}

	// DPU-wide bucket → encap / decap + headline drop counter.
	if dpu := src.GetDpu(); dpu != nil {
		report.VxlanDecap = dpu.GetPacketsIn()
		report.VxlanEncap = dpu.GetPacketsOut()
		report.DropAclIn = dpu.GetDrops()
	}

	// Visibility signal: number of distinct scopes the sim is tracking.
	// flow_table_size is the closest stable field in CounterReport for a
	// "how busy is this DPU" indicator until PE-3c ships real flow
	// counters. Using a single field keeps the proto-additive promise.
	report.FlowTableSize = int64(len(src.GetEnis()) + len(src.GetVnets()))

	return report
}

// MapPerEni returns the ENI sub-rollup as typed counters keyed by the
// ENI's scope key (e.g. "eni-001"). The shape mirrors what a future
// CounterReport.per_eni field will hold; today we expose it through
// the store + admin endpoint so operators can already correlate
// per-ENI counts on a single DPU even though the typed proto field
// hasn't been added yet (Future Scope #2 in the design doc).
//
// Empty result for nil/empty input.
func MapPerEni(src *dashapiv1.DpuCountersResponse) map[string]*dashcenterv1.CounterReport {
	if src == nil || len(src.GetEnis()) == 0 {
		return nil
	}
	out := make(map[string]*dashcenterv1.CounterReport, len(src.GetEnis()))
	for _, scoped := range src.GetEnis() {
		if scoped == nil || scoped.GetScopeKey() == "" {
			continue
		}
		out[scoped.GetScopeKey()] = scopedBucketToReport(scoped, src.GetSampledAtNs())
	}
	return out
}

// MapPerVnet mirrors MapPerEni but for the VNET sub-rollup.
func MapPerVnet(src *dashapiv1.DpuCountersResponse) map[string]*dashcenterv1.CounterReport {
	if src == nil || len(src.GetVnets()) == 0 {
		return nil
	}
	out := make(map[string]*dashcenterv1.CounterReport, len(src.GetVnets()))
	for _, scoped := range src.GetVnets() {
		if scoped == nil || scoped.GetScopeKey() == "" {
			continue
		}
		out[scoped.GetScopeKey()] = scopedBucketToReport(scoped, src.GetSampledAtNs())
	}
	return out
}

// scopedBucketToReport converts one ScopedCounters entry into a typed
// CounterReport using the same mapping rules as MapReport. The dpu_id
// field is left empty — callers know which DPU they polled.
func scopedBucketToReport(scoped *dashapiv1.ScopedCounters, sampledNs int64) *dashcenterv1.CounterReport {
	sampled := time.Now().UTC()
	if sampledNs > 0 {
		sampled = time.Unix(0, sampledNs).UTC()
	}
	out := &dashcenterv1.CounterReport{
		SampledAt: timestamppb.New(sampled),
	}
	if b := scoped.GetBucket(); b != nil {
		out.VxlanDecap = b.GetPacketsIn()
		out.VxlanEncap = b.GetPacketsOut()
		out.DropAclIn = b.GetDrops()
	}
	return out
}
