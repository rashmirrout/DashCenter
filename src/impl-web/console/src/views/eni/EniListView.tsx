import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { Plug } from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { DataTable, type Column } from "@/components/data/DataTable";

import { useEniList, useEniPlacement } from "@/queries/hooks";
import { formatIp, formatMac } from "@/lib/format";
import {
  placementDpuIds,
  placementEniName,
} from "@/lib/api-helpers";
import type { EniPlacement, EniSpec } from "@/api/types";

/**
 * EniListView — top-level browse for all ENIs in the default namespace.
 *
 * Acts as the entry point for the dedicated ENI detail page.
 * Clicking any row navigates to `/eni/:namespace/:name` which is the
 * comprehensive "everything about this ENI" view backed by the BFF
 * aggregator at /api/console/eni/{ns}/{name}/detail.
 */
export default function EniListView() {
  const navigate = useNavigate();
  const enis = useEniList("default");
  const placements = useEniPlacement();

  if (enis.isError) {
    return (
      <ErrorState
        message={enis.error?.message ?? "Failed to load ENIs"}
        onRetry={() => enis.refetch()}
      />
    );
  }

  const rows: EniListRow[] = useMemo(() => {
    const placementItems = (placements.data?.items ?? []) as EniPlacement[];
    const eniByName = new Map<string, EniSpec>();
    for (const e of enis.data?.items ?? []) {
      const n = e.metadata?.name ?? "";
      if (n) eniByName.set(n, e);
    }
    const seen = new Set<string>();
    const out: EniListRow[] = [];

    // Placements first (authoritative for which DPUs are hosting the ENI).
    for (const p of placementItems) {
      const name = placementEniName(p);
      if (!name) continue;
      seen.add(name);
      const eni = eniByName.get(name);
      const dpus = placementDpuIds(p);
      out.push({
        namespace: eni?.metadata?.namespace ?? "default",
        name,
        vnet_name: p.vnet_name ?? eni?.vnet_name ?? "",
        mac_address: p.mac_address ?? eni?.mac_address ?? "",
        underlay_ip: p.underlay_ip ?? eni?.underlay_ip ?? "",
        admin_state: p.admin_state ?? eni?.admin_state ?? "",
        dpus,
        haActiveActive: dpus.length > 1,
      });
    }
    // Then any declared ENIs not yet in placements (admin-only).
    for (const e of enis.data?.items ?? []) {
      const name = e.metadata?.name ?? "";
      if (!name || seen.has(name)) continue;
      out.push({
        namespace: e.metadata?.namespace ?? "default",
        name,
        vnet_name: e.vnet_name ?? "",
        mac_address: e.mac_address ?? "",
        underlay_ip: e.underlay_ip ?? "",
        admin_state: e.admin_state ?? "",
        dpus: e.placement_hint_dpu_ids ?? [],
        haActiveActive: false,
      });
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }, [enis.data, placements.data]);

  const cols: Column<EniListRow>[] = [
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
      key: "namespace",
      header: "Namespace",
      accessor: (r) => r.namespace,
      cell: (r) => (
        <span className="font-mono text-xs text-[color:var(--text-secondary)]">
          {r.namespace}
        </span>
      ),
      width: "w-32",
    },
    {
      key: "vnet",
      header: "Vnet",
      accessor: (r) => r.vnet_name,
      cell: (r) =>
        r.vnet_name ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              navigate(`/vnet/${encodeURIComponent(r.vnet_name)}`);
            }}
            className="font-mono text-xs text-[color:var(--accent-purple)] hover:underline"
          >
            {r.vnet_name}
          </button>
        ) : (
          <span className="text-[color:var(--text-muted)] text-xs">—</span>
        ),
    },
    {
      key: "admin_state",
      header: "State",
      accessor: (r) => r.admin_state,
      cell: (r) => <StatusBadge status={r.admin_state || "UNKNOWN"} />,
      width: "w-24",
    },
    {
      key: "dpus",
      header: "DPU(s)",
      accessor: (r) => r.dpus.join(","),
      cell: (r) =>
        r.dpus.length === 0 ? (
          <span className="text-[color:var(--text-muted)] text-xs">—</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {r.dpus.map((d) => (
              <button
                key={d}
                type="button"
                onClick={(e) => {
                  e.stopPropagation();
                  navigate(`/dpu/${encodeURIComponent(d)}`);
                }}
                className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono hover:bg-white/10 hover:text-[color:var(--accent-cyan)]"
              >
                {d}
              </button>
            ))}
            {r.haActiveActive && (
              <span
                title="Active-active across multiple DPUs"
                className="text-[10px] px-1.5 py-0.5 rounded-full bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] font-mono"
              >
                HA
              </span>
            )}
          </div>
        ),
    },
    {
      key: "mac_address",
      header: "MAC",
      accessor: (r) => r.mac_address,
      cell: (r) => (
        <span className="font-mono text-xs">{formatMac(r.mac_address)}</span>
      ),
    },
    {
      key: "underlay_ip",
      header: "Underlay IP",
      accessor: (r) => r.underlay_ip,
      cell: (r) => (
        <span className="font-mono text-xs">{formatIp(r.underlay_ip)}</span>
      ),
    },
  ];

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            <Plug size={22} className="text-[color:var(--accent-cyan)]" />
            ENIs
          </span>
        }
        subtitle={`${rows.length} ENI${rows.length === 1 ? "" : "s"} · click any row to open the dedicated detail page`}
      />
      {enis.isLoading || placements.isLoading ? (
        <CardSkeleton />
      ) : rows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No ENIs declared.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={cols}
            data={rows}
            rowKey={(r) => `${r.namespace}/${r.name}`}
            onRowClick={(r) =>
              navigate(
                `/eni/${encodeURIComponent(r.namespace)}/${encodeURIComponent(r.name)}`
              )
            }
            defaultSort={{ key: "name", direction: "asc" }}
            filterPlaceholder="Filter ENIs…"
            emptyMessage="No ENIs match this filter"
          />
        </GlassCard>
      )}
    </div>
  );
}

interface EniListRow {
  namespace: string;
  name: string;
  vnet_name: string;
  mac_address: string;
  underlay_ip: string;
  admin_state: string;
  dpus: string[];
  haActiveActive: boolean;
}