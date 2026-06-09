package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (a *Application) newVersionCmd() *cobra.Command {
	var clientOnly bool
	c := &cobra.Command{
		Use:   "version",
		Short: "Print dashctl + dashd versions",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(a.Out, "Client: dashctl %s (commit %s, built %s)\n", a.Build.Version, a.Build.Commit, a.Build.Date)
			if clientOnly {
				return nil
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				fmt.Fprintf(a.Out, "Server: unavailable (config: %v)\n", err)
				return nil
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			si, err := cl.ServerInfo(ctx)
			if err != nil {
				fmt.Fprintf(a.Out, "Server: unavailable (%v)\n", err)
				return nil
			}
			fmt.Fprintf(a.Out, "Server: dashd  %s (transport=%s endpoint=%s) leader=%t\n", si.Version, rc.Transport, rc.Endpoint, si.Leader)
			return nil
		},
	}
	c.Flags().BoolVar(&clientOnly, "client", false, "only print client version (do not dial server)")
	return c
}
