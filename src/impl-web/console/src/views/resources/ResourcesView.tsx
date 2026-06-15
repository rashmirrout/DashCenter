/* ═══════════════════════════════════════════════════════════════
 * ResourcesView — the new `/resources` page (A-IF4).
 *
 * One self-contained page that delivers customer-grade interactive
 * CRUD for all 7 dashd resource kinds:
 *   Vnets · ENIs · Vnet Mappings · Service Tunnels · ACL Policies ·
 *   Route Policies · HA Sets
 *
 * The page is intentionally isolated from every other view:
 *   • Reads list data via the shared `useXxxList` hooks (no new
 *     network calls — same polling cadence as the existing list
 *     views).
 *   • Uses the A-IF3 form components for Create/Edit/Clone.
 *   • Uses `useDeleteResource` for deletes (already wired for
 *     every kind including vnet-mappings).
 *
 * All seven existing read-only list views (`/enis`, `/vnets`,
 * `/mappings`, `/tunnels`, `/routing`, `/policies`, `/health`,
 * `/dashboard`) remain untouched. `AdminOpsView` remains untouched.
 *
 * Added in A-IF4-G1..G4.
 * ═══════════════════════════════════════════════════════════════ */

import { useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { Copy, Pencil, Plus, Trash2 } from "lucide-react";

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

import { VnetForm } from "./forms/VnetForm";
import { EniForm } from "./forms/EniForm";
import { VnetMappingForm } from "./forms/VnetMappingForm";
import { ServiceTunnelForm } from "./forms/ServiceTunnelForm";
import { AclPolicyForm } from "./forms/AclPolicyForm";
import { RoutePolicyForm } from "./forms/RoutePolicyForm";
import { HaSetForm } from "./forms/HaSetForm";

/* ── Resource registry ─────────────────────────────────────── */
/* Each entry pairs a tab with its data hook, form component,
 * and DataTable columns. Adding a new resource is a one-line
 * append here. */

type AnyResource =
  | VnetSpec
  | EniSpec
  | VnetMappingSpec
  | ServiceTunnelSpec
  | AclPolicySpec
  | RoutePolicySpec
  | HaSetSpec;

interface ResourceDef<T extends AnyResource = AnyResource> {
  /** URL slug — matches the `kind` used by `useDeleteResource`. */
  kind: string;
  /** Tab label. */
  label: string;
  /** Live list hook (must return `{ data: { items: T[] }, … }`). */
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
    // Form initial values are loosely typed to allow row → form coercion.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    initial?: any;
    titleOverride?: string;
    onSaved?: () => void;
  }>;
  /** Columns for the DataTable. */
  columns: Column<T>[];
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
    key: "mac_address",
    header: "MAC",
    accessor: (r) => r.mac_address ?? "",
    cell: (r) => (
      <span className="font-mono text-xs">{r.mac_address || "—"}</span>
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
  {
    key: "params",
    header: "Params",
    accessor: (r) => Object.keys(r.params ?? {}).length,
    cell: (r) => {
      const ps = r.params ?? {};
      const entries = Object.entries(ps);
      if (entries.length === 0)
        return <span className="text-[color:var(--text-muted)] text-xs">—</span>;
      return (
        <div className="flex flex-wrap gap-1 max-w-[240px]">
          {entries.slice(0, 3).map(([k, v]) => (
            <span
              key={k}
              className="text-[10px] px-1.5 py-0.5 rounded bg-white/5 font-mono"
            >
              {k}={v}
            </span>
          ))}
          {entries.length > 3 && (
            <span className="text-[10px] text-[color:var(--text-muted)]">
              +{entries.length - 3}
            </span>
          )}
        </div>
      );
    },
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
      // Prefer the canonical `member_dpu_ids`; fall back to the
      // legacy `members[].dpu_id` only when present.
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

/* ── RESOURCE_DEFS ─────────────────────────────────────────── */

const RESOURCE_DEFS: ResourceDef[] = [
  {
    kind: "vnets",
    label: "Vnets",
    useList: useVnetList as ResourceDef["useList"],
    FormComponent: VnetForm,
    columns: vnetCols as Column<AnyResource>[],
  },
  {
    kind: "enis",
    label: "ENIs",
    useList: useEniList as ResourceDef["useList"],
    FormComponent: EniForm,
    columns: eniCols as Column<AnyResource>[],
  },
  {
    kind: "vnet-mappings",
    label: "Mappings",
    useList: useVnetMappings as ResourceDef["useList"],
    FormComponent: VnetMappingForm,
    columns: mappingCols as Column<AnyResource>[],
  },
  {
    kind: "service-tunnels",
    label: "Tunnels",
    useList: useServiceTunnels as ResourceDef["useList"],
    FormComponent: ServiceTunnelForm,
    columns: tunnelCols as Column<AnyResource>[],
  },
  {
    kind: "acl-policies",
    label: "ACL Policies",
    useList: useAclPolicies as ResourceDef["useList"],
    FormComponent: AclPolicyForm,
    columns: aclCols as Column<AnyResource>[],
  },
  {
    kind: "route-policies",
    label: "Route Policies",
    useList: useRoutePolicies as ResourceDef["useList"],
    FormComponent: RoutePolicyForm,
    columns: routeCols as Column<AnyResource>[],
  },
  {
    // dashd route is `/v1/{ns}/ha-sets/...`; `ha` returns HTTP 405.
    kind: "ha-sets",
    label: "HA Sets",
    useList: useHaSets as ResourceDef["useList"],
    FormComponent: HaSetForm,
    columns: haCols as Column<AnyResource>[],
  },
];

/* ═══════════════════════════════════════════════════════════════
 * Top-level page
 * ═══════════════════════════════════════════════════════════════ */

export default function ResourcesView() {
  const [params, setParams] = useSearchParams();
  const requested = params.get("kind") ?? RESOURCE_DEFS[0]!.kind;
  const activeKind =
    RESOURCE_DEFS.find((d) => d.kind === requested)?.kind ??
    RESOURCE_DEFS[0]!.kind;

  // Use each list hook once at the top so the Tabs badges can show
  // live counts without each tab having to mount first.
  const counts = RESOURCE_DEFS.map((d) => {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    const list = d.useList();
    return {
      kind: d.kind,
      count: list.isLoading ? null : list.data?.items?.length ?? 0,
    };
  });

  /* Build the tab definitions. The `key` on the inner ResourceTab
   * element forces a full unmount/remount when the user switches
   * tabs, so internal form state (open/close, editing row, …)
   * doesn't leak across tabs.
   *
   * Tabs only mounts the active tab's content, so the inactive
   * ResourceTab subtrees pay no rendering cost. The list hooks
   * at the top of this component keep the badge counts live. */
  const tabs: TabDef[] = RESOURCE_DEFS.map((d) => {
    const c = counts.find((x) => x.kind === d.kind);
    return {
      id: d.kind,
      label: d.label,
      badge: c?.count != null ? String(c.count) : "…",
      content: <ResourceTab key={d.kind} def={d} />,
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
        subtitle="Create, edit, and manage all networking resources via guided forms"
      />

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

function ResourceTab({ def }: { def: ResourceDef }) {
  const list = def.useList();
  const deleteMut = useDeleteResource();

  // Form state
  const [mode, setMode] = useState<FormMode>({ kind: "closed" });

  // Delete confirmation state
  const [pendingDelete, setPendingDelete] = useState<AnyResource | null>(null);

  const items: AnyResource[] = list.data?.items ?? [];

  /* Build the augmented columns list: the resource's own columns
   * plus a final "Actions" column. We rebuild on every render
   * (the cost is negligible) so each row gets fresh closures. */
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
          Create {def.label.replace(/s$/, "")}
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

      {/* Form dialog.
       *
       * The `key` forces a full unmount/remount whenever the form
       * mode flips OR the target row changes. Without it, editing
       * row A then immediately editing row B (without closing the
       * dialog in between) would keep A's stale field values in
       * the FormDialog's internal state — the useEffect reset is
       * keyed only on `open` transitions.
       */}
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

      {/* Delete confirmation */}
      <ConfirmDialog
        open={!!pendingDelete}
        onClose={() => setPendingDelete(null)}
        title={`Delete ${def.label.replace(/s$/, "")}?`}
        message={
          <>
            This will permanently delete{" "}
            <code className="text-[color:var(--accent-red)] font-mono">
              {pendingDelete?.metadata?.name}
            </code>
            . This action cannot be undone.
          </>
        }
        confirmLabel={`Delete ${pendingDelete?.metadata?.name ?? ""}`.trim()}
        tone="danger"
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

/** Produce a clone-mode initial: drop the name + generation but
 *  keep every other field. Strips generation from BOTH the SPA
 *  metadata block AND the wire-shape top-level (just in case the
 *  row is in an un-normalised state when cloned). */
function cloneInitial(row: AnyResource): AnyResource {
  // Deep-clone via JSON to avoid mutating the source row.
  const copy = JSON.parse(JSON.stringify(row)) as AnyResource;
  if (copy.metadata) {
    copy.metadata.name = "";
    if ("generation" in copy.metadata) copy.metadata.generation = undefined;
  }
  // Defensive: also clear a top-level `generation` that a future
  // un-normalised row might carry. Cast through unknown because
  // AnyResource doesn't model the wire-shape field.
  const looseCopy = copy as unknown as Record<string, unknown>;
  if ("generation" in looseCopy) delete looseCopy.generation;
  return copy;
}

/** Compute a stable React key for the FormComponent that captures
 *  both the form mode and the row identity. When the user switches
 *  between rows (edit A → edit B) without first closing the
 *  dialog, the key changes and React fully remounts the form,
 *  resetting all internal state. */
function formKeyFor(mode: FormMode): string {
  switch (mode.kind) {
    case "edit":
      return `edit:${mode.row.metadata?.namespace ?? "default"}/${mode.row.metadata?.name ?? ""}`;
    case "clone":
      return `clone:${mode.row.metadata?.namespace ?? "default"}/${mode.row.metadata?.name ?? ""}`;
    case "create":
      return "create";
    case "closed":
      // Use a stable closed key so React doesn't churn while
      // the dialog is being torn down.
      return "closed";
  }
}

/* ═══════════════════════════════════════════════════════════════
 * Row actions — Edit / Clone / Delete (A-IF4-G3)
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
      // Stop propagation so the parent row (if clickable) doesn't fire.
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