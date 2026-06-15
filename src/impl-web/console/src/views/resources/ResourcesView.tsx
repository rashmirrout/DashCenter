/* ═══════════════════════════════════════════════════════════════
 * ResourcesView — `/resources` page with resource dependency
 * resolution + validation.
 *
 * Customer-grade interactive CRUD for all 7 dashd resource kinds:
 *   Vnets · Service Tunnels · ENIs · Vnet Mappings · ACL Policies ·
 *   Route Policies · HA Sets
 *
 * Now wired to the dependency module (`lib/resource-deps`) for:
 *   • Tab order matches the resource creation DAG topology
 *   • Visual dependency mini-map at the top showing the DAG
 *   • Delete confirmation shows a warning when the resource has
 *     dependents, listing every dependent that would be orphaned
 *
 * The page still reads list data via the shared `useXxxList` hooks
 * and uses the form components for Create/Edit/Clone.
 *
 * Note (A-IF3-G3): The per-row "Dependents" / "References" columns
 * were removed — they cluttered the table and the same information
 * is now editable directly in the resource's create/edit dialog
 * (e.g. EniForm has reverse-ref multi-selects for ACL/Route
 * policies). The delete-confirmation warning is kept because that
 * is the moment the user genuinely needs the warning.
 * ═══════════════════════════════════════════════════════════════ */

import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Copy, Pencil, Plus, Trash2, GitBranch } from "lucide-react";

import { PageHeader } from "@/components/layout/PageHeader";
import { GlassCard } from "@/components/feedback/GlassCard";
import { ConfirmDialog } from "@/components/feedback/ConfirmDialog";
import { CardSkeleton } from "@/components/feedback/LoadingSkeleton";
import { ErrorState } from "@/components/feedback/ErrorState";
import { Tabs, type TabDef } from "@/components/layout/Tabs";
import { DataTable, type Column } from "@/components/data/DataTable";

import {
  useAclPolicies,
  useDeleteResource,
  useEniList,
  useHaSets,
  useRoutePolicies,
  useServiceTunnels,
  useVnetList,
  useVnetMappings,
} from "@/queries/hooks";
import type {
  AclPolicySpec,
  EniSpec,
  HaSetSpec,
  ListResponse,
  RoutePolicySpec,
  ServiceTunnelSpec,
  VnetMappingSpec,
  VnetSpec,
} from "@/api/types";
import type { ResourceKind } from "@/lib/constants";

import { VnetForm } from "./forms/VnetForm";
import { EniForm } from "./forms/EniForm";
import { VnetMappingForm } from "./forms/VnetMappingForm";
import { ServiceTunnelForm } from "./forms/ServiceTunnelForm";
import { AclPolicyForm } from "./forms/AclPolicyForm";
import { RoutePolicyForm } from "./forms/RoutePolicyForm";
import { HaSetForm } from "./forms/HaSetForm";

import {
  RESOURCE_CREATION_ORDER,
  KIND_LABELS,
  KIND_LABELS_PLURAL,
  type AnyResource,
  type ResourceSnapshot,
  emptySnapshot,
  getDependents,
} from "@/lib/resource-deps";

import { DependencyMiniMap } from "./DependencyMiniMap";

/* ── Resource registry ─────────────────────────────────────── */

interface ResourceDef<T extends AnyResource = AnyResource> {
  /** URL slug — matches the `kind` used by `useDeleteResource`. */
  kind: ResourceKind;
  /** Tab label. */
  label: string;
  /** Live list hook. */
  useList: () => {
    data?: ListResponse<T>;
    isLoading: boolean;
    isError: boolean;
    error?: Error | null;
    refetch: () => void;
  };
  /** Form component (Create / Edit / Clone). */
  FormComponent: React.ComponentType<{
    open: boolean;
    onClose: () => void;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    initial?: any;
    titleOverride?: string;
    onSaved?: () => void;
  }>;
  /** Columns for the DataTable (excluding deps/actions, added at runtime). */
  columns: Column<T>[];
  /** True when this kind has upstream resource references — controls
   *  whether the "References" column is rendered. */
  hasUpstreamRefs: boolean;
}

/* ── Column factories ──────────────────────────────────────── */

