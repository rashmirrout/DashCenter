#!/usr/bin/env bash
# Launch a multi-DPU DashCenter fleet on the local host (native processes),
# driven by a fleet.{yaml,json} config file.
#
# Usage:
#   ./start-fleet.sh                                # auto-resolve config
#   ./start-fleet.sh -c ../my-fleet.json            # explicit config
#   ./start-fleet.sh --allow-privileged             # permit ports <1024
#   BIN_DIR=/path/to/bin ./start-fleet.sh           # override binary dir
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$here/../lib/fleet_config.sh"

config=""
allow_priv_flag=""
no_smoke=0

while (($#)); do
    case "$1" in
        -c|--config) config="$2"; shift 2 ;;
        --allow-privileged) allow_priv_flag="--allow-privileged"; shift ;;
        --no-smoke-test) no_smoke=1; shift ;;
        -h|--help) sed -n '1,12p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

cfg_path="$(fleet_resolve_config_path ${config:+-c "$config"})"
fleet_load "$cfg_path"
fleet_validate $allow_priv_flag
echo "==> Fleet config: $cfg_path"

# ---------- Binaries ----------
repo_root="$(fleet_repo_root)"
bin_dir="${BIN_DIR:-$repo_root/src/impl-go/bin}"
[[ -d "$bin_dir" ]] || { echo "ERROR: $bin_dir not found. Build first." >&2; exit 1; }

ext=""
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) ext=".exe" ;; esac
dash_sim="$bin_dir/dash-sim${ext}"
dash_adapter="$bin_dir/dash-redis-adapter${ext}"
dash_cli="$bin_dir/dash-sim-client${ext}"
for f in "$dash_sim" "$dash_adapter"; do
    [[ -x "$f" ]] || { echo "ERROR: missing binary $f" >&2; exit 1; }
done

logs_dir="$here/logs"
mkdir -p "$logs_dir"
state_path="$here/.fleet-state.json"
[[ -f "$state_path" ]] && echo "WARN: $state_path exists — run ./stop-fleet.sh first." >&2

start_one() {
    local name="$1" exe="$2" log="$3"; shift 3
    echo "==> Starting $name"
    echo "    $exe $*"
    echo "    log: $log"
    "$exe" "$@" >"$log" 2>"$log.err" &
    local pid=$!
    sleep 0.3
    if ! kill -0 "$pid" 2>/dev/null; then
        echo "ERROR: $name exited immediately" >&2
        cat "$log" "$log.err" >&2 || true
        exit 1
    fi
    echo "$pid"
}

bind_host="$(fleet_get '.defaults.bindHost // "127.0.0.1"')"
tick_interval="$(fleet_get '.defaults.tickInterval // "1s"')"
default_scenario="$(fleet_get '.defaults.scenario // ""')"

entries=()
grpcs=()

dpu_count="$(fleet_dpu_count)"
for ((i=0; i<dpu_count; i++)); do
    device_id="$(fleet_dpu "$i" '.deviceId')"
    grpc="$(fleet_dpu "$i" '.grpcPort')"
    admin="$(fleet_dpu "$i" '.adminPort')"
    scenario="$(fleet_dpu "$i" '.scenario // ""')"
    [[ -z "$scenario" || "$scenario" == "null" ]] && scenario="$default_scenario"
    log="$logs_dir/$device_id.log"

    args=(--grpc-listen ":$grpc" --admin-listen ":$admin" --device-id "$device_id" --tick-interval "$tick_interval")
    if [[ -n "$scenario" ]]; then
        scenario_full="$(fleet_resolve_path "$FLEET_BASE_DIR" "$scenario")"
        args+=(--scenario "$scenario_full")
    fi
    extra_count="$(fleet_dpu "$i" '.extraArgs // [] | length')"
    for ((j=0; j<extra_count; j++)); do
        args+=("$(fleet_dpu "$i" ".extraArgs[$j]")")
    done

    pid=$(start_one "dash-sim/$device_id" "$dash_sim" "$log" "${args[@]}")
    entries+=("{\"role\":\"dash-sim\",\"device_id\":\"$device_id\",\"pid\":$pid,\"grpc\":\"$bind_host:$grpc\",\"admin\":\"http://$bind_host:$admin\",\"log\":\"$log\"}")
    grpcs+=("$bind_host:$grpc")
done

adapter_enabled="$(fleet_get '.adapter.enabled // false')"
if [[ "$adapter_enabled" == "true" ]]; then
    adapter_port="$(fleet_get '.adapter.grpcPort // 52051')"
    redis_mode="$(fleet_get '.adapter.redis.mode // "embedded"')"
    redis_addr="$(fleet_get '.adapter.redis.address // ""')"

    adapter_args=(--grpc-listen ":$adapter_port")
    case "$redis_mode" in
        embedded) adapter_args+=(--embedded-redis) ;;
        external) adapter_args+=(--redis "$redis_addr") ;;
        container)
            echo "ERROR: adapter.redis.mode='container' is only supported in topology 03." >&2
            for e in "${entries[@]}"; do
                p="$(echo "$e" | grep -oE '"pid":[[:space:]]*[0-9]+' | grep -oE '[0-9]+')"
                kill "$p" 2>/dev/null || true
            done
            exit 1
            ;;
    esac

    log="$logs_dir/dash-redis-adapter.log"
    pid=$(start_one "dash-redis-adapter" "$dash_adapter" "$log" "${adapter_args[@]}")
    entries+=("{\"role\":\"dash-redis-adapter\",\"device_id\":\"redis-adapter\",\"pid\":$pid,\"grpc\":\"$bind_host:$adapter_port\",\"admin\":null,\"log\":\"$log\"}")
    grpcs+=("$bind_host:$adapter_port")
fi

started_at="$(date -u +%FT%TZ)"
printf '{\n  "started_at": "%s",\n  "config_path": "%s",\n  "bin_dir": "%s",\n  "components": [%s]\n}\n' \
    "$started_at" "$cfg_path" "$bin_dir" "$(IFS=,; echo "${entries[*]}")" > "$state_path"

echo ""
echo "==> Fleet up. State: $state_path"
echo ""

if [[ "$no_smoke" -eq 0 && -x "$dash_cli" ]]; then
    echo "Smoke test:"
    for g in "${grpcs[@]}"; do
        printf "  %-25s ... " "$g"
        if "$dash_cli" --target "$g" ping >/dev/null 2>&1; then
            echo "OK"
        else
            echo "FAIL"
        fi
    done
fi

echo ""
echo "Stop with: ./stop-fleet.sh"
