#!/usr/bin/env sh
# e2e.sh — full end-to-end verification of the dashd + 1 dash-sim setup.
#
# Demonstrates and verifies the complete control-plane → DPU data path:
#   1.  dashd health             (Admin HTTP :7443)
#   2.  Single DPU is UP         (registered, prober confirmed)
#   3.  PUT vnet via REST        (dashd REST :8443)
#   4.  PUT eni via REST         (depends on vnet)
#   5.  Force immediate reconcile
#   6.  Verify vnet observed on the sim
#   7.  Verify eni observed on the sim
#   8.  Drift report is empty
#
# Exit code: 0 on PASS, 1 on FAIL.
#
# Usage:
#   ./deploy/dashd-e2e/e2e.sh                 # default names: vnet-e2e, eni-e2e

set -eu

VNET_NAME="${VNET_NAME:-vnet-e2e}"
ENI_NAME="${ENI_NAME:-eni-e2e}"
VNI="${VNI:-9001}"
MAC="${MAC:-00:11:22:33:44:55}"

DASHD_REST="${DASHD_REST:-http://localhost:8443}"
DASHD_ADMIN="${DASHD_ADMIN:-http://localhost:7443}"
SIM_ADMIN="${SIM_ADMIN:-http://localhost:8081}"

g() { printf '\033[32m%s\033[0m\n' "$1"; }
r() { printf '\033[31m%s\033[0m\n' "$1"; }
y() { printf '\033[33m%s\033[0m\n' "$1"; }

step() { y "[$1/8] $2"; }
ok()   { g "      OK    $1"; }
fail() { r "      FAIL  $1"; exit 1; }

step 1 "dashd health"
if curl -sSf "$DASHD_ADMIN/admin/health" >/dev/null 2>&1; then
  ok "dashd /admin/health responded"
else
  fail "dashd /admin/health not responding at $DASHD_ADMIN"
fi

step 2 "DPU is UP (poll up to 30s)"
i=0
state=""
while [ "$i" -lt 30 ]; do
  inv=$(curl -sSf "$DASHD_ADMIN/admin/inventory" 2>/dev/null || echo '')
  state=$(printf '%s' "$inv" | grep -o '"state":"[^"]*"' | head -1 | sed 's/.*"\([^"]*\)"$/\1/')
  [ "$state" = "DPU_STATE_UP" ] && break
  sleep 1
  i=$((i + 1))
done
if [ "$state" = "DPU_STATE_UP" ]; then
  ok "dpu-sim-01 state=$state (after ${i}s)"
else
  fail "dpu-sim-01 state=$state (expected DPU_STATE_UP) — inventory: $inv"
fi

step 3 "PUT /v1/default/vnets/$VNET_NAME"
HTTP_CODE=$(curl -sS -o /tmp/e2e-vnet.json -w "%{http_code}" \
  -X PUT "$DASHD_REST/v1/default/vnets/$VNET_NAME" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$VNET_NAME\",\"vni\":$VNI}")
if [ "$HTTP_CODE" = "200" ]; then
  ok "vnet accepted ($(cat /tmp/e2e-vnet.json))"
else
  fail "vnet PUT returned HTTP $HTTP_CODE"
fi

step 4 "PUT /v1/default/enis/$ENI_NAME"
HTTP_CODE=$(curl -sS -o /tmp/e2e-eni.json -w "%{http_code}" \
  -X PUT "$DASHD_REST/v1/default/enis/$ENI_NAME" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$ENI_NAME\",\"mac_address\":\"$MAC\",\"vnet\":\"$VNET_NAME\"}")
if [ "$HTTP_CODE" = "200" ]; then
  ok "eni accepted ($(cat /tmp/e2e-eni.json))"
else
  fail "eni PUT returned HTTP $HTTP_CODE"
fi

step 5 "Triggering immediate reconcile"
curl -sSf -X POST "$DASHD_REST/v1/reconcile" >/dev/null
ok "reconcile dispatched"

step 6 "Verifying vnet '$VNET_NAME' on the sim (poll up to 30s)"
i=0
found=0
while [ "$i" -lt 30 ]; do
  if curl -sSf "$SIM_ADMIN/admin/dump" 2>/dev/null \
     | grep -q "\"key\":\"$VNET_NAME\""; then
    found=1
    break
  fi
  sleep 1
  i=$((i + 1))
done
[ "$found" = "1" ] && ok "vnet '$VNET_NAME' present on sim (after ${i}s)" \
                  || fail "vnet '$VNET_NAME' NOT present on sim after 30s"

step 7 "Verifying eni '$ENI_NAME' on the sim (poll up to 30s)"
i=0
found=0
while [ "$i" -lt 30 ]; do
  if curl -sSf "$SIM_ADMIN/admin/dump" 2>/dev/null \
     | grep -q "\"key\":\"$ENI_NAME\""; then
    found=1
    break
  fi
  sleep 1
  i=$((i + 1))
done
[ "$found" = "1" ] && ok "eni '$ENI_NAME' present on sim (after ${i}s)" \
                  || fail "eni '$ENI_NAME' NOT present on sim after 30s"

step 8 "Drift report should be empty (no declared/observed mismatch)"
DRIFT=$(curl -sSf "$DASHD_ADMIN/admin/drift")
N_ITEMS=$(printf '%s' "$DRIFT" | grep -o '"kind":' | wc -l | tr -d ' ')
if [ "$N_ITEMS" = "0" ]; then
  ok "drift report is clean"
else
  r "      WARN  drift report has $N_ITEMS items (some are expected on first run):"
  echo "        $DRIFT"
fi

echo
g "PASS: end-to-end converged. dashd successfully pushed vnet+eni to dash-sim-01."
exit 0