function nameCol<T extends AnyResource>(): Column<T> {
  return {
    key: "name",
    header: "Name",
    accessor: (r) => r.metadata?.name ?? "",
    cell: (r) => (
      <span className="font-mono text-[color:var(--text-primary)]">
        {r.metadata?.name ?? "—"}
      </span>
    ),
  };
}

function namespaceCol<T extends AnyResource>(): Column<T> {
  return {
    key: "namespace",
    header: "Namespace",
    accessor: (r) => r.metadata?.namespace ?? "",
    cell: (r) => (
      <span className="text-[10px] font-mono text-[color:var(--text-muted)]">
        {r.metadata?.namespace ?? "—"}
      </span>
    ),
    width: "w-28",
    hideWhenEmpty: true,
  };
}

function chipsCol<T extends AnyResource>(
  key: string,
  header: string,
  accessor: (r: T) => string[],
): Column<T> {
  return {
    key,
    header,
    accessor: (r) => accessor(r).join(","),
    cell: (r) => {
      const xs = accessor(r);
      if (xs.length === 0)
        return <span className="text-[color:var(--text-muted)] text-xs">—</span>;
      return (
        <div className="flex flex-wrap gap-1 max-w-[260px]">
          {xs.slice(0, 4).map((x) => (
            <span
              key={x}
              className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono"
            >
              {x}
            </span>
          ))}
          {xs.length > 4 && (
            <span className="text-[10px] text-[color:var(--text-muted)]">
              +{xs.length - 4}
            </span>
          )}
        </div>
      );
    },
  };
}

/* ── Per-resource column sets ──────────────────────────────── */

const vnetCols: Column<VnetSpec>[] = [
  nameCol<VnetSpec>(),
  namespaceCol<VnetSpec>(),
  {
    key: "vni",
    header: "VNI",
    accessor: (r) => r.vni ?? 0,
    cell: (r) => (
      <span className="font-mono text-xs text-[color:var(--accent-cyan)]">
        {r.vni ?? "—"}
      </span>
    ),
    width: "w-20",
  },
  {
    key: "gw_mac",
    header: "Gateway MAC",
    accessor: (r) => r.gw_mac ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.gw_mac || "—"}</span>
    ),
    hideWhenEmpty: true,
  },
];

const eniCols: Column<EniSpec>[] = [
  nameCol<EniSpec>(),
  namespaceCol<EniSpec>(),
  {
    key: "vnet_name",
    header: "Vnet",
    accessor: (r) => r.vnet_name ?? "",
    cell: (r) => (
      <span className="font-mono text-xs text-[color:var(--accent-purple)]">
        {r.vnet_name || "—"}
      </span>
    ),
  },
  {
    key: "mac_address",
    header: "MAC",
    accessor: (r) => r.mac_address ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.mac_address || "—"}</span>
    ),
  },
  {
    key: "underlay_ip",
    header: "Underlay IP",
    accessor: (r) => r.underlay_ip ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.underlay_ip || "—"}</span>
    ),
  },
  chipsCol<EniSpec>("dpus", "Placement DPUs", (r) => r.placement_hint_dpu_ids ?? []),
  {
    key: "admin_state",
    header: "State",
    accessor: (r) => r.admin_state ?? "",
    cell: (r) => (
      <span
        className={`text-[10px] font-mono uppercase px-1.5 py-0.5 rounded ${
          r.admin_state === "up"
            ? "bg-[color:var(--accent-green)]/15 text-[color:var(--accent-green)]"
            : "bg-white/5 text-[color:var(--text-muted)]"
        }`}
      >
        {r.admin_state || "—"}
      </span>
    ),
    width: "w-20",
  },
];

const mappingCols: Column<VnetMappingSpec>[] = [
  nameCol<VnetMappingSpec>(),
  {
    key: "vnet_name",
    header: "Vnet",
    accessor: (r) => r.vnet_name ?? "",
    cell: (r) => (
      <span className="font-mono text-xs text-[color:var(--accent-purple)]">
        {r.vnet_name || "—"}
      </span>
    ),
  },
  {
    key: "ip_address",
    header: "Overlay IP",
    accessor: (r) => r.ip_address ?? r.overlay_ip ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">
        {r.ip_address ?? r.overlay_ip ?? "—"}
      </span>
    ),
  },
  {
    key: "underlay_ip",
    header: "Underlay IP",
    accessor: (r) => r.underlay_ip ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.underlay_ip || "—"}</span>
    ),
  },
  {
    key: "action",
    header: "Action",
    accessor: (r) => r.action ?? "",
    cell: (r) => (
      <span className="text-[10px] font-mono uppercase px-1.5 py-0.5 rounded bg-white/5">
        {r.action || "—"}
      </span>
    ),
    width: "w-32",
  },
];

