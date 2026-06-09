package cmd

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/cli"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

func (a *Application) newDiffCmd() *cobra.Command {
	var files []string
	c := &cobra.Command{
		Use:   "diff",
		Short: "Preview what apply would change",
		Long: `Diff compares each manifest against the current spec in dashd and
prints the fields that would change. It does not mutate state.

Phase 1 performs the comparison client-side (Get → field diff). Phase 2
will use dashd's SimulateApply for an authoritative preview.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(files) == 0 {
				return errors.New(errors.CodeInvalidArgument, "diff: --file/-f is required")
			}
			envs, err := cli.LoadFiles(files, cli.LoadOpts{Stdin: a.In})
			if err != nil {
				return errors.Wrap(errors.CodeInvalidArgument, "diff", err)
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()

			changed := 0
			for _, e := range envs {
				ki, ok := manifest.LookupKind(e.Kind)
				if !ok {
					return errors.Newf(errors.CodeInvalidArgument, "diff: unknown kind %q", e.Kind)
				}
				if ki.Kind == "Inventory" {
					// Inventory diff is treated as "always changes" — explicit.
					fmt.Fprintf(a.Out, "%s (full-replace)\n", ki.Kind)
					changed++
					continue
				}
				it, err := cl.Get(ctx, rc.Namespace, ki.StoreKind, e.Metadata.Name)
				if err != nil {
					var ce *errors.Error
					if asErr(err, &ce) && ce.Code == errors.CodeNotFound {
						fmt.Fprintf(a.Out, "%s/%s would CREATE\n", ki.StoreKind, e.Metadata.Name)
						changed++
						continue
					}
					return err
				}
				diffs := compareSpecs(it.Spec, e.Spec)
				if len(diffs) == 0 {
					continue
				}
				for _, d := range diffs {
					fmt.Fprintf(a.Out, "%s/%s  %s: %v → %v\n", ki.StoreKind, e.Metadata.Name, d.Field, d.Old, d.New)
				}
				changed++
			}
			if changed == 0 {
				fmt.Fprintln(a.Out, "no changes")
			} else {
				fmt.Fprintf(a.Out, "\n%d spec(s) would change.\n", changed)
			}
			return nil
		},
	}
	c.Flags().StringArrayVarP(&files, "filename", "f", nil, "manifest file or '-' for stdin")
	return c
}

type fieldDiff struct {
	Field string
	Old   any
	New   any
}

// compareSpecs returns a stable list of key-level differences between the
// current (server-side) spec JSON and the proposed (client-side) spec map.
// Phase 1 keeps it simple: top-level field comparison, JSON-equality on
// values, deletions detected by missing keys on the proposed side.
func compareSpecs(currentJSON []byte, proposed map[string]any) []fieldDiff {
	var current map[string]any
	if len(currentJSON) > 0 {
		_ = json.Unmarshal(currentJSON, &current)
	}
	if current == nil {
		current = map[string]any{}
	}
	// Strip projected fields that the SpecJSON encoder adds (so they don't
	// show as bogus diffs).
	prop := make(map[string]any, len(proposed))
	for k, v := range proposed {
		switch k {
		case "name", "namespace", "expected_generation":
			continue
		}
		prop[k] = v
	}
	keys := uniqueKeys(current, prop)
	out := make([]fieldDiff, 0, len(keys))
	for _, k := range keys {
		ov, oPresent := current[k]
		nv, nPresent := prop[k]
		if !oPresent {
			out = append(out, fieldDiff{Field: k, Old: nil, New: nv})
			continue
		}
		if !nPresent {
			out = append(out, fieldDiff{Field: k, Old: ov, New: nil})
			continue
		}
		if !jsonEqual(ov, nv) {
			out = append(out, fieldDiff{Field: k, Old: ov, New: nv})
		}
	}
	return out
}

func uniqueKeys(a, b map[string]any) []string {
	set := map[string]struct{}{}
	for k := range a {
		set[k] = struct{}{}
	}
	for k := range b {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func jsonEqual(a, b any) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

// asErr is the minimal errors.As shim used here and elsewhere in the cmd
// package (avoids re-importing the stdlib errors package).
func asErr(err error, target **errors.Error) bool {
	for cur := err; cur != nil; {
		if e, ok := cur.(*errors.Error); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	return false
}
