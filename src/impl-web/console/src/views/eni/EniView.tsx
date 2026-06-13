import { useMemo } from "react";
import { useNavigate, useParams } from "react-router-dom";
import {
  ArrowLeft,
  Cpu,
  Layers,
  Network,
  Plug,
  Route as RouteIcon,
  Shield,
  ShieldCheck,
  Waypoints,
  Workflow,
} from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { StatusBadge } from "@/components/feedback/StatusBadge";
import { ErrorState } from "@/components/feedback/ErrorState";
import { EmptyState } from "@/components/feedback/EmptyState";
import { LoadingSkeleton } from "@/components/feedback/LoadingSkeleton";
import { DataTable, type Column } from "@/components/data/DataTable";
import { Tabs, type TabDef } from "@/components/layout/Tabs";

import { useEniDetail } from "@/queries/hooks";
import { formatIp, formatMac, stripStatePrefix } from "@/lib/format";

import type {
  AclPolicySpec,
  AclRule,
  EniDetail,
  RoutePolicySpec,
  RouteEntry,
  ServiceTunnelSpec,
  VnetMappingSpec,
} from "@/api/types";

/* ═══════════════════════════════════════════════════════════════
 * EniView — dedicated detail page for a single ENI.
 *
 * URL: /eni/:namespace/:name
 *
 * Backed by a single fetch to /api/console/eni/{ns}/{name}/detail
 * which the dashw BFF assembles by fanning out to 8 dashd endpoints
 * in parallel. The page is the "single comprehensive page" the user
 * asked for: everything attached to one ENI (DPU placement + HA,
 * parent Vnet with VNI, vnet-mappings, ACLs split by stage, routes,
 * service tunnels) on one URL, with a one-click jump into the
 * existing FlowTrace view pre-filled with this ENI + VNI.
 * ═══════════════════════════════════════════════════════════════ */

export default function EniView() {
  const params = useParams<{ namespace: string; name: string }>();
  const ns = params.namespace ?? "default";
  const name = params.name ?? "";

  if (!name) {
    return (
      <div className="animate-fade-in">
        <PageHeader title="ENI" subtitle="No ENI selected" />
        <ErrorState message="No ENI name in the URL. Browse Fleet → ENIs to pick one." />
      </div>
    );
  }

  return <EniDetailPanel ns={ns} name={name} />;
}

/* ── Main panel ────────────────────────────────────────────── */

function EniDetailPanel({ ns, name }: { ns: string; name: string }) {
  const navigate = useNavigate();
  const detail = useEniDetail(name, ns);

  if (detail.isLoading) {
    return (
      <div className="animate-fade-in space-y-6">
        <PageHeader title={name} subtitle="Loading…" />
        <LoadingSkeleton lines={8} />
      </div>
    );
  }

  if (detail.isError) {
    return (
      <ErrorState
        message={detail.error?.message ?? "Failed to load ENI"}
        onRetry={() => detail.refetch()}
      />
    );
  }

  const d = detail.data;
  if (!d) {
    return <EmptyState title={`ENI ${ns}/${name} not found`} />;
  }

  return <EniDetailContent detail={d} navigate={navigate} ns={ns} name={name} />;
}

/* ── Sections (split out so each is testable in isolation) ── */

interface ContentProps {
  detail: EniDetail;
  navigate: ReturnType<typeof useNavigate>;
  ns: string;
  name: string;
}

