import { useParams } from 'react-router-dom';
import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useVnetList, useVnetDetail } from '@/queries/hooks';
import { useNavigate } from 'react-router-dom';

export default function VnetView() {
  const { vnetName } = useParams<{ vnetName: string }>();
  
  if (vnetName) return <VnetDetailPanel vnetName={vnetName} />;
  return <VnetListPanel />;
}

function VnetListPanel() {
  const { data, isLoading, isError, error, refetch } = useVnetList();
  const navigate = useNavigate();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  return (
    <div className="animate-fade-in">
      <PageHeader title="Vnets" subtitle="Virtual networks" />
      {isLoading ? <LoadingSkeleton lines={6} /> : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {data?.items.map((v) => (
            <GlassCard key={v.metadata.name} onClick={() => navigate(`/vnets/${v.metadata.name}`)}>
              <p className="font-mono font-bold">{v.metadata.name}</p>
              <p className="text-sm text-text-secondary">VNI: {v.vni}</p>
              <p className="text-xs text-text-muted">{v.address_space.join(', ')}</p>
            </GlassCard>
          ))}
        </div>
      )}
    </div>
  );
}

function VnetDetailPanel({ vnetName }: { vnetName: string }) {
  const { data, isLoading, isError, error, refetch } = useVnetDetail(vnetName);
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  if (isLoading) return <LoadingSkeleton lines={8} />;
  return (
    <div className="animate-fade-in">
      <PageHeader title={vnetName} subtitle={`VNI: ${data?.spec.vni ?? '—'} | ENIs: ${data?.eni_count ?? 0} | Mappings: ${data?.mapping_count ?? 0}`} />
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-2">ENIs ({data?.eni_count})</p>
          <div className="space-y-1 text-sm font-mono">
            {data?.enis.map((e) => <div key={e.metadata.name}>{e.metadata.name} — {e.mac_address}</div>)}
          </div>
        </GlassCard>
        <GlassCard>
          <p className="text-xs text-text-secondary uppercase mb-2">Address Space</p>
          <div className="space-y-1 text-sm font-mono">
            {data?.spec.address_space.map((a) => <div key={a}>{a}</div>)}
          </div>
        </GlassCard>
      </div>
    </div>
  );
}