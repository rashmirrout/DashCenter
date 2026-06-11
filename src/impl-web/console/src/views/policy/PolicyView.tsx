import { useState } from "react";
import { Shield, ShieldCheck } from "lucide-react";
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
import { useAclPolicies, useHaSets } from "@/queries/hooks";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import type { AclPolicySpec, HaSetSpec } from "@/api/types";

export default function PolicyView() {
  const acls = useAclPolicies("default");
  const haSets = useHaSets("default");
  const [openAcl, setOpenAcl] = useState<AclPolicySpec | null>(null);
  const [openHa, setOpenHa] = useState<HaSetSpec | null>(null);

  /* ── ACL columns ─────────────────────────────────────── */
  const aclColumns: Column<AclPolicySpec>[] = [
    {
      key: "name",
      header: "Name",
      accessor: (a) => a.metadata?.name,
      cell: (a) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {a.metadata?.name}
        </span>
      ),
    },
    {
      key: "stage",
      header: "Stage",
      accessor: (a) => a.stage ?? "",
      cell: (a) => <StatusBadge status={(a.stage ?? "UNKNOWN").toUpperCase()} />,
      width: "w-28",
    },
    {
      key: "rules",
      header: "Rules",
      accessor: (a) => (a.rules ?? []).length,
      align: "right",
      cell: (a) => (
        <span className="font-mono text-[color:var(--accent-cyan)]">
          {(a.rules ?? []).length}
        </span>
      ),
    },
    {
      key: "enis",
      header: "ENIs",
      accessor: (a) => (a.eni_names ?? []).length,
      align: "right",
      cell: (a) => (
        <span className="font-mono">{(a.eni_names ?? []).length}</span>
      ),
    },
    {
      key: "actions",
      header: "Action mix",
      accessor: (a) =>
        Array.from(new Set((a.rules ?? []).map((r) => r.action))).join(","),
      cell: (a) => {
        const actions = Array.from(
          new Set((a.rules ?? []).map((r) => r.action))
        );
        return (
          <div className="flex flex-wrap gap-1">
            {actions.map((act) => (
              <StatusBadge key={act} status={act.toUpperCase()} />
            ))}
          </div>
        );
      },
    },
    {
      key: "labels",
      header: "Labels",
      accessor: (a) => Object.keys(a.labels ?? {}).join(","),
      cell: (a) => <LabelChips labels={a.labels} />,
    },
  ];

  /* ── HA columns ──────────────────────────────────────── */
  const haColumns: Column<HaSetSpec>[] = [
    {
      key: "name",
      header: "Name",
      accessor: (h) => h.metadata?.name,
      cell: (h) => (
        <span className="font-mono text-[color:var(--text-primary)]">
          {h.metadata?.name}
        </span>
      ),
    },
    {
      key: "scope",
      header: "Scope",
      accessor: (h) => h.scope,
      cell: (h) => <StatusBadge status={h.scope ?? "UNKNOWN"} />,
    },
    {
      key: "vip",
      header: "Virtual IP",
      accessor: (h) => h.virtual_ip ?? "",
      cell: (h) => (
        <span className="font-mono text-xs">{h.virtual_ip ?? "—"}</span>
      ),
    },
    {
      key: "members",
      header: "Members",
      accessor: (h) => (h.members ?? []).length,
      align: "right",
      cell: (h) => (
        <span className="font-mono">{(h.members ?? []).length}</span>
      ),
    },
    {
      key: "memberList",
      header: "DPUs",
      accessor: (h) => (h.members ?? []).map((m) => m.dpu_id).join(","),
      cell: (h) => (
        <div className="flex flex-wrap gap-1">
          {(h.members ?? []).map((m) => (
            <span
              key={m.dpu_id}
              className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono"
              title={`${m.role}: ${m.dpu_id}`}
            >
              {m.role}={m.dpu_id}
            </span>
          ))}
        </div>
      ),
    },
  ];

  /* ── Tab bodies ─────────────────────────────────────── */

  const aclTab = acls.isLoading ? (
    <CardSkeleton />
  ) : acls.isError ? (
    <ErrorState
      message={acls.error?.message ?? "Failed to load ACL policies"}
      onRetry={() => acls.refetch()}
    />
  ) : (
    <GlassCard className="p-0">
      <DataTable
        columns={aclColumns}
        data={acls.data?.items ?? []}
        rowKey={(a) => `${a.metadata?.namespace}/${a.metadata?.name}`}
        onRowClick={(a) => setOpenAcl(a)}
        defaultSort={{ key: "name", direction: "asc" }}
        emptyMessage="No ACL policies defined."
        filterPlaceholder="Filter ACL policies…"
      />
    </GlassCard>
  );

  const haTab = haSets.isLoading ? (
    <CardSkeleton />
  ) : haSets.isError ? (
    <ErrorState
      message={haSets.error?.message ?? "Failed to load HA sets"}
      onRetry={() => haSets.refetch()}
    />
  ) : (haSets.data?.items?.length ?? 0) === 0 ? (
    <GlassCard>
      <div className="text-center py-10 text-[color:var(--text-secondary)]">
        <ShieldCheck size={36} className="mx-auto mb-3 opacity-50" />
        <p className="text-sm">No HA sets configured on this fleet.</p>
        <p className="text-xs text-[color:var(--text-muted)] mt-1">
          HA sets define which DPUs form active/standby pairs.
        </p>
      </div>
    </GlassCard>
  ) : (
    <GlassCard className="p-0">
      <DataTable
        columns={haColumns}
        data={haSets.data?.items ?? []}
        rowKey={(h) => `${h.metadata?.namespace}/${h.metadata?.name}`}
        onRowClick={(h) => setOpenHa(h)}
        defaultSort={{ key: "name", direction: "asc" }}
        emptyMessage="No HA sets defined."
        filterPlaceholder="Filter HA sets…"
      />
    </GlassCard>
  );

  const tabs: TabDef[] = [
    {
      id: "acl",
      label: "ACL Policies",
      badge: acls.data?.items?.length ?? 0,
      icon: <Shield size={14} />,
      content: aclTab,
    },
    {
      id: "ha",
      label: "HA Sets",
      badge: haSets.data?.items?.length ?? 0,
      icon: <ShieldCheck size={14} />,
      content: haTab,
    },
  ];

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Policies"
        subtitle="Access-control rules and high-availability declarations"
      />
      <Tabs tabs={tabs} defaultTabId="acl" />

      {/* ACL Detail Drawer */}
      <Drawer
        open={!!openAcl}
        onClose={() => setOpenAcl(null)}
        width="xl"
        title={
          <span className="flex items-center gap-2">
            <Shield size={18} className="text-[color:var(--accent-cyan)]" />
            <span className="font-mono">{openAcl?.metadata?.name ?? "acl"}</span>
          </span>
        }
        subtitle={
          openAcl
            ? `${openAcl.stage ?? "stage?"} · ${openAcl.rules?.length ?? 0} rules · ${openAcl.eni_names?.length ?? 0} ENIs`
            : null
        }
      >
        {openAcl && <AclDetail acl={openAcl} />}
      </Drawer>

      {/* HA Detail Drawer */}
      <Drawer
        open={!!openHa}
        onClose={() => setOpenHa(null)}
        title={
          <span className="flex items-center gap-2">
            <ShieldCheck
              size={18}
              className="text-[color:var(--accent-green)]"
            />
            <span className="font-mono">{openHa?.metadata?.name ?? "ha"}</span>
          </span>
        }
      >
        {openHa && <HaDetail ha={openHa} />}
      </Drawer>
    </div>
  );
}

