# `dashd` — DashCenter daemon

The long-running process that orchestrates DASH DPU fleets. dashd accepts
northbound `dashcenter.v1` API calls (gRPC + REST), persists desired state,
discovers DPUs from inventory, and reconciles declared state onto each DPU
via the southbound `dashapi.v1` API.

## Architecture

See the implementation plans for full details:

- **Phase 1 (basic):** [`specs/Impl-Plan/impl-plan-basic.md`](../../specs/Impl-Plan/impl-plan-basic.md)
- **Phase 2 (advanced):** [`specs/Impl-Plan/impl-plan-advanced.md`](../../specs/Impl-Plan/impl-plan-advanced.md)
- **HLD:** [`specs/HLD/dashd-hld.md`](../../specs/HLD/dashd-hld.md)
- **LLD:** [`specs/LLD/dashd-lld.md`](../../specs/LLD/dashd-lld.md)

## Build

```bash
cd src/impl-go/dashd
go build ./cmd/dashd
```

## Run

```bash
./dashd --config configs/dashd.example.yaml
```

## Internal packages

See [`internal/README.md`](internal/README.md) for the full package table.

## Config

- [`configs/dashd.example.yaml`](configs/dashd.example.yaml) — daemon configuration
- [`configs/inventory.example.yaml`](configs/inventory.example.yaml) — DPU inventory