/* ═══════════════════════════════════════════════════════════════
 * EniForm — Create / Edit / Clone an ENI.
 *
 * Live cross-references:
 *   - `vnet_name` from `useResourceList('vnets')`
 *   - `placement_hint_dpu_ids[]` from `useResourceList('inventory')`
 *
 * Added in A-IF3-G2.
 * ═══════════════════════════════════════════════════════════════ */

import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { ResourceMultiSelect } from "@/components/form/ResourceMultiSelect";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import {
  FieldWrapper,
  IpInput,
  MacInput,
} from "@/components/form/NetworkInputs";
import type { EniSpec } from "@/api/types";
import { usePutEni } from "@/queries/hooks";
import { eniSchema, type EniInput } from "@/lib/schemas";

interface EniFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<EniInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

function emptyDefaults(): EniInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    vnet_name: "",
    mac_address: "",
    underlay_ip: "",
    admin_state: "up",
    placement_hint_dpu_ids: [],
  };
}

function mergeDefaults(initial?: Partial<EniInput>): EniInput {
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
  };
}

export function EniForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: EniFormProps) {
  const mutation = usePutEni();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;
  const title =
    titleOverride ??
    (isEdit ? `Edit ENI · ${defaults.metadata.name}` : "Create ENI");

  return (
    <FormDialog<EniInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Tenant interface — attached to a vnet and (optionally) placed on DPUs"
      schema={eniSchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as EniSpec,
        });
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