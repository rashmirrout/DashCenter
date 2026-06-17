#!/usr/bin/env python3
"""Convert dashcenter.v1 AclPolicy rule match fields from the invalid
singular scalar form to the schema-correct plural array form, operating
on raw text so comments and formatting are preserved.

Renames (and wraps the value in a single-element array when it's a scalar):
    src_prefix  -> src_prefixes
    dst_prefix  -> dst_prefixes
    src_port    -> src_ports
    dst_port    -> dst_ports
    protocol    -> protocols

dashcenter.v1 AclRuleSpec uses REPEATED fields; the singular scalar forms
are silently dropped by dashd's JSON decoder, leaving rules with only
priority+action.

SAFETY: files whose content declares `kind: Scenario` (the dash-sim
low-level acl_rule schema, apiVersion dashapi.dashcenter.io/v1) are skipped
entirely — their `protocol`/`dst_port` fields are correct for that schema.

Usage:
    python scripts/fix-acl-singular-keys.py <file.yaml> [more.yaml ...]
    python scripts/fix-acl-singular-keys.py --check <file.yaml ...>
"""
from __future__ import annotations
import re
import sys

RENAME = {
    "src_prefix": "src_prefixes",
    "dst_prefix": "dst_prefixes",
    "src_port": "src_ports",
    "dst_port": "dst_ports",
    "protocol": "protocols",
}

# Matches a "key: value" occurrence where key is one of the singular forms.
# value is captured up to a comma, closing brace, or end-of-line so we can
# wrap it. We intentionally use word boundaries and a trailing ':' to avoid
# matching substrings (e.g. won't match 'protocols:' which is already plural,
# because the negative lookahead (?![a-z_]) ensures the key ends here).
KEY_ALT = "|".join(sorted(RENAME, key=len, reverse=True))
RULE_FIELD_RE = re.compile(
    r"(?P<key>\b(?:" + KEY_ALT + r"))(?![a-z_])"      # singular key, not part of a longer ident
    r"(?P<sep>\s*:\s*)"                                # the ': '
    r"(?P<val>"                                        # value:
    r'"[^"]*"'                                         #   double-quoted
    r"|'[^']*'"                                        #   single-quoted
    r"|[^,}\r\n]+?"                                    #   bare scalar (non-greedy)
    r")"
    r"(?P<tail>\s*(?:,|}|$))",                         # delimiter: comma / } / EOL
    re.MULTILINE,                                       # so $ matches end-of-line (block-style YAML)
)

def _wrap(val: str) -> str:
    v = val.strip()
    # Already a YAML flow list -> leave as-is.
    if v.startswith("[") and v.endswith("]"):
        return v
    return "[" + v + "]"

def _sub(m: "re.Match[str]") -> str:
    key = m.group("key")
    newkey = RENAME[key]
    val = m.group("val")
    return newkey + m.group("sep") + _wrap(val) + m.group("tail")

def transform(text: str) -> tuple[str, int]:
    # Skip dash-sim Scenario files entirely.
    if re.search(r"^\s*kind:\s*Scenario\b", text, re.MULTILINE):
        return text, 0
    new, n = RULE_FIELD_RE.subn(_sub, text)
    return new, n

def main(argv: list[str]) -> int:
    check = False
    args = []
    for a in argv:
        if a == "--check":
            check = True
        else:
            args.append(a)
    if not args:
        print("usage: fix-acl-singular-keys.py [--check] <file.yaml ...>", file=sys.stderr)
        return 2
    total = 0
    for path in args:
        with open(path, "r", encoding="utf-8") as f:
            raw = f.read()
        new, n = transform(raw)
        total += n
        if n == 0:
            print(f"  ok (no change): {path}")
            continue
        if check:
            print(f"  WOULD FIX {n} field(s): {path}")
        else:
            with open(path, "w", encoding="utf-8", newline="\n") as f:
                f.write(new)
            print(f"  fixed {n} field(s): {path}")
    print(f"total fields {'to fix' if check else 'fixed'}: {total}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))