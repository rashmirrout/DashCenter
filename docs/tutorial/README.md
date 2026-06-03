# DashCenter Tutorial

> If you're new, start with [**00-how-to.md**](00-how-to.md). It picks the
> right reading path based on what you want to do.

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
