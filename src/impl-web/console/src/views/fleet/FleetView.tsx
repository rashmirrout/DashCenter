import { useNavigate } from 'react-router-dom';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { StatusBadge } from '@/components/feedback/StatusBadge';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { EmptyState } from '@/components/feedback/EmptyState';
import { useFleetSummary } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function FleetView() {
  const { data, isLoading, isError, error, refetch } = useFleetSummary();
  const navigate = useNavigate();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  
  const fs = data as any;
  const dpus: any[] = fs?.dpus ?? [];
  const dpuCount = fs?.dpu_count ?? fs?.total_dpus ?? dpus.length;
  
  return (
    <div className="animate-fade-in">
      <PageHeader title="Fleet" subtitle={`${dpuCount} DPUs`} />
      {isLoading ? <LoadingSkeleton lines={8} /> : dpus.length === 0 ? (
        <EmptyState title="No DPUs" description="No DPUs found in the fleet." />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {dpus.map((dpu: any) => {
            const id = dpu?.id ?? dpu?.dpu_id ?? '—';
            const state = dpu?.state ?? 'UNKNOWN';
            return (
              <GlassCard key={id} onClick={() => navigate(`/fleet/dpu/${id}`)} className="cursor-pointer">
                <div className="flex items-center justify-between mb-2">
                  <p className="font-mono font-bold text-sm">{id}</p>
                  <StatusBadge status={state} />
                </div>
                {dpu?.last_seen && (
                  <p className="text-xs text-text-muted">Last seen: {new Date(dpu.last_seen).toLocaleTimeString()}</p>
                )}
                {dpu?.eni_count != null && (
                  <p className="text-xs text-text-secondary mt-1">ENIs: {dpu.eni_count}</p>
                )}
              </GlassCard>
            );
          })}
        </div>
      )}
    </div>
  );
}