/* ═══════════════════════════════════════════════════════════════
 * resource-deps.ts — Resource Dependency Resolution & Validation
 *
 * Defines the resource dependency DAG and provides utilities for:
 *   • Forward dependency lookup: "what does X depend on?"
 *   • Reverse dependency lookup: "what depends on X?"
 *   • Creation-order ordering (topological)
 *   • Pre-submit dependency validation (does each referenced
 *     resource actually exist?)
 *   • Pre-delete safety checks (are there any dependents that
 *     would be orphaned?)
 *
 * The DAG (verified against deploy/test-setup/05-full-console/
 * manifest/bootstrap.py creation order):
 *
 *   vnets (root)            ─┐
 *   service-tunnels (root)  ─┤
 *                            ├──► vnet-mappings (vnet, optional tunnel)
 *                            ├──► enis (vnet)
 *                            │    ├──► acl-policies (eni_names[])
 *                            │    └──► route-policies (eni_names[],
 *                            │         routes[].next_hop_target)
 *                            └──► ha-sets (no resource deps; only
 *                                          inventory/DPU deps)
 *
 * NOTE: dashd does NOT enforce referential integrity on the wire
 * (you can PUT an ENI before its parent Vnet exists). This module
 * adds client-side safety so the UI catches issues earlier and
 * the user understands the dependency model.
 * ═══════════════════════════════════════════════════════════════ */

import type { ResourceKind } from "./constants";
import type {
  AclPolicySpec,
  EniSpec,
  HaSetSpec,
  RoutePolicySpec,
  ServiceTunnelSpec,
  VnetMappingSpec,
  VnetSpec,
} from "@/api/types";

/* ── Dependency DAG static definition ──────────────────────── */

/** Forward dependency map: which kinds does each kind reference? */
export const DEPENDENCY_GRAPH: Record<
  ResourceKind,
  ReadonlyArray<ResourceKind>
> = {
  vnets: [],
  "service-tunnels": [],
  enis: ["vnets"],
  "vnet-mappings": ["vnets", "service-tunnels"],
  "acl-policies": ["enis"],
  "route-policies": ["enis", "vnets", "service-tunnels"],
  // ha-sets reference DPUs (inventory), not other CRUD resources.
  // We model "inventory" implicitly — it's read-only and always
  // exists, so we don't include it in the DAG.
  "ha-sets": [],
};

/** Topologically sorted creation order. Use this to determine the
 *  ideal order to display tabs OR to suggest resource creation
 *  order to users. Sources (no deps) come first; sinks last. */
export const RESOURCE_CREATION_ORDER: ResourceKind[] = [
  "vnets",
  "service-tunnels",
  "enis",
  "vnet-mappings",
  "acl-policies",
  "route-policies",
  "ha-sets",
];

/** Human-friendly labels for each kind (used in UI messages). */
export const KIND_LABELS: Record<ResourceKind, string> = {
  vnets: "Vnet",
  "service-tunnels": "Service Tunnel",
  enis: "ENI",
  "vnet-mappings": "Vnet Mapping",
  "acl-policies": "ACL Policy",
  "route-policies": "Route Policy",
  "ha-sets": "HA Set",
};

/** Plural labels (for tab titles, counts). */
export const KIND_LABELS_PLURAL: Record<ResourceKind, string> = {
  vnets: "Vnets",
  "service-tunnels": "Service Tunnels",
  enis: "ENIs",
  "vnet-mappings": "Vnet Mappings",
  "acl-policies": "ACL Policies",
  "route-policies": "Route Policies",
  "ha-sets": "HA Sets",
};

/* ── Universal resource alias ──────────────────────────────── */

export type AnyResource =
  | VnetSpec
  | EniSpec
  | VnetMappingSpec
  | ServiceTunnelSpec
  | AclPolicySpec
  | RoutePolicySpec
  | HaSetSpec;

/** Snapshot of all live lists, keyed by kind. Pass this into the
 *  resolution/validation functions so they can look up names. */
export interface ResourceSnapshot {
  vnets: VnetSpec[];
  enis: EniSpec[];
  "vnet-mappings": VnetMappingSpec[];
  "service-tunnels": ServiceTunnelSpec[];
  "acl-policies": AclPolicySpec[];
  "route-policies": RoutePolicySpec[];
  "ha-sets": HaSetSpec[];
}

