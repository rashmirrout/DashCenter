import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  Cpu,
  Globe,
  Layers,
  Shield,
  Route as RouteIcon,
  Cable,
  ExternalLink,
} from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import { DataTable, type Column } from "@/components/data/DataTable";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import {
  Drawer,
  DrawerSection,
  KeyValueRow,
  LabelChips,
} from "@/components/feedback/Drawer";
import {
  useFleetSummary,
  useVnetList,
  useCapacityStats,
  useEniList,
  useEniPlacement,
  useAclPolicies,
  useRoutePolicies,
  useServiceTunnels,
  useVnetMappings,
} from "@/queries/hooks";
import {
  capacityRows,
  dpuEntryId,
  entryEniMax,
  entryEniUsed,
  entryRouteMax,
  entryRouteUsed,
  fleetDpuCount,
  fleetEniCount,
  fleetVnetCount,
  placementDpuIds,
  placementEniName,
} from "@/lib/api-helpers";
import {
  buildCrossResourceIndex,
  dpuCrossCounts,
  eniCrossCounts,
  inferVnetUnderlayCidrs,
} from "@/lib/cross-resource";
import { timeAgo, formatMac, formatIp } from "@/lib/format";
import type {
  EniPlacement,
  EniSpec,
  VnetSpec,
} from "@/api/types";

/* eslint-disable @typescript-eslint/no-explicit-any */

interface DpuRow {
  id: string;
  state: string;
  lastSeen: string | undefined;
  eniUsed: number;
  eniMax: number;
  routeUsed: number;
  routeMax: number;
  vnetCount: number;
  aclCount: number;
  tunnelCount: number;
}

interface EniRow {
  name: string;
  vnet_name: string;
  mac_address: string;
  underlay_ip: string;
  admin_state: string;
  dpus: string[];
  aclCount: number;
  routeCount: number;
  tunnelCount: number;
}

interface VnetRow {
  name: string;
  vni: number | undefined;
  namespace: string;
  eniCount: number;
  dpuCount: number;
  underlayCidrs: string[];
  labels: Record<string, string>;
}