const tunnelCols: Column<ServiceTunnelSpec>[] = [
  nameCol<ServiceTunnelSpec>(),
  {
    key: "local_underlay_ip",
    header: "Local IP",
    accessor: (r) => r.local_underlay_ip ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.local_underlay_ip || "—"}</span>
    ),
  },
  {
    key: "remote_underlay_ip",
    header: "Remote IP",
    accessor: (r) => r.remote_underlay_ip ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.remote_underlay_ip || "—"}</span>
    ),
  },
  {
    key: "vni",
    header: "VNI",
    accessor: (r) => r.vni ?? 0,
    cell: (r) => (
      <span className="font-mono text-xs text-[color:var(--accent-cyan)]">
        {r.vni ?? "—"}
      </span>
    ),
    width: "w-20",
  },
];

const aclCols: Column<AclPolicySpec>[] = [
  nameCol<AclPolicySpec>(),
  {
    key: "stage",
    header: "Stage",
    accessor: (r) => r.stage ?? "",
    cell: (r) => (
      <span
        className={`text-[10px] font-mono uppercase px-1.5 py-0.5 rounded ${
          r.stage === "inbound"
            ? "bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)]"
            : r.stage === "outbound"
              ? "bg-[color:var(--accent-purple)]/15 text-[color:var(--accent-purple)]"
              : "bg-white/5 text-[color:var(--text-muted)]"
        }`}
      >
        {r.stage || "—"}
      </span>
    ),
    width: "w-24",
  },
  chipsCol<AclPolicySpec>("enis", "Bound ENIs", (r) => r.eni_names ?? []),
  {
    key: "rules",
    header: "Rules",
    accessor: (r) => r.rules?.length ?? 0,
    cell: (r) => (
      <span className="font-mono text-xs">{r.rules?.length ?? 0}</span>
    ),
    width: "w-20",
  },
];

const routeCols: Column<RoutePolicySpec>[] = [
  nameCol<RoutePolicySpec>(),
  chipsCol<RoutePolicySpec>("enis", "Bound ENIs", (r) => r.eni_names ?? []),
  {
    key: "routes",
    header: "Routes",
    accessor: (r) => r.routes?.length ?? r.rules?.length ?? 0,
    cell: (r) => (
      <span className="font-mono text-xs">
        {r.routes?.length ?? r.rules?.length ?? 0}
      </span>
    ),
    width: "w-20",
  },
];

const haCols: Column<HaSetSpec>[] = [
  nameCol<HaSetSpec>(),
  {
    key: "mode",
    header: "Mode",
    accessor: (r) => r.mode ?? r.scope ?? "",
    cell: (r) => {
      const m = r.mode ?? r.scope;
      if (!m) return <span className="text-[color:var(--text-muted)] text-xs">—</span>;
      const isActiveActive = m === "active_active";
      return (
        <span
          className={`text-[10px] font-mono uppercase px-1.5 py-0.5 rounded ${
            isActiveActive
              ? "bg-[color:var(--accent-purple)]/15 text-[color:var(--accent-purple)]"
              : "bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)]"
          }`}
        >
          {m}
        </span>
      );
    },
    width: "w-32",
  },
  {
    key: "member_dpu_ids",
    header: "Member DPUs",
    accessor: (r) => (r.member_dpu_ids ?? r.members?.map((m) => m.dpu_id) ?? []).length,
    cell: (r) => {
      const ids =
        r.member_dpu_ids ??
        (r.members ?? []).map((m) => m.dpu_id).filter(Boolean);
      if (ids.length === 0)
        return <span className="text-[color:var(--text-muted)] text-xs">—</span>;
      return (
        <div className="flex flex-wrap gap-1 max-w-[260px]">
          {ids.slice(0, 4).map((id) => (
            <span
              key={id}
              className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono"
            >
              {id}
            </span>
          ))}
          {ids.length > 4 && (
            <span className="text-[10px] text-[color:var(--text-muted)]">
              +{ids.length - 4}
            </span>
          )}
        </div>
      );
    },
  },
  {
    key: "virtual_ip",
    header: "VIP",
    accessor: (r) => r.virtual_ip ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.virtual_ip || "—"}</span>
    ),
    hideWhenEmpty: true,
  },
];

