/* ═══════════════════════════════════════════════════════════════
 * StringArrayEditor — chip-style array of strings.
 *
 * User adds a chip by typing + Enter (or comma). Each chip has
 * an × button to remove. Optional per-string validator surfaces
 * inline errors as the user types.
 *
 * Used for ad-hoc string arrays. Specialised editors (Prefix,
 * Port, Protocol) wrap this for typed validation.
 *
 * Added in A-IF2-G3.
 * ═══════════════════════════════════════════════════════════════ */

import { useState, type KeyboardEvent, type ChangeEvent } from "react";
import { X } from "lucide-react";
import { cn } from "@/lib/cn";
import { FieldWrapper } from "./NetworkInputs";

interface StringArrayEditorProps {
  label?: string;
  hint?: string;
  error?: string;
  value: string[];
  onChange: (next: string[]) => void;
  placeholder?: string;
  /** Per-string validator. Return an error string to reject. */
  validate?: (s: string) => string | undefined;
  /** Show a small `+ Add` button alongside Enter behaviour. */
  showAddButton?: boolean;
  /** Disallow duplicate strings (default: true). */
  unique?: boolean;
  /** Max number of chips (default: unlimited). */
  max?: number;
  className?: string;
  id?: string;
}

const inputClass =
  "min-w-[80px] flex-1 px-1 py-0.5 text-xs bg-transparent text-text-primary placeholder:text-text-muted focus:outline-none";

export function StringArrayEditor({
  label,
  hint,
  error,
  value,
  onChange,
  placeholder = "Type and press Enter…",
  validate,
  showAddButton = false,
  unique = true,
  max,
  className,
  id,
}: StringArrayEditorProps) {
  const inputId = id ?? "string-array-editor";
  const [draft, setDraft] = useState("");
  const [draftError, setDraftError] = useState<string | undefined>();

  function commit(rawStr: string) {
    const candidate = rawStr.trim();
    if (!candidate) {
      setDraft("");
      setDraftError(undefined);
      return;
    }
    if (validate) {
      const ve = validate(candidate);
      if (ve) {
        setDraftError(ve);
        return;
      }
    }
    if (unique && value.includes(candidate)) {
      setDraftError("Duplicate value");
      return;
    }
    if (max && value.length >= max) {
      setDraftError(`At most ${max} entries allowed`);
      return;
    }
    onChange([...value, candidate]);
    setDraft("");
    setDraftError(undefined);
  }

  function remove(idx: number) {
    const next = [...value];
    next.splice(idx, 1);
    onChange(next);
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" || e.key === ",") {
      e.preventDefault();
      commit(draft);
    } else if (e.key === "Backspace" && draft === "" && value.length > 0) {
      // Quick-delete the last chip when the input is empty.
      remove(value.length - 1);
    }
  }

  function onChangeInput(e: ChangeEvent<HTMLInputElement>) {
    const v = e.target.value;
    // Smart-paste: commit on comma anywhere in the pasted value.
    if (v.includes(",")) {
      const parts = v.split(",");
      for (let i = 0; i < parts.length - 1; i++) {
        commit(parts[i] ?? "");
      }
      setDraft(parts[parts.length - 1] ?? "");
    } else {
      setDraft(v);
      setDraftError(undefined);
    }
  }

  const wrapped = (
    <div className={cn("flex flex-col gap-1", className)}>
      <div
        className={cn(
          "flex flex-wrap items-center gap-1 px-2 py-1 bg-bg-elevated border border-border rounded-lg focus-within:ring-1 focus-within:ring-accent-cyan/50",
          (error || draftError) && "border-accent-red focus-within:ring-accent-red/50",
        )}
      >
        {value.map((s, idx) => (
          <span
            key={`${s}-${idx}`}
            className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-bg-tertiary text-[10px] font-mono text-text-primary"
          >
            {s}
            <button
              type="button"
              onClick={() => remove(idx)}
              aria-label={`Remove ${s}`}
              className="text-text-muted hover:text-accent-red"
            >
              <X size={10} />
            </button>
          </span>
        ))}
        <input
          id={inputId}
          type="text"
          value={draft}
          onChange={onChangeInput}
          onKeyDown={onKey}
          onBlur={() => draft && commit(draft)}
          placeholder={value.length === 0 ? placeholder : ""}
          className={inputClass}
          aria-invalid={!!(error || draftError)}
        />
        {showAddButton && draft && (
          <button
            type="button"
            onClick={() => commit(draft)}
            className="text-xs text-accent-cyan hover:text-accent-cyan/80"
          >
            + Add
          </button>
        )}
      </div>
      {draftError && (
        <span className="text-xs text-accent-red" role="alert">
          {draftError}
        </span>
      )}
    </div>
  );

  if (label) {
    return (
      <FieldWrapper label={label} htmlFor={inputId} error={error} hint={hint}>
        {wrapped}
      </FieldWrapper>
    );
  }
  return wrapped;
}