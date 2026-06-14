#!/usr/bin/env bash
# 06-fleet-ui-diagnostics/show-leader.sh — print leader status for all 3 dashd.
#
# Uses dashctl `version` if a binary is reachable (matches 04-ha-fleet pattern);
# otherwise falls back to the raw /admin/leader REST endpoint.
#
# Usage:
#   ./show-leader.sh

set -uo pipefail
cd "$(dirname "$0")"

# Resolve a dashctl binary: this dir, sibling 04-ha-fleet, then PATH.
DASHCTL=""
for candidate in ./dashctl ./dashctl.exe ../04-ha-fleet/dashctl ../04-ha-fleet/dashctl.exe; do
    if [[ -x "$candidate" ]]; then DASHCTL="$(cd "$(dirname "$candidate")" && pwd)/$(basename "$candidate")"; break; fi
done
if [[ -z "$DASHCTL" ]] && command -v dashctl >/dev/null 2>&1; then
    DASHCTL="$(command -v dashctl)"
fi

# Node label : rest port : admin port
NODES=(
    "dashd-1:38443:37443"
    "dashd-2:38453:37453"
    "dashd-3:38463:37463"
)

printf "%-9s %-7s %s\n" "NODE" "LEADER" "DETAIL"
for entry in "${NODES[@]}"; do
    name="${entry%%:*}"; rest_port="${entry#*:}"; rest_port="${rest_port%%:*}"; admin_port="${entry##*:}"
    leader='?'; detail=''

    if [[ -n "$DASHCTL" ]]; then
        out=$("$DASHCTL" \
            --endpoint "http://127.0.0.1:${rest_port}" \
            --admin-endpoint "http://127.0.0.1:${admin_port}" \
            version 2>&1 || true)
        server_line=$(echo "$out" | grep -E '^Server:' || true)
        if [[ -n "$server_line" ]]; then
            if [[ "$server_line" =~ leader=([^[:space:]]+) ]]; then leader="${BASH_REMATCH[1]}"; fi
            detail="$server_line"
        else
            detail='(no response — container down?)'
        fi
    else
        # Fallback: raw REST
        if r=$(curl -fsS --max-time 3 "http://127.0.0.1:${admin_port}/admin/leader" 2>/dev/null); then
            if echo "$r" | grep -q '"leader":true'; then leader='true'; else leader='false'; fi
            leader_id=$(echo "$r" | sed -n 's/.*"leader_id":"\([^"]*\)".*/\1/p')
            ttl=$(echo "$r" | sed -n 's/.*"lease_ttl_sec":\([0-9]*\).*/\1/p')
            detail="leader_id=${leader_id} lease_ttl=${ttl}s"
        else
            detail='(no response — container down?)'
        fi
    fi

    case "$leader" in
        true)  color='\033[1;32m' ;;  # green
        false) color='\033[1;30m' ;;  # gray
        *)     color='\033[1;31m' ;;  # red
    esac
    reset='\033[0m'
    printf "${color}%-9s %-7s %s${reset}\n" "$name" "$leader" "$detail"
done