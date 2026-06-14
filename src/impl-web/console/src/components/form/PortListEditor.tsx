/* ═══════════════════════════════════════════════════════════════
 * PortListEditor — chip-style array of port spec strings.
 *
 * Each entry is a single port `"443"` or a range `"7777-7800"`.
 * Validates each entry against the same regex used by zod.
 *
 * Used in AclRuleEditor for src_ports / dst_ports.
 *
 * Added in A-IF2-G7.
 * ═══════════════════════════════════════════════════════════════ */

import { StringArrayEditor } from "./StringArrayEditor";

interface PortListEditorProps {
  label?: string;
  hint?: string;
  error?: string;
  value: string[] | undefined;
  onChange: (next: string[]) => void;
  placeholder?: string;
  max?: number;
  className?: string;
  id?: string;
}

const PORT_RE = /^\d+(-\d+)?$/;

function validatePortSpec(s: string): string | undefined {
  if (!PORT_RE.test(s)) return "Expected `N` or `N-M`";
  const [a, b] = s.split("-").map((x) => Number.parseInt(x, 10));
  if (a === undefined || !Number.isFinite(a) || a < 0 || a > 65535) {
    return "Port must be 0..65535";
  }
  if (b !== undefined) {
    if (!Number.isFinite(b) || b < 0 || b > 65535) {
      return "Range end must be 0..65535";
    }
    if (a > b) return "Range start must be ≤ end";
  }
  return undefined;
}

export function PortListEditor(props: PortListEditorProps) {
  return (
    <StringArrayEditor
      label={props.label}
      hint={props.hint ?? "Single port `443` or range `7777-7800`"}
      error={props.error}
      value={props.value ?? []}
      onChange={props.onChange}
      placeholder={props.placeholder ?? "443"}
      validate={validatePortSpec}
      max={props.max}
      className={props.className}
      id={props.id}
    />
  );
}