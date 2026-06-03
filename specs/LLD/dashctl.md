# LLD — dashctl (DashCenter controller CLI) — INITIAL DRAFT

> **Status: DRAFT.** This document captures the planned design of
> `dashctl`, the Phase 4 controller-facing CLI. The binary exists today
> as a scaffold ([`src/impl-go/dashctl/`](../../src/impl-go/dashctl/))
> that prints a version banner. This LLD is intended to drive its
> implementation, in lock-step with
> [LLD/dashd.md](dashd.md).

`dashctl` is to `dashd` what `dash-sim-client` is to `dash-sim`: a
transport-only Cobra-based CLI that speaks `dashcenter.v1.ControlPlane`
RPCs against a running controller. Operators use it to declare desired
state and inspect fleet reconciliation status.

---

## Table of contents

1. [Scope](#1-scope)
2. [Layering vs. dash-sim-client](#2-layering-vs-dash-sim-client)
3. [Module layout](#3-module-layout)
4. [Public SDK (pkg/client)](#4-public-sdk-pkgclient)
5. [Cobra command tree](#5-cobra-command-tree)
6. [Per-command sketches](#6-per-command-sketches)
7. [Configuration & contexts](#7-configuration-and-contexts)
8. [Output formats](#8-output-formats)
9. [Auth model](#9-auth-model)
10. [Rust pseudocode parity (sketch)](#10-rust-pseudocode-parity-sketch)
11. [Test surface](#11-test-surface)
12. [Open questions](#12-open-questions)
13. [Phased milestones](#13-phased-milestones)

---

## 1. Scope

A single binary that:
- dials `dashd`'s `dashcenter.v1` gRPC endpoint (default `:9443`)
- creates / updates / deletes operator-level policy objects
  (`Vnet`, `Eni`, `VnetMapping`, `AclPolicy`, `RoutePolicy`, ...)
- manages the DPU **inventory**
- inspects per-DPU **status, drift, and reconciliation queue**
- triggers manual **reconciliation** sweeps
- has a `--target` flag like `dash-sim-client` so users can swap
  controllers (dev vs prod) without rebuilds

What it is NOT:
- It does **not** talk `dashapi.v1` directly to DPUs. For single-DPU
  workflows, use [`dash-sim-client`](dash-sim-client.md).

---

## 2. Layering vs. dash-sim-client

```
operator
   │
   ▼
┌──────────────────┐                           ┌────────────────────────┐
│     dashctl      │                           │   dash-sim-client      │
│ (controller CLI) │                           │   (per-DPU CLI)        │
└────────┬─────────┘                           └────────┬───────────────┘
         │ dashcenter.v1                                │ dashapi.v1
         ▼                                              ▼
   ┌──────────┐                                   ┌──────────────────┐
   │  dashd   │ ── fans out dashapi.v1 ───► many │  dash-sim /      │
   │          │                                   │  dash-redis-     │
   └──────────┘                                   │  adapter / DPU   │
                                                  └──────────────────┘
```

Same architectural invariant as `dash-sim-client`: **transport only**,
no controller-internal imports. Mostly the same code shape, different
proto package.

---

## 3. Module layout

```
dashctl/
├── go.mod
├── Dockerfile                                   -- planned
├── README.md
├── cmd/dashctl/main.go                          -- one-line: calls cmd.Execute()
├── internal/
│   ├── cmd/                                     -- Cobra subcommand tree
│   │   ├── root.go                              -- root + persistent flags + context resolution
│   │   ├── inventory.go                         -- put/get inventory
│   │   ├── vnet.go                              -- put/get/list/delete vnets
│   │   ├── eni.go
│   │   ├── mapping.go                           -- vnet-mappings
│   │   ├── acl.go                               -- acl-policies + acl-rules
│   │   ├── route.go                             -- route-policies + routes
│   │   ├── dpu.go                               -- dpu status, drift
│   │   ├── reconcile.go                         -- trigger sweep
│   │   ├── apply.go                             -- apply -f <yaml> (multi-doc, all kinds)
│   │   ├── delete.go                            -- generic delete <kind> <name>
│   │   ├── get.go                               -- generic get <kind> <name>
│   │   ├── list.go                              -- generic list <kind>
│   │   ├── codec.go                             -- json/yaml dispatch helpers
│   │   └── helpers.go                           -- ack printing, name parsing
│   └── render/render.go                         -- json/yaml/table output
└── pkg/
    └── client/client.go                         -- Dial + every ControlPlane RPC wrapper
```

---

## 4. Public SDK (pkg/client)

Mirrors the shape of `dash-sim-client/pkg/client`, just over
`dashcenter.v1`:

```go
type Client struct { /* unexported */ }

func Dial(addr string, opts ...Option) (*Client, error)
func (c *Client) Close() error
func (c *Client) Raw() dashcenter.ControlPlaneClient

// Inventory
func (c *Client) PutInventory(ctx, *Inventory) (*Ack, error)
func (c *Client) GetInventory(ctx) (*Inventory, error)

// Generic policy CRUD (typed wrappers also exist per spec)
func (c *Client) Put(ctx, *PolicyObject) (*Ack, error)
func (c *Client) Get(ctx, kind, name string) (*PolicyObject, error)
func (c *Client) Delete(ctx, kind, name string) (*Ack, error)
func (c *Client) List(ctx, kind string) ([]*PolicyObject, error)

// DPU status
func (c *Client) DpuStatus(ctx, dpu string) (<-chan *DpuStatusReport, <-chan error, error)

// Reconciliation control
func (c *Client) Reconcile(ctx, dpuIds []string) (*Ack, error)
```

Insecure transport credentials by default; TLS / mTLS via
`client.WithDialOptions`. 10s per-RPC timeout default.

---

## 5. Cobra command tree

```
dashctl
├── inventory
│   ├── put           -- apply inventory from --file
│   ├── get           -- show current inventory
│   └── list          -- shorthand for `inventory get -o table`
├── apply             -- generic: apply any kind(s) from -f
├── get               -- generic: get <kind> <name>
├── delete            -- generic: delete <kind> <name>
├── list              -- generic: list <kind>
├── vnet              -- typed subcommand group
│   ├── put           -- inline: --name --vni [--peer ...]
│   ├── get
│   ├── list
│   └── delete
├── eni
│   ├── put           -- inline: --id --mac --vnet --dpu-id ...
│   ├── get / list / delete
├── mapping           -- vnet-mappings
│   ├── put / get / list / delete
├── acl
│   ├── policy
│   │   ├── put -f / get / list / delete
│   └── rule          -- inline rule add/remove
├── route
│   ├── policy
│   │   ├── put -f / get / list / delete
├── dpu
│   ├── list          -- snapshot of /admin/health style summary
│   ├── status        -- streaming DpuStatus (Ctrl+C to stop)
│   └── drift         -- show desired vs observed diff for a DPU
├── reconcile         -- force a sweep, all DPUs or --dpu repeatable
├── version
└── completion        -- bash/zsh/fish/pwsh autocompletion
```

### Persistent flags

| Flag | Default | Purpose |
|---|---|---|
| `--target` | `localhost:9443` | dashd gRPC endpoint |
| `--context` | (empty) | named context from config file |
| `-o, --output` | `json` | `json` / `yaml` / `table` |
| `--timeout` | `10s` | per-RPC timeout |
| `--insecure` | `true` | plaintext gRPC (TLS via `--ca`/`--cert`/`--key`) |
| `--token` | (env `DASHCTL_TOKEN`) | bearer token for auth |
| `--audit-by` | (empty) | optional caller identity tag (for `dashd` audit log) |

---

## 6. Per-command sketches

### 6.1 `inventory put`

```
dashctl inventory put -f inventory.yaml
```

```yaml
# inventory.yaml
dpus:
  - id: dpu-r1-r5
    endpoint: 10.0.5.7:50051
    region: westus2
    site: r1
    labels: { tier: prod }
  - id: dpu-r1-r6
    endpoint: 10.0.5.8:50051
    region: westus2
    site: r1
```

Behaviour: parse YAML → protojson → `Inventory` → `PutInventory`. Prints
`OK` Ack.

### 6.2 `vnet put`

Two modes (parity with `dash-sim-client apply`):

```bash
# inline
dashctl vnet put --name vnet-prod --vni 1001 --peer vnet-dev

# from file
dashctl vnet put -f vnet-prod.yaml
```

### 6.3 `eni put`

```bash
dashctl eni put \
    --id eni-001 --mac 00:11:22:33:44:55 \
    --vnet vnet-prod --dpu-id dpu-r1-r5 \
    --admin-state ENABLED \
    --acl-in-group-v4 acl-prod-in \
    --acl-out-group-v4 acl-prod-out
```

### 6.4 `dpu status`

Streams `DpuStatusReport`s (server-sent). One JSON line per report:

```json
{"dpu":"dpu-r1-r5","health":"HEALTHY","last_seen_ts_ns":...,
 "desired_objects":42,"observed_objects":42,"drift_objects":0}
```

### 6.5 `dpu drift`

Snapshot of the desired-vs-observed diff for one DPU:

```
DPU       OP      KIND          KEY                          REASON
dpu-r1-r5 ADD     vnet_mapping  vnet-prod:10.0.0.99           not yet applied
dpu-r1-r5 REMOVE  acl_rule      acl-prod-in:200               removed from policy
```

### 6.6 `reconcile`

```bash
dashctl reconcile                       # all DPUs
dashctl reconcile --dpu dpu-r1-r5 --dpu dpu-r1-r6
```

### 6.7 Generic `apply -f`

A multi-doc YAML where each entry is `{kind, name, spec}`:

```yaml
---
kind: vnet
name: vnet-prod
spec: { vni: 1001 }
---
kind: eni
name: eni-001
spec:
  id: eni-001
  mac: "00:11:22:33:44:55"
  vnet: vnet-prod
  dpu_id: dpu-r1-r5
  admin_state: ENABLED
```

Routes each doc to the right `Put<Kind>` RPC. Mirrors
`dash-sim-client apply -f` ergonomics.

### 6.8 `get` / `delete` / `list`

Generic dispatcher backed by `Client.Get / Delete / List`. Output is the
`PolicyObject` envelope `{kind, name, spec}` (mirrors
`dash-sim-client`'s `{kind, key, value}`).

---

## 7. Configuration and contexts

Optional `~/.dashctl/config.yaml` to manage multiple controllers:

```yaml
current-context: prod
contexts:
  prod:
    target: dashd.prod.example.com:9443
    ca:    /etc/dashctl/prod-ca.pem
    token-env: DASHCTL_TOKEN_PROD
  dev:
    target: localhost:9443
    insecure: true
```

```bash
dashctl --context prod inventory get
```

Per-invocation flags override the context.

---

## 8. Output formats

Identical scheme to `dash-sim-client`:

| Format | Notes |
|---|---|
| `json` | default; pretty, protojson with `UseProtoNames=true` |
| `yaml` | json round-tripped through `yaml.v3` |
| `table` | concise per-row summary, kind-specific columns |

The renderer lives in `internal/render` and shares the envelope concept
(`kind`, `name`, `spec`).

---

## 9. Auth model

- `--token` / `$DASHCTL_TOKEN`: bearer token sent as
  `Authorization: Bearer ...` (gRPC metadata).
- TLS / mTLS: via dial options (`--ca`, `--cert`, `--key`).
- Role mapping is done server-side in `dashd`. `dashctl` only carries
  credentials.

Future: OIDC device flow (`dashctl login`).

---

## 10. Rust pseudocode parity (sketch)

```rust
#[derive(Parser)]
#[command(name = "dashctl")]
struct Cli {
    #[arg(long, default_value = "localhost:9443")] target: String,
    #[arg(long)] context: Option<String>,
    #[arg(short = 'o', long, default_value = "json")] output: String,
    #[arg(long, default_value_t = true)] insecure: bool,
    #[arg(long, env = "DASHCTL_TOKEN")] token: Option<String>,
    #[command(subcommand)] cmd: Cmd,
}

#[derive(Subcommand)]
enum Cmd {
    Inventory(InventoryArgs),
    Apply(ApplyArgs),
    Vnet(VnetArgs),
    Eni(EniArgs),
    Mapping(MappingArgs),
    Acl(AclArgs),
    Route(RouteArgs),
    Dpu(DpuArgs),
    Reconcile(ReconcileArgs),
    Get(GetArgs),
    Delete(GetArgs),
    List(ListArgs),
    Version,
    Completion(CompletionArgs),
}
```

The SDK is a thin tonic wrapper, exactly parallel to the Rust pseudocode
in [LLD/dash-sim-client.md § 12](dash-sim-client.md#12-rust-pseudocode-parity).

---

## 11. Test surface

| Layer | Plan |
|---|---|
| Unit | `parseKindArg`, `parseContext`, value coercion, output rendering (golden files). |
| Integration | bufconn fixture serving a fake `ControlPlane` to exercise every subcommand end-to-end. |
| E2E | run against a live `dashd` + fleet of `dash-sim` containers from `deploy/compose/`. |

---

## 12. Open questions

1. **Inline vs file-only inputs.** `dash-sim-client` supports both; do
   we keep parity for `dashctl` or push users to YAML for anything
   non-trivial? Recommendation: keep both, like `kubectl`.
2. **Inventory CRUD vs. declarative.** Should `inventory put` be a
   full replacement, or do we add `inventory add/remove`? v1: full
   replace; partial later.
3. **Context format.** Borrow `kubeconfig` semantics outright? Probably
   yes; it's familiar.
4. **Streaming RPCs in `table` mode.** For `dpu status`, table mode
   needs to render incrementally — what's the best tabwriter pattern?

---

## 13. Phased milestones

### M1 — minimal viable CLI (with dashd M1)

- `dial` + persistent flags
- `inventory put` / `inventory get`
- `vnet put` / `vnet get` / `vnet list` / `vnet delete`
- `version`

**Exit criterion**: a user can declare an inventory and a VNET against a
running `dashd` M1 and read it back.

### M2 — full policy CRUD + reconciliation visibility

- `eni`, `mapping`, `acl`, `route` subcommand groups
- generic `apply -f`, `get`, `delete`, `list`
- `dpu list`, `dpu status` (streaming), `dpu drift`
- `reconcile`

**Exit criterion**: an operator can drive a fleet end-to-end from
`dashctl` alone, including triggering and observing reconciliation.

### M3 — production polish

- `kubeconfig`-style `~/.dashctl/config.yaml` contexts
- TLS / mTLS / token auth
- shell completion
- Dockerfile (multi-stage distroless)
- `dashctl completion <shell>` subcommand

**Exit criterion**: production users can switch between dev/staging/prod
controllers with `--context`, authenticate via tokens, and run inside
CI containers.

---

> Update this LLD as `dashcenter.v1` is finalized — every column in §5/§6
> should align exactly with the RPC names in
> [LLD/dashd.md § 5](dashd.md#5-dashcenterv1-proposed-proto-surface).
