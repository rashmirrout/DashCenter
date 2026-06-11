import { useState } from "react";
import { Route as RouteIcon, Layers } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { DataTable, type Column } from "@/components/data/DataTable";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import {
  Drawer,
  DrawerSection,
  KeyValueRow,
  LabelChips,
} from "@/components/feedback/Drawer";
import { useRoutePolicies, useVnetMappings } from "@/queries/hooks";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import type {
  RoutePolicySpec,
  VnetMappingSpec,
  RouteEntry,
} from "@/api/types";

export default function RoutingView() {
  const policies = useRoutePolicies("default");
  const mappings = useVnetMappings("default");
  const [openPolicy, setOpenPolicy] = useState<RoutePolicySpec | null>(null);
  const [openMapping, setOpenMapping] = useState<VnetMappingSpec | null>(null);

  /* ── Route policy columns ─────────────────────────── */
  const policyColumns: Column<RoutePolicySpec>[] = [
    {
      key: "name",
      header: "Name",
      accessor: (r) => r.metadata?.name,
      cell: (r) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {r.metadata?.name}
        </span>
      ),
    },
    {
      key: "enis",
      header: "ENIs",
      accessor: (r) => (r.eni_names ?? []).length,
      align: "right",
      cell: (r) => (
        <span className="font-mono">{(r.eni_names ?? []).length}</span>
      ),
    },
    {
      key: "routes",
      header: "Routes",
      accessor: (r) => (r.routes ?? r.rules ?? []).length,
      align: "right",
      cell: (r) => (
        <span className="font-mono text-[color:var(--accent-green)]">
          {(r.routes ?? r.rules ?? []).length}
        </span>
      ),
    },
    {
      key: "next_hops",
      header: "Next-hop kinds",
      accessor: (r) =>
        Array.from(
          new Set(
            (r.routes ?? r.rules ?? []).flatMap((e) => {
              const types: string[] = [];
              if (e.next_hop_type) types.push(e.next_hop_type);
              for (const m of e.ecmp_members ?? []) types.push(m.next_hop_type);
              return types;
            })
          )
        ).join(","),
      cell: (r) => {
        const types = Array.from(
          new Set(
            (r.routes ?? r.rules ?? []).flatMap((e) => {
              const ts: string[] = [];
              if (e.next_hop_type) ts.push(e.next_hop_type);
              for (const m of e.ecmp_members ?? []) ts.push(m.next_hop_type);
              return ts;
            })
          )
        );
        return (
          <div className="flex flex-wrap gap-1">
            {types.map((t) => (
              <span
                key={t}
                className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono"
              >
                {t}
              </span>
            ))}
          </div>
        );
      },
    },
    {
      key: "labels",
      header: "Labels",
      accessor: (r) => Object.keys(r.labels ?? {}).join(","),
      cell: (r) => <LabelChips labels={r.labels} />,
    },
  ];

  /* ── Vnet mapping columns ────────────────────────── */
  const mapColumns: Column<VnetMappingSpec>[] = [
    {
      key: "name",
      header: "Name",
      accessor: (m) => m.metadata?.name,
      cell: (m) => (
        <span className="font-mono text-[color:var(--text-primary)] text-xs">
          {m.metadata?.name}
        </span>
      ),
    },
    {
      key: "vnet",
      header: "Vnet",
      accessor: (m) => m.vnet_name,
      cell: (m) => (
        <span className="font-mono text-[color:var(--accent-purple)]">
          {m.vnet_name}
        </span>
      ),
    },
    {
      key: "overlay",
      header: "Overlay IP",
      accessor: (m) => m.ip_address ?? m.overlay_ip ?? "",
      cell: (m) => (
        <span className="font-mono text-xs">
          {m.ip_address ?? m.overlay_ip ?? "—"}
        </span>
      ),
    },
    {
      key: "underlay",
      header: "Underlay IP",
      accessor: (m) => m.underlay_ip,
      cell: (m) => <span className="font-mono text-xs">{m.underlay_ip}</span>,
    },
    {
      key: "mac",
      header: "MAC",
      accessor: (m) => m.mac_address,
      cell: (m) => <span className="font-mono text-xs">{m.mac_address}</span>,
    },
    {
      key: "action",
      header: "Action",
      accessor: (m) => m.action ?? "",
      cell: (m) =>
        m.action ? (
          <StatusBadge status={m.action.toUpperCase()} />
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
    },
    {
      key: "tunnel",
      header: "Tunnel",
      accessor: (m) => m.params?.tunnel ?? "",
      cell: (m) =>
        m.params?.tunnel ? (
          <span className="font-mono text-xs text-[color:var(--accent-amber)]">
            {m.params.tunnel}
          </span>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
    },
  ];

  /* ── Tab bodies ──────────────────────────────────── */

  const policiesTab = policies.isLoading ? (
    <CardSkeleton />
  ) : policies.isError ? (
    <ErrorState
      message={policies.error?.message ?? "Failed to load route policies"}
      onRetry={() => policies.refetch()}
    />
  ) : (
    <GlassCard className="p-0">
      <DataTable
        columns={policyColumns}
        data={policies.data?.items ?? []}
        rowKey={(r) => `${r.metadata?.namespace}/${r.metadata?.name}`}
        onRowClick={(r) => setOpenPolicy(r)}
        defaultSort={{ key: "name", direction: "asc" }}
        emptyMessage="No route policies defined."
        filterPlaceholder="Filter route policies…"
      />
    </GlassCard>
  );

  const mappingsTab = mappings.isLoading ? (
    <CardSkeleton />
  ) : mappings.isError ? (
    <ErrorState
      message={mappings.error?.message ?? "Failed to load vnet mappings"}
      onRetry={() => mappings.refetch()}
    />
  ) : (
    <GlassCard className="p-0">
      <DataTable
        columns={mapColumns}
        data={mappings.data?.items ?? []}
        rowKey={(m) => `${m.metadata?.namespace}/${m.metadata?.name}`}
        onRowClick={(m) => setOpenMapping(m)}
        defaultSort={{ key: "name", direction: "asc" }}
        emptyMessage="No vnet mappings defined."
        filterPlaceholder="Filter vnet mappings…"
      />
    </GlassCard>
  );

  const tabs: TabDef[] = [
    {
      id: "policies",
      label: "Route Policies",
      badge: policies.data?.items?.length ?? 0,
      icon: <RouteIcon size={14} />,
      content: policiesTab,
    },
    {
      id: "mappings",
      label: "Vnet Mappings",
      badge: mappings.data?.items?.length ?? 0,
      icon: <Layers size={14} />,
      content: mappingsTab,
    },
  ];

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader title="Routing" subtitle="Route policies and Vnet mappings" />
      <Tabs tabs={tabs} defaultTabId="policies" />

      <Drawer
        open={!!openPolicy}
        onClose={() => setOpenPolicy(null)}
        width="xl"
        title={
          <span className="flex items-center gap-2">
            <RouteIcon size={18} className="text-[color:var(--accent-green)]" />
            <span className="font-mono">
              {openPolicy?.metadata?.name ?? "route"}
            </span>
          </span>
        }
        subtitle={
          openPolicy
            ? `${(openPolicy.routes ?? openPolicy.rules ?? []).length} routes · ${(openPolicy.eni_names ?? []).length} ENIs`
            : null
        }
      >
        {openPolicy && <RouteDetail rp={openPolicy} />}
      </Drawer>

      <Drawer
        open={!!openMapping}
        onClose={() => setOpenMapping(null)}
        title={
          <span className="flex items-center gap-2">
            <Layers size={18} className="text-[color:var(--accent-purple)]" />
            <span className="font-mono">
              {openMapping?.metadata?.name ?? "mapping"}
            </span>
          </span>
        }
      >
        {openMapping && <MappingDetail m={openMapping} />}
      </Drawer>
    </div>
  );
}

