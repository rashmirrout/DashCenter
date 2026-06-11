import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useRoutePolicies } from '@/queries/hooks';

export default function RoutingView() {
  const { data, isLoading, isError, error, refetch } = useRoutePolicies();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  return (
    <div className="animate-fade-in">
      <PageHeader title="Routing" subtitle="Route policies and prefix tables" />
      {isLoading ? <LoadingSkeleton lines={6} /> : (
        <div className="space-y-3">
          {data?.items.map((rp) => (
            <GlassCard key={rp.metadata.name}>
              <div className="flex justify-between items-center mb-2">
                <span className="font-mono font-bold">{rp.metadata.name}</span>
                <span className="text-xs text-text-muted">{rp.direction} | {rp.rules.length} rules</span>
              </div>
              <p className="text-sm text-text-secondary">ENIs: {rp.eni_names.join(', ')}</p>
            </GlassCard>
          ))}
        </div>
      )}
    </div>
  );
}