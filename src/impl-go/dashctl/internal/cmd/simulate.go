package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cli"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/render"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

// newSimulateCmd builds the `dashctl simulate` subcommand (PB-2).
//
// Reads the same manifest envelopes that `apply` accepts and translates
// each one into a service.SimulateOp record posted to dashd's
// POST /v1/simulate endpoint. Renders per-DPU deltas and validation
// errors. Always exits 0 unless the round-trip itself fails — a "would
// not succeed" verdict is data, not an error (operator gets a non-zero
// exit only when they pass --error-on-violation).
func (a *Application) newSimulateCmd() *cobra.Command {
	var (
		files            []string
		recursive        bool
		action           string
		errorOnViolation bool
	)
	c := &cobra.Command{
		Use:   "simulate",
		Short: "Dry-run admission check (PB-2): per-DPU capacity deltas without writing",
		Long: `Simulate computes what would happen if the supplied manifests were applied,
without persisting anything. It returns a per-DPU delta table plus any
admission errors (capacity exhaustion, unknown DPU references, etc.).

Manifest selection mirrors 'apply': -f accepts files, directories, or '-'.
PB-2 supports kinds: Eni, VnetMapping, AclPolicy.

Use --action to override what each manifest is treated as (default: put).
Combine with 'dashctl simulate -f delete.yaml --action delete' to dry-run
deletions.

Exit status is 0 on a successful round-trip regardless of admission
verdict; pass --error-on-violation to exit non-zero when would_succeed=false.
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, "simulate: --file/-f is required")
			}
			if action != "put" && action != "delete" {
				return errors.Newf(errors.CodeInvalidArgument, "simulate: --action must be put|delete (got %q)", action)
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Recursive: recursive, Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "simulate", err)
			}
			return a.runSimulate(cmd.Context(), envs, action, errorOnViolation)
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file, directory, or '-' for stdin (repeatable)")
	c.Flags().BoolVarP(&recursive, "recursive", "R", false, "recursively process the given directory")
	c.Flags().StringVar(&action, "action", "put", "put|delete (how to interpret each manifest)")
	c.Flags().BoolVar(&errorOnViolation, "error-on-violation", false, "exit non-zero if would_succeed is false")
	return c
}

func (a *Application) runSimulate(parent context.Context, envs []*manifest.Envelope, action string, errorOnViolation bool) error {
	cl, rc, err := a.dial(parent)
	if err != nil {
		return err
	}
	defer cl.Close()
	ctx, cancel := withTimeout(parent, rc)
	defer cancel()

	ops, err := envelopesToSimulateOps(envs, action, rc.Namespace)
	if err != nil {
		return err
	}
	opsJSON, err := json.Marshal(ops)
	if err != nil {
		return errors.Wrap(errors.CodeInternal, "simulate marshal", err)
	}
	res, err := cl.Simulate(ctx, opsJSON)
	if err != nil {
		return err
	}
	if err := a.renderSimulateResult(res); err != nil {
		return err
	}
	if errorOnViolation && !res.WouldSucceed {
		return errors.New(errors.CodeConflict, "simulate: would_succeed=false")
	}
	return nil
}

// simulateOpJSON is the JSON shape dashd's POST /v1/simulate expects per
// op (mirrors service.SimulateOp).
type simulateOpJSON struct {
	Action          string         `json:"action"`
	Namespace       string         `json:"namespace,omitempty"`
	Kind            string         `json:"kind"`
	Name            string         `json:"name,omitempty"`
	EniSpec         map[string]any `json:"eni,omitempty"`
	VnetMappingSpec map[string]any `json:"vnet_mapping,omitempty"`
	AclPolicySpec   map[string]any `json:"acl_policy,omitempty"`
}

func envelopesToSimulateOps(envs []*manifest.Envelope, action, defaultNS string) ([]simulateOpJSON, error) {
	out := make([]simulateOpJSON, 0, len(envs))
	for i, e := range envs {
		ki, ok := manifest.LookupKind(e.Kind)
		if !ok {
			return nil, errors.Newf(errors.CodeInvalidArgument, "simulate: envelope[%d]: unknown kind %q", i, e.Kind)
		}
		ns := firstNonEmpty(e.Metadata.Namespace, defaultNS)
		op := simulateOpJSON{Action: action, Namespace: ns, Kind: ki.StoreKind, Name: e.Metadata.Name}
		switch ki.StoreKind {
		case "eni":
			op.EniSpec = e.Spec
			if op.EniSpec == nil {
				op.EniSpec = map[string]any{}
			}
			if _, ok := op.EniSpec["name"]; !ok && e.Metadata.Name != "" {
				op.EniSpec["name"] = e.Metadata.Name
			}
		case "vnet_mapping":
			op.VnetMappingSpec = e.Spec
		case "acl_policy":
			op.AclPolicySpec = e.Spec
			if op.AclPolicySpec == nil {
				op.AclPolicySpec = map[string]any{}
			}
			if _, ok := op.AclPolicySpec["name"]; !ok && e.Metadata.Name != "" {
				op.AclPolicySpec["name"] = e.Metadata.Name
			}
		default:
			return nil, errors.Newf(errors.CodeInvalidArgument,
				"simulate: PB-2 supports Eni|VnetMapping|AclPolicy (envelope[%d] kind=%s)", i, e.Kind)
		}
		out = append(out, op)
	}
	return out, nil
}

func (a *Application) renderSimulateResult(res *client.SimulateResult) error {
	format, expr, err := a.outFormat()
	if err != nil {
		return err
	}
	// Structured formats: delegate to the render package and we're done.
	switch format {
	case render.FormatJSON, render.FormatYAML, render.FormatJSONPath, render.FormatTemplate:
		return render.Render(a.Out, res, render.Options{Format: format, Expression: expr})
	}

	// Default (table/wide/name/empty) → operator-friendly summary.
	if res.WouldSucceed {
		fmt.Fprintln(a.Out, "would_succeed: true")
	} else {
		fmt.Fprintln(a.Out, "would_succeed: false")
	}
	if len(res.ValidationErrors) > 0 {
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "validation_errors:")
		for _, e := range res.ValidationErrors {
			fmt.Fprintf(a.Out, "  - %s\n", e)
		}
	}
	if len(res.PerDpuImpact) > 0 {
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "per_dpu_impact:")
		fmt.Fprintf(a.Out, "  %-20s %8s %8s %8s  %s\n", "DPU", "dENIs", "dMaps", "dAcl", "STATUS")
		for _, row := range res.PerDpuImpact {
			status := "OK"
			if row.ExceedsCapacity {
				status = "EXCEEDS: " + row.Reason
			}
			fmt.Fprintf(a.Out, "  %-20s %8d %8d %8d  %s\n",
				row.DpuID, row.DeltaEnis, row.DeltaVnetMappings, row.DeltaAclRules, status)
		}
	}
	return nil
}
