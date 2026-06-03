#!/usr/bin/env bash
# Vendors the sonic-net/sonic-dash-api .proto files into
# proto/vendor/sonic-dash-api/ at the commit pinned in
# proto/vendor/sonic-dash-api/VERSION.
set -euo pipefail

COMMIT="${1:-main}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
vendor_dir="$repo_root/proto/vendor/sonic-dash-api"
third_party="$repo_root/third_party/sonic-dash-api"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

git clone --depth 1 https://github.com/sonic-net/sonic-dash-api "$tmp"
if [[ "$COMMIT" != "main" ]]; then
  (cd "$tmp" && git fetch origin "$COMMIT" && git checkout "$COMMIT")
fi

mkdir -p "$vendor_dir" "$third_party"
cp -f "$tmp"/proto/*.proto "$vendor_dir/"
cp -f "$tmp/LICENSE" "$third_party/LICENSE"

resolved_commit="$(git -C "$tmp" rev-parse HEAD)"
cat >"$vendor_dir/VERSION" <<EOF
repo=https://github.com/sonic-net/sonic-dash-api
commit=$resolved_commit
date=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
EOF

echo "Vendored sonic-dash-api @ $resolved_commit into $vendor_dir"
