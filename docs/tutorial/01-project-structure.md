# 01 — Project Structure

This page walks every top-level folder, explains its **purpose**, and gives
**use cases** for when you'll touch it. The repo follows a strict separation
of *contract* (proto), *generated code* (gen), *implementation* (Go modules),
and *deployment* (Docker, scenarios).

---

## Top-level layout

```
DashCenter/
├── LICENSE
├── README.md                  -- short repo pitch
├── docs/                      -- design + tutorials (you are here)
│   ├── tutorial/              -- this folder
│   └── *.html, *.png          -- design pitch decks
├── proto/                     -- THE CONTRACT (immutable source of truth)
│   ├── vendor/sonic-dash-api/ -- upstream DASH schemas, vendored verbatim
│   └── dashapi/v1/dashapi.proto -- OUR thin service envelope
├── scripts/                   -- vendor + codegen automation (PowerShell + Bash)
├── specs/                     -- design specs (HLDs); informative, not normative
├── src/                       -- ALL implementations
│   ├── impl-go/               -- Go workspace (primary implementation)
│   └── impl-rust/             -- Rust workspace (placeholder; see modules.md)
├── deploy/                    -- containers + compose files
│   └── compose/
│       ├── docker-compose.yml
│       └── scenarios/         -- scenario YAMLs mounted into sim containers
├── test/                      -- cross-binary conformance & interop fixtures
├── third_party/               -- LICENSE notices for vendored upstream code
└── .gitignore
```

---

## `proto/` — the contract

```
proto/
├── README.md                          -- explains the layering
├── cluster/v1/README.md               -- (TBD) future cluster API
├── dashapi/v1/dashapi.proto           -- OUR service envelope
├── dashcenter/v1/README.md            -- (TBD) future control-plane API
└── vendor/
    └── sonic-dash-api/                -- pinned snapshot of upstream
        ├── VERSION                    -- commit SHA + fetch date
        ├── README.md
        ├── *.proto                    -- 30 upstream files
        └── (NO Go imports; pure proto)
```

- **`proto/vendor/sonic-dash-api/`** is the **upstream sonic-net/sonic-dash-api
  repo's `proto/` directory**, fetched and pinned by
  [`scripts/vendor-protos.ps1`](../../scripts/vendor-protos.ps1). Treat it as
  read-only — to upgrade, re-run the vendor script with a new commit and
  commit the diff.
- **`proto/dashapi/v1/dashapi.proto`** is the **gRPC service** that our
  binaries serve. It declares a small set of RPCs (`Apply / Get / Delete /
  List / Subscribe / GetCounters / SimulatePacket`) and an `Object` envelope
  whose `oneof payload` references every upstream message type.

**Use case**: edit `dashapi.proto` when you need a new RPC. Touch
`vendor/sonic-dash-api/` only via the vendor script.

---

## `scripts/` — automation

```
scripts/
├── bootstrap.ps1 / bootstrap.sh         -- one-shot dev env setup (placeholder)
├── vendor-protos.ps1 / vendor-protos.sh -- fetch + pin upstream sonic-dash-api
└── codegen-go.ps1                       -- generate Go stubs from all protos
```

**Use case**: run `codegen-go.ps1` whenever `dashapi.proto` or vendored
protos change. Run `vendor-protos.ps1` to bump the pinned upstream commit.

---

## `specs/` — informative design docs

Long-form design specs (HLD, CLI brief, ENI live-migration design, etc.).
These are **informative** — they pre-date the current implementation and may
not match it 1:1. Always cross-check against code in `src/impl-go/` before
acting on a spec claim.

---

## `src/impl-go/` — the Go implementation

Multi-module Go workspace. The `go.work` file at the top stitches every
module into one development tree.