function EniDetailContent({ detail, navigate, ns, name }: ContentProps) {
  const identity = detail.identity;
  const vnet = detail.vnet;
  const placement = detail.placement;
  const haSet = detail.ha_set ?? null;
  const counters = detail.counters;
  const warnings = detail.warnings ?? [];

  /* ── Tabs ───────────────────────────────────────────── */

  const overviewTab = (
    <OverviewSection detail={detail} navigate={navigate} />
  );

  const mappingsTab = (
    <MappingsSection mappings={detail.vnet_mappings_reachable} />
  );

  const aclInTab = (
    <AclSection
      stage="inbound"
      acls={detail.acls_inbound}
      eniName={name}
    />
  );

  const aclOutTab = (
    <AclSection
      stage="outbound"
      acls={detail.acls_outbound}
      eniName={name}
    />
  );

  const routesTab = (
    <RoutesSection
      routes={detail.route_policies}
      tunnels={detail.service_tunnels}
      navigate={navigate}
    />
  );

  const tunnelsTab = (
    <TunnelsSection tunnels={detail.service_tunnels} />
  );

  const haTab = (
    <HaSection haSet={haSet} placementDpuIds={placement.dpu_ids} navigate={navigate} />
  );

  const tabs: TabDef[] = [
    { id: "overview", label: "Overview", icon: <Plug size={14} />, content: overviewTab },
    {
      id: "mappings",
      label: "Vnet Mappings",
      icon: <Waypoints size={14} />,
      badge: counters.mappings,
      content: mappingsTab,
    },
    {
      id: "acl-in",
      label: "ACL Inbound",
      icon: <Shield size={14} />,
      badge: counters.acl_inbound,
      content: aclInTab,
    },
    {
      id: "acl-out",
      label: "ACL Outbound",
      icon: <ShieldCheck size={14} />,
      badge: counters.acl_outbound,
      content: aclOutTab,
    },
    {
      id: "routes",
      label: "Routes",
      icon: <RouteIcon size={14} />,
      badge: counters.routes,
      content: routesTab,
    },
    {
      id: "tunnels",
      label: "Tunnels",
      icon: <Network size={14} />,
      badge: counters.tunnels,
      content: tunnelsTab,
    },
    {
      id: "ha",
      label: "HA",
      icon: <Layers size={14} />,
      badge: haSet ? 1 : 0,
      content: haTab,
    },
  ];

  /* ── Header + identity ─────────────────────────────── */

  const traceUrl = buildTraceFlowUrl(name, vnet?.vni);

  return (
    <div className="animate-fade-in space-y-6">
      <div className="flex items-center justify-between">
        <PageHeader
          title={
            <span className="flex items-center gap-3">
              <Plug size={22} className="text-[color:var(--accent-cyan)]" />
              <span className="font-mono">{name}</span>
            </span>
          }
          subtitle={
            <span className="flex items-center gap-2 flex-wrap">
              <StatusBadge status={stripStatePrefix(identity.admin_state) || "UNKNOWN"} />
              <span className="text-[color:var(--text-muted)]">·</span>
              <span className="text-[color:var(--text-secondary)]">
                ns <span className="font-mono">{ns}</span>
              </span>
              {identity.vnet_name && (
                <>
                  <span className="text-[color:var(--text-muted)]">·</span>
                  <button
                    type="button"
                    onClick={() => navigate(`/vnet/${encodeURIComponent(identity.vnet_name)}`)}
                    className="text-[color:var(--accent-purple)] hover:underline"
                  >
                    vnet <span className="font-mono">{identity.vnet_name}</span>
                  </button>
                </>
              )}
              {vnet && (
                <>
                  <span className="text-[color:var(--text-muted)]">·</span>
                  <span className="text-[color:var(--text-secondary)]">
                    VNI{" "}
                    <span className="font-mono text-[color:var(--accent-cyan)]">
                      {vnet.vni}
                    </span>
                  </span>
                </>
              )}
              {placement.dpu_ids.length > 0 && (
                <>
                  <span className="text-[color:var(--text-muted)]">·</span>
                  <span className="text-[color:var(--text-secondary)]">
                    on{" "}
                    {placement.dpu_ids.map((d, i) => (
                      <span key={d}>
                        {i > 0 && ", "}
                        <button
                          type="button"
                          onClick={() => navigate(`/dpu/${encodeURIComponent(d)}`)}
                          className="font-mono text-[color:var(--accent-cyan)] hover:underline"
                        >
                          {d}
                        </button>
                      </span>
                    ))}
                  </span>
                </>
              )}
              {placement.ha_active_active && (
                <span
                  title="ENI is present on multiple DPUs simultaneously (HA active-active)"
                  className="px-1.5 py-0.5 text-[10px] rounded-full bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] font-mono"
                >
                  HA · active-active
                </span>
              )}
            </span>
          }
        />
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => navigate(traceUrl)}
            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs bg-[color:var(--accent-cyan)]/10 hover:bg-[color:var(--accent-cyan)]/20 border border-[color:var(--accent-cyan)]/40 text-[color:var(--accent-cyan)]"
            title="Open Flow Trace pre-filled with this ENI + VNI"
          >
            <Workflow size={12} /> Trace a flow…
          </button>
          <button
            type="button"
            onClick={() => navigate("/enis")}
            className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md text-xs bg-white/5 hover:bg-white/10 border border-[color:var(--border-subtle)] text-[color:var(--text-secondary)]"
          >
            <ArrowLeft size={12} /> Back to ENIs
          </button>
        </div>
      </div>

      {/* Warnings banner — only when partial data */}
      {warnings.length > 0 && (
        <GlassCard glow="amber">
          <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-2">
            Partial data
          </p>
          <ul className="text-xs text-[color:var(--text-secondary)] space-y-1">
            {warnings.map((w, i) => (
              <li key={i} className="font-mono">
                · {w}
              </li>
            ))}
          </ul>
        </GlassCard>
      )}

      <IdentityCard detail={detail} navigate={navigate} />

      <Tabs tabs={tabs} defaultTabId="overview" />
    </div>
  );
}

