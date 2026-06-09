package cmd

import (
	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
)

// phase2Stub returns a Cobra command that prints a clear Unimplemented
// notice. Used for HA / Migration / Diagnostics / Operations verbs that
// depend on dashd Phase 2 milestones.
func phase2Stub(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.Newf(errors.CodeUnimplemented, "%s: requires dashd Phase 2", name).
				WithHint("Track progress in specs/Impl-Plan/impl-phases.md")
		},
	}
}

// newPhase2Stubs registers the top-level Phase 2 verb groups so users see
// them in --help. Each group has a flat list of subcommands that return
// Unimplemented for now; signatures are stabilised in the LLD so we add
// the real implementations in Phase 2 without breaking flag names.
func (a *Application) newPhase2Stubs() []*cobra.Command {
	ha := &cobra.Command{Use: "ha", Short: "DPU HA orchestration (Phase 2)"}
	ha.AddCommand(
		phase2Stub("switchover", "planned HA switchover"),
		phase2Stub("failover", "immediate HA failover"),
		phase2Stub("events", "stream HA events"),
		phase2Stub("set", "inspect ha-set state"),
		phase2Stub("scope", "inspect ha-scope state"),
		phase2Stub("flow-sync-stats", "show flow-sync statistics"),
	)
	mig := &cobra.Command{Use: "migration", Short: "ENI live migration (Phase 2)"}
	mig.AddCommand(
		phase2Stub("plan", "create / validate a migration plan"),
		phase2Stub("start", "start a migration session"),
		phase2Stub("advance", "advance migration phase"),
		phase2Stub("stream", "stream migration state changes"),
		phase2Stub("rollback", "rollback an active migration"),
		phase2Stub("abort", "abort a migration"),
		phase2Stub("commit", "commit a migration"),
		phase2Stub("bundle", "export/import a migration bundle"),
	)
	trace := &cobra.Command{Use: "trace", Short: "Diagnostics (Phase 2)"}
	trace.AddCommand(
		phase2Stub("flow", "TraceFlow for a synthetic packet"),
		phase2Stub("explain", "explain ACL/route match"),
		phase2Stub("acl-stats", "dead-rule detection"),
		phase2Stub("drift-explain", "narrative drift explanation"),
		phase2Stub("resimulate", "trigger DPU resimulation"),
	)
	return []*cobra.Command{ha, mig, trace}
}