/* ───────────────── Route Policy detail ───────────── */

function RouteDetail({ rp }: { rp: RoutePolicySpec }) {
  const routes: RouteEntry[] = rp.routes ?? rp.rules ?? [];
  return (
    <>
      <DrawerSection title="Identity">
        <KeyValueRow label="Name" value={<code>{rp.metadata?.name}</code>} />
        <KeyValueRow
          label="Namespace"
          value={<code>{rp.metadata?.namespace}</code>}
        />
        <KeyValueRow
          label="Generation"
          value={<code>{rp.metadata?.generation ?? "—"}</code>}
        />
      </DrawerSection>

      <DrawerSection title="Bound ENIs">
        {(rp.eni_names ?? []).length === 0 ? (
          <span className="text-xs text-[color:var(--text-muted)]">—</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {(rp.eni_names ?? []).map((n) => (
              <span
                key={n}
                className="text-[11px] px-1.5 py-0.5 rounded bg-white/5 font-mono text-[color:var(--accent-green)]"
              >
                {n}
              </span>
            ))}
          </div>
        )}
      </DrawerSection>

      <DrawerSection title="Labels">
        <LabelChips labels={rp.labels} />
      </DrawerSection>

      <DrawerSection title={`Routes (${routes.length})`}>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-[color:var(--text-muted)] border-b border-[color:var(--border-subtle)]">
                <th className="px-2 py-1.5 font-medium">Prefix</th>
                <th className="px-2 py-1.5 font-medium">Next hop</th>
                <th className="px-2 py-1.5 font-medium">Target</th>
                <th className="px-2 py-1.5 font-medium text-right">Metric</th>
              </tr>
            </thead>
            <tbody>
              {routes.map((r, i) => (
                <tr
                  key={`${r.prefix}-${i}`}
                  className="border-b border-[color:var(--border-subtle)] last:border-0 align-top"
                >
                  <td className="px-2 py-1.5 font-mono">{r.prefix}</td>
                  <td className="px-2 py-1.5">
                    {r.ecmp_members && r.ecmp_members.length > 0 ? (
                      <StatusBadge status="ECMP" />
                    ) : r.next_hop_type ? (
                      <StatusBadge status={r.next_hop_type.toUpperCase()} />
                    ) : (
                      <span className="text-[color:var(--text-muted)]">—</span>
                    )}
                  </td>
                  <td className="px-2 py-1.5 font-mono">
                    {r.ecmp_members && r.ecmp_members.length > 0 ? (
                      <ul className="space-y-0.5">
                        {r.ecmp_members.map((m, j) => (
                          <li key={j} className="text-xs">
                            <span className="text-[color:var(--text-muted)]">
                              {m.next_hop_type}=
                            </span>
                            <span className="text-[color:var(--accent-cyan)]">
                              {m.next_hop_target ?? "—"}
                            </span>
                            {m.weight != null && (
                              <span className="text-[color:var(--text-muted)] ml-1">
                                ({m.weight})
                              </span>
                            )}
                          </li>
                        ))}
                      </ul>
                    ) : (
                      r.next_hop_target ?? (
                        <span className="text-[color:var(--text-muted)]">—</span>
                      )
                    )}
                  </td>
                  <td className="px-2 py-1.5 font-mono text-right">
                    {r.metric ?? "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </DrawerSection>
    </>
  );
}

/* ─────────────── Vnet Mapping detail ─────────────── */

function MappingDetail({ m }: { m: VnetMappingSpec }) {
  return (
    <>
      <DrawerSection title="Identity">
        <KeyValueRow label="Name" value={<code>{m.metadata?.name}</code>} />
        <KeyValueRow
          label="Namespace"
          value={<code>{m.metadata?.namespace}</code>}
        />
        <KeyValueRow label="Vnet" value={<code>{m.vnet_name}</code>} />
      </DrawerSection>

      <DrawerSection title="Addresses">
        <KeyValueRow
          label="Overlay IP"
          value={<code>{m.ip_address ?? m.overlay_ip ?? "—"}</code>}
        />
        <KeyValueRow label="Underlay IP" value={<code>{m.underlay_ip}</code>} />
        <KeyValueRow label="MAC" value={<code>{m.mac_address}</code>} />
      </DrawerSection>

      <DrawerSection title="Action">
        {m.action ? (
          <StatusBadge status={m.action.toUpperCase()} />
        ) : (
          <span className="text-xs text-[color:var(--text-muted)]">—</span>
        )}
      </DrawerSection>

      {m.params && Object.keys(m.params).length > 0 && (
        <DrawerSection title="Params">
          {Object.entries(m.params).map(([k, v]) => (
            <KeyValueRow key={k} label={k} value={<code>{v}</code>} />
          ))}
        </DrawerSection>
      )}

      {m.labels && Object.keys(m.labels).length > 0 && (
        <DrawerSection title="Labels">
          <LabelChips labels={m.labels} />
        </DrawerSection>
      )}
    </>
  );
}