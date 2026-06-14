// counters_wiring.go — small adapter glue between counters.Store and
// the three CounterReader interfaces declared by sibling packages
// (rest, grpcserver, broadcaster). Counter store types are themselves
// generic; the per-package interfaces exist only to keep packages
// from cyclically depending on counters. The adapters live in main
// because that's the only place all four packages meet.
//
// Two adapters (rest + grpc) wrap counters.Store and expose the
// matching DpuCounterEntry list. A third adapter (counterStoreAdapter)
// implements broadcaster.CounterStore so the Bridge can subscribe to
// store change events.

package main

import (
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/observability/broadcaster"
	grpcserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/grpc"
	restserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/rest"
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// restCounterReader implements rest.CounterReader.
type restCounterReader struct {
	store *counters.Store
}

func (r *restCounterReader) ListReports() []restserver.DpuCounterEntry {
	entries := r.store.List()
	out := make([]restserver.DpuCounterEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil || e.Report == nil {
			continue
		}
		out = append(out, restserver.DpuCounterEntry{DpuID: e.DpuID, Report: e.Report})
	}
	return out
}

func (r *restCounterReader) GetReport(id string) (*dashcenterv1.CounterReport, bool) {
	return r.store.GetReport(id)
}

// grpcCounterReader implements grpcserver.CounterReader.
type grpcCounterReader struct {
	store *counters.Store
}

func (r *grpcCounterReader) ListReports() []grpcserver.DpuCounterEntry {
	entries := r.store.List()
	out := make([]grpcserver.DpuCounterEntry, 0, len(entries))
	for _, e := range entries {
		if e == nil || e.Report == nil {
			continue
		}
		out = append(out, grpcserver.DpuCounterEntry{DpuID: e.DpuID, Report: e.Report})
	}
	return out
}

func (r *grpcCounterReader) GetReport(id string) (*dashcenterv1.CounterReport, bool) {
	return r.store.GetReport(id)
}

// counterStoreAdapter implements broadcaster.CounterStore by
// forwarding to counters.Store. Defined here (not in counters) so the
// counters package stays free of any broadcaster dependency — keeps
// PE-3b clean for future packagers.
type counterStoreAdapter struct {
	store *counters.Store
}

func (a *counterStoreAdapter) Subscribe(ch chan<- string) (cancel func()) {
	return a.store.Subscribe(ch)
}

func (a *counterStoreAdapter) GetReport(id string) (*dashcenterv1.CounterReport, bool) {
	return a.store.GetReport(id)
}

// counterStreamConfigFrom translates config.StreamConfig into the
// shape broadcaster.NewBroadcaster expects. Pure mechanical mapping;
// the validation already ran upstream (config.Validate).
func counterStreamConfigFrom(s config.StreamConfig) broadcaster.Config {
	_ = time.Duration(0) // silence unused-import noise across refactors
	return broadcaster.Config{
		MaxSubscribers:           s.MaxSubscribers,
		MaxSubscribersPerSubject: s.MaxSubscribersPerSubject,
		SubscriberBufferSize:     s.SubscriberBufferSize,
		RingSize:                 s.RingSize,
		CoalesceWindow:           s.CoalesceWindow,
		EventRatePerSec:          s.RateLimitPerSecond,
		BurstSize:                s.RateLimitBurst,
		KeepaliveInterval:        s.KeepaliveInterval,
	}
}
