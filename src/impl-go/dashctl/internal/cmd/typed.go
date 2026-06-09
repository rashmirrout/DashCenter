package cmd

import (
	"context"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cli"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

// newTypedKindGroups produces "vnet put / get / list / delete" style
// subcommand groups. They are thin wrappers around the generic verbs and
// share their full flag surface where it makes sense.
func (a *Application) newTypedKindGroups() []*cobra.Command {
	specs := []struct {
		group   string
		kindArg string
	}{
		{"vnet", "vnet"},
		{"eni", "eni"},
		{"vnet-mapping", "vnet_mapping"},
		{"acl-policy", "acl_policy"},
		{"route-policy", "route_policy"},
		{"ha-set", "ha_set"},
		{"service-tunnel", "service_tunnel"},
	}
	out := make([]*cobra.Command, 0, len(specs))
	for _, s := range specs {
		out = append(out, a.newTypedGroupFor(s.group, s.kindArg))
	}
	return out
}

func (a *Application) newTypedGroupFor(group, kindArg string) *cobra.Command {
	grp := &cobra.Command{
		Use:   group,
		Short: "Manage " + group + " specs",
	}
	grp.AddCommand(
		a.newTypedPutCmd(kindArg),
		a.newTypedGetCmd(kindArg),
		a.newTypedListCmd(kindArg),
		a.newTypedDeleteCmd(kindArg),
		a.newTypedDescribeCmd(kindArg),
	)
	return grp
}

func (a *Application) newTypedPutCmd(kindArg string) *cobra.Command {
	var files []string
	c := &cobra.Command{
		Use:   "put",
		Short: "Apply " + kindArg + " from -f manifest",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, kindArg+" put: --file/-f is required")
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, kindArg+" put", err)
			}
			ki, _ := manifest.LookupKind(kindArg)
			for _, e := range envs {
				if !strings.EqualFold(e.Kind, ki.Kind) {
					return errors.Newf(errors.CodeInvalidArgument, "%s put: manifest kind %q does not match command", kindArg, e.Kind)
				}
			}
			return a.runApply(cmd.Context(), envs, "none")
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file or '-' for stdin")
	return c
}

func (a *Application) newTypedGetCmd(kindArg string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name>",
		Short: "Get one " + kindArg,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTypedReadOne(cmd.Context(), kindArg, args[0])
		},
	}
}

func (a *Application) newTypedListCmd(kindArg string) *cobra.Command {
	var selector string
	c := &cobra.Command{
		Use:   "list",
		Short: "List " + kindArg + "s",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runTypedList(cmd.Context(), kindArg, client.ListOptions{Selector: selector})
		},
	}
	c.Flags().StringVarP(&selector, "selector", "l", "", "label selector")
	return c
}

func (a *Application) newTypedDeleteCmd(kindArg string) *cobra.Command {
	var ignore bool
	c := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete one " + kindArg,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ki, _ := manifest.LookupKind(kindArg)
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			return cl.Delete(ctx, rc.Namespace, ki.StoreKind, args[0], client.DeleteOptions{IgnoreNotFound: ignore})
		},
	}
	c.Flags().BoolVar(&ignore, "ignore-not-found", false, "exit 0 if absent")
	return c
}

func (a *Application) newTypedDescribeCmd(kindArg string) *cobra.Command {
	return &cobra.Command{
		Use:   "describe <name>",
		Short: "Human-readable detail for one " + kindArg,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ki, _ := manifest.LookupKind(kindArg)
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()
			it, err := cl.Get(ctx, rc.Namespace, ki.StoreKind, args[0])
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
}

func (a *Application) runTypedReadOne(parent context.Context, kindArg, name string) error {
	ki, _ := manifest.LookupKind(kindArg)
	cl, rc, err := a.dial(parent)
	if err != nil {
		return err
	}
	defer cl.Close()
	ctx, cancel := withTimeout(parent, rc)
	defer cancel()
	return a.getOne(ctx, cl, rc.Namespace, ki, name)
}

func (a *Application) runTypedList(parent context.Context, kindArg string, opts client.ListOptions) error {
	ki, _ := manifest.LookupKind(kindArg)
	cl, rc, err := a.dial(parent)
	if err != nil {
		return err
	}
	defer cl.Close()
	ctx, cancel := withTimeout(parent, rc)
	defer cancel()
	return a.getMany(ctx, cl, rc.Namespace, ki, opts)
}
