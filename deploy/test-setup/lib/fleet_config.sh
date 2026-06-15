#!/usr/bin/env bash
# fleet_config.sh — shared loader + validator for DashCenter test-setup configs.
#
# Sourceable: `source "$(dirname "$0")/../lib/fleet_config.sh"`.
#
# Public functions:
#   fleet_resolve_config_path [-c <path>]    -> echoes absolute path
#   fleet_load <path>                         -> populates FLEET_JSON (string, JSON)
#   fleet_validate [--allow-privileged]       -> exits non-zero on failure
#   fleet_get '<jq-expr>'                     -> echoes jq query result
#   fleet_dpu_count                           -> count of dpus
#   fleet_dpu '<index>' '<jq-expr>'           -> echoes a per-DPU field
#   fleet_repo_root                           -> echoes path to repo root
#   fleet_resolve_path <base> <relpath>       -> echoes absolute path
#
# Requires:
#   - jq         (always)
#   - yq         (only if loading a .yaml/.yml file; Mike Farah's Go port)

set -o pipefail

FLEET_JSON=""
FLEET_SOURCE=""
FLEET_BASE_DIR=""

# Path of this file's directory (lib/). Works under bash + POSIX shells with BASH_SOURCE.
_FLEET_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
_TEST_SETUP_ROOT="$(cd "$_FLEET_LIB_DIR/.." && pwd)"

fleet_repo_root() {
    local dir="$_FLEET_LIB_DIR"
    for _ in 1 2 3 4 5 6 7 8; do
        if [[ -f "$dir/src/impl-go/go.work" ]]; then echo "$dir"; return 0; fi
        local parent
        parent="$(cd "$dir/.." && pwd)"
        [[ "$parent" == "$dir" ]] && break
        dir="$parent"
    done
    echo "fleet_repo_root: src/impl-go/go.work not found above $_FLEET_LIB_DIR" >&2
    return 1
}

