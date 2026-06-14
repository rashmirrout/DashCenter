/* ═══════════════════════════════════════════════════════════════
 * useResourceList — generic dispatcher hook
 *
 * Returns the live list of items for any of the 7 dashd resource
 * kinds (vnets, enis, acl-policies, route-policies, vnet-mappings,
 * service-tunnels, ha) plus the special "inventory" kind for DPU
 * selectors.
 *
 * Used by `<ResourceSelect>` and `<ResourceMultiSelect>` form
 * primitives so a single component can populate a dropdown from
 * any resource type without each consumer rewriting the per-kind
 * hook wiring.
 *
 * Added in A-IF1-G4.
 * ═══════════════════════════════════════════════════════════════ */

import {
  useAclPolicies,
  useEniList,
  useHaSets,
  useInventory,
  useRoutePolicies,
  useServiceTunnels,
  useVnetList,
  useVnetMappings,
} from "./hooks";
import type { ResourceKind } from "@/lib/constants";

/** All resource kinds the dispatcher recognises. Includes the
 *  read-only "inventory" alias used by DPU selectors. */
export type ResourceListKind = ResourceKind | "inventory";

/** Uniform shape returned by every dispatcher branch. The `items`
 *  field is type-erased so a single dropdown component can accept
 *  any resource kind. Individual consumers are expected to know
 *  the concrete shape and narrow with their own type guards. */
export interface ResourceListResult {
  /** Live items array (empty when loading, error, or genuinely empty). */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  items: any[];
  isLoading: boolean;
  isError: boolean;
  /** Error object when isError; undefined otherwise. */
  error?: Error;
  /** Imperative re-fetch (for "no items, retry" affordances). */
  refetch: () => void;
}

/**
 * Dispatch to the right per-kind hook and normalise its return
 * shape so callers don't have to switch on `kind`.
 *
 * Every backing hook already polls on the FLEET / INVENTORY
 * cadence — this dispatcher inherits that polling. No new
 * network traffic is introduced.
 *
 * The namespace argument is honoured for all per-namespace
 * resources; it is ignored by `inventory` (which is global).
 */
export function useResourceList(
  kind: ResourceListKind,
  ns: string = "default",
): ResourceListResult {
  // We deliberately call every hook unconditionally to satisfy
  // React's rules-of-hooks. Only the result from the requested
  // kind is returned; the others sit idle. TanStack Query
  // de-duplicates their network calls across the app so the cost
  // is negligible — most of these are already running because
  // the corresponding list view is open elsewhere.

  const vnets = useVnetList(ns);
  const enis = useEniList(ns);
  const acls = useAclPolicies(ns);
  const routes = useRoutePolicies(ns);
  const tunnels = useServiceTunnels(ns);
  const mappings = useVnetMappings(ns);
  const haSets = useHaSets(ns);
  const inventory = useInventory();

  switch (kind) {
    case "vnets":
      return shape(vnets);
    case "enis":
      return shape(enis);
    case "acl-policies":
      return shape(acls);
    case "route-policies":
      return shape(routes);
    case "service-tunnels":
      return shape(tunnels);
    case "vnet-mappings":
      return shape(mappings);
    case "ha":
      return shape(haSets);
    case "inventory":
      return shape(inventory);
    default: {
      // Should be unreachable; the exhaustiveness check is here
      // to make compile fail loudly if `ResourceListKind` grows.
      const _exhaustive: never = kind;
      return {
        items: [],
        isLoading: false,
        isError: true,
        error: new Error(`Unknown resource kind: ${String(_exhaustive)}`),
        refetch: () => undefined,
      };
    }
  }
}

/* ── Helpers ───────────────────────────────────────────────── */

interface QueryLike {
  data?: { items?: unknown[] } | undefined;
  isLoading: boolean;
  isError: boolean;
  error?: unknown;
  refetch: () => unknown;
}

/** Coerce a TanStack Query result into the dispatcher's uniform
 *  `ResourceListResult` shape. Treats `data.items ?? []`
 *  defensively (some endpoints return `null` on empty). */
function shape(q: QueryLike): ResourceListResult {
  return {
    items: q.data?.items ?? [],
    isLoading: q.isLoading,
    isError: q.isError,
    error: q.error instanceof Error ? q.error : undefined,
    refetch: () => {
      void q.refetch();
    },
  };
}