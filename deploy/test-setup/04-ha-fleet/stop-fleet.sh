#!/usr/bin/env bash
# 04-ha-fleet/stop-fleet.sh — tear down the fleet and remove volumes.
#
# Usage:
#   ./stop-fleet.sh                     # graceful shutdown + volume removal
#   ./stop-fleet.sh --keep-volumes      # graceful shutdown only
set -euo pipefail
cd "$(dirname "$0")"

KEEP_VOLUMES=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --keep-volumes) KEEP_VOLUMES=1; shift ;;
        -h|--help) sed -n '2,7p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

echo "==> Stopping 04-ha-fleet"
if [[ $KEEP_VOLUMES -eq 1 ]]; then
    docker compose down
else
    docker compose down -v
fi
echo "==> Done"
