# `proto/dashcenter/v1/` — dashd northbound API

The gRPC contract that `dashctl`, the Web Console, and 3rd-party SDKs use to
talk to `dashd`. The same `.proto` files generate Go server stubs (for dashd)
and Go / Rust client stubs (for dashctl and external tooling).

> Relationship to `dashapi.v1`: `dashapi.v1` is the SOUTHBOUND contract dashd
> uses to talk to each DPU agent (dash-sim, dash-redis-adapter, real DPUs).
> `dashcenter.v1` (this directory) is the NORTHBOUND contract operators use
> to talk to dashd. dashd translates northbound intent into per-DPU
> dashapi.v1 calls via its placement function and 5-stage write pipeline.

## File map

| File | Service(s) | Purpose |
|---|---|---|
| [`types.proto`](./types.proto) | (shared types) | DPU identity, state, capacity, capabilities, inventory, generic envelopes (Ack, NameRef, KindFilter), FlowTableStats, CounterReport (30+ counters), AuditEntry. |
| [`control_plane.proto`](./control_plane.proto) | `ControlPlane` | Per-kind Put/Delete/Get/List, streamed `ApplyBatch`, unary `SimulateApply` (dry-run), `Reconcile`. Spec messages for Vnet / Eni / VnetMapping / AclPolicy / RoutePolicy / HaSet / ServiceTunnel — all namespace-aware with `expected_generation` for optimistic concurrency. |
| [`observability.proto`](./observability.proto) | `ObservabilityService` | Streamed `GetDpuStatus`, `GetFlowStats`, `GetFlowList`, `GetDrift`, `GetCounters`, `WatchEvents`, `GetAuditLog`. Read-only telemetry. |
| [`diagnostics.proto`](./diagnostics.proto) | `DiagnosticsService` | `TraceFlow`, `ExplainMatch`, `GetAclHitStats` (stream), `ExplainDrift`, `TriggerResimulation`. Fleet-wide computed diagnostics. |
| [`operations.proto`](./operations.proto) | `OperationsService` | `CordonDpu`, `UncordonDpu`, streamed `DrainDpu` with phase-aware `DrainProgress`. |
| [`migration.proto`](./migration.proto) | `MigrationService` | Full 10-phase ENI live-migration state machine: plan/validate/start/advance/rollback/abort/commit + streamed `StreamMigrationSession` + chunked `ExportMigrationBundle` / `ImportMigrationBundle`. 4 migration strategies. |
| [`ha.proto`](./ha.proto) | `HaService` | HA scope/set status, streamed `TriggerSwitchover` / `TriggerFailover`, `WatchHaEvents`, `GetFlowSyncStats`. 12-state HaScopeRole, 5-state FlowSyncState. |

## Conventions

* **Package**: `dashcenter.v1` everywhere.
* **Go package**: `github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1;dashcenterv1`.
* **Multi-tenancy**: every mutating spec carries a `namespace` field.
* **Concurrency**: every `Put*` accepts an `expected_generation`; mismatches return `FAILED_PRECONDITION`.
* **Streaming**: long-lived watches (`Get*`, `Watch*`, `Stream*`) use server-side keepalives. Clients must be prepared for re-subscription.
* **Enums**: closed (`_UNSPECIFIED = 0`), additive — never reuse a number.
* **Audit**: every mutating call emits one `AuditEntry` (see `types.proto`), surfaced via `ObservabilityService.GetAuditLog`.

## Codegen

The `proto/` tree is auto-discovered by both pipelines, so no codegen-config
change is required when adding files in this directory:

* **buf**: `src/impl-go/codegen/buf/buf.gen.yaml` reads `inputs.directory:
  ../../../proto`.
* **protoc fallback**: `src/impl-go/codegen/protoc/protoc.mk` globs
  `*.proto` recursively under `proto/`.

Generated Go output lives at:

```
src/impl-go/gen/go/dashcenter/v1/
```

## Coverage map vs. the LLD gap list

The audit in `docs/dash-sim-alignment-audit.md` enumerated 21 production
gaps. The proto surface above covers the API-visible portion of every gap:

| Gap (audit ID) | Covered by |
|---|---|
| G-1 Flow table | `observability.GetFlowStats`, `observability.GetFlowList` |
| G-2 HA orchestration | `ha.proto` (full service) |
| G-3 ENI migration | `migration.proto` (full service) |
| G-4 Multi-tenancy | `namespace` on every spec; `KindFilter` |
| G-5 Capacity mgmt | `DpuCapacityLimits` + `DpuCapacityUsage` + `SimulateApply` |
| G-6 `service_tunnel` | `control_plane.PutServiceTunnel` + capability flag |
| G-7 Cross-DPU sagas | `ApplyBatch` (transactional) + `BatchAck.per_dpu` |
| G-8 Cordon / drain | `operations.proto` (full service) |
| G-9 50+ real counters | `CounterReport` (30+ fields, additive) |
| G-10 Flow resim | `diagnostics.TriggerResimulation` + `EniSpec.resimulate_flows` |
| G-11 gNMI shim | (server-side concern; northbound semantics already covered) |
| G-12 Audit log | `AuditEntry` + `observability.GetAuditLog` |
| G-13 Fleet diagnostics | `diagnostics.proto` (TraceFlow, ExplainMatch, AclHits) |
| G-14 Batch + dry-run | `control_plane.ApplyBatch` + `control_plane.SimulateApply` |
| G-15 Webhook / events | `observability.WatchEvents`, `ha.WatchHaEvents` |
| G-16 Schema neg. | `DpuCapabilities.dash_api_schema_version` |
| G-17 DPU capability inventory | `DpuCapabilities` |
| G-18 ECMP | `RouteSpec.ecmp_members` |
| G-19 IPv6 | `DpuCapabilities.ipv6` + addressing is string-typed |
| G-20 fast-path ICMP redirection | `DpuCapabilities.fast_path_icmp_redirection` |
| G-21 trusted VNI | `DpuCapabilities.trusted_vni` |