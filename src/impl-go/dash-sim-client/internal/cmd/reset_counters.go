// reset_counters.go ships the `dash-sim-client reset-counters` subcommand
// (PE-3c add-on) — zeroes every per-object counter accumulator on the
// target sim/DPU WITHOUT disturbing programmed objects (ENIs, VNETs,
// policies, etc.).
//
// Usage:
//
//	dash-sim-client reset-counters                  # reset all accumulators
//	dash-sim-client reset-counters -o json          # machine-readable output
//
// This is the southbound counterpart of
// `dashctl counters clear --reset-sim` which cascades through dashd.
// Calling `dash-sim-client reset-counters` directly bypasses dashd and
// talks to the sim over the vendor gRPC proto.

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newResetCountersCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "reset-counters",
		Short: "Zero every counter accumulator on the DPU/sim (PE-3c)",
		Long: `Call dashapi.v1.DashApi.ResetDpuCounters to zero every per-object
counter accumulator without deleting any programmed objects (ENIs,
VNETs, policies, etc.).

After reset, the next GetDpuCounters / dpu-counters call will show
values near zero (reflecting only the few ticks that occurred between
the reset and the poll).

This is the direct sim/DPU call. For an operator-level clear that also
wipes dashd's cache, use: dashctl counters clear --reset-sim

Examples:
  dash-sim-client reset-counters                  # reset, table output
  dash-sim-client reset-counters -o json          # JSON output
  dash-sim-client reset-counters -o yaml          # YAML output`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			resp, err := cl.ResetDpuCounters(ctx)
			if err != nil {
				return err
			}
			fmtOut, fmtErr := resolveFormat()
			if fmtErr != nil {
				return fmtErr
			}
			keysReset := resp.GetKeysReset()
			switch fmtOut {
			case "json":
				b, _ := json.MarshalIndent(map[string]any{
					"keys_reset": keysReset,
				}, "", "  ")
				fmt.Fprintln(os.Stdout, string(b))
			case "yaml":
				b, _ := yaml.Marshal(map[string]any{
					"keys_reset": keysReset,
				})
				fmt.Fprint(os.Stdout, string(b))
			default:
				fmt.Fprintf(os.Stdout, "reset %d counter accumulator key(s)\n", keysReset)
			}
			return nil
		},
	}
	return c
}
