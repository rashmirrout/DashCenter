# DashCenter

> A hardware-free, vendor-neutral control plane for **DASH-compliant DPUs** —
> built on the upstream
> [sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api)
> proto schemas, behaviour-faithful to the HLDs in
> [sonic-net/DASH](https://github.com/sonic-net/DASH).

DashCenter ships a **behavioural DPU simulator**, a **SONiC-compatible
Redis APP_DB adapter**, and a **transport-only operator CLI** that drives
both — and, in the future, real hardware. Same wire contract everywhere.

[![status: phase 1-3 done](https://img.shields.io/badge/status-phase%201%E2%80%933%20done-brightgreen)]()
[![tests: 13 green](https://img.shields.io/badge/tests-13%20green-brightgreen)]()
[![object kinds: 29/29](https://img.shields.io/badge/DASH%20object%20kinds-29%2F29-blue)]()
[![upstream protos: pinned](https://img.shields.io/badge/upstream%20protos-pinned-blue)]()
[![license: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

---

## Table of contents

- [Why DashCenter](#why-dashcenter)
- [What ships today](#what-ships-today)
- [60-second tour (one terminal)](#60-second-tour-one-terminal)
- [Repository structure](#repository-structure)
- [Tutorials](#tutorials)
- [Build & run](#build--run)
- [The simulator (dash-sim)](#the-simulator-dash-sim)
- [The SONiC adapter (dash-redis-adapter)](#the-sonic-adapter-dash-redis-adapter)
- [The operator CLI (dash-sim-client)](#the-operator-cli-dash-sim-client)
- [Deployment](#deployment)
- [Status & roadmap](#status--roadmap)
- [Contributing](#contributing)
- [References](#references)
- [License](#license)

---

## Why DashCenter

Operating DASH-capable SmartNICs / DPUs today is fragmented. Schemas live
in [sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api).
Behavioural semantics live in
[sonic-net/DASH](https://github.com/sonic-net/DASH). State lives in
Redis. There's no hardware-free way to:

- **Iterate on configurations** without a DPU on the desk.
- **Verify a packet's journey** through the 5-stage ACL pipeline + LPM
  routing + `vnet_mapping` encap **before** it touches real traffic.
- **Validate that your declared state produces the exact APP_DB layout**
  SONiC orchagent will consume.
- **Drive a real DPU with the same CLI** you used in your laptop tests.

DashCenter is that missing layer: one wire contract (`dashapi.v1.DashApi`)
over the **upstream** sonic-dash-api types, two reference servers (sim +
Redis), one CLI that talks to both unchanged.

---

## What ships today

| Component | Binary | Role | Status |
|---|---|---|---|
| **dash-sim** | `dash-sim.exe` | Behavioural DPU simulator (in-memory; full DASH packet pipeline via `SimulatePacket`) | ✅ stable |
| **dash-redis-adapter** | `dash-redis-adapter.exe` | Same `DashApi` over a SONiC-compatible Redis APP_DB layout | ✅ stable |
| **dash-sim-client** | `dash-sim-client.exe` | Transport-only Cobra CLI; works against either backend unchanged | ✅ stable |
| **dashapi-runtime/kinds** | Go module | Shared registry for all 29 upstream DASH object kinds | ✅ stable |
| **dashd** | `dashd` (scaffold) | Phase 4 fleet controller daemon | ⏳ DRAFT LLD |
| **dashctl** | `dashctl` (scaffold) | Phase 4 controller CLI | ⏳ DRAFT LLD |
| **impl-rust** | — | Rust parity of the Go implementation | ⏳ planned |

- **29 / 29 upstream sonic-dash-api object kinds** modelled.
- **Behavioural DASH pipeline** — outbound + inbound, 5-stage ACL,
  LPM routing, `vnet_mapping` / `service_tunnel` / `appliance` encap,
  with conformance tests.
- **SONiC-compatible Redis layout** — `DASH_<KIND>_TABLE:<joined-key>`
  with binary-protobuf `pb` field, identical to what DASH orchagent reads.
- **`--embedded-redis` mode** — `dash-redis-adapter` self-contained for
  demos and CI (no external Redis needed).

---

## 60-second tour (one terminal)

```powershell
# 1. Toolchain on PATH (Windows; Linux equivalents in docs/tutorial/03-build-setup.md)
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"

# 2. Build the three binaries
cd src\impl-go
go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter

# 3. Start the sim (in another terminal)
.\bin\dash-sim.exe --scenario .\dash-sim\testdata\scenarios\small.yaml

# 4. Drive it
$c = ".\bin\dash-sim-client.exe"
& $c kinds -o table
& $c apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c list  --kind vnet -o table
& $c simulate --direction outbound --eni eni-001 --src-ip 10.0.0.1 --dst-ip 10.1.0.10 --protocol 6 --dst-port 80 --trace
```

`simulate --trace` walks the packet through ENI lookup → ACL → route LPM →
`vnet_mapping` encap and prints every step. See
[docs/CLI_GUIDE.md](docs/CLI_GUIDE.md) for the full command reference.

---

## Repository structure

```
DashCenter/
├── README.md                              -- this file
├── LICENSE                                -- Apache 2.0
├── docs/
│   ├── CLI_GUIDE.md                       -- canonical CLI reference (copy-paste ready)
│   ├── roadmap.md                         -- what's done, what's next, contributor-pickable
│   └── tutorial/                          -- textbook-style onboarding (12 pages)
│       ├── 00-how-to.md                   -- start here
│       ├── 01-project-structure.md
│       ├── 02-modules.md
│       ├── 03-build-setup.md              -- Win + Linux install + verify script
│       ├── 04-build.md
│       ├── 05-run.md
│       ├── 06-test.md
│       ├── 07-dash-sim-client.md
│       ├── 08-docker-compose.md
│       ├── scripts/install-check.{ps1,sh} -- toolchain verifier
│       └── modules/                       -- per-module deep dives (8 files)
│
├── specs/
│   ├── LLD/                               -- Low-Level Designs (textbook + Go + Rust pseudocode)
│   │   ├── dash-sim.md                    -- behavioural simulator (~830 lines, 18 sections)
│   │   ├── dash-redis-adapter.md          -- SONiC APP_DB backend
│   │   ├── dash-sim-client.md             -- CLI + SDK
│   │   ├── dashd.md                       -- DRAFT — fleet controller
│   │   └── dashctl.md                     -- DRAFT — controller CLI
│   └── *.md                               -- legacy HLDs (informative)
│
├── proto/
│   ├── dashapi/v1/dashapi.proto           -- our gRPC service envelope
│   └── vendor/sonic-dash-api/             -- upstream protos, vendored at pinned commit
│
├── scripts/
│   ├── vendor-protos.ps1                  -- snapshot upstream sonic-dash-api
│   └── codegen-go.ps1                     -- regenerate Go stubs
│
├── src/
│   ├── impl-go/                           -- Go workspace (primary impl)
│   │   ├── go.work                        -- 7 modules
│   │   ├── gen/go/                        -- generated stubs (31 packages)
│   │   ├── dashapi-runtime/kinds/         -- shared kinds registry
│   │   ├── dash-sim/                      -- behavioural simulator
│   │   ├── dash-sim-client/               -- operator CLI
│   │   ├── dash-redis-adapter/            -- SONiC-compatible backend
│   │   ├── dashd/                         -- (placeholder) fleet controller
│   │   └── dashctl/                       -- (placeholder) controller CLI
│   └── impl-rust/                         -- Rust workspace (placeholder)
│
├── deploy/
│   └── compose/                           -- docker-compose for a local fleet
│       ├── docker-compose.yml
│       └── scenarios/
│
├── test/
│   ├── conformance/                       -- planned cross-impl conformance suite
│   └── interop/                           -- planned sim ↔ adapter parity tests
│
└── third_party/                           -- LICENSE notes for vendored code
```

Folder-by-folder explanation:
[docs/tutorial/01-project-structure.md](docs/tutorial/01-project-structure.md).

---

## Tutorials

Textbook-style onboarding lives in [docs/tutorial/](docs/tutorial/). Start
with **[docs/tutorial/00-how-to.md](docs/tutorial/00-how-to.md)** — it
picks a reading path based on your role:

- **"I want to use the CLI"** → Build setup → Build → Run → CLI guide.
- **"I want to understand the architecture"** → Project structure →
  Modules → LLDs.
- **"I want to deploy a fleet"** → Build setup → Docker Compose.

| Page | Purpose |
|---|---|
| [00 — How to use this tutorial](docs/tutorial/00-how-to.md) | Navigation, three personas, elevator pitch |
| [01 — Project structure](docs/tutorial/01-project-structure.md) | Every folder explained |
| [02 — Modules](docs/tutorial/02-modules.md) | All 8 Go modules with roles & deps |
| [03 — Build setup](docs/tutorial/03-build-setup.md) | Windows + Linux install, verify script |
| [04 — Build](docs/tutorial/04-build.md) | Compile all binaries; codegen workflow |
| [05 — Run](docs/tutorial/05-run.md) | Start sim and adapter on either OS |
| [06 — Test](docs/tutorial/06-test.md) | Unit + conformance + smoke tests |
| [07 — dash-sim-client CLI](docs/tutorial/07-dash-sim-client.md) | Tutorial-style summary |
| [08 — Docker Compose](docs/tutorial/08-docker-compose.md) | Containerised fleet |
| [docs/CLI_GUIDE.md](docs/CLI_GUIDE.md) | **Canonical CLI reference — every flag, every command** |

For internal design, every binary has a Low-Level Design under
[specs/LLD/](specs/LLD/) with Go + Rust pseudocode.

---

## Build & run

### Prerequisites

- **Go 1.22+**
- **protoc 25.x**
- **protoc-gen-go v1.34.x** + **protoc-gen-go-grpc v1.5.x**
- (Optional) Docker 24+ for the compose fleet
- (Optional) PowerShell 7+ on Linux to run `.ps1` codegen scripts

Verify your toolchain (Windows or Linux):

```bash
# Windows
pwsh -File docs/tutorial/scripts/install-check.ps1

# Linux / macOS
bash docs/tutorial/scripts/install-check.sh
```

Detailed install:
[03 — Build setup](docs/tutorial/03-build-setup.md).

### Build

```bash
cd src/impl-go
go build -o bin/dash-sim           ./dash-sim/cmd/dash-sim
go build -o bin/dash-sim-client    ./dash-sim-client/cmd/dash-sim-client
go build -o bin/dash-redis-adapter ./dash-redis-adapter/cmd/dash-redis-adapter
```

(Use `.exe` suffixes on Windows.)

### Test

```bash
go test ./dash-sim/... ./dash-redis-adapter/...
```

13 tests across pipeline conformance + Redis adapter integration. All
green: [06 — Test](docs/tutorial/06-test.md).

---

## The simulator (dash-sim)

A single-process behavioural simulator for one DASH-compliant DPU agent.

- Implements every RPC of `dashapi.v1.DashApi` in-memory.
- Exposes `SimulatePacket` — walks the **full DASH pipeline**: direction
  lookup → ENI gate → ACL stages 1..5 → eni_route → route LPM →
  `vnet_mapping` encap / service-tunnel / routing-appliance / drop /
  direct. 8 conformance tests pin the semantics.
- Ships an **admin HTTP** surface (`/admin/health`, `/admin/dump`,
  `/admin/faults`, `/admin/scenario`, `/admin/counters`, `/admin/kinds`).
- Loads YAML scenarios so deterministic fixtures can be replayed.
- Fault injection by RPC name (`Apply`, `Get`, ...) with `error` /
  `delay` / `drop` modes.

Run it:

```bash
./bin/dash-sim --grpc-listen :50051 --admin-listen :8080 \
               --scenario ./dash-sim/testdata/scenarios/small.yaml
```

Deep dive: [specs/LLD/dash-sim.md](specs/LLD/dash-sim.md) — architecture,
every internal package, every RPC, the full pipeline algorithm, Rust
pseudocode parity, failure modes, extension recipes.

---

## The SONiC adapter (dash-redis-adapter)

Same `dashapi.v1.DashApi` service — but stored in **Redis** in the exact
APP_DB layout SONiC's DASH orchagent reads.

| Item | Format |
|---|---|
| Redis key | `DASH_<KIND>_TABLE:<joined-key>` (e.g. `DASH_VNET_MAPPING_TABLE:vnet-prod:10.0.0.10`) |
| Redis value | HASH `{ pb: <binary protobuf>, meta: <json timestamps> }` |
| Subscribe stream | Pub/Sub channel `dashapi.events` |
| SimulatePacket | returns `Unimplemented` — use `dash-sim` for pipeline simulation |

Self-contained demo (no external Redis):

```bash
./bin/dash-redis-adapter --grpc-listen :52051 --embedded-redis
```

Against a real Redis:

```bash
./bin/dash-redis-adapter --grpc-listen :52051 --redis localhost:6379
```

Deep dive: [specs/LLD/dash-redis-adapter.md](specs/LLD/dash-redis-adapter.md)
— APP_DB wire format, every RPC's Redis sequence, Pub/Sub algorithm,
per-kind table mapping for all 29 kinds, SONiC-orchagent compatibility
checklist.

---

## The operator CLI (dash-sim-client)

Transport-only Cobra CLI. Talks to **any** `dashapi.v1.DashApi` server —
the sim, the adapter, or a future real-hardware agent. Switch backends
with `--target`; nothing else changes.

```bash
$c = "./bin/dash-sim-client"

& $c kinds -o table                                # list 29 supported kinds
& $c ping                                          # connectivity check
& $c apply  --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c apply  -f scenario.yaml                       # multi-doc YAML stream
& $c get    --kind vnet --key vnet-prod -o yaml
& $c list   --kind vnet -o table
& $c subscribe --snapshot --kinds vnet,eni        # live event stream
& $c counters --kind eni --key eni-001 -o table
& $c simulate --direction outbound --eni eni-001 \
              --src-ip 10.0.0.1 --dst-ip 10.1.0.10 --trace
& $c delete --kind vnet --key vnet-prod
```

Canonical reference: [docs/CLI_GUIDE.md](docs/CLI_GUIDE.md). Tutorial:
[docs/tutorial/07-dash-sim-client.md](docs/tutorial/07-dash-sim-client.md).
Internals: [specs/LLD/dash-sim-client.md](specs/LLD/dash-sim-client.md).

---

## Deployment

### Local bare-metal

```bash
./bin/dash-sim --grpc-listen :50051 &
./bin/dash-redis-adapter --grpc-listen :52051 --embedded-redis &
./bin/dash-sim-client --target localhost:50051 ping
./bin/dash-sim-client --target localhost:52051 ping
```

### Docker Compose (Windows + Linux)

```bash
cd deploy/compose
docker compose up -d redis dash-sim dash-redis-adapter

# Drive the fleet from a one-shot CLI container — no local Go needed:
docker compose run --rm cli kinds -o table
docker compose run --rm cli apply --kind vnet --key vnet-prod --value '{"vni":1001}'
docker compose run --rm cli --target dash-redis-adapter:52051 list --kind vnet -o table
```

Full guide: [docs/tutorial/08-docker-compose.md](docs/tutorial/08-docker-compose.md).

Scale the sim fleet:
```bash
docker compose up -d --scale dash-sim=3
```

### Kubernetes / Helm

Planned. See [roadmap.md § 3.4](docs/roadmap.md#34-build--release).

---

## Status & roadmap

| Phase | Theme | Status |
|---|---|---|
| 1 | Schema parity with upstream sonic-dash-api (all 29 object kinds) | ✅ Done |
| 2 | Behavioural DASH packet pipeline (direction → ACL → route → encap) | ✅ Done |
| 3 | SONiC-compatible Redis APP_DB backend | ✅ Done |
| 4 | Fleet controller (`dashd`) + controller CLI (`dashctl`) | ⏳ DRAFT LLD |
| 5 | Rust parity workspace | ⏳ Planned |
| 6 | Production hardening (TLS, auth, observability, persistence) | 🟡 Partial |
| 7 | Behavioural parity uplift (HA state machine, metering, ECMP, PA validation, prefix tags, port maps, flow tables, ...) | 🟡 Partial |
| 8 | gNMI alternative front-end | ⏳ Planned |

The full breakdown — per-module status, every gap explicitly named, every
upstream HLD mapped — lives in **[docs/roadmap.md](docs/roadmap.md)**.
That document is the single source of truth for "what's left to do".

---

## Contributing

> **🆘 = good first issue.** The roadmap pages list 30+ self-contained
> tasks tagged this way. Each one is bounded, well-scoped, and unblocks
> downstream work.

### Why contribute?

- **You're working at the schema source.** Every line of DashCenter code
  is built against the official upstream sonic-dash-api proto types. No
  forks, no reshaping — your changes flow back into hardware-relevant
  workflows.
- **Your PR ships end-to-end.** With one wire contract across simulator,
  Redis backend, and CLI, even small features land visibly across the
  whole stack.
- **Open architecture, open vision.** Phases 4–8 are intentionally open
  for community design. The LLDs in [`specs/LLD/`](specs/LLD/) name what
  needs to be designed, not what has been decided.
- **Test-first culture.** All current features ship with conformance
  tests. New features are expected to do the same — and the existing
  fixtures (miniredis + bufconn + per-pipeline tests) make this
  inexpensive.
- **DASH is the future.** As SmartNIC/DPU offload becomes the standard
  for cloud-scale networking, DASH ecosystem tooling matters. Be part of
  the layer that makes it operable.

### Suggested first PRs (from the roadmap)

| # | Task | Module | Effort |
|---|---|---|---|
| 1 | `codegen-go.sh` — bash twin of the PowerShell codegen script | `scripts/` | ~30 min |
| 2 | Round-trip unit tests for every kind in `dashapi-runtime/kinds` | shared | ~1 hour |
| 3 | Cobra shell completion (`dash-sim-client completion <shell>`) | CLI | ~30 min |
| 4 | `apply --dry-run` flag — validate inputs without dialling the server | CLI | ~1 hour |
| 5 | Multi-doc YAML support in `scenarios.LoadFile` | dash-sim | ~1 hour |
| 6 | `/admin/faults` parity in `dash-redis-adapter` (copy `dash-sim`'s `faults` package) | adapter | ~2 hours |
| 7 | Cross-backend parity test under `test/interop/` | tests | ~3 hours |
| 8 | GitHub Actions CI (build + test + lint matrix on Linux/Windows) | infra | ~3 hours |
| 9 | Prefix-tag (`src_tag` / `dst_tag`) resolution in pipeline ACLs | dash-sim | ~4 hours |
| 10 | `gen-rust/` skeleton via `tonic-build` | Rust workspace | ~3 hours |
| 11 | Mermaid sequence diagrams in each LLD | docs | ~2 hours |

Big-ticket pickups (multi-PR):

- **Phase 4 — `dashd` M1** (single-node skeleton): see
  [specs/LLD/dashd.md § 24 milestones](specs/LLD/dashd.md#24-phased-implementation-milestones).
- **Phase 5 — Rust mirror of `dash-sim`** using the Rust pseudocode in
  [LLD/dash-sim.md § 15](specs/LLD/dash-sim.md#15-rust-pseudocode-parity).
- **Phase 7 — PrivateLink / PrivateLinkNSG routing paths** —
  upstream-documented flow not yet implemented in the simulator.

### How to contribute

1. Pick an item (or open an issue describing yours). Reference the
   roadmap row in the issue title:
   `[roadmap §x.x] <one-line-description>`.
2. Read the relevant LLD section. Every PR is expected to align with the
   design unless the PR is *itself* an LLD update.
3. Add or update tests:
   - Pipeline change → add a case in
     [`dash-sim/internal/sim/pipeline/pipeline_test.go`](src/impl-go/dash-sim/internal/sim/pipeline/pipeline_test.go).
   - Adapter change → add a case in
     [`dash-redis-adapter/internal/adapter/server_test.go`](src/impl-go/dash-redis-adapter/internal/adapter/server_test.go).
   - CLI change → add a golden-file test (one is on the roadmap too).
4. Update docs:
   - User-visible change → [`docs/CLI_GUIDE.md`](docs/CLI_GUIDE.md) and
     the relevant tutorial page.
   - Architectural change → the relevant LLD plus
     [`docs/roadmap.md`](docs/roadmap.md).
5. Run the full build + test sweep before opening the PR:
   ```bash
   cd src/impl-go
   go build ./dash-sim/... ./dash-sim-client/... ./dash-redis-adapter/... ./gen/go/... ./dashapi-runtime/...
   go test  ./dash-sim/... ./dash-redis-adapter/...
   ```

### Code style

- Go: idiomatic. `gofmt -s`. `go vet` clean. No new dependencies without a
  one-paragraph justification in the PR description.
- Proto: vendored upstream is **read-only** — modifications go upstream
  first and re-vendor via the script.
- Docs: keep `docs/CLI_GUIDE.md` outputs **real captures** from running
  binaries. Never hand-edit example outputs.
- Tests: prefer fixture-driven (`testdata/`) over hand-written setup.

### Wanted

Beyond the roadmap items, we'd love contributions in:

- **DASH adapters for other SmartNIC vendors** — anything that implements
  `dashapi.v1.DashApi` over a vendor-specific control surface.
- **Conformance suite** — make `test/conformance/` runnable against any
  DashApi server with `--target host:port`.
- **Web UI** — a thin Vue/React frontend over `dashcenter.v1` (when
  that proto lands in Phase 4).
- **Real-world scenario YAMLs** — anonymised production-shaped configs
  that exercise corner cases. Add under `dash-sim/testdata/scenarios/`.

### Code of conduct

This project follows the
[Contributor Covenant v2.1](https://www.contributor-covenant.org/version/2/1/code_of_conduct/).
Be welcoming. Be precise. Disagree on technical merits, not on people.

---

## References

### Upstream DASH

- **[sonic-net/DASH](https://github.com/sonic-net/DASH)** — the DASH
  project itself: HLDs, behavioural model, P4 reference pipeline, SAI
  extensions.
  - [documentation/](https://github.com/sonic-net/DASH/tree/main/documentation)
    — the HLDs that drive `dash-sim`'s packet pipeline (ACL stages,
    routing, vnet mapping, PrivateLink, HA, metering).
- **[sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api/tree/master/proto)**
  — the **proto schemas** vendored in `proto/vendor/sonic-dash-api/`.
  Every `dashapi.v1.Object` payload is one of these messages, verbatim.
- **[sonic-net/sonic-swss](https://github.com/sonic-net/sonic-swss)** —
  the orchagent that reads `DASH_<KIND>_TABLE:*` keys from APP_DB.
  `dash-redis-adapter`'s wire format is designed to match what orchagent
  expects.
- **[SAI DASH headers](https://github.com/opencomputeproject/SAI/tree/master/inc)**
  — the underlying hardware abstraction. DashCenter does not call SAI
  directly; orchagent does.

### Ecosystem & related

- **[SONiC](https://github.com/sonic-net/SONiC)** — the network OS
  whose APP_DB layout we target.
- **[gRPC](https://grpc.io/)** + **[protobuf](https://protobuf.dev/)** —
  transport + schema.
- **[redis/go-redis](https://github.com/redis/go-redis)** — Redis client
  in the adapter.
- **[spf13/cobra](https://github.com/spf13/cobra)** — CLI framework for
  `dash-sim-client`.
- **[alicebob/miniredis](https://github.com/alicebob/miniredis)** —
  in-process Redis for embedded mode and tests.

### DashCenter docs

- [docs/tutorial/](docs/tutorial/) — onboarding tutorials.
- [docs/CLI_GUIDE.md](docs/CLI_GUIDE.md) — canonical CLI reference.
- [docs/roadmap.md](docs/roadmap.md) — contributor-pickable roadmap.
- [specs/LLD/](specs/LLD/) — low-level designs.
- [specs/](specs/) — informative legacy HLDs.

---

## License

[Apache 2.0](LICENSE).

DashCenter vendors a snapshot of
[sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api)
under `proto/vendor/sonic-dash-api/`. See
[third_party/sonic-dash-api/LICENSE-NOTE.md](third_party/sonic-dash-api/LICENSE-NOTE.md)
for upstream license notice.

---

> *DashCenter exists because a software-defined network deserves
> software-defined operations. Help us build the open layer that makes
> DASH-capable hardware operable everywhere.*
