// Package cmd: `dashctl topology` — operator-facing CLI client for the
// PE-G6 / PE-G7 ClusterService surface.
//
// Two modes:
//
//	dashctl topology               One-shot snapshot. Pretty tree by
//	                               default; `-o json|yaml` for machine
//	                               consumption.
//
//	dashctl topology --follow      Open the SSE stream (same wire format
//	                               the browser's /topology-v2 page
//	                               consumes) and print each event as it
//	                               arrives. Honours --include-enis +
//	                               --since-id for resume.
//
// The command goes through dashd's REST surface, NOT through dashw. That
// matches dashctl's design: the BFF is for browsers; the operator CLI
// talks directly to the controller cluster. SSE parsing is done by the
// REST backend (pkg/client/rest); this file only handles render + flags.

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

func (a *Application) newTopologyCmd() *cobra.Command {
	var (
		follow      bool
		includeEnis bool
		sinceID     uint64
		jsonOut     bool
	)
	c := &cobra.Command{
		Use:   "topology",
		Short: "Show cluster topology (one-shot snapshot, or --follow for live stream)",
		Long: `Show the fleet topology served by dashd's ClusterService.

Without --follow, prints a one-shot snapshot (the same payload that
GET /v1/cluster/topology returns).

With --follow, opens the SSE stream and prints every TopologyEvent as
it arrives — equivalent to the browser's /topology-v2 page but rendered
as text. Streaming honours Last-Event-ID resume via --since-id.

Examples:
  dashctl topology                                 # pretty tree
  dashctl topology -o json                         # machine-readable
  dashctl topology --include-enis                  # include per-DPU ENI list
  dashctl topology --follow                        # live stream (Ctrl-C to stop)
  dashctl topology --follow --since-id 42          # resume after cursor 42
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx := cmd.Context()
			if follow {
				return a.runTopologyFollow(ctx, cl, includeEnis, sinceID, jsonOut)
			}
			tctx, cancel := withTimeout(ctx, rc)
			defer cancel()
			return a.runTopologySnapshot(tctx, cl, includeEnis, jsonOut)
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "stream live events (SSE) until Ctrl-C")
	c.Flags().BoolVar(&includeEnis, "include-enis", false, "include per-DPU ENI list in payloads")
	c.Flags().Uint64Var(&sinceID, "since-id", 0, "resume the stream after this event_id (Last-Event-ID)")
	c.Flags().BoolVar(&jsonOut, "json", false, "emit raw JSON (snapshot or each event) instead of pretty output")
	return c
}

// runTopologySnapshot fetches + renders one TopologyResponse.
func (a *Application) runTopologySnapshot(ctx context.Context, cl client.Client, includeEnis, jsonOut bool) error {
	snap, err := cl.GetTopology(ctx, includeEnis)
	if err != nil {
		return err
	}
	if jsonOut || a.Flags.output == "json" {
		b, merr := json.MarshalIndent(snap, "", "  ")
		if merr != nil {
			return merr
		}
		fmt.Fprintln(a.Out, string(b))
		return nil
	}
	renderSnapshotTree(a.Out, snap)
	return nil
}

// runTopologyFollow opens the SSE stream and prints events until the
// context is cancelled (Ctrl-C). One line per event for the pretty
// renderer; one JSON object per line in --json mode.
func (a *Application) runTopologyFollow(ctx context.Context, cl client.Client, includeEnis bool, sinceID uint64, jsonOut bool) error {
	pretty := !(jsonOut || a.Flags.output == "json")
	if pretty {
		fmt.Fprintf(a.Out, "→ following dashd topology stream (cursor=%d include_enis=%t). Ctrl-C to stop.\n", sinceID, includeEnis)
	}
	onEvent := func(ev client.TopologyEvent) error {
		if pretty {
			fmt.Fprintln(a.Out, renderEventLine(ev))
		} else {
			b, err := json.Marshal(ev)
			if err != nil {
				return nil // skip malformed line; keep stream alive
			}
			fmt.Fprintln(a.Out, string(b))
		}
		return nil
	}
	err := cl.StreamTopology(ctx, client.TopologyWatchOptions{
		IncludeEnis: includeEnis,
		LastEventID: sinceID,
		OnEvent:     onEvent,
	})
	// Treat context cancellation as a clean exit.
	if err != nil && ctx.Err() != nil {
		return nil
	}
	return err
}

// ── pretty renderers ─────────────────────────────────────────────────

// renderSnapshotTree writes a 3-section human-friendly view: cluster
// header, appliances grouped by zone, per-namespace object counts.
func renderSnapshotTree(w interface{ Write([]byte) (int, error) }, s *client.TopologySnapshot) {
	if s == nil {
		fmt.Fprintln(safeWriter{w}, "(empty topology)")
		return
	}
	out := safeWriter{w}
	if s.Cluster != nil {
		c := s.Cluster
		status := "healthy"
		if !c.Healthy {
			status = "degraded"
		}
		fmt.Fprintf(out, "CLUSTER  nodes=%d  leader=%s  status=%s\n", c.NodeCount, dashOr(c.LeaderID), status)
		for _, n := range c.Nodes {
			marker := "  "
			if n.IsLeader {
				marker = " *"
			}
			fmt.Fprintf(out, "%s %s   rest=%s  grpc=%s  ver=%s\n", marker,
				n.NodeID, dashOr(n.RestAddr), dashOr(n.GrpcAddr), dashOr(n.Version))
		}
	}

	if s.Summary != nil {
		fmt.Fprintf(out, "\nSUMMARY  appliances=%d  dpus=%d  enis=%d  healthy=%d  degraded=%d  offline=%d  cordoned=%d\n",
			s.Summary.TotalAppliances, s.Summary.TotalDpus, s.Summary.TotalEnis,
			s.Summary.HealthyDpus, s.Summary.DegradedDpus, s.Summary.OfflineDpus, s.Summary.CordonedDpus)
	}

	if len(s.Appliances) > 0 {
		// Group by zone for nicer output.
		byZone := map[string][]client.TopologyAppliance{}
		zoneOrder := []string{}
		for _, a := range s.Appliances {
			z := a.Zone
			if z == "" {
				z = "(unzoned)"
			}
			if _, ok := byZone[z]; !ok {
				zoneOrder = append(zoneOrder, z)
			}
			byZone[z] = append(byZone[z], a)
		}
		sort.Strings(zoneOrder)
		fmt.Fprintln(out, "\nAPPLIANCES")
		for _, z := range zoneOrder {
			fmt.Fprintf(out, "  zone=%s\n", z)
			for _, ap := range byZone[z] {
				fmt.Fprintf(out, "    %s  tier=%s  dpus=%d\n", ap.ID, dashOr(ap.Tier), len(ap.Dpus))
				for _, d := range ap.Dpus {
					cord := ""
					if d.Cordoned {
						cord = " CORDONED"
					}
					fmt.Fprintf(out, "      %-12s slot=%d  state=%s  enis=%d%s\n",
						d.ID, d.Slot, d.State, d.EniCount, cord)
					for _, e := range d.Enis {
						fmt.Fprintf(out, "          eni=%-20s ns=%-10s vnet=%-15s mac=%s admin=%s\n",
							e.Name, dashOr(e.Namespace), dashOr(e.VnetName), dashOr(e.MacAddress), dashOr(e.AdminState))
					}
				}
			}
		}
	}

	if len(s.Objects) > 0 {
		fmt.Fprintln(out, "\nOBJECTS (per namespace)")
		nss := make([]string, 0, len(s.Objects))
		for k := range s.Objects {
			nss = append(nss, k)
		}
		sort.Strings(nss)
		for _, ns := range nss {
			o := s.Objects[ns]
			fmt.Fprintf(out, "  %-10s vnets=%d  enis=%d  mappings=%d  acls=%d  routes=%d  ha_sets=%d  tunnels=%d\n",
				ns, o.Vnets, o.Enis, o.VnetMappings, o.AclPolicies, o.RoutePolicies, o.HaSets, o.ServiceTunnels)
		}
	}
}

// renderEventLine renders a single TopologyEvent as a one-line summary
// for --follow's pretty mode.
func renderEventLine(ev client.TopologyEvent) string {
	kind := strings.TrimPrefix(ev.Kind, "KIND_")
	if kind == "" {
		kind = "UNKNOWN"
	}
	parts := []string{
		fmt.Sprintf("#%-6d", ev.EventID),
		ev.Ts,
		fmt.Sprintf("%-15s", kind),
	}
	switch {
	case ev.Snapshot != nil && ev.Snapshot.Summary != nil:
		s := ev.Snapshot.Summary
		parts = append(parts, fmt.Sprintf("appliances=%d dpus=%d enis=%d", s.TotalAppliances, s.TotalDpus, s.TotalEnis))
	case ev.Peer != nil:
		parts = append(parts, fmt.Sprintf("peer=%s leader=%t addr=%s", ev.Peer.NodeID, ev.Peer.IsLeader, dashOr(ev.Peer.RestAddr)))
	case ev.Dpu != nil:
		cord := ""
		if ev.Dpu.Cordoned {
			cord = " CORDONED"
		}
		parts = append(parts, fmt.Sprintf("dpu=%s state=%s enis=%d%s", ev.Dpu.ID, ev.Dpu.State, ev.Dpu.EniCount, cord))
	case ev.NewLeaderID != "" || ev.OldLeaderID != "":
		parts = append(parts, fmt.Sprintf("%s → %s", dashOr(ev.OldLeaderID), dashOr(ev.NewLeaderID)))
	case ev.Notice != nil:
		bits := []string{}
		if ev.Notice.Message != "" {
			bits = append(bits, ev.Notice.Message)
		}
		if ev.Notice.DroppedCount > 0 {
			bits = append(bits, fmt.Sprintf("dropped=%d", ev.Notice.DroppedCount))
		}
		if ev.Notice.SuppressedCount > 0 {
			bits = append(bits, fmt.Sprintf("suppressed=%d", ev.Notice.SuppressedCount))
		}
		if ev.Notice.CurrentEventID > 0 {
			bits = append(bits, fmt.Sprintf("current_id=%d", ev.Notice.CurrentEventID))
		}
		parts = append(parts, strings.Join(bits, " "))
	}
	return strings.Join(parts, "  ")
}

// ── tiny local helpers ───────────────────────────────────────────────

func dashOr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// safeWriter swallows Write errors (operator CLI; if stdout is broken we
// can't report it anyway).
type safeWriter struct{ w interface{ Write([]byte) (int, error) } }

func (s safeWriter) Write(p []byte) (int, error) { return s.w.Write(p) }
