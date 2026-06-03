# `dash-sim-client` — standalone gRPC client for DASH endpoints

A small CLI + reusable Go SDK that speaks the `dashsim.v1.DashSim` gRPC
service defined in [proto/dashsim/v1/dashsim.proto](../../../proto/dashsim/v1/dashsim.proto).

**Independent of `dash-sim`.** This module is intentionally a pure client of
the wire contract — it can talk to:

- a local `dash-sim` simulator
- a remote `dash-sim` running on another host
- in the future, any real DPU agent that implements the same gRPC service
- (later) a thin compatibility shim in front of a SAI/gNMI DPU

That's why it lives in its own Go module — it must not pull in any of
`dash-sim`'s internal packages.

## Two things in one module

| Path             | What it is                                          |
|------------------|-----------------------------------------------------|
| `cmd/dash-sim-client/` | The `dash-sim-client` CLI binary              |
| `pkg/client/`    | Reusable Go SDK (also importable by tests & dashd)  |

`pkg/client/` is **public on purpose** so the conformance test harness in
[`test/conformance/`](../../../test/conformance/) and any future Go consumer
can reuse it without copy-paste.

## Build

```powershell
# From src/impl-go/
make all                    # builds bin/dash-sim-client
./bin/dash-sim-client --help
```

## CLI shape (planned — kubectl-style verbs)

```powershell
# Connection
dash-sim-client --target localhost:50051 ...

# CRUD
dash-sim-client vnet create vnet-prod --vni 1001
dash-sim-client vnet get    vnet-prod
dash-sim-client vnet list
dash-sim-client vnet delete vnet-prod

dash-sim-client eni create eni-100 --vnet vnet-prod --mac 00:aa:bb:00:01:00 --addr 10.0.0.4/24
dash-sim-client eni update eni-100 --addr 10.0.0.99/24
dash-sim-client eni list

dash-sim-client acl group  add  acl-default --stage inbound
dash-sim-client acl rule   add  acl-default --num 10 --action allow --src 10.0.0.0/24
dash-sim-client route add --table vnet-prod --dst 10.1.0.0/24 --action forward
dash-sim-client mapping add --vnet vnet-prod --underlay 10.0.0.10

# Streaming
dash-sim-client subscribe --kinds vnet,eni,acl_rule --snapshot

# Counters
dash-sim-client counters get eni-100

# Output modes
dash-sim-client vnet list --output json
dash-sim-client vnet list --output yaml
dash-sim-client vnet list --output wide
```

## Layout

```
dash-sim-client/
├── go.mod
├── cmd/
│   └── dash-sim-client/main.go    Cobra entry point
├── pkg/
│   └── client/                    Reusable SDK
│       ├── client.go              Dial / options / Close
│       ├── vnet.go                CRUD wrappers
│       ├── eni.go
│       ├── acl.go
│       ├── route.go
│       ├── mapping.go
│       ├── subscribe.go           Stream helper with reconnect
│       └── counters.go
└── internal/
    ├── cmd/                       Cobra command definitions
    │   ├── root.go
    │   ├── vnet.go
    │   ├── eni.go
    │   ├── acl.go
    │   ├── route.go
    │   ├── mapping.go
    │   ├── subscribe.go
    │   └── counters.go
    └── render/                    --output json/yaml/wide/table
```
