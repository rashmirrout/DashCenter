package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

// kindFields is an offline reference of dashd spec field shapes. We do
// NOT embed proto descriptors in Phase 1 (no proto deps in dashctl); the
// reference here is hand-curated and tracked alongside dashd's
// dashcenter.v1 protos via PR review.
var kindFields = map[string][]fieldDoc{
	"Vnet": {
		{"namespace", "string", "Tenant namespace (defaults to 'default')."},
		{"name", "string", "VNet name. Required and unique within a namespace."},
		{"vni", "uint32", "L2/L3 VNet identifier."},
		{"guid", "string", "Optional opaque global identifier."},
		{"labels", "map<string,string>", "Free-form labels for selectors."},
		{"expected_generation", "uint64", "If non-zero, optimistic concurrency."},
	},
	"Eni": {
		{"namespace", "string", "Tenant namespace."},
		{"name", "string", "ENI name. Required."},
		{"vnet_name", "string", "VNet this ENI belongs to."},
		{"mac_address", "string", "MAC in canonical aa:bb:cc:dd:ee:ff."},
		{"underlay_ip", "string", "DPU underlay IP."},
		{"admin_state", "string", "'up' or 'down'."},
		{"placement_hint_dpu_ids", "[]string", "Operator placement hint."},
		{"resimulate_flows", "bool", "Force re-evaluation of existing flows on Apply."},
		{"labels", "map<string,string>", "Free-form labels."},
		{"expected_generation", "uint64", "CAS."},
	},
	"VnetMapping": {
		{"namespace", "string", "Tenant namespace."},
		{"vnet_name", "string", "Owning VNet."},
		{"ip_address", "string", "Overlay IP being mapped."},
		{"underlay_ip", "string", "Underlay (physical) address."},
		{"mac_address", "string", "Destination MAC."},
		{"action", "string", "'vnet_encap' | 'service_tunnel' | 'drop' | ..."},
	},
	"AclPolicy": {
		{"namespace", "string", "Tenant namespace."},
		{"name", "string", "Policy name."},
		{"stage", "string", "'inbound' | 'outbound'."},
		{"eni_names", "[]string", "Bound ENIs."},
		{"rules", "[]AclRuleSpec", "Ordered rules."},
	},
	"RoutePolicy": {
		{"namespace", "string", "Tenant namespace."},
		{"name", "string", "Policy name."},
		{"eni_names", "[]string", "Bound ENIs."},
		{"routes", "[]RouteSpec", "Routes (prefix, next-hop, optional ECMP)."},
	},
	"HaSet": {
		{"namespace", "string", "Tenant namespace."},
		{"name", "string", "HA set name."},
	},
	"ServiceTunnel": {
		{"namespace", "string", "Tenant namespace (Phase 2)."},
		{"name", "string", "Service tunnel name."},
	},
	"Inventory": {
		{"dpus", "[]DpuInput", "Each entry: { id, endpoint, labels? }."},
	},
}

type fieldDoc struct {
	Name string
	Type string
	Desc string
}

func (a *Application) newExplainCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explain <kind>",
		Short: "Field reference for a spec kind (offline)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ki, ok := manifest.LookupKind(args[0])
			if !ok {
				return errors.Newf(errors.CodeInvalidArgument, "explain: unknown kind %q", args[0])
			}
			fields, ok := kindFields[ki.Kind]
			if !ok {
				return errors.Newf(errors.CodeInvalidArgument, "explain: no field reference registered for %s", ki.Kind)
			}
			fmt.Fprintf(a.Out, "KIND:     %s\nVERSION:  %s\n\nFIELDS:\n", ki.Kind, manifest.APIVersion)
			for _, f := range fields {
				fmt.Fprintf(a.Out, "  %s\t<%s>\n    %s\n\n", f.Name, f.Type, f.Desc)
			}
			return nil
		},
	}
}
