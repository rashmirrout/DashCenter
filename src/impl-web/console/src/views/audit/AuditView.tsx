import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { useAuditLog } from '@/queries/hooks';
import { timeAgo } from '@/lib/format';

export default function AuditView() {
  const { data, isLoading, isError, error, refetch } = useAuditLog();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  return (
    <div className="animate-fade-in">
      <PageHeader title="Audit Log" subtitle="Recent operations and changes" />
      {isLoading ? <LoadingSkeleton lines={10} /> : (
        <GlassCard>
          <div className="space-y-0">
            {data?.items.map((e) => (
              <div key={e.id} className="flex items-center gap-3 py-2 border-b border-border last:border-0 text-sm">
                <span className="text-text-muted text-xs w-20 flex-shrink-0">{timeAgo(e.timestamp)}</span>
                <span className="px-1.5 py-0.5 rounded text-[10px] bg-bg-elevated text-text-secondary uppercase">{e.action}</span>
                <span className="font-mono">{e.resource_kind}/{e.resource_name}</span>
                {e.result && <span className="ml-auto text-xs text-text-muted">{e.result}</span>}
              </div>
            ))}
          </div>
        </GlassCard>
      )}
    </div>
  );
}