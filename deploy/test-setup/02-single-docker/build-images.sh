#!/usr/bin/env bash
# Build the three DashCenter container images from the repo root.
#
# TAG=dev ./build-images.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../../.." && pwd)"
tag="${TAG:-dev}"

build_image() {
  local image="$1" dockerfile="$2"
  echo "==> docker build -t $image -f $dockerfile $repo_root"
  docker build -t "$image" -f "$dockerfile" "$repo_root"
}

build_image "dashcenter/dash-sim:$tag"           "$repo_root/src/impl-go/dash-sim/Dockerfile"
build_image "dashcenter/dash-redis-adapter:$tag" "$repo_root/src/impl-go/dash-redis-adapter/Dockerfile"
build_image "dashcenter/dash-sim-client:$tag"    "$repo_root/src/impl-go/dash-sim-client/Dockerfile"

echo ""
echo "==> Done. Images:"
docker images --filter "reference=dashcenter/*:$tag"
