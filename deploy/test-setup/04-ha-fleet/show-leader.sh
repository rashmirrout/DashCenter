#!/usr/bin/env bash
# 04-ha-fleet/show-leader.sh — print leader status for all 3 dashd.
#
# Uses `dashctl version` against each node's REST + admin endpoint pair.
# `version` already reports `leader=true|false`. This wrapper iterates so
# you see all 3 in one shot.
#
# Usage:  ./show-leader.sh
set -euo pipefail
cd "$(dirname "$0")"

DASHCTL="./dashctl.exe"
if [[ ! -x "$DASHCTL" ]]; then
    DASHCTL="./dashctl"
fi
if [[ ! -x "$DASHCTL" ]]; then
    echo "dashctl not found; build with:"
    echo "  go build -o ./dashctl ../../../src/impl-go/dashctl/cmd/dashctl"
    exit 1
fi

printf "%-9s %-10s %s\n" "NODE" "LEADER" "RESPONSE"
for pair in "dashd-1 28443 27443" "dashd-2 28453 27453" "dashd-3 28463 27463"; do
    read -r name rest admin <<<"$pair"
    line=$("$DASHCTL" --endpoint "http://127.0.0.1:${rest}" \
                       --admin-endpoint "http://127.0.0.1:${admin}" \
                       version 2>&1 | grep '^Server:' || true)
    if [[ -z "$line" ]]; then
        printf "%-9s %-10s (no response — container down?)\n" "$name" "?"
        continue
    fi
    leader=$(echo "$line" | grep -oE 'leader=[^ ]+' | cut -d= -f2)
    printf "%-9s %-10s %s\n" "$name" "${leader:-?}" "$line"
done
