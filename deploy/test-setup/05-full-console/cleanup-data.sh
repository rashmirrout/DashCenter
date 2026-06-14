#!/usr/bin/env bash
# 05-full-console/cleanup-data.sh — remove the loaded dataset (keeps fleet running).
#
# Reverse of provision.sh: deletes every loaded object across the 3
# namespaces (default + edge + staging) via the dashd REST API. The dashd
# controllers, sims, and etcd state remain up — only the application
# objects are removed.
#
# Use this when you want to reset state for the next provision iteration
# without paying the docker compose restart cost.
#
# For a full fleet shutdown (containers + volumes), use stop-fleet.sh.
#
# NOTE: This script does not use `dashctl delete` because the current
# dashctl version takes only `delete <kind> <name>` (no -f / batch mode);
# direct REST DELETEs are faster and don't require dashctl at all.
#
# Usage:
#   ./cleanup-data.sh
#   ./cleanup-data.sh --endpoint http://10.0.0.5:28443

set -euo pipefail
cd "$(dirname "$0")"

ENDPOINT='http://127.0.0.1:28443'
while [[ $# -gt 0 ]]; do
    case "$1" in
        --endpoint) ENDPOINT="$2"; shift 2 ;;
        -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

echo "==> Cleaning data via REST against $ENDPOINT"

# Need a JSON parser. Prefer python3 (always present on dev hosts here).
PY="$(command -v python3 || command -v python || true)"
[[ -n "$PY" ]] || { echo "!! python required for JSON parsing." >&2; exit 1; }

# Reverse-dependency order: ACLs → routes → mappings → ENIs → tunnels → vnets → HA sets.
KINDS=(acl-policies route-policies vnet-mappings enis service-tunnels vnets ha)
NAMESPACES=(default edge staging)
total=0; ok=0; err=0

for ns in "${NAMESPACES[@]}"; do
    for k in "${KINDS[@]}"; do
        url="${ENDPOINT}/v1/${ns}/${k}"
        body=$(curl -fsS --max-time 5 "$url" 2>/dev/null || echo '')
        [[ -z "$body" ]] && continue
        # Extract names: dashd returns { items: [{ name, namespace, kind, ... }] }
        # name is at the top level (not under metadata.name).
        # Filter to current ns to avoid cross-namespace double-deletes.
        names=$(NS="$ns" "$PY" -c '
import json, os, sys
ns = os.environ.get("NS", "")
try:
    d = json.loads(sys.stdin.read())
except Exception:
    sys.exit(0)
items = d.get("items", d) if isinstance(d, dict) else d
if not isinstance(items, list):
    sys.exit(0)
for it in items:
    if not isinstance(it, dict):
        continue
    item_ns = it.get("namespace") or (it.get("metadata") or {}).get("namespace")
    if item_ns and item_ns != ns:
        continue
    n = it.get("name") or (it.get("metadata") or {}).get("name")
    if n:
        print(n)
' <<<"$body" || true)
        while IFS= read -r name; do
            [[ -z "$name" ]] && continue
            total=$((total+1))
            if curl -fsS --max-time 5 -X DELETE "${url}/${name}" >/dev/null 2>&1; then
                printf "  \033[1;32m✓\033[0m DELETE /v1/%s/%s/%s\n" "$ns" "$k" "$name"
                ok=$((ok+1))
            else
                printf "  \033[1;31m✗\033[0m DELETE /v1/%s/%s/%s\n" "$ns" "$k" "$name"
                err=$((err+1))
            fi
        done <<<"$names"
    done
done

echo ""
if [[ $err -eq 0 ]]; then
    printf "\033[1;32m==> Cleanup complete: %d/%d ok, %d errors\033[0m\n" "$ok" "$total" "$err"
else
    printf "\033[1;33m==> Cleanup complete: %d/%d ok, %d errors\033[0m\n" "$ok" "$total" "$err"
    exit 1
fi