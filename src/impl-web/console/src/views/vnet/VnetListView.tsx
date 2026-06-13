import { useMemo } from "react";
import { useNavigate } from "react-router-dom";
import { Globe } from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { DataTable, type Column } from "@/components/data/DataTable";

import {
  useVnetList,
  useEniList,
  useEniPlacement,
  useVnetMappings,
} from "@/queries/hooks";
import { placementDpuIds, placementEniName } from "@/lib/api-helpers";
import type { EniPlacement, EniSpec, VnetSpec, VnetMappingSpec } from "@/api/types";

/**
 * VnetListView — top-level browse for all Vnets in the default namespace.
 *
 * Mirrors the ENI list view pattern: a click navigates to the existing
 * `/vnet/:name` detail page (`VnetView`). The list pre-joins three
 * sibling queries (vnets, enis, placements, vnet-mappings) so each
 * row can answer "which DPUs serve this vnet?" and "how many ENIs +
 * mappings does it host?" without a second round-trip.
 *
 * Data sources (all from /api/v1/* via react-query hooks):
 *   - useVnetList            : the vnets themselves
 *   - useEniList             : enis per vnet (group by vnet_name)
 *   - useEniPlacement        : eni → dpu mapping (HA-aware)
 *   - useVnetMappings        : per-vnet mapping counts
 *
 * The page intentionally has no BFF call of its own — VnetView keeps
 * the heavy joining server-side, while VnetListView is a thin client
 * fan-in over already-cached lists, which gives consistent reads
 * within the same react-query cache window.
 */
