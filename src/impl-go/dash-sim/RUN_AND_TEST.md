# DashCenter — Run + Test Guide (Phases 1, 2, 3)

This guide covers all three DashCenter binaries:

| Binary | Role | Storage |
|---|---|---|
| `dash-sim` | Behavioural DASH-DPU simulator (full packet pipeline) | in-memory |
| `dash-redis-adapter` | Same `dashapi.v1.DashApi` over SONiC APP_DB | Redis (real or embedded miniredis) |
| `dash-sim-client` | Operator CLI — works against either backend unchanged | n/a |

All three speak the same wire contract: `dashapi.v1.DashApi` over upstream
`sonic-net/sonic-dash-api` proto types, vendored under
`proto/vendor/sonic-dash-api/` (pinned commit in `proto/vendor/sonic-dash-api/VERSION`).

---

## 1. Toolchain (Windows, per session)

```powershell
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"
$env:GOBIN="$env:USERPROFILE\go\bin"
```

## 2. Regenerate protos (only when upstream changes)

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter
pwsh -File .\scripts\vendor-protos.ps1
pwsh -File .\scripts\codegen-go.ps1
```

## 3. Build all three binaries

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null
go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter
```

## 4. Test suite

```powershell
# Everything (unit + integration) for both server modules + CLI:
go test .\dash-sim\... .\dash-sim-client\... .\dash-redis-adapter\...
```

Pipeline conformance (`dash-sim/test/integration/pipeline_test.go`): 8 test
cases covering outbound ENCAP, route DIRECT, route DROP, ACL deny,
disabled-ENI drop, missing-route drop, inbound deliver, inbound mac
lookup, inbound no-rule drop.

Redis adapter conformance: 5 test cases covering Apply/Get/Delete/Update
round-trip, ordered List, Subscribe snapshot+live, ENI bytes
round-trip, SimulatePacket-Unimplemented.

### 4a. PE-3a (typed per-DPU counter rollups) — gate PE-G8

```powershell
# Unit tests for the new rollup engine + handler (100% line coverage on new code):
go test .\dash-sim\internal\sim\counters\... .\dash-sim\internal\sim\server\... -count=1 -v

# End-to-end gRPC tests (in-process server + real client, no network):
go test .\dash-sim\test\integration\... -run 'EndToEnd_.*' -count=1 -v

# Client-side: CLI subcommand + render formatters + GetDpuCounters wrapper:
go test .\dash-sim-client\pkg\client\... `
        .\dash-sim-client\internal\render\... `
        .\dash-sim-client\internal\cmd\... -count=1 -v
```

What lights up:

| Package | New tests | Covers |
|---|---|---|
| `dash-sim/internal/sim/counters` | `TestBucket_Add`, `TestRegistry_SnapshotBucket`, `TestRegistry_TotalBucket`, `TestRegistry_Rollup_*`, `TestRegistry_RollupAll_*` | `Bucket.Add`, `Registry.SnapshotBucket/TotalBucket/Rollup/RollupAll` + scope-prefix rule |
| `dash-sim/internal/sim/server` | `TestGetDpuCounters_*` (default, include-enis, include-vnets, both, unknown-scope filter, fault-inject) | `GetDpuCounters` handler + `scopesForKind` + `bucketToProto` |
| `dash-sim/test/integration` | `EndToEnd_DefaultRequest`, `EndToEnd_IncludeEnisAndVnets`, `EndToEnd_FilterFlagsPropagate`, `EndToEnd_LegacyGetCountersStillWorks` | Full RPC round-trip + legacy back-compat |
| `dash-sim-client/pkg/client` | `TestClient_GetDpuCounters_*` | Wrapper, nil-req normalization |
| `dash-sim-client/internal/render` | `TestDpuCounters_Table/Json/Yaml/Csv`, `TestParseFormatExt_*` | Four output formats + CSV header stability |
| `dash-sim-client/internal/cmd` | `TestDpuCountersCmd_PreRunE_*`, `TestOneWatchTick_*` | Flag validation + single watch tick |

Optional coverage gate (run from `src/impl-go/`):

```powershell
go test .\dash-sim\internal\sim\counters\... .\dash-sim-client\internal\render\... `
        -coverprofile cov.out
