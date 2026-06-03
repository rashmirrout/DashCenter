// Package scenarios loads a YAML scenario file into the model.Store. The YAML
// schema deliberately mirrors a pared-down DASH appliance config so users can
// hand-author them.
//
// Example:
//
//	apiVersion: dashsim/v1
//	kind: Scenario
//	metadata:
//	  name: small
//	spec:
//	  vnets:
//	    - id: vnet-prod
//	      vni: 1001
//	  enis:
//	    - id: eni-001
//	      vnet_id: vnet-prod
//	      mac: 00:11:22:33:44:55
//	      addresses: [10.0.0.10]
//	  acl_groups:
//	    - id: acl-prod-in
//	      stage: INBOUND
//	  acl_rules:
//	    - group_id: acl-prod-in
//	      num: 100
//	      action: ALLOW
//	      src_prefix: 0.0.0.0/0
//	      dst_prefix: 10.0.0.0/24
//	  routes:
//	    - table: vnet-prod
//	      dst_prefix: 10.1.0.0/16
//	      action: FORWARD
//	      next_hop_ip: 10.0.0.1
//	  vnet_mappings:
//	    - vnet_id: vnet-prod
//	      overlay_ip: 10.0.0.20
//	      underlay_ip: 100.64.0.20
//	      mac: 00:aa:bb:cc:dd:ee
//	      vni: 1001
package scenarios

import (
	"fmt"
	"os"
	"strings"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"gopkg.in/yaml.v3"
)

// Document is the on-disk YAML shape.
type Document struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

// Metadata is informational only; ignored by Load.
type Metadata struct {
	Name     string `yaml:"name"`
	DeviceID string `yaml:"device_id"`
}

// Spec is the actual object inventory.
type Spec struct {
	Vnets        []VnetSpec        `yaml:"vnets"`
	Enis         []EniSpec         `yaml:"enis"`
	AclGroups    []AclGroupSpec    `yaml:"acl_groups"`
	AclRules     []AclRuleSpec     `yaml:"acl_rules"`
	Routes       []RouteSpec       `yaml:"routes"`
	VnetMappings []VnetMappingSpec `yaml:"vnet_mappings"`
}

type VnetSpec struct {
	ID     string            `yaml:"id"`
	VNI    uint32            `yaml:"vni"`
	Labels map[string]string `yaml:"labels"`
}

type EniSpec struct {
	ID              string            `yaml:"id"`
	VnetID          string            `yaml:"vnet_id"`
	MAC             string            `yaml:"mac"`
	Addresses       []string          `yaml:"addresses"`
	AdminState      string            `yaml:"admin_state"`
	BandwidthMinBps uint64            `yaml:"bandwidth_min_bps"`
	BandwidthMaxBps uint64            `yaml:"bandwidth_max_bps"`
	Labels          map[string]string `yaml:"labels"`
}

type AclGroupSpec struct {
	ID    string `yaml:"id"`
	Stage string `yaml:"stage"` // INBOUND or OUTBOUND
}

type AclRuleSpec struct {
	ID        string `yaml:"id"`
	GroupID   string `yaml:"group_id"`
	Num       uint32 `yaml:"num"`
	Action    string `yaml:"action"` // ALLOW or DENY
	SrcPrefix string `yaml:"src_prefix"`
	DstPrefix string `yaml:"dst_prefix"`
	Protocol  string `yaml:"protocol"`
	SrcPort   uint32 `yaml:"src_port"`
	DstPort   uint32 `yaml:"dst_port"`
}

type RouteSpec struct {
	ID        string `yaml:"id"`
	Table     string `yaml:"table"`
	DstPrefix string `yaml:"dst_prefix"`
	Action    string `yaml:"action"` // FORWARD, DROP, ENCAP
	NextHopIP string `yaml:"next_hop_ip"`
	VnetID    string `yaml:"vnet_id"`
}

type VnetMappingSpec struct {
	ID         string `yaml:"id"`
	VnetID     string `yaml:"vnet_id"`
	OverlayIP  string `yaml:"overlay_ip"`
	UnderlayIP string `yaml:"underlay_ip"`
	MAC        string `yaml:"mac"`
	VNI        uint32 `yaml:"vni"`
}

// LoadFile reads + parses + applies the YAML scenario at path. The store is
// NOT reset first; the caller is responsible.
func LoadFile(path string, store *model.Store) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("scenario: read %q: %w", path, err)
	}
	return LoadBytes(raw, store)
}

