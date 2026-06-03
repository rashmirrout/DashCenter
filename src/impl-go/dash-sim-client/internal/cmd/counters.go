package cmd

import (
	"os"

	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newCountersCmd() *cobra.Command {
	root := &cobra.Command{Use: "counters", Short: "Counter inspection"}
	root.AddCommand(&cobra.Command{
		Use:   "get <object-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Fetch counters for the given object id",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			m, err := cl.GetCounters(ctx, args[0])
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.Map(os.Stdout, f, m)
		},
	})
	return root
}

func newHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Connectivity check: dial + list VNETs",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			vs, err := cl.ListVnets(ctx)
			if err != nil {
				return err
			}
			c.Printf("ok: target=%s vnets=%d\n", flagTarget, len(vs))
			return nil
		},
	}
}
