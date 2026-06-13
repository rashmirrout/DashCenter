import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Waypoints, Cpu, Plug, Globe } from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { DataTable, type Column } from "@/components/data/DataTable";
import {
  Drawer,
  DrawerSection,
  KeyValueRow,
  LabelChips,
} from "@/components/feedback/Drawer";

import {
  useVnetMappings,
  useEniList,
  useEniPlacement,
} from "@/queries/hooks";
import { placementDpuIds, placementEniName } from "@/lib/api-helpers";
import { formatIp, formatMac } from "@/lib/format";
import type {
  EniPlacement,
  EniSpec,
  VnetMappingSpec,
} from "@/api/types";

/**
 * MappingsView — top-level fleet-wide browser for vnet-mappings.
 *
 * For each mapping the page also derives:
 *  - **which ENI it serves** — by joining (underlay_ip + mac_address)
 *    against the ENI list. Mirrors the same join the Go BFF does for
 *    the dedicated ENI detail page (overlayIPFromMappings).
 *  - **which DPU(s) host that ENI** — by walking the ENI placements.
 *
 * Clicking a row opens a right-side `Drawer` with the full mapping
 * record (overlay IP, underlay IP, MAC, action, params), the matched
 * ENI link, and the DPU chip(s) hosting that ENI. The raw JSON spec
 * is included at the bottom so operators have an authoritative view.
 */
