import { useParams } from 'react-router-dom';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { CapacityGauge } from '@/components/visualization/CapacityGauge';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useDpuDetail } from '@/queries/hooks';

export default function DpuView() {
  const { dpuId } = useParams<{ dpuId: string }>();
  const { data, isLoading, isError, error, refetch } = useDpuDetail(dpuId ?? '');

  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  if (isLoading) return <LoadingSkeleton lines={10} />;

  const d = data;
  return (
    <div className="animate-fade-in">
      <PageHeader
        title={dpuId ?? 'DPU'}
        subtitle={d ? `State: ${d.state}` : undefined}
        actions={d && <StatusBadge status={d.state} size="md" />}
      />
      {d && (
        <>
          <div className="flex flex-wrap gap-4 mb-6">
            <CapacityGauge label="ENIs" used={d.capacity.eni_count} max={d.capacity.eni_max} />
            <CapacityGauge label="Routes" used={d.capacity.route_count} max={d.capacity.route_max} />
            <CapacityGauge label="ACL Rules" used={d.capacity.acl_rule_count} max={d.capacity.acl_rule_max} />
            <CapacityGauge label="Flows" used={d.capacity.flow_count} max={d.capacity.flow_max} />
          </div>
          <GlassCard className="mb-4">
            <p className="text-xs text-text-secondary uppercase mb-2">ENIs ({d.enis.length})</p>
            <div className="space-y-1 text-sm font-mono">
              {d.enis.map((e) => (
                <div key={e.metadata.name} className="flex justify-between">
                  <span>{e.metadata.name}</span>
                  <span className="text-text-muted">{e.vnet_name}</span>
                </div>
              ))}
            </div>
          </GlassCard>
          {d.drift_items.length > 0 && (
            <GlassCard>
              <p className="text-xs text-accent-amber uppercase mb-2">Drift ({d.drift_items.length})</p>
              <div className="space-y-1 text-sm">
                {d.drift_items.map((di, i) => (
                  <div key={i} className="flex gap-2 text-text-secondary">
                    <span className="font-mono">{di.target_ref.kind}/{di.target_ref.name}</span>
                    <span className="text-text-muted">— {di.field}</span>
                  </div>
                ))}
              </div>
            </GlassCard>
          )}
        </>
      )}
    </div>
  );
}