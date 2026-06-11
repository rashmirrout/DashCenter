#!/usr/bin/env bash
# 04-ha-fleet/start-fleet.sh — bring up the full HA fleet.
#
# Usage:
#   ./start-fleet.sh                    # build + start + wait + setup dashctl context
#   ./start-fleet.sh --skip-build       # reuse cached images
#   ./start-fleet.sh --skip-context     # skip dashctl context setup
set -euo pipefail
cd "$(dirname "$0")"

SKIP_BUILD=0
SKIP_CONTEXT=0
READY_TIMEOUT_SEC=60
while [[ $# -gt 0 ]]; do
    case "$1" in
        --skip-build) SKIP_BUILD=1; shift ;;
        --skip-context) SKIP_CONTEXT=1; shift ;;
        --ready-timeout) READY_TIMEOUT_SEC="$2"; shift 2 ;;
        -h|--help)
            sed -n '2,7p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

echo "==> Verifying docker is available"
docker version --format '{{.Server.Version}}' > /dev/null

if [[ $SKIP_BUILD -eq 0 ]]; then
    echo "==> Building dashd, dashctl, dash-sim images"
    docker compose build dashd-1 dashctl dash-sim-1
fi

echo "==> Starting fleet (etcd + 5 dash-sim + 3 dashd)"
docker compose up -d etcd dash-sim-1 dash-sim-2 dash-sim-3 dash-sim-4 dash-sim-5 dashd-1 dashd-2 dashd-3

echo "==> Waiting for a dashd leader to be elected (max ${READY_TIMEOUT_SEC}s)"
deadline=$(( $(date +%s) + READY_TIMEOUT_SEC ))
leader=""
# Probe 127.0.0.1 (not 'localhost') to match start-fleet.ps1: on some hosts
# 'localhost' resolves to ::1 first and the IPv6 connect blocks before the
# IPv4 fallback kicks in.
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

# Auto-configure dashctl context (no --endpoint needed on subsequent calls).
if [[ $SKIP_CONTEXT -eq 0 ]]; then
    DASHCTL="./dashctl"; [[ -x ./dashctl.exe ]] && DASHCTL="./dashctl.exe"
    if [[ -x "$DASHCTL" ]]; then
        "$DASHCTL" config set-context ha-fleet \
            --endpoint http://127.0.0.1:28443 \
            --admin-endpoint http://127.0.0.1:27443 >/dev/null
        "$DASHCTL" config use-context ha-fleet >/dev/null
        echo "==> dashctl context 'ha-fleet' active (no --endpoint needed)"
    else
        echo "   (dashctl not in fleet dir — skipped context setup; pass --skip-context to silence)"
    fi
fi
echo ""
echo "Per-node REST/admin endpoints (host):"
echo "  dashd-1: http://127.0.0.1:28443  (admin :27443)"
echo "  dashd-2: http://127.0.0.1:28453  (admin :27453)"
echo "  dashd-3: http://127.0.0.1:28463  (admin :27463)"
echo ""
echo "Confirm leader on every node:"
echo "  ./show-leader.sh"
echo ""
echo "Apply the pre-built manifest set (~130 objects across 2 namespaces):"
echo "  ./dashctl apply -R -f ./manifest"
