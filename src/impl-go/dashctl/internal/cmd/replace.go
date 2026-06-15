package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cli"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
)

func (a *Application) newReplaceCmd() *cobra.Command {
	var files []string
	c := &cobra.Command{
		Use:   "replace",
		Short: "Strict-CAS apply (manifest must carry metadata.generation)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, "replace: --file/-f is required")
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "replace", err)
			}
			for _, e := range envs {
				if e.Metadata.Generation == 0 && e.Kind != "Inventory" {
					return errors.Newf(errors.CodeInvalidArgument, "replace: %s/%s missing metadata.generation (use `dashctl apply` for create)", e.Kind, e.Metadata.Name)
				}
			}
			return a.runApply(cmd.Context(), envs, "none", true) // replace always forces
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file or '-' for stdin")
	return c
}
