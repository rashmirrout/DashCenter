package manifest

import (
	"fmt"
)

// ExpandBundles iterates the envelope list and expands any EniBundle
// envelopes into individual spec envelopes in dependency-tier order
// (Tier 0 → Tier 1 → policies). Non-bundle envelopes pass through
// unchanged.
func ExpandBundles(envs []*Envelope) []*Envelope {
	var out []*Envelope
	for _, env := range envs {
		switch env.Kind {
		case "EniBundle":
			out = append(out, expandEniBundle(env)...)
		case "AclBundle":
			out = append(out, expandAclBundle(env)...)
		case "RouteBundle":
			out = append(out, expandRouteBundle(env)...)
		case "HaBundle":
			out = append(out, expandHaBundle(env)...)
		default:
			out = append(out, env)
		}
	}
	return out
}

// expandEniBundle takes a single EniBundle envelope and returns the
// individual spec envelopes in the correct creation order.
//
// Expected EniBundle spec shape:
//
//	spec:
//	  vnet:           { name: bank-prod-web, vni: 1001 }
//	  service_tunnel: { name: st-egress, ... }           # optional
//	  eni:            { mac_address: ..., underlay_ip: ..., admin_state: up, placement_hint_dpu_ids: [...] }
//	  vnet_mappings:  [ { ip_address: ..., underlay_ip: ..., ... }, ... ]
//	  route_policy:   { name: rp-bank, routes: [...] }   # optional
//	  acl_policies:   [ { name: acl-in, stage: inbound, rules: [...] }, ... ]  # optional
func expandEniBundle(bundle *Envelope) []*Envelope {
	ns := bundle.Metadata.Namespace
	eniName := bundle.Metadata.Name
	labels := bundle.Metadata.Labels
	var out []*Envelope

	// ── Tier 0: VNet ──────────────────────────────────────────────
	if vnetSpec, ok := bundle.Spec["vnet"].(map[string]any); ok {
		vnetName := strField(vnetSpec, "name")
		if vnetName == "" {
			vnetName = fmt.Sprintf("%s-vnet", eniName)
		}
		env := &Envelope{
			APIVersion: APIVersion,
			Kind:       "Vnet",
			Metadata:   Metadata{Namespace: ns, Name: vnetName, Labels: labels},
			Spec:       copyMap(vnetSpec),
		}
		out = append(out, env)
	}

	// ── Tier 0: ServiceTunnel (optional) ──────────────────────────
	if stSpec, ok := bundle.Spec["service_tunnel"].(map[string]any); ok {
		stName := strField(stSpec, "name")
		if stName == "" {
			stName = fmt.Sprintf("%s-tunnel", eniName)
		}
		env := &Envelope{
			APIVersion: APIVersion,
			Kind:       "ServiceTunnel",
			Metadata:   Metadata{Namespace: ns, Name: stName, Labels: labels},
			Spec:       copyMap(stSpec),
		}
		out = append(out, env)
	}

	// ── Tier 1: ENI ───────────────────────────────────────────────
	eniSpec := map[string]any{}
	if es, ok := bundle.Spec["eni"].(map[string]any); ok {
		eniSpec = copyMap(es)
	}
	// Auto-wire vnet_name from the vnet section if not explicitly set.
	if _, hasVnet := eniSpec["vnet_name"]; !hasVnet {
		if vnetSpec, ok := bundle.Spec["vnet"].(map[string]any); ok {
			vn := strField(vnetSpec, "name")
			if vn == "" {
				vn = fmt.Sprintf("%s-vnet", eniName)
			}
			eniSpec["vnet_name"] = vn
		}
	}
	out = append(out, &Envelope{
		APIVersion: APIVersion,
		Kind:       "Eni",
		Metadata:   Metadata{Namespace: ns, Name: eniName, Labels: labels},
		Spec:       eniSpec,
	})

	// ── Tier 1: VNetMappings ──────────────────────────────────────
	if mappings, ok := bundle.Spec["vnet_mappings"].([]any); ok {
		vnetName := strField(eniSpec, "vnet_name")
		for _, raw := range mappings {
			mSpec, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			ms := copyMap(mSpec)
			// Auto-wire vnet_name if not set.
			if _, has := ms["vnet_name"]; !has && vnetName != "" {
				ms["vnet_name"] = vnetName
			}
			ipAddr := strField(ms, "ip_address")
			mappingName := fmt.Sprintf("%s-%s", vnetName, ipAddr)
			if ipAddr == "" {
				mappingName = fmt.Sprintf("%s-mapping", eniName)
			}
			out = append(out, &Envelope{
				APIVersion: APIVersion,
				Kind:       "VnetMapping",
				Metadata:   Metadata{Namespace: ns, Name: mappingName, Labels: labels},
				Spec:       ms,
			})
		}
	}

	// ── Policies: RoutePolicy ─────────────────────────────────────
	if rpSpec, ok := bundle.Spec["route_policy"].(map[string]any); ok {
		rpName := strField(rpSpec, "name")
		if rpName == "" {
			rpName = fmt.Sprintf("rp-%s", eniName)
		}
		rp := copyMap(rpSpec)
		// Auto-wire eni_names if not set.
		if _, has := rp["eni_names"]; !has {
			rp["eni_names"] = []any{eniName}
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion,
			Kind:       "RoutePolicy",
			Metadata:   Metadata{Namespace: ns, Name: rpName, Labels: labels},
			Spec:       rp,
		})
	}

	// ── Policies: AclPolicies ─────────────────────────────────────
	if acls, ok := bundle.Spec["acl_policies"].([]any); ok {
		for _, raw := range acls {
			aclSpec, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			as := copyMap(aclSpec)
			aclName := strField(as, "name")
			if aclName == "" {
				aclName = fmt.Sprintf("acl-%s", eniName)
			}
			// Auto-wire eni_names if not set.
			if _, has := as["eni_names"]; !has {
				as["eni_names"] = []any{eniName}
			}
			out = append(out, &Envelope{
				APIVersion: APIVersion,
				Kind:       "AclPolicy",
				Metadata:   Metadata{Namespace: ns, Name: aclName, Labels: labels},
				Spec:       as,
			})
		}
	}

	return out
}

