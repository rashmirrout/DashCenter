#!/usr/bin/env bash
# One-time developer setup for Linux/macOS.
set -euo pipefail

need() { command -v "$1" >/dev/null 2>&1; }

if ! need go;       then echo "Install Go from https://go.dev/dl/"; fi
if ! need buf;      then echo "Install buf:      https://buf.build/docs/installation"; fi
if ! need protoc;   then echo "Install protoc:   https://github.com/protocolbuffers/protobuf/releases"; fi
if ! need grpcurl;  then echo "Install grpcurl:  https://github.com/fullstorydev/grpcurl"; fi

echo "==> Versions:"
go version       2>/dev/null || true
buf --version    2>/dev/null || true
protoc --version 2>/dev/null || true
grpcurl -version 2>&1        || true
