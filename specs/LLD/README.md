# DashCenter — Low-Level Design (LLD)

This folder contains the **module-level low-level design** documents for the
DashCenter implementation. Each LLD is a textbook-style, self-contained
description of one binary: data structures, algorithms, concurrency model,
RPC handlers, error paths, extension points, and pseudocode in **both Go
and Rust** so the design can be re-implemented in either language.

## Reading order

1. [dash-sim.md](dash-sim.md) — behavioural DASH-DPU simulator (the
   reference implementation of `dashapi.v1.DashApi`, including the full
   `SimulatePacket` pipeline).
2. [dash-redis-adapter.md](dash-redis-adapter.md) — SONiC-compatible Redis
   APP_DB backend exposing the same `DashApi`.
3. [dash-sim-client.md](dash-sim-client.md) — transport-only operator CLI
   plus embeddable Go SDK.
4. [dashd.md](dashd.md) — **DRAFT** — Phase 4 fleet controller daemon.
5. [dashctl.md](dashctl.md) — **DRAFT** — Phase 4 controller-facing CLI.

## Conventions used in every LLD

| Convention | Meaning |
|---|---|
| `dashapi.v1` | Our service envelope, see [`proto/dashapi/v1/dashapi.proto`](../../proto/dashapi/v1/dashapi.proto). |
| `dash.<area>` | Upstream sonic-net/sonic-dash-api package, vendored under [`proto/vendor/sonic-dash-api/`](../../proto/vendor/sonic-dash-api/). |
| Pseudocode blocks | Side-by-side Go / Rust where it clarifies algorithm intent. |
| `kinds` | The shared per-kind registry in [`src/impl-go/dashapi-runtime/kinds`](../../src/impl-go/dashapi-runtime/kinds/kinds.go). |
| `<KIND>` in tables | The upper-snake-case name used as the SONiC APP_DB table prefix (`DASH_<KIND>_TABLE`). |

## Upstream traceability

Every behavioural decision is traceable to an upstream document. Two anchors:

- The upstream **proto schemas** under
  [sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api/tree/master/proto)
  are vendored verbatim — we never reshape them.
- The upstream **behavioural HLDs** under
  [sonic-net/DASH/documentation](https://github.com/sonic-net/DASH/tree/main/documentation)
  drive the pipeline semantics implemented in
  [dash-sim.md § 9 Pipeline](dash-sim.md#9-pipeline-behavioural-model).

When upstream changes (new fields, new HA semantics, new metering rules),
the change-control workflow is:

1. Re-vendor with [`scripts/vendor-protos.ps1`](../../scripts/vendor-protos.ps1).
2. Re-generate Go stubs with
   [`scripts/codegen-go.ps1`](../../scripts/codegen-go.ps1).
3. Update [`kinds.go`](../../src/impl-go/dashapi-runtime/kinds/kinds.go) if
   new ObjectKinds were added.
4. Update the relevant LLD section here.
5. Update tests and the
   [tutorial](../../docs/tutorial/README.md).

See [`docs/roadmap.md`](../../docs/roadmap.md) for what is implemented
today vs. what is pending.
