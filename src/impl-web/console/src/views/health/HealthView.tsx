import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useDashdHealth, useLeader } from '@/queries/hooks';
import { formatDuration } from '@/lib/format';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function HealthView() {
  const health = useDashdHealth();
  const leader = useLeader();

  if (health.isError) return <ErrorState message={health.error.message} onRetry={() => health.refetch()} />;
  if (health.isLoading) return <LoadingSkeleton lines={6} />;

  const hd = health.data as any;
  const ld = leader.data as any;

  return (
    <div className="animate-fade-in">
      <PageHeader title="dashd Health" subtitle="Controller status" />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
        {/* Leader Status */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Leader Status</p>
          <div className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-text-secondary">Is Leader</span>
              <StatusBadge status={hd?.leader ? 'LEADER' : 'FOLLOWER'} />
            </div>
            <div className="flex justify-between">
              <span className="text-text-secondary">Leader ID</span>
              <span className="font-mono">{ld?.leader_id ?? hd?.leader_id ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-text-secondary">Member ID</span>
              <span className="font-mono">{ld?.member_id ?? hd?.member_id ?? '—'}</span>
            </div>
            {(ld?.cluster_size ?? hd?.cluster_size) != null && (
              <div className="flex justify-between">
                <span className="text-text-secondary">Cluster Size</span>
                <span className="font-mono">{ld?.cluster_size ?? hd?.cluster_size}</span>
              </div>
            )}
          </div>
        </GlassCard>

        {/* Health Details */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-3">Health</p>
          <div className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-text-secondary">Status</span>
              <span className="font-mono">{hd?.status ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-text-secondary">Connected DPUs</span>
              <span className="font-mono">{hd?.connected_dpus ?? '—'}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-text-secondary">Uptime</span>
              <span className="font-mono">{hd?.uptime_seconds != null ? formatDuration(hd.uptime_seconds) : '—'}</span>
            </div>
            {hd?.version && (
              <div className="flex justify-between">
                <span className="text-text-secondary">Version</span>
                <span className="font-mono">{hd.version}</span>
              </div>
            )}
          </div>
        </GlassCard>
      </div>

      {/* Reconcile */}
      <GlassCard>
        <p className="text-xs text-text-secondary uppercase mb-3">Reconcile</p>
        <p className="text-sm text-text-muted mb-3">Trigger a full reconciliation of declared state → DPU observed state.</p>
        <button
          className="px-4 py-2 text-sm rounded-lg bg-accent-cyan/10 text-accent-cyan border border-accent-cyan/30 hover:bg-accent-cyan/20 transition-colors"
          onClick={() => { /* reconcile mutation — Phase A placeholder */ }}
        >
          Reconcile Now
        </button>
      </GlassCard>
    </div>
  );
}