/* ── RESOURCE_DEFS — ORDERED BY DEPENDENCY DAG ─────────────── */
/* The order here matches RESOURCE_CREATION_ORDER from
 * lib/resource-deps.ts. Vnets and Service Tunnels come first
 * (roots), then ENIs (depend on vnets), then mappings (depend on
 * vnets + tunnels), then ACLs and Routes (depend on ENIs +
 * vnets + tunnels), and finally HA Sets. */

const RESOURCE_DEFS: ResourceDef[] = [
  {
    kind: "vnets",
    label: KIND_LABELS_PLURAL.vnets,
    useList: useVnetList as ResourceDef["useList"],
    FormComponent: VnetForm,
    columns: vnetCols as Column<AnyResource>[],
    hasUpstreamRefs: false,
  },
  {
    kind: "service-tunnels",
    label: KIND_LABELS_PLURAL["service-tunnels"],
    useList: useServiceTunnels as ResourceDef["useList"],
    FormComponent: ServiceTunnelForm,
    columns: tunnelCols as Column<AnyResource>[],
    hasUpstreamRefs: false,
  },
  {
    kind: "enis",
    label: KIND_LABELS_PLURAL.enis,
    useList: useEniList as ResourceDef["useList"],
    FormComponent: EniForm,
    columns: eniCols as Column<AnyResource>[],
    hasUpstreamRefs: true,
  },
  {
    kind: "vnet-mappings",
    label: KIND_LABELS_PLURAL["vnet-mappings"],
    useList: useVnetMappings as ResourceDef["useList"],
    FormComponent: VnetMappingForm,
    columns: mappingCols as Column<AnyResource>[],
    hasUpstreamRefs: true,
  },
  {
    kind: "acl-policies",
    label: KIND_LABELS_PLURAL["acl-policies"],
    useList: useAclPolicies as ResourceDef["useList"],
    FormComponent: AclPolicyForm,
    columns: aclCols as Column<AnyResource>[],
    hasUpstreamRefs: true,
  },
  {
    kind: "route-policies",
    label: KIND_LABELS_PLURAL["route-policies"],
    useList: useRoutePolicies as ResourceDef["useList"],
    FormComponent: RoutePolicyForm,
    columns: routeCols as Column<AnyResource>[],
    hasUpstreamRefs: true,
  },
  {
    kind: "ha-sets",
    label: KIND_LABELS_PLURAL["ha-sets"],
    useList: useHaSets as ResourceDef["useList"],
    FormComponent: HaSetForm,
    columns: haCols as Column<AnyResource>[],
    hasUpstreamRefs: false,
  },
];

// Sanity check: RESOURCE_DEFS order must match RESOURCE_CREATION_ORDER
// (the dependency module's topological order). This is enforced at
// runtime in dev builds to catch desync if someone reorders one but
// not the other.
if (import.meta.env.DEV) {
  const defOrder = RESOURCE_DEFS.map((d) => d.kind).join(",");
  const expected = RESOURCE_CREATION_ORDER.join(",");
  if (defOrder !== expected) {
    // eslint-disable-next-line no-console
    console.warn(
      `[ResourcesView] RESOURCE_DEFS order (${defOrder}) does not match RESOURCE_CREATION_ORDER (${expected}). Tab order will be inconsistent with the dependency DAG.`,
    );
  }
}

/* ═══════════════════════════════════════════════════════════════
 * Top-level page
 * ═══════════════════════════════════════════════════════════════ */

