# `internal/` — dashd subsystems (Phase 1)

dashd is implemented as 11 internal packages plus the bootstrap in
`cmd/dashd/main.go`. Full plan: [`../../specs/Impl-Plan/impl-plan-basic.md`](../../specs/Impl-Plan/impl-plan-basic.md).

## Phase 1 packages

| Package | Purpose |
|---|---|
| `config/`     | YAML config loader + defaults + flag overrides |
| `store/`      | `DesiredStore` interface |
| `store/file/` | On-disk JSON backend |
| `inventory/`  | DPU registry + liveness prober |
| `model/`      | Domain types: `ObsCache`, `Diff`, `ObjectKey` |
| `placement/`  | Pure function: fleet specs → per-DPU dashapi objects |
| `subscribe/`  | Per-DPU Subscribe pump (observed state ingestion) |
| `dispatch/`   | Per-DPU worker pool (Apply/Delete dispatch) |
| `reconciler/` | Dirty-set manager + tick loop |
| `server/grpc/` | gRPC server (ControlPlane + Observability) |
| `server/rest/` | HTTP REST gateway |
| `server/admin/` | Admin HTTP (health, drift, reconcile) |

## Dependency rules
- `placement/` must remain pure (no I/O, no goroutines, no global state)
- `dispatch/` is the only package that owns `*dashapi.DashApiClient`
- `server/*` packages depend on business packages but never the reverse

## Phase 2 (not in Phase 1)
`store/etcd/`, `ha/leader/`, `ha/orchestrator/`, `namespace/`, `capacity/`,
`schema/`, `migration/`, `operations/`, `auth/`, `audit/`, `flow/`,
`saga/`, `api/gnmi/`. See [`../../specs/Impl-Plan/impl-plan-advanced.md`](../../specs/Impl-Plan/impl-plan-advanced.md).