export default function MappingsView() {
  const navigate = useNavigate();
  const mappings = useVnetMappings("default");
  const enis = useEniList("default");
  const placements = useEniPlacement();
  const [selectedKey, setSelectedKey] = useState<string | null>(null);

  if (mappings.isError) {
    return (
      <ErrorState
        message={mappings.error?.message ?? "Failed to load vnet-mappings"}
        onRetry={() => mappings.refetch()}
      />
    );
  }

  // ── Build cross-resource indexes once per data refresh ────
  // 1. (underlay_ip|mac) → ENI name        — to find which ENI a mapping serves.
  // 2. eni_name          → []dpu_id        — to find which DPUs host that ENI.
  const eniNameByEndpoint = useMemo(() => {
    const m = new Map<string, string>();
    for (const e of (enis.data?.items ?? []) as EniSpec[]) {
      const name = e.metadata?.name ?? "";
      const underlay = (e.underlay_ip ?? "").trim();
      const mac = (e.mac_address ?? "").trim().toLowerCase();
      if (name && underlay && mac) m.set(`${underlay}|${mac}`, name);
    }
    return m;
  }, [enis.data]);

  const dpusByEni = useMemo(() => {
    const m = new Map<string, string[]>();
    for (const p of (placements.data?.items ?? []) as EniPlacement[]) {
      const n = placementEniName(p);
      if (!n) continue;
      const dpus = placementDpuIds(p);
      if (dpus.length > 0) m.set(n, dpus);
    }
    return m;
  }, [placements.data]);

  const rows: MappingListRow[] = useMemo(
    () =>
      ((mappings.data?.items ?? []) as VnetMappingSpec[]).map((m) => {
        const r = projectMappingRow(m);
        const eniName =
          eniNameByEndpoint.get(`${r.underlay_ip}|${r.mac_address.toLowerCase()}`) ?? "";
        const dpus = eniName ? dpusByEni.get(eniName) ?? [] : [];
        return { ...r, eni_name: eniName, dpus };
      }),
    [mappings.data, eniNameByEndpoint, dpusByEni]
  );

  const cols: Column<MappingListRow>[] = [
    {
      key: "name",
      header: "Mapping",
      accessor: (r) => r.name,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">{r.name}</span>
      ),
    },
    {
      key: "vnet_name",
      header: "Vnet",
      accessor: (r) => r.vnet_name,
      cell: (r) => (
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
      ),
    },
    {
      key: "overlay_ip",
      header: "Overlay IP",
      accessor: (r) => r.overlay_ip,
      cell: (r) => (
        <span className="font-mono text-xs text-[color:var(--accent-purple)]">
          {formatIp(r.overlay_ip)}
        </span>
      ),
    },
    {
      key: "underlay_ip",
      header: "→ Underlay IP",
      accessor: (r) => r.underlay_ip,
      cell: (r) => <span className="font-mono text-xs">{formatIp(r.underlay_ip)}</span>,
    },
    {
      key: "mac_address",
      header: "MAC",
      accessor: (r) => r.mac_address,
      cell: (r) => <span className="font-mono text-xs">{formatMac(r.mac_address)}</span>,
    },
    {
      key: "action",
      header: "Action",
      accessor: (r) => r.action,
      cell: (r) => (
        <span className="px-1.5 py-0.5 text-[10px] rounded font-mono bg-white/5">
          {r.action}
        </span>
      ),
      width: "w-32",
    },
    {
      key: "eni_name",
      header: "ENI",
      accessor: (r) => r.eni_name,
      cell: (r) =>
        r.eni_name ? (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              navigate(
                `/eni/${encodeURIComponent("default")}/${encodeURIComponent(r.eni_name)}`
              );
            }}
            className="font-mono text-xs text-[color:var(--accent-cyan)] hover:underline"
          >
            {r.eni_name}
          </button>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
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

  const loading = mappings.isLoading || enis.isLoading || placements.isLoading;
  const selected = selectedKey ? rows.find((r) => r.name === selectedKey) ?? null : null;

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title={
          <span className="flex items-center gap-3">
            <Waypoints size={22} className="text-[color:var(--accent-cyan)]" />
            Vnet Mappings
          </span>
        }
        subtitle={`${rows.length} mapping${rows.length === 1 ? "" : "s"} · click any row to see full record + DPU association`}
      />

      {loading ? (
        <CardSkeleton />
      ) : rows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No vnet-mappings declared.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={cols}
            data={rows}
            rowKey={(r) => r.name}
            onRowClick={(r) => setSelectedKey(r.name)}
            defaultSort={{ key: "name", direction: "asc" }}
            filterPlaceholder="Filter mappings…"
            emptyMessage="No mappings match this filter"
          />
        </GlassCard>
      )}

      <Drawer
        open={selected != null}
        onClose={() => setSelectedKey(null)}
        title={
          selected ? (
            <span className="flex items-center gap-2">
              <Waypoints size={16} className="text-[color:var(--accent-cyan)]" />
              <span className="font-mono">{selected.name}</span>
            </span>
          ) : (
            ""
          )
        }
        subtitle={
          selected ? (
            <span>
              vnet <span className="font-mono">{selected.vnet_name}</span>
            </span>
          ) : undefined
        }
        width="lg"
      >
        {selected && <MappingDrawerContent row={selected} navigate={navigate} />}
      </Drawer>
    </div>
  );
}

/* ── Drawer body ──────────────────────────────────────────── */

