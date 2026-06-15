#!/usr/bin/env bash
# 07-full-experiment/provision.sh — apply the 449-object manifest set.
#
# Applies manifests in dependency order:
#   00-vnets (25) → 01-service-tunnels (5) → 02-enis (120) →
#   03-vnet-mappings (240) → 04-route-policies (17) → 05-acl-policies (32) →
#   06-ha-sets (10)
set -euo pipefail
cd "$(dirname "$0")"

ENDPOINT="${1:-http://localhost:28443}"
FORCE=""
[ "${2:-}" = "--force" ] && FORCE="--force"

# Find dashctl
DASHCTL=""
HOST_BIN="$(cd ../../.. && pwd)/src/impl-go/dashctl/bin/dashctl"
if [ -x "$HOST_BIN" ]; then
  DASHCTL="$HOST_BIN --endpoint $ENDPOINT --insecure"
  echo "Using host dashctl: $HOST_BIN"
else
  DASHCTL="docker compose run --rm dashctl"
  echo "Using container dashctl"
fi

MANIFESTS=(
  manifest/00-vnets.yaml
  manifest/01-service-tunnels.yaml
  manifest/02-enis.yaml
  manifest/03-vnet-mappings.yaml
  manifest/04-route-policies.yaml
  manifest/05-acl-policies.yaml
  manifest/06-ha-sets.yaml
)

for m in "${MANIFESTS[@]}"; do
  echo "==> Applying $m"
  $DASHCTL apply -f "$m" $FORCE
done

echo ""
echo "Provisioned ~449 objects"
echo ""
echo "Verify:"
echo "  dashctl get vnet -o table            # 25 VNets"
echo "  dashctl get eni -o wide              # 120 ENIs"
echo "  dashctl get vnet-mapping -o table     # 240 mappings"
echo "  dashctl get route-policy -o table     # 17 route policies"
echo "  dashctl get acl-policy -o table       # 32 ACL policies"
echo "  dashctl get ha-set -o table           # 10 HA sets"
echo "  dashctl dpu list -o table             # 50 DPUs"