/* ── Identity card ─────────────────────────────────────── */

function IdentityCard({ detail, navigate }: { detail: EniDetail; navigate: ReturnType<typeof useNavigate> }) {
  const id = detail.identity;
  const vnet = detail.vnet;
  return (
    <GlassCard glow="cyan">
      <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
        Identity
      </p>
      <dl className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-2 text-sm">
        <Row label="Name" value={<span className="font-mono">{detail.name}</span>} />
        <Row label="Namespace" value={<span className="font-mono">{detail.namespace}</span>} />
        <Row
          label="Vnet"
          value={
            id.vnet_name ? (
              <button
                type="button"
                onClick={() => navigate(`/vnet/${encodeURIComponent(id.vnet_name)}`)}
                className="font-mono text-[color:var(--accent-purple)] hover:underline"
              >
                {id.vnet_name}
              </button>
            ) : (
              <span className="text-[color:var(--text-muted)]">—</span>
            )
          }
        />
        <Row
          label="VNI (inherited from vnet)"
          value={
            vnet ? (
              <span className="font-mono text-[color:var(--accent-cyan)]">{vnet.vni}</span>
            ) : (
              <span className="text-[color:var(--text-muted)]">—</span>
            )
          }
        />
        <Row label="MAC address" value={<span className="font-mono text-xs">{formatMac(id.mac_address)}</span>} />
        <Row label="Underlay IP" value={<span className="font-mono text-xs">{formatIp(id.underlay_ip)}</span>} />
        <Row label="Admin state" value={<StatusBadge status={stripStatePrefix(id.admin_state) || "UNKNOWN"} />} />
        <Row
          label="Generation"
          value={
            id.generation != null ? (
              <span className="font-mono text-xs">{id.generation}</span>
            ) : (
              <span className="text-[color:var(--text-muted)]">—</span>
            )
          }
        />
        {id.labels && Object.keys(id.labels).length > 0 && (
          <div className="md:col-span-2">
            <dt className="text-[color:var(--text-secondary)] text-xs mb-1">Labels</dt>
            <dd className="flex flex-wrap gap-1">
              {Object.entries(id.labels).map(([k, v]) => (
                <span
                  key={k}
                  className="px-2 py-0.5 text-[10px] rounded-full bg-white/5 border border-[color:var(--border-subtle)] font-mono"
                >
                  {k}={v}
                </span>
              ))}
            </dd>
          </div>
        )}
      </dl>
    </GlassCard>
  );
}

