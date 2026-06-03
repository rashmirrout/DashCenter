package cmd

import (
	"fmt"
	"os"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newEniCmd() *cobra.Command {
	root := &cobra.Command{Use: "eni", Short: "Manage ENIs"}
	root.AddCommand(newEniCreateCmd())
	root.AddCommand(newEniGetCmd())
	root.AddCommand(newEniListCmd())
	root.AddCommand(newEniDeleteCmd())
	root.AddCommand(newEniUpdateCmd())
	return root
}

func newEniCreateCmd() *cobra.Command {
	var (
		vnetID, mac, adminState string
		addresses               []string
		bwMin, bwMax            uint64
	)
	c := &cobra.Command{
		Use:   "create <eni-id>",
		Short: "Create an ENI attached to a VNET",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.CreateEni(ctx, &dashsimv1.Eni{
				Id:              args[0],
				VnetId:          vnetID,
				Mac:             mac,
				Addresses:       addresses,
				AdminState:      adminState,
				BandwidthMinBps: bwMin,
				BandwidthMaxBps: bwMax,
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&vnetID, "vnet-id", "", "owning VNET id (required)")
	c.Flags().StringVar(&mac, "mac", "", "MAC address (e.g. 00:11:22:33:44:55)")
	c.Flags().StringSliceVar(&addresses, "address", nil, "ENI address (repeatable)")
	c.Flags().StringVar(&adminState, "admin-state", "up", "admin state: up|down")
	c.Flags().Uint64Var(&bwMin, "bw-min-bps", 0, "minimum guaranteed bandwidth in bps")
	c.Flags().Uint64Var(&bwMax, "bw-max-bps", 0, "maximum bandwidth in bps")
	_ = c.MarkFlagRequired("vnet-id")
	return c
}

func newEniUpdateCmd() *cobra.Command {
	var (
		vnetID, mac, adminState string
		addresses               []string
		bwMin, bwMax            uint64
	)
	c := &cobra.Command{
		Use:   "update <eni-id>",
		Short: "Replace an ENI's fields (full-object update)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.UpdateEni(ctx, &dashsimv1.Eni{
				Id:              args[0],
				VnetId:          vnetID,
				Mac:             mac,
				Addresses:       addresses,
				AdminState:      adminState,
				BandwidthMinBps: bwMin,
				BandwidthMaxBps: bwMax,
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&vnetID, "vnet-id", "", "owning VNET id")
	c.Flags().StringVar(&mac, "mac", "", "MAC address")
	c.Flags().StringSliceVar(&addresses, "address", nil, "ENI address (repeatable)")
	c.Flags().StringVar(&adminState, "admin-state", "", "admin state: up|down")
	c.Flags().Uint64Var(&bwMin, "bw-min-bps", 0, "minimum guaranteed bandwidth")
	c.Flags().Uint64Var(&bwMax, "bw-max-bps", 0, "maximum bandwidth")
	return c
}

func newEniGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <eni-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Fetch a single ENI",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			e, err := cl.GetEni(ctx, args[0])
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.One(os.Stdout, f, e, eniCols, eniRow)
		},
	}
}

func newEniListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List all ENIs",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.ListEnis(ctx)
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.Many[*dashsimv1.Eni](os.Stdout, f, items, eniCols, eniRow)
		},
	}
}

func newEniDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <eni-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete an ENI",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.DeleteEni(ctx, args[0])
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
}

var eniCols = []string{"ID", "VNET", "MAC", "ADDRS", "ADMIN", "BW_MAX"}

func eniRow(e *dashsimv1.Eni) []string {
	addrs := ""
	for i, a := range e.GetAddresses() {
		if i > 0 {
			addrs += ","
		}
		addrs += a
	}
	return []string{e.GetId(), e.GetVnetId(), e.GetMac(), addrs, e.GetAdminState(), fmt.Sprintf("%d", e.GetBandwidthMaxBps())}
}
