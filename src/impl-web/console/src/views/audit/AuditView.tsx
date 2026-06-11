import { PageHeader } from '@/components/layout/PageHeader';
import { GlassCard } from '@/components/feedback/GlassCard';
import { ErrorState } from '@/components/feedback/ErrorState';
import { LoadingSkeleton } from '@/components/feedback/LoadingSkeleton';
import { EmptyState } from '@/components/feedback/EmptyState';
import { useAuditLog } from '@/queries/hooks';

/* eslint-disable @typescript-eslint/no-explicit-any */

export default function AuditView() {
  const { data, isLoading, isError, error, refetch } = useAuditLog();
  if (isError) return <ErrorState message={error.message} onRetry={() => refetch()} />;
  const entries: any[] = Array.isArray(data) ? data : (data as any)?.items ?? (data as any)?.entries ?? [];
  return (
    <div className="animate-fade-in">
      <PageHeader title="Audit" subtitle="Event log" />
      {isLoading ? <LoadingSkeleton lines={8} /> : entries.length === 0 ? (
        <EmptyState title="No Audit Entries" description="No audit events recorded yet." />
      ) : (
        <GlassCard>
          <div className="space-y-2 max-h-[600px] overflow-auto">
            {entries.map((e: any, i: number) => (
              <div key={e?.id ?? i} className="flex items-start gap-3 text-sm border-b border-border/30 pb-2">
                <span className="text-text-muted text-xs font-mono w-20 flex-shrink-0">
                  {e?.timestamp ? new Date(e.timestamp).toLocaleTimeString() : '—'}
                </span>
                <span className="px-1.5 py-0.5 rounded text-xs bg-bg-elevated">{e?.action ?? '—'}</span>
                <span className="text-text-secondary">{e?.resource_kind ?? '—'}/{e?.resource_name ?? '—'}</span>
                {e?.detail && <span className="text-text-muted text-xs">{e.detail}</span>}
              </div>
            ))}
          </div>
        </GlassCard>
      )}
    </div>
  );
}