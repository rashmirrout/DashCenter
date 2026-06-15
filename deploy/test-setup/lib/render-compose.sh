#!/usr/bin/env bash
# Render deploy/test-setup/03-multi-docker-fleet/docker-compose.fleet.yaml
# + .env from the active fleet.{yaml,json}.
#
# Usage:
#   ./lib/render-compose.sh                       # auto-resolve config
#   ./lib/render-compose.sh -c path/to/fleet.json
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$here/fleet_config.sh"

config=""
if [[ "${1:-}" == "-c" || "${1:-}" == "--config" ]]; then
    config="${2:-}"
    shift 2 || true
fi

cfg_path="$(fleet_resolve_config_path ${config:+-c "$config"})"
fleet_load "$cfg_path"
fleet_validate

setup_root="$_TEST_SETUP_ROOT"
repo_root="$(fleet_repo_root)"
out="$setup_root/03-multi-docker-fleet/docker-compose.fleet.yaml"
env_file="$setup_root/03-multi-docker-fleet/.env"
compose_dir="$setup_root/03-multi-docker-fleet"
mkdir -p "$compose_dir"

# POSIX relpath using python3 if available, else realpath.
posix_rel() {
    local from="$1" to="$2"
    if command -v python3 >/dev/null 2>&1; then
        python3 -c "import os,sys; print(os.path.relpath(sys.argv[2], sys.argv[1]).replace(os.sep,'/'))" "$from" "$to"
    else
        # Fallback: realpath --relative-to (GNU coreutils).
        realpath --relative-to="$from" "$to" 2>/dev/null | tr '\\' '/'
    fi
}

build_context="$(posix_rel "$compose_dir" "$repo_root")"
scenarios_mount="$(posix_rel "$compose_dir" "$setup_root/scenarios")"

bind_host="$(fleet_get '.defaults.bindHost // "127.0.0.1"')"
network="$(fleet_get '.defaults.network // "dashcenter-fleet"')"
image_tag="$(fleet_get '.defaults.imageTag // "dev"')"
default_scenario="$(fleet_get '.defaults.scenario // ""')"

dpu_count="$(fleet_dpu_count)"
adapter_enabled="$(fleet_get '.adapter.enabled // false')"
redis_mode="$(fleet_get '.adapter.redis.mode // ""')"
redis_addr="$(fleet_get '.adapter.redis.address // ""')"
redis_host_port="$(fleet_get '.adapter.redis.hostPort // 0')"
adapter_port="$(fleet_get '.adapter.grpcPort // 52051')"

{
    echo "# AUTO-GENERATED from $cfg_path — DO NOT EDIT."
    echo "# Re-run: ../lib/render-compose.sh   (or ../lib/render-compose.ps1)"
    echo "# Source of truth: deploy/test-setup/fleet.{yaml,json}"
    echo ""
    echo "services:"

    if [[ "$adapter_enabled" == "true" && "$redis_mode" == "container" ]]; then
        echo "  redis:"
        echo "    image: redis:7-alpine"
        echo "    container_name: dc-redis-fleet"
        echo "    ports: [\"${bind_host}:${redis_host_port}:6379\"]"
        echo "    restart: unless-stopped"
        echo "    networks: [\"$network\"]"
        echo ""
    fi

    for ((i=0; i<dpu_count; i++)); do
        device_id="$(fleet_dpu "$i" '.deviceId')"
        grpc="$(fleet_dpu "$i" '.grpcPort')"
        admin="$(fleet_dpu "$i" '.adminPort')"
        tag="$(fleet_dpu "$i" '.imageTag // ""')"
        [[ -z "$tag" || "$tag" == "null" ]] && tag="$image_tag"
        scenario="$(fleet_dpu "$i" '.scenario // ""')"
        [[ -z "$scenario" || "$scenario" == "null" ]] && scenario="$default_scenario"
        scenario_base=""
        [[ -n "$scenario" ]] && scenario_base="$(basename "$scenario")"

        svc="dash-sim-$device_id"
        container="dc-$svc"

        echo "  $svc:"
        echo "    build:"
        echo "      context: $build_context"
        echo "      dockerfile: src/impl-go/dash-sim/Dockerfile"
        echo "    image: dashcenter/dash-sim:$tag"
        echo "    container_name: $container"
        echo "    restart: unless-stopped"
        echo "    networks: [\"$network\"]"
        echo "    volumes:"
        echo "      - $scenarios_mount:/scenarios:ro"
        echo "    command:"
        echo "      - --grpc-listen=:50051"
        echo "      - --admin-listen=:8080"
        echo "      - --device-id=$device_id"
        if [[ -n "$scenario_base" ]]; then
            echo "      - --scenario=/scenarios/$scenario_base"
        fi
        extra_count="$(fleet_dpu "$i" '.extraArgs // [] | length')"
        for ((j=0; j<extra_count; j++)); do
            arg="$(fleet_dpu "$i" ".extraArgs[$j]")"
            echo "      - $arg"
        done
        echo "    ports:"
        echo "      - \"${bind_host}:${grpc}:50051\""
        echo "      - \"${bind_host}:${admin}:8080\""
        echo ""
    done

    if [[ "$adapter_enabled" == "true" ]]; then
        echo "  dash-redis-adapter:"
        echo "    build:"
        echo "      context: $build_context"
        echo "      dockerfile: src/impl-go/dash-redis-adapter/Dockerfile"
        echo "    image: dashcenter/dash-redis-adapter:$image_tag"
        echo "    container_name: dc-dash-redis-adapter"
        echo "    restart: unless-stopped"
        echo "    networks: [\"$network\"]"
        echo "    command:"
        echo "      - --grpc-listen=:52051"
        case "$redis_mode" in
            embedded)
                echo "      - --embedded-redis"
                ;;
            external)
                echo "      - --redis=$redis_addr"
                ;;
            container)
                echo "      - --redis=redis:6379"
                echo "    depends_on: [\"redis\"]"
                ;;
        esac
        echo "    ports:"
        echo "      - \"${bind_host}:${adapter_port}:52051\""
        echo ""
    fi

    echo "  cli:"
    echo "    build:"
    echo "      context: $build_context"
    echo "      dockerfile: src/impl-go/dash-sim-client/Dockerfile"
    echo "    image: dashcenter/dash-sim-client:$image_tag"
    echo "    profiles: [\"cli\"]"
    echo "    networks: [\"$network\"]"
    echo "    entrypoint: [\"/usr/local/bin/dash-sim-client\"]"
    echo ""
    echo "networks:"
    echo "  $network:"
    echo "    name: $network"
} > "$out"

{
    echo "# AUTO-GENERATED from $cfg_path — DO NOT EDIT."
    echo "DC_FLEET_CONFIG=$cfg_path"
    echo "DC_IMAGE_TAG=$image_tag"
    echo "DC_BIND_HOST=$bind_host"
    echo "DC_NETWORK=$network"
} > "$env_file"

echo "wrote $out"
echo "wrote $env_file"
echo ""
echo "Next: docker compose -f \"$out\" up -d --build"
