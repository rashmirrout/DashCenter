package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *Application) newReconcileCmd() *cobra.Command {
	var dpus []string
	c := &cobra.Command{
		Use:   "reconcile",
		Short: "Force dashd to re-run reconciliation",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			if err := cl.Reconcile(ctx, dpus); err != nil {
				return err
			}
			if len(dpus) == 0 {
				fmt.Fprintln(a.Out, "Triggered reconcile on all DPUs.")
			} else {
				fmt.Fprintf(a.Out, "Triggered reconcile on %d DPU(s): %v\n", len(dpus), dpus)
			}
			return nil
		},
	}
	c.Flags().StringArrayVar(&dpus, "dpu", nil, "limit reconcile to one or more DPU IDs (repeatable)")
	return c
}
