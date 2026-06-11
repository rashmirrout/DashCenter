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
            <StatsCard label="DPUs" value={fs?.total_dpus ?? 0} icon={<span className="text-2xl">⬡</span>} />
            <StatsCard label="ENIs" value={formatNumber(fs?.total_enis ?? 0)} icon={<span className="text-2xl">◈</span>} />
            <StatsCard label="Vnets" value={fs?.total_vnets ?? 0} icon={<span className="text-2xl">◉</span>} />
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
              <span className="text-2xl font-bold text-accent-green">{fs?.healthy_dpus ?? '—'}</span>
              <p className="text-[10px] text-text-muted">Healthy</p>
            </div>
            <div className="text-center">
              <span className="text-2xl font-bold text-accent-amber">{fs?.degraded_dpus ?? '—'}</span>
              <p className="text-[10px] text-text-muted">Degraded</p>
            </div>
            <div className="text-center">
              <span className="text-2xl font-bold text-accent-red">{fs?.disconnected_dpus ?? '—'}</span>
              <p className="text-[10px] text-text-muted">Disconnected</p>
            </div>
          </div>
        </GlassCard>

        {/* DPU State Breakdown */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase tracking-wider mb-3">DPU States</p>
          <div className="flex flex-wrap gap-2">
            {fs?.dpu_states && Object.entries(fs.dpu_states).map(([state, count]) => (
              <div key={state} className="flex items-center gap-1.5">
                <StatusBadge status={state} />
                <span className="text-sm font-mono">{count}</span>
              </div>
            ))}
            {!fs?.dpu_states && <span className="text-text-muted text-sm">No data</span>}
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
              used={cs.fleet_totals.total_enis}
              max={cs.fleet_totals.max_enis}
            />
            <CapacityGauge
              label="Routes"
              used={cs.fleet_totals.total_routes}
              max={cs.fleet_totals.max_routes}
            />
            <CapacityGauge
              label="ACL Rules"
              used={cs.fleet_totals.total_acl_rules}
              max={cs.fleet_totals.max_acl_rules}
            />
            <CapacityGauge
              label="Flows"
              used={cs.fleet_totals.total_flows}
              max={cs.fleet_totals.max_flows}
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
            <p className="font-mono text-lg">{fs?.total_acl_policies ?? '—'}</p>
          </div>
          <div>
            <span className="text-text-muted">Route Policies</span>
            <p className="font-mono text-lg">{fs?.total_route_policies ?? '—'}</p>
          </div>
          <div>
            <span className="text-text-muted">Service Tunnels</span>
            <p className="font-mono text-lg">{fs?.total_service_tunnels ?? '—'}</p>
          </div>
          <div>
            <span className="text-text-muted">HA Sets</span>
            <p className="font-mono text-lg">{fs?.total_ha_sets ?? '—'}</p>
          </div>
        </div>
      </GlassCard>
    </div>
  );
}