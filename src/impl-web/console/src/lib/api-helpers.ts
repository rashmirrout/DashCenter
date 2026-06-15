/**
 * Helper utilities for normalizing dashd/BFF API responses.
 *
 * The actual API responses sometimes use different field names than the
 * TypeScript types expect (e.g., `id` vs `dpu_id`, `enis_used` vs `eni_count`).
 * These helpers provide safe access with multiple field-name fallbacks.
 */

import type {
  CapacityStats,
  DashdHealthResponse,
  DpuCapacityEntry,
  EniPlacement,
  FleetSummary,
} from "@/api/types";

/* ── DPU id helpers ──────────────────────────────────────── */

export function dpuEntryId(entry: { id?: string; dpu_id?: string }): string {
  return entry.dpu_id ?? entry.id ?? "";
}

/* ── Capacity helpers ────────────────────────────────────── */

export function entryEniUsed(e: DpuCapacityEntry): number {
  return e.eni_count ?? e.enis_used ?? 0;
}
export function entryEniMax(e: DpuCapacityEntry): number {
  return e.eni_max ?? e.enis_max ?? 0;
}
export function entryRouteUsed(e: DpuCapacityEntry): number {
  return e.route_count ?? e.routes_used ?? 0;
}
export function entryRouteMax(e: DpuCapacityEntry): number {
  return e.route_max ?? e.routes_max ?? 0;
}
export function entryAclUsed(e: DpuCapacityEntry): number {
  return e.acl_rule_count ?? e.acl_rules_used ?? 0;
}
export function entryAclMax(e: DpuCapacityEntry): number {
  return e.acl_rule_max ?? e.acl_rules_max ?? 0;
}
export function entryFlowUsed(e: DpuCapacityEntry): number {
  return e.flow_count ?? e.flows_used ?? 0;
}
export function entryFlowMax(e: DpuCapacityEntry): number {
  return e.flow_max ?? e.flows_max ?? 0;
}

/**
 * Sum a list of `DpuCapacityEntry` rows for a given selector.
 * Used to compute fleet totals from per_dpu when fleet.max_* fields
 * are not returned by the API.
 */
export function sumCapacity(
  entries: DpuCapacityEntry[] | undefined,
  selector: (e: DpuCapacityEntry) => number
): number {
  if (!entries || entries.length === 0) return 0;
  let total = 0;
  for (const e of entries) total += selector(e);
  return total;
}

/**
 * Return per-DPU rows from a capacity stats payload, accepting
 * either `per_dpu` (current BFF) or `dpus` (legacy alias).
 */
export function capacityRows(cs: CapacityStats | undefined): DpuCapacityEntry[] {
  if (!cs) return [];
  return cs.per_dpu ?? cs.dpus ?? [];
}

/**
 * Compute fleet totals from a CapacityStats payload, prefering
 * the `fleet` object but falling back to summing per-DPU rows.
 */
export interface FleetCapacity {
  enisUsed: number;
  enisMax: number;
  routesUsed: number;
  routesMax: number;
  aclRulesUsed: number;
  aclRulesMax: number;
  flowsUsed: number;
  flowsMax: number;
}

export function fleetCapacity(cs: CapacityStats | undefined): FleetCapacity {
  const rows = capacityRows(cs);
  const fleet = cs?.fleet ?? cs?.fleet_totals ?? {};
  return {
    enisUsed: fleet.total_enis ?? sumCapacity(rows, entryEniUsed),
    enisMax: fleet.max_enis ?? sumCapacity(rows, entryEniMax),
    routesUsed: fleet.total_routes ?? sumCapacity(rows, entryRouteUsed),
    routesMax: fleet.max_routes ?? sumCapacity(rows, entryRouteMax),
    aclRulesUsed: fleet.total_acl_rules ?? sumCapacity(rows, entryAclUsed),
    aclRulesMax: fleet.max_acl_rules ?? sumCapacity(rows, entryAclMax),
    flowsUsed: fleet.total_flows ?? sumCapacity(rows, entryFlowUsed),
    flowsMax: fleet.max_flows ?? sumCapacity(rows, entryFlowMax),
  };
}

/* ── Health helpers ──────────────────────────────────────── */

/**
 * Return the connected-DPU count from a /admin/health payload.
 * Prefers the `connected_dpus` aggregate, falls back to dpus.length.
 */
export function connectedDpuCount(hd: DashdHealthResponse | undefined): number {
  if (!hd) return 0;
  if (typeof hd.connected_dpus === "number") return hd.connected_dpus;
  return Array.isArray(hd.dpus) ? hd.dpus.length : 0;
}

/* ── Fleet summary helpers ───────────────────────────────── */

export function fleetDpuCount(fs: FleetSummary | undefined): number {
  if (!fs) return 0;
  return fs.dpu_count ?? fs.total_dpus ?? fs.dpus?.length ?? 0;
}

export function fleetEniCount(fs: FleetSummary | undefined): number {
  if (!fs) return 0;
  return fs.eni_count ?? fs.total_enis ?? 0;
}

export function fleetVnetCount(fs: FleetSummary | undefined): number {
  if (!fs) return 0;
  return fs.vnet_count ?? fs.total_vnets ?? 0;
}

export function fleetDpuStates(
  fs: FleetSummary | undefined
): Record<string, number> {
  if (!fs) return {};
  return fs.dpus_by_state ?? fs.dpu_states ?? {};
}

export function fleetHealthyDpus(fs: FleetSummary | undefined): number {
  if (!fs) return 0;
  const states = fleetDpuStates(fs);
  return (
    states["DPU_STATE_UP"] ??
    states["READY"] ??
    states["CONNECTED"] ??
    fs.healthy_dpus ??
    0
  );
}

export function fleetDegradedDpus(fs: FleetSummary | undefined): number {
  if (!fs) return 0;
  const states = fleetDpuStates(fs);
  return (
    states["DPU_STATE_DEGRADED"] ??
    states["DRAINING"] ??
    states["CORDONED"] ??
    fs.degraded_dpus ??
    0
  );
}

export function fleetDisconnectedDpus(fs: FleetSummary | undefined): number {
  if (!fs) return 0;
  const states = fleetDpuStates(fs);
  return (
    states["DPU_STATE_DISCONNECTED"] ??
    states["DPU_STATE_DOWN"] ??
    states["DISCONNECTED"] ??
    fs.disconnected_dpus ??
    0
  );
}

/* ── ENI placement helpers ───────────────────────────────── */

/**
 * Return the set of DPU ids that host the given ENI placement.
 * Handles both the modern shape (`placements: [{dpu_id,…}]`) and the
 * legacy shape (`dpu_id: string`).
 */
export function placementDpuIds(p: EniPlacement | undefined): string[] {
  if (!p) return [];
  if (Array.isArray(p.placements) && p.placements.length > 0) {
    return p.placements.map((s) => s.dpu_id).filter((id): id is string => !!id);
  }
  return p.dpu_id ? [p.dpu_id] : [];
}

/**
 * Count how many ENIs each DPU hosts.
 * Returns a `dpu_id → count` map.
 */
export function eniPlacementCountsByDpu(
  placements: EniPlacement[] | undefined
): Record<string, number> {
  const out: Record<string, number> = {};
  if (!placements) return out;
  for (const p of placements) {
    for (const dpuId of placementDpuIds(p)) {
      out[dpuId] = (out[dpuId] ?? 0) + 1;
    }
  }
  return out;
}

/**
 * Best-effort ENI display name (the API uses `name`; legacy uses `eni_name`).
 */
export function placementEniName(p: EniPlacement): string {
  return p.name ?? p.eni_name ?? "";
}
