import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { queryKeys } from './keys';
import { consoleApi } from '@/api/console-api';
import { adminApi } from '@/api/dashd-admin';
import {
  vnetApi,
  eniApi,
  aclPolicyApi,
  routePolicyApi,
  serviceTunnelApi,
  vnetMappingApi,
  haSetApi,
  inventoryApi,
  opsApi,
} from '@/api/dashd-rest';
import { POLL_INTERVALS } from '@/lib/constants';
import { diagnosticsApi } from '@/api/diagnostics';
import type {
  VnetSpec,
  EniSpec,
  AclPolicySpec,
  RoutePolicySpec,
  ServiceTunnelSpec,
  HaSetSpec,
  SimulateRequest,
  ReconcileRequest,
  TraceFlowRequest,
  ExplainMatchRequest,
  AclHitStatsRequest,
  ExplainDriftRequest,
  TriggerResimRequest,
} from '@/api/types';

/* ═══════════════════════════════════════════════════════════
 * QUERY HOOKS (A5-G7) — read-only data fetching with polling
 * ═══════════════════════════════════════════════════════════ */

export function useFleetSummary() {
  return useQuery({
    queryKey: queryKeys.fleet.summary(),
    queryFn: consoleApi.fleetSummary,
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useFleetTopology() {
  return useQuery({
    queryKey: queryKeys.fleet.topology(),
    queryFn: consoleApi.topology,
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useDpuDetail(dpuId: string) {
  return useQuery({
    queryKey: queryKeys.dpu.detail(dpuId),
    queryFn: () => consoleApi.dpuDetail(dpuId),
    refetchInterval: POLL_INTERVALS.DPU,
    enabled: !!dpuId,
  });
}

export function useVnetDetail(vnetName: string) {
  return useQuery({
    queryKey: queryKeys.vnet.detail(vnetName),
    queryFn: () => consoleApi.vnetDetail(vnetName),
    refetchInterval: POLL_INTERVALS.VNET,
    enabled: !!vnetName,
  });
}

export function useVnetCanvas(vnetName: string) {
  return useQuery({
    queryKey: queryKeys.vnet.canvas(vnetName),
    queryFn: () => consoleApi.vnetCanvas(vnetName),
    refetchInterval: POLL_INTERVALS.VNET,
    enabled: !!vnetName,
  });
}

export function useVnetList(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.vnet.list(ns),
    queryFn: () => vnetApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useEniList(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.eni.list(ns),
    queryFn: () => eniApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

/**
 * Fetch the pre-joined ENI detail view from the BFF.
 *
 * The handler at `/api/console/eni/{ns}/{name}/detail` fans out to 8
 * dashd endpoints in parallel and returns a single, fully resolved
 * `EniDetail` object (identity, parent Vnet + VNI, placement with
 * HA, ACLs split by stage, routes, tunnels, HaSet membership,
 * counters). Polled at the Vnet cadence — ENIs change less often
 * than fleet health.
 */
export function useEniDetail(name: string, ns = 'default') {
  return useQuery({
    queryKey: queryKeys.eni.detail(name, ns),
    queryFn: () => consoleApi.eniDetail(ns, name),
    refetchInterval: POLL_INTERVALS.VNET,
    enabled: !!name,
  });
}

export function useAclPolicies(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.policy.acl(ns),
    queryFn: () => aclPolicyApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useRoutePolicies(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.policy.route(ns),
    queryFn: () => routePolicyApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useServiceTunnels(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.tunnel.list(ns),
    queryFn: () => serviceTunnelApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useVnetMappings(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.mapping.list(ns),
    queryFn: () => vnetMappingApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useHaSets(ns = 'default') {
  return useQuery({
    queryKey: queryKeys.ha.list(ns),
    queryFn: () => haSetApi.list(ns),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useDashdHealth() {
  return useQuery({
    queryKey: queryKeys.health.dashd(),
    queryFn: adminApi.health,
    refetchInterval: POLL_INTERVALS.HEALTH,
  });
}

export function useLeader() {
  return useQuery({
    queryKey: queryKeys.health.leader(),
    queryFn: adminApi.leader,
    refetchInterval: POLL_INTERVALS.HEALTH,
  });
}

export function useCapacityStats() {
  return useQuery({
    queryKey: queryKeys.capacity.stats(),
    queryFn: consoleApi.capacityStats,
    refetchInterval: POLL_INTERVALS.CAPACITY,
  });
}

export function useInventory() {
  return useQuery({
    queryKey: queryKeys.inventory.list(),
    queryFn: inventoryApi.list,
    refetchInterval: POLL_INTERVALS.INVENTORY,
  });
}

export function useDpuHealth() {
  return useQuery({
    queryKey: queryKeys.dpu.health(),
    queryFn: adminApi.dpuHealth,
    refetchInterval: POLL_INTERVALS.HEALTH,
  });
}

export function useDrift(dpuId?: string) {
  return useQuery({
    queryKey: queryKeys.drift.list(dpuId),
    queryFn: () => adminApi.drift(dpuId),
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useAuditLog() {
  return useQuery({
    queryKey: queryKeys.audit.list(),
    queryFn: () => adminApi.audit(200),
    refetchInterval: POLL_INTERVALS.AUDIT,
  });
}

export function useEniPlacement() {
  return useQuery({
    queryKey: queryKeys.eniPlacement.list(),
    queryFn: adminApi.eniPlacement,
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

export function useServiceTopology() {
  return useQuery({
    queryKey: queryKeys.topology.service(),
    queryFn: consoleApi.serviceTopology,
    refetchInterval: POLL_INTERVALS.FLEET,
  });
}

/* ═══════════════════════════════════════════════════════════
 * MUTATION HOOKS (A5-G8) — write operations with toast + invalidation
 * ═══════════════════════════════════════════════════════════ */

export function usePutVnet() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ns, name, body }: { ns: string; name: string; body: VnetSpec }) =>
      vnetApi.put(ns, name, body),
    onSuccess: (_data, vars) => {
      toast.success(`Vnet ${vars.name} saved`);
      void qc.invalidateQueries({ queryKey: queryKeys.vnet.all });
      void qc.invalidateQueries({ queryKey: queryKeys.fleet.all });
    },
    onError: (err) => toast.error(`Failed to save Vnet: ${err.message}`),
  });
}

export function usePutEni() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ns, name, body }: { ns: string; name: string; body: EniSpec }) =>
      eniApi.put(ns, name, body),
    onSuccess: (_data, vars) => {
      toast.success(`ENI ${vars.name} saved`);
      void qc.invalidateQueries({ queryKey: queryKeys.eni.all });
      void qc.invalidateQueries({ queryKey: queryKeys.fleet.all });
      void qc.invalidateQueries({ queryKey: queryKeys.dpu.all });
    },
    onError: (err) => toast.error(`Failed to save ENI: ${err.message}`),
  });
}

export function usePutAclPolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ns, name, body }: { ns: string; name: string; body: AclPolicySpec }) =>
      aclPolicyApi.put(ns, name, body),
    onSuccess: (_data, vars) => {
      toast.success(`ACL Policy ${vars.name} saved`);
      void qc.invalidateQueries({ queryKey: queryKeys.policy.all });
    },
    onError: (err) => toast.error(`Failed to save ACL Policy: ${err.message}`),
  });
}

export function usePutRoutePolicy() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ns, name, body }: { ns: string; name: string; body: RoutePolicySpec }) =>
      routePolicyApi.put(ns, name, body),
    onSuccess: (_data, vars) => {
      toast.success(`Route Policy ${vars.name} saved`);
      void qc.invalidateQueries({ queryKey: queryKeys.policy.all });
    },
    onError: (err) => toast.error(`Failed to save Route Policy: ${err.message}`),
  });
}

export function usePutServiceTunnel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ns, name, body }: { ns: string; name: string; body: ServiceTunnelSpec }) =>
      serviceTunnelApi.put(ns, name, body),
    onSuccess: (_data, vars) => {
      toast.success(`Service Tunnel ${vars.name} saved`);
      void qc.invalidateQueries({ queryKey: queryKeys.tunnel.all });
      void qc.invalidateQueries({ queryKey: queryKeys.fleet.all });
    },
    onError: (err) => toast.error(`Failed to save Service Tunnel: ${err.message}`),
  });
}

