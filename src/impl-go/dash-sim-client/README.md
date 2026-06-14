# `dash-sim-client` — standalone gRPC client for DASH endpoints

A small CLI + reusable Go SDK that speaks the `dashsim.v1.DashSim` gRPC
service defined in [proto/dashsim/v1/dashsim.proto](../../../proto/dashsim/v1/dashsim.proto).

**Independent of `dash-sim`.** This module is intentionally a pure client of
the wire contract — it can talk to:

- a local `dash-sim` simulator
- a remote `dash-sim` running on another host
- in the future, any real DPU agent that implements the same gRPC service
- (later) a thin compatibility shim in front of a SAI/gNMI DPU

That's why it lives in its own Go module — it must not pull in any of
`dash-sim`'s internal packages.

## Two things in one module

| Path             | What it is                                          |
|------------------|-----------------------------------------------------|
| `cmd/dash-sim-client/` | The `dash-sim-client` CLI binary              |
| `pkg/client/`    | Reusable Go SDK (also importable by tests & dashd)  |

`pkg/client/` is **public on purpose** so the conformance test harness in
[`test/conformance/`](../../../test/conformance/) and any future Go consumer
can reuse it without copy-paste.

## Build

```powershell
# From src/impl-go/
make all                    # builds bin/dash-sim-client
./bin/dash-sim-client --help
```

## CLI shape (planned — kubectl-style verbs)

```powershell
# Connection
dash-sim-client --target localhost:50051 ...

# CRUD
dash-sim-client vnet create vnet-prod --vni 1001
dash-sim-client vnet get    vnet-prod
dash-sim-client vnet list
dash-sim-client vnet delete vnet-prod

dash-sim-client eni create eni-100 --vnet vnet-prod --mac 00:aa:bb:00:01:00 --addr 10.0.0.4/24
dash-sim-client eni update eni-100 --addr 10.0.0.99/24
dash-sim-client eni list

dash-sim-client acl group  add  acl-default --stage inbound
dash-sim-client acl rule   add  acl-default --num 10 --action allow --src 10.0.0.0/24
dash-sim-client route add --table vnet-prod --dst 10.1.0.0/24 --action forward
dash-sim-client mapping add --vnet vnet-prod --underlay 10.0.0.10

# Streaming
dash-sim-client subscribe --kinds vnet,eni,acl_rule --snapshot

# Counters
dash-sim-client counters get eni-100

# Output modes
dash-sim-client vnet list --output json
dash-sim-client vnet list --output yaml
dash-sim-client vnet list --output wide
```

## PE-3a — typed per-DPU counter rollups (`dpu-counters`)

Added in PE-3a (gate **PE-G8**). The legacy `counters --kind X --key Y`
command returns the bag of synthetic counters scoped to one object;
`dpu-counters` returns a typed **rollup** at three nested scopes in a
single round-trip:

- **DPU-wide** — sum of every (kind, key) tracked by the device.
  Always populated.
- **Per-ENI** — opt-in via `--include-enis`. Walks every key whose
  first joined component matches an ENI name from the store
  (`OBJECT_KIND_ENI` keys + child keys like
  `OBJECT_KIND_ENI_ROUTE` / `OBJECT_KIND_ACL_IN` that begin with
  `<eni>:…`).
- **Per-VNET** — opt-in via `--include-vnets`. Same shape using
  `OBJECT_KIND_VNET` keys + child `OBJECT_KIND_VNET_MAPPING` keys.

Filters keep responses small in busy fleets; an explicit filter scope
that doesn't exist in the store is **still returned** with a zero
bucket so the operator gets a visible "I asked, you have none" signal.

Full design + Future Scopes:
[`docs/dashd-features/dash-sim-counter-rollups.md`](../../../docs/dashd-features/dash-sim-counter-rollups.md).

### Flag reference

| Flag | Default | Description |
|---|---|---|
| `--include-enis` | `false` | Populate the per-ENI rollup table. |
| `--include-vnets` | `false` | Populate the per-VNET rollup table. |
| `--eni-names` | (empty) | Comma-separated ENI scope keys. Implies `--include-enis`. |
| `--vnet-keys` | (empty) | Comma-separated VNET scope keys. Implies `--include-vnets`. |
| `--watch` | `false` | Stream periodic snapshots until Ctrl-C. |
| `--interval` | `1s` | Watch-mode sample interval. Must be > 0. |
| `-o` / `--output` | `json` | One of: `table` (default for human reads), `json`, `yaml`, **`csv`** (new in PE-3a). |