export default function ResourcesView() {
  const [params, setParams] = useSearchParams();
  const requested = params.get("kind") ?? RESOURCE_DEFS[0]!.kind;
  const activeKind =
    RESOURCE_DEFS.find((d) => d.kind === requested)?.kind ??
    RESOURCE_DEFS[0]!.kind;

  // Fetch every list once at the top so we can:
  //   1. Show live tab counts without each tab having to mount.
  //   2. Build a `ResourceSnapshot` for dependency rollups.
  //   3. Power the mini-map's per-node counts.
  const listResults = RESOURCE_DEFS.map((d) => ({
    kind: d.kind,
    // eslint-disable-next-line react-hooks/rules-of-hooks
    list: d.useList(),
  }));

  // Build a unified snapshot. Each list may still be loading; we use
  // empty arrays as defensive defaults so rollups don't crash.
  const snapshot: ResourceSnapshot = useMemo(() => {
    const snap = emptySnapshot();
    for (const { kind, list } of listResults) {
      const items = (list.data?.items ?? []) as AnyResource[];
      (snap as unknown as Record<string, AnyResource[]>)[kind] = items;
    }
    return snap;
    // listResults identity changes on every render, but the *contents*
    // only change when list data refreshes. We snapshot by-data via
    // JSON length signatures.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    listResults.map((r) => r.list.data?.items?.length ?? 0).join(","),
    listResults.map((r) => (r.list.isLoading ? 1 : 0)).join(","),
  ]);

  // Counts for tab badges + mini-map.
  const counts: Partial<Record<ResourceKind, number | null>> = {};
  for (const { kind, list } of listResults) {
    counts[kind] = list.isLoading ? null : list.data?.items?.length ?? 0;
  }

  /* Build the tab definitions. */
  const tabs: TabDef[] = RESOURCE_DEFS.map((d) => {
    const c = counts[d.kind];
    return {
      id: d.kind,
      label: d.label,
      badge: c != null ? String(c) : "…",
      content: <ResourceTab key={d.kind} def={d} snapshot={snapshot} />,
    };
  });

  function onTabChange(next: string) {
    const sp = new URLSearchParams(params);
    sp.set("kind", next);
    setParams(sp, { replace: true });
  }

  return (
    <div className="animate-fade-in space-y-6">
      <PageHeader
        title="Resources"
        subtitle="Create, edit, and manage all networking resources via guided forms — with dependency tracking"
      />

      {/* Dependency mini-map */}
      <GlassCard>
        <div className="flex items-start gap-3">
          <GitBranch
            size={18}
            className="shrink-0 mt-1 text-[color:var(--accent-cyan)]"
            aria-hidden
          />
          <div className="flex-1">
            <DependencyMiniMap
              activeKind={activeKind}
              onSelectKind={(k) => onTabChange(k)}
              counts={counts}
            />
          </div>
        </div>
      </GlassCard>

      <Tabs tabs={tabs} activeTabId={activeKind} onChange={onTabChange} />
    </div>
  );
}

/* ═══════════════════════════════════════════════════════════════
 * Per-resource tab
 * ═══════════════════════════════════════════════════════════════ */

type FormMode =
  | { kind: "create" }
  | { kind: "edit"; row: AnyResource }
  | { kind: "clone"; row: AnyResource }
  | { kind: "closed" };

interface ResourceTabProps {
  def: ResourceDef;
  /** Live snapshot of all resource kinds (for dep rollups). */
  snapshot: ResourceSnapshot;
}

