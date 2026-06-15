#!/usr/bin/env bash
# 07-full-experiment/show-leader.sh — show which dashd is the current leader.
set -euo pipefail

ENDPOINTS=(
  "dashd-1:http://127.0.0.1:27443/admin/health"
  "dashd-2:http://127.0.0.1:27453/admin/health"
  "dashd-3:http://127.0.0.1:27463/admin/health"
)

for ep in "${ENDPOINTS[@]}"; do
  NAME="${ep%%:*}"
  URL="${ep#*:}"
  HEALTH=$(curl -s --max-time 3 "$URL" 2>/dev/null || echo '{}')
  LEADER=$(echo "$HEALTH" | python3 -c "import json,sys; d=json.load(sys.stdin); print('LEADER' if d.get('leader') else 'follower')" 2>/dev/null || echo "unreachable")
  DPUS=$(echo "$HEALTH" | python3 -c "import json,sys; d=json.load(sys.stdin); print(len(d.get('dpus',[])))" 2>/dev/null || echo "?")
  echo "$NAME: $LEADER (dpus=$DPUS)"
done
