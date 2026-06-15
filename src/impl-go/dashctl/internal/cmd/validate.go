package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cli"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

func (a *Application) newValidateCmd() *cobra.Command {
	var (
		files     []string
		recursive bool
	)
	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate manifests against the live dashd store",
		Long: `Validate loads manifests and PUTs each spec to dashd, reporting
which objects pass referential integrity checks and which are rejected.

Unlike 'apply', validate continues past failures and prints a summary
of all results. Exit code = number of rejected objects (0 = all valid).

Objects are validated against dashd's FK rules:
  - ENI → vnet must exist in the same namespace
  - VnetMapping → vnet must exist in the same namespace
  - AclPolicy → every eni_names[i] must exist
  - RoutePolicy → every eni_names[i] + vnet/service_tunnel targets must exist
  - HaSet → every member_dpu_ids[i] must exist in inventory

Delete-side: dashd rejects deleting vnet/eni/service_tunnel when
dependents still reference them.

Examples:
  dashctl validate -f manifest/
  dashctl validate -f scenario.yaml -R
  dashctl validate -f - < manifest.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, "validate: --file/-f is required")
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Recursive: recursive, Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "validate", err)
			}
			return a.runValidate(cmd.Context(), envs)
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file, directory, or '-' for stdin (repeatable)")
	c.Flags().BoolVarP(&recursive, "recursive", "R", false, "recursively process the given directory")
	return c
}

type validateRow struct {
	Kind      string
	Namespace string
	Name      string
	Status    string
	Err       error
}

func (a *Application) runValidate(parent context.Context, envs []*manifest.Envelope) error {
	cl, rc, err := a.dial(parent)
	if err != nil {
		return err
	}
	defer cl.Close()
	ctx, cancel := withTimeout(parent, rc)
	defer cancel()

	rows := make([]validateRow, 0, len(envs))
	failures := 0
	for _, env := range envs {
		row := a.validateOne(ctx, cl, env, rc.Namespace)
		rows = append(rows, row)
		if row.Err != nil {
			failures++
		}
	}

	// Render table
	fmt.Fprintf(a.Out, "%-6s  %-15s  %-12s  %-30s  %s\n",
		"STATUS", "KIND", "NAMESPACE", "NAME", "ERROR")
	fmt.Fprintf(a.Out, "%-6s  %-15s  %-12s  %-30s  %s\n",
		"------", "---------------", "------------",
		"------------------------------", "-----")
	for _, r := range rows {
		status := "✅ OK"
		errMsg := ""
		if r.Err != nil {
			status = "❌ FAIL"
			errMsg = r.Err.Error()
		}
		fmt.Fprintf(a.Out, "%-6s  %-15s  %-12s  %-30s  %s\n",
			status, r.Kind, r.Namespace, r.Name, errMsg)
	}
	fmt.Fprintf(a.Out, "\nTotal: %d  Accepted: %d  Rejected: %d\n",
		len(rows), len(rows)-failures, failures)

	if failures > 0 {
		return errors.Newf(errors.CodeInvalidArgument, "validate: %d of %d objects rejected", failures, len(rows))
	}
	return nil
}

func (a *Application) validateOne(ctx context.Context, cl client.Client, env *manifest.Envelope, defaultNS string) validateRow {
	ns := firstNonEmpty(env.Metadata.Namespace, defaultNS)
	row := validateRow{
		Kind:      strings.ToLower(env.Kind),
		Namespace: ns,
		Name:      env.Metadata.Name,
	}

	if env.Kind == "Inventory" {
		// Inventory is always valid at the envelope level
		row.Status = "ok"
		return row
	}

	ki, ok := manifest.LookupKind(env.Kind)
	if !ok {
		row.Status = "fail"
		row.Err = errors.Newf(errors.CodeInvalidArgument, "unknown kind %q", env.Kind)
		return row
	}

	body, err := env.SpecJSON()
	if err != nil {
		row.Status = "fail"
		row.Err = errors.Wrap(errors.CodeInvalidArgument, "encode spec", err)
		return row
	}

	// Actually PUT to dashd — dashd validates FK refs and either accepts
	// or rejects. The object IS persisted if validation passes.
	_, err = cl.Put(ctx, ns, ki.StoreKind, env.Metadata.Name, body)
	if err != nil {
		row.Status = "fail"
		row.Err = err
		return row
	}

	row.Status = "ok"
	return row
}
