import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { useAuditLog } from "@/queries/hooks";
import { timeAgo } from "@/lib/format";
import type { AuditEntry } from "@/api/types";

export default function AuditView() {
  const audit = useAuditLog();

  const columns: Column<AuditEntry>[] = [
    {
      key: "timestamp",
      header: "When",
      accessor: (e) => e.timestamp,
      cell: (e) => (
        <span
          className="text-[color:var(--text-muted)]"
          title={new Date(e.timestamp).toISOString()}
        >
          {timeAgo(e.timestamp)}
        </span>
      ),
      width: "w-28",
    },
    {
      key: "action",
      header: "Action",
      accessor: (e) => e.action,
      cell: (e) => <StatusBadge status={e.action ?? "UNKNOWN"} />,
      width: "w-32",
    },
    {
      key: "kind",
      header: "Kind",
      accessor: (e) => e.resource_kind,
      cell: (e) => <span className="font-mono">{e.resource_kind}</span>,
      width: "w-32",
    },
    {
      key: "name",
      header: "Resource",
      accessor: (e) => `${e.namespace}/${e.resource_name}`,
      cell: (e) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {e.namespace}/{e.resource_name}
        </span>
      ),
    },
    {
      key: "result",
      header: "Result",
      accessor: (e) => e.result ?? "",
      cell: (e) =>
        e.result ? (
          <StatusBadge status={e.result} />
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
      width: "w-24",
    },
    {
      key: "txn",
      header: "Txn",
      accessor: (e) => e.txn_id ?? "",
      cell: (e) => (
        <span
          className="font-mono text-[10px] text-[color:var(--text-muted)] truncate max-w-[120px] inline-block"
          title={e.txn_id}
        >
          {e.txn_id ? `${e.txn_id.slice(0, 10)}…` : "—"}
        </span>
      ),
    },
    {
      key: "operator",
      header: "Operator",
      accessor: (e) => e.operator_id ?? "",
      cell: (e) => (
        <span className="font-mono text-xs">{e.operator_id ?? "—"}</span>
      ),
    },
  ];

  if (audit.isError) {
    return (
      <ErrorState
        message={audit.error?.message ?? "Failed to load audit log"}
        onRetry={() => audit.refetch()}
      />
    );
  }

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Audit Log"
        subtitle="Recent declarative changes recorded by dashd"
      />
      {audit.isLoading ? (
        <CardSkeleton />
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={columns}
            data={audit.data?.items ?? []}
            rowKey={(e) => e.id}
            defaultSort={{ key: "timestamp", direction: "desc" }}
            emptyMessage="No audit entries yet."
            filterPlaceholder="Filter entries…"
            pageSize={50}
          />
        </GlassCard>
      )}
    </div>
  );
}