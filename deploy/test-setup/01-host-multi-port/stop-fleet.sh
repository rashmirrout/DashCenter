#!/usr/bin/env bash
# Stop a fleet started by start-fleet.sh.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
state_path="$here/.fleet-state.json"

if [[ ! -f "$state_path" ]]; then
  echo "No $state_path — nothing to stop."
  exit 0
fi

# Extract pids without requiring jq.
pids=$(grep -oE '"pid":[[:space:]]*[0-9]+' "$state_path" | grep -oE '[0-9]+')
for pid in $pids; do
  if kill -0 "$pid" 2>/dev/null; then
    echo "  Stopping pid $pid ..."
    kill "$pid" 2>/dev/null || true
  else
    echo "  pid $pid already gone"
  fi
done

# Give them a moment, then force.
sleep 1
for pid in $pids; do
  if kill -0 "$pid" 2>/dev/null; then
    kill -9 "$pid" 2>/dev/null || true
  fi
done

rm -f "$state_path"
echo "Fleet stopped."
