#!/usr/bin/env sh
# push-vnet.sh — push a vnet across the 5-DPU dashd-fleet and verify convergence.
#
# Usage (from any directory):
#   ./deploy/dashd-fleet/push-vnet.sh                   # default: vnet-fleet-001, vni=1001
#   ./deploy/dashd-fleet/push-vnet.sh vnet-foo 4242     # custom name + vni
#
# What it does:
#   1. PUT /v1/default/vnets/<name> on dashd REST (port 8443)
#   2. POST /v1/reconcile — force the reconciler to run now (not wait 30s)
#   3. GET /admin/inventory — sanity-check all 5 DPUs are UP
#   4. Poll each dash-sim admin :8081..8085 for the vnet kind in observed state

set -eu

NAME="${1:-vnet-fleet-001}"
VNI="${2:-1001}"
DASHD_REST="${DASHD_REST:-http://localhost:8443}"
DASHD_ADMIN="${DASHD_ADMIN:-http://localhost:7443}"

color_g() { printf '\033[32m%s\033[0m\n' "$1"; }
color_r() { printf '\033[31m%s\033[0m\n' "$1"; }
color_y() { printf '\033[33m%s\033[0m\n' "$1"; }

color_y "[1/4] Pushing vnet name=$NAME vni=$VNI to dashd at $DASHD_REST"
HTTP_CODE=$(curl -sS -o /tmp/push-vnet-resp.json -w "%{http_code}" \
  -X PUT "$DASHD_REST/v1/default/vnets/$NAME" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$NAME\",\"vni\":$VNI}")
if [ "$HTTP_CODE" != "200" ]; then
  color_r "FAIL: PUT returned HTTP $HTTP_CODE"
  cat /tmp/push-vnet-resp.json
  exit 1
fi
color_g "      OK ($(cat /tmp/push-vnet-resp.json))"

color_y "[2/4] Triggering immediate reconcile"
curl -sSf -X POST "$DASHD_REST/v1/reconcile" >/dev/null
color_g "      OK"

color_y "[3/4] Checking inventory — expect 5 × DPU_STATE_UP"
INV=$(curl -sSf "$DASHD_ADMIN/admin/inventory")
UP_COUNT=$(printf '%s' "$INV" | grep -o '"DPU_STATE_UP"' | wc -l | tr -d ' ')
TOTAL=$(printf '%s' "$INV" | grep -o '"id":' | wc -l | tr -d ' ')
if [ "$UP_COUNT" -lt 5 ]; then
  color_r "FAIL: only $UP_COUNT / $TOTAL DPUs are UP"
  echo "$INV"
  exit 1
fi
color_g "      OK ($UP_COUNT / $TOTAL DPUs UP)"

color_y "[4/4] Verifying vnet '$NAME' landed on each dash-sim (poll up to 30s)"
ok=0
fail=0
for port in 8081 8082 8083 8084 8085; do
  found=0
  i=0
  while [ "$i" -lt 30 ]; do
    if curl -sSf "http://localhost:$port/admin/dump" 2>/dev/null \
       | grep -q "\"key\":\"$NAME\""; then
      found=1
      break
    fi
    sleep 1
    i=$((i + 1))
  done
  if [ "$found" -eq 1 ]; then
    color_g "      sim:$port  vnet '$NAME' present (after ${i}s)"
    ok=$((ok + 1))
  else
    color_r "      sim:$port  vnet '$NAME' NOT present after 30s"
    fail=$((fail + 1))
  fi
done

echo
if [ "$fail" -eq 0 ]; then
  color_g "PASS: vnet $NAME (vni=$VNI) converged to all 5 DPUs"
  exit 0
fi
color_r "FAIL: vnet $NAME converged to $ok / 5 DPUs"
exit 1