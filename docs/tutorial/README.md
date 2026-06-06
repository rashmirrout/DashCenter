# DashCenter Tutorial

> If you're new, start with [**00-how-to.md**](00-how-to.md). It picks the
> right reading path based on what you want to do.

> **🚀 Impatient? Get a 3-DPU fleet running in 5 minutes:**
>
> 1. Install Go + Docker Desktop (see [03 — Build setup](03-build-setup.md)).
> 2. `cd <repo>/src/impl-go && go build -o bin\dash-sim.exe ./dash-sim/cmd/dash-sim && go build -o bin\dash-sim-client.exe ./dash-sim-client/cmd/dash-sim-client && go build -o bin\dash-redis-adapter.exe ./dash-redis-adapter/cmd/dash-redis-adapter`
> 3. `cd ../../deploy/test-setup && Copy-Item fleet.example.json fleet.json`
> 4. `pwsh -File .\01-host-multi-port\start-fleet.ps1` → 3 sims + 1 adapter come up on ports 50051/50052/50053/52051.
> 5. `..\..\src\impl-go\bin\dash-sim-client.exe --target 127.0.0.1:50051 list --kind vnet -o table`
> 6. Done exploring? `pwsh -File .\01-host-multi-port\stop-fleet.ps1`
>
> Full context + bash version + topology choices in
> [**09 — Multi-DPU test infra**](09-multi-dpu-test-infra.md).

## Page index

| # | Page | What you'll learn |
|---|---|---|
| 0 | [How to use this tutorial](00-how-to.md) | Navigation, three personas, project elevator pitch |
| 1 | [Project structure](01-project-structure.md) | Every folder, why it's there, when you'll touch it |
| 2 | [Modules](02-modules.md) | All 8 Go modules; deps, status, use cases |
| 3 | [Build setup](03-build-setup.md) | Install Go, protoc, plugins on Windows/Linux + verify script |
| 4 | [Build](04-build.md) | Compile every binary; codegen workflow |
| 5 | [Run](05-run.md) | Start the simulator and the adapter; common runtime patterns |
| 6 | [Test](06-test.md) | Unit + integration + pipeline conformance tests |
| 7 | [dash-sim-client CLI](07-dash-sim-client.md) | Tutorial-style CLI summary + tips |
| 8 | [Docker Compose](08-docker-compose.md) | Containerized fleet; CLI-in-container |
| 9 | [Multi-DPU test infra](09-multi-dpu-test-infra.md) | One config, three topologies, N DPUs; hands-on walkthroughs |

### Module deep dives ([modules/](modules/))

| Module | Page |
|---|---|
| `proto/dashapi/v1` | [proto-dashapi.md](modules/proto-dashapi.md) |
| `gen/go` | [gen-go.md](modules/gen-go.md) |
| `dashapi-runtime` | [dashapi-runtime.md](modules/dashapi-runtime.md) |
| `dash-sim` | [dash-sim.md](modules/dash-sim.md) |
| `dash-sim-client` | [dash-sim-client.md](modules/dash-sim-client.md) |
| `dash-redis-adapter` | [dash-redis-adapter.md](modules/dash-redis-adapter.md) |
| `dashd` (placeholder) | [dashd.md](modules/dashd.md) |
| `dashctl` (placeholder) | [dashctl.md](modules/dashctl.md) |

### Reference (lives next door, not under `tutorial/`)

- [**docs/CLI_GUIDE.md**](../CLI_GUIDE.md) — every CLI subcommand with
  copy-paste input and real output. The canonical reference.

### Helper scripts

- [`scripts/install-check.ps1`](scripts/install-check.ps1) — verify
  toolchain (Windows).
- [`scripts/install-check.sh`](scripts/install-check.sh) — verify toolchain
  (Linux).
