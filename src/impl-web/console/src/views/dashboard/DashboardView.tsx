import { PageHeader } from '@/components/layout/PageHeader';
import { StatsCard } from '@/components/feedback/StatsCard';
import { GlassCard } from '@/components/feedback/GlassCard';
import { CapacityGauge } from '@/components/visualization/CapacityGauge';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { CardSkeleton } from '@/components/feedback/LoadingSkeleton';
import { ErrorState } from '@/components/feedback/ErrorState';
import { useFleetSummary, useCapacityStats, useDashdHealth } from '@/queries/hooks';
import { formatNumber, formatDuration } from '@/lib/format';

export default function DashboardView() {
  const fleet = useFleetSummary();
  const capacity = useCapacityStats();
  const health = useDashdHealth();

  if (fleet.isError) {
    return <ErrorState message={fleet.error.message} onRetry={() => fleet.refetch()} />;
  }

  const fs = fleet.data;
  const cs = capacity.data;
  const hd = health.data;

  return (
    <div className="animate-fade-in">
      <PageHeader title="Dashboard" subtitle="Fleet health and capacity overview" />

      {/* Stats row */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
        {fleet.isLoading ? (
          Array.from({ length: 4 }, (_, i) => <CardSkeleton key={i} />)
        ) : (
          <>
            <StatsCard label="DPUs" value={fs?.dpu_count ?? fs?.total_dpus ?? 0} icon={<span className="text-2xl">⬡</span>} />
            <StatsCard label="ENIs" value={formatNumber(fs?.eni_count ?? fs?.total_enis ?? 0)} icon={<span className="text-2xl">◈</span>} />
            <StatsCard label="Vnets" value={fs?.vnet_count ?? fs?.total_vnets ?? 0} icon={<span className="text-2xl">◉</span>} />
            <StatsCard label="Drift Items" value={fs?.drift_count ?? 0} icon={<span className="text-2xl">⚠</span>} />
          </>
        )}
      </div>

      {/* Health + DPU States */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4 mb-6">
        {/* Fleet health donut placeholder */}
        <GlassCard className="flex flex-col items-center justify-center">
          <p className="text-xs text-text-secondary uppercase tracking-wider mb-3">Fleet Health</p>
          <div className="flex gap-6">
            <div className="text-center">
              <span className="text-2xl font-bold text-accent-green">{fs?.dpus_by_state?.['DPU_STATE_UP'] ?? fs?.healthy_dpus ?? '—'}</span>
              <p className="text-[10px] text-text-muted">Healthy</p>
            </div>
            <div className="text-center">
              <span className="text-2xl font-bold text-accent-amber">{fs?.dpus_by_state?.['DPU_STATE_DEGRADED'] ?? fs?.degraded_dpus ?? 0}</span>
              <p className="text-[10px] text-text-muted">Degraded</p>
            </div>
            <div className="text-center">
              <span className="text-2xl font-bold text-accent-red">{fs?.dpus_by_state?.['DPU_STATE_DISCONNECTED'] ?? fs?.disconnected_dpus ?? 0}</span>
              <p className="text-[10px] text-text-muted">Disconnected</p>
            </div>
          </div>
        </GlassCard>

        {/* DPU State Breakdown */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase tracking-wider mb-3">DPU States</p>
          <div className="flex flex-wrap gap-2">
            {(fs?.dpus_by_state || fs?.dpu_states) && Object.entries(fs.dpus_by_state || fs.dpu_states || {}).map(([state, count]) => (
              <div key={state} className="flex items-center gap-1.5">
                <StatusBadge status={state} />
                <span className="text-sm font-mono">{count as number}</span>
              </div>
            ))}
            {!fs?.dpus_by_state && !fs?.dpu_states && <span className="text-text-muted text-sm">No data</span>}
          </div>
        </GlassCard>

        {/* dashd Health */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase tracking-wider mb-3">dashd Controller</p>
          {hd ? (
            <div className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-text-secondary">Status</span>
                <StatusBadge status={hd.leader ? 'LEADER' : 'FOLLOWER'} />
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Connected DPUs</span>
                <span className="font-mono">{hd.connected_dpus}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-text-secondary">Uptime</span>
                <span className="font-mono">{formatDuration(hd.uptime_seconds)}</span>
              </div>
              {hd.cluster_size != null && (
                <div className="flex justify-between">
                  <span className="text-text-secondary">Cluster Size</span>
                  <span className="font-mono">{hd.cluster_size}</span>
                </div>
              )}
            </div>
          ) : (
            <span className="text-text-muted text-sm">Loading...</span>
          )}
        </GlassCard>
      </div>

      {/* Capacity Gauges */}
      <GlassCard className="mb-6">
        <p className="text-xs text-text-secondary uppercase tracking-wider mb-4">Fleet Capacity</p>
        {cs ? (
          <div className="flex flex-wrap justify-around gap-4">
            <CapacityGauge
              label="ENIs"
              used={cs.fleet?.total_enis ?? 0}
              max={cs.fleet?.max_enis ?? 100}
            />
            <CapacityGauge
              label="Routes"
              used={cs.fleet?.total_routes ?? 0}
              max={cs.fleet?.max_routes ?? 1000}
            />
            <CapacityGauge
              label="ACL Rules"
              used={cs.fleet?.total_acl_rules ?? 0}
              max={cs.fleet?.max_acl_rules ?? 1000}
            />
            <CapacityGauge
              label="Flows"
              used={cs.fleet?.total_flows ?? 0}
              max={cs.fleet?.max_flows ?? 10000}
            />
          </div>
        ) : (
          <div className="flex justify-center py-4 text-text-muted text-sm">Loading capacity data...</div>
        )}
      </GlassCard>

      {/* Resource summary */}
      <GlassCard>
        <p className="text-xs text-text-secondary uppercase tracking-wider mb-3">Resources</p>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
          <div>
            <span className="text-text-muted">ACL Policies</span>
            <p className="font-mono text-lg">{fs?.acl_policy_count ?? fs?.total_acl_policies ?? '—'}</p>
          </div>
          <div>
            <span className="text-text-muted">Route Policies</span>
            <p className="font-mono text-lg">{fs?.route_policy_count ?? fs?.total_route_policies ?? '—'}</p>
          </div>
          <div>
            <span className="text-text-muted">Service Tunnels</span>
            <p className="font-mono text-lg">{fs?.service_tunnel_count ?? fs?.total_service_tunnels ?? '—'}</p>
          </div>
          <div>
            <span className="text-text-muted">HA Sets</span>
            <p className="font-mono text-lg">{fs?.ha_set_count ?? fs?.total_ha_sets ?? '—'}</p>
          </div>
        </div>
      </GlassCard>
    </div>
  );
}