function MappingDrawerContent({
  row,
  navigate,
}: {
  row: MappingListRow;
  navigate: ReturnType<typeof useNavigate>;
}) {
  return (
    <>
      <DrawerSection title="Endpoint">
        <KeyValueRow
          label="Overlay IP"
          value={
            <span className="font-mono text-[color:var(--accent-purple)]">
              {formatIp(row.overlay_ip)}
            </span>
          }
        />
        <KeyValueRow
          label="Underlay IP"
          value={<span className="font-mono">{formatIp(row.underlay_ip)}</span>}
        />
        <KeyValueRow
          label="MAC"
          value={<span className="font-mono">{formatMac(row.mac_address)}</span>}
        />
        <KeyValueRow
          label="Action"
          value={
            <span className="px-1.5 py-0.5 text-[10px] rounded font-mono bg-white/5">
              {row.action}
            </span>
          }
        />
        {row.tunnel && (
          <KeyValueRow
            label="Tunnel"
            value={
              <button
                type="button"
                onClick={() => navigate("/tunnels")}
                className="font-mono text-[color:var(--accent-cyan)] hover:underline"
              >
                {row.tunnel}
              </button>
            }
          />
        )}
      </DrawerSection>

      <DrawerSection title="Vnet">
        <KeyValueRow
          label="Name"
          value={
            <button
              type="button"
              onClick={() => navigate(`/vnet/${encodeURIComponent(row.vnet_name)}`)}
              className="inline-flex items-center gap-1 font-mono text-[color:var(--accent-purple)] hover:underline"
            >
              <Globe size={12} />
              {row.vnet_name}
            </button>
          }
        />
      </DrawerSection>

      <DrawerSection title="Bound ENI">
        {row.eni_name ? (
          <KeyValueRow
            label="ENI"
            value={
              <button
                type="button"
                onClick={() =>
                  navigate(
                    `/eni/${encodeURIComponent("default")}/${encodeURIComponent(row.eni_name)}`
                  )
                }
                className="inline-flex items-center gap-1 font-mono text-[color:var(--accent-cyan)] hover:underline"
              >
                <Plug size={12} />
                {row.eni_name}
              </button>
            }
          />
        ) : (
          <p className="text-xs text-[color:var(--text-muted)] italic py-1">
            No ENI in this namespace matches{" "}
            <span className="font-mono">
              {row.underlay_ip} · {row.mac_address}
            </span>
            . The mapping is orphaned or points at a peer-vnet endpoint.
          </p>
        )}
      </DrawerSection>

      <DrawerSection title="DPU(s) hosting the ENI">
        {row.dpus.length === 0 ? (
          <p className="text-xs text-[color:var(--text-muted)] italic py-1">
            No DPU placement found.
          </p>
        ) : (
          <div className="flex flex-wrap gap-1.5">
            {row.dpus.map((d) => (
              <button
                key={d}
                type="button"
                onClick={() => navigate(`/dpu/${encodeURIComponent(d)}`)}
                className="inline-flex items-center gap-1.5 px-2 py-1 rounded-md bg-white/[0.02] border border-[color:var(--border-subtle)] hover:bg-white/5 hover:border-[color:var(--accent-cyan)]/40 transition-colors"
              >
                <Cpu size={12} className="text-[color:var(--accent-cyan)]" />
                <span className="font-mono text-xs">{d}</span>
              </button>
            ))}
          </div>
        )}
      </DrawerSection>

      {row.labels && Object.keys(row.labels).length > 0 && (
        <DrawerSection title="Labels">
          <LabelChips labels={row.labels} />
        </DrawerSection>
      )}

      <DrawerSection title="Raw spec">
        <pre className="text-[10px] font-mono bg-[color:var(--bg-primary)] border border-[color:var(--border-subtle)] rounded p-2 overflow-auto max-h-64">
{JSON.stringify(row.raw, null, 2)}
        </pre>
      </DrawerSection>
    </>
  );
}

/* ── Helpers ──────────────────────────────────────────────── */

interface MappingListRow {
  name: string;
  vnet_name: string;
  overlay_ip: string;
  underlay_ip: string;
  mac_address: string;
  action: string;
  tunnel: string;
  labels: Record<string, string> | undefined;
  /** Derived: which ENI this mapping serves (joined via underlay_ip+mac). */
  eni_name: string;
  /** Derived: which DPUs host that ENI. */
  dpus: string[];
  /** Raw spec for the Drawer "Raw spec" section. */
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  raw: any;
}

function projectMappingRow(m: VnetMappingSpec): Omit<MappingListRow, "eni_name" | "dpus"> {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const raw = m as any;
  return {
    name: raw.metadata?.name ?? raw.name ?? "",
    vnet_name: raw.vnet_name ?? "",
    overlay_ip: raw.ip_address ?? raw.overlay_ip ?? "",
    underlay_ip: raw.underlay_ip ?? "",
    mac_address: raw.mac_address ?? "",
    action: raw.action ?? "vnet_encap",
    tunnel: raw.params?.tunnel ?? "",
    labels: raw.metadata?.labels ?? raw.labels,
    raw,
  };
}