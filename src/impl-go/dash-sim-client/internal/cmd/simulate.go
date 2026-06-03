package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func newSimulateCmd() *cobra.Command {
	var (
		direction string
		eni       string
		vni       uint32
		srcMac    string
		dstMac    string
		srcIP     string
		dstIP     string
		protocol  uint32
		srcPort   uint32
		dstPort   uint32
		length    uint32
		trace     bool
		file      string
	)
	c := &cobra.Command{
		Use:   "simulate",
		Short: "Run one synthetic packet through the DASH pipeline",
		Long: `simulate exercises the behavioural DASH pipeline (direction lookup
-> ACL stages 1..5 -> route lookup -> vnet_mapping encap) and prints the
resulting Decision plus an optional trace.

Two input modes:

  --file <path>       read a Packet (JSON or YAML) from disk.
  --direction ... --eni ... ...   build a Packet from flags.

Example (outbound, encap via vnet_mapping):

  dash-sim-client simulate --direction outbound --eni eni-001 \
      --src-ip 10.0.0.1 --dst-ip 10.1.0.10 --protocol 6 --dst-port 80 --trace`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()

			var pkt *dashapi.Packet
			if file != "" {
				raw, err := os.ReadFile(file)
				if err != nil {
					return err
				}
				pkt = &dashapi.Packet{}
				if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(raw, pkt); err != nil {
					return fmt.Errorf("parse packet: %w", err)
				}
			} else {
				dir, err := parseDirection(direction)
				if err != nil {
					return err
				}
				pkt = &dashapi.Packet{
					Direction:   dir,
					Eni:         eni,
					Vni:         vni,
					SrcMac:      srcMac,
					DstMac:      dstMac,
					SrcIp:       srcIP,
					DstIp:       dstIP,
					Protocol:    protocol,
					SrcPort:     srcPort,
					DstPort:     dstPort,
					LengthBytes: length,
				}
			}

			ctx, cancel := rpcContext()
			defer cancel()
			resp, err := cl.Raw().SimulatePacket(ctx, &dashapi.SimulatePacketRequest{
				Packet: pkt, Trace: trace,
			})
			if err != nil {
				return err
			}
			return printDecision(resp.GetDecision(), trace)
		},
	}
	c.Flags().StringVar(&file, "file", "", "path to a Packet JSON/YAML file")
	c.Flags().StringVar(&direction, "direction", "outbound", "outbound|inbound")
	c.Flags().StringVar(&eni, "eni", "", "ENI key")
	c.Flags().Uint32Var(&vni, "vni", 0, "VNI (inbound)")
	c.Flags().StringVar(&srcMac, "src-mac", "", "source MAC (e.g. 00:11:22:33:44:55)")
	c.Flags().StringVar(&dstMac, "dst-mac", "", "destination MAC")
	c.Flags().StringVar(&srcIP, "src-ip", "", "source IP")
	c.Flags().StringVar(&dstIP, "dst-ip", "", "destination IP")
	c.Flags().Uint32Var(&protocol, "protocol", 0, "IP protocol (6=TCP, 17=UDP, 1=ICMP)")
	c.Flags().Uint32Var(&srcPort, "src-port", 0, "L4 source port")
	c.Flags().Uint32Var(&dstPort, "dst-port", 0, "L4 destination port")
	c.Flags().Uint32Var(&length, "length", 64, "packet length in bytes")
	c.Flags().BoolVar(&trace, "trace", false, "include per-step pipeline trace")
	return c
}

func parseDirection(s string) (dashapi.Packet_Direction, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "outbound", "out", "tx", "egress":
		return dashapi.Packet_DIRECTION_OUTBOUND, nil
	case "inbound", "in", "rx", "ingress":
		return dashapi.Packet_DIRECTION_INBOUND, nil
	}
	return 0, fmt.Errorf("unknown --direction %q (want outbound|inbound)", s)
}

func printDecision(d *dashapi.Decision, withTrace bool) error {
	if d == nil {
		return fmt.Errorf("nil decision")
	}
	out := map[string]interface{}{
		"action":           strings.TrimPrefix(d.GetAction().String(), "ACTION_"),
		"reason":           d.GetReason(),
		"out_eni":          d.GetOutEni(),
		"out_underlay_ip":  d.GetOutUnderlayIp(),
		"out_vni":          d.GetOutVni(),
		"out_routing_type": d.GetOutRoutingType(),
		"matched_acl_stage":    d.GetMatchedAclStage(),
		"matched_acl_priority": d.GetMatchedAclPriority(),
		"matched_route_prefix": d.GetMatchedRoutePrefix(),
	}
	if withTrace {
		out["trace"] = d.GetTrace()
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
