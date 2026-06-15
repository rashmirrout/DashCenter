#!/usr/bin/env bash
# test-roundtrip.sh — Roundtrip tester for the full-specs library.
#
# Usage:
#   ./test-roundtrip.sh apply    # apply every full-spec
#   ./test-roundtrip.sh verify   # count resources by demo label
#   ./test-roundtrip.sh delete   # delete every full-spec
#   ./test-roundtrip.sh test     # apply → verify → delete → verify-gone
#
# Env:
#   DASHCTL_ENDPOINT  defaults to http://localhost:28443
#   DASHCTL_BIN       path to dashctl (auto-discovers test-setup/04-ha-fleet/dashctl)
set -euo pipefail

action="${1:-test}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ── Resolve endpoint ──────────────────────────────────────────────────
export DASHCTL_ENDPOINT="${DASHCTL_ENDPOINT:-http://localhost:28443}"

# ── Resolve dashctl binary ────────────────────────────────────────────
DASHCTL_BIN="${DASHCTL_BIN:-}"
if [ -z "$DASHCTL_BIN" ]; then
  for cand in \
      "$script_dir/../../../../../src/impl-go/dashctl/dashctl" \
      "$script_dir/../../../04-ha-fleet/dashctl" \
      "$script_dir/../../../05-full-console/dashctl" \
      "$script_dir/../../../07-full-experiment/dashctl" \
      "$(command -v dashctl 2>/dev/null || true)"; do
    if [ -n "$cand" ] && [ -x "$cand" ]; then DASHCTL_BIN="$cand"; break; fi
  done
fi
if [ -z "$DASHCTL_BIN" ] || [ ! -x "$DASHCTL_BIN" ]; then
  echo "dashctl not found. Set DASHCTL_BIN=<path> or put it on PATH." >&2
  exit 2
fi

# ── Spec files (file → demo label) ────────────────────────────────────
SPEC_FILES=(
  "eni-full.yaml:eni-full"
  "vnet-full.yaml:vnet-full"
  "route-full.yaml:route-full"
  "mapping-full.yaml:mapping-full"
  "acl-full.yaml:acl-full"
  "service-tunnel-full.yaml:st-full"
  "private-link-full.yaml:pl-full"
  "ha-full.yaml:ha-full"
)

KINDS=(vnet service-tunnel eni vnet-mapping route-policy acl-policy ha-set)

# ── Colors ────────────────────────────────────────────────────────────
c_cyan="$(printf '\033[36m')"; c_grn="$(printf '\033[32m')"
c_red="$(printf '\033[31m')";  c_ylw="$(printf '\033[33m')"
c_gry="$(printf '\033[90m')";  c_off="$(printf '\033[0m')"

apply_specs() {
  echo "${c_cyan}==> Apply (endpoint=$DASHCTL_ENDPOINT)${c_off}"
  for entry in "${SPEC_FILES[@]}"; do
    file="${entry%%:*}"
    echo "    ${c_gry}apply $file${c_off}"
    if ! "$DASHCTL_BIN" apply -f "$script_dir/$file" --force; then
      echo "    ${c_ylw}! apply $file failed${c_off}"
    fi
  done
}

count_label() {
  local label="$1" total=0 out
  for k in "${KINDS[@]}"; do
    if out=$("$DASHCTL_BIN" get "$k" -l "demo=$label" -o name 2>/dev/null); then
      if [ -n "$out" ]; then
        n=$(printf '%s\n' "$out" | grep -c '[^[:space:]]' || true)
        total=$((total + n))
      fi
    fi
  done
  echo "$total"
}

verify_specs() {
  local expect_gone="$1" ok=true
  local mode="present"
  [ "$expect_gone" = "1" ] && mode="gone"
  echo "${c_cyan}==> Verify ($mode)${c_off}"
  for entry in "${SPEC_FILES[@]}"; do
    file="${entry%%:*}"
    demo="${entry##*:}"
    n=$(count_label "$demo")
    if [ "$expect_gone" = "1" ]; then
      if [ "$n" = "0" ]; then
        printf '    %sOK   %-28s demo=%s -> %s objects%s\n' "$c_grn" "$file" "$demo" "$n" "$c_off"
      else
        printf '    %sFAIL %-28s demo=%s -> %s objects (expected 0)%s\n' "$c_red" "$file" "$demo" "$n" "$c_off"
        ok=false
      fi
    else
      if [ "$n" -gt 0 ]; then
        printf '    %sOK   %-28s demo=%s -> %s objects%s\n' "$c_grn" "$file" "$demo" "$n" "$c_off"
      else
        printf '    %sFAIL %-28s demo=%s -> %s objects (expected >0)%s\n' "$c_red" "$file" "$demo" "$n" "$c_off"
        ok=false
      fi
    fi
  done
  $ok
}

delete_specs() {
  echo "${c_cyan}==> Delete (endpoint=$DASHCTL_ENDPOINT)${c_off}"
  # dashctl delete takes <kind> <name>, not -f. We delete by label in
  # REVERSE tier order (policies → Tier 1 → Tier 0) to honour FK protection.
  local delete_order=(acl-policy route-policy ha-set vnet-mapping eni service-tunnel vnet)
  for entry in "${SPEC_FILES[@]}"; do
    demo="${entry##*:}"
    echo "    ${c_gry}delete demo=$demo${c_off}"
    for k in "${delete_order[@]}"; do
      if names=$("$DASHCTL_BIN" get "$k" -l "demo=$demo" -o name 2>/dev/null); then
        [ -z "$names" ] && continue
        while IFS= read -r line; do
          [ -z "$line" ] && continue
          # name format may be "kind/name" — keep the name portion
          name="${line##*/}"
          [ -z "$name" ] && continue
          "$DASHCTL_BIN" delete "$k" "$name" --ignore-not-found >/dev/null 2>&1 || true
        done <<< "$names"
      fi
    done
  done
}

case "$action" in
  apply)  apply_specs ;;
  verify) verify_specs 0 || exit 1 ;;
  delete) delete_specs ;;
  test)
    apply_specs
    sleep 2
    verify_specs 0 || { echo "${c_red}ROUNDTRIP FAIL (after apply)${c_off}"; exit 1; }
    delete_specs
    sleep 2
    verify_specs 1 || { echo "${c_red}ROUNDTRIP FAIL (after delete)${c_off}"; exit 1; }
    echo "${c_grn}==> ROUNDTRIP OK${c_off}"
    ;;
  *)
    echo "Usage: $0 {apply|verify|delete|test}" >&2; exit 2
    ;;
esac
