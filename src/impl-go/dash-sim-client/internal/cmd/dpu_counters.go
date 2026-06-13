// dpu_counters.go ships the `dash-sim-client dpu-counters` subcommand
// (PE-3a / PE-G8) — operator inspector for dashapi.v1.GetDpuCounters.
//
// Three operator workflows in one command:
//
//   dash-sim-client dpu-counters                        # one-shot, table
//   dash-sim-client dpu-counters --include-enis -o json # script-friendly
//   dash-sim-client dpu-counters --watch --interval 2s  # live tail
//   dash-sim-client dpu-counters -o csv | tee out.csv   # spreadsheet
//
// Watch mode prints one snapshot per interval until SIGINT/SIGTERM —
// reuses the existing streamContext helper for Ctrl-C handling.
//
// This command is intentionally simple — no resume cursors, no
// fan-out semantics. Those concerns live at the dashd layer and are
// out of PE-3a's scope (see PE-3c for the dashd-side `dashctl counters
// --follow` that uses the broadcaster pattern).

package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newDpuCountersCmd() *cobra.Command {
	var (
		includeEnis  bool
		includeVnets bool
		eniNames     []string
		vnetKeys     []string
		watch        bool
		interval     time.Duration
	)
	c := &cobra.Command{
		Use:   "dpu-counters",
		Short: "Inspect typed per-DPU / per-ENI / per-VNET counter rollups (PE-3a)",
		Long: `Fetch dashapi.v1.GetDpuCounters and render the typed counter rollup.

Without --watch the command emits a single snapshot. With --watch it
prints a fresh snapshot every --interval (default 1s) until Ctrl-C.

Per-ENI and per-VNET sections are opt-in to keep responses small when
the operator only needs the DPU-wide bucket.

Output formats:

  -o table   pretty ASCII tables (default for interactive sessions)
  -o json    full nested envelope (scripting / piping)
  -o yaml    same content as json, friendlier for inline edits
  -o csv     one row per scope (dpu, then each eni, then each vnet) —
             ideal for piping into a spreadsheet

The new --format flag mirrors -o; either works.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmtOut, err := render.ParseFormatExt(flagOutput)
			if err != nil {
				return err
			}
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			req := &dashapi.DpuCountersRequest{
				IncludeEnis:  includeEnis,
				IncludeVnets: includeVnets,
				EniNames:     eniNames,
				VnetKeys:     vnetKeys,
			}
			if !watch {
				ctx, cancel := rpcContext()
				defer cancel()
				resp, err := cl.GetDpuCounters(ctx, req)
				if err != nil {
					return err
				}
				return render.DpuCounters(os.Stdout, fmtOut, resp)
			}
			// Watch mode — Ctrl-C cancels the long-lived loop ctx.
			ctx, cancel := streamContext()
			defer cancel()
			return watchDpuCounters(ctx, cl, req, fmtOut, interval, os.Stdout)
		},
	}
	c.Flags().BoolVar(&includeEnis, "include-enis", false, "include per-ENI rollups")
	c.Flags().BoolVar(&includeVnets, "include-vnets", false, "include per-VNET rollups")
	c.Flags().StringSliceVar(&eniNames, "eni-names", nil, "comma-separated ENI scope keys to filter on (implies --include-enis)")
	c.Flags().StringSliceVar(&vnetKeys, "vnet-keys", nil, "comma-separated VNET scope keys to filter on (implies --include-vnets)")
	c.Flags().BoolVar(&watch, "watch", false, "stream periodic snapshots until Ctrl-C")
	c.Flags().DurationVar(&interval, "interval", time.Second, "snapshot interval in watch mode")
	// --eni-names / --vnet-keys implicitly enable include — make that visible
	// to the user via the PreRunE hook.
	c.PreRunE = func(_ *cobra.Command, _ []string) error {
		if len(eniNames) > 0 {
			includeEnis = true
		}
		if len(vnetKeys) > 0 {
			includeVnets = true
		}
		if watch && interval <= 0 {
			return fmt.Errorf("--interval must be > 0 when --watch is set")
		}
		return nil
	}
	return c
}

// watchDpuCounters drives the polling loop. The first tick fires immediately
// so the operator sees data without waiting for `interval`.
//
// Each iteration uses a fresh per-RPC context bounded by flagTimeout so a
// hung sim never blocks the watch loop indefinitely. RPC errors are printed
// to stderr but do NOT exit the loop — counters streaming is "best-effort
// continuous" by design (see dash-sim's faults injection scenarios).
func watchDpuCounters(
	ctx context.Context,
	cl interface {
		GetDpuCounters(context.Context, *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error)
	},
	req *dashapi.DpuCountersRequest,
	fmtOut render.Format,
	interval time.Duration,
	out *os.File,
) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	// First snapshot immediately.
	if err := oneWatchTick(ctx, cl, req, fmtOut, out); err != nil && ctx.Err() != nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := oneWatchTick(ctx, cl, req, fmtOut, out); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				_, _ = fmt.Fprintf(os.Stderr, "dpu-counters: rpc error: %v\n", err)
				// fall through — keep watching
			}
		}
	}
}

func oneWatchTick(
	ctx context.Context,
	cl interface {
		GetDpuCounters(context.Context, *dashapi.DpuCountersRequest) (*dashapi.DpuCountersResponse, error)
	},
	req *dashapi.DpuCountersRequest,
	fmtOut render.Format,
	out *os.File,
) error {
	rpcCtx, cancel := context.WithTimeout(ctx, flagTimeout)
	defer cancel()
	resp, err := cl.GetDpuCounters(rpcCtx, req)
	if err != nil {
		return err
	}
	// Separator between successive snapshots so the operator can tell
	// them apart in scrollback (no-op for the very first tick — caller
	// sees the header line as the first thing on screen).
	_, _ = fmt.Fprintln(out, "----")
	return render.DpuCounters(out, fmtOut, resp)
}