export function usePutHaSet() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ ns, name, body }: { ns: string; name: string; body: HaSetSpec }) =>
      haSetApi.put(ns, name, body),
    onSuccess: (_data, vars) => {
      toast.success(`HA Set ${vars.name} saved`);
      void qc.invalidateQueries({ queryKey: queryKeys.ha.all });
    },
    onError: (err) => toast.error(`Failed to save HA Set: ${err.message}`),
  });
}

export function useDeleteResource() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ kind, ns, name }: { kind: string; ns: string; name: string }) => {
      const apis: Record<string, { delete: (ns: string, name: string) => Promise<void> }> = {
        vnets: vnetApi,
        enis: eniApi,
        'acl-policies': aclPolicyApi,
        'route-policies': routePolicyApi,
        'service-tunnels': serviceTunnelApi,
        ha: haSetApi,
        'vnet-mappings': vnetMappingApi,
      };
      const apiForKind = apis[kind];
      if (!apiForKind) throw new Error(`Unknown kind: ${kind}`);
      return apiForKind.delete(ns, name);
    },
    onSuccess: (_data, vars) => {
      toast.success(`Deleted ${vars.kind}/${vars.name}`);
      // Invalidate everything — deletion can affect multiple views
      void qc.invalidateQueries({ queryKey: queryKeys.fleet.all });
      void qc.invalidateQueries({ queryKey: queryKeys.vnet.all });
      void qc.invalidateQueries({ queryKey: queryKeys.eni.all });
      void qc.invalidateQueries({ queryKey: queryKeys.policy.all });
      void qc.invalidateQueries({ queryKey: queryKeys.tunnel.all });
      void qc.invalidateQueries({ queryKey: queryKeys.dpu.all });
    },
    onError: (err) => toast.error(`Delete failed: ${err.message}`),
  });
}