fleet_resolve_path() {
    # $1 = base directory, $2 = relative POSIX path (or absolute).
    local base="$1" rel="$2"
    case "$rel" in
        /*|[A-Za-z]:/*|[A-Za-z]:\\*) echo "$rel"; return ;;
    esac
    # Use python if available for absolute-path normalisation, else fall back.
    (cd "$base" && cd "$(dirname "$rel")" 2>/dev/null && echo "$(pwd)/$(basename "$rel")") \
        || echo "$base/$rel"
}

fleet_resolve_config_path() {
    local override=""
    if [[ "${1:-}" == "-c" || "${1:-}" == "--config" ]]; then
        override="${2:-}"
        shift 2 || true
    fi
    if [[ -n "$override" ]]; then
        [[ -f "$override" ]] || { echo "fleet config not found: $override" >&2; return 1; }
        (cd "$(dirname "$override")" && echo "$(pwd)/$(basename "$override")")
        return 0
    fi
    if [[ -n "${DASHCENTER_FLEET_CONFIG:-}" ]]; then
        [[ -f "$DASHCENTER_FLEET_CONFIG" ]] || {
            echo "DASHCENTER_FLEET_CONFIG points to a missing file: $DASHCENTER_FLEET_CONFIG" >&2
            return 1
        }
        (cd "$(dirname "$DASHCENTER_FLEET_CONFIG")" && echo "$(pwd)/$(basename "$DASHCENTER_FLEET_CONFIG")")
        return 0
    fi
    for name in fleet.yaml fleet.yml fleet.json; do
        if [[ -f "$_TEST_SETUP_ROOT/$name" ]]; then
            echo "$_TEST_SETUP_ROOT/$name"
            return 0
        fi
    done
    if [[ -f "$_TEST_SETUP_ROOT/fleet.example.yaml" ]]; then
        echo "WARN: no fleet.{yaml,json} found; using fleet.example.yaml" >&2
        echo "$_TEST_SETUP_ROOT/fleet.example.yaml"
        return 0
    fi
    echo "fleet config: nothing to load in $_TEST_SETUP_ROOT" >&2
    return 1
}

fleet_load() {
    local path="$1"
    FLEET_SOURCE="$path"
    FLEET_BASE_DIR="$(cd "$(dirname "$path")" && pwd)"
    local ext="${path##*.}"
    ext="${ext,,}"
    case "$ext" in
        json)
            command -v jq >/dev/null 2>&1 || { echo "fleet_load: jq is required" >&2; return 1; }
            FLEET_JSON="$(jq -c '.' "$path")"
            ;;
        yaml|yml)
            command -v yq >/dev/null 2>&1 || {
                echo "fleet_load: yq is required for YAML configs." >&2
                echo "Install: 'winget install MikeFarah.yq' (Windows) or 'brew install yq' / 'snap install yq' (Linux/macOS)." >&2
                echo "Alternatively, use a JSON fleet config." >&2
                return 1
            }
            command -v jq >/dev/null 2>&1 || { echo "fleet_load: jq is required" >&2; return 1; }
            FLEET_JSON="$(yq -o=json '.' "$path" | jq -c '.')"
            ;;
        *)
            echo "fleet_load: unsupported extension '.$ext' (use .yaml, .yml, or .json)" >&2
            return 1
            ;;
    esac
}

fleet_get() {
    # $1 = jq expression. Outputs raw result.
    echo "$FLEET_JSON" | jq -r "$1"
}

fleet_dpu_count() {
    echo "$FLEET_JSON" | jq '.dpus | length'
}

fleet_dpu() {
    # $1 = index, $2 = jq expression relative to that DPU.
    local i="$1" expr="$2"
    echo "$FLEET_JSON" | jq -r ".dpus[$i] | $expr"
}

fleet_validate() {
    local allow_priv=0
    if [[ "${1:-}" == "--allow-privileged" ]]; then allow_priv=1; fi

    local errors=()

    local api kind dpu_count
    api="$(fleet_get '.apiVersion // ""')"
    kind="$(fleet_get '.kind // ""')"
    dpu_count="$(fleet_dpu_count)"

    [[ "$api"  == "dashcenter.io/test-setup/v1" ]] || errors+=("apiVersion: expected 'dashcenter.io/test-setup/v1', got '$api'")
    [[ "$kind" == "FleetConfig"                ]] || errors+=("kind: expected 'FleetConfig', got '$kind'")
    [[ "$dpu_count" -gt 0                      ]] || errors+=("dpus: must contain at least one entry")

    # Unique device IDs.
    local dups
    dups="$(fleet_get '[.dpus[].deviceId] | (length) - (unique | length)')"
    [[ "$dups" -eq 0 ]] || errors+=("dpus: duplicate deviceId values present")

    # Collect all host-side ports (DPU grpc/admin + adapter grpc + redis hostPort if container).
    local ports
    ports="$(fleet_get '
        ([.dpus[] | .grpcPort, .adminPort] +
         (if .adapter and .adapter.enabled then [.adapter.grpcPort] else [] end) +
         (if .adapter and .adapter.enabled and .adapter.redis.mode=="container"
             then [.adapter.redis.hostPort] else [] end))
        | .[]
    ')"
    local dup_ports
    dup_ports="$(echo "$ports" | sort | uniq -d)"
    [[ -z "$dup_ports" ]] || errors+=("duplicate port assignment(s): $(echo "$dup_ports" | tr '\n' ' ')")

    if [[ "$allow_priv" -eq 0 ]]; then
        while IFS= read -r p; do
            [[ -z "$p" || "$p" == "null" ]] && continue
            if [[ "$p" -lt 1024 ]]; then
                errors+=("port $p: privileged (<1024); pass --allow-privileged to permit")
            fi
            if [[ "$p" -gt 65535 ]]; then
                errors+=("port $p: out of range (>65535)")
            fi
        done <<<"$ports"
    fi

    # Adapter / redis mode consistency.
    local adapter_enabled redis_mode redis_addr redis_port
    adapter_enabled="$(fleet_get '.adapter.enabled // false')"
    if [[ "$adapter_enabled" == "true" ]]; then
        redis_mode="$(fleet_get '.adapter.redis.mode // ""')"
        redis_addr="$(fleet_get '.adapter.redis.address // ""')"
        redis_port="$(fleet_get '.adapter.redis.hostPort // 0')"
        case "$redis_mode" in
            embedded)  ;;
            external)
                [[ -n "$redis_addr" ]] || errors+=("adapter.redis.address: required when mode=external")
                ;;
            container)
                [[ "$redis_port" -gt 0 ]] || errors+=("adapter.redis.hostPort: required when mode=container")
                ;;
            *)
                errors+=("adapter.redis.mode: expected embedded|external|container, got '$redis_mode'")
                ;;
        esac
    fi

    # Scenario files exist.
    local default_scenario
    default_scenario="$(fleet_get '.defaults.scenario // ""')"
    for ((i=0; i<dpu_count; i++)); do
        local s
        s="$(fleet_dpu "$i" '.scenario // ""')"
        [[ -z "$s" ]] && s="$default_scenario"
        [[ -z "$s" ]] && continue
        local full
        full="$(fleet_resolve_path "$FLEET_BASE_DIR" "$s")"
        [[ -f "$full" ]] || errors+=("dpus[$i].scenario: file not found (resolved to '$full')")
    done

    if (( ${#errors[@]} > 0 )); then
        echo "fleet config invalid:" >&2
        printf '  - %s\n' "${errors[@]}" >&2
        return 1
    fi
}
