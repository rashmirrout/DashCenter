# Go implementation of DashCenter

This is the **primary** implementation. Four Go modules tied together by
`go.work`:

| Module               | Binary(s)         | Purpose |
|----------------------|-------------------|---------|
| `dashd/`             | `dashd`           | The daemon: API front door, ingestion, state cache, write engine, compute engine. |
| `dashctl/`           | `dashctl`         | Operator CLI (kubectl-style) against `dashd`. |
| `dash-sim/`          | `dash-sim`        | DPU simulator: gRPC server + admin HTTP + scenarios + fault injection. |
| `dash-sim-client/`   | `dash-sim-client` | Standalone gRPC client for any DASH endpoint (sim or real DPU). Owns the reusable Go SDK in `pkg/client/`. |

`dash-sim-client` is intentionally **separate** from `dash-sim` so it can
talk to any compatible endpoint and can be reused by tests and external
tooling without dragging in the simulator's internals.

## Quick start

```powershell
# 1. Install toolchain (Go, buf, protoc, grpcurl)
../../scripts/bootstrap.ps1

# 2. Vendor upstream protos (one-time + when refreshing)
../../scripts/vendor-protos.ps1

# 3. Generate Go stubs
make protos                        # default: buf
# or
$env:PROTOGEN="protoc"; make protos

# 4. Build every binary
make all

# 5. Run the sim and poke it
./bin/dash-sim --grpc-listen :50051 --admin-listen :8080 &
./bin/dash-sim-client --target localhost:50051 vnet create vnet-prod --vni 1001
./bin/dash-sim-client --target localhost:50051 vnet list
```

## Layout

```
src/impl-go/
├── go.work                    Four-module workspace
├── Makefile                   make protos | make all | make test
├── .golangci.yml
├── codegen/
│   ├── buf/                   Default protobuf pipeline
│   └── protoc/                Fallback protobuf pipeline
├── gen/go/                    Generated stubs (checked in)
├── tools/tools.go             Pinned dev-only Go tools
├── dashd/                     Module 1 — the daemon
├── dashctl/                   Module 2 — the operator CLI (uses dashd's API)
├── dash-sim/                  Module 3 — DPU simulator
└── dash-sim-client/           Module 4 — standalone CLI + SDK for DASH gRPC
```

## Choosing buf vs protoc

```powershell
make protos                      # uses buf (recommended)
$env:PROTOGEN="protoc"; make protos   # uses plain protoc
```

Both pipelines emit identical packages into `gen/go/`, so module code never
needs to know which one ran.