go tool cover -func=cov.out | Select-String 'total:'
# Expected: total: (statements) 100.0%
Remove-Item cov.out
```

> **Tip — avoid the `$` / `cov` literal-file trap.** Do *not* write
> `-coverprofile=$(Resolve-Path .)\cov.out` on the PowerShell command
> line; the subexpression sometimes fails to expand and produces
> literal files named `$` and `cov` in the package directory. Either
> stay in the package dir and use the bare `cov.out` form above, or
> pre-compute the path into a variable. The repo `.gitignore` now
> shields against the typo, but the files are still unwanted.

---

## 5. Run dash-sim (in-memory backend, with behavioural pipeline)

```powershell
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8080 --device-id dpu-sim-01
```

Optional preload:

```powershell
.\bin\dash-sim.exe --scenario .\dash-sim\testdata\scenarios\small.yaml
```

### 5a. Docker: in-container diagnostics

The dash-sim Docker image ships both `dash-sim` and `dash-sim-client`
(Alpine-based runtime with shell). Operators can exec into any running
sim container for direct diagnostics:

```bash
# Enter the container:
docker exec -it dc-console-sim-01 sh

# All dash-sim-client commands work against localhost:
dash-sim-client ping --target localhost:50051
dash-sim-client dpu-counters --target localhost:50051 -o table
dash-sim-client dpu-counters --include-enis --target localhost:50051
dash-sim-client reset-counters --target localhost:50051
dash-sim-client kinds --target localhost:50051 -o table

# One-liner (no shell entry):
docker exec dc-console-sim-01 dash-sim-client reset-counters --target localhost:50051
```

## 6. Run dash-redis-adapter (Redis APP_DB backend)

Self-contained demo (no Redis required — uses embedded miniredis):

```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --embedded-redis
```

Against real Redis:

```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --redis localhost:6379 --redis-db 0
```

Wire format (matches SONiC orchagent expectations):

```
Key:   "DASH_<KIND>_TABLE:<joined-key>"   e.g. "DASH_VNET_TABLE:vnet-prod"
Value: HASH { pb: <binary protobuf>, meta: <json {created_ts_ns, updated_ts_ns}> }
```

Subscribe uses Redis Pub/Sub on channel `dashapi.events` (no keyspace
notifications needed).

`SimulatePacket` returns `code = Unimplemented` against the Redis backend —
Redis APP_DB has no behavioural pipeline. Use `dash-sim` for packet
simulations.

## 7. CLI works against either backend

The same `dash-sim-client.exe` drives both. Switch with `--target`.

```powershell
$c = ".\bin\dash-sim-client.exe"

# Against dash-sim:
& $c --target localhost:50051 kinds -o table
& $c --target localhost:50051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'

# Against dash-redis-adapter:
& $c --target localhost:52051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:52051 list  --kind vnet -o table
```

## 8. Pipeline (Phase 2) — SimulatePacket

Only `dash-sim` implements this. Walks
**direction → ENI → ACL_OUT/IN stages 1..5 → route LPM / route_rule →
vnet_mapping encap / service_tunnel / appliance → counter ticks**.

```powershell
# Outbound encap (after applying the prerequisite vnet_mapping):
.\bin\dash-sim-client.exe --target localhost:50051 simulate `
  --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 80 --trace

# Inbound deliver (route_rule lookup + ACL_IN):
.\bin\dash-sim-client.exe --target localhost:50051 simulate `
  --direction inbound --eni eni-001 --vni 1001 `
  --src-ip 100.64.0.5 --dst-ip 10.0.0.4 --trace
```

The Decision returned includes `action`, `reason`, `out_eni`,
`out_underlay_ip`, `out_vni`, `out_routing_type`, `matched_acl_stage`,
`matched_acl_priority`, `matched_route_prefix`, and (with `--trace`) a
per-step pipeline trace.

## 9. Admin HTTP (dash-sim only)

```powershell
Invoke-RestMethod http://localhost:8080/admin/health     | ConvertTo-Json -Depth 5
Invoke-RestMethod http://localhost:8080/admin/kinds      | ConvertTo-Json -Depth 3
Invoke-RestMethod http://localhost:8080/admin/dump       | ConvertTo-Json -Depth 6

# Inject a one-shot Apply failure
$body = @{ op = "Apply"; mode = "error"; count = 1; message = "injected" } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/faults -Body $body -ContentType application/json

# Preload a scenario at runtime
$body = @{ path = "...\testdata\scenarios\small.yaml"; reset = $true } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/scenario -Body $body -ContentType application/json
```

