#!/usr/bin/env bash
# 05-full-console/start-fleet.sh — bring up the full DashCenter stack.
#
# Brings up: etcd + 10 dash-sim + 3 dashd (+ optionally dashw web console).
#
# Usage:
#   ./start-fleet.sh                    # build + start (no dashw) + wait + dashctl context
#   ./start-fleet.sh --with-console     # also build & start dashw at :3000
#   ./start-fleet.sh --skip-build       # reuse cached images
#   ./start-fleet.sh --skip-context     # skip dashctl context setup
#   ./start-fleet.sh --skip-dashctl-build  # skip auto-building dashctl from source
#   ./start-fleet.sh --ready-timeout 120

set -euo pipefail
cd "$(dirname "$0")"

WITH_CONSOLE=0
SKIP_BUILD=0
SKIP_CONTEXT=0
SKIP_DASHCTL_BUILD=0
READY_TIMEOUT_SEC=90
while [[ $# -gt 0 ]]; do
    case "$1" in
        --with-console) WITH_CONSOLE=1; shift ;;
        --skip-build) SKIP_BUILD=1; shift ;;
        --skip-context) SKIP_CONTEXT=1; shift ;;
        --skip-dashctl-build) SKIP_DASHCTL_BUILD=1; shift ;;
        --ready-timeout) READY_TIMEOUT_SEC="$2"; shift 2 ;;
        -h|--help) sed -n '2,12p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

echo "==> Verifying docker is available"
docker version --format '{{.Server.Version}}' > /dev/null

CORE_SERVICES=(
    etcd
    dash-sim-01 dash-sim-02 dash-sim-03 dash-sim-04 dash-sim-05
    dash-sim-06 dash-sim-07 dash-sim-08 dash-sim-09 dash-sim-10
    dashd-1 dashd-2 dashd-3
)
ALL_SERVICES=("${CORE_SERVICES[@]}")
[[ $WITH_CONSOLE -eq 1 ]] && ALL_SERVICES+=(dashw)

if [[ $SKIP_BUILD -eq 0 ]]; then
    # Build only one of each image kind; replicas reuse the cached image.
    BUILD_TARGETS=(dashd-1 dash-sim-01)
    [[ $WITH_CONSOLE -eq 1 ]] && BUILD_TARGETS+=(dashw)
    echo "==> Building images: ${BUILD_TARGETS[*]}"
    docker compose build "${BUILD_TARGETS[@]}"
fi

SVC_LABEL=$([[ $WITH_CONSOLE -eq 1 ]] && echo "14 (incl. dashw)" || echo "13 (core)")
echo "==> Starting fleet (${SVC_LABEL} services)"
docker compose up -d "${ALL_SERVICES[@]}"

# Build dashctl from source while the fleet is starting up.
# This runs in parallel with leader election — Go compilation takes
# ~5-10s on a warm cache, and we need to wait ~10s for the leader
# anyway. If Go is not installed, the script continues gracefully
# and provision.sh falls back to bootstrap.py.
if [[ $SKIP_DASHCTL_BUILD -eq 0 ]]; then
    ./build-dashctl.sh
fi

echo "==> Waiting for a dashd leader to be elected (max ${READY_TIMEOUT_SEC}s)"
deadline=$(( $(date +%s) + READY_TIMEOUT_SEC ))
leader=""
# Probe 127.0.0.1 (not 'localhost') to match start-fleet.ps1.
while [[ $(date +%s) -lt $deadline ]]; do
    for port in 27443 27453 27463; do
        if r=$(curl -fsS --max-time 3 "http://127.0.0.1:${port}/admin/leader" 2>/dev/null); then
            if echo "$r" | grep -q '"leader":true'; then
                leader=$(echo "$r" | sed -n 's/.*"leader_id":"\([^"]*\)".*/\1/p')
                break
            fi
        fi
    done
    [[ -n "$leader" ]] && break
    sleep 0.5
done

if [[ -z "$leader" ]]; then
    echo "!! No leader within ${READY_TIMEOUT_SEC}s. Check 'docker compose logs dashd-1 dashd-2 dashd-3'." >&2
    exit 1
fi
echo "==> Leader: $leader"
echo ""

if [[ $SKIP_CONTEXT -eq 0 ]]; then
    DASHCTL=""
    for candidate in \
        "./dashctl" "./dashctl.exe" \
        "../04-ha-fleet/dashctl" "../04-ha-fleet/dashctl.exe"; do
        if [[ -x "$candidate" ]]; then DASHCTL="$(cd "$(dirname "$candidate")" && pwd)/$(basename "$candidate")"; break; fi
    done
    if [[ -z "$DASHCTL" ]] && command -v dashctl >/dev/null 2>&1; then
        DASHCTL="$(command -v dashctl)"
    fi

    if [[ -n "$DASHCTL" ]]; then
        "$DASHCTL" config set-context full-console \
            --endpoint http://127.0.0.1:28443 \
            --admin-endpoint http://127.0.0.1:27443 >/dev/null
        "$DASHCTL" config use-context full-console >/dev/null
        echo "==> dashctl context 'full-console' active (using $DASHCTL)"
    else
        echo "   (dashctl not found in fleet dir, ../04-ha-fleet, or PATH — skipped context setup)"
        echo "   Build it once: (cd ../../../src/impl-go/dashctl && go build -o ../../../deploy/test-setup/05-full-console/dashctl ./cmd/dashctl)"
    fi
fi

echo ""
echo "Per-node REST/admin endpoints (host):"
echo "  dashd-1: http://127.0.0.1:28443  (admin :27443)"
echo "  dashd-2: http://127.0.0.1:28453  (admin :27453)"
echo "  dashd-3: http://127.0.0.1:28463  (admin :27463)"
[[ $WITH_CONSOLE -eq 1 ]] && echo "  dashw  : http://localhost:3000   (web console)"
echo ""
echo "Confirm leader on every node:"
echo "  ./show-leader.sh"
echo ""
echo "Load the 157-object pre-built scenario:"
echo "  ./provision.sh                            # uses dashctl if found, else bootstrap.py"
echo "  python3 manifest/bootstrap.py             # direct REST PUTs (no dashctl required)"
echo ""
echo "Clean fleet teardown:"
echo "  ./stop-fleet.sh                           # down + remove volumes (clean slate)"
echo "  ./stop-fleet.sh --keep-volumes            # down + keep etcd state"