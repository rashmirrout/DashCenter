/* ═══════════════════════════════════════════════════════════════
 * AclPolicyForm — Create / Edit / Clone an ACL Policy.
 *
 * Includes a nested <AclRuleEditor> sub-form for each rule in
 * the rules[] array. Rules are auto-sorted by priority on render
 * but the underlying order matches the wire format.
 *
 * Added in A-IF3-G5 (complex).
 * ═══════════════════════════════════════════════════════════════ */

import { Plus, Trash2, ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";
import { FormDialog } from "@/components/form/FormDialog";
import { LabelsEditor } from "@/components/form/LabelsEditor";
import { PortListEditor } from "@/components/form/PortListEditor";
import { PrefixListEditor } from "@/components/form/PrefixListEditor";
import { ProtocolEditor } from "@/components/form/ProtocolEditor";
import { ResourceMultiSelect } from "@/components/form/ResourceMultiSelect";
import { FieldWrapper } from "@/components/form/NetworkInputs";
import type { AclPolicySpec } from "@/api/types";
import { usePutAclPolicy } from "@/queries/hooks";
import {
  aclPolicySchema,
  type AclPolicyInput,
  type AclRuleInput,
} from "@/lib/schemas";
import { cn } from "@/lib/cn";

interface AclPolicyFormProps {
  open: boolean;
  onClose: () => void;
  initial?: Partial<AclPolicyInput>;
  onSaved?: () => void;
  titleOverride?: string;
}

function defaultRule(priority = 100): AclRuleInput {
  return {
    priority,
    action: "allow",
    description: "",
    src_prefixes: [],
    dst_prefixes: [],
    src_ports: [],
    dst_ports: [],
    protocols: [],
  };
}

function emptyDefaults(): AclPolicyInput {
  return {
    metadata: { namespace: "default", name: "", labels: {} },
    stage: "inbound",
    eni_names: [],
    rules: [defaultRule(100)],
  };
}

function mergeDefaults(initial?: Partial<AclPolicyInput>): AclPolicyInput {
  const base = emptyDefaults();
  if (!initial) return base;
  return {
    metadata: {
      namespace: initial.metadata?.namespace ?? base.metadata.namespace,
      name: initial.metadata?.name ?? base.metadata.name,
      labels: initial.metadata?.labels ?? base.metadata.labels,
    },
    stage: initial.stage ?? base.stage,
    eni_names: initial.eni_names ?? base.eni_names,
    rules: initial.rules && initial.rules.length > 0 ? initial.rules : base.rules,
  };
}

export function AclPolicyForm({
  open,
  onClose,
  initial,
  onSaved,
  titleOverride,
}: AclPolicyFormProps) {
  const mutation = usePutAclPolicy();
  const defaults = mergeDefaults(initial);
  const isEdit = !!initial?.metadata?.name && !titleOverride;
  const title =
    titleOverride ??
    (isEdit
      ? `Edit ACL Policy · ${defaults.metadata.name}`
      : "Create ACL Policy");

  return (
    <FormDialog<AclPolicyInput>
      open={open}
      onClose={onClose}
      title={title}
      subtitle="Inbound or outbound stage — bound to one or more ENIs"
      schema={aclPolicySchema}
      defaultValues={defaults}
      onSubmit={async (values) => {
        await mutation.mutateAsync({
          ns: values.metadata.namespace,
          name: values.metadata.name,
          body: values as unknown as AclPolicySpec,
        });
        onSaved?.();
      }}
      submitLabel={isEdit ? "Save changes" : "Create ACL Policy"}
      width="lg"
    >
      {({ values, errorAt, setField }) => {
        // Sort indices by priority for display so the UI reflects
        // dashd's match-order. We don't mutate the underlying array.
        const sortedIndices = values.rules
          .map((_r, idx) => idx)
          .sort((a, b) => values.rules[a]!.priority - values.rules[b]!.priority);

        function addRule() {
          // Suggest next priority = max+10, rounded up to nearest 10.
          const maxPri = values.rules.reduce(
            (m, r) => Math.max(m, r.priority),
            0,
          );
          const nextPri = Math.max(100, Math.ceil((maxPri + 10) / 10) * 10);
          setField("rules", [...values.rules, defaultRule(nextPri)]);
        }

        function removeRule(idx: number) {
          if (values.rules.length <= 1) return; // schema requires ≥ 1
          setField(
            "rules",
            values.rules.filter((_, i) => i !== idx),
          );
        }

        return (
          <>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              <FieldWrapper
                label="Name"
                htmlFor="acl-name"
                error={errorAt("metadata.name")}
                required
              >
                <input
                  id="acl-name"
                  type="text"
                  value={values.metadata.name}
                  onChange={(e) => setField("metadata.name", e.target.value)}
                  disabled={isEdit}
                  placeholder="e.g. acl-bank-web-inbound"
                  className="w-full px-3 py-1.5 text-sm bg-bg-elevated border border-border rounded-lg text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50"
                  aria-invalid={!!errorAt("metadata.name")}
                />
              </FieldWrapper>

              <FieldWrapper
                label="Stage"
                htmlFor="acl-stage"
                error={errorAt("stage")}
                required
              >
                <div id="acl-stage" role="radiogroup" className="flex gap-2 pt-1">
                  {(["inbound", "outbound"] as const).map((s) => (
                    <label
                      key={s}
                      className={cn(
                        "flex items-center gap-1.5 px-3 py-1 text-xs rounded-md cursor-pointer border",
                        values.stage === s
                          ? "bg-accent-cyan/15 text-accent-cyan border-accent-cyan/40"
                          : "bg-bg-elevated text-text-secondary border-border hover:text-text-primary",
                      )}
                    >
                      <input
                        type="radio"
                        name="stage"
                        value={s}
                        checked={values.stage === s}
                        onChange={() => setField("stage", s)}
                        className="sr-only"
                      />
                      <span className="font-mono">{s}</span>
                    </label>
                  ))}
                </div>
              </FieldWrapper>
            </div>

            <ResourceMultiSelect
              kind="enis"
              ns={values.metadata.namespace}
              label="Bound ENIs"
              hint="The ACL stage applies to traffic on these ENIs"
              value={values.eni_names}
              onChange={(next) => setField("eni_names", next)}
              error={errorAt("eni_names")}
            />

            {/* Rules editor */}
            <FieldWrapper
              label={`Rules (${values.rules.length}) — sorted by priority`}
              htmlFor="acl-rules"
              error={errorAt("rules")}
              required
            >
              <div className="space-y-2">
                {sortedIndices.map((idx) => (
                  <AclRuleCard
                    key={idx}
                    idx={idx}
                    rule={values.rules[idx]!}
                    errorAt={errorAt}
                    setField={setField}
                    onRemove={() => removeRule(idx)}
                    canRemove={values.rules.length > 1}
                  />
                ))}
                <button
                  type="button"
                  onClick={addRule}
                  className="flex items-center gap-1 text-xs text-accent-cyan hover:text-accent-cyan/80"
                >
                  <Plus size={12} />
                  Add rule
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

/* ── Per-rule card (collapsible) ──────────────────────────── */

interface AclRuleCardProps {
  idx: number;
  rule: AclRuleInput;
  errorAt: (path: string) => string | undefined;
  setField: (path: string, value: unknown) => void;
  onRemove: () => void;
  canRemove: boolean;
}

const ACL_ACTIONS: AclRuleInput["action"][] = [
  "allow",
  "deny",
  "allow_and_continue",
];

function AclRuleCard({
  idx,
  rule,
  errorAt,
  setField,
  onRemove,
  canRemove,
}: AclRuleCardProps) {
  const [collapsed, setCollapsed] = useState(true);
  // Aggregate every error path that could surface inside this rule
  // card so submit-time validation reliably auto-expands it.
  // (Without this, a "must constrain at least one of..." error on
  // a collapsed card would render invisibly above the chevron.)
  const hasError =
    !!errorAt(`rules.${idx}`) ||
    !!errorAt(`rules.${idx}.priority`) ||
    !!errorAt(`rules.${idx}.action`) ||
    !!errorAt(`rules.${idx}.description`) ||
    !!errorAt(`rules.${idx}.src_prefixes`) ||
    !!errorAt(`rules.${idx}.dst_prefixes`) ||
    !!errorAt(`rules.${idx}.src_ports`) ||
    !!errorAt(`rules.${idx}.dst_ports`) ||
    !!errorAt(`rules.${idx}.protocols`);

  // When validation surfaces an error for this card, force-expand
  // so the offending field is visible. We only flip from collapsed
  // → expanded; never the other way (so the user doesn't lose
  // their place if they manually collapsed an erroring card).
  useEffect(() => {
    if (hasError && collapsed) setCollapsed(false);
    // Intentionally not depending on `collapsed` — we want the
    // one-way transition to fire only on error appearance.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hasError]);

  const ruleErr = errorAt(`rules.${idx}`);
  const actionColor =
    rule.action === "allow"
      ? "text-accent-green"
      : rule.action === "deny"
        ? "text-accent-red"
        : "text-accent-cyan";

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
          aria-label={collapsed ? "Expand rule" : "Collapse rule"}
          aria-expanded={!collapsed}
        >
          {collapsed ? (
            <ChevronRight size={14} />
          ) : (
            <ChevronDown size={14} />
          )}
        </button>
        <span className="text-xs font-mono text-text-muted shrink-0">
          pri={rule.priority}
        </span>
        <span className={cn("text-xs font-mono uppercase shrink-0", actionColor)}>
          {rule.action}
        </span>
        <span className="text-xs text-text-secondary truncate flex-1">
          {rule.description || (
            <span className="italic text-text-muted">no description</span>
          )}
        </span>
        <button
          type="button"
          onClick={onRemove}
          disabled={!canRemove}
          aria-label={`Remove rule ${idx + 1}`}
          className="p-1 text-text-muted hover:text-accent-red disabled:opacity-30"
          title={canRemove ? "Remove rule" : "At least one rule required"}
        >
          <Trash2 size={12} />
        </button>
      </div>

      {!collapsed && (
        <div className="p-3 pt-0 border-t border-border space-y-3">
          {/* Priority + Action */}
          <div className="grid grid-cols-2 gap-2">
            <FieldWrapper
              label="Priority"
              htmlFor={`rule-${idx}-priority`}
              error={errorAt(`rules.${idx}.priority`)}
              required
            >
              <input
                id={`rule-${idx}-priority`}
                type="number"
                min={1}
                max={65535}
                value={rule.priority}
                onChange={(e) =>
                  setField(
                    `rules.${idx}.priority`,
                    Number.parseInt(e.target.value, 10) || 0,
                  )
                }
                className="w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
              />
            </FieldWrapper>
            <FieldWrapper
              label="Action"
              htmlFor={`rule-${idx}-action`}
              error={errorAt(`rules.${idx}.action`)}
              required
            >
              <select
                id={`rule-${idx}-action`}
                value={rule.action}
                onChange={(e) => setField(`rules.${idx}.action`, e.target.value)}
                className="w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
              >
                {ACL_ACTIONS.map((a) => (
                  <option key={a} value={a}>
                    {a}
                  </option>
                ))}
              </select>
            </FieldWrapper>
          </div>

          {/* Source / destination prefixes */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <PrefixListEditor
              label="Source prefixes"
              value={rule.src_prefixes ?? []}
              onChange={(next) => setField(`rules.${idx}.src_prefixes`, next)}
              error={errorAt(`rules.${idx}.src_prefixes`)}
              placeholder="0.0.0.0/0"
            />
            <PrefixListEditor
              label="Destination prefixes"
              value={rule.dst_prefixes ?? []}
              onChange={(next) => setField(`rules.${idx}.dst_prefixes`, next)}
              error={errorAt(`rules.${idx}.dst_prefixes`)}
              placeholder="10.0.0.0/24"
            />
          </div>

          {/* Source / destination ports */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
            <PortListEditor
              label="Source ports"
              value={rule.src_ports ?? []}
              onChange={(next) => setField(`rules.${idx}.src_ports`, next)}
              error={errorAt(`rules.${idx}.src_ports`)}
            />
            <PortListEditor
              label="Destination ports"
              value={rule.dst_ports ?? []}
              onChange={(next) => setField(`rules.${idx}.dst_ports`, next)}
              error={errorAt(`rules.${idx}.dst_ports`)}
              placeholder="443"
            />
          </div>

          {/* Protocols */}
          <ProtocolEditor
            value={rule.protocols ?? []}
            onChange={(next) => setField(`rules.${idx}.protocols`, next)}
            error={errorAt(`rules.${idx}.protocols`)}
          />

          {/* Description */}
          <FieldWrapper
            label="Description"
            htmlFor={`rule-${idx}-desc`}
            error={errorAt(`rules.${idx}.description`)}
          >
            <textarea
              id={`rule-${idx}-desc`}
              value={rule.description ?? ""}
              onChange={(e) =>
                setField(`rules.${idx}.description`, e.target.value)
              }
              rows={2}
              placeholder="What does this rule do? (free-form text)"
              className="w-full px-2 py-1 text-xs bg-bg-elevated border border-border rounded text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 resize-y"
            />
          </FieldWrapper>

          {/* Rule-level cross-field error (e.g. "must constrain something") */}
          {ruleErr && (
            <p
              className="text-xs text-accent-red"
              role="alert"
            >
              {ruleErr}
            </p>
          )}
        </div>
      )}
    </div>
  );
}