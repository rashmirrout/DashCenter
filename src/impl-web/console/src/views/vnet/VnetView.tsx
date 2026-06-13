import { useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, Globe, Layers, Cpu } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { ErrorState } from "@/components/feedback/ErrorState";
import { LoadingSkeleton } from "@/components/feedback/LoadingSkeleton";
import { DataTable, type Column } from "@/components/data/DataTable";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import {
  useVnetDetail,
  useEniPlacement,
  useEniList,
  useVnetMappings,
} from "@/queries/hooks";
import {
  placementDpuIds,
  placementEniName,
} from "@/lib/api-helpers";
import {
  inferVnetOverlayCidrs,
  inferVnetUnderlayCidrs,
} from "@/lib/cross-resource";
import { formatIp, formatMac } from "@/lib/format";
import type { EniPlacement } from "@/api/types";

/* eslint-disable @typescript-eslint/no-explicit-any */

interface VnetEniRow {
  name: string;
  mac_address: string;
  underlay_ip: string;
  admin_state: string;
  dpus: string[];
}

export default function VnetView() {
  const { vnetName } = useParams<{ vnetName: string }>();
  if (!vnetName) {
    return (
      <div className="animate-fade-in">
        <PageHeader title="Vnet" subtitle="No vnet selected" />
        <ErrorState
          message="No vnet name in the URL. Browse Fleet → Vnets to pick one."
        />
      </div>
    );
  }
  return <VnetDetailPanel vnetName={vnetName} />;
}

