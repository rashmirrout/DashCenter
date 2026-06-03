package cmd

import (
	"fmt"
	"os"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newVnetCmd() *cobra.Command {
	root := &cobra.Command{Use: "vnet", Short: "Manage VNETs"}
	root.AddCommand(newVnetCreateCmd())
	root.AddCommand(newVnetGetCmd())
	root.AddCommand(newVnetListCmd())
	root.AddCommand(newVnetDeleteCmd())
	return root
}

func newVnetCreateCmd() *cobra.Command {
	var vni uint32
	var labels []string
	c := &cobra.Command{
		Use:   "create <vnet-id>",
		Short: "Create a VNET",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.CreateVnet(ctx, &dashsimv1.Vnet{
				Id:     args[0],
				Vni:    vni,
				Labels: parseLabels(labels),
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().Uint32Var(&vni, "vni", 0, "VNI for the VNET")
	c.Flags().StringSliceVar(&labels, "label", nil, "labels key=val (repeatable)")
	return c
}

func newVnetGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <vnet-id>",
		Short: "Fetch a single VNET",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			v, err := cl.GetVnet(ctx, args[0])
			if err != nil {
				return err
			}
			f, err := resolveFormat()
			if err != nil {
				return err
			}
			return render.One(os.Stdout, f, v, vnetCols, vnetRow)
		},
	}
}

func newVnetListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all VNETs",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.ListVnets(ctx)
			if err != nil {
				return err
			}
			f, err := resolveFormat()
			if err != nil {
				return err
			}
			return render.Many[*dashsimv1.Vnet](os.Stdout, f, items, vnetCols, vnetRow)
		},
	}
}

func newVnetDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <vnet-id>",
		Short: "Delete a VNET (cascades to ENIs/mappings/routes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.DeleteVnet(ctx, args[0])
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
}

var vnetCols = []string{"ID", "VNI", "LABELS", "CREATED_NS"}

func vnetRow(v *dashsimv1.Vnet) []string {
	return []string{v.GetId(), fmt.Sprintf("%d", v.GetVni()), fmtLabels(v.GetLabels()), fmt.Sprintf("%d", v.GetCreatedTsNs())}
}
