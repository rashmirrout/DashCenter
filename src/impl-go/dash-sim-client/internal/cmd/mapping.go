package cmd

import (
	"fmt"
	"os"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newMappingCmd() *cobra.Command {
	root := &cobra.Command{Use: "mapping", Short: "Manage VNET overlay->underlay mappings"}
	root.AddCommand(newMappingAddCmd())
	root.AddCommand(newMappingListCmd())
	root.AddCommand(newMappingDeleteCmd())
	return root
}

func newMappingAddCmd() *cobra.Command {
	var vnetID, overlay, underlay, mac string
	var vni uint32
	c := &cobra.Command{
		Use:   "add",
		Args:  cobra.NoArgs,
		Short: "Add an overlay->underlay mapping in a VNET",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.AddVnetMapping(ctx, &dashsimv1.VnetMapping{
				VnetId:     vnetID,
				OverlayIp:  overlay,
				UnderlayIp: underlay,
				Mac:        mac,
				Vni:        vni,
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&vnetID, "vnet-id", "", "owning VNET id (required)")
	c.Flags().StringVar(&overlay, "overlay-ip", "", "overlay IP (required)")
	c.Flags().StringVar(&underlay, "underlay-ip", "", "underlay (physical) IP")
	c.Flags().StringVar(&mac, "mac", "", "MAC behind the overlay IP")
	c.Flags().Uint32Var(&vni, "vni", 0, "VNI to encap with")
	_ = c.MarkFlagRequired("vnet-id")
	_ = c.MarkFlagRequired("overlay-ip")
	return c
}

func newMappingListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List VNET mappings",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.ListVnetMappings(ctx)
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.Many[*dashsimv1.VnetMapping](os.Stdout, f, items, mapCols, mapRow)
		},
	}
}

func newMappingDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <mapping-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete a VNET mapping by id (vnet_id/overlay_ip)",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.DeleteVnetMapping(ctx, args[0])
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
}

var mapCols = []string{"ID", "VNET", "OVERLAY", "UNDERLAY", "MAC", "VNI"}

func mapRow(x *dashsimv1.VnetMapping) []string {
	return []string{x.GetId(), x.GetVnetId(), x.GetOverlayIp(), x.GetUnderlayIp(), x.GetMac(), fmt.Sprintf("%d", x.GetVni())}
}
