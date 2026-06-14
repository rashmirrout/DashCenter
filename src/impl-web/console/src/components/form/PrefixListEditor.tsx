/* ═══════════════════════════════════════════════════════════════
 * PrefixListEditor — chip-style array of IPv4 CIDRs.
 *
 * Thin wrapper over StringArrayEditor that pre-validates each
 * entry against the IPv4-CIDR regex used in the zod schema.
 * Common placeholders: `0.0.0.0/0`, `10.0.0.0/24`.
 *
 * Used in AclRuleEditor for src_prefixes / dst_prefixes.
 *
 * Added in A-IF2-G6.
 * ═══════════════════════════════════════════════════════════════ */

import { StringArrayEditor } from "./StringArrayEditor";

interface PrefixListEditorProps {
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

const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;

function validateCidr(s: string): string | undefined {
  if (!CIDR_RE.test(s)) return "Expected `A.B.C.D/M`";
  // Cheap structural validation of octets + prefix length.
  const [addr, m] = s.split("/");
  const mNum = Number.parseInt(m ?? "", 10);
  if (!Number.isFinite(mNum) || mNum < 0 || mNum > 32) {
    return "Mask must be 0..32";
  }
  for (const oct of (addr ?? "").split(".")) {
    const n = Number.parseInt(oct, 10);
    if (!Number.isFinite(n) || n < 0 || n > 255) {
      return "Each octet must be 0..255";
    }
  }
  return undefined;
}

export function PrefixListEditor(props: PrefixListEditorProps) {
  return (
    <StringArrayEditor
      label={props.label}
      hint={props.hint}
      error={props.error}
      value={props.value ?? []}
      onChange={props.onChange}
      placeholder={props.placeholder ?? "10.0.0.0/24"}
      validate={validateCidr}
      max={props.max}
      className={props.className}
      id={props.id}
    />
  );
}