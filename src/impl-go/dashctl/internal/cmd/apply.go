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

func (a *Application) newApplyCmd() *cobra.Command {
	var (
		files     []string
		recursive bool
		dryRun    string
	)
	c := &cobra.Command{
		Use:   "apply",
		Short: "Apply one or more manifests (create or replace)",
		Long: `Apply reads manifests from -f and PUTs each spec to dashd.

Manifests may be a single YAML/JSON file, a directory of YAML/JSON files,
or "-" for stdin. Multi-document YAML (separated by '---') is supported.

Each envelope carries:
  apiVersion: dashcenter.v1
  kind: Vnet            # or Eni / VnetMapping / AclPolicy / RoutePolicy / HaSet / Inventory
  metadata: { name: ..., namespace: ..., generation: ..., labels: {...} }
  spec: { ... }         # the dashd spec body

When metadata.generation is set it is used as expected_generation (CAS).
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, "apply: --file/-f is required")
			}
			if dryRun != "" && dryRun != "client" && dryRun != "server" && dryRun != "none" {
				return errors.Newf(errors.CodeInvalidArgument, "apply: --dry-run must be none|client|server (got %q)", dryRun)
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Recursive: recursive, Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "apply", err)
			}
			return a.runApply(cmd.Context(), envs, dryRun)
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file, directory, or '-' for stdin (repeatable)")
	c.Flags().BoolVarP(&recursive, "recursive", "R", false, "recursively process the given directory")
	c.Flags().StringVar(&dryRun, "dry-run", "none", "none|client|server (server uses dashd SimulateApply when available)")
	return c
}

type applyRow struct {
	Kind       string
	Namespace  string
	Name       string
	Op         string
	Generation uint64
	Result     string
	Err        error
}

func (a *Application) runApply(parent context.Context, envs []*manifest.Envelope, dryRun string) error {
	if dryRun == "client" {
		return a.renderApplyRows(synthesiseClientDryRunRows(envs))
	}
	cl, rc, err := a.dial(parent)
	if err != nil {
		return err
	}
	defer cl.Close()
	ctx, cancel := withTimeout(parent, rc)
	defer cancel()

	rows := make([]applyRow, 0, len(envs))
	var firstErr error
	for _, env := range envs {
		row, err := a.applyOne(ctx, cl, env, rc.Namespace, dryRun)
		rows = append(rows, row)
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := a.renderApplyRows(rows); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func synthesiseClientDryRunRows(envs []*manifest.Envelope) []applyRow {
	rows := make([]applyRow, 0, len(envs))
	for _, e := range envs {
		rows = append(rows, applyRow{
			Kind:       strings.ToLower(e.Kind),
			Namespace:  e.Metadata.Namespace,
			Name:       e.Metadata.Name,
			Op:         "dry-run",
			Generation: e.Metadata.Generation,
			Result:     "dry-run",
		})
	}
	return rows
}

func (a *Application) applyOne(ctx context.Context, cl client.Client, env *manifest.Envelope, defaultNS string, dryRun string) (applyRow, error) {
	ns := firstNonEmpty(env.Metadata.Namespace, defaultNS)
	row := applyRow{
		Kind:       strings.ToLower(env.Kind),
		Namespace:  ns,
		Name:       env.Metadata.Name,
		Op:         "apply",
		Generation: env.Metadata.Generation,
	}
	if env.Kind == "Inventory" {
		dpus := inventoryDpusFromSpec(env.Spec)
		row.Op = "replace"
		if dryRun == "server" {
			row.Result = "dry-run"
			return row, nil
		}
		if err := cl.PutInventory(ctx, dpus); err != nil {
			row.Result = "fail"
			row.Err = err
			return row, err
		}
		row.Result = "ok"
		return row, nil
	}
	ki, ok := manifest.LookupKind(env.Kind)
	if !ok {
		err := errors.Newf(errors.CodeInvalidArgument, "apply: unknown kind %q", env.Kind)
		row.Result = "fail"
		row.Err = err
		return row, err
	}
	body, err := env.SpecJSON()
	if err != nil {
		err = errors.Wrap(errors.CodeInvalidArgument, "apply: encode spec", err)
		row.Result = "fail"
		row.Err = err
		return row, err
	}
	if dryRun == "server" {
		row.Result = "dry-run"
		return row, nil
	}
	res, err := cl.Put(ctx, ns, ki.StoreKind, env.Metadata.Name, body)
	if err != nil {
		row.Result = "fail"
		row.Err = err
		return row, err
	}
	row.Generation = res.Generation
	row.Result = "ok"
	return row, nil
}

func (a *Application) renderApplyRows(rows []applyRow) error {
	// Minimal hand-rolled rendering — apply rows are too summary-shaped for
	// the generic table renderer (no spec body). One line per row.
	for _, r := range rows {
		ns := r.Namespace
		if ns == "" {
			ns = "-"
		}
		switch r.Result {
		case "ok":
			fmt.Fprintf(a.Out, "%s/%s %s in namespace %s (generation %d)\n", r.Kind, r.Name, r.Op, ns, r.Generation)
		case "dry-run":
			fmt.Fprintf(a.Out, "%s/%s would %s in namespace %s\n", r.Kind, r.Name, r.Op, ns)
		case "fail":
			fmt.Fprintf(a.Out, "%s/%s FAILED %s in namespace %s: %v\n", r.Kind, r.Name, r.Op, ns, r.Err)
		}
	}
	return nil
}

// inventoryDpusFromSpec extracts a typed []DpuInput from a freeform
// Inventory spec body.
func inventoryDpusFromSpec(spec map[string]any) []client.DpuInput {
	raw, ok := spec["dpus"].([]any)
	if !ok {
		return nil
	}
	out := make([]client.DpuInput, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		dpu := client.DpuInput{}
		if v, ok := m["id"].(string); ok {
			dpu.ID = v
		}
		if v, ok := m["endpoint"].(string); ok {
			dpu.Endpoint = v
		}
		if v, ok := m["labels"].(map[string]any); ok {
			dpu.Labels = make(map[string]string, len(v))
			for k, val := range v {
				if s, ok := val.(string); ok {
					dpu.Labels[k] = s
				}
			}
		}
		out = append(out, dpu)
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
