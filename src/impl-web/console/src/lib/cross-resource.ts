/**
 * Cross-resource indexing.
 *
 * The dashd API exposes ACL policies, route policies, ENIs, tunnels, vnets
 * etc. as flat lists. Most UI panels need to ask "which policies apply to
 * this DPU?" or "which ENIs live on this DPU?" or "which tunnels does this
 * ENI use?". This module builds those indexes once and exposes typed lookups.
 */

import type {
  AclPolicySpec,
  EniPlacement,
  EniSpec,
  RoutePolicySpec,
  ServiceTunnelSpec,
  VnetMappingSpec,
} from "@/api/types";
import { placementDpuIds, placementEniName } from "./api-helpers";

/* ── Helpers ────────────────────────────────────────────── */

function resourceName(r: { metadata?: { name?: string }; name?: string } | undefined): string {
  if (!r) return "";
  return r.metadata?.name ?? (r as { name?: string }).name ?? "";
}

function addToMapSet<V>(m: Map<string, Set<V>>, key: string, value: V) {
  if (!key) return;
  let s = m.get(key);
  if (!s) {
    s = new Set();
    m.set(key, s);
  }
  s.add(value);
}

function asArray<T>(v: T[] | undefined | null): T[] {
  return Array.isArray(v) ? v : [];
}

/* ── Input bundle ───────────────────────────────────────── */

export interface CrossResourceInput {
  enis?: EniSpec[];
  placements?: EniPlacement[];
  acls?: AclPolicySpec[];
  routes?: RoutePolicySpec[];
  tunnels?: ServiceTunnelSpec[];
  vnetMappings?: VnetMappingSpec[];
}

/* ── Indexes ─────────────────────────────────────────────── */

export interface CrossResourceIndex {
  /** All ENIs hosted on a DPU. */
  enisByDpu: Map<string, EniSpec[]>;
  /** All vnet names covered by ENIs on a DPU. */
  vnetsByDpu: Map<string, Set<string>>;
  /** ACL policies bound to each ENI. */
  aclsByEni: Map<string, AclPolicySpec[]>;
  /** Route policies bound to each ENI. */
  routesByEni: Map<string, RoutePolicySpec[]>;
  /** Tunnels referenced (by next-hop) from each ENI. */
  tunnelsByEni: Map<string, ServiceTunnelSpec[]>;
  /** Mappings present in each Vnet. */
  mappingsByVnet: Map<string, VnetMappingSpec[]>;
  /** Tunnel lookup by name. */
  tunnelByName: Map<string, ServiceTunnelSpec>;
}