/* ── Overview tab ──────────────────────────────────────── */

function OverviewSection({
  detail,
  navigate,
}: {
  detail: EniDetail;
  navigate: ReturnType<typeof useNavigate>;
}) {
  const placement = detail.placement;
  const vnet = detail.vnet;
  const c = detail.counters;
  return (
    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
      {/* Placement card */}
      <GlassCard glow="cyan">
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          Placement
        </p>
        {placement.dpu_ids.length === 0 ? (
          <p className="text-sm text-[color:var(--text-muted)]">
            ENI is declared but not yet placed on any DPU.
          </p>
        ) : (
          <>
            <div className="flex flex-wrap gap-2 mb-3">
              {placement.dpu_ids.map((d) => (
                <button
                  key={d}
                  type="button"
                  onClick={() => navigate(`/dpu/${encodeURIComponent(d)}`)}
                  className="inline-flex items-center gap-2 px-3 py-1.5 rounded-md bg-white/[0.02] border border-[color:var(--border-subtle)] hover:bg-white/5 hover:border-[color:var(--accent-cyan)]/40"
                >
                  <Cpu size={14} className="text-[color:var(--accent-cyan)]" />
                  <span className="font-mono text-xs text-[color:var(--text-primary)]">{d}</span>
                </button>
              ))}
            </div>
            <p className="text-xs text-[color:var(--text-secondary)]">
              {placement.ha_active_active
                ? "Active-active across the DPUs above (HA)."
                : "Placed on a single DPU."}
            </p>
          </>
        )}
      </GlassCard>

      {/* Vnet card */}
      <GlassCard glow="purple">
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          Parent Vnet
        </p>
        {vnet ? (
          <dl className="space-y-2 text-sm">
            <Row
              label="Name"
              value={
                <button
                  type="button"
                  onClick={() => navigate(`/vnet/${encodeURIComponent(vnet.name)}`)}
                  className="font-mono text-[color:var(--accent-purple)] hover:underline"
                >
                  {vnet.name}
                </button>
              }
            />
            <Row
              label="VNI"
              value={
                <span className="font-mono text-[color:var(--accent-cyan)] text-lg">
                  {vnet.vni}
                </span>
              }
            />
            {vnet.gw_mac && (
              <Row label="Gateway MAC" value={<span className="font-mono text-xs">{formatMac(vnet.gw_mac)}</span>} />
            )}
            {vnet.state && <Row label="State" value={<StatusBadge status={vnet.state} />} />}
            <p className="text-[10px] text-[color:var(--text-muted)] italic pt-2">
              ENIs inherit their VNI from the parent Vnet. There is no <code>vni</code>{" "}
              field on the ENI spec itself.
            </p>
          </dl>
        ) : (
          <p className="text-sm text-[color:var(--text-muted)]">
            Parent vnet could not be loaded (see warnings).
          </p>
        )}
      </GlassCard>

      {/* Cross-resource counters card */}
      <GlassCard className="lg:col-span-2">
        <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
          Attached resources
        </p>
        <div className="grid grid-cols-2 md:grid-cols-6 gap-4">
          <Counter label="ACL inbound" value={c.acl_inbound} accent="cyan" />
          <Counter label="ACL outbound" value={c.acl_outbound} accent="cyan" />
          <Counter label="Routes" value={c.routes} accent="green" />
          <Counter label="Mappings" value={c.mappings} accent="purple" />
          <Counter label="Tunnels" value={c.tunnels} accent="amber" />
          <Counter label="Placements" value={c.placements} accent="cyan" />
        </div>
      </GlassCard>
    </div>
  );
}

