#!/usr/bin/env bash
# DashCenter install-check (Linux / Bash)
#
# Verifies that the toolchain required to build DashCenter is installed and
# correctly on PATH. Exits 0 on success, 1 on failure.
#
# Usage:  bash docs/tutorial/scripts/install-check.sh

set -u
FAILED=0
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
DGRAY='\033[1;30m'
NC='\033[0m'

check() {
  local label="$1"; shift
  local required="$1"; shift
  if out=$("$@" 2>&1); then
    printf "${GREEN}[OK]${NC}   %-22s %s\n" "$label" "$(echo "$out" | head -n 1)"
  else
    if [ "$required" = "yes" ]; then
      printf "${RED}[FAIL]${NC} %-22s %s\n" "$label" "command failed: $*"
      FAILED=$((FAILED+1))
    else
      printf "${YELLOW}[INFO]${NC} %-22s not installed (optional)\n" "$label"
    fi
  fi
}

echo -e "${CYAN}DashCenter toolchain check (Linux)${NC}"
echo -e "${DGRAY}PATH = $PATH${NC}"
echo

check "go"                  yes go version
check "protoc"              yes protoc --version
check "protoc-gen-go"       yes protoc-gen-go --version
check "protoc-gen-go-grpc"  yes protoc-gen-go-grpc --version
check "git"                 yes git --version
check "bash"                yes bash --version

# PATH sanity
GOBIN_DIR="${GOBIN:-$HOME/go/bin}"
if echo ":$PATH:" | grep -q ":$GOBIN_DIR:"; then
  printf "${GREEN}[OK]${NC}   %-22s %s\n" "PATH includes GOBIN" "$GOBIN_DIR"
else
  printf "${RED}[FAIL]${NC} %-22s %s missing from PATH\n" "PATH includes GOBIN" "$GOBIN_DIR"
  FAILED=$((FAILED+1))
fi

# Optional tools
check "docker (optional)"   no docker --version
check "rustc (optional)"    no rustc --version
check "cargo (optional)"    no cargo --version

echo
if [ "$FAILED" -eq 0 ]; then
  echo -e "${GREEN}=== All required checks passed ===${NC}"
  exit 0
else
  echo -e "${RED}=== $FAILED required check(s) failed ===${NC}"
  echo -e "${YELLOW}See docs/tutorial/03-build-setup.md for installation steps.${NC}"
  exit 1
fi
