package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/render"
)

func (a *Application) newDpuCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "dpu",
		Short: "Manage and inspect DPUs",
	}
	c.AddCommand(
		a.newDpuListCmd(),
		a.newDpuStatusCmd(),
		a.newDpuDriftCmd(),
		a.newDpuDescribeCmd(),
		a.newDpuCordonCmd(),
		a.newDpuUncordonCmd(),
		a.newDpuDrainCmd(),
	)
	return c
}

func (a *Application) newDpuListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Snapshot inventory + state for every DPU",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			h, err := cl.Health(ctx)
			if err != nil {
				return err
			}
			return a.renderInventory(h.Dpus)
		},
	}
}

func (a *Application) newDpuStatusCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Snapshot DPU status (Phase 1) — streaming added in Phase 2",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			h, err := cl.Health(ctx)
			if err != nil {
				return err
			}
			return a.renderInventory(h.Dpus)
		},
	}
	return c
}

func (a *Application) newDpuDriftCmd() *cobra.Command {
	var dpu string
	c := &cobra.Command{
		Use:   "drift",
		Short: "Show declared-vs-observed drift for a DPU",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dpu == "" {
				return errors.New(errors.CodeInvalidArgument, "drift: --dpu is required")
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			items, err := cl.AdminDrift(ctx, dpu)
			if err != nil {
				return err
			}
			if len(items) == 0 {
				fmt.Fprintln(a.Out, "0 drift items.")
				return nil
			}
			rows := make([]any, 0, len(items))
			for _, it := range items {
				rows = append(rows, it)
			}
			return render.Render(a.Out, rows, render.Options{Format: render.FormatTable, Columns: render.DriftColumns()})
		},
	}
	c.Flags().StringVar(&dpu, "dpu", "", "DPU id to inspect (required)")
	return c
}

func (a *Application) newDpuDescribeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "describe <dpu-id>",
		Short: "Inventory + observed + drift summary for one DPU",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dpu := args[0]
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()

			h, err := cl.Health(ctx)
			if err != nil {
				return err
			}
			var found *struct {
				ID, Endpoint, State, LastSeen string
				Labels                        map[string]string
			}
			for _, d := range h.Dpus {
				if d.ID == dpu {
					found = &struct {
						ID, Endpoint, State, LastSeen string
						Labels                        map[string]string
					}{d.ID, d.Endpoint, d.State, d.LastSeen, d.Labels}
					break
				}
			}
			if found == nil {
				return errors.Newf(errors.CodeNotFound, "describe: dpu %q not found", dpu)
			}
			drift, dErr := cl.AdminDrift(ctx, dpu)
			fmt.Fprintf(a.Out, "Name:        %s\nEndpoint:    %s\nState:       %s\nLast seen:   %s\nLabels:      %s\n",
				found.ID, found.Endpoint, found.State, found.LastSeen, formatLabelsMap(found.Labels))
			if dErr != nil {
				fmt.Fprintf(a.Out, "Drift:       <unavailable: %v>\n", dErr)
				return nil
			}
			fmt.Fprintf(a.Out, "Drift:       %d item(s)\n", len(drift))
			for _, it := range drift {
				fmt.Fprintf(a.Out, "  %s %s %s %s\n", it.Op, it.Kind, it.Key, it.Detail)
			}
			return nil
		},
	}
	return c
}

func (a *Application) newDpuCordonCmd() *cobra.Command {
	return phase2Stub("cordon", "exclude a DPU from new ENI placements (Phase 2)")
}

func (a *Application) newDpuUncordonCmd() *cobra.Command {
	return phase2Stub("uncordon", "re-include a DPU in placement (Phase 2)")
}

func (a *Application) newDpuDrainCmd() *cobra.Command {
	return phase2Stub("drain", "migrate every ENI off a DPU then cordon it (Phase 2)")
}

func formatLabelsMap(m map[string]string) string {
	if len(m) == 0 {
		return "<none>"
	}
	out := ""
	for k, v := range m {
		if out != "" {
			out += ","
		}
		out += k + "=" + v
	}
	return out
}