function Counter({
  label,
  value,
  accent,
}: {
  label: string;
  value: number;
  accent: "cyan" | "green" | "purple" | "amber";
}) {
  const accentColor = `text-[color:var(--accent-${accent})]`;
  return (
    <div className="rounded-md bg-white/[0.02] border border-[color:var(--border-subtle)] p-3 text-center">
      <p className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider mb-1">
        {label}
      </p>
      <p className={`text-2xl font-mono ${accentColor}`}>{value}</p>
    </div>
  );
}

/* ── Vnet Mappings tab ─────────────────────────────────── */

interface MappingRow {
  name: string;
  overlay_ip: string;
  underlay_ip: string;
  mac_address: string;
  action: string;
  tunnel: string;
}

function MappingsSection({ mappings }: { mappings: VnetMappingSpec[] }) {
  const rows: MappingRow[] = useMemo(
    () =>
      mappings.map((m) => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const any = m as any;
        return {
          name: any.metadata?.name ?? any.name ?? "",
          overlay_ip: any.ip_address ?? any.overlay_ip ?? "",
          underlay_ip: any.underlay_ip ?? "",
          mac_address: any.mac_address ?? "",
          action: any.action ?? "vnet_encap",
          tunnel: any.params?.tunnel ?? "",
        };
      }),
    [mappings]
  );

  if (rows.length === 0) {
    return (
      <EmptyState
        icon={<Waypoints size={28} />}
        title="No vnet-mappings reach this ENI's vnet."
      />
    );
  }

  const cols: Column<MappingRow>[] = [
    {
      key: "overlay_ip",
      header: "Overlay IP",
      accessor: (r) => r.overlay_ip,
      cell: (r) => <span className="font-mono text-xs">{formatIp(r.overlay_ip)}</span>,
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
      key: "tunnel",
      header: "Tunnel",
      accessor: (r) => r.tunnel,
      cell: (r) =>
        r.tunnel ? (
          <span className="font-mono text-xs text-[color:var(--accent-cyan)]">{r.tunnel}</span>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
    },
  ];

  return (
    <GlassCard className="p-0">
      <DataTable
        columns={cols}
        data={rows}
        rowKey={(r) => `${r.overlay_ip}-${r.underlay_ip}`}
        defaultSort={{ key: "overlay_ip", direction: "asc" }}
        filterPlaceholder="Filter mappings…"
        emptyMessage="No mappings match this filter"
      />
    </GlassCard>
  );
}

/* ── ACL tab (one collapsible card per policy, rule table inside) ── */

function AclSection({
  stage,
  acls,
  eniName,
}: {
  stage: "inbound" | "outbound";
  acls: AclPolicySpec[];
  eniName: string;
}) {
  if (acls.length === 0) {
    return (
      <EmptyState
        icon={stage === "inbound" ? <Shield size={28} /> : <ShieldCheck size={28} />}
        title={`No ${stage} ACL policies reference ${eniName}.`}
      />
    );
  }
  return (
    <div className="space-y-3">
      {acls.map((acl) => (
        <AclPolicyCard key={getName(acl)} acl={acl} />
      ))}
    </div>
  );
}

