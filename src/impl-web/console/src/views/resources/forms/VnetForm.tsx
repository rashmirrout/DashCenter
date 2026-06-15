/* ═══════════════════════════════════════════════════════════════
 * VnetForm — Create / Edit / Clone a Vnet.
 *
 * Schema-validated dialog rendered via <FormDialog>. Submits via
 * usePutVnet which auto-denormalises into the dashd wire format.
 *
 * Added in A-IF3-G1.
 * ═══════════════════════════════════════════════════════════════ */

import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import {
  FieldWrapper,
  MacInput,
  VniInput,
} from "@/components/form/NetworkInputs";
import type { VnetSpec } from "@/api/types";
import { usePutVnet } from "@/queries/hooks";
import { vnetSchema, type VnetInput } from "@/lib/schemas";

interface VnetFormProps {
  open: boolean;
  onClose: () => void;
  /** When provided, the form opens in Edit mode (name disabled).
   *  When omitted, opens in Create mode with empty defaults. */
  initial?: Partial<VnetInput>;
  /** Optional success callback (mutation already shows a toast). */
  onSaved?: () => void;
  /** Override title (used by Clone mode). */
  titleOverride?: string;
}

function emptyDefaults(): VnetInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    vni: 100,
    gw_mac: "",
  };
}

function mergeDefaults(initial?: Partial<VnetInput>): VnetInput {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    vni: initial.vni ?? base.vni,
    gw_mac: initial.gw_mac ?? base.gw_mac,
  };
}

export function VnetForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: VnetFormProps) {
  const mutation = usePutVnet();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;

  const title =
    titleOverride ??
    (isEdit ? `Edit Vnet · ${defaults.metadata.name}` : "Create Vnet");

  return (
    <FormDialog<VnetInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Overlay network — VNI scope for all attached ENIs"
      schema={vnetSchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as VnetSpec,
        });
        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create Vnet"}
      width="md"
    >
      {({ values, errorAt, setField }) => (
        <>
          {/* Identity */}
          <FieldWrapper
            label="Name"
            htmlFor="vnet-name"
            error={errorAt("metadata.name")}
            required
            hint="Lowercase alphanumeric + dashes — used as the unique identifier"
          >
            <input
              id="vnet-name"
              type="text"
              value={values.metadata.name}
              onChange={(e) => setField("metadata.name", e.target.value)}
              disabled={isEdit}
              placeholder="e.g. bank-prod-web"
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
              aria-invalid={!!errorAt("metadata.name")}
            />
          </FieldWrapper>

          <FieldWrapper
            label="Namespace"
            htmlFor="vnet-namespace"
            error={errorAt("metadata.namespace")}
            hint="dashd partition — usually `default`"
          >
            <input
              id="vnet-namespace"
              type="text"
              value={values.metadata.namespace}
              onChange={(e) => setField("metadata.namespace", e.target.value)}
              disabled={isEdit}
              placeholder="default"
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
            />
          </FieldWrapper>

          {/* VNI */}
          <VniInput
            label="VNI"
            value={values.vni}
            onChange={(e) =>
              setField("vni", Number.parseInt(e.target.value, 10) || 0)
            }
            error={errorAt("vni")}
            required
          />

          {/* Optional gateway MAC */}
          <MacInput
            label="Gateway MAC (optional)"
            value={values.gw_mac ?? ""}
            onChange={(e) => setField("gw_mac", e.target.value)}
            error={errorAt("gw_mac")}
          />

          {/* Labels */}
          <LabelsEditor
            label="Labels (optional)"
            hint="Free-form key/value metadata (e.g. tenant=bank, tier=web)"
            value={values.metadata.labels}
            onChange={(next) => setField("metadata.labels", next)}
          />

          <p className="text-[10px] text-text-muted">
            Note: address space is derived automatically from vnet-mappings
            and attached ENIs — it&apos;s not configured here.
          </p>
        </>
      )}
    </FormDialog>
  );
}