export function buildCrossResourceIndex(
  input: CrossResourceInput
): CrossResourceIndex {
  const enis = asArray(input.enis);
  const placements = asArray(input.placements);
  const acls = asArray(input.acls);
  const routes = asArray(input.routes);
  const tunnels = asArray(input.tunnels);
  const vnetMappings = asArray(input.vnetMappings);

  /* ── ENIs by DPU ─────────────────────────────────────── */
  // Build a name → EniSpec map from the REST list, and use placements
  // (which authoritatively tell us which DPU each ENI is on) to bucket.
  const eniByName = new Map<string, EniSpec>();
  for (const e of enis) {
    const name = resourceName(e);
    if (name) eniByName.set(name, e);
  }

  const enisByDpu = new Map<string, EniSpec[]>();
  for (const p of placements) {
    const name = placementEniName(p);
    if (!name) continue;
    const eni = eniByName.get(name);
    for (const dpu of placementDpuIds(p)) {
      const arr = enisByDpu.get(dpu) ?? [];
      arr.push(eni ?? ({ metadata: { namespace: "default", name }, vnet_name: p.vnet_name ?? "", mac_address: p.mac_address ?? "", underlay_ip: p.underlay_ip ?? "" } as EniSpec));
      enisByDpu.set(dpu, arr);
    }
  }
  // If we have no placement data, fall back to placement_hint_dpu_ids on the spec.
  if (placements.length === 0 && enis.length > 0) {
    for (const e of enis) {
      const dpus = asArray(e.placement_hint_dpu_ids);
      for (const dpu of dpus) {
        const arr = enisByDpu.get(dpu) ?? [];
        arr.push(e);
        enisByDpu.set(dpu, arr);
      }
    }
  }

  /* ── Vnets by DPU ────────────────────────────────────── */
  const vnetsByDpu = new Map<string, Set<string>>();
  for (const [dpu, list] of enisByDpu) {
    for (const e of list) {
      if (e.vnet_name) addToMapSet(vnetsByDpu, dpu, e.vnet_name);
    }
  }

  /* ── ACLs by ENI ─────────────────────────────────────── */
  const aclsByEni = new Map<string, AclPolicySpec[]>();
  for (const acl of acls) {
    for (const eni of asArray(acl.eni_names)) {
      const arr = aclsByEni.get(eni) ?? [];
      arr.push(acl);
      aclsByEni.set(eni, arr);
    }
  }

  /* ── Routes by ENI + tunnel discovery ────────────────── */
  const routesByEni = new Map<string, RoutePolicySpec[]>();
  const tunnelByName = new Map<string, ServiceTunnelSpec>();
  for (const t of tunnels) {
    const name = resourceName(t);
    if (name) tunnelByName.set(name, t);
  }

  const tunnelsByEni = new Map<string, ServiceTunnelSpec[]>();

  for (const rp of routes) {
    const routeEntries = asArray(rp.routes ?? rp.rules);
    for (const eni of asArray(rp.eni_names)) {
      const arr = routesByEni.get(eni) ?? [];
      arr.push(rp);
      routesByEni.set(eni, arr);

      // Walk route next-hops to discover referenced tunnels.
      for (const entry of routeEntries) {
        const targets: { type?: string; target?: string }[] = [];
        if (entry.next_hop_type) {
          targets.push({ type: entry.next_hop_type, target: entry.next_hop_target });
        }
        for (const m of asArray(entry.ecmp_members)) {
          targets.push({ type: m.next_hop_type, target: m.next_hop_target });
        }
        for (const t of targets) {
          if (t.type === "service_tunnel" && t.target) {
            const tn = tunnelByName.get(t.target);
            if (tn) {
              const ta = tunnelsByEni.get(eni) ?? [];
              if (!ta.includes(tn)) ta.push(tn);
              tunnelsByEni.set(eni, ta);
            }
          }
        }
      }
    }
  }

  // Also walk vnet-mappings with action=service_tunnel — they reference tunnels.
  // The mapping's vnet → its ENIs (via eniByName.vnet_name) → those ENIs use this tunnel.
  const enisByVnet = new Map<string, EniSpec[]>();
  for (const e of enis) {
    if (e.vnet_name) {
      const arr = enisByVnet.get(e.vnet_name) ?? [];
      arr.push(e);
      enisByVnet.set(e.vnet_name, arr);
    }
  }

  /* ── Mappings by Vnet ────────────────────────────────── */
  const mappingsByVnet = new Map<string, VnetMappingSpec[]>();
  for (const m of vnetMappings) {
    const v = m.vnet_name;
    if (!v) continue;
    const arr = mappingsByVnet.get(v) ?? [];
    arr.push(m);
    mappingsByVnet.set(v, arr);

    if (m.action === "service_tunnel") {
      const tn = m.params?.tunnel;
      if (tn) {
        const tunnel = tunnelByName.get(tn);
        if (tunnel) {
          for (const eni of asArray(enisByVnet.get(v))) {
            const name = resourceName(eni);
            const ta = tunnelsByEni.get(name) ?? [];
            if (!ta.includes(tunnel)) ta.push(tunnel);
            tunnelsByEni.set(name, ta);
          }
        }
      }
    }
  }

  return {
    enisByDpu,
    vnetsByDpu,
    aclsByEni,
    routesByEni,
    tunnelsByEni,
    mappingsByVnet,
    tunnelByName,
  };
}

