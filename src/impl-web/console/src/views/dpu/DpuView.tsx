import { useParams } from 'react-router-dom';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { CapacityGauge } from '@/components/visualization/CapacityGauge';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useDpuDetail } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function DpuView() {
  const { dpuId } = useParams<{ dpuId: string }>();
  const { data, isLoading, isError, error, refetch } = useDpuDetail(dpuId ?? '');

  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  if (isLoading) return <LoadingSkeleton lines={10} />;

  const d = data as any;
  const cap = d?.capacity ?? {};
  const enis: any[] = d?.enis ?? [];
  const driftItems: any[] = d?.drift_items ?? [];
  const state = d?.state ?? d?.health?.state ?? 'UNKNOWN';

  return (
    <div className="animate-fade-in">
      <PageHeader
        title={dpuId ?? 'DPU'}
        subtitle={`State: ${state}`}
      />

      {/* Capacity Gauges */}
      <GlassCard className="mb-4">
        <p className="text-xs text-text-secondary uppercase tracking-wider mb-4">Capacity</p>
        <div className="flex flex-wrap justify-around gap-4">
          <CapacityGauge label="ENIs" used={cap?.eni_count ?? enis.length} max={cap?.eni_max ?? 64} />
          <CapacityGauge label="Routes" used={cap?.route_count ?? 0} max={cap?.route_max ?? 1000} />
          <CapacityGauge label="ACL Rules" used={cap?.acl_rule_count ?? 0} max={cap?.acl_rule_max ?? 500} />
          <CapacityGauge label="Flows" used={cap?.flow_count ?? 0} max={cap?.flow_max ?? 10000} />
        </div>
      </GlassCard>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* ENI List */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-2">ENIs ({enis.length})</p>
          <div className="space-y-1 text-sm font-mono max-h-64 overflow-auto">
            {enis.length > 0 ? enis.map((e: any) => (
              <div key={e?.metadata?.name ?? e?.name} className="flex justify-between">
                <span>{e?.metadata?.name ?? e?.name}</span>
                <span className="text-text-muted">{e?.mac_address ?? '—'}</span>
              </div>
            )) : <span className="text-text-muted">No ENIs assigned</span>}
          </div>
        </GlassCard>

        {/* Drift */}
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-2">Drift ({driftItems.length})</p>
          <div className="space-y-1 text-sm max-h-64 overflow-auto">
            {driftItems.length > 0 ? driftItems.map((di: any, i: number) => (
              <div key={i} className="border-l-2 border-accent-amber pl-2">
                <span className="font-mono">{di?.field ?? '—'}</span>
                <span className="text-text-muted ml-2">declared: {di?.declared_value ?? '—'}</span>
                <span className="text-accent-red ml-2">observed: {di?.observed_value ?? '—'}</span>
              </div>
            )) : <span className="text-text-muted">No drift detected ✓</span>}
          </div>
        </GlassCard>
      </div>

      {/* Health */}
      {d?.health && (
        <GlassCard className="mt-4">
          <p className="text-xs text-text-secondary uppercase mb-2">Health</p>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
            <div>
              <span className="text-text-muted">State</span>
              <div><StatusBadge status={d.health?.state ?? state} /></div>
            </div>
            <div>
              <span className="text-text-muted">Last Heartbeat</span>
              <p className="font-mono">{d.health?.last_heartbeat ? new Date(d.health.last_heartbeat).toLocaleTimeString() : '—'}</p>
            </div>
            <div>
              <span className="text-text-muted">Connected At</span>
              <p className="font-mono">{d.health?.connected_at ? new Date(d.health.connected_at).toLocaleTimeString() : '—'}</p>
            </div>
            <div>
              <span className="text-text-muted">Address</span>
              <p className="font-mono">{d.health?.address ?? '—'}</p>
            </div>
          </div>
        </GlassCard>
      )}
    </div>
  );
}