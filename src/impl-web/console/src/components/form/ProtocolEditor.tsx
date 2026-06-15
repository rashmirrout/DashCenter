/* ═══════════════════════════════════════════════════════════════
 * ProtocolEditor — chip multi-select for protocol identifiers.
 *
 * Provides quick chips for `tcp`, `udp`, `icmp` and a custom-add
 * input for numeric protocols (e.g. `"47"` for GRE, `"50"` for
 * ESP). Order is not significant on the wire; we sort the chips
 * for visual stability.
 *
 * Used in AclRuleEditor for the `protocols[]` field.
 *
 * Added in A-IF2-G8.
 * ═══════════════════════════════════════════════════════════════ */

import { useState } from "react";
import { Plus, X } from "lucide-react";
import { cn } from "@/lib/cn";
import { FieldWrapper } from "./NetworkInputs";

interface ProtocolEditorProps {
  label?: string;
  hint?: string;
  error?: string;
  value: string[] | undefined;
  onChange: (next: string[]) => void;
  className?: string;
  id?: string;
}

const QUICK_PROTOS = ["tcp", "udp", "icmp"] as const;
const PROTO_RE = /^(tcp|udp|icmp|esp|gre|ah|sctp|\d{1,3})$/i;

function validateProto(s: string): string | undefined {
  if (!PROTO_RE.test(s.trim())) {
    return "Use `tcp`/`udp`/`icmp` or a number";
  }
  // Numeric range 0..255 (IP protocol field is 8 bits).
  if (/^\d+$/.test(s)) {
    const n = Number.parseInt(s, 10);
    if (n < 0 || n > 255) return "Protocol number must be 0..255";
  }
  return undefined;
}

export function ProtocolEditor({
  label = "Protocols",
  hint = "Click chips or add a numeric protocol (e.g. 47 for GRE)",
  error,
  value,
  onChange,
  className,
  id,
}: ProtocolEditorProps) {
  const inputId = id ?? "protocol-editor";
  const current = value ?? [];
  const [draft, setDraft] = useState("");
  const [draftError, setDraftError] = useState<string | undefined>();

  function toggleQuick(proto: string) {
    if (current.includes(proto)) {
      onChange(current.filter((p) => p !== proto));
    } else {
      onChange([...current, proto]);
    }
  }

  function commitCustom() {
    const v = draft.trim().toLowerCase();
    if (!v) {
      setDraftError(undefined);
      return;
    }
    const ve = validateProto(v);
    if (ve) {
      setDraftError(ve);
      return;
    }
    if (current.includes(v)) {
      setDraftError("Already added");
      return;
    }
    onChange([...current, v]);
    setDraft("");
    setDraftError(undefined);
  }

  function remove(p: string) {
    onChange(current.filter((x) => x !== p));
  }

  const customChips = current.filter(
    (p) => !(QUICK_PROTOS as readonly string[]).includes(p),
  );

  const content = (
    <div className={cn("flex flex-col gap-1.5", className)}>
      {/* Quick chips */}
      <div className="flex flex-wrap gap-1.5">
        {QUICK_PROTOS.map((p) => {
          const selected = current.includes(p);
          return (
            <button
              key={p}
              type="button"
              onClick={() => toggleQuick(p)}
              className={cn(
                "px-2 py-0.5 text-xs rounded-full border font-mono transition-colors",
                selected
                  ? "bg-accent-cyan/15 text-accent-cyan border-accent-cyan/40"
                  : "bg-bg-elevated text-text-secondary border-border hover:text-text-primary",
              )}
              aria-pressed={selected}
            >
              {p}
            </button>
          );
        })}
      </div>

      {/* Custom-added chips */}
      {customChips.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {customChips.map((p) => (
            <span
              key={p}
              className="inline-flex items-center gap-1 px-2 py-0.5 text-xs rounded-full border border-accent-purple/40 bg-accent-purple/15 text-accent-purple font-mono"
            >
              {p}
              <button
                type="button"
                onClick={() => remove(p)}
                aria-label={`Remove ${p}`}
                className="text-accent-purple/70 hover:text-accent-purple"
              >
                <X size={10} />
              </button>
            </span>
          ))}
        </div>
      )}

      {/* Add-custom input */}
      <div className="flex items-center gap-1.5">
        <input
          id={inputId}
          type="text"
          value={draft}
          onChange={(e) => {
            setDraft(e.target.value);
            setDraftError(undefined);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commitCustom();
            }
          }}
          placeholder="Or type a number / name…"
          className={cn(
            "flex-1 px-2 py-1 text-xs bg-bg-elevated border border-border rounded text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-cyan/50",
            (error || draftError) && "border-accent-red focus:ring-accent-red/50",
          )}
          aria-invalid={!!(error || draftError)}
        />
        <button
          type="button"
          onClick={commitCustom}
          className="flex items-center gap-1 px-2 py-1 text-xs rounded border border-border text-text-secondary hover:text-text-primary hover:bg-white/5"
        >
          <Plus size={12} />
        </button>
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
        {content}
      </FieldWrapper>
    );
  }
  return content;
}