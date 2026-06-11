import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useAclPolicies } from '@/queries/hooks';

export default function PolicyView() {
  const { data, isLoading, isError, error, refetch } = useAclPolicies();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  return (
    <div className="animate-fade-in">
      <PageHeader title="Policies" subtitle="ACL and route policies" />
      {isLoading ? <LoadingSkeleton lines={6} /> : (
        <div className="space-y-3">
          {data?.items.map((p) => (
            <GlassCard key={p.metadata.name}>
              <div className="flex justify-between items-center mb-2">
                <span className="font-mono font-bold">{p.metadata.name}</span>
                <span className="text-xs px-2 py-0.5 rounded bg-bg-elevated text-text-secondary">
                  {p.default_action} | {p.rules.length} rules
                </span>
              </div>
              <p className="text-sm text-text-secondary">ENIs: {p.eni_names.join(', ')}</p>
              <div className="mt-2 space-y-1">
                {p.rules.slice(0, 3).map((r) => (
                  <div key={r.priority} className="text-xs font-mono text-text-muted">
                    P{r.priority} {r.direction} {r.action} proto={r.protocol ?? '*'}
                  </div>
                ))}
                {p.rules.length > 3 && <div className="text-xs text-text-muted">...and {p.rules.length - 3} more</div>}
              </div>
            </GlassCard>
          ))}
        </div>
      )}
    </div>
  );
}