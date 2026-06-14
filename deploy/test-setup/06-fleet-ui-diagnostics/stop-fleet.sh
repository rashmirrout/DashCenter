#!/usr/bin/env bash
# 06-fleet-ui-diagnostics/stop-fleet.sh — tear down the fleet and remove volumes.
#
# Usage:
#   ./stop-fleet.sh                   # graceful shutdown + volume removal (clean slate)
#   ./stop-fleet.sh --keep-volumes    # graceful shutdown only (preserves etcd state)
#   ./stop-fleet.sh --remove-images   # also remove the built images (deep clean)

set -euo pipefail
cd "$(dirname "$0")"

KEEP_VOLUMES=0
REMOVE_IMAGES=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --keep-volumes) KEEP_VOLUMES=1; shift ;;
        --remove-images) REMOVE_IMAGES=1; shift ;;
        -h|--help) sed -n '2,7p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

echo "==> Stopping 06-fleet-ui-diagnostics"
ARGS=()
[[ $KEEP_VOLUMES -eq 0 ]] && ARGS+=(-v)
[[ $REMOVE_IMAGES -eq 1 ]] && ARGS+=(--rmi local)

if [[ ${#ARGS[@]} -gt 0 ]]; then
    docker compose down "${ARGS[@]}"
else
    docker compose down
fi
echo "==> Done"