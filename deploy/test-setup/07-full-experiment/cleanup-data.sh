#!/usr/bin/env bash
# 07-full-experiment/cleanup-data.sh — delete all provisioned objects.
set -euo pipefail
cd "$(dirname "$0")"

ENDPOINT="${1:-http://localhost:28443}"
HOST_BIN="$(cd ../../.. && pwd)/src/impl-go/dashctl/bin/dashctl"
if [ -x "$HOST_BIN" ]; then
  DC="$HOST_BIN --endpoint $ENDPOINT --insecure"
else
  DC="docker compose run --rm dashctl"
fi

TENANTS=(bank retail media iot analytics telecom health fintech gaming logistics)
TIERS=(web api db cache worker)

echo "==> Deleting ACL policies"
for t in "${TENANTS[@]}"; do
  $DC delete acl-policy "acl-$t-inbound" --ignore-not-found 2>/dev/null || true
  $DC delete acl-policy "acl-$t-outbound" --ignore-not-found 2>/dev/null || true
done
for t in bank retail media iot analytics; do
  $DC delete acl-policy "acl-$t-staging-restricted" --ignore-not-found 2>/dev/null || true
done
for tier in "${TIERS[@]}"; do
  $DC delete acl-policy "acl-tier-$tier" --ignore-not-found 2>/dev/null || true
done
$DC delete acl-policy acl-platform-ssh --ignore-not-found 2>/dev/null || true
$DC delete acl-policy acl-platform-monitor --ignore-not-found 2>/dev/null || true

echo "==> Deleting route policies"
for t in "${TENANTS[@]}"; do
  $DC delete route-policy "rp-$t-prod" --ignore-not-found 2>/dev/null || true
done
$DC delete route-policy rp-platform-mgmt --ignore-not-found 2>/dev/null || true
$DC delete route-policy rp-platform-dmz --ignore-not-found 2>/dev/null || true
for tier in "${TIERS[@]}"; do
  $DC delete route-policy "rp-tier-$tier" --ignore-not-found 2>/dev/null || true
done

echo "==> Deleting HA sets"
for i in $(seq 1 5); do
  $DC delete ha-set "ha-appliance-$i" --ignore-not-found 2>/dev/null || true
  $DC delete ha-set "ha-cross-zone-$i" --ignore-not-found 2>/dev/null || true
done

echo "==> Deleting VNet mappings, ENIs, tunnels, VNets"
# This is a large set — use dashctl to list and delete
for kind in vnet-mapping eni service-tunnel vnet; do
  echo "    Deleting all $kind..."
  NAMES=$($DC get "$kind" -o name --ignore-not-found 2>/dev/null | grep "^$kind/" | sed "s|^$kind/||" || true)
  for name in $NAMES; do
    $DC delete "$kind" "$name" --ignore-not-found 2>/dev/null || true
  done
done

echo "All objects deleted"