export function useReconcile() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: ReconcileRequest) => opsApi.reconcile(req),
    onSuccess: (data) => {
      toast.success(`Reconciled ${data.reconciled_count} items`);
      // Full cache invalidation on reconcile
      void qc.invalidateQueries();
    },
    onError: (err) => toast.error(`Reconcile failed: ${err.message}`),
  });
}

export function useSimulateFlow() {
  return useMutation({
    mutationFn: (req: SimulateRequest) => opsApi.simulate(req),
    onError: (err) => toast.error(`Simulate failed: ${err.message}`),
  });
}

/* ═══════════════════════════════════════════════════════════
 * DIAGNOSTICS HOOKS (PE-1) — observability mutations
 * ═══════════════════════════════════════════════════════════ */

/** Trace a packet through the full ACL→Route→Mapping pipeline. */
export function useTraceFlow() {
  return useMutation({
    mutationFn: (req: TraceFlowRequest) => diagnosticsApi.traceFlow(req),
    onError: (err) => toast.error(`Trace flow failed: ${err.message}`),
  });
}

/** Walk every candidate and explain why each matched or didn't. */
export function useExplainMatch() {
  return useMutation({
    mutationFn: (req: ExplainMatchRequest) => diagnosticsApi.explainMatch(req),
    onError: (err) => toast.error(`Explain match failed: ${err.message}`),
  });
}

/** Get ACL hit stats (optionally zero-hit only). */
export function useAclHitStats() {
  return useMutation({
    mutationFn: (req: AclHitStatsRequest) => diagnosticsApi.aclHitStats(req),
    onError: (err) => toast.error(`ACL hit stats failed: ${err.message}`),
  });
}

/** Get remediation suggestions for a specific drift item. */
export function useExplainDrift() {
  return useMutation({
    mutationFn: (req: ExplainDriftRequest) => diagnosticsApi.explainDrift(req),
    onError: (err) => toast.error(`Explain drift failed: ${err.message}`),
  });
}

/** Trigger re-evaluation of active flows on named DPUs/ENIs. */
export function useTriggerResimulation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: TriggerResimRequest) => diagnosticsApi.triggerResimulation(req),
    onSuccess: (data) => {
      toast.success(`Resimulated ${data.resimulated_count} flows`);
      void qc.invalidateQueries({ queryKey: queryKeys.dpu.all });
      void qc.invalidateQueries({ queryKey: queryKeys.fleet.all });
    },
    onError: (err) => toast.error(`Resimulation failed: ${err.message}`),
  });
}
