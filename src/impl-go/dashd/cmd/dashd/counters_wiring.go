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
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/observability/broadcaster"
	dashapiv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
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

func (r *restCounterReader) GetDetails(id string) (*restserver.DpuCounterDetails, bool) {
	e, ok := r.store.Get(id)
	if !ok || e == nil {
		return nil, false
	}
	return &restserver.DpuCounterDetails{
		DpuID:    e.DpuID,
		Report:   e.Report,
		PerEni:   e.PerEni,
		PerVnet:  e.PerVnet,
		UpdateAt: e.UpdateAt,
	}, true
}

func (r *restCounterReader) ClearAll() int {
	return r.store.DeleteAll()
}

func (r *restCounterReader) Clear(id string) bool {
	return r.store.Delete(id)
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

// counterResetter implements rest.CounterResetter. It opens a
// short-lived gRPC connection to each target DPU's sim/agent and
// calls ResetDpuCounters. Connections are per-call (not pooled) —
// the operator hits this endpoint rarely; no need to hold open
// connections.
type counterResetter struct {
	inv     *inventory.Inventory
	factory dpuclient.ClientFactory
}

func (cr *counterResetter) ResetDpuCounters(dpuID string) (int, error) {
	entry, err := cr.inv.Get(dpuID)
	if err != nil || entry.Endpoint == "" {
		return 0, fmt.Errorf("DPU %q not in inventory or has no endpoint", dpuID)
	}
	cli, cliErr := cr.factory(entry.Endpoint)
	if cliErr != nil {
		return 0, fmt.Errorf("dial %s: %w", dpuID, cliErr)
	}
	defer cli.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, rpcErr := cli.ResetDpuCounters(ctx, &dashapiv1.ResetDpuCountersRequest{})
	if rpcErr != nil {
		return 0, fmt.Errorf("ResetDpuCounters %s: %w", dpuID, rpcErr)
	}
	return int(resp.GetKeysReset()), nil
}

func (cr *counterResetter) ResetAllDpuCounters() (int, error) {
	dpus := cr.inv.List()
	total := 0
	var lastErr error
	for _, d := range dpus {
		n, err := cr.ResetDpuCounters(d.ID)
		if err != nil {
			slog.Warn("ResetDpuCounters failed", "dpu", d.ID, "error", err)
			lastErr = err
			continue
		}
		total += n
	}
	return total, lastErr
}