function VnetDetailPanel({ vnetName }: { vnetName: string }) {
  const navigate = useNavigate();
  const detail = useVnetDetail(vnetName);
  const placements = useEniPlacement();
  const enis = useEniList("default");
  const mappings = useVnetMappings("default");

  if (detail.isError) {
    return (
      <ErrorState
        message={detail.error?.message ?? "Failed to load vnet"}
        onRetry={() => detail.refetch()}
      />
    );
  }

  const d = detail.data as any;
  const spec = d?.spec ?? d ?? {};
  const vni: number | string = spec?.vni ?? "—";
  const detailEnis: any[] = d?.enis ?? [];

  // Derive ENIs in this vnet from the live REST list.
  const myEnis = useMemo(
    () =>
      (enis.data?.items ?? []).filter((e) => e.vnet_name === vnetName),
    [enis.data, vnetName]
  );

  // Derive vnet-mappings for this vnet.
  const myMappings = useMemo(
    () =>
      (mappings.data?.items ?? []).filter((m) => m.vnet_name === vnetName),
    [mappings.data, vnetName]
  );

  // Address space: vnets in current dashd don't expose `address_space`, so
  // derive from (a) the explicit overlay IPs in vnet-mappings and (b) the
  // underlay /24s used by ENIs in this vnet.
  const overlayCidrs = useMemo(
    () => inferVnetOverlayCidrs(myMappings),
    [myMappings]
  );
  const underlayCidrs = useMemo(
    () => inferVnetUnderlayCidrs(myEnis),
    [myEnis]
  );
  const addrSpace: string[] =
    spec?.address_space ?? d?.address_space ?? overlayCidrs;

  // Filter the placement list to ENIs belonging to this vnet.
  const placementRows: VnetEniRow[] = useMemo(() => {
    const items = (placements.data?.items ?? []) as EniPlacement[];
    const out: VnetEniRow[] = [];
    for (const p of items) {
      if ((p.vnet_name ?? "") !== vnetName) continue;
      out.push({
        name: placementEniName(p),
        mac_address: p.mac_address ?? "",
        underlay_ip: p.underlay_ip ?? "",
        admin_state: p.admin_state ?? "",
        dpus: placementDpuIds(p),
      });
    }
    // Also include any declared ENIs that aren't in placements yet.
    const seen = new Set(out.map((o) => o.name));
    for (const e of myEnis) {
      const name = e.metadata?.name ?? "";
      if (!name || seen.has(name)) continue;
      out.push({
        name,
        mac_address: e.mac_address ?? "",
        underlay_ip: e.underlay_ip ?? "",
        admin_state: e.admin_state ?? "",
        dpus: e.placement_hint_dpu_ids ?? [],
      });
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }, [placements.data, vnetName, myEnis]);

  // Merge: prefer placement rows (have MAC/IP/DPUs); fall back to detail's enis.
  const eniRows: VnetEniRow[] = useMemo(() => {
    if (placementRows.length > 0) return placementRows;
    return detailEnis.map((e: any) => ({
      name: e?.metadata?.name ?? e?.name ?? "",
      mac_address: e?.mac_address ?? "",
      underlay_ip: e?.underlay_ip ?? "",
      admin_state: e?.admin_state ?? "",
      dpus: e?.dpu_id ? [e.dpu_id] : [],
    }));
  }, [placementRows, detailEnis]);

  // Set of unique DPUs hosting this vnet's ENIs.
  const dpuSet = useMemo(() => {
    const s = new Set<string>();
    for (const r of eniRows) for (const id of r.dpus) s.add(id);
    return Array.from(s).sort();
  }, [eniRows]);

  const eniColumns: Column<VnetEniRow>[] = [
    {
      key: "name",
      header: "ENI",
      accessor: (r) => r.name,
      cell: (r) => (
        <button
          type="button"
          onClick={() =>
            navigate(`/eni/${encodeURIComponent("default")}/${encodeURIComponent(r.name)}`)
          }
          className="font-mono text-[color:var(--accent-cyan)] hover:underline"
          title="Open the dedicated ENI detail page"
        >
          {r.name}
        </button>
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
      key: "dpus",
      header: "DPUs",
      accessor: (r) => r.dpus.join(","),
      cell: (r) =>
        r.dpus.length === 0 ? (
          <span className="text-[color:var(--text-muted)]">—</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {r.dpus.map((dpu) => (
              <button
                key={dpu}
                type="button"
                onClick={() => navigate(`/dpu/${encodeURIComponent(dpu)}`)}
                className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono hover:bg-white/10"
              >
                {dpu}
              </button>
            ))}
          </div>
        ),
    },
  ];

  const enisTab = (
    <div className="space-y-4">
      {detail.isLoading || placements.isLoading ? (
        <LoadingSkeleton lines={6} />
      ) : eniRows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No ENIs in this vnet.
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

  const dpuTab = (
    <div className="space-y-4">
      <GlassCard>
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          DPUs hosting {vnetName}
        </p>
        {dpuSet.length === 0 ? (
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No DPUs host this vnet yet.
          </p>
        ) : (
          <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-2">
            {dpuSet.map((dpu) => {
              const enisOnDpu = eniRows.filter((r) =>
                r.dpus.includes(dpu)
              ).length;
              return (
                <button
                  key={dpu}
                  type="button"
                  onClick={() => navigate(`/dpu/${encodeURIComponent(dpu)}`)}
                  className="flex items-center gap-2 px-3 py-2 rounded-md bg-white/[0.02] border border-[color:var(--border-subtle)] hover:bg-white/5 hover:border-[color:var(--accent-cyan)]/40 transition-colors text-left"
                >
                  <Cpu size={14} className="text-[color:var(--accent-cyan)]" />
                  <div className="min-w-0 flex-1">
                    <p className="font-mono text-xs text-[color:var(--text-primary)] truncate">
                      {dpu}
                    </p>
                    <p className="text-[10px] text-[color:var(--text-muted)]">
                      {enisOnDpu} ENI{enisOnDpu === 1 ? "" : "s"}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </GlassCard>
    </div>
  );

  const overviewTab = (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      <GlassCard glow="purple">
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          Spec
        </p>
        <dl className="space-y-2 text-sm">
          <Row label="Name" value={<span className="font-mono">{vnetName}</span>} />
          <Row
            label="VNI"
            value={<span className="font-mono text-[color:var(--accent-cyan)]">{vni}</span>}
          />
          <Row
            label="ENI count"
            value={<span className="font-mono">{eniRows.length}</span>}
          />
          <Row
            label="DPU placements"
            value={<span className="font-mono">{dpuSet.length}</span>}
          />
          {spec?.gw_mac && (
            <Row
              label="Gateway MAC"
              value={<span className="font-mono text-xs">{formatMac(spec.gw_mac)}</span>}
            />
          )}
          {spec?.state && (
            <Row label="State" value={<StatusBadge status={spec.state} />} />
          )}
        </dl>
      </GlassCard>

      <GlassCard>
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          Address Space
        </p>
        <div className="space-y-3">
          <div>
            <p className="text-[10px] text-[color:var(--text-muted)] mb-1">
              Overlay /24s ({overlayCidrs.length})
            </p>
            {overlayCidrs.length === 0 ? (
              <span className="text-[color:var(--text-muted)] text-sm">—</span>
            ) : (
              <ul className="flex flex-wrap gap-1">
                {overlayCidrs.map((c) => (
                  <li
                    key={c}
                    className="px-2 py-0.5 text-xs rounded bg-white/[0.02] border border-[color:var(--border-subtle)] font-mono text-[color:var(--accent-cyan)]"
                  >
                    {c}
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div>
            <p className="text-[10px] text-[color:var(--text-muted)] mb-1">
              Underlay /24s ({underlayCidrs.length})
            </p>
            {underlayCidrs.length === 0 ? (
              <span className="text-[color:var(--text-muted)] text-sm">—</span>
            ) : (
              <ul className="flex flex-wrap gap-1">
                {underlayCidrs.map((c) => (
                  <li
                    key={c}
                    className="px-2 py-0.5 text-xs rounded bg-white/[0.02] border border-[color:var(--border-subtle)] font-mono text-[color:var(--accent-green)]"
                  >
                    {c}
                  </li>
                ))}
              </ul>
            )}
          </div>
          {addrSpace.length > 0 && addrSpace !== overlayCidrs && (
            <div>
              <p className="text-[10px] text-[color:var(--text-muted)] mb-1">
                Declared spec
              </p>
              <ul className="flex flex-wrap gap-1">
                {addrSpace.map((a) => (
                  <li
                    key={a}
                    className="px-2 py-0.5 text-xs rounded bg-white/[0.02] border border-[color:var(--border-subtle)] font-mono"
                  >
                    {a}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <p className="text-[10px] text-[color:var(--text-muted)] italic">
            {myMappings.length} vnet-mapping{myMappings.length === 1 ? "" : "s"} ·{" "}
            derived from live data (vnets don't carry an explicit address-space
            field in the current API).
          </p>
        </div>
      </GlassCard>
    </div>
  );

  const tabs: TabDef[] = [
    {
      id: "overview",
      label: "Overview",
      icon: <Globe size={14} />,
      content: overviewTab,
    },
    {
      id: "enis",
      label: "ENIs",
      badge: eniRows.length,
      icon: <Layers size={14} />,
      content: enisTab,
    },
    {
      id: "dpus",
      label: "DPUs",
      badge: dpuSet.length,
      icon: <Cpu size={14} />,
      content: dpuTab,
    },
  ];

  return (
    <div className="animate-fade-in space-y-6">
      <div className="flex items-center justify-between">
        <PageHeader
          title={
            <span className="flex items-center gap-3">
              <Globe size={22} className="text-[color:var(--accent-purple)]" />
              <span className="font-mono">{vnetName}</span>
            </span>
          }
          subtitle={
            <span className="flex items-center gap-2">
              <span className="text-[color:var(--text-secondary)]">
                VNI {vni}
              </span>
              <span className="text-[color:var(--text-muted)]">·</span>
              <span className="text-[color:var(--text-secondary)]">
                {eniRows.length} ENIs
              </span>
              <span className="text-[color:var(--text-muted)]">·</span>
              <span className="text-[color:var(--text-secondary)]">
                {dpuSet.length} DPU placements
              </span>
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
      <Tabs tabs={tabs} defaultTabId="overview" />
    </div>
  );
}

function Row({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div className="flex justify-between items-center">
      <dt className="text-[color:var(--text-secondary)]">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}