function AclPolicyCard({ acl }: { acl: AclPolicySpec }) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const any = acl as any;
  const name = getName(acl);
  const stage = any.stage ?? "—";
  const rules: AclRule[] = any.rules ?? [];

  const cols: Column<AclRule>[] = [
    {
      key: "priority",
      header: "Prio",
      accessor: (r) => r.priority,
      cell: (r) => <span className="font-mono text-xs">{r.priority}</span>,
      width: "w-16",
    },
    {
      key: "action",
      header: "Action",
      accessor: (r) => r.action,
      cell: (r) => <StatusBadge status={r.action ?? "—"} />,
      width: "w-32",
    },
    {
      key: "protocols",
      header: "Protocol",
      accessor: (r) => (r.protocols ?? []).join(","),
      cell: (r) => <ChipList items={r.protocols} />,
    },
    {
      key: "src",
      header: "Src",
      accessor: (r) => (r.src_prefixes ?? []).join(","),
      cell: (r) => (
        <div className="space-y-0.5">
          <ChipList items={r.src_prefixes} mono />
          {r.src_ports && r.src_ports.length > 0 && (
            <ChipList items={r.src_ports.map((p) => `:${p}`)} mono dim />
          )}
        </div>
      ),
    },
    {
      key: "dst",
      header: "Dst",
      accessor: (r) => (r.dst_prefixes ?? []).join(","),
      cell: (r) => (
        <div className="space-y-0.5">
          <ChipList items={r.dst_prefixes} mono />
          {r.dst_ports && r.dst_ports.length > 0 && (
            <ChipList items={r.dst_ports.map((p) => `:${p}`)} mono dim />
          )}
        </div>
      ),
    },
  ];

  return (
    <GlassCard>
      <div className="flex items-center justify-between mb-3">
        <div>
          <p className="font-mono text-sm text-[color:var(--text-primary)]">{name}</p>
          <p className="text-[10px] text-[color:var(--text-muted)]">
            stage {stage} · {rules.length} rule{rules.length === 1 ? "" : "s"}
          </p>
        </div>
      </div>
      {rules.length === 0 ? (
        <p className="text-sm text-[color:var(--text-muted)] py-4 text-center">No rules</p>
      ) : (
        <DataTable
          columns={cols}
          data={rules}
          rowKey={(r) => String(r.priority)}
          defaultSort={{ key: "priority", direction: "asc" }}
          emptyMessage="No rules"
        />
      )}
    </GlassCard>
  );
}

/* ── Routes tab ─────────────────────────────────────────── */

function RoutesSection({
  routes,
  tunnels,
  navigate,
}: {
  routes: RoutePolicySpec[];
  tunnels: ServiceTunnelSpec[];
  navigate: ReturnType<typeof useNavigate>;
}) {
  if (routes.length === 0) {
    return (
      <EmptyState icon={<RouteIcon size={28} />} title="No route policies reference this ENI." />
    );
  }
  const tunnelNames = new Set(tunnels.map((t) => getName(t)));
  return (
    <div className="space-y-3">
      {routes.map((rp) => (
        <RoutePolicyCard
          key={getName(rp)}
          rp={rp}
          tunnelNames={tunnelNames}
          navigate={navigate}
        />
      ))}
    </div>
  );
}

function RoutePolicyCard({
  rp,
  tunnelNames,
  navigate,
}: {
  rp: RoutePolicySpec;
  tunnelNames: Set<string>;
  navigate: ReturnType<typeof useNavigate>;
}) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const any = rp as any;
  const name = getName(rp);
  const entries: RouteEntry[] = any.routes ?? any.rules ?? [];

  const cols: Column<RouteEntry>[] = [
    {
      key: "prefix",
      header: "Prefix",
      accessor: (r) => r.prefix,
      cell: (r) => <span className="font-mono text-xs">{r.prefix}</span>,
    },
    {
      key: "next_hop_type",
      header: "Next-hop",
      accessor: (r) => r.next_hop_type ?? "",
      cell: (r) => (
        <span className="px-1.5 py-0.5 text-[10px] rounded font-mono bg-white/5">
          {r.next_hop_type ?? "—"}
        </span>
      ),
      width: "w-36",
    },
    {
      key: "next_hop_target",
      header: "Target",
      accessor: (r) => r.next_hop_target ?? "",
      cell: (r) => {
        const t = r.next_hop_target ?? "";
        if (!t) return <span className="text-[color:var(--text-muted)]">—</span>;
        const isTunnel = r.next_hop_type === "service_tunnel" && tunnelNames.has(t);
        return (
          <button
            type="button"
            disabled={!isTunnel}
            onClick={() => isTunnel && navigate("/tunnels")}
            className={
              isTunnel
                ? "font-mono text-xs text-[color:var(--accent-cyan)] hover:underline"
                : "font-mono text-xs text-[color:var(--text-primary)]"
            }
          >
            {t}
          </button>
        );
      },
    },
    {
      key: "ecmp",
      header: "ECMP",
      accessor: (r) => (r.ecmp_members?.length ?? 0).toString(),
      cell: (r) =>
        r.ecmp_members && r.ecmp_members.length > 0 ? (
          <span className="text-[10px] text-[color:var(--text-secondary)] font-mono">
            {r.ecmp_members.length} members
          </span>
        ) : (
          <span className="text-[color:var(--text-muted)]">—</span>
        ),
      width: "w-28",
    },
  ];

  return (
    <GlassCard>
      <div className="flex items-center justify-between mb-3">
        <p className="font-mono text-sm text-[color:var(--text-primary)]">{name}</p>
        <p className="text-[10px] text-[color:var(--text-muted)]">
          {entries.length} entr{entries.length === 1 ? "y" : "ies"}
        </p>
      </div>
      {entries.length === 0 ? (
        <p className="text-sm text-[color:var(--text-muted)] py-4 text-center">No route entries</p>
      ) : (
        <DataTable
          columns={cols}
          data={entries}
          rowKey={(r) => r.prefix}
          defaultSort={{ key: "prefix", direction: "asc" }}
          emptyMessage="No routes"
        />
      )}
    </GlassCard>
  );
}

