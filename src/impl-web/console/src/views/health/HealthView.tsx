import { useState } from "react";
import { toast } from "sonner";
import { HeartPulse, AlertTriangle, RefreshCw, Crown } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import {
  useDashdHealth,
  useLeader,
  useDpuHealth,
  useDrift,
  useReconcile,
} from "@/queries/hooks";
import { formatDuration, timeAgo } from "@/lib/format";
import { connectedDpuCount } from "@/lib/api-helpers";
import type { DpuHealthEntry, DriftItem } from "@/api/types";

export default function HealthView() {
  const health = useDashdHealth();
  const leader = useLeader();
  const dpuHealth = useDpuHealth();
  const drift = useDrift();
  const reconcile = useReconcile();
  const [reconciling, setReconciling] = useState(false);

  async function handleReconcile(scope: "fleet" | string) {
    try {
      setReconciling(true);
      const result = await reconcile.mutateAsync(
        scope === "fleet" ? {} : { dpu_ids: [scope] }
      );
      toast.success(
        `Reconciled ${result.reconciled_count} resources`,
        result.errors.length > 0
          ? { description: `${result.errors.length} errors` }
          : undefined
      );
    } catch (err) {
      toast.error("Reconcile failed", {
        description: err instanceof Error ? err.message : String(err),
      });
    } finally {
      setReconciling(false);
    }
  }

  const dpuHealthColumns: Column<DpuHealthEntry>[] = [
    {
      key: "dpu_id",
      header: "DPU",
      accessor: (r) => r.dpu_id,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {r.dpu_id}
        </span>
      ),
    },
    {
      key: "state",
      header: "State",
      accessor: (r) => r.state,
      cell: (r) => <StatusBadge status={r.state} />,
      width: "w-32",
    },
    {
      key: "heartbeat",
      header: "Last Heartbeat",
      accessor: (r) => r.last_heartbeat ?? "",
      cell: (r) => (
        <span className="text-[color:var(--text-muted)]">
          {timeAgo(r.last_heartbeat)}
        </span>
      ),
    },
    {
      key: "enis",
      header: "ENIs",
      accessor: (r) => r.eni_count ?? 0,
      align: "right",
      cell: (r) => <span className="font-mono">{r.eni_count ?? 0}</span>,
    },
    {
      key: "addr",
      header: "Address",
      accessor: (r) => r.address ?? "",
      cell: (r) => (
        <span className="font-mono text-xs text-[color:var(--text-muted)]">
          {r.address ?? "—"}
        </span>
      ),
    },
    {
      key: "actions",
      header: "",
      accessor: () => "",
      sortable: false,
      cell: (r) => (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            handleReconcile(r.dpu_id);
          }}
          disabled={reconciling}
          className="text-xs px-2 py-1 rounded bg-white/5 hover:bg-white/10 border border-[color:var(--border-subtle)] text-[color:var(--accent-cyan)] disabled:opacity-50"
        >
          Reconcile
        </button>
      ),
      width: "w-32",
    },
  ];

  const driftColumns: Column<DriftItem>[] = [
    {
      key: "kind",
      header: "Kind",
      accessor: (d) => d.target_ref?.kind,
      cell: (d) => <span className="font-mono">{d.target_ref?.kind}</span>,
    },
    {
      key: "name",
      header: "Resource",
      accessor: (d) => `${d.target_ref?.namespace}/${d.target_ref?.name}`,
      cell: (d) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {d.target_ref?.namespace}/{d.target_ref?.name}
        </span>
      ),
    },
    {
      key: "dpu",
      header: "DPU",
      accessor: (d) => d.dpu_id,
      cell: (d) => <span className="font-mono text-xs">{d.dpu_id}</span>,
    },
    {
      key: "field",
      header: "Field",
      accessor: (d) => d.field,
      cell: (d) => <code className="text-xs">{d.field}</code>,
    },
    {
      key: "declared",
      header: "Declared",
      accessor: (d) => d.declared_value,
      cell: (d) => (
        <span className="text-xs font-mono text-[color:var(--accent-green)]">
          {d.declared_value}
        </span>
      ),
    },
    {
      key: "observed",
      header: "Observed",
      accessor: (d) => d.observed_value,
      cell: (d) => (
        <span className="text-xs font-mono text-[color:var(--accent-red)]">
          {d.observed_value}
        </span>
      ),
    },
  ];

  /* ─ Status panel ─ */
  const statusPanel = (
    <GlassCard glow={health.data?.leader ? "cyan" : "amber"}>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-md bg-white/5">
            <Crown size={20} className="text-[color:var(--accent-cyan)]" />
          </div>
          <div className="min-w-0">
            <p className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
              Role
            </p>
            <p className="text-base font-medium">
              {leader.data?.is_leader ? "Leader" : "Follower"}
            </p>
            {leader.data?.leader_id && (
              <p
                className="text-[10px] font-mono text-[color:var(--text-muted)] truncate max-w-[200px]"
                title={leader.data.leader_id}
              >
                leader={leader.data.leader_id}
              </p>
            )}
          </div>
        </div>

        <div className="flex items-center gap-3">
          <div className="p-2 rounded-md bg-white/5">
            <HeartPulse size={20} className="text-[color:var(--accent-green)]" />
          </div>
          <div>
            <p className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
              Connected DPUs
            </p>
            <p className="text-xl font-mono font-bold">
              {connectedDpuCount(health.data)}
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <div className="p-2 rounded-md bg-white/5">
            <RefreshCw size={20} className="text-[color:var(--accent-purple)]" />
          </div>
          <div>
            <p className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
              Uptime
            </p>
            <p className="text-base font-mono">
              {formatDuration(health.data?.uptime_seconds)}
            </p>
            {health.data?.cluster_size != null && (
              <p className="text-[10px] text-[color:var(--text-muted)]">
                cluster={health.data.cluster_size}
              </p>
            )}
          </div>
        </div>
      </div>
    </GlassCard>
  );

  const dpuHealthTab = (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <p className="text-xs text-[color:var(--text-secondary)]">
          {dpuHealth.data?.items?.length ?? 0} DPUs reporting
        </p>
        <button
          type="button"
          onClick={() => handleReconcile("fleet")}
          disabled={reconciling || reconcile.isPending}
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs bg-[color:var(--accent-cyan)]/15 hover:bg-[color:var(--accent-cyan)]/25 border border-[color:var(--accent-cyan)]/30 text-[color:var(--accent-cyan)] disabled:opacity-50 transition-colors"
        >
          <RefreshCw size={12} className={reconciling ? "animate-spin" : ""} />
          Reconcile Fleet
        </button>
      </div>
      {dpuHealth.isLoading ? (
        <CardSkeleton />
      ) : dpuHealth.isError ? (
        <ErrorState
          message={dpuHealth.error?.message ?? "Failed to load DPU health"}
          onRetry={() => dpuHealth.refetch()}
        />
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={dpuHealthColumns}
            data={dpuHealth.data?.items ?? []}
            rowKey={(r) => r.dpu_id}
            defaultSort={{ key: "dpu_id", direction: "asc" }}
            emptyMessage="No DPU health reports."
            filterPlaceholder="Filter DPUs…"
          />
        </GlassCard>
      )}
    </div>
  );

  const driftTab = (
    <div className="space-y-4">
      <p className="text-xs text-[color:var(--text-secondary)]">
        {drift.data?.items?.length ?? 0} drift items detected
      </p>
      {drift.isLoading ? (
        <CardSkeleton />
      ) : drift.isError ? (
        <ErrorState
          message={drift.error?.message ?? "Failed to load drift"}
          onRetry={() => drift.refetch()}
        />
      ) : (drift.data?.items?.length ?? 0) === 0 ? (
        <GlassCard glow="green">
          <div className="flex flex-col items-center gap-2 py-8 text-[color:var(--text-secondary)]">
            <HeartPulse
              size={28}
              className="text-[color:var(--accent-green)]"
            />
            <p className="text-sm font-medium">No drift — fleet in sync</p>
            <p className="text-xs text-[color:var(--text-muted)]">
              Declared and observed state match for all DPUs.
            </p>
          </div>
        </GlassCard>
      ) : (
        <GlassCard className="p-0" glow="amber">
          <DataTable
            columns={driftColumns}
            data={drift.data?.items ?? []}
            rowKey={(d) => `${d.dpu_id}-${d.target_ref?.namespace}-${d.target_ref?.name}-${d.field}`}
            defaultSort={{ key: "name", direction: "asc" }}
            emptyMessage="No drift items."
            filterPlaceholder="Filter drift…"
          />
        </GlassCard>
      )}
    </div>
  );

  const tabs: TabDef[] = [
    {
      id: "dpus",
      label: "DPU Health",
      badge: dpuHealth.data?.items?.length ?? 0,
      icon: <HeartPulse size={14} />,
      content: dpuHealthTab,
    },
    {
      id: "drift",
      label: "Drift",
      badge: drift.data?.items?.length ?? 0,
      icon: <AlertTriangle size={14} />,
      content: driftTab,
    },
  ];

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Health"
        subtitle="dashd controller and DPU reconciliation status"
      />
      {statusPanel}
      <Tabs tabs={tabs} defaultTabId="dpus" />
    </div>
  );
}