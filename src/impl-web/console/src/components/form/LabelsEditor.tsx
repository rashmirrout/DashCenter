/* ═══════════════════════════════════════════════════════════════
 * LabelsEditor — edits a Record<string, string>
 *
 * Used by every form for `metadata.labels`. Allows add/remove
 * key/value rows. Enforces unique keys.
 *
 * Added in A-IF2-G2.
 * ═══════════════════════════════════════════════════════════════ */

import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { cn } from "@/lib/cn";
import { FieldWrapper } from "./NetworkInputs";

interface LabelsEditorProps {
  label?: string;
  hint?: string;
  value: Record<string, string> | undefined;
  onChange: (next: Record<string, string>) => void;
  /** Common keys to suggest in the key dropdown. */
  suggestedKeys?: string[];
  /** Placeholder shown when the map is empty. */
  emptyText?: string;
  /** Maximum number of entries allowed (default: unlimited). */
  max?: number;
  className?: string;
}

const baseInputClass =
  "px-2 py-1 text-xs bg-bg-elevated border border-border rounded text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50";

export function LabelsEditor({
  label = "Labels",
  hint,
  value,
  onChange,
  suggestedKeys,
  emptyText = "No labels — click + to add one",
  max,
  className,
}: LabelsEditorProps) {
  // Internal representation keeps insertion order so the UI doesn't
  // shuffle when the user edits a key. We mirror it back to the
  // parent's `Record<string, string>` shape on every change.
  const initial: Array<[string, string]> = Object.entries(value ?? {});
  const [entries, setEntries] = useState(initial);
  const [dupKey, setDupKey] = useState<string | null>(null);

  // If the parent passes a new `value`, resync. Cheap because
  // labels are small.
  if (Object.keys(value ?? {}).length !== entries.length) {
    // Heuristic: if the count differs, the parent did a wholesale
    // change (e.g., Edit-form load). Reset our internal state.
    setEntries(Object.entries(value ?? {}));
  }

  function emit(next: Array<[string, string]>) {
    setEntries(next);
    const seen = new Set<string>();
    const out: Record<string, string> = {};
    for (const [k, v] of next) {
      if (!k) continue;
      if (seen.has(k)) {
        setDupKey(k);
        return; // don't emit when duplicate; user must fix first
      }
      seen.add(k);
      out[k] = v;
    }
    setDupKey(null);
    onChange(out);
  }

  function updateKey(idx: number, newKey: string) {
    const next = [...entries];
    next[idx] = [newKey, next[idx]![1]];
    emit(next);
  }

  function updateValue(idx: number, newVal: string) {
    const next = [...entries];
    next[idx] = [next[idx]![0], newVal];
    emit(next);
  }

  function removeRow(idx: number) {
    const next = entries.filter((_, i) => i !== idx);
    emit(next);
  }

  function addRow() {
    if (max && entries.length >= max) return;
    emit([...entries, ["", ""]]);
  }

  return (
    <FieldWrapper label={label} htmlFor="labels-editor" hint={hint}>
      <div className={cn("space-y-1", className)}>
        {entries.length === 0 ? (
          <p className="text-xs text-text-muted italic">{emptyText}</p>
        ) : (
          entries.map(([k, v], idx) => {
            const isDup = dupKey === k && k !== "";
            return (
              <div key={idx} className="flex items-center gap-1">
                {suggestedKeys && suggestedKeys.length > 0 ? (
                  <input
                    type="text"
                    list="labels-editor-keys"
                    value={k}
                    onChange={(e) => updateKey(idx, e.target.value)}
                    placeholder="key"
                    className={cn(baseInputClass, "flex-1", isDup && "border-accent-red")}
                    aria-label={`Label ${idx + 1} key`}
                    aria-invalid={isDup}
                  />
                ) : (
                  <input
                    type="text"
                    value={k}
                    onChange={(e) => updateKey(idx, e.target.value)}
                    placeholder="key"
                    className={cn(baseInputClass, "flex-1", isDup && "border-accent-red")}
                    aria-label={`Label ${idx + 1} key`}
                    aria-invalid={isDup}
                  />
                )}
                <span className="text-xs text-text-muted">=</span>
                <input
                  type="text"
                  value={v}
                  onChange={(e) => updateValue(idx, e.target.value)}
                  placeholder="value"
                  className={cn(baseInputClass, "flex-[2]")}
                  aria-label={`Label ${idx + 1} value`}
                />
                <button
                  type="button"
                  onClick={() => removeRow(idx)}
                  className="p-1 text-text-muted hover:text-accent-red transition-colors"
                  aria-label={`Remove label ${idx + 1}`}
                >
                  <Trash2 size={12} />
                </button>
              </div>
            );
          })
        )}
        {dupKey && (
          <p className="text-xs text-accent-red" role="alert">
            Duplicate key &quot;{dupKey}&quot; — keys must be unique.
          </p>
        )}
        {suggestedKeys && suggestedKeys.length > 0 && (
          <datalist id="labels-editor-keys">
            {suggestedKeys.map((k) => (
              <option key={k} value={k} />
            ))}
          </datalist>
        )}
        <button
          type="button"
          onClick={addRow}
          disabled={max ? entries.length >= max : false}
          className="flex items-center gap-1 text-xs text-accent-cyan hover:text-accent-cyan/80 disabled:opacity-40 disabled:cursor-not-allowed mt-1"
        >
          <Plus size={12} />
          Add label
          {max ? ` (${entries.length}/${max})` : ""}
        </button>
      </div>
    </FieldWrapper>
  );
}