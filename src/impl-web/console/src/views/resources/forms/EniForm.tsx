/* ═══════════════════════════════════════════════════════════════
 * EniForm — Create / Edit / Clone an ENI.
 *
 * Live cross-references (forward refs — fields on the ENI itself):
 *   - `vnet_name`                 from `useResourceList('vnets')`
 *   - `placement_hint_dpu_ids[]`  from `useResourceList('inventory')`
 *
 * Reverse-reference selectors (the binding lives on the other side
 * in the dashd REST model):
 *   - ACL Policies   — `AclPolicy.eni_names[]` includes this ENI
 *   - Route Policies — `RoutePolicy.eni_names[]` includes this ENI
 *
 * Why reverse refs? The DASH vendor protos key both `AclIn/AclOut`
 * and `EniRoute` by `{eni, ...}` — i.e. policy-per-ENI. dashd's
 * REST shape inverts that: ACL/Route are top-level resources that
 * each carry an `eni_names[]` list of ENIs they apply to. So the
 * user-facing question "which policies bind this ENI?" can only
 * be answered (and edited) from the policy side. This form lets
 * you do that without leaving the ENI dialog by:
 *
 *   1. Pre-loading every AclPolicy/RoutePolicy in the namespace,
 *      filtering to those that already list this ENI in their
 *      `eni_names[]`. These pre-populate the multi-selects in
 *      EDIT mode.
 *   2. On submit, PUTting the ENI first, then iterating over the
 *      diff between original and current selections and PUTting
 *      each changed policy with the ENI added/removed from its
 *      `eni_names[]`.
 *
 * Refactored in A-IF3-G3 to remove the awkward "Dependents" column
 * idea and put the editing capability where the user actually
 * wants it: inside the ENI create/edit dialog.
 * ═══════════════════════════════════════════════════════════════ */

import { useMemo } from "react";
import { z } from "zod";
import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { ResourceMultiSelect } from "@/components/form/ResourceMultiSelect";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import {
  FieldWrapper,
  IpInput,
  MacInput,
} from "@/components/form/NetworkInputs";
import type {
  AclPolicySpec,
  EniSpec,
  RoutePolicySpec,
} from "@/api/types";
import {
  useAclPolicies,
  usePutAclPolicy,
  usePutEni,
  usePutRoutePolicy,
  useRoutePolicies,
} from "@/queries/hooks";
import { eniSchema, type EniInput } from "@/lib/schemas";

/* ── Form-only schema ───────────────────────────────────────────
 * The canonical `eniSchema` strips unknown keys on parse (zod's
 * default `.strip()` mode). That means our synthetic reverse-ref
 * fields would silently disappear before `onSubmit` is called.
 *
 * We extend the schema *locally* with the two binding arrays so:
 *   - Zod preserves them in `parsed.data`
 *   - The wire-level schema in `lib/schemas.ts` stays clean and
 *     keeps mirroring the actual dashd ENI shape
 *
 * The bindings are stripped from the wire payload inside the
 * submit handler before PUTing the ENI.
 * ────────────────────────────────────────────────────────────── */
const eniFormSchema = eniSchema.extend({
  __acl_policy_bindings: z.array(z.string().min(1)),
  __route_policy_bindings: z.array(z.string().min(1)),
});

interface EniFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<EniInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

/**
 * Internal form input — adds the two reverse-reference fields on
 * top of the wire shape. These are NOT part of `EniSpec`; they're
 * stripped before PUTing the ENI and applied to the policies in
 * a separate pass.
 */
type EniFormValues = EniInput & {
  __acl_policy_bindings: string[];
  __route_policy_bindings: string[];
};

function emptyDefaults(): EniFormValues {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    vnet_name: "",
    mac_address: "",
    underlay_ip: "",
    admin_state: "up",
    placement_hint_dpu_ids: [],
    __acl_policy_bindings: [],
    __route_policy_bindings: [],
  };
}

function mergeDefaults(initial: Partial<EniInput> | undefined): EniFormValues {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    vnet_name: initial.vnet_name ?? base.vnet_name,
    mac_address: initial.mac_address ?? base.mac_address,
    underlay_ip: initial.underlay_ip ?? base.underlay_ip,
    admin_state: initial.admin_state ?? base.admin_state,
    placement_hint_dpu_ids:
      initial.placement_hint_dpu_ids ?? base.placement_hint_dpu_ids,
    __acl_policy_bindings: base.__acl_policy_bindings,
    __route_policy_bindings: base.__route_policy_bindings,
  };
}

/**
 * Compute the set of policy names whose `eni_names[]` contains
 * `eniName`. Returns an empty array when `eniName` is blank
 * (create mode) or the list hasn't loaded yet.
 */
