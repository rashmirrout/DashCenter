package cmd

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

func (a *Application) newDescribeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "describe <kind> <name>",
		Short: "Human-readable detail for one spec",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ki, ok := manifest.LookupKind(args[0])
			if !ok {
				return errors.Newf(errors.CodeInvalidArgument, "describe: unknown kind %q", args[0])
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			it, err := cl.Get(ctx, rc.Namespace, ki.StoreKind, args[1])
			if err != nil {
				return err
			}
			env, err := manifest.EnvelopeFromStoredItem(ki.StoreKind, it.Namespace, it.Name, it.Generation, it.Spec, nil)
			if err != nil {
				return errors.Wrap(errors.CodeInternal, "describe", err)
			}
			writeDescribe(a.Out, env)
			return nil
		},
	}
	return c
}

// writeDescribe renders a kubectl-describe style block for one envelope.
func writeDescribe(w io.Writer, env *manifest.Envelope) {
	fmt.Fprintf(w, "Name:        %s\n", env.Metadata.Name)
	fmt.Fprintf(w, "Namespace:   %s\n", env.Metadata.Namespace)
	fmt.Fprintf(w, "Kind:        %s\n", env.Kind)
	fmt.Fprintf(w, "Generation:  %d\n", env.Metadata.Generation)
	if len(env.Metadata.Labels) > 0 {
		keys := make([]string, 0, len(env.Metadata.Labels))
		for k := range env.Metadata.Labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(w, "Labels:      ")
		for i, k := range keys {
			if i > 0 {
				fmt.Fprintf(w, ",")
			}
			fmt.Fprintf(w, "%s=%s", k, env.Metadata.Labels[k])
		}
		fmt.Fprintln(w)
	}
	if len(env.Spec) > 0 {
		fmt.Fprintln(w, "Spec:")
		keys := make([]string, 0, len(env.Spec))
		for k := range env.Spec {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "  %s: %v\n", k, env.Spec[k])
		}
	}
}
