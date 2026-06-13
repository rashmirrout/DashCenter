import { api } from './client';
import { API_BASE } from '@/lib/constants';
import type {
  FleetSummary,
  DpuDetail,
  TopologyGraph,
  VnetDetail,
  VnetCanvasData,
  CapacityStats,
  ServiceTopologyResponse,
  EniDetail,
} from './types';

const C = API_BASE.CONSOLE;

export const consoleApi = {
  fleetSummary: () => api.get<FleetSummary>(`${C}/fleet/summary`),
  dpuDetail: (dpuId: string) => api.get<DpuDetail>(`${C}/dpu/${dpuId}/detail`),
  topology: () => api.get<TopologyGraph>(`${C}/topology`),
  vnetDetail: (vnetName: string) =>
    api.get<VnetDetail>(`${C}/vnet/${vnetName}/detail`),
  vnetCanvas: (vnetName: string) =>
    api.get<VnetCanvasData>(`${C}/vnet/${vnetName}/canvas`),
  capacityStats: () => api.get<CapacityStats>(`${C}/stats/capacity`),
  serviceTopology: () =>
    api.get<ServiceTopologyResponse>(`${C}/service-topology`),
  /**
   * Pre-joined view of a single ENI built by the BFF.
   * See: src/impl-go/console/internal/aggregation/aggregator.go EniDetail().
   * Namespace and name are URL-encoded to handle any operator-chosen names
   * containing reserved characters.
   */
  eniDetail: (namespace: string, name: string) =>
    api.get<EniDetail>(
      `${C}/eni/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/detail`
    ),
};
