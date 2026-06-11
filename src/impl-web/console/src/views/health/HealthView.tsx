import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useDashdHealth, useLeader, useDpuHealth, useReconcile } from '@/queries/hooks';
import { formatDuration } from '@/lib/format';

export default function HealthView() {
  const health = useDashdHealth();
  const leader = useLeader();
  const dpus = useDpuHealth();
  const reconcile = useReconcile();

  if (health.isError) return <ErrorState message={health.error.message} onRetry={() => health.refetch()} />;

  return (
    <div className="animate-fade-in">
      <PageHeader
        title="dashd Health"
        subtitle="Controller cluster and connected DPUs"
        actions={
          <button
            onClick={() => reconcile.mutate({})}
            disabled={reconcile.isPending}
            className="px-3 py-1.5 text-sm rounded-lg bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/30 transition-colors disabled:opacity-50"
          >
            {reconcile.isPending ? 'Reconciling...' : 'Reconcile All'}
          </button>
        }
      />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-6">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Controller</p>
          {health.isLoading ? <LoadingSkeleton lines={4} /> : health.data && (
            <div className="space-y-2 text-sm">
              <div className="flex justify-between"><span className="text-text-secondary">Status</span><StatusBadge status={health.data.status} /></div>
              <div className="flex justify-between"><span className="text-text-secondary">Leader</span><span className="font-mono">{health.data.leader ? 'Yes' : 'No'}</span></div>
              <div className="flex justify-between"><span className="text-text-secondary">Connected DPUs</span><span className="font-mono">{health.data.connected_dpus}</span></div>
              <div className="flex justify-between"><span className="text-text-secondary">Uptime</span><span className="font-mono">{formatDuration(health.data.uptime_seconds)}</span></div>
              {health.data.version && <div className="flex justify-between"><span className="text-text-secondary">Version</span><span className="font-mono">{health.data.version}</span></div>}
            </div>
          )}
        </GlassCard>
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Leader Election</p>
          {leader.isLoading ? <LoadingSkeleton lines={3} /> : leader.data && (
            <div className="space-y-2 text-sm">
              <div className="flex justify-between"><span className="text-text-secondary">Leader ID</span><span className="font-mono">{leader.data.leader_id}</span></div>
              <div className="flex justify-between"><span className="text-text-secondary">Member ID</span><span className="font-mono">{leader.data.member_id}</span></div>
              <div className="flex justify-between"><span className="text-text-secondary">Cluster Size</span><span className="font-mono">{leader.data.cluster_size}</span></div>
            </div>
          )}
        </GlassCard>
      </div>
      <GlassCard>
        <p className="text-xs text-text-secondary uppercase mb-3">Connected DPUs</p>
        {dpus.isLoading ? <LoadingSkeleton lines={5} /> : (
          <div className="space-y-2">
            {dpus.data?.items.map((d) => (
              <div key={d.dpu_id} className="flex items-center justify-between text-sm py-1 border-b border-border last:border-0">
                <span className="font-mono">{d.dpu_id}</span>
                <div className="flex items-center gap-3">
                  {d.eni_count != null && <span className="text-text-muted">{d.eni_count} ENIs</span>}
                  <StatusBadge status={d.state} />
                </div>
              </div>
            ))}
          </div>
        )}
      </GlassCard>
    </div>
  );
}