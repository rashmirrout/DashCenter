// dpu_counters.go implements dashapi.v1.DashApi.GetDpuCounters (PE-3a /
// PE-G8). The handler is split from service.go so the rollup logic is
// inspectable in one file alongside its unit tests; the original
// service.go keeps the lean per-RPC entry points.
//
// Design contract — see proto/dashapi/v1/dashapi.proto comments around
// DpuCountersRequest / DpuCountersResponse:
//
//   1. The DPU-wide bucket is ALWAYS populated by summing every key
//      tracked by counters.Registry (regardless of which kinds the
//      keys belong to).
//   2. Per-ENI rollups are opt-in (request.include_enis). When opted in
//      we enumerate the ENI scope keys from the model.Store
//      (OBJECT_KIND_ENI) and sum every counter key whose first joined
//      component matches the ENI name. Optionally filtered by
//      request.eni_names (intersection).
//   3. Per-VNET rollups are opt-in (request.include_vnets) and follow
//      the same enumeration pattern using OBJECT_KIND_VNET.
//   4. Fault injection: operators can inject latency/error on the
//      "GetDpuCounters" op name to test dashd's polling resilience
//      (parity with every other RPC on the sim).
//
// The handler ignores empty filters (returns every scope) and skips
// scopes that don't exist in the store (returns empty Bucket per scope
// per the RollupAll contract in counters/rollup.go).
//
// Per-flow + per-DPU-namespace scopes are NOT in this RPC — see
// docs/dashd-features/dash-sim-counter-rollups.md §11 Future Scopes
// for the rationale and the scalability constraints we'd need to
// solve first.

package server

import (
	"context"
	"sort"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetDpuCounters implements DashApi.GetDpuCounters.
func (s *Server) GetDpuCounters(_ context.Context, req *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error) {
	if err := s.faults.Apply("GetDpuCounters"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	resp := &dashapi.DpuCountersResponse{
		DeviceId:    s.deviceID,
		SampledAtNs: nowNs(),
		Dpu:         bucketToProto(s.counters.TotalBucket()),
	}

	keysByKind := s.store.AllKeys()

	if req.GetIncludeEnis() {
		resp.Enis = scopedRollups(s.counters,
			scopesForKind(keysByKind, dashapi.ObjectKind_OBJECT_KIND_ENI),
			req.GetEniNames())
	}
	if req.GetIncludeVnets() {
		resp.Vnets = scopedRollups(s.counters,
			scopesForKind(keysByKind, dashapi.ObjectKind_OBJECT_KIND_VNET),
			req.GetVnetKeys())
	}
	return resp, nil
}

// scopesForKind extracts every joined-key for the given kind from the
// store snapshot, returning them deterministically sorted. A scope key
// missing from the store but requested by the caller would still be
// served by scopedRollups (as an empty bucket); this function only
// supplies the *enumerated* set.
func scopesForKind(keysByKind map[dashapi.ObjectKind][]string, kind dashapi.ObjectKind) []string {
	out := append([]string(nil), keysByKind[kind]...)
	sort.Strings(out)
	return out
}

// scopedRollups walks `enumerated` and any extra `requestedFilter` scopes,
// fetches each Bucket via counters.Rollup, and returns the result as a
// sorted []*ScopedCounters. If `requestedFilter` is empty, every
// enumerated scope is returned. If `requestedFilter` is non-empty, only
// the intersection (filter ∩ enumerated) + any explicitly-requested-but-
// missing scope (with an empty bucket) is returned — preserves the
// caller's "I want to see this scope even if it has no data" intent.
func scopedRollups(reg *counters.Registry, enumerated, requestedFilter []string) []*dashapi.ScopedCounters {
	var scopes []string
	if len(requestedFilter) == 0 {
		scopes = enumerated
	} else {
		// Build a set of enumerated for O(1) lookup; preserve the
		// filter's deduped, sorted order in the result.
		known := make(map[string]struct{}, len(enumerated))
		for _, s := range enumerated {
			known[s] = struct{}{}
		}
		seen := make(map[string]struct{}, len(requestedFilter))
		for _, s := range requestedFilter {
			if s == "" {
				continue
			}
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			// Always include the requested scope — even if it's
			// not in the store. counters.Rollup returns zero bucket
			// for unknown scopes; the operator gets a visible "I
			// asked for this and got nothing" signal.
			_ = known // we no longer gate on `known` (kept for future audit)
			scopes = append(scopes, s)
		}
		sort.Strings(scopes)
	}

	out := make([]*dashapi.ScopedCounters, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, &dashapi.ScopedCounters{
			ScopeKey: s,
			Bucket:   bucketToProto(reg.Rollup(s)),
		})
	}
	return out
}

// bucketToProto converts the counters.Bucket value type into the
// generated *dashapi.CounterBucket proto. Centralised so the handler
// + tests share one converter.
func bucketToProto(b counters.Bucket) *dashapi.CounterBucket {
	return &dashapi.CounterBucket{
		PacketsIn:  b.PacketsIn,
		PacketsOut: b.PacketsOut,
		BytesIn:    b.BytesIn,
		BytesOut:   b.BytesOut,
		Drops:      b.Drops,
	}
}

// ResetDpuCounters implements DashApi.ResetDpuCounters.
// Zeroes every counter accumulator on this sim without touching the
// programmed objects (ENIs, VNETs, policies, etc.). The next
// GetDpuCounters call will see fresh-from-zero values.
func (s *Server) ResetDpuCounters(_ context.Context, _ *dashapi.ResetDpuCountersRequest) (*dashapi.ResetDpuCountersResponse, error) {
	if err := s.faults.Apply("ResetDpuCounters"); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	n := s.counters.ResetAll()
	return &dashapi.ResetDpuCountersResponse{KeysReset: int32(n)}, nil
}
