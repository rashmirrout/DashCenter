/* ═══════════════════════════════════════════════════════════════
 * RoutePolicyForm — Create / Edit / Clone a Route Policy.
 *
 * Each route can be EITHER single-hop OR an ECMP fan-out of
 * weighted members. The form has a per-route toggle that
 * swaps the next-hop fields for an ECMP members array.
 *
 * Added in A-IF3-G6 (complex).
 * ═══════════════════════════════════════════════════════════════ */

import { Plus, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";
import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { ResourceMultiSelect } from "@/components/form/ResourceMultiSelect";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import { FieldWrapper } from "@/components/form/NetworkInputs";
import type { RoutePolicySpec } from "@/api/types";
import { usePutRoutePolicy } from "@/queries/hooks";
import {
  routePolicySchema,
  type RoutePolicyInput,
  type RouteEntryInput,
  type EcmpMemberInput,
} from "@/lib/schemas";
import { cn } from "@/lib/cn";

interface RoutePolicyFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<RoutePolicyInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

function defaultRoute(): RouteEntryInput {
  return {
    prefix: "",
    next_hop_type: "vnet",
    next_hop_target: "",
    metric: 100,
    description: "",
  };
}

function defaultEcmpMember(): EcmpMemberInput {
  return { next_hop_type: "service_tunnel", next_hop_target: "", weight: 50 };
}

function emptyDefaults(): RoutePolicyInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    eni_names: [],
    routes: [defaultRoute()],
  };
}

function mergeDefaults(
  initial?: Partial<RoutePolicyInput>,
): RoutePolicyInput {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    eni_names: initial.eni_names ?? base.eni_names,
    routes:
      initial.routes && initial.routes.length > 0 ? initial.routes : base.routes,
  };
}

export function RoutePolicyForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: RoutePolicyFormProps) {
  const mutation = usePutRoutePolicy();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;
  const title =
    titleOverride ??
    (isEdit
      ? `Edit Route Policy · ${defaults.metadata.name}`
      : "Create Route Policy");

  return (
    <FormDialog<RoutePolicyInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Prefix-based routing rules bound to one or more ENIs"
      schema={routePolicySchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as RoutePolicySpec,
        });
        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create Route Policy"}
      width="lg"
    >
      {({ values, errorAt, setField }) => {
        function addRoute() {
          setField("routes", [...values.routes, defaultRoute()]);
        }
        function removeRoute(idx: number) {
          if (values.routes.length <= 1) return;
          setField(
            "routes",
            values.routes.filter((_, i) => i !== idx),
          );
        }

        return (
          <>
            <FieldWrapper
              label="Name"
              htmlFor="rp-name"
              error={errorAt("metadata.name")}
              required
            >
              <input
                id="rp-name"
                type="text"
                value={values.metadata.name}
                onChange={(e) => setField("metadata.name", e.target.value)}
                disabled={isEdit}
                placeholder="e.g. rp-gaming-geo-lb"
                className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
                aria-invalid={!!errorAt("metadata.name")}
              />
            </FieldWrapper>

            <ResourceMultiSelect
              kind="enis"
              ns={values.metadata.namespace}
              label="Bound ENIs"
              hint="Routes apply to traffic on these ENIs"
              value={values.eni_names}
              onChange={(next) => setField("eni_names", next)}
              error={errorAt("eni_names")}
            />

            {/* Routes */}
            <FieldWrapper
              label={`Routes (${values.routes.length})`}
              htmlFor="rp-routes"
              error={errorAt("routes")}
              required
            >
              <div className="space-y-2">
                {values.routes.map((r, idx) => (
                  <RouteEntryCard
                    key={idx}
                    idx={idx}
                    route={r}
                    ns={values.metadata.namespace}
                    errorAt={errorAt}
                    setField={setField}
                    onRemove={() => removeRoute(idx)}
                    canRemove={values.routes.length > 1}
                  />
                ))}
                <button
                  type="button"
                  onClick={addRoute}
                  className="flex items-center gap-1 text-xs text-accent-cyan hover:text-accent-cyan/80"
                >
                  <Plus size={12} />
                  Add route
                </button>
              </div>
            </FieldWrapper>

            <LabelsEditor
              label="Labels (optional)"
              value={values.metadata.labels}
              onChange={(next) => setField("metadata.labels", next)}
            />
          </>
        );
      }}
    </FormDialog>
  );
}

