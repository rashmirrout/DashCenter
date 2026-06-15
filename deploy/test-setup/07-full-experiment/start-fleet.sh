#!/usr/bin/env bash
# 07-full-experiment/start-fleet.sh — bring up the 50-DPU experiment stack.
#
# Usage:
#   ./start-fleet.sh                # build + start (no dashw) + wait for leader
#   ./start-fleet.sh --with-console # also start dashw at :3000
#   ./start-fleet.sh --skip-build   # reuse cached images
set -euo pipefail
cd "$(dirname "$0")"

WITH_CONSOLE=false
SKIP_BUILD=false
TIMEOUT=180

for arg in "$@"; do
  case "$arg" in
    --with-console) WITH_CONSOLE=true ;;
    --skip-build)   SKIP_BUILD=true ;;
    --timeout=*)    TIMEOUT="${arg#*=}" ;;
  esac
done

echo "==> Verifying docker is available"
docker version --format '{{.Server.Version}}' >/dev/null

# Build one of each image kind
if [ "$SKIP_BUILD" = false ]; then
  echo "==> Building images (dash-sim, dashd$([ "$WITH_CONSOLE" = true ] && echo ', dashw'))"
  TARGETS="dash-sim-01 dashd-1"
  [ "$WITH_CONSOLE" = true ] && TARGETS="$TARGETS dashw"
  docker compose build $TARGETS
  echo "    Build done"
fi

# Generate service list
SIM_SERVICES=""
for i in $(seq -w 1 50); do
  SIM_SERVICES="$SIM_SERVICES dash-sim-$i"
done
CORE="etcd-1 etcd-2 $SIM_SERVICES dashd-1 dashd-2 dashd-3"
SERVICES="$CORE"
[ "$WITH_CONSOLE" = true ] && SERVICES="$CORE dashw"

echo "==> Starting 56 services"
docker compose up -d $SERVICES

echo "==> Waiting for etcd cluster health"
DEADLINE=$((SECONDS + 60))
while [ $SECONDS -lt $DEADLINE ]; do
  if docker exec dc-exp-etcd-1 etcdctl endpoint health --endpoints=http://127.0.0.1:2379 2>&1 | grep -q "true"; then
    echo "    etcd-1 healthy"
    break
  fi
  sleep 2
done

echo "==> Waiting for dashd leader election (timeout ${TIMEOUT}s)"
DEADLINE=$((SECONDS + TIMEOUT))
while [ $SECONDS -lt $DEADLINE ]; do
  HEALTH=$(curl -s --max-time 3 http://127.0.0.1:27443/admin/health 2>/dev/null || true)
  if echo "$HEALTH" | grep -q '"leader":true'; then
    NODE=$(echo "$HEALTH" | python3 -c "import json,sys; print(json.load(sys.stdin).get('node_id','?'))" 2>/dev/null || echo "?")
    echo "    Leader: $NODE"
    break
  fi
  sleep 3
done

echo ""
echo "Fleet is up: 50 DPU sims + 3 dashd + 2 etcd"
echo "  REST:  http://localhost:28443"
echo "  Admin: http://localhost:27443"
[ "$WITH_CONSOLE" = true ] && echo "  Console: http://localhost:3000"
echo ""
echo "Next: ./provision.sh"
