// Package cmd: `dashctl counters` — operator-facing CLI for the
// PE-3c counter streaming surface.
//
// Modes:
//
//	dashctl counters                One-shot snapshot of every DPU's
//	                                counters; pretty table by default,
//	                                `-o json|csv|yaml` for machine use.
//	dashctl counters --follow       Live stream of CounterEvent frames.
//	dashctl counters --dpu=ID...    Filter to one or more DPUs.
//
// Backend selection (`--backend`):
//
//	rest  (default)  SSE through dashd's REST surface.
//	grpc             Real gRPC stream against ObservabilityService.
//	                 Useful when an operator needs the lower-latency
//	                 stream path or wants to bypass any reverse proxy.
//
// The command goes through dashd, NOT dashw. dashw is the BFF for
// browsers; the operator CLI talks directly to the cluster.

package cmd

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	grpcclient "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client/grpc"
)

func (a *Application) newCountersCmd() *cobra.Command {
	var (
		follow   bool
		dpuIDs   []string
		sinceID  uint64
		jsonOut  bool
		csvOut   bool
		backend  string
		grpcEP   string
	)
	c := &cobra.Command{
		Use:   "counters",
		Short: "Show per-DPU counter snapshots (one-shot) or live stream (--follow)",
		Long: `Show per-DPU counter data served by dashd's ObservabilityService.

Without --follow, prints a one-shot snapshot. With --follow, opens a
long-lived stream and prints each CounterEvent (snapshot, report,
keepalive, dropped, rate_limited, resync) as it arrives.

Examples:
  dashctl counters                                 # table snapshot
  dashctl counters -o json                         # machine-readable
  dashctl counters -o csv > counters.csv           # spreadsheet feed
  dashctl counters --dpu=dpu-1 --dpu=dpu-3         # filter
  dashctl counters --follow                        # SSE live stream
  dashctl counters --follow --backend=grpc         # gRPC live stream
  dashctl counters --follow --since-id=42          # resume after cursor
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			// Validate backend selector.
			switch backend {
			case "", "rest":
				backend = "rest"
			case "grpc":
				// ok
			default:
				return fmt.Errorf("counters: unsupported --backend=%q (rest|grpc)", backend)
			}
			// gRPC backend doesn't share the REST factory; build it
			// inline from the resolved config endpoint.
			if backend == "grpc" {
				return a.runCountersGRPC(ctx, dpuIDs, follow, sinceID, jsonOut, csvOut, grpcEP)
			}
			cl, rc, err := a.dial(ctx)
			if err != nil {
				return err
			}
			defer cl.Close()
			if follow {
				return a.runCountersFollowREST(ctx, cl, dpuIDs, sinceID, jsonOut, csvOut)
			}
			tctx, cancel := withTimeout(ctx, rc)
			defer cancel()
			return a.runCountersSnapshotREST(tctx, cl, dpuIDs, jsonOut, csvOut)
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "stream live events until Ctrl-C")
	c.Flags().StringSliceVar(&dpuIDs, "dpu", nil, "filter to one or more DPU ids (repeatable)")
	c.Flags().Uint64Var(&sinceID, "since-id", 0, "resume the stream after this event_id")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit raw JSON (snapshot or each event)")
	c.Flags().BoolVar(&csvOut, "csv", false, "emit CSV (snapshot only)")
	c.Flags().StringVar(&backend, "backend", "rest", "transport backend: rest (SSE, default) | grpc")
	c.Flags().StringVar(&grpcEP, "grpc-endpoint", "", "host:port for --backend=grpc (default: REST endpoint host with port 9443)")
	return c
}

// ── snapshot (REST) ─────────────────────────────────────────────────────

func (a *Application) runCountersSnapshotREST(ctx context.Context, cl client.Client, dpuIDs []string, jsonOut, csvOut bool) error {
	snap, err := cl.GetCountersSnapshot(ctx, dpuIDs)
	if err != nil {
		return err
	}
	return renderCountersSnapshot(snap, jsonOut, csvOut)
}

// ── follow (REST) ───────────────────────────────────────────────────────

func (a *Application) runCountersFollowREST(ctx context.Context, cl client.Client, dpuIDs []string, sinceID uint64, jsonOut, csvOut bool) error {
	enc := json.NewEncoder(os.Stdout)
	opts := client.CountersWatchOptions{
		DpuIDs:      dpuIDs,
		LastEventID: sinceID,
		OnEvent: func(ev client.CounterEvent) error {
			return renderCounterEvent(enc, ev, jsonOut, csvOut)
		},
	}
	return cl.StreamCounters(ctx, opts)
}

// ── follow (gRPC) + snapshot (gRPC) ─────────────────────────────────────

func (a *Application) runCountersGRPC(ctx context.Context, dpuIDs []string, follow bool, sinceID uint64, jsonOut, csvOut bool, grpcEP string) error {
	if grpcEP == "" {
		// Default: take the configured REST endpoint and swap to gRPC port.
		// Operators with non-default ports MUST supply --grpc-endpoint.
		_, rc, err := a.dial(ctx)
		if err != nil {
			return err
		}
		ep, perr := guessGrpcFromRest(rc.Endpoint)
		if perr != nil {
			return fmt.Errorf("counters: cannot derive grpc endpoint from %q; pass --grpc-endpoint explicitly: %w", rc.Endpoint, perr)
		}
		grpcEP = ep
	}
	cl, err := grpcclient.NewCountersClient(ctx, grpcclient.DialOptions{
		Endpoint:    grpcEP,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return err
	}
	defer cl.Close()

	if !follow {
		snap, err := cl.GetCountersSnapshot(ctx, dpuIDs)
		if err != nil {
			return err
		}
		return renderCountersSnapshot(snap, jsonOut, csvOut)
	}
	enc := json.NewEncoder(os.Stdout)
	return cl.StreamCounters(ctx, client.CountersWatchOptions{
		DpuIDs:      dpuIDs,
		LastEventID: sinceID,
		OnEvent: func(ev client.CounterEvent) error {
			return renderCounterEvent(enc, ev, jsonOut, csvOut)
		},
	})
}

// guessGrpcFromRest parses a REST endpoint URL (http[s]://host:port)
// and returns the inferred gRPC endpoint (host:gport). Convention:
// gport = rport + 1000 (8443→9443, 28443→29443, ...). Operators with
// custom layouts MUST pass --grpc-endpoint explicitly.
func guessGrpcFromRest(restURL string) (string, error) {
	u, err := url.Parse(restURL)
	if err != nil {
		return "", err
	}
	host := u.Hostname()
	portStr := u.Port()
	if host == "" || portStr == "" {
		return "", fmt.Errorf("missing host or port in %q", restURL)
	}
	rport, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("non-numeric port %q", portStr)
	}
	return fmt.Sprintf("%s:%d", host, rport+1000), nil
}

// ── rendering ───────────────────────────────────────────────────────────

func renderCountersSnapshot(snap *client.CountersSnapshot, jsonOut, csvOut bool) error {
	if snap == nil {
		snap = &client.CountersSnapshot{}
	}
	if jsonOut {
		return json.NewEncoder(os.Stdout).Encode(snap)
	}
	if csvOut {
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		if err := w.Write([]string{"dpu_id", "sampled_at", "vxlan_decap", "vxlan_encap", "drop_acl_in", "drop_acl_out", "drop_other", "flows_created_total", "flow_table_size"}); err != nil {
			return err
		}
		// Stable order.
		reps := append([]*client.CounterReport(nil), snap.Reports...)
		sort.Slice(reps, func(i, j int) bool { return reps[i].DpuId < reps[j].DpuId })
		for _, r := range reps {
			if err := w.Write([]string{
				r.DpuId, r.SampledAt,
				orZero(r.VxlanDecap), orZero(r.VxlanEncap),
				orZero(r.DropAclIn), orZero(r.DropAclOut), orZero(r.DropOther),
				orZero(r.FlowsCreatedTotal), orZero(r.FlowTableSize),
			}); err != nil {
				return err
			}
		}
		return nil
	}
	// Pretty table.
	return renderCountersTable(os.Stdout, snap)
}

func renderCountersTable(w *os.File, snap *client.CountersSnapshot) error {
	fmt.Fprintf(w, "DPU                  SAMPLED                          DECAP        ENCAP        DROP_IN      DROP_OUT     FLOWS\n")
	reps := append([]*client.CounterReport(nil), snap.Reports...)
	sort.Slice(reps, func(i, j int) bool { return reps[i].DpuId < reps[j].DpuId })
	for _, r := range reps {
		fmt.Fprintf(w, "%-20s %-32s %-12s %-12s %-12s %-12s %-12s\n",
			truncate(r.DpuId, 20), truncate(r.SampledAt, 32),
			orZero(r.VxlanDecap), orZero(r.VxlanEncap),
			orZero(r.DropAclIn), orZero(r.DropAclOut),
			orZero(r.FlowsCreatedTotal),
		)
	}
	return nil
}

func renderCounterEvent(enc *json.Encoder, ev client.CounterEvent, jsonOut, csvOut bool) error {
	if jsonOut {
		return enc.Encode(ev)
	}
	if csvOut {
		// CSV in follow mode is awkward; degrade to one CSV row per
		// report frame, ignore sentinels.
		if ev.Report == nil {
			return nil
		}
		w := csv.NewWriter(os.Stdout)
		defer w.Flush()
		return w.Write([]string{
			ev.Kind, fmt.Sprintf("%d", ev.EventID),
			ev.Report.DpuId, ev.Report.SampledAt,
			orZero(ev.Report.VxlanDecap), orZero(ev.Report.VxlanEncap),
			orZero(ev.Report.DropAclIn), orZero(ev.Report.DropAclOut),
			orZero(ev.Report.FlowsCreatedTotal),
		})
	}
	// Pretty one-liner.
	switch strings.ToUpper(ev.Kind) {
	case "KIND_SNAPSHOT", "KIND_REPORT":
		if ev.Report == nil {
			fmt.Printf("[%s id=%d] (empty report)\n", ev.Kind, ev.EventID)
			return nil
		}
		fmt.Printf("[%s id=%d] dpu=%s decap=%s encap=%s drop_in=%s flows=%s\n",
			ev.Kind, ev.EventID, ev.Report.DpuId,
			orZero(ev.Report.VxlanDecap), orZero(ev.Report.VxlanEncap), orZero(ev.Report.DropAclIn), orZero(ev.Report.FlowsCreatedTotal))
	case "KIND_KEEPALIVE":
		fmt.Printf("[%s id=%d] %s\n", ev.Kind, ev.EventID, msgOf(ev.Notice))
	case "KIND_DROPPED":
		fmt.Printf("[%s id=%d] dropped=%d %s\n", ev.Kind, ev.EventID, droppedOf(ev.Notice), msgOf(ev.Notice))
	case "KIND_RATE_LIMITED":
		fmt.Printf("[%s id=%d] suppressed=%d %s\n", ev.Kind, ev.EventID, suppressedOf(ev.Notice), msgOf(ev.Notice))
	case "KIND_RESYNC":
		fmt.Printf("[%s id=%d] current_event_id=%d %s\n", ev.Kind, ev.EventID, currentOf(ev.Notice), msgOf(ev.Notice))
	default:
		fmt.Printf("[%s id=%d]\n", ev.Kind, ev.EventID)
	}
	return nil
}

// helpers

// orZero returns "0" when s is empty; otherwise s. Used because
// protojson omits int64 fields that are zero, so the dashctl-side
// struct receives empty strings for unset counters.
func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
func msgOf(n *client.TopologyNotice) string {
	if n == nil {
		return ""
	}
	return n.Message
}
func droppedOf(n *client.TopologyNotice) uint64 {
	if n == nil {
		return 0
	}
	return n.DroppedCount
}
func suppressedOf(n *client.TopologyNotice) uint64 {
	if n == nil {
		return 0
	}
	return n.SuppressedCount
}
func currentOf(n *client.TopologyNotice) uint64 {
	if n == nil {
		return 0
	}
	return n.CurrentEventID
}
