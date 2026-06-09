package render

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// ItemName satisfies render.Namer for StoredItems.
func ItemName(it *client.StoredItem) string {
	if it == nil {
		return ""
	}
	return it.Kind + "/" + it.Name
}

// ColumnsFor returns the table column set for one of dashd's spec kinds.
// `kind` is the lowercase store kind ("vnet", "eni", "vnet_mapping", ...).
func ColumnsFor(kind string) []Column {
	switch kind {
	case "vnet":
		return vnetColumns()
	case "eni":
		return eniColumns()
	case "vnet_mapping":
		return vnetMappingColumns()
	case "acl_policy":
		return aclPolicyColumns()
	case "route_policy":
		return routePolicyColumns()
	case "ha_set":
		return haSetColumns()
	case "service_tunnel":
		return serviceTunnelColumns()
	default:
		return genericColumns()
	}
}

// Generic columns (when the kind has no dedicated set).
func genericColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "GENERATION", Get: func(o any) string { return readField(o, "generation") }},
	}
}

func vnetColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "VNI", Get: func(o any) string { return readSpecField(o, "vni") }},
		{Header: "GENERATION", Get: func(o any) string { return readField(o, "generation") }},
		{Header: "LABELS", Wide: false, Get: func(o any) string { return formatLabels(readSpecLabels(o)) }},
	}
}

func eniColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "VNET", Get: func(o any) string { return readSpecField(o, "vnet_name") }},
		{Header: "MAC", Get: func(o any) string { return readSpecField(o, "mac_address") }},
		{Header: "UNDERLAY", Get: func(o any) string { return readSpecField(o, "underlay_ip") }},
		{Header: "ADMIN", Get: func(o any) string { return readSpecField(o, "admin_state") }},
		{Header: "PLACED-ON", Wide: true, Get: func(o any) string { return formatList(readSpecListField(o, "placement_hint_dpu_ids")) }},
		{Header: "GEN", Get: func(o any) string { return readField(o, "generation") }},
	}
}

func vnetMappingColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "VNET", Get: func(o any) string { return readSpecField(o, "vnet_name") }},
		{Header: "OVERLAY", Get: func(o any) string { return readSpecField(o, "ip_address") }},
		{Header: "UNDERLAY", Get: func(o any) string { return readSpecField(o, "underlay_ip") }},
		{Header: "ACTION", Get: func(o any) string { return readSpecField(o, "action") }},
		{Header: "GEN", Get: func(o any) string { return readField(o, "generation") }},
	}
}

func aclPolicyColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "STAGE", Get: func(o any) string { return readSpecField(o, "stage") }},
		{Header: "ENIs", Get: func(o any) string { return formatList(readSpecListField(o, "eni_names")) }},
		{Header: "RULES", Get: func(o any) string { return fmt.Sprintf("%d", lenSpecList(o, "rules")) }},
		{Header: "GEN", Get: func(o any) string { return readField(o, "generation") }},
	}
}

func routePolicyColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "ENIs", Get: func(o any) string { return formatList(readSpecListField(o, "eni_names")) }},
		{Header: "ROUTES", Get: func(o any) string { return fmt.Sprintf("%d", lenSpecList(o, "routes")) }},
		{Header: "GEN", Get: func(o any) string { return readField(o, "generation") }},
	}
}

func haSetColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "GEN", Get: func(o any) string { return readField(o, "generation") }},
	}
}

func serviceTunnelColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readField(o, "namespace") }},
		{Header: "NAME", Get: func(o any) string { return readField(o, "name") }},
		{Header: "GEN", Get: func(o any) string { return readField(o, "generation") }},
	}
}

// DpuColumns is used by `dashctl dpu list` and `dashctl get dpus`.
func DpuColumns() []Column {
	return []Column{
		{Header: "ID", Get: func(o any) string { return readDpuField(o, "id") }},
		{Header: "ENDPOINT", Get: func(o any) string { return readDpuField(o, "endpoint") }},
		{Header: "STATE", Get: func(o any) string { return readDpuField(o, "state") }},
		{Header: "LAST_SEEN", Get: func(o any) string { return readDpuField(o, "last_seen") }},
		{Header: "LABELS", Wide: true, Get: func(o any) string {
			if s, ok := o.(client.DpuStatus); ok {
				return formatLabels(s.Labels)
			}
			if s, ok := o.(*client.DpuStatus); ok {
				return formatLabels(s.Labels)
			}
			return ""
		}},
	}
}

