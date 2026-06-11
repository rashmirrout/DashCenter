import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { EmptyState } from '@/components/feedback/EmptyState';
import { useRoutePolicies } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function RoutingView() {
  const { data, isLoading, isError, error, refetch } = useRoutePolicies();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  const items: any[] = data?.items ?? [];
  return (
    <div className="animate-fade-in">
      <PageHeader title="Routing" subtitle="Route policies" />
      {isLoading ? <LoadingSkeleton lines={6} /> : items.length === 0 ? (
        <EmptyState title="No Route Policies" description="No route policies found." />
      ) : (
        <div className="space-y-4">
          {items.map((p: any) => {
            const name = p?.metadata?.name ?? p?.name ?? '—';
            const rules: any[] = p?.rules ?? [];
            const eniNames: string[] = p?.eni_names ?? [];
            return (
              <GlassCard key={name}>
                <div className="flex items-center justify-between mb-2">
                  <p className="font-mono font-bold">{name}</p>
                  <span className="text-xs text-text-muted">{p?.direction ?? '—'} | {eniNames.length} ENIs</span>
                </div>
                <div className="text-xs text-text-muted mb-2">ENIs: {eniNames.join(', ') || '—'}</div>
                <div className="space-y-1">
                  {rules.map((r: any, i: number) => (
                    <div key={i} className="flex items-center gap-2 text-sm">
                      <span className="font-mono text-text-muted w-8">P{r?.priority ?? i}</span>
                      <span className={r?.action === 'PERMIT' ? 'text-accent-green' : 'text-accent-red'}>{r?.action ?? '—'}</span>
                      <span className="text-text-secondary">{(r?.prefixes ?? []).join(', ') || '—'}</span>
                    </div>
                  ))}
                  {rules.length === 0 && <span className="text-text-muted text-sm">No rules</span>}
                </div>
              </GlassCard>
            );
          })}
        </div>
      )}
    </div>
  );
}