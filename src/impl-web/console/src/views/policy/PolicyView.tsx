import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { EmptyState } from '@/components/feedback/EmptyState';
import { useAclPolicies } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function PolicyView() {
  const { data, isLoading, isError, error, refetch } = useAclPolicies();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  const items: any[] = data?.items ?? [];
  return (
    <div className="animate-fade-in">
      <PageHeader title="Policies" subtitle="ACL policies" />
      {isLoading ? <LoadingSkeleton lines={6} /> : items.length === 0 ? (
        <EmptyState title="No ACL Policies" description="No ACL policies found." />
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
                  <span className="text-xs px-2 py-0.5 rounded bg-bg-elevated">
                    Default: {p?.default_action ?? '—'}
                  </span>
                </div>
                <div className="text-xs text-text-muted mb-2">ENIs: {eniNames.join(', ') || '—'}</div>
                <div className="space-y-1">
                  {rules.map((r: any, i: number) => (
                    <div key={i} className="flex items-center gap-2 text-sm border-l-2 pl-2" style={{ borderColor: r?.action === 'ALLOW' ? 'var(--accent-green)' : 'var(--accent-red)' }}>
                      <span className="font-mono text-text-muted w-8">P{r?.priority ?? i}</span>
                      <span className={r?.action === 'ALLOW' ? 'text-accent-green' : 'text-accent-red'}>{r?.action ?? '—'}</span>
                      <span className="text-text-muted">{r?.direction ?? '—'}</span>
                      <span className="text-text-secondary">
                        {r?.protocol ? `proto:${r.protocol}` : ''} 
                        {r?.dst_port_range ? ` port:${r.dst_port_range.start}-${r.dst_port_range.end}` : ''}
                      </span>
                      <span className="text-text-muted text-xs">{(r?.src_prefixes ?? []).join(', ')}</span>
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