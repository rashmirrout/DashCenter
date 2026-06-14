import { api } from './client';
import { API_BASE } from '@/lib/constants';
import type {
  DashdHealthResponse,
  LeaderInfo,
  DriftItem,
  DpuHealthEntry,
  EniPlacement,
  ListResponse,
  AuditEntry,
} from './types';

const A = API_BASE.ADMIN;

export const adminApi = {
  health: () => api.get<DashdHealthResponse>(`${A}/health`),
  leader: () => api.get<LeaderInfo>(`${A}/leader`),
  dpuHealth: () => api.get<ListResponse<DpuHealthEntry>>(`${A}/health/dpus`),
  drift: (dpuId?: string) => {
    const q = dpuId ? `?dpu=${dpuId}` : '';
    return api.get<ListResponse<DriftItem>>(`${A}/drift${q}`);
  },
  observed: (ns: string, kind: string, name: string) =>
    api.get<unknown>(`${A}/observed/${ns}/${kind}/${name}`),
  eniPlacement: () =>
    api.get<ListResponse<EniPlacement>>(`${A}/eni-placement`),
  audit: (limit = 100) =>
    api.get<ListResponse<AuditEntry>>(`${A}/audit?limit=${limit}`),
};