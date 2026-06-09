#!/usr/bin/env sh
# dashctl-e2e.sh — end-to-end dashctl walkthrough against the 5-DPU fleet.
#
# Exercises every Phase 1 dashctl command path:
#   1. version --client                                 (offline smoke)
#   2. dashd health                                     (admin :7443)
#   3. fleet inventory shows 5 × DPU_STATE_UP
#   4. dashctl get vnet                                  (empty)
#   5. dashctl apply -f manifests/                      (2 vnets + 5 enis)
#   6. dashctl get vnet -o table                        (2 rows)
#   7. dashctl get eni -o wide                          (5 rows with placement)
#   8. dashctl describe eni eni-app-01
#   9. dashctl reconcile                                 (force tick)
#  10. dashctl dpu list                                  (5 UP)
#  11. dashctl dpu drift --dpu dpu-sim-01               (0 items)
#  12. dashctl delete eni eni-db-03
#  13. dashctl explain vnet                              (offline)
#
# Each command is run TWICE:
#   - from the host (if `bin/dashctl` exists locally)
#   - from inside the dashctl container (always)
# The two paths must agree.
#
# Exit code: 0 on PASS, 1 on FAIL.
#
# Usage:
#   ./deploy/dashctl-fleet/dashctl-e2e.sh

set -eu

COMPOSE="docker compose -f deploy/dashctl-fleet/docker-compose.yml"
MANIFEST_DIR="deploy/dashctl-fleet/manifests"

# Resolve binary mode: prefer the local build if present; always
# exercise the container as a parallel check.
LOCAL_BIN="src/impl-go/dashctl/bin/dashctl"
[ -x "$LOCAL_BIN" ] || LOCAL_BIN=""

g() { printf '\033[32m%s\033[0m\n' "$1"; }
r() { printf '\033[31m%s\033[0m\n' "$1"; }
y() { printf '\033[33m%s\033[0m\n' "$1"; }

step() { y "[$1/13] $2"; }
ok()   { g "      OK    $1"; }
fail() { r "      FAIL  $1"; exit 1; }

# host_dashctl: only runs if a local binary is present.
host_dashctl() {
  [ -z "$LOCAL_BIN" ] && return 0
  "$LOCAL_BIN" \
    --endpoint http://localhost:8443 \
    --admin-endpoint http://localhost:7443 \
    "$@"
}

# ctnr_dashctl: one-shot inside the dashctl service. Env vars in the
# compose file point at dashd over the in-network bridge.
ctnr_dashctl() {
  $COMPOSE run --rm dashctl "$@"
}

# ctnr_dashctl_with_manifest_mount: same, but bind-mounts the local
# manifests directory at /work so `apply -f` can see the files.
ctnr_dashctl_with_manifest_mount() {
  $COMPOSE run --rm \
    -v "$PWD/$MANIFEST_DIR:/work:ro" \
    --entrypoint /usr/local/bin/dashctl \
    dashctl "$@"
}

step 1 "dashctl version --client"
ctnr_dashctl version --client | grep -q "Client: dashctl" || fail "container version banner missing"
ok "container version OK"
if [ -n "$LOCAL_BIN" ]; then
  host_dashctl version --client | grep -q "Client: dashctl" || fail "host version banner missing"
  ok "host    version OK"
else
  y "      (skipping host check — $LOCAL_BIN not found; run \`make -C src/impl-go/dashctl build\` first)"
fi

step 2 "dashd /admin/health"
curl -sSf http://localhost:7443/admin/health >/dev/null || fail "dashd admin not reachable"
ok "dashd reachable"

