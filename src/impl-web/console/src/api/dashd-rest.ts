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
 * dashd's serialization is asymmetric:
 *
 *   READ  (GET/LIST returns envelope):
 *     { kind, name, namespace, generation, spec: { …spec_fields… } }
 *
 *   WRITE (PUT accepts flat shape — VERIFIED via bootstrap.py + a
 *          head-to-head probe in scripts/debug-resources-api-v2.py;
 *          the envelope shape returns HTTP 200 but silently
 *          DROPS every spec field):
 *     { metadata: { namespace, name, [labels], … }, …spec_fields }
 *
 * `normalizeItem` handles the inbound flattening.
 * `bodyForPut` formerly denormalized into the envelope; we now
 * passthrough the SPA flat shape because that is what PUT actually
 * accepts. The earlier "A-IF1-G3" denormalization was a misdiagnosis
 * that caused every form submit to silently lose the entire spec.
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
  // dashd routes HaSet under `/v1/{ns}/ha-sets/...`. The earlier
  // `ha` slug returned HTTP 405 on PUT (route matched a different
  // method-set). Verified via scripts/debug-labels-and-errors.py.
  'ha-sets': 'HaSet',
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
 * Coerce a body into the shape dashd PUT actually accepts.
 *
 * dashd's PUT contract — empirically verified via the head-to-head
 * probe in `scripts/debug-labels-and-errors.py` — has two quirks
 * relative to the SPA's natural form-state shape:
 *
 *   • The whole `spec` is silently swallowed if the body is wrapped
 *     in the wire envelope `{kind, name, namespace, spec: {...}}`.
 *     dashd expects the flat shape:
 *
 *         { metadata: { namespace, name, [generation] },
 *           ...spec_fields_inline,
 *           [labels: {...}] }
 *
 *   • `labels` must live at the TOP LEVEL alongside the spec
 *     fields. Labels nested under `metadata.labels` are dropped —
 *     dashd's serializer reads only the top-level `labels` field
 *     and persists it inside `spec.labels` on the readback path.
 *
 * This function:
 *   - Unwraps any incoming wire-envelope body (defensive — covers
 *     pre-fix callers that handcrafted the envelope shape).
 *   - Canonicalises `metadata.{namespace,name}` from the form
 *     values or the URL-path fallbacks.
 *   - **Lifts `metadata.labels` → top-level `labels`** so they
 *     survive the round-trip.
 *
 * `kindSlug`, `fallbackName`, `fallbackNs` are kept for
 * source-compat with the prior `denormalizeForPut` signature; we
 * no longer add a `kind` field — dashd PUT ignores it.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function bodyForPut(
  body: any,
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  _kindSlug: string,
  fallbackName?: string,
  fallbackNs?: string,
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
): any {
  if (!body) return body;

  // Unwrap envelope shape if a caller pre-wrapped.
  if (body.kind !== undefined && body.spec !== undefined) {
    const { kind: _kind, name, namespace, generation, spec, ...rest } = body;
    body = {
      metadata: {
        namespace: namespace ?? fallbackNs ?? 'default',
        name: name ?? fallbackName ?? '',
        ...(generation !== undefined && { generation }),
      },
      ...spec,
      ...rest,
    };
  }

  // Canonicalise the metadata block.
  const meta = body.metadata ?? {};
  const namespace = meta.namespace ?? fallbackNs ?? 'default';
  const name = meta.name ?? fallbackName ?? '';

  // Pull labels OUT of metadata. dashd ignores `metadata.labels`;
  // top-level `labels` is the only path that round-trips.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  const { labels: metaLabels, namespace: _ns, name: _nm, ...metaRest } = meta;

  const result: Record<string, unknown> = {
    ...body,
    metadata: {
      ...metaRest,
      namespace,
      name,
    },
  };

  // Merge top-level labels: caller's top-level wins over metadata's
  // (matches dashd's "top-level wins" behaviour).
  if (metaLabels !== undefined || body.labels !== undefined) {
    result.labels = { ...(metaLabels ?? {}), ...(body.labels ?? {}) };
  }

  return result;
}

/** @deprecated Use `bodyForPut`. Retained for backwards-source-compat. */
export const denormalizeForPut = bodyForPut;

/* ── Generic CRUD helpers ──────────────────────────────────── */

function crudApi<T>(kind: string) {
  return {
    list: (ns = 'default') =>
      api.get<ListResponse<T>>(`${R}/${ns}/${kind}`).then(normalizeList),
    get: (ns: string, name: string) =>
      api.get<T>(`${R}/${ns}/${kind}/${name}`).then(normalizeItem),
    put: (ns: string, name: string, body: T) =>
      // dashd's PUT accepts the SPA flat shape natively but silently
      // drops every spec field when given the wire-envelope shape.
      // `bodyForPut` is essentially a passthrough that canonicalises
      // the metadata block. See scripts/debug-resources-api-v2.py
      // for the head-to-head shape probe that proves this.
      api.put<T>(
        `${R}/${ns}/${kind}/${name}`,
        bodyForPut(body as unknown, kind, name, ns),
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
export const haSetApi = crudApi<HaSetSpec>('ha-sets');

/* ── Inventory ─────────────────────────────────────────────── */

/* dashd's inventory endpoint uses a different envelope and a
 * different per-item shape than the per-namespace CRUD endpoints:
 *
 *   wire:  { dpus: [ { id, endpoint, state, labels, ... }, ... ] }
 *
 *   SPA:   { items: [ DpuRecord { identity: { dpu_id }, ... }, ... ] }
 *
 * `normalizeDpu` translates each entry; `inventoryApi.list` wraps
 * the response in the SPA-uniform `{items: [...]}` envelope so the
 * generic `useResourceList` dispatcher (and every form selector
 * built on top of it) treats inventory like any other kind. */

interface DpuWireRecord {
  id?: string;
  endpoint?: string;
  state?: string;
  zone?: string;
  tier?: string;
  appliance_id?: string;
  slot?: number;
  serial_number?: string;
  labels?: Record<string, string>;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  capabilities?: any;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  capacity_limits?: any;
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function normalizeDpu(raw: any): DpuRecord {
  if (!raw) return raw as DpuRecord;
  // Already in DpuRecord shape (defensive — supports server-side evolution).
  if (raw.identity?.dpu_id) return raw as DpuRecord;
  // Wire shape: flat object with `id` field.
  if (raw.id) {
    const { id, appliance_id, slot, serial_number, ...rest } =
      raw as DpuWireRecord;
    return {
      identity: {
        dpu_id: id!,
        ...(appliance_id !== undefined && { appliance_id }),
        ...(slot !== undefined && { slot }),
        ...(serial_number !== undefined && { serial_number }),
      },
      ...rest,
    } as DpuRecord;
  }
  return raw as DpuRecord;
}

export const inventoryApi = {
  list: async () => {
    const raw = await api.get<{
      dpus?: DpuWireRecord[];
      items?: DpuWireRecord[];
    }>(`${R}/inventory`);
    const dpus = raw?.dpus ?? raw?.items ?? [];
    return { items: dpus.map(normalizeDpu) } satisfies ListResponse<DpuRecord>;
  },
  get: async (dpuId: string) => {
    const raw = await api.get<DpuWireRecord>(`${R}/inventory/${dpuId}`);
    return normalizeDpu(raw);
  },
};

/* ── Operations ────────────────────────────────────────────── */

export const opsApi = {
  simulate: (req: SimulateRequest) =>
    api.post<SimulateResult>(`${R}/simulate`, req),
  reconcile: (req: ReconcileRequest) =>
    api.post<ReconcileResult>(`${R}/reconcile`, req),
};