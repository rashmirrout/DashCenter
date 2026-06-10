# DashCenter Tutorial — Start Here

Welcome. This tutorial is the canonical "first day on DashCenter" reference.
If you're new to the project, read pages in order; if you're returning, jump
to a section by name.

> DashCenter is a distributed control plane for DASH-compliant DPUs. It
> ships a behavioural DPU simulator, a SONiC-APP_DB adapter, and a
> transport-only operator CLI — all speaking the **upstream
> sonic-net/sonic-dash-api proto schemas**. The same CLI works against the
> simulator, the adapter, and (in future) real hardware.

---

## What you'll learn

| # | Page | Time | You will be able to... |
|---|---|---|---|
| 0 | this file | 5 min | Understand what DashCenter is and pick where to go next |
| 1 | [Project structure](01-project-structure.md) | 10 min | Navigate the repo by folder and explain every top-level directory |
| 2 | [Modules](02-modules.md) | 15 min | List every module, its role, its deps, and its use case |
| 3 | [Build setup](03-build-setup.md) | 20 min | Install Go, protoc, plugins on Windows and Linux; verify with a script |
| 4 | [Build](04-build.md) | 10 min | Build every binary from source, both Go (primary) and Rust (placeholder) |
| 5 | [Run](05-run.md) | 10 min | Start the simulator and adapter on Windows and Linux |
| 6 | [Test](06-test.md) | 10 min | Run all unit and conformance tests; understand what they prove |
| 7 | [dash-sim-client CLI](07-dash-sim-client.md) | 20 min | Use every CLI subcommand against the running services |
| 8 | [Docker Compose](08-docker-compose.md) | 15 min | Stand the whole fleet up under Docker on Windows and Linux |
| 9 | [Multi-DPU test infra](09-multi-dpu-test-infra.md) | 15 min | Drive an N-DPU fleet from one config file across three topologies |
| 10 | [dashd build](10-dashd-build.md) | 10 min | Build `dashd` from source on Windows + Linux |
| 11 | [dashd unit tests](11-dashd-unit-tests.md) | 10 min | Run dashd's full unit suite + read the coverage report |
| 12 | [dashd integration tests](12-dashd-integration-tests.md) | 20 min | Drive dashd + dash-sim end-to-end by hand, then via the Go suite |
| 13 | [dashd single-node experiment](13-dashd-single-node.md) | 15 min | Configure dashd, walk every listener and the on-disk store |
| 14 | [dashd in Docker (single-DPU e2e)](14-dashd-docker-e2e.md) | 10 min | Compose-up `dashd-e2e/`, run the 8-step verifier |
| 15 | [dashd fleet (5 DPUs)](15-dashd-fleet.md) | 15 min | Compose-up `dashd-fleet/`, see fan-out + targeted placement |
| 16 | [dashctl quickstart](16-dashctl-quickstart.md) | 20 min | Build and drive the operator CLI; contexts, formats, CAS |
| 17 | [full-fleet experiments](17-full-fleet-experiments.md) | 25 min | Two end-to-end experiments incl. a custom ENI + ACL playground |
|   | [Modules deep-dives](modules/) |   | One markdown per module — internals + extension points |
|   | [Contributing to the tutorial](CONTRIBUTING-TO-TUTORIAL.md) | 5 min | The template and quality bar every new page must meet |

---

## The 60-second overview

```
                        ┌────────────────────────┐
                        │   dash-sim-client      │   (operator CLI; works
                        │   (Cobra; transport-   │   identically against
                        │    only; YOU run this) │   either backend)
                        └───────────┬────────────┘
                                    │  dashapi.v1.DashApi gRPC
                                    │  + upstream sonic-dash-api proto types
                                    ▼
        ┌────────────────────────────┴────────────────────────────┐
        │                                                         │
┌───────▼─────────┐                                  ┌───────────▼────────────┐
│   dash-sim      │                                  │  dash-redis-adapter    │
│ (behavioural    │                                  │ (SONiC-compatible:     │
│  simulator;     │                                  │  writes/reads the same │
│  in-memory;     │                                  │  APP_DB layout DASH    │
│  SimulatePacket │                                  │  orchagent consumes)   │
│  pipeline)      │                                  └───────────┬────────────┘
└─────────────────┘                                              │
                                                                 │
                                                       ┌─────────▼─────────┐
                                                       │   Redis APP_DB    │
                                                       │ (real OR embedded │
                                                       │  miniredis)       │
                                                       └───────────────────┘
```

