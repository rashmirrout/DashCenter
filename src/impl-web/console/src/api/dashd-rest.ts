import { api } from './client';
import { API_BASE } from '@/lib/constants';
import type {
  ListResponse,
  VnetSpec,
  EniSpec,
  AclPolicySpec,
  RoutePolicySpec,
  VnetMappingSpec,
  ServiceTunnelSpec,
  HaSetSpec,
  DpuRecord,
  SimulateRequest,
  SimulateResult,
  ReconcileRequest,
  ReconcileResult,
} from './types';

const R = API_BASE.REST;

/* ═══════════════════════════════════════════════════════════════
 * Wire-format ↔ SPA-format conversion.
 *
 * dashd returns items as: { kind, name, namespace, generation, spec: {…} }
 * SPA views consume:      { metadata: { namespace, name, generation }, …spec_fields }
 *
 * `normalizeItem` does the inbound flattening (used by every list
 * and detail read).
 *
 * `denormalizeForPut` does the inverse — it's required because
 * dashd's PUT validators reject the flat shape. Without it, every
 * form submit would silently 4xx (A-IF1-G3).
 * ═══════════════════════════════════════════════════════════════ */

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function normalizeItem<T>(raw: any): T {
  if (!raw) return raw;
  // Already normalized (has metadata)
  if (raw.metadata) return raw as T;
  // dashd format: { kind, name, namespace, generation, spec }
  if (raw.name && raw.spec !== undefined) {
    const { kind: _kind, name, namespace, generation, spec, ...rest } = raw;
    return {
      metadata: { namespace: namespace || 'default', name, generation },
      ...spec,
      ...rest,
    } as T;
  }
  return raw as T;
}

function normalizeList<T>(raw: ListResponse<T>): ListResponse<T> {
  if (!raw?.items) return { items: [] };
  return { items: raw.items.map((item) => normalizeItem<T>(item)) };
}

/* ── Kind → wire-format Kind name map ──────────────────────── */
/* dashd's wire envelope carries an explicit `kind` field with a
 * Pascal-case name. The router/URL slug is lowercased-with-dashes;
 * the wire kind is PascalCase. This map bridges the two. */

const WIRE_KIND_BY_SLUG: Record<string, string> = {
  vnets: 'Vnet',
  enis: 'Eni',
  'acl-policies': 'AclPolicy',
  'route-policies': 'RoutePolicy',
  'vnet-mappings': 'VnetMapping',
  'service-tunnels': 'ServiceTunnel',
  ha: 'HaSet',
};

/** Returns the dashd wire `kind` for a URL slug, or PascalCases
 *  the slug as a best-effort fallback. */
export function wireKindForSlug(slug: string): string {
  const known = WIRE_KIND_BY_SLUG[slug];
  if (known) return known;
  return slug
    .split('-')
    .map((p) => (p.length === 0 ? p : p[0]!.toUpperCase() + p.slice(1)))
    .join('');
}

/**
 * Denormalize a SPA-flattened object back into dashd's wire format.
 *
 * Input  (SPA shape):
 *   {
 *     metadata: { namespace, name, generation?, labels?, annotations? },
 *     vnet_name: "vnet-blue",
 *     mac_address: "aa:bb:…",
 *     ...
 *   }
 *
 * Output (dashd wire shape):
 *   {
 *     kind: "Eni",
 *     name: "eni-blue-1",
 *     namespace: "default",
 *     generation?: 3,
 *     spec: { vnet_name: "vnet-blue", mac_address: "aa:bb:…", … }
 *   }
 *
 * The fallback `kindOverride` is taken when the caller knows the
 * wire kind explicitly (e.g., the `crudApi(kind).put` wrapper).
 *
 * Already-denormalized inputs (those containing a `spec` field
 * already) are passed through unchanged.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function denormalizeForPut(
  body: any,
  kindSlug: string,
  fallbackName?: string,
  fallbackNs?: string,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): any {
  if (!body) return body;

  // Pass-through: caller already gave us the wire shape.
  if (body.kind && body.spec !== undefined) {
    return body;
  }

  const meta = body.metadata ?? {};
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { metadata, ...rest } = body;

  const name = meta.name ?? fallbackName ?? '';
  const namespace = meta.namespace ?? fallbackNs ?? 'default';

  const wireBody: Record<string, unknown> = {
    kind: wireKindForSlug(kindSlug),
    name,
    namespace,
    spec: rest,
  };

  if (meta.generation !== undefined) wireBody.generation = meta.generation;
  if (meta.labels !== undefined) {
    (wireBody.spec as Record<string, unknown>).labels = meta.labels;
  }
  if (meta.annotations !== undefined) {
    (wireBody.spec as Record<string, unknown>).annotations = meta.annotations;
  }

  return wireBody;
}

/* ── Generic CRUD helpers ──────────────────────────────────── */

function crudApi<T>(kind: string) {
  return {
    list: (ns = 'default') =>
      api.get<ListResponse<T>>(`${R}/${ns}/${kind}`).then(normalizeList),
    get: (ns: string, name: string) =>
      api.get<T>(`${R}/${ns}/${kind}/${name}`).then(normalizeItem),
    put: (ns: string, name: string, body: T) =>
      // Denormalize BEFORE sending — without this, dashd rejects
      // the SPA's flattened shape (A-IF1-G3).
      api.put<T>(
        `${R}/${ns}/${kind}/${name}`,
        denormalizeForPut(body as unknown, kind, name, ns),
      ),
    delete: (ns: string, name: string) =>
      api.delete(`${R}/${ns}/${kind}/${name}`),
  };
}

/* ── Per-kind APIs ─────────────────────────────────────────── */

export const vnetApi = crudApi<VnetSpec>('vnets');
export const eniApi = crudApi<EniSpec>('enis');
export const aclPolicyApi = crudApi<AclPolicySpec>('acl-policies');
export const routePolicyApi = crudApi<RoutePolicySpec>('route-policies');
export const vnetMappingApi = crudApi<VnetMappingSpec>('vnet-mappings');
export const serviceTunnelApi = crudApi<ServiceTunnelSpec>('service-tunnels');
export const haSetApi = crudApi<HaSetSpec>('ha');

/* ── Inventory ─────────────────────────────────────────────── */

export const inventoryApi = {
  list: () => api.get<ListResponse<DpuRecord>>(`${R}/inventory`),
  get: (dpuId: string) => api.get<DpuRecord>(`${R}/inventory/${dpuId}`),
};

/* ── Operations ────────────────────────────────────────────── */

export const opsApi = {
  simulate: (req: SimulateRequest) =>
    api.post<SimulateResult>(`${R}/simulate`, req),
  reconcile: (req: ReconcileRequest) =>
    api.post<ReconcileResult>(`${R}/reconcile`, req),
};