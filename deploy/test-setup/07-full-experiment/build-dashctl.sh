#!/usr/bin/env bash
# 07-full-experiment/build-dashctl.sh — build the host dashctl binary.
set -euo pipefail
cd "$(dirname "$0")/../../.."

echo "==> Building dashctl"
cd src/impl-go/dashctl
go build -o bin/dashctl ./cmd/dashctl
echo "    Built: $(pwd)/bin/dashctl"
echo ""
echo "Usage:"
echo "  export DASHCTL_ENDPOINT=http://localhost:28443"
echo "  export DASHCTL_ADMIN_ENDPOINT=http://localhost:27443"
echo "  ./src/impl-go/dashctl/bin/dashctl version --insecure"
