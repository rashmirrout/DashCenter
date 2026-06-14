/* ═══════════════════════════════════════════════════════════════
 * ResourceMultiSelect — chip-style multi-select of live resources.
 *
 * Populated from `useResourceList(kind, ns)`. The selection is an
 * array of resource names. Shows live count, supports check-all
 * and clear-all.
 *
 * Used in EniForm (placement_hint_dpu_ids[]), AclPolicyForm
 * (eni_names[]), RoutePolicyForm (eni_names[]), HaSetForm
 * (members[].dpu_id).
 *
 * Added in A-IF2-G5.
 * ═══════════════════════════════════════════════════════════════ */

import { useMemo, useState } from "react";
import { Check, ChevronDown, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { FieldWrapper } from "./NetworkInputs";
import {
  useResourceList,
  type ResourceListKind,
} from "@/queries/use-resource-list";

interface ResourceItem {
  metadata?: { name?: string };
  identity?: { dpu_id?: string };
}

interface ResourceMultiSelectProps {
  /** Which resource kind to fetch. */
  kind: ResourceListKind;
  /** Namespace override; defaults to "default". */
  ns?: string;
  value: string[];
  onChange: (next: string[]) => void;
  label?: string;
  hint?: string;
  error?: string;
  /** Disable the editor entirely. */
  disabled?: boolean;
  /** Filter the list. e.g. exclude already-selected items. */
  filter?: (name: string) => boolean;
  className?: string;
  id?: string;
}

function nameOf(item: ResourceItem): string {
  const n = item.metadata?.name;
  if (n) return n;
  return item.identity?.dpu_id ?? "";
}

export function ResourceMultiSelect({
  kind,
  ns = "default",
  value,
  onChange,
  label,
  hint,
  error,
  disabled = false,
  filter,
  className,
  id,
}: ResourceMultiSelectProps) {
  const inputId = id ?? `resource-multi-${kind}`;
  const list = useResourceList(kind, ns);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");

  const available = useMemo(() => {
    const all = Array.from(
      new Set(
        (list.items as ResourceItem[])
          .map(nameOf)
          .filter((n) => n !== ""),
      ),
    ).sort();
    const filtered = filter ? all.filter(filter) : all;
    if (!search) return filtered;
    const q = search.toLowerCase();
    return filtered.filter((n) => n.toLowerCase().includes(q));
  }, [list.items, filter, search]);

  function toggle(name: string) {
    if (value.includes(name)) {
      onChange(value.filter((v) => v !== name));
    } else {
      onChange([...value, name]);
    }
  }

  function remove(name: string) {
    onChange(value.filter((v) => v !== name));
  }

  function checkAll() {
    onChange(Array.from(new Set([...value, ...available])));
  }

  function clearAll() {
    onChange([]);
  }

  const totalAvailable = list.isLoading ? "…" : available.length;
  const fullLabel = label
    ? `${label} · ${value.length} selected${
        !list.isLoading ? ` of ${list.items.length} total` : ""
      }`
    : undefined;

  const content = (
    <div className={cn("flex flex-col gap-1", className)}>
      {/* Selected chips */}
      <div
        className={cn(
          "flex flex-wrap gap-1 items-center min-h-[28px] px-2 py-1 bg-bg-elevated border border-border rounded",
          error && "border-accent-red",
        )}
      >
        {value.length === 0 ? (
          <span className="text-xs text-text-muted italic">
            Nothing selected
          </span>
        ) : (
          value.map((n) => (
            <span
              key={n}
              className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-bg-tertiary text-[10px] font-mono text-text-primary"
            >
              {n}
              <button
                type="button"
                onClick={() => remove(n)}
                disabled={disabled}
                aria-label={`Remove ${n}`}
                className="text-text-muted hover:text-accent-red disabled:opacity-40"
              >
                <X size={10} />
              </button>
            </span>
          ))
        )}
        <button
          type="button"
          onClick={() => !disabled && setOpen((o) => !o)}
          disabled={disabled}
          className="ml-auto inline-flex items-center gap-0.5 text-xs text-accent-cyan hover:text-accent-cyan/80 disabled:opacity-40"
          aria-expanded={open}
          aria-controls={`${inputId}-menu`}
          id={inputId}
        >
          {open ? "Hide" : "Browse"}
          <ChevronDown
            size={12}
            className={cn("transition-transform", open && "rotate-180")}
            aria-hidden
          />
        </button>
      </div>

      {/* Dropdown menu */}
      {open && (
        <div
          id={`${inputId}-menu`}
          className="mt-1 border border-border rounded bg-bg-elevated p-2 max-h-64 overflow-y-auto"
          role="listbox"
          aria-multiselectable="true"
        >
          {/* Search + bulk actions */}
          <div className="flex items-center gap-2 mb-2 sticky top-0 bg-bg-elevated pb-1.5 border-b border-border">
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={`Filter ${kind}…`}
              className="flex-1 px-2 py-1 text-xs bg-bg-primary border border-border rounded text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50"
            />
            <button
              type="button"
              onClick={checkAll}
              disabled={available.length === 0}
              className="text-xs text-accent-cyan hover:text-accent-cyan/80 disabled:opacity-40"
            >
              All
            </button>
            <button
              type="button"
              onClick={clearAll}
              disabled={value.length === 0}
              className="text-xs text-text-secondary hover:text-text-primary disabled:opacity-40"
            >
              Clear
            </button>
          </div>

          {list.isError ? (
            <p className="text-xs text-accent-red px-2 py-1">
              Failed to load — {list.error?.message ?? "unknown error"}.{" "}
              <button
                type="button"
                onClick={() => list.refetch()}
                className="underline"
              >
                Retry
              </button>
            </p>
          ) : list.isLoading ? (
            <p className="text-xs text-text-muted px-2 py-1">Loading…</p>
          ) : available.length === 0 ? (
            <p className="text-xs text-text-muted italic px-2 py-1">
              {search
                ? "No matches"
                : `No ${kind} exist yet — create one in the appropriate tab first.`}
            </p>
          ) : (
            <ul className="space-y-0.5">
              {available.map((n) => {
                const selected = value.includes(n);
                return (
                  <li key={n}>
                    <button
                      type="button"
                      onClick={() => toggle(n)}
                      role="option"
                      aria-selected={selected}
                      className={cn(
                        "w-full flex items-center justify-between gap-2 px-2 py-1 text-xs rounded font-mono text-left",
                        selected
                          ? "bg-accent-cyan/15 text-accent-cyan"
                          : "text-text-primary hover:bg-white/5",
                      )}
                    >
                      <span className="truncate">{n}</span>
                      {selected && (
                        <Check size={12} className="shrink-0" aria-hidden />
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}

          <p className="text-[10px] text-text-muted mt-2 px-1">
            {available.length} of {totalAvailable} shown
          </p>
        </div>
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