# `dash-sim` — DPU simulator

Long-running daemon that pretends to be a DASH DPU. Exposes:

- **gRPC** on `:50051` — the `dashsim.v1.DashSim` service defined in
  [proto/dashsim/v1/dashsim.proto](../../../proto/dashsim/v1/dashsim.proto).
- **Admin HTTP** on `:8080` — fault injection, dump, reset, scenario load.

To program / inspect a running simulator, use the separate
[`dash-sim-client`](../dash-sim-client) module.

## Build & run

```powershell
# From src/impl-go/
go build -o bin\dash-sim.exe .\dash-sim\cmd\dash-sim
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8080
```

## Admin HTTP

```powershell
curl http://localhost:8080/admin/health
curl http://localhost:8080/admin/dump
curl -X POST http://localhost:8080/admin/reset
curl -X POST http://localhost:8080/admin/faults `
  -d '{"op":"AddAclRule","mode":"error","count":1,"message":"injected"}'
curl -X POST http://localhost:8080/admin/scenario `
  -d '{"path":"testdata/scenarios/small.yaml"}'
```

> **PE-3a / PE-G8** added a new RPC name `"GetDpuCounters"` that
> participates in the same fault injector. Inject latency / errors on
> the typed per-DPU counter rollup with:
>
> ```powershell
> curl -X POST http://localhost:8080/admin/faults `
>   -d '{"op":"GetDpuCounters","mode":"delay","count":3,"delay_ms":500}'
> ```
>
> Hits 3 successive `GetDpuCounters` calls with a 500ms server-side
> delay then auto-resets. Useful for testing dashd polling resilience
> (PE-3b) and the dash-sim-client `--watch` loop's transient-error
> handling. See [`dash-sim-counter-rollups.md`](../../../docs/dashd-features/dash-sim-counter-rollups.md)
> for the full design + flag reference.

## Layout

```
dash-sim/
├── cmd/dash-sim/main.go              Entry point: flags, signals, wire-up
├── internal/sim/
│   ├── model/                        In-memory DASH object store (sync.RWMutex)
│   ├── server/                       gRPC service implementation
│   ├── events/                       Pub/sub bus powering Subscribe stream
│   ├── faults/                       Per-op fault injection (drop|error|delay)
│   ├── admin/                        HTTP :8080 (health, dump, reset, faults, scenario)
│   ├── scenarios/                    YAML scenario loader
│   └── counters/                     Synthetic per-object counters
└── testdata/scenarios/               Built-in scenarios
```
