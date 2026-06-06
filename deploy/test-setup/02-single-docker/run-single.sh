#!/usr/bin/env bash
# Run a single dash-sim container plus (optionally) a dash-redis-adapter
# container, picking the port plan from the active fleet config.
#
# Usage:
#   ./run-single.sh                              # first DPU in fleet config
#   ./run-single.sh -d dpu-sim-02                # pick a specific DPU
#   ./run-single.sh -c ../my-fleet.json          # custom config
#   ./run-single.sh --no-adapter                 # skip dash-redis-adapter
#   ./run-single.sh stop                         # tear down
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$here/../lib/fleet_config.sh"

config=""
device_id=""
no_adapter=0
allow_priv_flag=""
action="start"

while (($#)); do
    case "$1" in
        stop) action="stop"; shift ;;
        -c|--config) config="$2"; shift 2 ;;
        -d|--device-id) device_id="$2"; shift 2 ;;
        --no-adapter) no_adapter=1; shift ;;
        --allow-privileged) allow_priv_flag="--allow-privileged"; shift ;;
        -h|--help) sed -n '1,12p' "$0"; exit 0 ;;
        *) echo "unknown arg: $1" >&2; exit 1 ;;
    esac
done

cfg_path="$(fleet_resolve_config_path ${config:+-c "$config"})"
fleet_load "$cfg_path"
fleet_validate $allow_priv_flag

network="$(fleet_get '.defaults.network // "dashcenter-fleet"')-single"
tag="$(fleet_get '.defaults.imageTag // "dev"')"
bind_host="$(fleet_get '.defaults.bindHost // "127.0.0.1"')"
adapter_enabled="$(fleet_get '.adapter.enabled // false')"

sim_name="dc-single-dash-sim"
adapter_name="dc-single-dash-redis-adapter"

if [[ "$action" == "stop" ]]; then
    echo "==> Stopping single-DPU topology"
    docker rm -f "$sim_name"     >/dev/null 2>&1 || true
    docker rm -f "$adapter_name" >/dev/null 2>&1 || true
    docker network rm "$network" >/dev/null 2>&1 || true
    echo "Done."
    exit 0
fi

# Pick DPU.
dpu_count="$(fleet_dpu_count)"
idx=-1
if [[ -n "$device_id" ]]; then
    for ((i=0; i<dpu_count; i++)); do
        if [[ "$(fleet_dpu "$i" '.deviceId')" == "$device_id" ]]; then idx=$i; break; fi
    done
    if [[ "$idx" -lt 0 ]]; then echo "deviceId '$device_id' not found in $cfg_path" >&2; exit 1; fi
else
    idx=0
    device_id="$(fleet_dpu 0 '.deviceId')"
    echo "==> No -d specified; using $device_id"
fi
echo "==> Fleet config: $cfg_path"

grpc="$(fleet_dpu "$idx" '.grpcPort')"
admin="$(fleet_dpu "$idx" '.adminPort')"
dpu_tag="$(fleet_dpu "$idx" '.imageTag // ""')"
[[ -z "$dpu_tag" || "$dpu_tag" == "null" ]] && dpu_tag="$tag"
scenario="$(fleet_dpu "$idx" '.scenario // ""')"
[[ -z "$scenario" || "$scenario" == "null" ]] && scenario="$(fleet_get '.defaults.scenario // ""')"
scenario_base=""
[[ -n "$scenario" ]] && scenario_base="$(basename "$scenario")"

img_sim="dashcenter/dash-sim:$dpu_tag"
img_adapter="dashcenter/dash-redis-adapter:$tag"
img_cli="dashcenter/dash-sim-client:$tag"

missing=()
required=("$img_sim" "$img_cli")
if [[ "$adapter_enabled" == "true" && "$no_adapter" -eq 0 ]]; then required+=("$img_adapter"); fi
for img in "${required[@]}"; do
    docker image inspect "$img" >/dev/null 2>&1 || missing+=("$img")
done
if (( ${#missing[@]} > 0 )); then
    echo "ERROR: missing image(s): ${missing[*]}" >&2
    echo "Run: ./build-images.sh" >&2
    exit 1
fi

if ! docker network inspect "$network" >/dev/null 2>&1; then
    echo "==> Creating network $network"
    docker network create "$network" >/dev/null
fi

docker rm -f "$sim_name"     >/dev/null 2>&1 || true
docker rm -f "$adapter_name" >/dev/null 2>&1 || true

# Mount the shared scenarios folder.
setup_root="$(cd "$here/.." && pwd)"
scen_mount="$setup_root/scenarios"

sim_args=(
    run -d --name "$sim_name" --network "$network"
    -p "${bind_host}:${grpc}:50051"
    -p "${bind_host}:${admin}:8080"
    -v "$scen_mount:/scenarios:ro"
    "$img_sim"
    --grpc-listen=:50051
    --admin-listen=:8080
    "--device-id=$device_id"
)
[[ -n "$scenario_base" ]] && sim_args+=("--scenario=/scenarios/$scenario_base")
extra_count="$(fleet_dpu "$idx" '.extraArgs // [] | length')"
for ((j=0; j<extra_count; j++)); do
    sim_args+=("$(fleet_dpu "$idx" ".extraArgs[$j]")")
done

echo "==> Starting $sim_name"
docker "${sim_args[@]}" >/dev/null

if [[ "$adapter_enabled" == "true" && "$no_adapter" -eq 0 ]]; then
    adapter_port="$(fleet_get '.adapter.grpcPort // 52051')"
    redis_mode="$(fleet_get '.adapter.redis.mode // "embedded"')"
    redis_addr="$(fleet_get '.adapter.redis.address // ""')"

    ad_args=(
        run -d --name "$adapter_name" --network "$network"
        -p "${bind_host}:${adapter_port}:52051"
        "$img_adapter"
        --grpc-listen=:52051
    )
    case "$redis_mode" in
        embedded) ad_args+=(--embedded-redis) ;;
        external) ad_args+=("--redis=$redis_addr") ;;
        container) echo "ERROR: adapter.redis.mode='container' is not supported in topology 02." >&2; exit 1 ;;
    esac

    echo "==> Starting $adapter_name"
    docker "${ad_args[@]}" >/dev/null
fi

sleep 1
docker ps --filter "name=dc-single-" --format "table {{.Names}}\t{{.Image}}\t{{.Ports}}\t{{.Status}}"

echo ""
echo "==> Smoke test (via CLI container):"
docker run --rm --network "$network" "$img_cli" --target "${sim_name}:50051" ping
if [[ "$adapter_enabled" == "true" && "$no_adapter" -eq 0 ]]; then
    docker run --rm --network "$network" "$img_cli" --target "${adapter_name}:52051" ping
fi

echo ""
echo "Drive from host: dash-sim-client --target ${bind_host}:${grpc} ping"
echo "Tear down:       ./run-single.sh stop"
