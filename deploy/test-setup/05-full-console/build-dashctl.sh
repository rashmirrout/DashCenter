#!/usr/bin/env bash
# build-dashctl.sh — compile dashctl from source and place it in this directory.
#
# Requires Go 1.22+ on PATH. Automatically called by start-fleet.sh
# unless --skip-dashctl-build is passed.
#
# Usage:
#   ./build-dashctl.sh              # build and place ./dashctl
#   ./build-dashctl.sh --check      # exit 0 if already built, 1 if missing

set -euo pipefail
cd "$(dirname "$0")"

REPO_ROOT="$(cd ../../.. && pwd)"
SRC_DIR="$REPO_ROOT/src/impl-go"
OUTPUT="$(pwd)/dashctl"

if [[ "${1:-}" == "--check" ]]; then
    [[ -x "$OUTPUT" ]] && exit 0 || exit 1
fi

# Check if already built and up-to-date (skip rebuild if binary exists
# and is newer than go.mod — good enough heuristic for local dev).
if [[ -x "$OUTPUT" ]]; then
    if [[ "$OUTPUT" -nt "$SRC_DIR/dashctl/go.mod" ]]; then
        echo "==> dashctl already built at $OUTPUT (up-to-date)"
        exit 0
    fi
fi

if ! command -v go >/dev/null 2>&1; then
    echo "   (Go not found on PATH — skipping dashctl build)"
    echo "   Install Go 1.22+: https://go.dev/dl/"
    echo "   Or use the bootstrap.py fallback: python3 manifest/bootstrap.py"
    exit 0
fi

GO_VERSION=$(go version | grep -oP 'go1\.(\d+)' | head -1)
echo "==> Building dashctl (Go: $GO_VERSION)"

cd "$SRC_DIR"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUTPUT" ./dashctl/cmd/dashctl

echo "==> dashctl built at $OUTPUT"
"$OUTPUT" version 2>/dev/null || true