// strField reads a string field from a map, returning "" if absent or non-string.
func strField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// copyMap makes a shallow copy of a string-keyed map.
func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func init() {
	// Register bundle kinds so Parse/Validate accepts them.
	registry["EniBundle"] = KindInfo{
		Kind:      "EniBundle",
		StoreKind: "eni_bundle",
		URLPlural: "eni-bundles",
		Aliases:   []string{"enibundle", "eni-bundle", "bundle"},
		Phase:     1,
	}
	registry["AclBundle"] = KindInfo{
		Kind:      "AclBundle",
		StoreKind: "acl_bundle",
		URLPlural: "acl-bundles",
		Aliases:   []string{"aclbundle", "acl-bundle"},
		Phase:     1,
	}
	registry["RouteBundle"] = KindInfo{
		Kind:      "RouteBundle",
		StoreKind: "route_bundle",
		URLPlural: "route-bundles",
		Aliases:   []string{"routebundle", "route-bundle"},
		Phase:     1,
	}
	registry["HaBundle"] = KindInfo{
		Kind:      "HaBundle",
		StoreKind: "ha_bundle",
		URLPlural: "ha-bundles",
		Aliases:   []string{"habundle", "ha-bundle"},
		Phase:     1,
	}
}

// ── AclBundle ─────────────────────────────────────────────────────────
//
// At the dashd level, an AclPolicy already contains its rules inline.
// AclBundle adds the ability to also create referenced VNet + ENI
// dependencies and the AclPolicy binding. Expands into:
//   Tier 0: Vnet (if provided)
//   Tier 1: Eni (if provided, auto-wires vnet_name)
//   Policy: AclPolicy (with inline rules, stage, eni_names)
//
//	spec:
//	  vnet:       { name: v1, vni: 1001 }               # optional
//	  eni:        { name: eni-1, mac_address: ..., ... } # optional
//	  acl_policy: { stage: inbound, eni_names: [eni-1],
//	                rules: [{ priority: 100, action: allow }] }

func expandAclBundle(bundle *Envelope) []*Envelope {
	ns := bundle.Metadata.Namespace
	bundleName := bundle.Metadata.Name
	labels := bundle.Metadata.Labels
	var out []*Envelope

	// Tier 0: vnet (optional dependency)
	if vnetSpec, ok := bundle.Spec["vnet"].(map[string]any); ok {
		vnetName := strField(vnetSpec, "name")
		if vnetName == "" {
			vnetName = fmt.Sprintf("%s-vnet", bundleName)
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "Vnet",
			Metadata: Metadata{Namespace: ns, Name: vnetName, Labels: labels},
			Spec:     copyMap(vnetSpec),
		})
	}

	// Tier 1: eni (optional dependency)
	if eniSpec, ok := bundle.Spec["eni"].(map[string]any); ok {
		eniName := strField(eniSpec, "name")
		if eniName == "" {
			eniName = fmt.Sprintf("%s-eni", bundleName)
		}
		es := copyMap(eniSpec)
		if _, has := es["vnet_name"]; !has {
			if vs, ok := bundle.Spec["vnet"].(map[string]any); ok {
				vn := strField(vs, "name")
				if vn == "" {
					vn = fmt.Sprintf("%s-vnet", bundleName)
				}
				es["vnet_name"] = vn
			}
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "Eni",
			Metadata: Metadata{Namespace: ns, Name: eniName, Labels: labels},
			Spec:     es,
		})
	}

	// AclPolicy with inline rules
	if apSpec, ok := bundle.Spec["acl_policy"].(map[string]any); ok {
		ap := copyMap(apSpec)
		apName := strField(ap, "name")
		if apName == "" {
			apName = bundleName
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "AclPolicy",
			Metadata: Metadata{Namespace: ns, Name: apName, Labels: labels},
			Spec:     ap,
		})
	}

	return out
}

