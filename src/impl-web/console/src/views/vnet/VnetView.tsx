import { useParams, useNavigate } from 'react-router-dom';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { EmptyState } from '@/components/feedback/EmptyState';
import { useVnetList, useVnetDetail } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function VnetView() {
  const { vnetName } = useParams<{ vnetName: string }>();
  if (vnetName) return <VnetDetailPanel vnetName={vnetName} />;
  return <VnetListPanel />;
}

function VnetListPanel() {
  const { data, isLoading, isError, error, refetch } = useVnetList();
  const navigate = useNavigate();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  const items = data?.items ?? [];
  return (
    <div className="animate-fade-in">
      <PageHeader title="Vnets" subtitle="Virtual networks" />
      {isLoading ? <LoadingSkeleton lines={6} /> : items.length === 0 ? (
        <EmptyState title="No Vnets" description="No virtual networks found." />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {items.map((v: any) => {
            const name = v?.metadata?.name ?? v?.name ?? '—';
            return (
              <GlassCard key={name} onClick={() => navigate(`/vnets/${name}`)}>
                <p className="font-mono font-bold">{name}</p>
                <p className="text-sm text-text-secondary">VNI: {v?.vni ?? '—'}</p>
                <p className="text-xs text-text-muted">{(v?.address_space ?? []).join(', ') || '—'}</p>
              </GlassCard>
            );
          })}
        </div>
      )}
    </div>
  );
}

function VnetDetailPanel({ vnetName }: { vnetName: string }) {
  const { data, isLoading, isError, error, refetch } = useVnetDetail(vnetName);
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  if (isLoading) return <LoadingSkeleton lines={8} />;
  const d = data as any;
  const vni = d?.spec?.vni ?? d?.vni ?? '—';
  const enis: any[] = d?.enis ?? [];
  const addrSpace: string[] = d?.spec?.address_space ?? d?.address_space ?? [];
  return (
    <div className="animate-fade-in">
      <PageHeader title={vnetName} subtitle={`VNI: ${vni} | ENIs: ${enis.length} | Mappings: ${d?.mapping_count ?? 0}`} />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-2">ENIs ({enis.length})</p>
          <div className="space-y-1 text-sm font-mono">
            {enis.length > 0 ? enis.map((e: any) => (
              <div key={e?.metadata?.name ?? e?.name}>{e?.metadata?.name ?? e?.name} — {e?.mac_address ?? '—'}</div>
            )) : <span className="text-text-muted">No ENIs</span>}
          </div>
        </GlassCard>
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-2">Address Space</p>
          <div className="space-y-1 text-sm font-mono">
            {addrSpace.length > 0 ? addrSpace.map((a) => <div key={a}>{a}</div>) : <span className="text-text-muted">—</span>}
          </div>
        </GlassCard>
      </div>
    </div>
  );
}