### Quick experiments

> **Pre-req**: bring up a sim with the small scenario in a separate
> terminal (so we have something to inspect):
>
> ```powershell
> .\dash-sim.exe --device-id dpu-sim-01 --tick-interval 1s `
>   --scenario .\testdata\scenarios\small.yaml
> ```

#### A) One-shot DPU snapshot (default pretty table)

```powershell
.\dash-sim-client.exe dpu-counters -o table
```

Expected (counters tick once per `--tick-interval`, so values vary
with how long the sim has been running):

```text
DEVICE  dpu-sim-01
TIME    2026-06-14T20:14:33Z (ns=1718388873000000000)

DPU TOTALS
SCOPE  PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
dpu    1247        2486         87104     174208     12
```

#### B) Include per-ENI rollups

```powershell
.\dash-sim-client.exe dpu-counters --include-enis -o table
```

Expected (the `small` scenario defines `eni-001` + `eni-002` with one
`acl_in` child key under `eni-001` — both ENIs appear, sorted):

```text
DEVICE  dpu-sim-01
TIME    2026-06-14T20:14:34Z (ns=1718388874000000000)

DPU TOTALS
SCOPE  PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
dpu    1251        2492         87308     174616     12

PER-ENI
SCOPE    PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
eni-001  412         824          28832     57664      4
eni-002  421         842          29462     58924      4
```

#### C) Include per-VNET rollups (child mappings attribute upward)

```powershell
.\dash-sim-client.exe dpu-counters --include-vnets -o table
```

The `small` scenario defines `vnet-prod` (+ child `vnet_mapping` keyed
`vnet-prod:10.0.0.20`) and `vnet-stage`. `vnet-prod`'s bucket strictly
exceeds `vnet-stage` because its child mapping contributes:

```text
PER-VNET
SCOPE       PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
vnet-prod   168         337          11808     23560      4
vnet-stage  84          168          5896      11792      2
```

#### D) Watch mode (live tail until Ctrl-C)

```powershell
.\dash-sim-client.exe dpu-counters --watch --interval 2s --include-enis
```

A `----` separator delimits each successive snapshot in scrollback:

```text
→ first immediate snapshot prints here

DEVICE  dpu-sim-01
TIME    2026-06-14T20:14:36Z (ns=...)
DPU TOTALS
...
----
DEVICE  dpu-sim-01
TIME    2026-06-14T20:14:38Z (ns=...)
...
^C       # exits cleanly
```

> Transient RPC errors during watch DO NOT kill the loop — the error
> is logged to stderr (`dpu-counters: rpc error: …`) and the next tick
> continues. Use Ctrl-C to stop.

#### E) Filter to specific scopes (with a deliberately missing one)

```powershell
.\dash-sim-client.exe dpu-counters --eni-names eni-001,eni-missing -o table
```

Expected — `eni-missing` appears with a zero bucket on purpose:

```text
PER-ENI
SCOPE        PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
eni-001      412         824          28832     57664      4
eni-missing  0           0            0         0          0
```

#### F) CSV for spreadsheets / scripts

```powershell
.\dash-sim-client.exe dpu-counters --include-enis --include-vnets -o csv `
  | Out-File -Encoding utf8 snapshot.csv
Get-Content snapshot.csv -TotalCount 4
```

Expected first four rows (header + DPU + first 2 entities):

```csv
device_id,sampled_at_ns,scope_kind,scope_key,packets_in,packets_out,bytes_in,bytes_out,drops
dpu-sim-01,1718388873000000000,dpu,,1247,2486,87104,174208,12
dpu-sim-01,1718388873000000000,eni,eni-001,412,824,28832,57664,4
dpu-sim-01,1718388873000000000,eni,eni-002,421,842,29462,58924,4
```

CSV is intentionally a flat shape with one row per scope (DPU first,
then every ENI, then every VNET) and a `scope_kind` discriminator so
the consumer can route by dimension. Header is stable across releases.