/* ── Tunnels tab ─────────────────────────────────────────── */

interface TunnelRow {
  name: string;
  local_underlay_ip: string;
  remote_underlay_ip: string;
  vni: number | string;
  action: string;
}

function TunnelsSection({ tunnels }: { tunnels: ServiceTunnelSpec[] }) {
  const rows: TunnelRow[] = useMemo(
    () =>
      tunnels.map((t) => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const any = t as any;
        return {
          name: any.metadata?.name ?? any.name ?? "",
          local_underlay_ip: any.local_underlay_ip ?? "",
          remote_underlay_ip: any.remote_underlay_ip ?? "",
          vni: any.vni ?? "—",
          action: any.params?.action ?? "—",
        };
      }),
    [tunnels]
  );

  if (rows.length === 0) {
    return (
      <EmptyState
        icon={<Network size={28} />}
        title="No service tunnels are reachable from this ENI."
      />
    );
  }

  const cols: Column<TunnelRow>[] = [
    {
      key: "name",
      header: "Tunnel",
      accessor: (r) => r.name,
      cell: (r) => <span className="font-mono text-xs">{r.name}</span>,
    },
    {
      key: "local_underlay_ip",
      header: "Local underlay",
      accessor: (r) => r.local_underlay_ip,
      cell: (r) => <span className="font-mono text-xs">{formatIp(r.local_underlay_ip)}</span>,
    },
    {
      key: "remote_underlay_ip",
      header: "Remote underlay",
      accessor: (r) => r.remote_underlay_ip,
      cell: (r) => <span className="font-mono text-xs">{formatIp(r.remote_underlay_ip)}</span>,
    },
    {
      key: "vni",
      header: "VNI",
      accessor: (r) => String(r.vni),
      cell: (r) => <span className="font-mono text-xs">{r.vni}</span>,
      width: "w-24",
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
  ];

  return (
    <GlassCard className="p-0">
      <DataTable
        columns={cols}
        data={rows}
        rowKey={(r) => r.name}
        defaultSort={{ key: "name", direction: "asc" }}
        filterPlaceholder="Filter tunnels…"
        emptyMessage="No tunnels match this filter"
      />
    </GlassCard>
  );
}

/* ── HA tab ─────────────────────────────────────────────── */