function ResourceTab({ def, snapshot }: ResourceTabProps) {
  const list = def.useList();
  const deleteMut = useDeleteResource();

  const [mode, setMode] = useState<FormMode>({ kind: "closed" });
  const [pendingDelete, setPendingDelete] = useState<AnyResource | null>(null);

  const items: AnyResource[] = list.data?.items ?? [];

  // Build the column list: original cols + Actions.
  // (The previous "Dependents" / "References" columns were removed
  // in A-IF3-G3 — the same info is now editable inside each form
  // dialog, e.g. EniForm's ACL/Route policy multi-selects.)
  const columns: Column<AnyResource>[] = useMemo(() => {
    const actionsCol: Column<AnyResource> = {
      key: "__actions",
      header: "Actions",
      accessor: () => "",
      sortable: false,
      width: "w-28",
      cell: (row) => (
        <ResourceRowActions
          onEdit={() => setMode({ kind: "edit", row })}
          onClone={() => setMode({ kind: "clone", row })}
          onDelete={() => setPendingDelete(row)}
        />
      ),
    };

    return [...def.columns, actionsCol];
  }, [def.columns]);

  const FormComponent = def.FormComponent;

  // For the delete dialog: compute live dependents of the row being deleted
  const pendingDependents = useMemo(() => {
    if (!pendingDelete) return [];
    return getDependents(def.kind, pendingDelete.metadata?.name ?? "", snapshot);
  }, [pendingDelete, def.kind, snapshot]);

  return (
    <div className="space-y-3">
      {/* Header with live count + Create button */}
      <div className="flex items-center justify-between">
        <p className="text-xs text-[color:var(--text-secondary)]">
          {list.isLoading
            ? "Loading…"
            : list.isError
              ? "Failed to load"
              : `${items.length} ${def.label.toLowerCase()} · namespace=default`}
        </p>
        <button
          type="button"
          onClick={() => setMode({ kind: "create" })}
          className="flex items-center gap-1.5 px-3 py-1.5 text-sm rounded-md bg-[color:var(--accent-cyan)]/15 text-[color:var(--accent-cyan)] border border-[color:var(--accent-cyan)]/40 hover:bg-[color:var(--accent-cyan)]/25 transition-colors"
        >
          <Plus size={14} />
          Create {KIND_LABELS[def.kind]}
        </button>
      </div>

      {/* Body */}
      {list.isError ? (
        <ErrorState
          message={list.error?.message ?? "Failed to load"}
          onRetry={() => list.refetch()}
        />
      ) : list.isLoading ? (
        <CardSkeleton />
      ) : items.length === 0 ? (
        <GlassCard>
          <div className="text-center py-8 space-y-2">
            <p className="text-[color:var(--text-muted)]">
              No {def.label.toLowerCase()} yet.
            </p>
            <button
              type="button"
              onClick={() => setMode({ kind: "create" })}
              className="text-sm text-[color:var(--accent-cyan)] hover:underline"
            >
              Create the first one →
            </button>
          </div>
        </GlassCard>
      ) : (
        <GlassCard className="p-0">
          <DataTable<AnyResource>
            columns={columns}
            data={items}
            rowKey={(r) => `${r.metadata?.namespace ?? "default"}/${r.metadata?.name ?? ""}`}
            defaultSort={{ key: "name", direction: "asc" }}
            filterPlaceholder={`Filter ${def.label.toLowerCase()}…`}
          />
        </GlassCard>
      )}

      {/* Form dialog */}
      <FormComponent
        key={formKeyFor(mode)}
        open={mode.kind === "create" || mode.kind === "edit" || mode.kind === "clone"}
        onClose={() => setMode({ kind: "closed" })}
        initial={
          mode.kind === "edit"
            ? mode.row
            : mode.kind === "clone"
              ? cloneInitial(mode.row)
              : undefined
        }
        titleOverride={mode.kind === "clone" ? `Clone — pick a new name` : undefined}
      />

      {/* Delete confirmation — augmented with dependency warning */}
      <ConfirmDialog
        open={!!pendingDelete}
        onClose={() => setPendingDelete(null)}
        title={
          pendingDependents.length > 0
            ? `Delete ${KIND_LABELS[def.kind]} with ${pendingDependents.length} dependent${pendingDependents.length === 1 ? "" : "s"}?`
            : `Delete ${KIND_LABELS[def.kind]}?`
        }
        message={
          <DeleteWarning
            kindLabel={KIND_LABELS[def.kind]}
            name={pendingDelete?.metadata?.name ?? ""}
            dependents={pendingDependents}
          />
        }
        confirmLabel={`Delete ${pendingDelete?.metadata?.name ?? ""}`.trim()}
        tone={pendingDependents.length > 0 ? "warning" : "danger"}
        showReasonInput={false}
        onConfirm={async () => {
          if (!pendingDelete) return;
          await deleteMut.mutateAsync({
            kind: def.kind,
            ns: pendingDelete.metadata?.namespace ?? "default",
            name: pendingDelete.metadata?.name ?? "",
          });
        }}
      />
    </div>
  );
}

/* ── DeleteWarning — rich message body for the confirm dialog ── */

interface DeleteWarningProps {
  kindLabel: string;
  name: string;
  dependents: Array<{ kind: ResourceKind; name: string; field: string }>;
}