export default function VnetListView() {
  const navigate = useNavigate();
  const vnets = useVnetList("default");
  const enis = useEniList("default");
  const placements = useEniPlacement();
  const mappings = useVnetMappings("default");

  if (vnets.isError) {
    return (
      <ErrorState
        message={vnets.error?.message ?? "Failed to load Vnets"}
        onRetry={() => vnets.refetch()}
      />
    );
  }

  // Pre-join: build dpu-id set per vnet by walking enis + placements.
  // We collect three indexes once, then read them per row in O(1).
  const rows: VnetListRow[] = useMemo(() => {
    const eniItems = (enis.data?.items ?? []) as EniSpec[];
    const placementItems = (placements.data?.items ?? []) as EniPlacement[];
    const mappingItems = (mappings.data?.items ?? []) as VnetMappingSpec[];

    // eni_name → vnet_name
    const eniToVnet = new Map<string, string>();
    for (const e of eniItems) {
      const n = e.metadata?.name ?? "";
      if (n && e.vnet_name) eniToVnet.set(n, e.vnet_name);
    }

    // vnet_name → eni count
    const eniCountByVnet = new Map<string, number>();
    for (const v of eniToVnet.values()) {
      eniCountByVnet.set(v, (eniCountByVnet.get(v) ?? 0) + 1);
    }

    // vnet_name → Set<dpu_id> derived via placements
    const dpusByVnet = new Map<string, Set<string>>();
    for (const p of placementItems) {
      const eniName = placementEniName(p);
      // Trust the placement's vnet_name when present, else fall back to the
      // ENI spec map. dashd's placement record carries vnet_name for
      // exactly this kind of lookup.
      const vnet = p.vnet_name ?? eniToVnet.get(eniName) ?? "";
      if (!vnet) continue;
      const set = dpusByVnet.get(vnet) ?? new Set<string>();
      for (const d of placementDpuIds(p)) set.add(d);
      dpusByVnet.set(vnet, set);
    }

    // vnet_name → mapping count
    const mappingCountByVnet = new Map<string, number>();
    for (const m of mappingItems) {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const raw = m as any;
      const vn: string = raw.vnet_name ?? raw.metadata?.labels?.vnet ?? "";
      if (!vn) continue;
      mappingCountByVnet.set(vn, (mappingCountByVnet.get(vn) ?? 0) + 1);
    }

    return (vnets.data?.items ?? []).map((v) => projectVnetRow(
      v,
      eniCountByVnet.get(v.metadata?.name ?? "") ?? 0,
      Array.from(dpusByVnet.get(v.metadata?.name ?? "") ?? new Set<string>()).sort(),
      mappingCountByVnet.get(v.metadata?.name ?? "") ?? 0,
    )).sort((a, b) => a.name.localeCompare(b.name));
  }, [vnets.data, enis.data, placements.data, mappings.data]);

  const cols: Column<VnetListRow>[] = [
    {
      key: "name",
      header: "Vnet",
      accessor: (r) => r.name,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {r.name}
        </span>
      ),
    },
    {
      key: "vni",
      header: "VNI",
      accessor: (r) => r.vni,
      cell: (r) => (
        <span className="font-mono text-xs text-[color:var(--accent-cyan)]">
          {r.vni}
        </span>
      ),
      width: "w-24",
    },
    {
      key: "tenant",
      header: "Tenant",
      accessor: (r) => r.tenant ?? "",
      cell: (r) =>
        r.tenant ? (
          <span className="px-1.5 py-0.5 text-[10px] rounded font-mono bg-white/5">
            {r.tenant}
          </span>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
      width: "w-28",
    },
    {
      key: "tier",
      header: "Tier",
      accessor: (r) => r.tier ?? "",
      cell: (r) =>
        r.tier ? (
          <span className="px-1.5 py-0.5 text-[10px] rounded font-mono bg-white/5">
            {r.tier}
          </span>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
      width: "w-24",
    },
    {
      key: "eni_count",
      header: "ENIs",
      accessor: (r) => r.eni_count,
      cell: (r) => (
        <span className="font-mono text-xs text-[color:var(--accent-purple)]">
          {r.eni_count}
        </span>
      ),
      width: "w-20",
    },
    {
      key: "mapping_count",
      header: "Mappings",
      accessor: (r) => r.mapping_count,
      cell: (r) => (
        <span className="font-mono text-xs text-[color:var(--text-secondary)]">
          {r.mapping_count}
        </span>
      ),
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
          </div>
        ),
    },
  ];

  const loading = vnets.isLoading || enis.isLoading || placements.isLoading || mappings.isLoading;

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            <Globe size={22} className="text-[color:var(--accent-purple)]" />
            Vnets
          </span>
        }
        subtitle={`${rows.length} Vnet${rows.length === 1 ? "" : "s"} · click any row to open the dedicated detail page`}
      />
      {loading ? (
        <CardSkeleton />
      ) : rows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No Vnets declared.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={cols}
            data={rows}
            rowKey={(r) => r.name}
            onRowClick={(r) => navigate(`/vnet/${encodeURIComponent(r.name)}`)}
            defaultSort={{ key: "name", direction: "asc" }}
            filterPlaceholder="Filter vnets…"
            emptyMessage="No vnets match this filter"
          />
        </GlassCard>
      )}
    </div>
  );
}

/* ── Helpers ──────────────────────────────────────────────── */

interface VnetListRow {
  name: string;
  vni: number;
  tenant: string;
  tier: string;
  eni_count: number;
  mapping_count: number;
  dpus: string[];
}

/**
 * Project a Vnet wire object into the row shape the table consumes.
 * Pulls metadata.labels for tenant/tier when available; both dashd
 * implementations (the Go server and the Rust simulator) attach
 * these labels in the test fixtures.
 */
function projectVnetRow(
  v: VnetSpec,
  eni_count: number,
  dpus: string[],
  mapping_count: number,
): VnetListRow {
  const labels = v.metadata?.labels ?? {};
  return {
    name: v.metadata?.name ?? "",
    vni: v.vni ?? 0,
    tenant: labels.tenant ?? "",
    tier: labels.tier ?? "",
    eni_count,
    mapping_count,
    dpus,
  };
}