Every object schema (`Vnet`, `Eni`, `AclRule`, `VnetMapping`, `Route`,
`RouteRule`, `HaSet`, `MeterPolicy`, ... 29 in total) is the
upstream [sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api)
type, vendored verbatim under `proto/vendor/sonic-dash-api/` at the commit
recorded in `proto/vendor/sonic-dash-api/VERSION`. We do **not** invent or
reshape any DASH message — our service envelope (`dashapi.v1.DashApi`) only
adds a generic `Apply / Get / Delete / List / Subscribe / GetCounters /
SimulatePacket` surface on top.

---

## Three personas

Pick one — each has a recommended reading path.

### 🧑‍💻 "I just want to use the CLI"

1. [03 — Build setup](03-build-setup.md) — install toolchain
2. [04 — Build](04-build.md) — `go build`
3. [05 — Run](05-run.md) — start `dash-sim`
4. [07 — CLI](07-dash-sim-client.md) — apply, get, list, subscribe, simulate

### 🔬 "I want to understand the architecture"

1. [01 — Project structure](01-project-structure.md)
2. [02 — Modules](02-modules.md)
3. [modules/dash-sim.md](modules/dash-sim.md) — pipeline internals
4. [modules/dash-redis-adapter.md](modules/dash-redis-adapter.md) — SONiC mapping
5. [modules/proto-dashapi.md](modules/proto-dashapi.md) — wire contract

### 🚢 "I want to deploy a fleet"

1. [03 — Build setup](03-build-setup.md)
2. [08 — Docker Compose](08-docker-compose.md)
3. [07 — CLI](07-dash-sim-client.md) — drive the running fleet
### 🔧 "I'm setting up a multi-DPU test fleet"

1. [03 — Build setup](03-build-setup.md)
2. [09 — Multi-DPU test infra](09-multi-dpu-test-infra.md) — pick a topology
3. The matching hands-on walkthrough in [deploy/test-setup/](../../deploy/test-setup/) — step by step with expected logs
4. [07 — CLI](07-dash-sim-client.md) — drive the fleet

### 🖥️ "I'm a new dashd contributor"

1. [03 — Build setup](03-build-setup.md)
2. [10 — dashd build](10-dashd-build.md) — your first build
3. [11 — dashd unit tests](11-dashd-unit-tests.md) — the bar new code must clear
4. [12 — dashd integration tests](12-dashd-integration-tests.md) — end-to-end by hand
5. [13 — single-node experiment](13-dashd-single-node.md) — the reference for every listener + on-disk file

### 🏛️ "I want to run a full DashCenter fleet"

1. [03 — Build setup](03-build-setup.md)
2. [10 — dashd build](10-dashd-build.md)
3. [14 — dashd in Docker, single-DPU e2e](14-dashd-docker-e2e.md)
4. [15 — dashd fleet (5 DPUs)](15-dashd-fleet.md)
5. [17 — full-fleet experiments](17-full-fleet-experiments.md)

### 🧑‍🔧 "I'm an operator / SRE driving dashctl"

1. [03 — Build setup](03-build-setup.md)
2. [15 — dashd fleet](15-dashd-fleet.md) — stand up the cluster you'll talk to
3. [16 — dashctl quickstart](16-dashctl-quickstart.md)
4. [17 — full-fleet experiments](17-full-fleet-experiments.md)
---

## A note on naming

- The simulator is **dash-sim** (not "dash-shim").
- The CLI is **dash-sim-client**.
- The SONiC adapter is **dash-redis-adapter**.

The word "shim" sometimes appears in older notes — it always means the same
**simulator** binary.

---

## Conventions used in this tutorial

- All shell snippets are runnable on a fresh checkout once the toolchain is
  installed (page 3).
- PowerShell prompts start with `PS>`. Bash prompts start with `$`. When a
  block applies to both, only the command is shown.
- Output blocks are real captures from running the binaries — not invented.
- File references use workspace-relative paths and link directly to the file.
- The string `<repo>` always means
  `C:\WorkSpace\PS\PublicRepo\DashCenter` (Windows) or
  `~/work/DashCenter` (Linux) — wherever you cloned the repo.
