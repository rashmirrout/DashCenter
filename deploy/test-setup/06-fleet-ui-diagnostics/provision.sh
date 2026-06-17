#!/usr/bin/env bash
# 06-fleet-ui-diagnostics/provision.sh — load the rich superset (157 objects, 3 ns).
#
# Loads the YAML manifests under ./manifest/ into dashd. Prefers `dashctl
# apply -R -f` if a dashctl binary is reachable (1 RPC per file, generation
# tracking, dry-run support). Falls back to `python3 manifest/bootstrap.py`
# (pure-stdlib REST PUTs) if dashctl is not available.
#
# Usage:
#   ./provision.sh                     # dashctl preferred, bootstrap.py fallback
#   ./provision.sh --use-bootstrap     # force bootstrap.py (skip dashctl)
#   ./provision.sh --dry-run           # dashctl --dry-run (no fallback)
#   ./provision.sh --endpoint http://10.0.0.5:38443
#   ./provision.sh --max-retries 10 --max-wait-seconds 180   # retry transient errors

set -euo pipefail
cd "$(dirname "$0")"

ENDPOINT='http://127.0.0.1:38443'
USE_BOOTSTRAP=0
DRY_RUN=0
MAX_RETRIES="${MAX_RETRIES:-6}"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-90}"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --use-bootstrap) USE_BOOTSTRAP=1; shift ;;
        --dry-run) DRY_RUN=1; shift ;;
        --endpoint) ENDPOINT="$2"; shift 2 ;;
        --max-retries) MAX_RETRIES="$2"; shift 2 ;;
        --max-wait-seconds) MAX_WAIT_SECONDS="$2"; shift 2 ;;
        -h|--help) sed -n '2,15p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 2 ;;
    esac
done

case "$MAX_RETRIES" in ''|*[!0-9]*) echo "!! --max-retries must be a non-negative integer" >&2; exit 2 ;; esac
case "$MAX_WAIT_SECONDS" in ''|*[!0-9]*) echo "!! --max-wait-seconds must be a non-negative integer" >&2; exit 2 ;; esac

wait_for_control_plane() {
    local admin_ep="$1"
    local deadline=$(( $(date +%s) + MAX_WAIT_SECONDS ))
    while true; do
        if curl -fsS --max-time 2 "$admin_ep/admin/health" >/dev/null 2>&1; then return 0; fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
            echo "!! control plane not healthy at $admin_ep after ${MAX_WAIT_SECONDS}s" >&2; return 1
        fi
        sleep 2
    done
}

is_transient_network_error() {
    echo "$1" | grep -Eqi 'network error|connection reset by peer|EOF$|context deadline exceeded|i/o timeout|connection refused|server closed idle connection'
}

MANIFEST_DIR="$(pwd)/manifest"
[[ -d "$MANIFEST_DIR" ]] || { echo "!! manifest/ not found at $MANIFEST_DIR" >&2; exit 1; }

resolve_dashctl() {
    for candidate in ./dashctl ./dashctl.exe ../04-ha-fleet/dashctl ../04-ha-fleet/dashctl.exe; do
        if [[ -x "$candidate" ]]; then
            echo "$(cd "$(dirname "$candidate")" && pwd)/$(basename "$candidate")"; return
        fi
    done
    command -v dashctl 2>/dev/null || true
}

DASHCTL=""
[[ $USE_BOOTSTRAP -eq 0 ]] && DASHCTL="$(resolve_dashctl)"

if [[ -n "$DASHCTL" ]]; then
    echo "==> Provisioning via dashctl ($DASHCTL)"
    ADMIN_EP="${ENDPOINT/:38443/:37443}"
    # NOTE: dashctl `-R -f <dir>` would walk every file in manifest/ which
    # also contains bootstrap.py / bootstrap.sh / bootstrap.json. dashctl
    # tries to YAML-parse them and fails. So we enumerate *.yaml explicitly
    # and pass one -f per file. Sort-by-name preserves the 00..10 prefix
    # so FK validation (vnets before ENIs, etc.) holds.
    YAMLS=()
    while IFS= read -r -d '' f; do YAMLS+=("$f"); done < <(find "$MANIFEST_DIR" -maxdepth 1 -name '*.yaml' -print0 | sort -z)
    if [[ ${#YAMLS[@]} -eq 0 ]]; then
        echo "!! No *.yaml files found under $MANIFEST_DIR" >&2; exit 1
    fi
    ARGS=(--endpoint "$ENDPOINT" --admin-endpoint "$ADMIN_EP" apply)
    for f in "${YAMLS[@]}"; do ARGS+=(-f "$f"); done
    [[ $DRY_RUN -eq 1 ]] && ARGS+=(--dry-run server)
    attempt=1
    while true; do
        wait_for_control_plane "$ADMIN_EP" || exit 1
        echo "==> dashctl apply (attempt $attempt/$MAX_RETRIES)"
        if out=$("$DASHCTL" "${ARGS[@]}" 2>&1); then
            echo "$out"
            break
        fi
        echo "$out" >&2
        if is_transient_network_error "$out" && [ "$attempt" -lt "$MAX_RETRIES" ]; then
            backoff=$(( attempt * 2 ))
            echo "   transient failure; retrying in ${backoff}s..." >&2
            sleep "$backoff"; attempt=$(( attempt + 1 )); continue
        fi
        echo "!! dashctl apply failed. Try: ./provision.sh --use-bootstrap" >&2
        exit 1
    done
    echo "==> dashctl apply complete"
else
    if [[ $DRY_RUN -eq 1 ]]; then
        echo "!! --dry-run is only supported with dashctl. dashctl not found." >&2
        exit 1
    fi
    if [[ $USE_BOOTSTRAP -eq 0 ]]; then
        echo "   (dashctl not found in fleet dir, ../04-ha-fleet, or PATH — using bootstrap.py)"
    fi
    echo "==> Provisioning via bootstrap.py against $ENDPOINT"
    PY="$(command -v python3 || command -v python || true)"
    [[ -n "$PY" ]] || { echo "!! python not found on PATH." >&2; exit 1; }
    "$PY" "$MANIFEST_DIR/bootstrap.py" "$ENDPOINT"
fi

echo ""
echo "Verify loaded resources:"
echo "  curl -s http://127.0.0.1:38443/v1/default/vnets | python3 -m json.tool"
echo "  ./show-leader.sh"