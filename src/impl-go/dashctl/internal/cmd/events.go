package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
)

// Events streaming is a Phase 2 feature in the dashd roadmap (gRPC
// server-stream `WatchEvents`). dashctl Phase 1 ships the command so
// users see it in `--help`; it returns a clear, typed Unimplemented.
func (a *Application) newEventsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "events",
		Short: "Stream PolicyEvent feed (Phase 2 — currently unimplemented)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(a.Err, "dashctl events: streaming is provided by dashd Phase 2 (WatchEvents).")
			return errors.New(errors.CodeUnimplemented, "events: not yet supported by dashd Phase 1B")
		},
	}
	c.Flags().Bool("watch", true, "follow live events (always true for this command)")
	c.Flags().String("since", "", "tail events emitted in the last duration (Phase 2)")
	c.Flags().StringP("selector", "l", "", "label selector (Phase 2)")
	return c
}
