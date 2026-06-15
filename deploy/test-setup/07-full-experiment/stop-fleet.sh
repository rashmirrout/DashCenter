#!/usr/bin/env bash
# 07-full-experiment/stop-fleet.sh — tear down the experiment stack.
#
# Usage:
#   ./stop-fleet.sh                # stop + remove volumes
#   ./stop-fleet.sh --keep-volumes # stop but keep etcd state
#   ./stop-fleet.sh --remove-images # also remove built images
set -euo pipefail
cd "$(dirname "$0")"

ARGS="down"
for arg in "$@"; do
  case "$arg" in
    --keep-volumes)  ;; # don't add -v
    --remove-images) ARGS="$ARGS --rmi local" ;;
    *)               ;;
  esac
done

# Add -v unless --keep-volumes was specified
if ! echo "$*" | grep -q -- "--keep-volumes"; then
  ARGS="$ARGS -v"
fi

echo "==> Stopping fleet"
docker compose $ARGS

echo "Fleet stopped"
