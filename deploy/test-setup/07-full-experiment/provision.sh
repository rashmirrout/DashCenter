#!/usr/bin/env bash
# 07-full-experiment/provision.sh — apply the 449-object manifest set.
#
# Applies manifests in dependency order:
#   00-vnets (25) → 01-service-tunnels (5) → 02-enis (120) →
#   03-vnet-mappings (240) → 04-route-policies (17) → 05-acl-policies (32) →
#   06-ha-sets (10)
set -euo pipefail
cd "$(dirname "$0")"

usage() {
  cat <<'EOF'
Usage:
  provision.sh [endpoint] [--force]
  provision.sh [--endpoint URL] [--admin-endpoint URL] [--force]
               [--max-retries N] [--max-wait-seconds N]

Options:
  --endpoint URL           dashd REST endpoint (default: http://localhost:28443)
  --admin-endpoint URL     dashd admin endpoint (default: endpoint with :28443 -> :27443)
  --force                  pass --force to dashctl apply
  --max-retries N          retries per manifest for transient network errors (default: 6)
  --max-wait-seconds N     max wait for /admin/health before each attempt (default: 90)
  -h, --help               show this help

Notes:
  - Env vars MAX_RETRIES / MAX_WAIT_SECONDS / DASHCTL_ADMIN_ENDPOINT are still supported.
  - Positional endpoint is kept for backward compatibility.
EOF
}

ENDPOINT="http://localhost:28443"
FORCE=""
ADMIN_ENDPOINT="${DASHCTL_ADMIN_ENDPOINT:-}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-90}"
MAX_RETRIES="${MAX_RETRIES:-6}"

POSITIONAL=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    --endpoint)
      [ "$#" -ge 2 ] || { echo "ERROR: --endpoint requires a value" >&2; exit 2; }
      ENDPOINT="$2"
      shift 2
      ;;
    --admin-endpoint)
      [ "$#" -ge 2 ] || { echo "ERROR: --admin-endpoint requires a value" >&2; exit 2; }
      ADMIN_ENDPOINT="$2"
      shift 2
      ;;
    --force)
      FORCE="--force"
      shift
      ;;
    --max-retries)
      [ "$#" -ge 2 ] || { echo "ERROR: --max-retries requires a value" >&2; exit 2; }
      MAX_RETRIES="$2"
      shift 2
      ;;
    --max-wait-seconds)
      [ "$#" -ge 2 ] || { echo "ERROR: --max-wait-seconds requires a value" >&2; exit 2; }
      MAX_WAIT_SECONDS="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      break
      ;;
    -*)
      echo "ERROR: unknown option: $1" >&2
      usage
      exit 2
      ;;
    *)
      POSITIONAL+=("$1")
      shift
      ;;
  esac
done

# Backward compatibility: first positional arg is endpoint.
if [ "${#POSITIONAL[@]}" -ge 1 ]; then
  ENDPOINT="${POSITIONAL[0]}"
fi

if [ -z "$ADMIN_ENDPOINT" ]; then
  ADMIN_ENDPOINT="${ENDPOINT/:28443/:27443}"
fi

case "$MAX_RETRIES" in
  ''|*[!0-9]*) echo "ERROR: --max-retries must be a non-negative integer" >&2; exit 2 ;;
esac
case "$MAX_WAIT_SECONDS" in
  ''|*[!0-9]*) echo "ERROR: --max-wait-seconds must be a non-negative integer" >&2; exit 2 ;;
esac

# Find dashctl
declare -a DASHCTL_CMD
HOST_BIN="$(cd ../../.. && pwd)/src/impl-go/dashctl/bin/dashctl"
if [ -x "$HOST_BIN" ]; then
  DASHCTL_CMD=("$HOST_BIN" "--endpoint" "$ENDPOINT" "--insecure")
  echo "Using host dashctl: $HOST_BIN"
else
  DASHCTL_CMD=(docker compose run --rm dashctl)
  echo "Using container dashctl"
fi

wait_for_control_plane() {
  local deadline=$(( $(date +%s) + MAX_WAIT_SECONDS ))
  local now
  while true; do
    # Health endpoint is enough to gate retries. Leader may briefly flap during elections.
    if curl -fsS --max-time 2 "$ADMIN_ENDPOINT/admin/health" >/dev/null 2>&1; then
      return 0
    fi
    now=$(date +%s)
    if [ "$now" -ge "$deadline" ]; then
      echo "ERROR: control plane not healthy at $ADMIN_ENDPOINT after ${MAX_WAIT_SECONDS}s" >&2
      return 1
    fi
    sleep 2
  done
}

is_transient_network_error() {
  local msg="$1"
  echo "$msg" | grep -Eqi \
    'network error|connection reset by peer|read: connection reset by peer|EOF$|context deadline exceeded|i/o timeout|connection refused|server closed idle connection'
}

apply_with_retry() {
  local manifest="$1"
  local attempt=1
  local output

  while [ "$attempt" -le "$MAX_RETRIES" ]; do
    wait_for_control_plane
    echo "==> Applying $manifest (attempt $attempt/$MAX_RETRIES)"
    if output=$("${DASHCTL_CMD[@]}" apply -f "$manifest" ${FORCE:+$FORCE} 2>&1); then
      echo "$output"
      return 0
    fi

    echo "$output" >&2
    if is_transient_network_error "$output"; then
      if [ "$attempt" -lt "$MAX_RETRIES" ]; then
        local backoff=$(( attempt * 2 ))
        echo "WARN: transient apply failure; retrying in ${backoff}s..." >&2
        sleep "$backoff"
        attempt=$(( attempt + 1 ))
        continue
      fi
      echo "ERROR: exhausted retries for $manifest" >&2
      return 1
    fi

    echo "ERROR: non-transient apply failure for $manifest" >&2
    return 1
  done
}

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
  apply_with_retry "$m"
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
