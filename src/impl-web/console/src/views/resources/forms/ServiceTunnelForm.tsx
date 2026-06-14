/* ═══════════════════════════════════════════════════════════════
 * ServiceTunnelForm — Create / Edit / Clone a ServiceTunnel.
 *
 * Underlay-IP tunnel with free-form `params` map. The most common
 * params keys (action, mtu, nat_pool) are surfaced as suggestions.
 *
 * Added in A-IF3-G4.
 * ═══════════════════════════════════════════════════════════════ */

import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import {
  FieldWrapper,
  IpInput,
  VniInput,
} from "@/components/form/NetworkInputs";
import type { ServiceTunnelSpec } from "@/api/types";
import { usePutServiceTunnel } from "@/queries/hooks";
import {
  serviceTunnelSchema,
  type ServiceTunnelInput,
} from "@/lib/schemas";

interface ServiceTunnelFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<ServiceTunnelInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

function emptyDefaults(): ServiceTunnelInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    local_underlay_ip: "",
    remote_underlay_ip: "",
    vni: 200,
    params: {},
  };
}

function mergeDefaults(
  initial?: Partial<ServiceTunnelInput>,
): ServiceTunnelInput {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    local_underlay_ip: initial.local_underlay_ip ?? base.local_underlay_ip,
    remote_underlay_ip: initial.remote_underlay_ip ?? base.remote_underlay_ip,
    vni: initial.vni ?? base.vni,
    params: initial.params ?? base.params,
  };
}

const SUGGESTED_PARAMS = ["action", "mtu", "nat_pool", "encap_type"];

export function ServiceTunnelForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: ServiceTunnelFormProps) {
  const mutation = usePutServiceTunnel();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;
  const title =
    titleOverride ??
    (isEdit
      ? `Edit Service Tunnel · ${defaults.metadata.name}`
      : "Create Service Tunnel");

  return (
    <FormDialog<ServiceTunnelInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Underlay tunnel for NAT / cross-region / DDoS-scrub traffic"
      schema={serviceTunnelSchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as ServiceTunnelSpec,
        });
        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create Tunnel"}
      width="md"
    >
      {({ values, errorAt, setField }) => (
        <>
          <FieldWrapper
            label="Name"
            htmlFor="tunnel-name"
            error={errorAt("metadata.name")}
            required
          >
            <input
              id="tunnel-name"
              type="text"
              value={values.metadata.name}
              onChange={(e) => setField("metadata.name", e.target.value)}
              disabled={isEdit}
              placeholder="e.g. st-internet-egress"
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
              aria-invalid={!!errorAt("metadata.name")}
            />
          </FieldWrapper>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <IpInput
              label="Local underlay IP"
              version={4}
              value={values.local_underlay_ip}
              onChange={(e) =>
                setField("local_underlay_ip", e.target.value)
              }
              error={errorAt("local_underlay_ip")}
              required
            />
            <IpInput
              label="Remote underlay IP"
              version={4}
              value={values.remote_underlay_ip}
              onChange={(e) =>
                setField("remote_underlay_ip", e.target.value)
              }
              error={errorAt("remote_underlay_ip")}
              required
            />
          </div>

          <VniInput
            label="Tunnel VNI"
            value={values.vni}
            onChange={(e) =>
              setField("vni", Number.parseInt(e.target.value, 10) || 0)
            }
            error={errorAt("vni")}
            required
          />

          <LabelsEditor
            label="Tunnel params"
            hint="Common keys: action, mtu, nat_pool, encap_type"
            suggestedKeys={SUGGESTED_PARAMS}
            value={values.params}
            onChange={(next) => setField("params", next)}
            emptyText="No params — click + to add (e.g. action=nat)"
          />

          <LabelsEditor
            label="Labels (optional)"
            value={values.metadata.labels}
            onChange={(next) => setField("metadata.labels", next)}
          />
        </>
      )}
    </FormDialog>
  );
}