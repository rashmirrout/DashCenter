/* ═══════════════════════════════════════════════════════════════
 * ResourceSelect — single-select dropdown of live resources.
 *
 * Populated from `useResourceList(kind, ns)`. Shows live count
 * in the label area; renders an inline "no items — create one
 * first" affordance when the list is empty.
 *
 * Used in EniForm (vnet_name), VnetMappingForm (vnet_name +
 * conditional params.tunnel), RouteEntryEditor (next_hop_target).
 *
 * Added in A-IF2-G4.
 * ═══════════════════════════════════════════════════════════════ */

import { cn } from "@/lib/cn";
import { FieldWrapper } from "./NetworkInputs";
import {
  useResourceList,
  type ResourceListKind,
} from "@/queries/use-resource-list";

interface ResourceItem {
  metadata?: { name?: string };
  // Some resource shapes (inventory) put the id elsewhere.
  identity?: { dpu_id?: string };
}

interface ResourceSelectProps {
  /** Which resource kind to fetch. */
  kind: ResourceListKind;
  /** Namespace override; defaults to "default". */
  ns?: string;
  value: string;
  onChange: (next: string) => void;
  label?: string;
  hint?: string;
  error?: string;
  placeholder?: string;
  /** Allow the empty string as a valid selection. */
  allowEmpty?: boolean;
  /** Disable the dropdown. */
  disabled?: boolean;
  className?: string;
  id?: string;
}

const selectClass =
  "w-full px-2 py-1 text-sm bg-bg-elevated border border-border rounded text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent-cyan/50 disabled:opacity-50 disabled:cursor-not-allowed";

/** Best-effort name extractor that handles both the standard
 *  `metadata.name` shape and the inventory `identity.dpu_id` shape. */
function nameOf(item: ResourceItem): string {
  const n = item.metadata?.name;
  if (n) return n;
  return item.identity?.dpu_id ?? "";
}

export function ResourceSelect({
  kind,
  ns = "default",
  value,
  onChange,
  label,
  hint,
  error,
  placeholder = "— select —",
  allowEmpty = false,
  disabled = false,
  className,
  id,
}: ResourceSelectProps) {
  const inputId = id ?? `resource-select-${kind}`;
  const list = useResourceList(kind, ns);

  // Build the list of selectable names. De-dup just in case.
  const names = Array.from(
    new Set(
      (list.items as ResourceItem[])
        .map(nameOf)
        .filter((n) => n !== ""),
    ),
  ).sort();

  const isEmpty = !list.isLoading && !list.isError && names.length === 0;
  const fullLabel = label
    ? `${label}${!list.isLoading ? ` · ${names.length} available` : ""}`
    : undefined;

  const content = (
    <div className="flex flex-col gap-1">
      <select
        id={inputId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={disabled || list.isLoading}
        aria-invalid={!!error}
        className={cn(
          selectClass,
          error && "border-accent-red focus:ring-accent-red/50",
          className,
        )}
      >
        {(allowEmpty || value === "") && (
          <option value="">{list.isLoading ? "Loading…" : placeholder}</option>
        )}
        {names.map((n) => (
          <option key={n} value={n}>
            {n}
          </option>
        ))}
      </select>
      {list.isError && (
        <span className="text-xs text-accent-red" role="alert">
          Failed to load — {list.error?.message ?? "unknown error"}.{" "}
          <button
            type="button"
            onClick={() => list.refetch()}
            className="underline hover:text-accent-red/80"
          >
            Retry
          </button>
        </span>
      )}
      {isEmpty && (
        <span className="text-xs text-text-muted italic">
          No {kind} exist yet — create one in the appropriate tab first.
        </span>
      )}
    </div>
  );

  if (fullLabel) {
    return (
      <FieldWrapper label={fullLabel} htmlFor={inputId} error={error} hint={hint}>
        {content}
      </FieldWrapper>
    );
  }
  return content;
}