/* ───────────────── ACL Detail content ───────────────── */

function AclDetail({ acl }: { acl: AclPolicySpec }) {
  const rules = (acl.rules ?? []).slice().sort((a, b) => a.priority - b.priority);
  return (
    <>
      <DrawerSection title="Identity">
        <KeyValueRow label="Name" value={<code>{acl.metadata?.name}</code>} />
        <KeyValueRow
          label="Namespace"
          value={<code>{acl.metadata?.namespace}</code>}
        />
        <KeyValueRow
          label="Stage"
          value={<StatusBadge status={(acl.stage ?? "UNKNOWN").toUpperCase()} />}
        />
        <KeyValueRow
          label="Generation"
          value={<code>{acl.metadata?.generation ?? "—"}</code>}
        />
      </DrawerSection>

      <DrawerSection title="Bound ENIs">
        {(acl.eni_names ?? []).length === 0 ? (
          <span className="text-xs text-[color:var(--text-muted)]">—</span>
        ) : (
          <div className="flex flex-wrap gap-1">
            {(acl.eni_names ?? []).map((n) => (
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
        <LabelChips labels={acl.labels} />
      </DrawerSection>

      <DrawerSection title={`Rules (${rules.length})`}>
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-[color:var(--text-muted)] border-b border-[color:var(--border-subtle)]">
                <th className="px-2 py-1.5 font-medium">Pri</th>
                <th className="px-2 py-1.5 font-medium">Action</th>
                <th className="px-2 py-1.5 font-medium">Src</th>
                <th className="px-2 py-1.5 font-medium">Dst</th>
                <th className="px-2 py-1.5 font-medium">Proto</th>
                <th className="px-2 py-1.5 font-medium">Ports</th>
                <th className="px-2 py-1.5 font-medium">Description</th>
              </tr>
            </thead>
            <tbody>
              {rules.map((r, i) => (
                <tr
                  key={`${r.priority}-${i}`}
                  className="border-b border-[color:var(--border-subtle)] last:border-0 align-top"
                >
                  <td className="px-2 py-1.5 font-mono">{r.priority}</td>
                  <td className="px-2 py-1.5">
                    <StatusBadge status={r.action.toUpperCase()} />
                  </td>
                  <td className="px-2 py-1.5 font-mono">
                    {(r.src_prefixes ?? []).join(", ") || "—"}
                  </td>
                  <td className="px-2 py-1.5 font-mono">
                    {(r.dst_prefixes ?? []).join(", ") || "—"}
                  </td>
                  <td className="px-2 py-1.5 font-mono">
                    {(r.protocols ?? []).join(", ") || "—"}
                  </td>
                  <td className="px-2 py-1.5 font-mono">
                    {(r.dst_ports ?? r.src_ports ?? []).join(", ") || "—"}
                  </td>
                  <td className="px-2 py-1.5 text-[color:var(--text-secondary)]">
                    {r.description ?? "—"}
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

/* ───────────────── HA Detail content ───────────────── */

function HaDetail({ ha }: { ha: HaSetSpec }) {
  return (
    <>
      <DrawerSection title="Identity">
        <KeyValueRow label="Name" value={<code>{ha.metadata?.name}</code>} />
        <KeyValueRow
          label="Namespace"
          value={<code>{ha.metadata?.namespace}</code>}
        />
        <KeyValueRow label="Scope" value={<StatusBadge status={ha.scope} />} />
        {ha.virtual_ip && (
          <KeyValueRow label="Virtual IP" value={<code>{ha.virtual_ip}</code>} />
        )}
      </DrawerSection>

      <DrawerSection title={`Members (${(ha.members ?? []).length})`}>
        <table className="w-full text-xs">
          <thead>
            <tr className="text-left text-[color:var(--text-muted)] border-b border-[color:var(--border-subtle)]">
              <th className="px-2 py-1.5 font-medium">DPU</th>
              <th className="px-2 py-1.5 font-medium">Role</th>
            </tr>
          </thead>
          <tbody>
            {(ha.members ?? []).map((m) => (
              <tr
                key={m.dpu_id}
                className="border-b border-[color:var(--border-subtle)] last:border-0"
              >
                <td className="px-2 py-1.5 font-mono">{m.dpu_id}</td>
                <td className="px-2 py-1.5">
                  <StatusBadge status={m.role} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </DrawerSection>
    </>
  );
}