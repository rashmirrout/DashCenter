/* ═══════════════════════════════════════════════════════════════
 * VnetMappingForm — Create / Edit / Clone a VnetMapping.
 *
 * Overlay IP ↔ underlay IP mapping. When action == "service_tunnel"
 * the form conditionally shows a tunnel-select field for
 * params.tunnel.
 *
 * Important dashd quirk (verified via scripts/debug-spa-shape.py
 * test 3): dashd IGNORES the user-supplied `metadata.name` and
 * derives the mapping's name server-side as
 * `{vnet_name}-{ip_address}`. The form now mirrors that derivation
 * client-side so the URL/PUT name matches what dashd actually
 * stores — without this the post-PUT GET would 404.
 *
 * Added in A-IF3-G3.
 * ═══════════════════════════════════════════════════════════════ */

import { useEffect } from "react";
import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import {
  FieldWrapper,
  IpInput,
  MacInput,
} from "@/components/form/NetworkInputs";
import type { VnetMappingSpec } from "@/api/types";
import { usePutVnetMapping } from "@/queries/hooks";
import { vnetMappingSchema, type VnetMappingInput } from "@/lib/schemas";

/** Replicates dashd's server-side name-derivation for vnet-mappings.
 *  Returns an empty string until both inputs are present. */
function derivedMappingName(vnetName: string, ipAddress: string): string {
  if (!vnetName || !ipAddress) return "";
  return `${vnetName}-${ipAddress}`;
}

interface VnetMappingFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<VnetMappingInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

function emptyDefaults(): VnetMappingInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    vnet_name: "",
    ip_address: "",
    underlay_ip: "",
    mac_address: "",
    action: "vnet_encap",
    params: {},
  };
}

function mergeDefaults(initial?: Partial<VnetMappingInput>): VnetMappingInput {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    vnet_name: initial.vnet_name ?? base.vnet_name,
    ip_address: initial.ip_address ?? base.ip_address,
    underlay_ip: initial.underlay_ip ?? base.underlay_ip,
    mac_address: initial.mac_address ?? base.mac_address,
    action: initial.action ?? base.action,
    params: initial.params ?? base.params,
  };
}

export function VnetMappingForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: VnetMappingFormProps) {
  const mutation = usePutVnetMapping();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;
  const title =
    titleOverride ??
    (isEdit
      ? `Edit Vnet Mapping · ${defaults.metadata.name}`
      : "Create Vnet Mapping");

  return (
    <FormDialog<VnetMappingInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Overlay-IP → underlay-IP entry in a Vnet's mapping table"
      schema={vnetMappingSchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as VnetMappingSpec,
        });
        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create Mapping"}
      width="md"
    >
      {({ values, errorAt, setField }) => {
        // Auto-derive the name in create mode so it matches what
        // dashd actually persists (`{vnet_name}-{ip_address}`).
        // Edit mode keeps whatever name was loaded into `initial`.
        // eslint-disable-next-line react-hooks/rules-of-hooks
        useEffect(() => {
          if (isEdit) return;
          const next = derivedMappingName(
            values.vnet_name,
            values.ip_address ?? "",
          );
          if (next && next !== values.metadata.name) {
            setField("metadata.name", next);
          }
        }, [values.vnet_name, values.ip_address, isEdit, values.metadata.name, setField]);

        return (
          <>
            <FieldWrapper
              label="Name (auto-derived)"
              htmlFor="mapping-name"
              error={errorAt("metadata.name")}
              required
              hint="dashd uses `{vnet_name}-{ip_address}` as the key — this field is read-only and reflects what will be stored"
            >
              <input
                id="mapping-name"
                type="text"
                value={values.metadata.name}
                readOnly
                disabled
                placeholder="— pick a Vnet and overlay IP below —"
                className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-muted font-mono placeholder:text-text-muted focus:outline-none cursor-not-allowed"
                aria-invalid={!!errorAt("metadata.name")}
              />
            </FieldWrapper>

          <ResourceSelect
            kind="vnets"
            ns={values.metadata.namespace}
            label="Vnet"
            value={values.vnet_name}
            onChange={(v) => setField("vnet_name", v)}
            error={errorAt("vnet_name")}
            hint="Mapping belongs to this Vnet's table"
          />

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <IpInput
              label="Overlay IP (ip_address)"
              version={4}
              value={values.ip_address}
              onChange={(e) => setField("ip_address", e.target.value)}
              error={errorAt("ip_address")}
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

          <MacInput
            label="MAC address"
            value={values.mac_address}
            onChange={(e) => setField("mac_address", e.target.value)}
            error={errorAt("mac_address")}
            required
          />

          {/* Action radio */}
          <FieldWrapper
            label="Action"
            htmlFor="mapping-action"
            error={errorAt("action")}
            required
            hint="`vnet_encap` for normal overlay traffic; `service_tunnel` to route via a ServiceTunnel"
          >
            <div id="mapping-action" role="radiogroup" className="flex gap-2">
              {(["vnet_encap", "service_tunnel"] as const).map((a) => (
                <label
                  key={a}
                  className={`flex items-center gap-1.5 px-3 py-1 text-xs rounded-md cursor-pointer border ${
                    values.action === a
                      ? "bg-accent-cyan/15 text-accent-cyan border-accent-cyan/40"
                      : "bg-bg-elevated text-text-secondary border-border hover:text-text-primary"
                  }`}
                >
                  <input
                    type="radio"
                    name="action"
                    value={a}
                    checked={values.action === a}
                    onChange={() => {
                      setField("action", a);
                      // Reset params.tunnel when switching back to vnet_encap
                      if (a === "vnet_encap") {
                        setField("params", {});
                      }
                    }}
                    className="sr-only"
                  />
                  <span className="font-mono">{a}</span>
                </label>
              ))}
            </div>
          </FieldWrapper>

          {/* Conditional tunnel selector */}
          {values.action === "service_tunnel" && (
            <ResourceSelect
              kind="service-tunnels"
              ns={values.metadata.namespace}
              label="Service Tunnel (params.tunnel)"
              value={values.params?.tunnel ?? ""}
              onChange={(v) =>
                setField("params", { ...(values.params ?? {}), tunnel: v })
              }
              error={errorAt("params.tunnel")}
              hint="Traffic for this overlay IP is routed via this tunnel"
            />
          )}

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
