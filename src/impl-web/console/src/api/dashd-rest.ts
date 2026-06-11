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

/* ── Generic CRUD helpers ──────────────────────────────────── */

function crudApi<T>(kind: string) {
  return {
    list: (ns = 'default') => api.get<ListResponse<T>>(`${R}/${ns}/${kind}`),
    get: (ns: string, name: string) => api.get<T>(`${R}/${ns}/${kind}/${name}`),
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