function policiesReferencingEni<T extends { metadata?: { name?: string }; eni_names?: string[] }>(
  policies: T[] | undefined,
  eniName: string,
): string[] {
  if (!eniName || !policies) return [];
  const out: string[] = [];
  for (const p of policies) {
    const name = p.metadata?.name;
    if (!name) continue;
    if ((p.eni_names ?? []).includes(eniName)) out.push(name);
  }
  return out.sort();
}

/**
 * Sync a single policy's `eni_names[]` list to add or remove the
 * named ENI. Returns the new spec if a change is needed, or null
 * if the policy already has the desired state.
 *
 * We deliberately do a copy + sort so the wire payload is stable
 * and easy to diff on the server side.
 */
function syncPolicyEniNames<T extends { eni_names?: string[] }>(
  policy: T,
  eniName: string,
  shouldContain: boolean,
): T | null {
  const current = new Set(policy.eni_names ?? []);
  const already = current.has(eniName);
  if (shouldContain && already) return null;
  if (!shouldContain && !already) return null;

  if (shouldContain) current.add(eniName);
  else current.delete(eniName);

  return { ...policy, eni_names: Array.from(current).sort() };
}

export function EniForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: EniFormProps) {
  const ns = initial?.metadata?.namespace ?? "default";
  const eniName = initial?.metadata?.name ?? "";

  const putEni = usePutEni();
  const putAcl = usePutAclPolicy();
  const putRoute = usePutRoutePolicy();

  // Live policy lists used to (a) pre-populate the multi-selects
  // in edit mode and (b) look up the full policy spec when we need
  // to PUT a mutated copy back.
  const aclList = useAclPolicies(ns);
  const routeList = useRoutePolicies(ns);

  const isEdit = !!eniName && !titleOverride;
  const title =
    titleOverride ?? (isEdit ? `Edit ENI · ${eniName}` : "Create ENI");

  // Pre-populated reverse-ref bindings (edit mode only).
  const initialAclBindings = useMemo(
    () => policiesReferencingEni(aclList.data?.items, eniName),
    [aclList.data?.items, eniName],
  );
  const initialRouteBindings = useMemo(
    () => policiesReferencingEni(routeList.data?.items, eniName),
    [routeList.data?.items, eniName],
  );

  // Defaults — merge wire payload with the computed bindings.
  const defaults: EniFormValues = useMemo(() => {
    const base = mergeDefaults(initial);
    return {
      ...base,
      __acl_policy_bindings: initialAclBindings,
      __route_policy_bindings: initialRouteBindings,
    };
    // Defaults are re-computed whenever the source ENI or the live
    // policy lists change. The form dialog re-keys on `initial`
    // changes, so a fresh defaults object will reset the form.
  }, [initial, initialAclBindings, initialRouteBindings]);

  return (
    <FormDialog<EniFormValues>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Tenant interface — attached to a vnet, placed on DPUs, optionally bound to ACL / Route policies"
      schema={eniFormSchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        const name = values.metadata.name;
        const namespace = values.metadata.namespace;

        // Strip the synthetic reverse-ref fields from the wire payload.
        const {
          __acl_policy_bindings: nextAclBindings,
          __route_policy_bindings: nextRouteBindings,
          ...eniWire
        } = values;

        // 1. PUT the ENI itself.
        await putEni.mutateAsync({
          ns: namespace,
          name,
          body: eniWire as unknown as EniSpec,
        });

        // 2. Diff ACL bindings and PUT each changed policy.
        const aclAdded = nextAclBindings.filter(
          (p) => !initialAclBindings.includes(p),
        );
        const aclRemoved = initialAclBindings.filter(
          (p) => !nextAclBindings.includes(p),
        );
        const aclSource = aclList.data?.items ?? [];
        const aclMutations: Promise<unknown>[] = [];
        for (const policyName of aclAdded) {
          const found = aclSource.find(
            (p) => p.metadata?.name === policyName,
          );
          if (!found) continue;
          const next = syncPolicyEniNames(found, name, true);
          if (!next) continue;
          aclMutations.push(
            putAcl.mutateAsync({
              ns: namespace,
              name: policyName,
              body: next as AclPolicySpec,
            }),
          );
        }
        for (const policyName of aclRemoved) {
          const found = aclSource.find(
            (p) => p.metadata?.name === policyName,
          );
          if (!found) continue;
          const next = syncPolicyEniNames(found, name, false);
          if (!next) continue;
          aclMutations.push(
            putAcl.mutateAsync({
              ns: namespace,
              name: policyName,
              body: next as AclPolicySpec,
            }),
          );
        }

        // 3. Same diff dance for Route policies.
        const routeAdded = nextRouteBindings.filter(
          (p) => !initialRouteBindings.includes(p),
        );
        const routeRemoved = initialRouteBindings.filter(
          (p) => !nextRouteBindings.includes(p),
        );
        const routeSource = routeList.data?.items ?? [];
        const routeMutations: Promise<unknown>[] = [];
        for (const policyName of routeAdded) {
          const found = routeSource.find(
            (p) => p.metadata?.name === policyName,
          );
          if (!found) continue;
          const next = syncPolicyEniNames(found, name, true);
          if (!next) continue;
          routeMutations.push(
            putRoute.mutateAsync({
              ns: namespace,
              name: policyName,
              body: next as RoutePolicySpec,
            }),
          );
        }
        for (const policyName of routeRemoved) {
          const found = routeSource.find(
            (p) => p.metadata?.name === policyName,
          );
          if (!found) continue;
          const next = syncPolicyEniNames(found, name, false);
          if (!next) continue;
          routeMutations.push(
            putRoute.mutateAsync({
              ns: namespace,
              name: policyName,
              body: next as RoutePolicySpec,
            }),
          );
        }

        // Run policy updates in parallel — they're independent
        // resources and dashd serializes the writes anyway.
        await Promise.all([...aclMutations, ...routeMutations]);

        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create ENI"}
      width="md"
    >
      {({ values, errorAt, setField }) => (
        <>
          {/* Identity */}
          <FieldWrapper
            label="Name"
            htmlFor="eni-name"
            error={errorAt("metadata.name")}
            required
          >
            <input
              id="eni-name"
              type="text"
              value={values.metadata.name}
              onChange={(e) => setField("metadata.name", e.target.value)}
              disabled={isEdit}
              placeholder="e.g. eni-bank-web-01"
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
              aria-invalid={!!errorAt("metadata.name")}
            />
          </FieldWrapper>

          {/* Parent vnet */}
          <ResourceSelect
            kind="vnets"
            ns={values.metadata.namespace}
            label="Vnet"
            value={values.vnet_name}
            onChange={(v) => setField("vnet_name", v)}
            error={errorAt("vnet_name")}
            hint="ENI lives inside this Vnet"
          />

          {/* MAC + Underlay IP */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <MacInput
              label="MAC address"
              value={values.mac_address}
              onChange={(e) => setField("mac_address", e.target.value)}
              error={errorAt("mac_address")}
              required
            />
            <IpInput
              label="Underlay IP"
              version={4}
              value={values.underlay_ip}
              onChange={(e) => setField("underlay_ip", e.target.value)}
              error={errorAt("underlay_ip")}
              required
            />
          </div>

          {/* Admin state */}
          <FieldWrapper
            label="Admin state"
            htmlFor="eni-admin-state"
            error={errorAt("admin_state")}
          >
            <div id="eni-admin-state" role="radiogroup" className="flex gap-2">
              {(["up", "down"] as const).map((s) => (
                <label
                  key={s}
                  className={`flex items-center gap-1.5 px-3 py-1 text-xs rounded-md cursor-pointer border ${
                    values.admin_state === s
                      ? "bg-accent-cyan/15 text-accent-cyan border-accent-cyan/40"
                      : "bg-bg-elevated text-text-secondary border-border hover:text-text-primary"
                  }`}
                >
                  <input
                    type="radio"
                    name="admin_state"
                    value={s}
                    checked={values.admin_state === s}
                    onChange={() => setField("admin_state", s)}
                    className="sr-only"
                  />
                  <span className="font-mono uppercase">{s}</span>
                </label>
              ))}
            </div>
          </FieldWrapper>

          {/* DPU placement hint */}
          <ResourceMultiSelect
            kind="inventory"
            label="Placement DPUs"
            hint="dashd schedules the ENI on these DPUs (HA when ≥ 2)"
            value={values.placement_hint_dpu_ids ?? []}
            onChange={(next) => setField("placement_hint_dpu_ids", next)}
            error={errorAt("placement_hint_dpu_ids")}
          />

          {/* Reverse-reference: ACL policies that bind this ENI */}
          <ResourceMultiSelect
            kind="acl-policies"
            ns={values.metadata.namespace}
            label="ACL Policies"
            hint="Inbound/outbound ACL policies to bind this ENI to (the binding lives in each policy's eni_names[])"
            value={values.__acl_policy_bindings}
            onChange={(next) => setField("__acl_policy_bindings", next)}
          />

          {/* Reverse-reference: Route policies that bind this ENI */}
          <ResourceMultiSelect
            kind="route-policies"
            ns={values.metadata.namespace}
            label="Route Policies"
            hint="Route policies to bind this ENI to (the binding lives in each policy's eni_names[])"
            value={values.__route_policy_bindings}
            onChange={(next) => setField("__route_policy_bindings", next)}
          />

          {/* Labels */}
          <LabelsEditor
            label="Labels (optional)"
            hint="e.g. tenant=bank, tier=web"
            value={values.metadata.labels}
            onChange={(next) => setField("metadata.labels", next)}
          />
        </>
      )}
    </FormDialog>
  );
}