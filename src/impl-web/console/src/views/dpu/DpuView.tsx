import { useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Cpu, Layers, AlertTriangle, Activity, ArrowLeft } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { CapacityGauge } from "@/components/visualization/CapacityGauge";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { LoadingSkeleton } from "@/components/feedback/LoadingSkeleton";
import { DataTable, type Column } from "@/components/data/DataTable";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import {
  useDpuDetail,
  useEniPlacement,
  useCapacityStats,
} from "@/queries/hooks";
import {
  capacityRows,
  dpuEntryId,
  entryAclMax,
  entryAclUsed,
  entryEniMax,
  entryEniUsed,
  entryFlowMax,
  entryFlowUsed,
  entryRouteMax,
  entryRouteUsed,
  placementDpuIds,
  placementEniName,
} from "@/lib/api-helpers";
import { timeAgo, formatMac, formatIp } from "@/lib/format";
import type { EniPlacement, DriftItem } from "@/api/types";

/* eslint-disable @typescript-eslint/no-explicit-any */

interface EniRow {
  name: string;
  vnet_name: string;
  mac_address: string;
  underlay_ip: string;
  admin_state: string;
  observed: boolean;
  haPeers: string[];
}

export default function DpuView() {
  const { dpuId = "" } = useParams<{ dpuId: string }>();
  const navigate = useNavigate();
  const dpu = useDpuDetail(dpuId);
  const placements = useEniPlacement();
  const capacity = useCapacityStats();

  if (dpu.isError) {
    return (
      <ErrorState message={dpu.error?.message ?? "Failed to load DPU"} onRetry={() => dpu.refetch()} />
    );
  }

  const d = dpu.data as any;
  const detailEnis: any[] = d?.enis ?? [];
  const driftItems: DriftItem[] = d?.drift_items ?? [];
  const state: string = d?.state ?? d?.health?.state ?? "UNKNOWN";

  // Filter the placement list down to this DPU and shape into rows.
  const placementEnis: EniRow[] = useMemo(() => {
    const items = placements.data?.items ?? [];
    const out: EniRow[] = [];
    for (const p of items as EniPlacement[]) {
      const dpuIds = placementDpuIds(p);
      if (!dpuIds.includes(dpuId)) continue;
      const slot = p.placements?.find((s) => s.dpu_id === dpuId);
      out.push({
        name: placementEniName(p),
        vnet_name: p.vnet_name ?? "",
        mac_address: p.mac_address ?? "",
        underlay_ip: p.underlay_ip ?? "",
        admin_state: p.admin_state ?? "",
        observed: slot?.observed ?? !!p.dpu_id,
        haPeers: dpuIds.filter((id) => id !== dpuId),
      });
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }, [placements.data, dpuId]);

  // Capacity row for this DPU (from /api/console/stats/capacity).
  const myCapRow = useMemo(() => {
    const rows = capacityRows(capacity.data);
    return rows.find((r) => dpuEntryId(r) === dpuId);
  }, [capacity.data, dpuId]);

  // Pick the best ENI count: placement live count, then DpuDetail.enis,
  // then capacity, then 0.
  const eniLiveCount = placementEnis.length;
  const eniDetailCount = detailEnis.length;
  const eniCapCount = myCapRow ? entryEniUsed(myCapRow) : 0;
  const eniUsed = eniLiveCount > 0 ? eniLiveCount : eniDetailCount > 0 ? eniDetailCount : eniCapCount;
  const eniMax = myCapRow ? entryEniMax(myCapRow) : 64;

  // Merge ENI rows: prefer the live placement (has MAC, IP), fall back to detail.
  const eniRows: EniRow[] = useMemo(() => {
    if (placementEnis.length > 0) return placementEnis;
    return detailEnis.map((e: any) => ({
      name: e?.metadata?.name ?? e?.name ?? "",
      vnet_name: e?.vnet_name ?? "",
      mac_address: e?.mac_address ?? "",
      underlay_ip: e?.underlay_ip ?? "",
      admin_state: e?.admin_state ?? "",
      observed: true,
      haPeers: [],
    }));
  }, [placementEnis, detailEnis]);

  const eniColumns: Column<EniRow>[] = [
    {
      key: "name",
      header: "ENI",
      accessor: (r) => r.name,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {r.name}
        </span>
      ),
    },
    {
      key: "vnet_name",
      header: "Vnet",
      accessor: (r) => r.vnet_name,
      cell: (r) =>
        r.vnet_name ? (
          <button
            type="button"
            className="font-mono text-[color:var(--accent-cyan)] hover:underline"
            onClick={() =>
              navigate(`/vnet/${encodeURIComponent(r.vnet_name)}`)
            }
          >
            {r.vnet_name}
          </button>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
    },
    {
      key: "admin_state",
      header: "State",
      accessor: (r) => r.admin_state,
      cell: (r) => <StatusBadge status={r.admin_state || "UNKNOWN"} />,
      width: "w-28",
    },
    {
      key: "mac_address",
      header: "MAC",
      accessor: (r) => r.mac_address,
      cell: (r) => <span className="font-mono text-xs">{formatMac(r.mac_address)}</span>,
    },
    {
      key: "underlay_ip",
      header: "Underlay IP",
      accessor: (r) => r.underlay_ip,
      cell: (r) => <span className="font-mono text-xs">{formatIp(r.underlay_ip)}</span>,
    },
    {
      key: "haPeers",
      header: "HA Peers",
      accessor: (r) => r.haPeers.join(","),
      cell: (r) =>
        r.haPeers.length === 0 ? (
          <span className="text-[color:var(--text-muted)]">—</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {r.haPeers.map((peer) => (
              <button
                key={peer}
                type="button"
                onClick={() => navigate(`/dpu/${encodeURIComponent(peer)}`)}
                className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono hover:bg-white/10"
              >
                {peer}
              </button>
            ))}
          </div>
        ),
    },
  ];

  const driftColumns: Column<DriftItem>[] = [
    { key: "field", header: "Field", accessor: (r) => r.field, cell: (r) => <code className="text-xs">{r.field}</code> },
    {
      key: "kind",
      header: "Resource",
      accessor: (r) => `${r.target_ref?.namespace}/${r.target_ref?.name}`,
      cell: (r) => (
        <span className="font-mono text-xs">{r.target_ref?.namespace}/{r.target_ref?.name}</span>
      ),
    },
    {
      key: "declared",
      header: "Declared",
      accessor: (r) => r.declared_value,
      cell: (r) => <span className="text-xs font-mono text-[color:var(--accent-green)]">{r.declared_value}</span>,
    },
    {
      key: "observed",
      header: "Observed",
      accessor: (r) => r.observed_value,
      cell: (r) => <span className="text-xs font-mono text-[color:var(--accent-red)]">{r.observed_value}</span>,
    },
  ];

  const enisTab = (
    <div className="space-y-4">
      {dpu.isLoading || placements.isLoading ? (
        <LoadingSkeleton lines={6} />
      ) : eniRows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No ENIs placed on this DPU.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={eniColumns}
            data={eniRows}
            rowKey={(r) => r.name}
            defaultSort={{ key: "name", direction: "asc" }}
            emptyMessage="No ENIs match this filter"
            filterPlaceholder="Filter ENIs…"
          />
        </GlassCard>
      )}
    </div>
  );

  const driftTab = (
    <div className="space-y-4">
      {driftItems.length === 0 ? (
        <GlassCard glow="green">
          <div className="flex flex-col items-center gap-2 py-8 text-[color:var(--text-secondary)]">
            <Activity size={28} className="text-[color:var(--accent-green)]" />
            <p className="text-sm font-medium">No drift on this DPU</p>
          </div>
        </GlassCard>
      ) : (
        <GlassCard className="p-0" glow="amber">
          <DataTable
            columns={driftColumns}
            data={driftItems}
            rowKey={(r) => `${r.target_ref?.namespace}-${r.target_ref?.name}-${r.field}`}
            defaultSort={{ key: "field", direction: "asc" }}
            emptyMessage="No drift items"
            filterPlaceholder="Filter drift…"
          />
        </GlassCard>
      )}
    </div>
  );

  const tabs: TabDef[] = [
    {
      id: "enis",
      label: "ENIs",
      badge: eniRows.length,
      icon: <Layers size={14} />,
      content: enisTab,
    },
    {
      id: "drift",
      label: "Drift",
      badge: driftItems.length,
      icon: <AlertTriangle size={14} />,
      content: driftTab,
    },
  ];

  // Compute capacity values (with fallbacks)
  const routeUsed = myCapRow ? entryRouteUsed(myCapRow) : 0;
  const routeMax = myCapRow ? entryRouteMax(myCapRow) : 1000;
  const aclUsed = myCapRow ? entryAclUsed(myCapRow) : 0;
  const aclMax = myCapRow ? entryAclMax(myCapRow) : 500;
  const flowUsed = myCapRow ? entryFlowUsed(myCapRow) : 0;
  const flowMax = myCapRow ? entryFlowMax(myCapRow) : 10000;

  return (
    <div className="animate-fade-in space-y-6">
      <div className="flex items-center justify-between">
        <PageHeader
          title={
            <span className="flex items-center gap-3">
              <Cpu size={22} className="text-[color:var(--accent-cyan)]" />
              <span className="font-mono">{dpuId}</span>
            </span>
          }
          subtitle={
            <span className="flex items-center gap-2">
              <StatusBadge status={state} />
              <span className="text-[color:var(--text-muted)]">·</span>
              <span className="text-[color:var(--text-secondary)]">
                {eniUsed} ENIs attached
              </span>
              {d?.health?.last_heartbeat && (
                <>
                  <span className="text-[color:var(--text-muted)]">·</span>
                  <span className="text-[color:var(--text-secondary)]">
                    Last heartbeat {timeAgo(d.health.last_heartbeat)}
                  </span>
                </>
              )}
            </span>
          }
        />
        <button
          type="button"
          onClick={() => navigate("/fleet")}
          className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs bg-white/5 hover:bg-white/10 border border-[color:var(--border-subtle)] text-[color:var(--text-secondary)]"
        >
          <ArrowLeft size={12} /> Back to Fleet
        </button>
      </div>

      {/* Capacity */}
      <GlassCard>
        <div className="flex items-center justify-between mb-4">
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em]">
            Capacity
          </p>
          {capacity.isFetching && (
            <span className="text-[10px] text-[color:var(--text-muted)] animate-pulse">
              refreshing…
            </span>
          )}
        </div>
        <div className="grid grid-cols-2 md:grid-cols-4 gap-6">
          <CapacityGauge label="ENIs" used={eniUsed} max={eniMax} />
          <CapacityGauge label="Routes" used={routeUsed} max={routeMax} />
          <CapacityGauge label="ACL Rules" used={aclUsed} max={aclMax} />
          <CapacityGauge label="Flows" used={flowUsed} max={flowMax} />
        </div>
      </GlassCard>

      {/* Health */}
      {d?.health && (
        <GlassCard>
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
            Health
          </p>
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
            <HealthRow label="State" value={<StatusBadge status={d.health?.state ?? state} />} />
            <HealthRow
              label="Last Heartbeat"
              value={
                <span className="font-mono text-xs">
                  {d.health?.last_heartbeat
                    ? new Date(d.health.last_heartbeat).toLocaleTimeString()
                    : "—"}
                </span>
              }
            />
            <HealthRow
              label="Connected At"
              value={
                <span className="font-mono text-xs">
                  {d.health?.connected_at
                    ? new Date(d.health.connected_at).toLocaleTimeString()
                    : "—"}
                </span>
              }
            />
            <HealthRow
              label="Address"
              value={<span className="font-mono text-xs">{d.health?.address ?? "—"}</span>}
            />
          </div>
        </GlassCard>
      )}

      {/* Tabs: ENIs + Drift */}
      <Tabs tabs={tabs} defaultTabId="enis" />
    </div>
  );
}

function HealthRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <span className="block text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider">
        {label}
      </span>
      <div className="mt-1">{value}</div>
    </div>
  );
}