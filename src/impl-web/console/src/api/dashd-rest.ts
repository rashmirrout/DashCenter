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

/* ── Response normalizer ───────────────────────────────────── */
// dashd returns items as: { kind, name, namespace, generation, spec: {...} }
// SPA views expect:        { metadata: { namespace, name }, ...spec_fields }
// This normalizer bridges the gap at the API adapter layer.

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

/* ── Generic CRUD helpers ──────────────────────────────────── */

function crudApi<T>(kind: string) {
  return {
    list: (ns = 'default') =>
      api.get<ListResponse<T>>(`${R}/${ns}/${kind}`).then(normalizeList),
    get: (ns: string, name: string) =>
      api.get<T>(`${R}/${ns}/${kind}/${name}`).then(normalizeItem),
    put: (ns: string, name: string, body: T) =>
      api.put<T>(`${R}/${ns}/${kind}/${name}`, body),
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