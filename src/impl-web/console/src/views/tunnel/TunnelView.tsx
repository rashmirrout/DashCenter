import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useServiceTunnels } from '@/queries/hooks';

export default function TunnelView() {
  const { data, isLoading, isError, error, refetch } = useServiceTunnels();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  return (
    <div className="animate-fade-in">
      <PageHeader title="Tunnels" subtitle="Service tunnels between vnets" />
      {isLoading ? <LoadingSkeleton lines={6} /> : (
        <div className="space-y-3">
          {data?.items.map((t) => (
            <GlassCard key={t.metadata.name}>
              <span className="font-mono font-bold">{t.metadata.name}</span>
              <p className="text-sm text-text-secondary mt-1">{t.source_vnet} → {t.destination_vnet} ({t.tunnel_type})</p>
            </GlassCard>
          ))}
        </div>
      )}
    </div>
  );
}