/** Build an empty snapshot — useful as a defensive default. */
export function emptySnapshot(): ResourceSnapshot {
  return {
    vnets: [],
    enis: [],
    "vnet-mappings": [],
    "service-tunnels": [],
    "acl-policies": [],
    "route-policies": [],
    "ha-sets": [],
  };
}

/* ── Helpers ───────────────────────────────────────────────── */

function resourceName(r: { metadata?: { name?: string } } | undefined): string {
  return r?.metadata?.name ?? "";
}

function asArray<T>(v: T[] | undefined | null): T[] {
  return Array.isArray(v) ? v : [];
}

/* ── Forward dependency extraction ─────────────────────────── */

/** A single reference from a resource to another resource. */
export interface DependencyRef {
  /** The kind being referenced. */
  kind: ResourceKind;
  /** The referenced resource name. */
  name: string;
  /** The field path on the source resource (for error reporting). */
  field: string;
}

/**
 * Extract all upstream references from a given resource.
 *
 * Returns a flat list of `{kind, name, field}` triples. Empty/missing
 * references are skipped. The caller can then check each against a
 * `ResourceSnapshot` to validate that the referenced resources exist.
 */
export function getDependencies(
  kind: ResourceKind,
  resource: AnyResource,
): DependencyRef[] {
  const refs: DependencyRef[] = [];

  switch (kind) {
    case "vnets":
    case "service-tunnels":
    case "ha-sets":
      // No outbound resource refs (ha-sets only ref DPUs, which we
      // don't validate here since inventory is read-only).
      break;

    case "enis": {
      const eni = resource as EniSpec;
      if (eni.vnet_name) {
        refs.push({ kind: "vnets", name: eni.vnet_name, field: "vnet_name" });
      }
      break;
    }

    case "vnet-mappings": {
      const m = resource as VnetMappingSpec;
      if (m.vnet_name) {
        refs.push({ kind: "vnets", name: m.vnet_name, field: "vnet_name" });
      }
      if (m.action === "service_tunnel" && m.params?.tunnel) {
        refs.push({
          kind: "service-tunnels",
          name: m.params.tunnel,
          field: "params.tunnel",
        });
      }
      break;
    }

    case "acl-policies": {
      const acl = resource as AclPolicySpec;
      for (const eniName of asArray(acl.eni_names)) {
        if (eniName) {
          refs.push({ kind: "enis", name: eniName, field: "eni_names" });
        }
      }
      break;
    }

    case "route-policies": {
      const rp = resource as RoutePolicySpec;
      for (const eniName of asArray(rp.eni_names)) {
        if (eniName) {
          refs.push({ kind: "enis", name: eniName, field: "eni_names" });
        }
      }
      // routes[].next_hop_target — split by next_hop_type
      const routes = asArray(rp.routes ?? rp.rules);
      for (let i = 0; i < routes.length; i++) {
        const r = routes[i]!;
        // single-hop
        if (r.next_hop_target && r.next_hop_type) {
          if (r.next_hop_type === "vnet") {
            refs.push({
              kind: "vnets",
              name: r.next_hop_target,
              field: `routes[${i}].next_hop_target`,
            });
          } else if (r.next_hop_type === "service_tunnel") {
            refs.push({
              kind: "service-tunnels",
              name: r.next_hop_target,
              field: `routes[${i}].next_hop_target`,
            });
          }
          // "drop" has no target — skip
        }
        // ecmp_members
        for (let j = 0; j < asArray(r.ecmp_members).length; j++) {
          const m = r.ecmp_members![j]!;
          if (m.next_hop_target && m.next_hop_type) {
            if (m.next_hop_type === "vnet") {
              refs.push({
                kind: "vnets",
                name: m.next_hop_target,
                field: `routes[${i}].ecmp_members[${j}].next_hop_target`,
              });
            } else if (m.next_hop_type === "service_tunnel") {
              refs.push({
                kind: "service-tunnels",
                name: m.next_hop_target,
                field: `routes[${i}].ecmp_members[${j}].next_hop_target`,
              });
            }
          }
        }
      }
      break;
    }
  }

  return refs;
}

/* ── Reverse dependency lookup ─────────────────────────────── */

/** A single dependent: some resource that references the target. */
export interface DependentRef {
  /** The kind of the dependent. */
  kind: ResourceKind;
  /** The dependent resource's name. */
  name: string;
  /** The field path on the dependent that holds the reference. */
  field: string;
}

/**
 * Find all resources that reference the given target.
 *
 * E.g. `getDependents("vnets", "bank-prod-web", snapshot)` returns
 * every ENI, Vnet Mapping, and Route Policy that names `bank-prod-web`.
 *
 * This is the engine behind delete-safety warnings.
 */
