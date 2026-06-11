import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useDpuHealth } from '@/queries/hooks';
import { useNavigate } from 'react-router-dom';

export default function FleetView() {
  const { data, isLoading, isError, error, refetch } = useDpuHealth();
  const navigate = useNavigate();

  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;

  return (
    <div className="animate-fade-in">
      <PageHeader title="Fleet" subtitle="All DPUs and their status" />
      {isLoading ? (
        <LoadingSkeleton lines={8} />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {data?.items.map((dpu) => (
            <GlassCard
              key={dpu.dpu_id}
              onClick={() => navigate(`/fleet/dpu/${dpu.dpu_id}`)}
              className="hover:shadow-[var(--shadow-glow-cyan)]"
            >
              <div className="flex items-center justify-between mb-2">
                <span className="font-mono font-bold">{dpu.dpu_id}</span>
                <StatusBadge status={dpu.state} />
              </div>
              <div className="text-sm text-text-secondary space-y-1">
                {dpu.eni_count != null && <p>ENIs: {dpu.eni_count}</p>}
                {dpu.address && <p className="font-mono text-xs">{dpu.address}</p>}
              </div>
            </GlassCard>
          ))}
        </div>
      )}
    </div>
  );
}