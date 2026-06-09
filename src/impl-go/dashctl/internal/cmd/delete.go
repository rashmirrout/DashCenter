package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

func (a *Application) newDeleteCmd() *cobra.Command {
	var (
		ignoreNotFound bool
		expectedGen    uint64
	)
	c := &cobra.Command{
		Use:   "delete <kind> <name>",
		Short: "Delete a spec",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ki, ok := manifest.LookupKind(args[0])
			if !ok {
				return errors.Newf(errors.CodeInvalidArgument, "delete: unknown kind %q", args[0])
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			err = cl.Delete(ctx, rc.Namespace, ki.StoreKind, args[1], client.DeleteOptions{IgnoreNotFound: ignoreNotFound, ExpectedGeneration: expectedGen})
			if err != nil {
				return err
			}
			fmt.Fprintf(a.Out, "%s/%s deleted\n", ki.StoreKind, args[1])
			return nil
		},
	}
	c.Flags().BoolVar(&ignoreNotFound, "ignore-not-found", false, "exit 0 if the spec does not exist")
	c.Flags().Uint64Var(&expectedGen, "expected-generation", 0, "CAS: only delete if generation equals this value (0 = no CAS)")
	return c
}