// DriftColumns is used by `dashctl dpu drift`.
func DriftColumns() []Column {
	return []Column{
		{Header: "DPU", Get: func(o any) string { return readDriftField(o, "dpu_id") }},
		{Header: "OP", Get: func(o any) string { return readDriftField(o, "op") }},
		{Header: "KIND", Get: func(o any) string { return readDriftField(o, "kind") }},
		{Header: "KEY", Get: func(o any) string { return readDriftField(o, "key") }},
		{Header: "REASON", Get: func(o any) string { return readDriftField(o, "detail") }},
	}
}

// PlacementColumns is used by `dashctl get eni -o wide` and `dpu describe`.
func PlacementColumns() []Column {
	return []Column{
		{Header: "NAMESPACE", Get: func(o any) string { return readPlacementField(o, "namespace") }},
		{Header: "ENI", Get: func(o any) string { return readPlacementField(o, "eni_name") }},
		{Header: "VNET", Get: func(o any) string { return readPlacementField(o, "vnet_name") }},
		{Header: "DPU", Get: func(o any) string { return readPlacementField(o, "dpu_id") }},
		{Header: "OBSERVED", Get: func(o any) string { return readPlacementField(o, "observed") }},
	}
}

// --- accessors ---

func readField(o any, name string) string {
	switch x := o.(type) {
	case *client.StoredItem:
		if x == nil {
			return ""
		}
		switch name {
		case "namespace":
			return x.Namespace
		case "name":
			return x.Name
		case "kind":
			return x.Kind
		case "generation":
			return fmt.Sprintf("%d", x.Generation)
		}
	case client.StoredItem:
		return readField(&x, name)
	case map[string]any:
		if v, ok := x[name]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func readSpecField(o any, key string) string {
	spec := extractSpec(o)
	if spec == nil {
		return ""
	}
	if v, ok := spec[key]; ok {
		switch t := v.(type) {
		case string:
			return t
		case float64:
			return formatNumber(t)
		case bool:
			if t {
				return "true"
			}
			return "false"
		default:
			bs, _ := json.Marshal(v)
			return string(bs)
		}
	}
	return ""
}

func readSpecLabels(o any) map[string]string {
	spec := extractSpec(o)
	if spec == nil {
		return nil
	}
	raw, ok := spec["labels"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func readSpecListField(o any, key string) []string {
	spec := extractSpec(o)
	if spec == nil {
		return nil
	}
	raw, ok := spec[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func lenSpecList(o any, key string) int {
	spec := extractSpec(o)
	if spec == nil {
		return 0
	}
	raw, ok := spec[key].([]any)
	if !ok {
		return 0
	}
	return len(raw)
}

func extractSpec(o any) map[string]any {
	switch x := o.(type) {
	case *client.StoredItem:
		if x == nil || len(x.Spec) == 0 {
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal(x.Spec, &m); err != nil {
			return nil
		}
		return m
	case client.StoredItem:
		return extractSpec(&x)
	case map[string]any:
		if v, ok := x["spec"]; ok {
			if m, ok := v.(map[string]any); ok {
				return m
			}
		}
	}
	return nil
}

func readDpuField(o any, name string) string {
	switch x := o.(type) {
	case client.DpuStatus:
		return readDpuField(&x, name)
	case *client.DpuStatus:
		if x == nil {
			return ""
		}
		switch name {
		case "id":
			return x.ID
		case "endpoint":
			return x.Endpoint
		case "state":
			return x.State
		case "last_seen":
			return x.LastSeen
		}
	case map[string]any:
		if v, ok := x[name]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func readDriftField(o any, name string) string {
	switch x := o.(type) {
	case client.DriftItem:
		return readDriftField(&x, name)
	case *client.DriftItem:
		if x == nil {
			return ""
		}
		switch name {
		case "dpu_id":
			return x.DpuID
		case "op":
			return x.Op
		case "kind":
			return x.Kind
		case "key":
			return x.Key
		case "detail":
			return x.Detail
		}
	case map[string]any:
		if v, ok := x[name]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func readPlacementField(o any, name string) string {
	switch x := o.(type) {
	case client.EniPlacementRow:
		return readPlacementField(&x, name)
	case *client.EniPlacementRow:
		if x == nil {
			return ""
		}
		switch name {
		case "namespace":
			return x.Namespace
		case "eni_name":
			return x.EniName
		case "vnet_name":
			return x.VnetName
		case "dpu_id":
			return x.DpuID
		case "observed":
			if x.Observed {
				return "true"
			}
			return "false"
		}
	}
	return ""
}

func formatLabels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + m[k]
	}
	return strings.Join(parts, ",")
}

func formatList(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return strings.Join(ss, ",")
}