/* ── Per-route card ────────────────────────────────────────── */

interface RouteEntryCardProps {
  idx: number;
  route: RouteEntryInput;
  ns: string;
  errorAt: (path: string) => string | undefined;
  setField: (path: string, value: unknown) => void;
  onRemove: () => void;
  canRemove: boolean;
}

const NEXT_HOP_TYPES = ["vnet", "service_tunnel", "drop"] as const;

function RouteEntryCard({
  idx,
  route,
  ns,
  errorAt,
  setField,
  onRemove,
  canRemove,
}: RouteEntryCardProps) {
  const [collapsed, setCollapsed] = useState(true);
  const isEcmp = !!route.ecmp_members && route.ecmp_members.length > 0;

  // Aggregate every error path that could surface inside this card.
  // Top-level `routes.${idx}` catches the .refine() cross-field
  // error; the per-field paths catch individual validators. We
  // also probe a handful of ECMP-member paths so an invalid
  // weight on a collapsed card still triggers auto-expand.
  const ecmpMemberErrIdx =
    (route.ecmp_members ?? []).findIndex(
      (_m, mIdx) =>
        !!errorAt(`routes.${idx}.ecmp_members.${mIdx}.next_hop_type`) ||
        !!errorAt(`routes.${idx}.ecmp_members.${mIdx}.next_hop_target`) ||
        !!errorAt(`routes.${idx}.ecmp_members.${mIdx}.weight`),
    );
  const hasError =
    !!errorAt(`routes.${idx}`) ||
    !!errorAt(`routes.${idx}.prefix`) ||
    !!errorAt(`routes.${idx}.metric`) ||
    !!errorAt(`routes.${idx}.next_hop_type`) ||
    !!errorAt(`routes.${idx}.next_hop_target`) ||
    !!errorAt(`routes.${idx}.description`) ||
    ecmpMemberErrIdx >= 0;

  // One-way auto-expand when an error appears, so the user can
  // actually see what they need to fix. We never auto-collapse —
  // a manually collapsed-then-erroring card should still expand
  // on the next submit attempt to keep behaviour predictable.
  useEffect(() => {
    if (hasError && collapsed) setCollapsed(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasError]);

  const summaryLabel = isEcmp
    ? `ECMP × ${route.ecmp_members?.length ?? 0}`
    : route.next_hop_type === "drop"
      ? "drop"
      : route.next_hop_target
        ? `${route.next_hop_type} → ${route.next_hop_target}`
        : route.next_hop_type ?? "—";

  function toggleEcmp() {
    if (isEcmp) {
      // Switch to single-hop. Clear members, restore defaults.
      setField(`routes.${idx}.ecmp_members`, undefined);
      setField(`routes.${idx}.next_hop_type`, "vnet");
      setField(`routes.${idx}.next_hop_target`, "");
    } else {
      // Switch to ECMP. Seed two members.
      setField(`routes.${idx}.ecmp_members`, [
        defaultEcmpMember(),
        defaultEcmpMember(),
      ]);
      setField(`routes.${idx}.next_hop_type`, undefined);
      setField(`routes.${idx}.next_hop_target`, undefined);
    }
  }

  function addEcmpMember() {
    setField(`routes.${idx}.ecmp_members`, [
      ...(route.ecmp_members ?? []),
      defaultEcmpMember(),
    ]);
  }

  function removeEcmpMember(mIdx: number) {
    const next = (route.ecmp_members ?? []).filter((_, i) => i !== mIdx);
    if (next.length < 2) return; // schema requires ≥ 2
    setField(`routes.${idx}.ecmp_members`, next);
  }

  return (
    <div
      className={cn(
        "border rounded-md bg-bg-elevated/50",
        hasError ? "border-accent-red/50" : "border-border",
      )}
    >
      <div className="flex items-center gap-2 p-2">
        <button
          type="button"
          onClick={() => setCollapsed((c) => !c)}
          className="text-text-muted hover:text-text-primary"
          aria-label={collapsed ? "Expand route" : "Collapse route"}
        >
          {collapsed ? (
            <ChevronRight size={14} />
          ) : (
            <ChevronDown size={14} />
          )}
        </button>
        <span className="text-xs font-mono text-text-primary shrink-0 truncate max-w-[180px]">
          {route.prefix || "<no prefix>"}
        </span>
        <span className="text-xs font-mono text-text-secondary truncate flex-1">
          → {summaryLabel}
          {route.metric !== undefined ? ` · m=${route.metric}` : ""}
        </span>
        <button
          type="button"
          onClick={onRemove}
          disabled={!canRemove}
          aria-label={`Remove route ${idx + 1}`}
          className="p-1 text-text-muted hover:text-accent-red disabled:opacity-30"
          title={canRemove ? "Remove route" : "At least one route required"}
        >
          <Trash2 size={12} />
        </button>
      </div>

      {!collapsed && (
        <div className="p-3 pt-0 border-t border-border space-y-3">
          {/* Prefix + Metric */}
          <div className="grid grid-cols-2 gap-2">
            <FieldWrapper
              label="Prefix (CIDR)"
              htmlFor={`route-${idx}-prefix`}
              error={errorAt(`routes.${idx}.prefix`)}
              required
            >
              <input
                id={`route-${idx}-prefix`}
                type="text"
                value={route.prefix}
                onChange={(e) => setField(`routes.${idx}.prefix`, e.target.value)}
                placeholder="0.0.0.0/0"
                className="w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
              />
            </FieldWrapper>
            <FieldWrapper
              label="Metric (optional)"
              htmlFor={`route-${idx}-metric`}
              error={errorAt(`routes.${idx}.metric`)}
            >
              <input
                id={`route-${idx}-metric`}
                type="number"
                min={0}
                max={65535}
                value={route.metric ?? ""}
                onChange={(e) =>
                  setField(
                    `routes.${idx}.metric`,
                    e.target.value === ""
                      ? undefined
                      : Number.parseInt(e.target.value, 10),
                  )
                }
                placeholder="100"
                className="w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
              />
            </FieldWrapper>
          </div>

          {/* ECMP toggle */}
          <label className="flex items-center gap-2 text-xs text-text-secondary cursor-pointer">
            <input
              type="checkbox"
              checked={isEcmp}
              onChange={toggleEcmp}
              className="accent-accent-cyan"
            />
            Use ECMP (weighted multi-hop)
          </label>

          {/* Single next-hop OR ECMP members */}
          {!isEcmp ? (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-2">
              <FieldWrapper
                label="Next-hop type"
                htmlFor={`route-${idx}-nht`}
                error={errorAt(`routes.${idx}.next_hop_type`)}
                required
              >
                <select
                  id={`route-${idx}-nht`}
                  value={route.next_hop_type ?? "vnet"}
                  onChange={(e) =>
                    setField(`routes.${idx}.next_hop_type`, e.target.value)
                  }
                  className="w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
                >
                  {NEXT_HOP_TYPES.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
              </FieldWrapper>
              {route.next_hop_type !== "drop" && (
                <ResourceSelect
                  kind={
                    route.next_hop_type === "service_tunnel"
                      ? "service-tunnels"
                      : "vnets"
                  }
                  ns={ns}
                  label="Next-hop target"
                  value={route.next_hop_target ?? ""}
                  onChange={(v) =>
                    setField(`routes.${idx}.next_hop_target`, v)
                  }
                  error={errorAt(`routes.${idx}.next_hop_target`)}
                />
              )}
            </div>
          ) : (
            <div className="space-y-2">
              <p className="text-xs text-text-muted">
                ECMP members (weights determine load-balance ratio):
              </p>
              {(route.ecmp_members ?? []).map((m, mIdx) => (
                <div
                  key={mIdx}
                  className="flex items-end gap-2 p-2 rounded border border-border bg-bg-primary/40"
                >
                  <div className="w-32">
                    <FieldWrapper
                      label="Type"
                      htmlFor={`route-${idx}-ecmp-${mIdx}-type`}
                      error={errorAt(
                        `routes.${idx}.ecmp_members.${mIdx}.next_hop_type`,
                      )}
                    >
                      <select
                        id={`route-${idx}-ecmp-${mIdx}-type`}
                        value={m.next_hop_type}
                        onChange={(e) =>
                          setField(
                            `routes.${idx}.ecmp_members.${mIdx}.next_hop_type`,
                            e.target.value,
                          )
                        }
                        className="w-full px-2 py-1 text-xs bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
                      >
                        <option value="vnet">vnet</option>
                        <option value="service_tunnel">service_tunnel</option>
                      </select>
                    </FieldWrapper>
                  </div>
                  <div className="flex-1">
                    <ResourceSelect
                      kind={
                        m.next_hop_type === "service_tunnel"
                          ? "service-tunnels"
                          : "vnets"
                      }
                      ns={ns}
                      label="Target"
                      value={m.next_hop_target}
                      onChange={(v) =>
                        setField(
                          `routes.${idx}.ecmp_members.${mIdx}.next_hop_target`,
                          v,
                        )
                      }
                      error={errorAt(
                        `routes.${idx}.ecmp_members.${mIdx}.next_hop_target`,
                      )}
                    />
                  </div>
                  <div className="w-20">
                    <FieldWrapper
                      label="Weight"
                      htmlFor={`route-${idx}-ecmp-${mIdx}-w`}
                      error={errorAt(
                        `routes.${idx}.ecmp_members.${mIdx}.weight`,
                      )}
                    >
                      <input
                        id={`route-${idx}-ecmp-${mIdx}-w`}
                        type="number"
                        min={1}
                        max={255}
                        value={m.weight}
                        onChange={(e) =>
                          setField(
                            `routes.${idx}.ecmp_members.${mIdx}.weight`,
                            Number.parseInt(e.target.value, 10) || 0,
                          )
                        }
                        className="w-full px-2 py-1 text-xs bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
                      />
                    </FieldWrapper>
                  </div>
                  <button
                    type="button"
                    onClick={() => removeEcmpMember(mIdx)}
                    disabled={(route.ecmp_members ?? []).length <= 2}
                    aria-label={`Remove ECMP member ${mIdx + 1}`}
                    className="mb-1 p-1 text-text-muted hover:text-accent-red disabled:opacity-30"
                  >
                    <Trash2 size={12} />
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={addEcmpMember}
                className="flex items-center gap-1 text-xs text-accent-cyan hover:text-accent-cyan/80"
              >
                <Plus size={12} />
                Add ECMP member
              </button>
            </div>
          )}

          {/* Description */}
          <FieldWrapper
            label="Description"
            htmlFor={`route-${idx}-desc`}
            error={errorAt(`routes.${idx}.description`)}
          >
            <textarea
              id={`route-${idx}-desc`}
              value={route.description ?? ""}
              onChange={(e) =>
                setField(`routes.${idx}.description`, e.target.value)
              }
              rows={2}
              placeholder="e.g. fallback via DDoS scrub"
              className="w-full px-2 py-1 text-xs bg-bg-elevated border border-border rounded text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 resize-y"
            />
          </FieldWrapper>

          {/* Cross-field error */}
          {errorAt(`routes.${idx}`) && (
            <p className="text-xs text-accent-red" role="alert">
              {errorAt(`routes.${idx}`)}
            </p>
          )}
        </div>
      )}
    </div>
  );
}