export function getDependents(
  kind: ResourceKind,
  name: string,
  snapshot: ResourceSnapshot,
): DependentRef[] {
  const out: DependentRef[] = [];

  // Walk every resource of every kind, extract its dependencies,
  // and keep those that match (kind, name).
  for (const dependentKind of RESOURCE_CREATION_ORDER) {
    const items = snapshot[dependentKind] as AnyResource[];
    for (const item of items) {
      const deps = getDependencies(dependentKind, item);
      for (const d of deps) {
        if (d.kind === kind && d.name === name) {
          out.push({
            kind: dependentKind,
            name: resourceName(item),
            field: d.field,
          });
        }
      }
    }
  }

  return out;
}

/* ── Validation: do all referenced resources exist? ──────── */

/** A single validation problem found in a resource's references. */
export interface DependencyValidationIssue {
  /** Severity: "error" blocks submit; "warning" allows but flags. */
  severity: "error" | "warning";
  /** The reference that failed. */
  ref: DependencyRef;
  /** Human-readable explanation. */
  message: string;
}

/**
 * Validate that every upstream dependency of `resource` exists in
 * `snapshot`. Returns an empty array when the resource is fully valid.
 *
 * Currently every missing reference is an "error" (matches the
 * stricter UX recommendation). Future modes could treat unknown
 * tunnels as warnings if e.g. they're known to be created externally.
 */
export function validateDependencies(
  kind: ResourceKind,
  resource: AnyResource,
  snapshot: ResourceSnapshot,
): DependencyValidationIssue[] {
  const issues: DependencyValidationIssue[] = [];
  const deps = getDependencies(kind, resource);

  for (const dep of deps) {
    const list = snapshot[dep.kind] as AnyResource[];
    const exists = list.some((r) => resourceName(r) === dep.name);
    if (!exists) {
      issues.push({
        severity: "error",
        ref: dep,
        message: `${KIND_LABELS[dep.kind]} "${dep.name}" does not exist (referenced by ${dep.field})`,
      });
    }
  }

  return issues;
}

/* ── Per-resource dependency rollup ────────────────────────── */

/** Per-kind count of dependents. */
export type DependentCounts = Partial<Record<ResourceKind, number>>;

/** Compact per-resource dependency rollup, suitable for tables. */
export interface ResourceDepInfo {
  /** Total count of dependents across all kinds. */
  totalDependents: number;
  /** Per-kind dependent counts. */
  byKind: DependentCounts;
  /** Optional full list (omitted by default to keep payload small). */
  dependents?: DependentRef[];
}

/** Build a dependency-info rollup for a single resource. */
export function rollupDependents(
  kind: ResourceKind,
  name: string,
  snapshot: ResourceSnapshot,
  includeList = false,
): ResourceDepInfo {
  const deps = getDependents(kind, name, snapshot);
  const byKind: DependentCounts = {};
  for (const d of deps) {
    byKind[d.kind] = (byKind[d.kind] ?? 0) + 1;
  }
  return {
    totalDependents: deps.length,
    byKind,
    dependents: includeList ? deps : undefined,
  };
}

/* ── Bulk rollup: dependency-info for an entire kind's list ── */

/**
 * Compute dependency rollups for every resource of a given kind.
 * Returned as a `Map<name, ResourceDepInfo>` for O(1) lookup from
 * a DataTable cell renderer.
 */
export function rollupAllDependents(
  kind: ResourceKind,
  snapshot: ResourceSnapshot,
): Map<string, ResourceDepInfo> {
  const out = new Map<string, ResourceDepInfo>();
  const items = snapshot[kind] as AnyResource[];
  for (const item of items) {
    const name = resourceName(item);
    if (!name) continue;
    out.set(name, rollupDependents(kind, name, snapshot));
  }
  return out;
}

/* ── Reachability for visual graph ─────────────────────────── */

/** Get the immediate "depends on" kinds for a given kind (DAG edges). */
export function upstreamKinds(kind: ResourceKind): ResourceKind[] {
  return [...DEPENDENCY_GRAPH[kind]];
}

/** Get all kinds that reference (directly) the given kind. */
export function downstreamKinds(kind: ResourceKind): ResourceKind[] {
  const out: ResourceKind[] = [];
  for (const k of RESOURCE_CREATION_ORDER) {
    if (DEPENDENCY_GRAPH[k].includes(kind)) {
      out.push(k);
    }
  }
  return out;
}