export default function FleetView() {
  const navigate = useNavigate();
  const fleet = useFleetSummary();
  const vnets = useVnetList("default");
  const capacity = useCapacityStats();
  const enis = useEniList("default");
  const placements = useEniPlacement();
  const acls = useAclPolicies("default");
  const routes = useRoutePolicies("default");
  const tunnels = useServiceTunnels("default");
  const mappings = useVnetMappings("default");
  const [drawerVnet, setDrawerVnet] = useState<VnetSpec | null>(null);

  if (fleet.isError) {
    return (
      <ErrorState message={fleet.error.message} onRetry={() => fleet.refetch()} />
    );
  }

  const idx = useMemo(
    () =>
      buildCrossResourceIndex({
        enis: enis.data?.items,
        placements: placements.data?.items,
        acls: acls.data?.items,
        routes: routes.data?.items,
        tunnels: tunnels.data?.items,
        vnetMappings: mappings.data?.items,
      }),
    [
      enis.data,
      placements.data,
      acls.data,
      routes.data,
      tunnels.data,
      mappings.data,
    ]
  );

  /* ── DPU rows ─────────────────────────────────────────── */
  const dpuRows: DpuRow[] = useMemo(() => {
    const fs = fleet.data;
    const rows = capacityRows(capacity.data);
    const byId = new Map<string, (typeof rows)[number]>();
    for (const r of rows) byId.set(dpuEntryId(r), r);
    const dpus = fs?.dpus ?? [];
    return dpus.map((d) => {
      const cap = byId.get(d.id);
      const cross = dpuCrossCounts(idx, d.id);
      return {
        id: d.id,
        state: d.state,
        lastSeen: d.last_seen,
        eniUsed: cross.eniCount || (cap ? entryEniUsed(cap) : 0),
        eniMax: cap ? entryEniMax(cap) : 0,
        routeUsed: cross.routeCount || (cap ? entryRouteUsed(cap) : 0),
        routeMax: cap ? entryRouteMax(cap) : 0,
        vnetCount: cross.vnetCount,
        aclCount: cross.aclCount,
        tunnelCount: cross.tunnelCount,
      };
    });
  }, [fleet.data, capacity.data, idx]);

  /* ── ENI rows (cross-referenced) ──────────────────────── */
  const eniRows: EniRow[] = useMemo(() => {
    const placementItems = (placements.data?.items ?? []) as EniPlacement[];
    const eniByName = new Map<string, EniSpec>();
    for (const e of enis.data?.items ?? []) {
      const name = e.metadata?.name ?? "";
      if (name) eniByName.set(name, e);
    }
    // Build rows from placements (authoritative for runtime placement).
    const seen = new Set<string>();
    const out: EniRow[] = [];
    for (const p of placementItems) {
      const name = placementEniName(p);
      if (!name) continue;
      seen.add(name);
      const eni = eniByName.get(name);
      const dpus = placementDpuIds(p);
      const cross = eniCrossCounts(idx, name);
      out.push({
        name,
        vnet_name: p.vnet_name ?? eni?.vnet_name ?? "",
        mac_address: p.mac_address ?? eni?.mac_address ?? "",
        underlay_ip: p.underlay_ip ?? eni?.underlay_ip ?? "",
        admin_state: p.admin_state ?? eni?.admin_state ?? "",
        dpus,
        aclCount: cross.aclCount,
        routeCount: cross.routeCount,
        tunnelCount: cross.tunnelCount,
      });
    }
    // Add any declared ENIs that don't appear in placements (admin-only / no DPU yet).
    for (const e of enis.data?.items ?? []) {
      const name = e.metadata?.name ?? "";
      if (!name || seen.has(name)) continue;
      const cross = eniCrossCounts(idx, name);
      out.push({
        name,
        vnet_name: e.vnet_name ?? "",
        mac_address: e.mac_address ?? "",
        underlay_ip: e.underlay_ip ?? "",
        admin_state: e.admin_state ?? "",
        dpus: e.placement_hint_dpu_ids ?? [],
        aclCount: cross.aclCount,
        routeCount: cross.routeCount,
        tunnelCount: cross.tunnelCount,
      });
    }
    return out.sort((a, b) => a.name.localeCompare(b.name));
  }, [enis.data, placements.data, idx]);

  /* ── Vnet rows ────────────────────────────────────────── */
  const vnetRows: VnetRow[] = useMemo(() => {
    const items = vnets.data?.items ?? [];
    return items.map((v) => {
      const name = v.metadata?.name ?? "";
      const enisInVnet = (enis.data?.items ?? []).filter(
        (e) => e.vnet_name === name
      );
      const dpuSet = new Set<string>();
      for (const e of enisInVnet) {
        // Use placement when available; fall back to hints.
        const pl = (placements.data?.items ?? []).find(
          (p) => placementEniName(p) === (e.metadata?.name ?? "")
        );
        const dpus = pl
          ? placementDpuIds(pl)
          : e.placement_hint_dpu_ids ?? [];
        for (const d of dpus) dpuSet.add(d);
      }
      return {
        name,
        vni: v.vni,
        namespace: v.metadata?.namespace ?? "default",
        eniCount: enisInVnet.length,
        dpuCount: dpuSet.size,
        underlayCidrs: inferVnetUnderlayCidrs(enisInVnet),
        labels: v.metadata?.labels ?? {},
      };
    });
  }, [vnets.data, enis.data, placements.data]);

  /* ── Columns ──────────────────────────────────────────── */

  // Clickable count cell helper
  function CountLink({
    count,
    onClick,
    color,
  }: {
    count: number;
    onClick: () => void;
    color?: string;
  }) {
    if (count === 0)
      return <span className="text-[color:var(--text-muted)] text-xs">—</span>;
    return (
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onClick();
        }}
        className={cnHover(color)}
      >
        {count}
      </button>
    );
  }

  const dpuColumns: Column<DpuRow>[] = [
    {
      key: "id",
      header: "DPU ID",
      accessor: (r) => r.id,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">{r.id}</span>
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
      key: "enis",
      header: "ENIs",
      accessor: (r) => r.eniUsed,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.eniUsed}
          color="cyan"
          onClick={() => navigate(`/dpu/${encodeURIComponent(r.id)}`)}
        />
      ),
    },
    {
      key: "vnets",
      header: "Vnets",
      accessor: (r) => r.vnetCount,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.vnetCount}
          color="purple"
          onClick={() => navigate(`/dpu/${encodeURIComponent(r.id)}`)}
        />
      ),
    },
    {
      key: "routes",
      header: "Routes",
      accessor: (r) => r.routeUsed,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.routeUsed}
          color="green"
          onClick={() => navigate(`/routing`)}
        />
      ),
    },
    {
      key: "tunnels",
      header: "Tunnels",
      accessor: (r) => r.tunnelCount,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.tunnelCount}
          color="amber"
          onClick={() => navigate(`/tunnels`)}
        />
      ),
    },
    {
      key: "acls",
      header: "Policies",
      accessor: (r) => r.aclCount,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.aclCount}
          color="cyan"
          onClick={() => navigate(`/policies`)}
        />
      ),
    },
    {
      key: "lastSeen",
      header: "Health",
      accessor: (r) => r.lastSeen ?? "",
      cell: (r) => (
        <span className="text-[color:var(--text-muted)] text-xs">
          {timeAgo(r.lastSeen)}
        </span>
      ),
    },
  ];

  const eniColumns: Column<EniRow>[] = [
    {
      key: "name",
      header: "ENI",
      accessor: (r) => r.name,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">{r.name}</span>
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
                onClick={() => navigate(`/dpu/${encodeURIComponent(d)}`)}
                className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono hover:bg-white/10 hover:text-[color:var(--accent-cyan)]"
              >
                {d}
              </button>
            ))}
          </div>
        ),
    },
    {
      key: "vnet",
      header: "Vnet",
      accessor: (r) => r.vnet_name,
      cell: (r) =>
        r.vnet_name ? (
          <button
            type="button"
            onClick={() =>
              navigate(`/vnet/${encodeURIComponent(r.vnet_name)}`)
            }
            className="font-mono text-[color:var(--accent-purple)] hover:underline text-xs"
          >
            {r.vnet_name}
          </button>
        ) : (
          <span className="text-[color:var(--text-muted)] text-xs">—</span>
        ),
    },
    {
      key: "acls",
      header: "Policies",
      accessor: (r) => r.aclCount,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.aclCount}
          color="cyan"
          onClick={() => navigate(`/policies`)}
        />
      ),
    },
    {
      key: "routes",
      header: "Routes",
      accessor: (r) => r.routeCount,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.routeCount}
          color="green"
          onClick={() => navigate(`/routing`)}
        />
      ),
    },
    {
      key: "tunnels",
      header: "Tunnels",
      accessor: (r) => r.tunnelCount,
      align: "right",
      cell: (r) => (
        <CountLink
          count={r.tunnelCount}
          color="amber"
          onClick={() => navigate(`/tunnels`)}
        />
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
      key: "mac",
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
  ];

  const vnetColumns: Column<VnetRow>[] = [
    {
      key: "name",
      header: "Name",
      accessor: (v) => v.name,
      cell: (v) => (
        <span className="font-mono text-[color:var(--text-primary)]">{v.name}</span>
      ),
    },
    {
      key: "vni",
      header: "VNI",
      accessor: (v) => v.vni ?? 0,
      align: "right",
      cell: (v) => <span className="font-mono text-xs">{v.vni ?? "—"}</span>,
    },
    {
      key: "eni",
      header: "ENIs",
      accessor: (v) => v.eniCount,
      align: "right",
      cell: (v) => <span className="font-mono">{v.eniCount}</span>,
    },
    {
      key: "dpus",
      header: "DPUs",
      accessor: (v) => v.dpuCount,
      align: "right",
      cell: (v) => <span className="font-mono">{v.dpuCount}</span>,
    },
    {
      key: "cidrs",
      header: "Underlay CIDRs",
      accessor: (v) => v.underlayCidrs.join(","),
      cell: (v) =>
        v.underlayCidrs.length === 0 ? (
          <span className="text-[color:var(--text-muted)] text-xs">—</span>
        ) : (
          <span className="font-mono text-xs">{v.underlayCidrs.join(", ")}</span>
        ),
    },
    {
      key: "labels",
      header: "Labels",
      accessor: (v) => Object.keys(v.labels).join(","),
      cell: (v) => <LabelChips labels={v.labels} />,
    },
  ];

  /* ── Tab bodies ───────────────────────────────────────── */

  const dpuTab = (
    <div className="space-y-4">
      {fleet.isLoading ? (
        <CardSkeleton />
      ) : dpuRows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No DPUs reported by dashd.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={dpuColumns}
            data={dpuRows}
            rowKey={(r) => r.id}
            onRowClick={(r) => navigate(`/dpu/${encodeURIComponent(r.id)}`)}
            defaultSort={{ key: "id", direction: "asc" }}
            filterPlaceholder="Filter DPUs…"
            emptyMessage="No DPUs match this filter"
          />
        </GlassCard>
      )}
    </div>
  );

  const eniTab = (
    <div className="space-y-4">
      {enis.isLoading || placements.isLoading ? (
        <CardSkeleton />
      ) : eniRows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No ENIs declared.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={eniColumns}
            data={eniRows}
            rowKey={(r) => r.name}
            defaultSort={{ key: "name", direction: "asc" }}
            filterPlaceholder="Filter ENIs…"
            emptyMessage="No ENIs match this filter"
          />
        </GlassCard>
      )}
    </div>
  );

  const vnetsTab = (
    <div className="space-y-4">
      {vnets.isLoading ? (
        <CardSkeleton />
      ) : vnetRows.length === 0 ? (
        <GlassCard>
          <p className="text-center text-[color:var(--text-muted)] py-6">
            No Vnets defined.
          </p>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable
            columns={vnetColumns}
            data={vnetRows}
            rowKey={(v) => `${v.namespace}/${v.name}`}
            onRowClick={(v) => {
              const spec = (vnets.data?.items ?? []).find(
                (x) => (x.metadata?.name ?? "") === v.name
              );
              if (spec) setDrawerVnet(spec);
            }}
            defaultSort={{ key: "name", direction: "asc" }}
            filterPlaceholder="Filter Vnets…"
            emptyMessage="No Vnets match this filter"
          />
        </GlassCard>
      )}
    </div>
  );

  const tabs: TabDef[] = [
    {
      id: "dpus",
      label: "DPUs",
      badge: dpuRows.length,
      icon: <Cpu size={14} />,
      content: dpuTab,
    },
    {
      id: "enis",
      label: "ENIs",
      badge: eniRows.length,
      icon: <Layers size={14} />,
      content: eniTab,
    },
    {
      id: "vnets",
      label: "Vnets",
      badge: vnetRows.length,
      icon: <Globe size={14} />,
      content: vnetsTab,
    },
  ];

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Fleet"
        subtitle={`${fleetDpuCount(fleet.data)} DPUs · ${fleetEniCount(fleet.data)} ENIs · ${fleetVnetCount(fleet.data)} Vnets · ${acls.data?.items?.length ?? 0} ACL · ${routes.data?.items?.length ?? 0} Routes · ${tunnels.data?.items?.length ?? 0} Tunnels`}
      />
      <Tabs tabs={tabs} defaultTabId="dpus" />

      {/* Vnet quick-detail drawer */}
      <Drawer
        open={!!drawerVnet}
        onClose={() => setDrawerVnet(null)}
        title={
          <span className="flex items-center gap-2">
            <Globe size={18} className="text-[color:var(--accent-purple)]" />
            <span className="font-mono">
              {drawerVnet?.metadata?.name ?? "vnet"}
            </span>
          </span>
        }
        subtitle={
          drawerVnet ? (
            <button
              type="button"
              onClick={() => {
                if (drawerVnet?.metadata?.name)
                  navigate(`/vnet/${encodeURIComponent(drawerVnet.metadata.name)}`);
              }}
              className="inline-flex items-center gap-1 hover:text-[color:var(--accent-cyan)]"
            >
              Open full view <ExternalLink size={10} />
            </button>
          ) : null
        }
      >
        {drawerVnet && (
          <>
            <DrawerSection title="Identity">
              <KeyValueRow label="Name" value={<code>{drawerVnet.metadata?.name}</code>} />
              <KeyValueRow label="Namespace" value={<code>{drawerVnet.metadata?.namespace}</code>} />
              <KeyValueRow label="VNI" value={<code>{drawerVnet.vni ?? "—"}</code>} />
              <KeyValueRow
                label="Generation"
                value={<code>{drawerVnet.metadata?.generation ?? "—"}</code>}
              />
            </DrawerSection>
            <DrawerSection title="Labels">
              <LabelChips labels={drawerVnet.metadata?.labels} />
            </DrawerSection>
            <DrawerSection title="ENIs in this Vnet">
              <ul className="flex flex-wrap gap-1">
                {(enis.data?.items ?? [])
                  .filter((e) => e.vnet_name === drawerVnet.metadata?.name)
                  .map((e) => (
                    <li key={e.metadata?.name}>
                      <button
                        type="button"
                        className="text-[11px] px-2 py-1 rounded bg-white/5 hover:bg-white/10 font-mono text-[color:var(--accent-green)]"
                      >
                        {e.metadata?.name}
                      </button>
                    </li>
                  ))}
              </ul>
            </DrawerSection>
          </>
        )}
      </Drawer>
    </div>
  );
}

/* Hover-color helper for the clickable count cells. */
function cnHover(color: string | undefined): string {
  const base =
    "font-mono px-2 py-0.5 rounded transition-colors hover:bg-white/10";
  switch (color) {
    case "cyan":
      return `${base} text-[color:var(--accent-cyan)] hover:underline`;
    case "purple":
      return `${base} text-[color:var(--accent-purple)] hover:underline`;
    case "green":
      return `${base} text-[color:var(--accent-green)] hover:underline`;
    case "amber":
      return `${base} text-[color:var(--accent-amber)] hover:underline`;
    case "red":
      return `${base} text-[color:var(--accent-red)] hover:underline`;
    default:
      return `${base} text-[color:var(--accent-cyan)] hover:underline`;
  }
}