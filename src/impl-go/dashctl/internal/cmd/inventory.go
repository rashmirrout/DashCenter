package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cli"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
)

func (a *Application) newInventoryCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "inventory",
		Short: "Manage the dashd DPU inventory",
	}
	c.AddCommand(a.newInventoryPutCmd(), a.newInventoryGetCmd())
	return c
}

func (a *Application) newInventoryPutCmd() *cobra.Command {
	var files []string
	c := &cobra.Command{
		Use:   "put",
		Short: "Replace the inventory from a manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, "inventory put: --file/-f is required")
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "inventory put", err)
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			for _, e := range envs {
				if e.Kind != "Inventory" {
					return errors.Newf(errors.CodeInvalidArgument, "inventory put: expected kind Inventory, got %s", e.Kind)
				}
				if err := cl.PutInventory(ctx, inventoryDpusFromSpec(e.Spec)); err != nil {
					return err
				}
			}
			fmt.Fprintln(a.Out, "inventory updated")
			return nil
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file or '-' for stdin")
	return c
}

func (a *Application) newInventoryGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the current inventory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			dpus, err := cl.GetInventory(ctx)
			if err != nil {
				return err
			}
			return a.renderInventory(dpus)
		},
	}
}