#### G) JSON for scripting

```powershell
.\dash-sim-client.exe dpu-counters --include-enis -o json
```

Expected — stable envelope (snake_case keys mirror the wire shape, plus
an RFC 3339 `sampled_at` derived from `sampled_at_ns` for convenience):

```json
{
  "device_id": "dpu-sim-01",
  "sampled_at": "2026-06-14T20:14:33Z",
  "sampled_at_ns": 1718388873000000000,
  "dpu": {
    "packets_in": 1247,
    "packets_out": 2486,
    "bytes_in": 87104,
    "bytes_out": 174208,
    "drops": 12
  },
  "enis": [
    { "scope_key": "eni-001", "bucket": { "packets_in": 412, "packets_out": 824, "bytes_in": 28832, "bytes_out": 57664, "drops": 4 } },
    { "scope_key": "eni-002", "bucket": { "packets_in": 421, "packets_out": 842, "bytes_in": 29462, "bytes_out": 58924, "drops": 4 } }
  ]
}
```

Pipeable with `jq`:

```powershell
.\dash-sim-client.exe dpu-counters --include-enis -o json `
  | jq '.enis[] | select(.bucket.drops > 0) | .scope_key'
```

#### H) Fault injection (operator-controlled chaos)

The new RPC participates in the same fault-injection framework as every
other DashApi RPC. Use the sim's admin HTTP API to inject a one-shot
`Unavailable` and observe how the client behaves:

```powershell
# Tell the sim to fail the next GetDpuCounters call:
Invoke-RestMethod -Method POST `
  -Uri http://localhost:8080/admin/faults `
  -ContentType application/json `
  -Body '{"op":"GetDpuCounters","mode":"error","count":1,"message":"injected for demo"}'

# Run the CLI — first call exits non-zero:
.\dash-sim-client.exe dpu-counters
# rpc error: code = Unavailable desc = injected for demo

# Run again — fault count was 1, so the next call succeeds:
.\dash-sim-client.exe dpu-counters
```

In `--watch` mode the same fault is logged to stderr and the loop
keeps polling (won't exit on the injected error).

#### I) Legacy `counters` command still works

PE-3a is purely additive. The pre-existing per-object counter
inspector is unchanged:

```powershell
.\dash-sim-client.exe counters --kind eni --key eni-001
```

Expected (the bag of synthetic counters for one (kind, key)):

```json
{
  "bytes_in": 28832,
  "bytes_out": 57664,
  "drops": 4,
  "packets_in": 412,
  "packets_out": 824
}
```

### Scope membership rules (cheat sheet)

A registry key `K` contributes to scope `S` iff:

- `K == S` (exact, single-component key) **OR**
- `K` begins with `S + ":"` (multi-component child key)

So in the `small` scenario, scope `eni-001` claims:

- `eni-001` itself (`OBJECT_KIND_ENI`)
- `eni-001:1` from `OBJECT_KIND_ACL_IN ["eni-001", "1"]`

And scope `vnet-prod` claims:

- `vnet-prod` itself
- `vnet-prod:10.0.0.20` from `OBJECT_KIND_VNET_MAPPING ["vnet-prod", "10.0.0.20"]`

The substring trap (`eni-1` vs `eni-10`) is explicitly NOT confused —
exact-match-or-`:`-prefix means `eni-1` never claims `eni-10`'s rows.

## Layout

```
dash-sim-client/
├── go.mod
├── cmd/
│   └── dash-sim-client/main.go    Cobra entry point
├── pkg/
│   └── client/                    Reusable SDK
│       ├── client.go              Dial / options / Close
│       ├── vnet.go                CRUD wrappers
│       ├── eni.go
│       ├── acl.go
│       ├── route.go
│       ├── mapping.go
│       ├── subscribe.go           Stream helper with reconnect
│       └── counters.go
└── internal/
    ├── cmd/                       Cobra command definitions
    │   ├── root.go
    │   ├── vnet.go
    │   ├── eni.go
    │   ├── acl.go
    │   ├── route.go
    │   ├── mapping.go
    │   ├── subscribe.go
    │   └── counters.go
    └── render/                    --output json/yaml/wide/table
```