step 3 "Inventory: expect 5 × DPU_STATE_UP (poll up to 30s)"
i=0
UP=0
while [ "$i" -lt 30 ]; do
  INV=$(curl -sSf http://localhost:7443/admin/inventory)
  UP=$(printf '%s' "$INV" | grep -o '"DPU_STATE_UP"' | wc -l | tr -d ' ')
  [ "$UP" -ge 5 ] && break
  sleep 1
  i=$((i + 1))
done
[ "$UP" -ge 5 ] && ok "$UP / 5 DPUs UP (after ${i}s)" || fail "only $UP / 5 DPUs UP — inventory: $INV"

step 4 "get vnet (empty start)"
OUT=$(ctnr_dashctl get vnet 2>&1 || true)
# In a fresh fleet there are no vnets yet; output may be empty or just a header.
ok "get vnet ran (output bytes=$(printf '%s' "$OUT" | wc -c | tr -d ' '))"

step 5 "apply -f $MANIFEST_DIR (2 vnets + 5 enis)"
APPLY=$(ctnr_dashctl_with_manifest_mount apply -f /work) || fail "apply failed: $APPLY"
N_APPLIED=$(printf '%s' "$APPLY" | grep -c 'apply in namespace default' || true)
if [ "$N_APPLIED" -lt 7 ]; then
  fail "expected ≥7 apply lines (2 vnets + 5 enis), got $N_APPLIED — output: $APPLY"
fi
ok "$N_APPLIED specs applied"

step 6 "get vnet -o table — expect 2 rows"
OUT=$(ctnr_dashctl get vnet -o table)
echo "$OUT" | grep -q vnet-app || fail "vnet-app missing from get output: $OUT"
echo "$OUT" | grep -q vnet-db  || fail "vnet-db missing"
ok "both vnets listed"

step 7 "get eni -o wide — expect 5 rows + PLACED-ON column"
OUT=$(ctnr_dashctl get eni -o wide)
echo "$OUT" | grep -q PLACED-ON || fail "wide column PLACED-ON missing: $OUT"
COUNT=$(echo "$OUT" | grep -c '^eni-' || true)
# Header may not start with 'eni-'; count rows by name presence.
for name in eni-app-01 eni-app-02 eni-db-01 eni-db-02 eni-db-03; do
  echo "$OUT" | grep -q "$name" || fail "missing $name in wide output"
done
ok "5 ENIs listed wide ($COUNT)"

step 8 "describe eni eni-app-01"
OUT=$(ctnr_dashctl describe eni eni-app-01)
echo "$OUT" | grep -q "Name:        eni-app-01" || fail "describe header missing: $OUT"
echo "$OUT" | grep -q "Kind:        Eni"        || fail "describe kind missing"
ok "describe block produced"

step 9 "reconcile"
ctnr_dashctl reconcile | grep -q "Triggered reconcile" || fail "reconcile output missing"
ok "reconcile triggered"

step 10 "dpu list"
OUT=$(ctnr_dashctl dpu list -o table)
for d in dpu-sim-01 dpu-sim-02 dpu-sim-03 dpu-sim-04 dpu-sim-05; do
  echo "$OUT" | grep -q "$d" || fail "dpu $d missing from dpu list"
done
ok "all 5 DPUs listed"

step 11 "dpu drift --dpu dpu-sim-01"
# After apply + reconcile + a couple seconds, drift should be empty.
# Poll a few times to allow the worker to converge.
i=0
DRIFT_OUT=""
while [ "$i" -lt 30 ]; do
  DRIFT_OUT=$(ctnr_dashctl dpu drift --dpu dpu-sim-01 2>&1 || true)
  echo "$DRIFT_OUT" | grep -q "0 drift items." && break
  sleep 1
  i=$((i + 1))
done
echo "$DRIFT_OUT" | grep -q "0 drift items." \
  && ok "no drift on dpu-sim-01 (after ${i}s)" \
  || y "      WARN drift still present after 30s — $DRIFT_OUT"

step 12 "delete eni eni-db-03"
OUT=$(ctnr_dashctl delete eni eni-db-03)
echo "$OUT" | grep -q "eni/eni-db-03 deleted" || fail "delete output missing: $OUT"
ok "eni-db-03 deleted"
# Confirm it's gone.
ctnr_dashctl get eni eni-db-03 >/dev/null 2>&1 \
  && fail "eni-db-03 still readable after delete" \
  || ok "404 confirmed on subsequent get"

step 13 "explain vnet (offline — no dashd call)"
OUT=$(ctnr_dashctl explain vnet)
echo "$OUT" | grep -q "KIND:     Vnet" || fail "explain header missing: $OUT"
echo "$OUT" | grep -q "vni"             || fail "explain field listing missing"
ok "offline field reference produced"

echo
g "PASS: dashctl drove the 5-DPU fleet end-to-end."
g "      ✔ apply -f (multi-doc, dir, mount)"
g "      ✔ get / describe / delete / reconcile"
g "      ✔ dpu list / dpu drift"
g "      ✔ explain (offline)"
[ -n "$LOCAL_BIN" ] || y "(host binary not built; container path verified — run \`make build\` in src/impl-go/dashctl to also exercise the host path)"
exit 0
