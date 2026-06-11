import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { EmptyState } from '@/components/feedback/EmptyState';
import { useServiceTunnels } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function TunnelView() {
  const { data, isLoading, isError, error, refetch } = useServiceTunnels();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  const items: any[] = data?.items ?? [];
  return (
    <div className="animate-fade-in">
      <PageHeader title="Tunnels" subtitle="Service tunnels" />
      {isLoading ? <LoadingSkeleton lines={4} /> : items.length === 0 ? (
        <EmptyState title="No Service Tunnels" description="No service tunnels found." />
      ) : (
        <div className="space-y-3">
          {items.map((t: any) => {
            const name = t?.metadata?.name ?? t?.name ?? '—';
            return (
              <GlassCard key={name}>
                <div className="flex items-center justify-between">
                  <p className="font-mono font-bold">{name}</p>
                  <span className="text-xs px-2 py-0.5 rounded bg-bg-elevated">{t?.tunnel_type ?? '—'}</span>
                </div>
                <div className="flex gap-4 mt-2 text-sm">
                  <span className="text-text-secondary">Source: <span className="font-mono">{t?.source_vnet ?? '—'}</span></span>
                  <span className="text-text-muted">→</span>
                  <span className="text-text-secondary">Dest: <span className="font-mono">{t?.destination_vnet ?? '—'}</span></span>
                </div>
                {t?.bidirectional && <span className="text-xs text-accent-cyan mt-1 inline-block">⇄ Bidirectional</span>}
              </GlassCard>
            );
          })}
        </div>
      )}
    </div>
  );
}