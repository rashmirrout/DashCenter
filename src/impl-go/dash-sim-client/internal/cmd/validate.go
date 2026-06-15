// validate.go ships the `dash-sim-client validate` subcommand — a
// pre-flight FK validation tool that loads a scenario file and applies
// each object to the target sim, reporting which objects pass
// referential integrity checks and which are rejected.
//
// Unlike `apply`, validate reports ALL errors (does not stop at the
// first failure) and prints a summary table. Exit code = number of
// failed objects.
//
// Usage:
//
//	dash-sim-client validate -f scenario.yaml          # check a file
//	dash-sim-client validate -f scenario.yaml -o json  # machine output
//
// The sim must be running with --strict-refs (the default).
// Objects that pass FK validation ARE applied to the store — this is
// an apply-and-report tool, not a dry-run (the sim has no dry-run
// RPC). Use a test sim instance for non-destructive validation.

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type validateResult struct {
	Index    int    `json:"index" yaml:"index"`
	Kind     string `json:"kind" yaml:"kind"`
	Key      string `json:"key" yaml:"key"`
	Accepted bool   `json:"accepted" yaml:"accepted"`
	Error    string `json:"error,omitempty" yaml:"error,omitempty"`
}

func newValidateCmd() *cobra.Command {
	var file string

	c := &cobra.Command{
		Use:   "validate",
		Short: "Validate a scenario file against the sim's FK rules",
		Long: `Load a JSON/YAML scenario file and apply each object to the target
sim, reporting which objects pass referential integrity checks and
which are rejected.

Unlike 'apply', validate continues past failures and prints a summary
of all results. Exit code equals the number of rejected objects (0 =
all valid).

The sim must be running with --strict-refs (the default). Objects that
pass FK validation ARE applied to the store. Use a test sim for
non-destructive validation.

Objects should be ordered Tier 0 → Tier 1 → Tier 2:
  Tier 0: vnet, qos, acl_group, route_group, tunnel, ...
  Tier 1: eni, acl_rule, route, vnet_mapping, ...
  Tier 2: eni_route, acl_in/out, route_rule, meter, ...

Examples:
  dash-sim-client validate -f scenario.yaml
  dash-sim-client validate -f scenario.yaml -o json
  dash-sim-client validate -f scenario.yaml -o table`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("validate: -f <file> is required")
			}
			docs, err := readDocuments(file)
			if err != nil {
				return fmt.Errorf("validate: %w", err)
			}
			if len(docs) == 0 {
				fmt.Fprintln(os.Stderr, "validate: no documents found in file")
				return nil
			}

			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()

			ctx, cancel := rpcContext()
			defer cancel()

			var results []validateResult
			failures := 0
			for i, d := range docs {
				obj, err := docToObject(d)
				if err != nil {
					r := validateResult{
						Index: i, Kind: d.Kind,
						Key: strings.Join(d.Key, ":"),
						Error: err.Error(),
					}
					results = append(results, r)
					failures++
					continue
				}
				ack, err := cl.Apply(ctx, obj)
				if err != nil {
					r := validateResult{
						Index: i, Kind: d.Kind,
						Key:   strings.Join(d.Key, ":"),
						Error: fmt.Sprintf("gRPC error: %v", err),
					}
					results = append(results, r)
					failures++
					continue
				}
				r := validateResult{
					Index:    i,
					Kind:     d.Kind,
					Key:      strings.Join(d.Key, ":"),
					Accepted: ack.GetAccepted(),
				}
				if !ack.GetAccepted() {
					r.Error = ack.GetError()
					failures++
				}
				results = append(results, r)
			}

			fmtOut, fmtErr := resolveFormat()
			if fmtErr != nil {
				return fmtErr
			}

			switch fmtOut {
			case "json":
				out := map[string]any{
					"total":    len(results),
					"accepted": len(results) - failures,
					"rejected": failures,
					"results":  results,
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				fmt.Fprintln(os.Stdout, string(b))
			case "yaml":
				out := map[string]any{
					"total":    len(results),
					"accepted": len(results) - failures,
					"rejected": failures,
					"results":  results,
				}
				b, _ := yaml.Marshal(out)
				fmt.Fprint(os.Stdout, string(b))
			default: // table
				fmt.Fprintf(os.Stdout, "%-5s  %-6s  %-25s  %-30s  %s\n",
					"INDEX", "STATUS", "KIND", "KEY", "ERROR")
				fmt.Fprintf(os.Stdout, "%-5s  %-6s  %-25s  %-30s  %s\n",
					"-----", "------", "-------------------------",
					"------------------------------", "-----")
				for _, r := range results {
					status := "✅ OK"
					errMsg := ""
					if !r.Accepted {
						status = "❌ FAIL"
						errMsg = r.Error
					}
					fmt.Fprintf(os.Stdout, "%-5d  %-6s  %-25s  %-30s  %s\n",
						r.Index, status, r.Kind, r.Key, errMsg)
				}
				fmt.Fprintf(os.Stdout, "\nTotal: %d  Accepted: %d  Rejected: %d\n",
					len(results), len(results)-failures, failures)
			}

			if failures > 0 {
				return fmt.Errorf("%d of %d objects rejected", failures, len(results))
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "path to JSON/YAML scenario file (required)")
	_ = c.MarkFlagRequired("file")
	return c
}