function HaSection({
  haSet,
  placementDpuIds,
  navigate,
}: {
  haSet: EniDetail["ha_set"];
  placementDpuIds: string[];
  navigate: ReturnType<typeof useNavigate>;
}) {
  if (!haSet) {
    return (
      <EmptyState
        icon={<Layers size={28} />}
        title="This ENI's DPU(s) are not members of an HaSet."
      />
    );
  }
  const memberIDs = haSet.member_dpu_ids ?? [];
  const rolesByDpu = haSet.members_by_role ?? {};
  return (
    <GlassCard glow="cyan">
      <p className="text-[10px] text-[color:var(--text-secondary)] uppercase tracking-[0.14em] mb-3">
        HA Set: <span className="font-mono">{haSet.name}</span>
      </p>
      <dl className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-2 text-sm mb-4">
        {haSet.scope && <Row label="Scope" value={<span className="font-mono">{haSet.scope}</span>} />}
        {haSet.virtual_ip && (
          <Row
            label="Virtual IP"
            value={<span className="font-mono text-xs">{formatIp(haSet.virtual_ip)}</span>}
          />
        )}
        <Row
          label="Members"
          value={<span className="font-mono">{memberIDs.length}</span>}
        />
      </dl>
      <p className="text-[10px] text-[color:var(--text-muted)] uppercase tracking-wider mb-2">
        Member DPUs
      </p>
      <div className="flex flex-wrap gap-2">
        {memberIDs.map((d) => {
          const isMine = placementDpuIds.includes(d);
          const role = rolesByDpu[d];
          return (
            <button
              key={d}
              type="button"
              onClick={() => navigate(`/dpu/${encodeURIComponent(d)}`)}
              className={`inline-flex items-center gap-2 px-3 py-1.5 rounded-md border text-xs ${
                isMine
                  ? "bg-[color:var(--accent-cyan)]/10 border-[color:var(--accent-cyan)]/40 text-[color:var(--accent-cyan)]"
                  : "bg-white/[0.02] border-[color:var(--border-subtle)] text-[color:var(--text-primary)] hover:bg-white/5"
              }`}
              title={isMine ? "This is one of the ENI's placement DPUs" : "Peer DPU in the HA set"}
            >
              <Cpu size={12} />
              <span className="font-mono">{d}</span>
              {role && <span className="text-[10px] opacity-70 font-mono">· {role}</span>}
              {isMine && <span className="text-[10px] font-mono">· mine</span>}
            </button>
          );
        })}
      </div>
    </GlassCard>
  );
}

/* ── Small reusable bits ───────────────────────────────── */

function Row({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex justify-between items-center">
      <dt className="text-[color:var(--text-secondary)]">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

function ChipList({
  items,
  mono,
  dim,
}: {
  items?: string[];
  mono?: boolean;
  dim?: boolean;
}) {
  if (!items || items.length === 0) {
    return <span className="text-[color:var(--text-muted)]">—</span>;
  }
  return (
    <div className="flex flex-wrap gap-1">
      {items.map((i) => (
        <span
          key={i}
          className={`px-1.5 py-0.5 text-[10px] rounded bg-white/5 ${
            mono ? "font-mono" : ""
          } ${dim ? "text-[color:var(--text-muted)]" : ""}`}
        >
          {i}
        </span>
      ))}
    </div>
  );
}

/* ── Helpers ──────────────────────────────────────────── */

/**
 * Pull the bare name of a resource regardless of whether it's projected
 * as `metadata.name` or top-level `name` (dashd uses both shapes).
 */
function getName(r: unknown): string {
  if (!r || typeof r !== "object") return "";
  const obj = r as { metadata?: { name?: string }; name?: string };
  return obj.metadata?.name ?? obj.name ?? "";
}

/**
 * Build the URL for the existing FlowTrace view, pre-filled with this
 * ENI's name and (when known) the parent VNI. The FlowTraceView reads
 * these as query params if present; otherwise they're ignored. This is
 * a soft contract — no FlowTraceView changes are required for the link
 * itself, only for auto-fill to take effect.
 */
function buildTraceFlowUrl(eniName: string, vni: number | undefined): string {
  const qs = new URLSearchParams();
  qs.set("eni_name", eniName);
  if (vni != null) qs.set("vni", String(vni));
  return `/flow-trace?${qs.toString()}`;
}