// ── RouteBundle ───────────────────────────────────────────────────────
//
// At the dashd level, a RoutePolicy already contains its routes inline.
// RouteBundle adds the ability to create referenced dependencies
// (VNet, ServiceTunnel, ENI) and the RoutePolicy binding. Expands into:
//   Tier 0: Vnet, ServiceTunnel (if provided)
//   Tier 1: Eni (if provided, auto-wires vnet_name)
//   Policy: RoutePolicy (with inline routes, eni_names)
//
//	spec:
//	  vnet:           { name: v1, vni: 1001 }                  # optional
//	  service_tunnel: { name: st-1, ... }                      # optional
//	  eni:            { name: eni-1, ... }                     # optional
//	  route_policy:   { eni_names: [eni-1],
//	                    routes: [{ prefix: ..., next_hop_type: vnet, next_hop_target: v1 }] }

func expandRouteBundle(bundle *Envelope) []*Envelope {
	ns := bundle.Metadata.Namespace
	bundleName := bundle.Metadata.Name
	labels := bundle.Metadata.Labels
	var out []*Envelope

	// Tier 0: vnet (optional)
	if vnetSpec, ok := bundle.Spec["vnet"].(map[string]any); ok {
		vnetName := strField(vnetSpec, "name")
		if vnetName == "" {
			vnetName = fmt.Sprintf("%s-vnet", bundleName)
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "Vnet",
			Metadata: Metadata{Namespace: ns, Name: vnetName, Labels: labels},
			Spec:     copyMap(vnetSpec),
		})
	}

	// Tier 0: service_tunnel (optional)
	if stSpec, ok := bundle.Spec["service_tunnel"].(map[string]any); ok {
		stName := strField(stSpec, "name")
		if stName == "" {
			stName = fmt.Sprintf("%s-tunnel", bundleName)
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "ServiceTunnel",
			Metadata: Metadata{Namespace: ns, Name: stName, Labels: labels},
			Spec:     copyMap(stSpec),
		})
	}

	// Tier 1: eni (optional)
	if eniSpec, ok := bundle.Spec["eni"].(map[string]any); ok {
		eniName := strField(eniSpec, "name")
		if eniName == "" {
			eniName = fmt.Sprintf("%s-eni", bundleName)
		}
		es := copyMap(eniSpec)
		if _, has := es["vnet_name"]; !has {
			if vs, ok := bundle.Spec["vnet"].(map[string]any); ok {
				vn := strField(vs, "name")
				if vn != "" {
					es["vnet_name"] = vn
				}
			}
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "Eni",
			Metadata: Metadata{Namespace: ns, Name: eniName, Labels: labels},
			Spec:     es,
		})
	}

	// RoutePolicy with inline routes
	if rpSpec, ok := bundle.Spec["route_policy"].(map[string]any); ok {
		rp := copyMap(rpSpec)
		rpName := strField(rp, "name")
		if rpName == "" {
			rpName = bundleName
		}
		out = append(out, &Envelope{
			APIVersion: APIVersion, Kind: "RoutePolicy",
			Metadata: Metadata{Namespace: ns, Name: rpName, Labels: labels},
			Spec:     rp,
		})
	}

	return out
}

// ── HaBundle ──────────────────────────────────────────────────────────
//
// At the dashd level, only HaSet exists. The ha_scope/ha_scope_config
// kinds are sim-layer only (managed internally by the HA orchestrator).
// HaBundle simply creates the HaSet.
//
//	spec:
//	  ha_set: { mode: active_standby, member_dpu_ids: [...], virtual_ip: ... }

func expandHaBundle(bundle *Envelope) []*Envelope {
	ns := bundle.Metadata.Namespace
	setName := bundle.Metadata.Name
	labels := bundle.Metadata.Labels

	setSpec := map[string]any{}
	if ss, ok := bundle.Spec["ha_set"].(map[string]any); ok {
		setSpec = copyMap(ss)
	}
	return []*Envelope{{
		APIVersion: APIVersion, Kind: "HaSet",
		Metadata: Metadata{Namespace: ns, Name: setName, Labels: labels},
		Spec:     setSpec,
	}}
}