function DeleteWarning({ kindLabel, name, dependents }: DeleteWarningProps) {
  if (dependents.length === 0) {
    return (
      <>
        <p>
          This will permanently delete{" "}
          <code className="text-[color:var(--accent-red)] font-mono">{name}</code>.
          This action cannot be undone.
        </p>
      </>
    );
  }

  // Group dependents by kind so the warning is readable
  const byKind = new Map<ResourceKind, string[]>();
  for (const d of dependents) {
    const arr = byKind.get(d.kind) ?? [];
    if (!arr.includes(d.name)) arr.push(d.name);
    byKind.set(d.kind, arr);
  }

  return (
    <div className="space-y-2">
      <p>
        Deleting <code className="text-[color:var(--accent-red)] font-mono">{name}</code>{" "}
        will leave the following resources with broken references:
      </p>
      <div className="rounded-md border border-[color:var(--accent-amber)]/30 bg-[color:var(--accent-amber)]/5 p-2 space-y-1 max-h-44 overflow-y-auto">
        {Array.from(byKind.entries()).map(([kind, names]) => (
          <div key={kind} className="text-xs">
            <span className="font-semibold text-[color:var(--accent-amber)]">
              {names.length} {names.length === 1 ? KIND_LABELS[kind] : KIND_LABELS_PLURAL[kind]}
            </span>
            <span className="text-[color:var(--text-muted)] ml-1">·</span>
            <span className="font-mono text-[10px] ml-1 text-[color:var(--text-secondary)]">
              {names.slice(0, 6).join(", ")}
              {names.length > 6 && ` +${names.length - 6} more`}
            </span>
          </div>
        ))}
      </div>
      <p className="text-[10px] text-[color:var(--text-muted)] italic">
        Tip: delete or update these dependents first to avoid orphan references.
        dashd will accept the delete regardless, but the {kindLabel.toLowerCase()}{" "}
        name will remain referenced in those resources until you fix them.
      </p>
    </div>
  );
}

/** Produce a clone-mode initial: drop the name + generation but
 *  keep every other field. */
function cloneInitial(row: AnyResource): AnyResource {
  const copy = JSON.parse(JSON.stringify(row)) as AnyResource;
  if (copy.metadata) {
    copy.metadata.name = "";
    if ("generation" in copy.metadata) copy.metadata.generation = undefined;
  }
  const looseCopy = copy as unknown as Record<string, unknown>;
  if ("generation" in looseCopy) delete looseCopy.generation;
  return copy;
}

/** Stable React key for the FormComponent. */
function formKeyFor(mode: FormMode): string {
  switch (mode.kind) {
    case "edit":
      return `edit:${mode.row.metadata?.namespace ?? "default"}/${mode.row.metadata?.name ?? ""}`;
    case "clone":
      return `clone:${mode.row.metadata?.namespace ?? "default"}/${mode.row.metadata?.name ?? ""}`;
    case "create":
      return "create";
    case "closed":
      return "closed";
  }
}

/* ═══════════════════════════════════════════════════════════════
 * Row actions — Edit / Clone / Delete
 * ═══════════════════════════════════════════════════════════════ */

interface ResourceRowActionsProps {
  onEdit: () => void;
  onClone: () => void;
  onDelete: () => void;
}

function ResourceRowActions({
  onEdit,
  onClone,
  onDelete,
}: ResourceRowActionsProps) {
  return (
    <div
      className="flex items-center gap-1"
      onClick={(e) => e.stopPropagation()}
    >
      <button
        type="button"
        onClick={onEdit}
        aria-label="Edit"
        title="Edit"
        className="p-1 rounded text-[color:var(--text-secondary)] hover:text-[color:var(--accent-cyan)] hover:bg-white/5"
      >
        <Pencil size={13} />
      </button>
      <button
        type="button"
        onClick={onClone}
        aria-label="Clone"
        title="Clone"
        className="p-1 rounded text-[color:var(--text-secondary)] hover:text-[color:var(--accent-purple)] hover:bg-white/5"
      >
        <Copy size={13} />
      </button>
      <button
        type="button"
        onClick={onDelete}
        aria-label="Delete"
        title="Delete"
        className="p-1 rounded text-[color:var(--text-secondary)] hover:text-[color:var(--accent-red)] hover:bg-white/5"
      >
        <Trash2 size={13} />
      </button>
    </div>
  );
}