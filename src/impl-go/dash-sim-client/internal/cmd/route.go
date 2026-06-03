package cmd

import (
	"os"
	"strings"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newRouteCmd() *cobra.Command {
	root := &cobra.Command{Use: "route", Short: "Manage routes"}
	root.AddCommand(newRouteAddCmd())
	root.AddCommand(newRouteListCmd())
	root.AddCommand(newRouteDeleteCmd())
	return root
}

func newRouteAddCmd() *cobra.Command {
	var table, dst, action, nh, vnetID string
	c := &cobra.Command{
		Use:   "add",
		Args:  cobra.NoArgs,
		Short: "Add a route to a routing table",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.AddRoute(ctx, &dashsimv1.Route{
				Table:     table,
				DstPrefix: dst,
				Action:    parseRouteAction(action),
				NextHopIp: nh,
				VnetId:    vnetID,
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&table, "table", "", "routing table id (required)")
	c.Flags().StringVar(&dst, "dst-prefix", "", "destination CIDR (required)")
	c.Flags().StringVar(&action, "action", "FORWARD", "FORWARD, DROP or ENCAP")
	c.Flags().StringVar(&nh, "next-hop-ip", "", "next-hop IP")
	c.Flags().StringVar(&vnetID, "vnet-id", "", "optional VNET id reference")
	_ = c.MarkFlagRequired("table")
	_ = c.MarkFlagRequired("dst-prefix")
	return c
}

func newRouteListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List routes",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.ListRoutes(ctx)
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.Many[*dashsimv1.Route](os.Stdout, f, items, routeCols, routeRow)
		},
	}
}

func newRouteDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <route-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a route by id (table/dst_prefix)",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.DeleteRoute(ctx, args[0])
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
}

var routeCols = []string{"ID", "TABLE", "DST", "ACTION", "NEXTHOP", "VNET"}

func routeRow(r *dashsimv1.Route) []string {
	return []string{
		r.GetId(),
		r.GetTable(),
		r.GetDstPrefix(),
		strings.TrimPrefix(r.GetAction().String(), "ROUTE_ACTION_"),
		r.GetNextHopIp(),
		r.GetVnetId(),
	}
}