/* ── Per-DPU aggregates ─────────────────────────────────── */

export interface DpuCrossCounts {
  eniCount: number;
  vnetCount: number;
  aclCount: number;
  routeCount: number;
  tunnelCount: number;
  enis: EniSpec[];
  vnets: string[];
  acls: AclPolicySpec[];
  routes: RoutePolicySpec[];
  tunnels: ServiceTunnelSpec[];
}

export function dpuCrossCounts(
  idx: CrossResourceIndex,
  dpuId: string
): DpuCrossCounts {
  const enis = idx.enisByDpu.get(dpuId) ?? [];
  const vnetSet = idx.vnetsByDpu.get(dpuId) ?? new Set<string>();
  const aclSet = new Set<AclPolicySpec>();
  const routeSet = new Set<RoutePolicySpec>();
  const tunnelSet = new Set<ServiceTunnelSpec>();
  for (const e of enis) {
    const name = resourceName(e);
    for (const a of idx.aclsByEni.get(name) ?? []) aclSet.add(a);
    for (const r of idx.routesByEni.get(name) ?? []) routeSet.add(r);
    for (const t of idx.tunnelsByEni.get(name) ?? []) tunnelSet.add(t);
  }
  const vnets = Array.from(vnetSet).sort();
  const acls = Array.from(aclSet);
  const routes = Array.from(routeSet);
  const tunnels = Array.from(tunnelSet);
  return {
    eniCount: enis.length,
    vnetCount: vnets.length,
    aclCount: acls.length,
    routeCount: routes.length,
    tunnelCount: tunnels.length,
    enis,
    vnets,
    acls,
    routes,
    tunnels,
  };
}

/* ── Per-ENI aggregates ─────────────────────────────────── */

export interface EniCrossCounts {
  acls: AclPolicySpec[];
  routes: RoutePolicySpec[];
  tunnels: ServiceTunnelSpec[];
  aclCount: number;
  routeCount: number;
  tunnelCount: number;
}

export function eniCrossCounts(
  idx: CrossResourceIndex,
  eniName: string
): EniCrossCounts {
  const acls = idx.aclsByEni.get(eniName) ?? [];
  const routes = idx.routesByEni.get(eniName) ?? [];
  const tunnels = idx.tunnelsByEni.get(eniName) ?? [];
  return {
    acls,
    routes,
    tunnels,
    aclCount: acls.length,
    routeCount: routes.length,
    tunnelCount: tunnels.length,
  };
}

/* ── Vnet address-space inference ───────────────────────── */

/**
 * Vnets in the current API don't carry `address_space`. Infer it from
 * the underlay IPs used by their ENIs (an approximation).
 */
export function inferVnetUnderlayCidrs(
  enisInVnet: EniSpec[]
): string[] {
  const cidrs = new Set<string>();
  for (const e of enisInVnet) {
    const ip = e.underlay_ip;
    if (!ip) continue;
    // Reduce to /24 grouping (e.g. "10.0.1.11" → "10.0.1.0/24").
    const parts = ip.split(".");
    if (parts.length === 4) {
      cidrs.add(`${parts[0]}.${parts[1]}.${parts[2]}.0/24`);
    }
  }
  return Array.from(cidrs).sort();
}

/**
 * Vnet "overlay" address space from vnet-mappings (the actual overlay IPs
 * in use, reduced to /24 groupings).
 */
export function inferVnetOverlayCidrs(
  mappings: VnetMappingSpec[]
): string[] {
  const cidrs = new Set<string>();
  for (const m of mappings) {
    const ip = m.ip_address ?? m.overlay_ip;
    if (!ip) continue;
    const parts = ip.split(".");
    if (parts.length === 4) {
      cidrs.add(`${parts[0]}.${parts[1]}.${parts[2]}.0/24`);
    }
  }
  return Array.from(cidrs).sort();
}