// LoadBytes parses raw YAML and applies the scenario.
func LoadBytes(raw []byte, store *model.Store) error {
	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("scenario: parse: %w", err)
	}
	return Apply(&doc, store)
}

// Apply pushes the parsed scenario into the store.
func Apply(doc *Document, store *model.Store) error {
	for _, v := range doc.Spec.Vnets {
		if _, err := store.CreateVnet(&dashsimv1.Vnet{
			Id:     v.ID,
			Vni:    v.VNI,
			Labels: v.Labels,
		}); err != nil {
			return fmt.Errorf("scenario: vnet %q: %w", v.ID, err)
		}
	}
	for _, e := range doc.Spec.Enis {
		if _, err := store.CreateEni(&dashsimv1.Eni{
			Id:              e.ID,
			VnetId:          e.VnetID,
			Mac:             e.MAC,
			Addresses:       e.Addresses,
			AdminState:      e.AdminState,
			BandwidthMinBps: e.BandwidthMinBps,
			BandwidthMaxBps: e.BandwidthMaxBps,
			Labels:          e.Labels,
		}); err != nil {
			return fmt.Errorf("scenario: eni %q: %w", e.ID, err)
		}
	}
	for _, g := range doc.Spec.AclGroups {
		if _, err := store.AddAclGroup(&dashsimv1.AclGroup{
			Id:    g.ID,
			Stage: parseAclStage(g.Stage),
		}); err != nil {
			return fmt.Errorf("scenario: acl_group %q: %w", g.ID, err)
		}
	}
	for _, r := range doc.Spec.AclRules {
		if _, err := store.AddAclRule(&dashsimv1.AclRule{
			Id:        r.ID,
			GroupId:   r.GroupID,
			Num:       r.Num,
			Action:    parseAclAction(r.Action),
			SrcPrefix: r.SrcPrefix,
			DstPrefix: r.DstPrefix,
			Protocol:  r.Protocol,
			SrcPort:   r.SrcPort,
			DstPort:   r.DstPort,
		}); err != nil {
			return fmt.Errorf("scenario: acl_rule %s/%d: %w", r.GroupID, r.Num, err)
		}
	}
	for _, r := range doc.Spec.Routes {
		if _, err := store.AddRoute(&dashsimv1.Route{
			Id:        r.ID,
			Table:     r.Table,
			DstPrefix: r.DstPrefix,
			Action:    parseRouteAction(r.Action),
			NextHopIp: r.NextHopIP,
			VnetId:    r.VnetID,
		}); err != nil {
			return fmt.Errorf("scenario: route %s/%s: %w", r.Table, r.DstPrefix, err)
		}
	}
	for _, m := range doc.Spec.VnetMappings {
		if _, err := store.AddVnetMapping(&dashsimv1.VnetMapping{
			Id:         m.ID,
			VnetId:     m.VnetID,
			OverlayIp:  m.OverlayIP,
			UnderlayIp: m.UnderlayIP,
			Mac:        m.MAC,
			Vni:        m.VNI,
		}); err != nil {
			return fmt.Errorf("scenario: vnet_mapping %s/%s: %w", m.VnetID, m.OverlayIP, err)
		}
	}
	return nil
}

func parseAclStage(s string) dashsimv1.AclStage {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "INBOUND":
		return dashsimv1.AclStage_ACL_STAGE_INBOUND
	case "OUTBOUND":
		return dashsimv1.AclStage_ACL_STAGE_OUTBOUND
	}
	return dashsimv1.AclStage_ACL_STAGE_UNSPECIFIED
}

func parseAclAction(s string) dashsimv1.AclAction {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ALLOW", "PERMIT":
		return dashsimv1.AclAction_ACL_ACTION_ALLOW
	case "DENY", "DROP":
		return dashsimv1.AclAction_ACL_ACTION_DENY
	}
	return dashsimv1.AclAction_ACL_ACTION_UNSPECIFIED
}

func parseRouteAction(s string) dashsimv1.RouteAction {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FORWARD":
		return dashsimv1.RouteAction_ROUTE_ACTION_FORWARD
	case "DROP":
		return dashsimv1.RouteAction_ROUTE_ACTION_DROP
	case "ENCAP":
		return dashsimv1.RouteAction_ROUTE_ACTION_ENCAP
	}
	return dashsimv1.RouteAction_ROUTE_ACTION_UNSPECIFIED
}
