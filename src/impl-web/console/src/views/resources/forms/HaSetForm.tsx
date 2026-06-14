/* ═══════════════════════════════════════════════════════════════
 * HaSetForm — Create / Edit / Clone an HA Set.
 *
 * Members[] is an array of { dpu_id, role }, min 2, with unique
 * dpu_ids enforced. dpu_id is sourced from inventory.
 *
 * Added in A-IF3-G7.
 * ═══════════════════════════════════════════════════════════════ */

import { Plus, Trash2 } from "lucide-react";
import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { ResourceSelect } from "@/components/form/ResourceSelect";
import { FieldWrapper, IpInput } from "@/components/form/NetworkInputs";
import type { HaSetSpec } from "@/api/types";
import { usePutHaSet } from "@/queries/hooks";
import {
  haSetSchema,
  type HaSetInput,
  type HaSetMemberInput,
} from "@/lib/schemas";

interface HaSetFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<HaSetInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

const ROLES: HaSetMemberInput["role"][] = [
  "ACTIVE",
  "STANDBY",
  "ACTIVE_ACTIVE",
  "WITNESS",
];

function emptyDefaults(): HaSetInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    scope: "appliance",
    members: [
      { dpu_id: "", role: "ACTIVE" },
      { dpu_id: "", role: "STANDBY" },
    ],
    virtual_ip: "",
  };
}

function mergeDefaults(initial?: Partial<HaSetInput>): HaSetInput {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    scope: initial.scope ?? base.scope,
    members:
      initial.members && initial.members.length >= 2
        ? initial.members
        : base.members,
    virtual_ip: initial.virtual_ip ?? base.virtual_ip,
  };
}

export function HaSetForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: HaSetFormProps) {
  const mutation = usePutHaSet();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;
  const title =
    titleOverride ??
    (isEdit
      ? `Edit HA Set · ${defaults.metadata.name}`
      : "Create HA Set");

  return (
    <FormDialog<HaSetInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="High-availability group of DPUs — at least 2 members required"
      schema={haSetSchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as HaSetSpec,
        });
        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create HA Set"}
      width="md"
    >
      {({ values, errorAt, setField }) => (
        <>
          <FieldWrapper
            label="Name"
            htmlFor="ha-name"
            error={errorAt("metadata.name")}
            required
          >
            <input
              id="ha-name"
              type="text"
              value={values.metadata.name}
              onChange={(e) => setField("metadata.name", e.target.value)}
              disabled={isEdit}
              placeholder="e.g. ha-bank-prod"
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
              aria-invalid={!!errorAt("metadata.name")}
            />
          </FieldWrapper>

          <FieldWrapper
            label="Scope"
            htmlFor="ha-scope"
            error={errorAt("scope")}
            required
            hint="appliance | zone | region"
          >
            <input
              id="ha-scope"
              type="text"
              list="ha-scopes"
              value={values.scope}
              onChange={(e) => setField("scope", e.target.value)}
              placeholder="appliance"
              className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
              aria-invalid={!!errorAt("scope")}
            />
            <datalist id="ha-scopes">
              <option value="appliance" />
              <option value="zone" />
              <option value="region" />
            </datalist>
          </FieldWrapper>

          {/* Members editor */}
          <FieldWrapper
            label={`Members (${values.members.length})`}
            htmlFor="ha-members"
            error={errorAt("members")}
            required
          >
            <div className="space-y-1.5">
              {values.members.map((m, idx) => (
                <div key={idx} className="flex items-end gap-2">
                  <div className="flex-1">
                    <ResourceSelect
                      kind="inventory"
                      label={`Member ${idx + 1} · DPU`}
                      value={m.dpu_id}
                      onChange={(v) => setField(`members.${idx}.dpu_id`, v)}
                      error={errorAt(`members.${idx}.dpu_id`)}
                    />
                  </div>
                  <div className="w-32">
                    <FieldWrapper
                      label="Role"
                      htmlFor={`ha-member-${idx}-role`}
                      error={errorAt(`members.${idx}.role`)}
                    >
                      <select
                        id={`ha-member-${idx}-role`}
                        value={m.role}
                        onChange={(e) =>
                          setField(`members.${idx}.role`, e.target.value)
                        }
                        className="w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
                      >
                        {ROLES.map((r) => (
                          <option key={r} value={r}>
                            {r}
                          </option>
                        ))}
                      </select>
                    </FieldWrapper>
                  </div>
                  <button
                    type="button"
                    onClick={() => {
                      const next = values.members.filter((_, i) => i !== idx);
                      setField("members", next);
                    }}
                    disabled={values.members.length <= 2}
                    aria-label={`Remove member ${idx + 1}`}
                    className="mb-1 p-1.5 text-text-muted hover:text-accent-red transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
                    title={
                      values.members.length <= 2
                        ? "HA Set requires ≥ 2 members"
                        : "Remove member"
                    }
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              ))}
              <button
                type="button"
                onClick={() =>
                  setField("members", [
                    ...values.members,
                    { dpu_id: "", role: "STANDBY" as const },
                  ])
                }
                className="flex items-center gap-1 text-xs text-accent-cyan hover:text-accent-cyan/80"
              >
                <Plus size={12} />
                Add member
              </button>
            </div>
          </FieldWrapper>

          <IpInput
            label="Virtual IP (optional)"
            version={4}
            value={values.virtual_ip ?? ""}
            onChange={(e) => setField("virtual_ip", e.target.value)}
            error={errorAt("virtual_ip")}
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