## 9a. PE-3a — typed per-DPU counter rollups

**RPC**: `dashapi.v1.DashApi.GetDpuCounters`. **CLI**: `dash-sim-client dpu-counters`.
Adds a typed rollup at three nested scopes (DPU-wide, per-ENI, per-VNET)
in a single round-trip — no more N+M `GetCounters` calls to assemble a
DPU snapshot. Legacy `GetCounters` is preserved for back-compat.

Build + test gate for this feature lives in
[§4a above](#4a-pe-3a-typed-per-dpu-counter-rollups--gate-pe-g8).
Full design + Future Scopes:
[`docs/dashd-features/dash-sim-counter-rollups.md`](../../../docs/dashd-features/dash-sim-counter-rollups.md).

### Quick lab (4 experiments)

> **Pre-req**: bring up `dash-sim` with the small scenario:
>
> ```powershell
> .\bin\dash-sim.exe --device-id dpu-sim-01 `
>   --scenario .\dash-sim\testdata\scenarios\small.yaml
> ```

#### One-shot snapshot

```powershell
.\bin\dash-sim-client.exe dpu-counters -o table
```

Expected — DPU-wide bucket only (per-ENI/per-VNET are opt-in):

```text
DEVICE  dpu-sim-01
TIME    2026-06-14T20:14:33Z (ns=1718388873000000000)

DPU TOTALS
SCOPE  PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
dpu    1247        2486         87104     174208     12
```

#### Include per-ENI + per-VNET rollups

```powershell
.\bin\dash-sim-client.exe dpu-counters --include-enis --include-vnets -o table
```

Expected (sorted alphabetically; `vnet-prod` strictly exceeds
`vnet-stage` because its child `vnet_mapping ["vnet-prod","10.0.0.20"]`
contributes via the first-component scope rule):

```text
PER-ENI
SCOPE    PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
eni-001  412         824          28832     57664      4
eni-002  421         842          29462     58924      4

PER-VNET
SCOPE       PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
vnet-prod   168         337          11808     23560      4
vnet-stage  84          168          5896      11792      2
```

#### Watch mode + CSV pipe

```powershell
# Live tail at 2s intervals (Ctrl-C exits cleanly):
.\bin\dash-sim-client.exe dpu-counters --watch --interval 2s --include-enis

# Dump 5s of samples to a spreadsheet-shaped CSV (header + per-scope rows):
1..5 | ForEach-Object { .\bin\dash-sim-client.exe dpu-counters `
  --include-enis --include-vnets -o csv; Start-Sleep -Seconds 1 } `
  | Out-File -Encoding utf8 burst.csv
Get-Content burst.csv -TotalCount 6
```

CSV header is stable across releases:

```csv
device_id,sampled_at_ns,scope_kind,scope_key,packets_in,packets_out,bytes_in,bytes_out,drops
```

#### Fault injection on the new RPC

```powershell
# Make the next GetDpuCounters fail once:
$body = @{ op = "GetDpuCounters"; mode = "error"; count = 1; message = "demo" } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/faults -Body $body -ContentType application/json

# First call -> "rpc error: code = Unavailable desc = demo" (exit 1)
.\bin\dash-sim-client.exe dpu-counters

# Second call succeeds (fault count exhausted)
.\bin\dash-sim-client.exe dpu-counters
```

In `--watch` mode the same injected error is logged to stderr as
`dpu-counters: rpc error: …` and the loop keeps polling — does NOT
exit on transient failures.

## 9b. PE-3c add-on — `ResetDpuCounters` (counter-only reset)

**RPC**: `dashapi.v1.DashApi.ResetDpuCounters`. **REST cascade**: dashd
`DELETE /v1/observability/counters[/{dpu_id}]` with `?reset_sim=true`
calls this RPC on each target DPU's sim before clearing the local cache.
**CLI**: `dashctl counters clear [--dpu=ID] --reset-sim`.

**Motivation**: `POST /admin/reset` on dash-sim wipes ALL objects (ENIs,
VNETs, etc.) — far too destructive. Operators need a counter-only reset
that zeros the accumulators without disturbing the programmed state.

**Proto change** (additive):
```proto
// proto/dashapi/v1/dashapi.proto
message ResetDpuCountersRequest {
  // Empty = reset all counters on this DPU.
  // Future: optional filter fields (per-ENI, per-VNET scope).
}

message ResetDpuCountersResponse {
  int32 keys_reset = 1; // number of object-counter entries zeroed
}

service DashApi {
  // ... existing RPCs ...
  rpc ResetDpuCounters (ResetDpuCountersRequest) returns (ResetDpuCountersResponse);
}
```

**Implementation plan**:

| Layer | What | LOC est. |
|---|---|---|
| `counters.Registry.ResetAll()` | Zero every `objectCounters` entry atomically (swap buckets) | ~12 |
| `sim/server.ResetDpuCounters()` | gRPC handler: call `Registry.ResetAll()`, return count | ~15 |
| `dpuclient.DpuClient` interface | Add `ResetDpuCounters(ctx, req)` method | ~5 |
| `dpuclient.realClient` | gRPC wrapper | ~10 |
| `dpuclient.MockClient` | Test mock with `ResetErr` / `ResetCallCount` | ~15 |
| dashd REST handler | `DELETE` gains `?reset_sim=true` query param; cascades to `dpuclient.ResetDpuCounters` before cache wipe | ~25 |
| dashctl cmd | `--reset-sim` flag on `counters clear` | ~10 |
| Tests | sim handler UT, dpuclient UT, REST handler UT, dashctl cmd UT, live e2e | ~80 |

**Test plan**:

```powershell
# Before:
dashctl counters --endpoint http://localhost:28443 --insecure
# (values at ~7M)

# Reset + clear:
dashctl counters clear --reset-sim --endpoint http://localhost:28443 --insecure
# "cleared 10 + reset 10 DPU counter accumulators"

# After (wait 6s for refill):
dashctl counters --endpoint http://localhost:28443 --insecure
# (values near zero — fresh accumulation from the ~6s of ticking since reset)
```


## 9c. Referential integrity — FK validation

### Why this matters

The DASH pipeline is a **dependency graph**, not a flat list. An ENI
needs a VNet to know which virtual network it belongs to. A route
needs a route group to be reachable. An ACL rule needs its ACL group.
If any of these references are wrong — a typo, a missing parent, a
wrong creation order — the DPU silently drops packets. The operator
discovers the problem minutes later through counter spikes, not at
the moment of misconfiguration.

With `--strict-refs` (enabled by default), dash-sim validates **all
25 foreign-key relationships at apply time**. A bad reference gets
an immediate, actionable error — not a silent drop 20 minutes later.

**Launch with FK validation (default):**

```powershell
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8080 --device-id dpu-sim-01
```

**Disable for legacy/test pipelines:**

```powershell
.\bin\dash-sim.exe --strict-refs=false --grpc-listen :50051 --admin-listen :8080
```

### Understanding the dependency graph

Every DASH object kind lives on one of three tiers. Objects on higher
tiers **depend on** objects on lower tiers. You must create lower-tier
objects first.

```
                          ┌─────────────────────────────────────────────┐
                          │              Tier 2 (leaf objects)          │
                          │  eni_route → eni + route_group             │
                          │  acl_in/out → eni + acl_group              │
                          │  route_rule → eni + vnet                   │
                          │  meter → eni                               │
                          │  ha_scope_config → ha_scope + ha_set       │
                          └─────────────────┬───────────────────────────┘
                                            │ depends on
                          ┌─────────────────▼───────────────────────────┐
                          │              Tier 1 (mid-level)             │
                          │  eni → vnet (+ optional qos)               │
                          │  acl_rule → acl_group (+ optional tags)    │
                          │  route → route_group + vnet/tunnel         │
                          │  vnet_mapping → vnet (+ optional tunnel)   │
                          │  meter_rule → meter_policy                 │
                          │  ha_scope → ha_set                         │
                          └─────────────────┬───────────────────────────┘
                                            │ depends on
                          ┌─────────────────▼───────────────────────────┐
                          │              Tier 0 (roots — no deps)       │
                          │  vnet, qos, acl_group, route_group,        │
                          │  routing_appliance, prefix_tag, tunnel,    │
                          │  meter_policy, outbound_port_map, ha_set   │
                          └─────────────────────────────────────────────┘

      Rule: create bottom-up (Tier 0 → Tier 1 → Tier 2)
            delete top-down  (Tier 2 → Tier 1 → Tier 0)
```

### Experiment 1 — wrong config: ENI references a vnet that doesn't exist

**What we'll try**: create an ENI that references `"vnet-bllue"` — a
typo for `"vnet-blue"`. The store is empty, so no vnet exists at all.

```powershell
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind eni --key eni-bad `
  --value '{"vnet":"vnet-bllue"}'
```

**What happens**: the Apply is **rejected**.

```
Apply rejected: referential integrity: eni references vnet "vnet-bllue"
(field vnet) which does not exist; create it first
```

**Why it failed**: ENI is a Tier 1 object. It has a `vnet` field that
references a `vnet` object (Tier 0). The sim looked up `"vnet-bllue"`
in the store's vnet table — it's not there. The error tells you:
- **what** you tried to create (`eni`)
- **what's missing** (`vnet "vnet-bllue"`)
- **which field** carries the bad reference (`vnet`)
- **how to fix it** ("create it first")

The ENI was **not stored** — no silent corruption.

### Experiment 2 — wrong config: Tier 2 object before its Tier 1 parent

**What we'll try**: create an `eni_route` (Tier 2) that binds
`"eni-ghost"` to a route group — but `eni-ghost` doesn't exist.

```powershell
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind eni_route --key eni-ghost `
  --value '{"group_id":"rg-prod"}'
```

**What happens**: **rejected** — two FK violations.

```
Apply rejected: referential integrity: eni_route references eni "eni-ghost"
(field key.eni) which does not exist; create it first
```

**Why it failed**: `eni_route` is Tier 2. Its key contains the ENI
name (`key[0] = "eni-ghost"`), and its `group_id` field references a
route group. Both are checked. The ENI doesn't exist, so the first
check fails immediately. Even if the ENI existed, `"rg-prod"` would
also fail unless a route group by that name was created first.

### Experiment 3 — wrong config: ACL rule without its ACL group

**What we'll try**: create an ACL rule in group `"acl-web"` — but the
group doesn't exist.

```powershell
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind acl_rule --key acl-web:100 `
  --value '{}'
```

**What happens**: **rejected**.

```
Apply rejected: referential integrity: acl_rule references acl_group "acl-web"
(field key.group_id) which does not exist; create it first
```

**Why it failed**: ACL rules live inside ACL groups. The rule's key
starts with the group ID (`key[0] = "acl-web"`). The group must exist
first because the sim needs to know the rule belongs to a valid group.

### Experiment 4 — right config: build a complete ENI pipeline bottom-up

Now let's do it correctly — Tier 0 first, then Tier 1, then Tier 2.
Every Apply succeeds because each object's dependencies already exist.

```powershell
# ─── Tier 0: create roots (no dependencies) ─────────────────────
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind vnet --key vnet-lab --value '{"vni":9999}'
# → accepted ✅  (vnet is Tier 0 — no FK checks needed)

.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind acl_group --key acl-lab --value '{}'
# → accepted ✅

.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind route_group --key rg-lab --value '{}'
# → accepted ✅

# ─── Tier 1: reference Tier 0 ───────────────────────────────────
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind eni --key eni-lab --value '{"vnet":"vnet-lab"}'
# → accepted ✅  (vnet-lab exists — FK check passes)

.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind acl_rule --key acl-lab:100 --value '{}'
# → accepted ✅  (acl_group "acl-lab" exists)

.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind route --key rg-lab:10.0.0.0/8 `
  --value '{"vnet":"vnet-lab"}'
# → accepted ✅  (route_group "rg-lab" + vnet "vnet-lab" both exist)

# ─── Tier 2: reference Tier 0 + Tier 1 ──────────────────────────
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind eni_route --key eni-lab `
  --value '{"group_id":"rg-lab"}'
# → accepted ✅  (eni "eni-lab" + route_group "rg-lab" both exist)

.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind acl_in --key eni-lab:1 `
  --value '{"v4_acl_group_id":"acl-lab"}'
# → accepted ✅  (eni "eni-lab" + acl_group "acl-lab" both exist)
```

**What you built**: a complete ENI pipeline — vnet → eni → eni_route
→ acl_in, with routes and ACL rules. Every object's FK references
resolved successfully because you created them in tier order.

### Experiment 5 — fix-then-retry workflow

Real operators hit FK errors by accident (typos, wrong order). The
workflow is simple: read the error → create what's missing → retry.

```powershell
# Step 1: attempt fails (tunnel doesn't exist)
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind route --key rg-lab:192.168.0.0/16 `
  --value '{"tunnel":"tun-missing"}'
# → rejected: route references tunnel "tun-missing" which does not exist

# Step 2: create the missing tunnel
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind tunnel --key tun-missing --value '{}'
# → accepted ✅

# Step 3: retry the route — now succeeds
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind route --key rg-lab:192.168.0.0/16 `
  --value '{"tunnel":"tun-missing"}'
# → accepted ✅
```

### Experiment 6 — backward compatibility: disable strict refs

For legacy pipelines that create objects in arbitrary order, disable
FK validation:

```powershell
# Start sim with --strict-refs=false
.\bin\dash-sim.exe --strict-refs=false --grpc-listen :50051 --admin-listen :8080

# Now any Apply succeeds regardless of FK order
.\bin\dash-sim-client.exe --target localhost:50051 apply `
  --kind eni --key eni-any --value '{"vnet":"nonexistent"}'
# → accepted (no FK check — the dangling ref sits silently in the store)
```

> **Warning**: with `--strict-refs=false`, typos and missing parents go
> undetected. You'll only discover them at packet time as traffic drops.

### Common FK relationships (quick reference)

| Object (Tier) | References | Via field |
|---|---|---|
| eni (T1) | vnet, qos | `vnet`, `qos` |
| acl_rule (T1) | acl_group, prefix_tag | `key[0]`, `src_tag/dst_tag` |
| route (T1) | route_group, vnet, tunnel, appliance | `key[0]`, `vnet`, `tunnel`, `appliance` |
| vnet_mapping (T1) | vnet, tunnel, port_map | `key[0]`, `tunnel`, `port_map` |
| eni_route (T2) | eni, route_group | `key[0]`, `group_id` |
| acl_in/out (T2) | eni, acl_group | `key[0]`, `v4/v6_acl_group_id` |
| route_rule (T2) | eni, vnet | `key[0]`, `vnet` |
| meter (T2) | eni | `key[0]` |

### Tests

```powershell
# 51 unit tests covering all 25 FK families (100% coverage on core functions)
& $go test -v -count=1 ./internal/sim/model/ -run TestRefs

# 9 integration tests over gRPC (reject, accept, error quality, fix-then-retry)
& $go test -v -count=1 ./test/integration/ -run TestIntegration_Refs
```

See [referential-integrity-validation.md](../../../docs/dashd-features/referential-integrity-validation.md)
for the full FK map and design.

---

## 10. Object kinds

```powershell
.\bin\dash-sim-client.exe kinds -o table
```

29 upstream DASH kinds: appliance, vnet, eni, eni_route, acl_group, acl_rule,
acl_in, acl_out, route, route_group, route_rule, route_type,
routing_appliance, prefix_tag, vnet_mapping, tunnel, pa_validation, qos,
meter, meter_policy, meter_rule, outbound_port_map, outbound_port_map_range,
ha_scope, ha_scope_config, ha_scope_state, ha_set, ha_set_config,
ha_set_state.

## 11. Troubleshooting

| Symptom | Fix |
|---|---|
| `bind: ... forbidden by its access permissions` | Pick a port not in Windows reserved ranges (e.g. 50051, 52051 work; 50061 is often reserved). |
| `apply: kind X expects N key parts ...` | Joined `--key` doesn't have N `:`-separated parts. Run `kinds` to see KEY_PARTS. |
| `decode value: unknown field` | YAML field name doesn't match upstream protojson; consult the generated `.pb.go` file for that kind. |
| `SimulatePacket ... Unimplemented` | You're talking to `dash-redis-adapter` — use `dash-sim` for the pipeline. |
| Adapter `connection refused` | Make sure Redis is up at `--redis <addr>`, or use `--embedded-redis` for a self-contained demo. |
