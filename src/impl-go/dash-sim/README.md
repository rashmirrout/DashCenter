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