```
src/impl-go/
├── go.work                    -- multi-module workspace
├── Makefile                   -- top-level build aliases (placeholder)
├── README.md
├── .golangci.yml              -- linter config
├── tools/                     -- tools.go for buf, protoc-gen-go, protoc-gen-go-grpc
├── codegen/                   -- buf + protoc helper makefiles
├── bin/                       -- BUILD OUTPUT (gitignored)
│
├── gen/go/                    -- generated Go stubs from ALL protos
│   ├── go.mod
│   ├── dashapi/v1/            -- our service stubs (server + client)
│   └── dash/                  -- 30 upstream packages
│       ├── vnet/, eni/, acl_rule/, vnet_mapping/, route/, route_rule/,
│       │ acl_in/, acl_out/, route_type/, ha_scope/, ha_set/, ...
│       └── types/             -- IpAddress, IpPrefix, Guid, HaState, ...
│
├── dashapi-runtime/           -- SHARED runtime (kinds registry)
│   ├── go.mod
│   └── kinds/                 -- one place to know every kind
│
├── dash-sim/                  -- behavioural simulator binary
│   ├── go.mod
│   ├── cmd/dash-sim/main.go
│   ├── internal/sim/
│   │   ├── admin/             -- HTTP /admin/* endpoints
│   │   ├── counters/          -- per-key synthetic counters
│   │   ├── events/            -- pub/sub bus for Subscribe RPC
│   │   ├── faults/            -- fault injection
│   │   ├── model/             -- generic in-memory store
│   │   ├── pipeline/          -- BEHAVIOURAL DASH packet pipeline
│   │   ├── scenarios/         -- YAML scenario loader
│   │   └── server/            -- DashApi gRPC service impl
│   ├── testdata/scenarios/    -- example scenario YAMLs
│   ├── Dockerfile
│   └── RUN_AND_TEST.md
│
├── dash-sim-client/           -- operator CLI binary
│   ├── go.mod
│   ├── cmd/dash-sim-client/main.go
│   ├── internal/cmd/          -- Cobra subcommands (apply, get, ...)
│   ├── internal/render/       -- output formatting (json/yaml/table)
│   ├── pkg/client/            -- thin SDK (Dial + RPC wrappers)
│   └── README.md
│
├── dash-redis-adapter/        -- SONiC-compatible Redis backend
│   ├── go.mod
│   ├── cmd/dash-redis-adapter/main.go
│   └── internal/adapter/      -- same DashApi service, Redis-backed
│
├── dashd/                     -- planned controller daemon (PLACEHOLDER)
│   ├── go.mod
│   ├── cmd/dashd/main.go      -- stub
│   ├── internal/, configs/    -- shape only
│   └── Dockerfile
│
└── dashctl/                   -- planned controller CLI (PLACEHOLDER)
    ├── go.mod
    └── cmd/dashctl/main.go    -- stub
```

### Why a multi-module workspace?

| Reason | Detail |
|---|---|
| Independent versioning | Each binary can be released on its own (`dash-sim/go.mod`). |
| Sharp dep boundary | `dash-sim-client` must NOT import `dash-sim/internal/*`. Module separation enforces it at the toolchain layer. |
| Generated code reuse | One `gen/go` module is depended on by every other module via `replace ../gen/go`. Zero duplication. |
| Future polyglot | `impl-rust/` will mirror this layout. |

### Why a `dashapi-runtime` module?

Both `dash-sim` and `dash-redis-adapter` need to switch on the 29 object
kinds (look up the right proto type, pack/unpack the `oneof`, compute the
Redis table name). Putting all per-kind logic in a single shared module
means **adding a new kind is a one-place change**.

---

## `src/impl-rust/` — Rust implementation (placeholder)

```
src/impl-rust/
├── Cargo.toml                 -- workspace root
├── rust-toolchain.toml        -- pinned toolchain
├── .cargo/, codegen/, crates/ -- skeletons
└── crates/README.md           -- documents the planned crate layout
```

Currently a placeholder. The expectation is to mirror `impl-go` (one crate
per binary plus a shared `dashapi-runtime` crate generated from the same
`proto/`). See [modules.md](02-modules.md) for status.

---

## `deploy/compose/` — containers

```
deploy/
└── compose/
    ├── docker-compose.yml     -- redis + dashd + N dash-sims
    └── scenarios/             -- YAMLs mounted into sim containers at runtime
```

**Use case**: stand up a local fleet for integration tests. See
[08 — Docker Compose](08-docker-compose.md).

---

## `test/` — cross-binary fixtures

```
test/
├── conformance/README.md    -- planned: shared conformance suite for any
│                                DashApi server implementation
└── interop/README.md        -- planned: sim ↔ adapter cross-checks
```

In-module Go tests (under `dash-sim/internal/sim/pipeline/`,
`dash-redis-adapter/internal/adapter/`) are the **current** test coverage.
`test/` is the planned home for cross-binary integration suites.

---

## `third_party/` — license notices

Any vendored upstream code that needs an attribution gets a `LICENSE-NOTE.md`
here. Currently lists `sonic-dash-api`.

---

## Anything not in this list

Anything else under the repo is build output (`bin/`, `target/`), editor
metadata (`.vscode/`), or generated and gitignored (`gen/go/dash/*/`).

---

## Where to go next

- See **how the modules interact** → [02 — Modules](02-modules.md)
- Set up your dev environment → [03 — Build setup](03-build-setup.md)
