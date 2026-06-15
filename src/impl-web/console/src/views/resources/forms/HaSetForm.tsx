/* ═══════════════════════════════════════════════════════════════
 * HaSetForm — Create / Edit / Clone an HA Set.
 *
 * Rewritten to match dashd's actual write-shape (verified via
 * `scripts/debug-spa-shape.py`):
 *   • `mode` (active_standby | active_active) — set per-HaSet
 *   • `member_dpu_ids[]` — flat list of DPU IDs (≥ 2)
 *   • `virtual_ip` — optional VIP
 *   • `flow_sync_endpoints[]` — optional `udp://host:port` strings
 *
 * The earlier `scope` + `members[].dpu_id+role` shape was wrong;
 * dashd silently dropped those fields. The HA *role* is not
 * tracked per-member by dashd — the controller resolves leader
 * at runtime based on `mode`.
 * ═══════════════════════════════════════════════════════════════ */

import { Plus, Trash2 } from "lucide-react";
import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { ResourceMultiSelect } from "@/components/form/ResourceMultiSelect";
import { FieldWrapper, IpInput } from "@/components/form/NetworkInputs";
import type { HaSetSpec } from "@/api/types";
import { usePutHaSet } from "@/queries/hooks";
import { haSetSchema, type HaSetInput } from "@/lib/schemas";

interface HaSetFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<HaSetInput> & {
    // Older list rows may still carry the legacy `scope`/`members[]`
    // shape. We migrate them into the new shape inside mergeDefaults.
    scope?: string;
    members?: Array<{ dpu_id: string; role?: string }>;
  };
  onSaved?: () => void;
  titleOverride?: string;
}

const HA_MODES: HaSetInput["mode"][] = ["active_standby", "active_active"];

function emptyDefaults(): HaSetInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    mode: "active_standby",
    member_dpu_ids: [],
    virtual_ip: "",
    flow_sync_endpoints: [],
  };
}

function mergeDefaults(initial?: HaSetFormProps["initial"]): HaSetInput {
  const base = emptyDefaults();
  if (!initial) return base;

  // Migrate from any legacy shape — `members[].dpu_id` → flat
  // `member_dpu_ids`. Skips any blank dpu_id entries.
  const migratedDpuIds =
    initial.member_dpu_ids ??
    (initial.members ?? [])
      .map((m) => m.dpu_id)
      .filter((id) => typeof id === "string" && id.length > 0);

  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    mode: initial.mode ?? base.mode,
    member_dpu_ids: migratedDpuIds.length > 0 ? migratedDpuIds : base.member_dpu_ids,
    virtual_ip: initial.virtual_ip ?? base.virtual_ip,
    flow_sync_endpoints:
      initial.flow_sync_endpoints ?? base.flow_sync_endpoints,
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
      subtitle="High-availability group of DPUs — at least 2 member DPUs required"
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
      {({ values, errorAt, setField }) => {
        function addSyncEndpoint() {
          setField("flow_sync_endpoints", [
            ...(values.flow_sync_endpoints ?? []),
            "",
          ]);
        }
        function removeSyncEndpoint(idx: number) {
          setField(
            "flow_sync_endpoints",
            (values.flow_sync_endpoints ?? []).filter((_, i) => i !== idx),
          );
        }
        function updateSyncEndpoint(idx: number, next: string) {
          const arr = [...(values.flow_sync_endpoints ?? [])];
          arr[idx] = next;
          setField("flow_sync_endpoints", arr);
        }

        return (
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

            {/* Mode (per-HaSet, not per-member) */}
            <FieldWrapper
              label="Mode"
              htmlFor="ha-mode"
              error={errorAt("mode")}
              required
              hint="active_standby = one DPU active; active_active = both forward"
            >
              <div id="ha-mode" role="radiogroup" className="flex gap-2 pt-1">
                {HA_MODES.map((m) => (
                  <label
                    key={m}
                    className={`flex items-center gap-1.5 px-3 py-1 text-xs rounded-md cursor-pointer border ${
                      values.mode === m
                        ? "bg-accent-cyan/15 text-accent-cyan border-accent-cyan/40"
                        : "bg-bg-elevated text-text-secondary border-border hover:text-text-primary"
                    }`}
                  >
                    <input
                      type="radio"
                      name="ha-mode"
                      value={m}
                      checked={values.mode === m}
                      onChange={() => setField("mode", m)}
                      className="sr-only"
                    />
                    <span className="font-mono">{m}</span>
                  </label>
                ))}
              </div>
            </FieldWrapper>

            {/* Member DPUs (flat list — pick ≥ 2 from inventory) */}
            <ResourceMultiSelect
              kind="inventory"
              label="Member DPUs"
              hint="Select 2 or more DPUs that share state for this HA Set"
              value={values.member_dpu_ids}
              onChange={(next) => setField("member_dpu_ids", next)}
              error={errorAt("member_dpu_ids")}
            />

            {/* Virtual IP */}
            <IpInput
              label="Virtual IP (optional)"
              version={4}
              value={values.virtual_ip ?? ""}
              onChange={(e) => setField("virtual_ip", e.target.value)}
              error={errorAt("virtual_ip")}
            />

            {/* Flow-sync endpoints */}
            <FieldWrapper
              label={`Flow-sync endpoints (${(values.flow_sync_endpoints ?? []).length})`}
              htmlFor="ha-flow-sync"
              error={errorAt("flow_sync_endpoints")}
              hint="Optional. e.g. udp://dpu-sim-01:4789 — one per member typically."
            >
              <div className="space-y-1.5">
                {(values.flow_sync_endpoints ?? []).map((ep, idx) => (
                  <div key={idx} className="flex items-end gap-2">
                    <div className="flex-1">
                      <input
                        id={`ha-fs-${idx}`}
                        type="text"
                        value={ep}
                        onChange={(e) => updateSyncEndpoint(idx, e.target.value)}
                        placeholder="udp://dpu-sim-01:4789"
                        className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
                        aria-invalid={
                          !!errorAt(`flow_sync_endpoints.${idx}`)
                        }
                      />
                      {errorAt(`flow_sync_endpoints.${idx}`) && (
                        <span
                          className="text-xs text-accent-red"
                          role="alert"
                        >
                          {errorAt(`flow_sync_endpoints.${idx}`)}
                        </span>
                      )}
                    </div>
                    <button
                      type="button"
                      onClick={() => removeSyncEndpoint(idx)}
                      aria-label={`Remove endpoint ${idx + 1}`}
                      className="p-1.5 text-text-muted hover:text-accent-red transition-colors"
                      title="Remove endpoint"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  onClick={addSyncEndpoint}
                  className="flex items-center gap-1 text-xs text-accent-cyan hover:text-accent-cyan/80"
                >
                  <Plus size={12} />
                  Add endpoint
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