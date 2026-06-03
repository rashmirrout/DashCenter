# `dashd` — DashCenter daemon

The long-running process that aggregates DPU state into Redis Stack, serves
the operator API, and (in Model 2) participates in the Raft/SWIM cluster.

## Build

```powershell
# From src/impl-go/
make all                # builds bin/dashd
./bin/dashd --help
```

## Internal packages

```
internal/
├── api/{rest,grpc,ws}/      External API surface (operator-facing)
├── inventory/               appliances.yaml loader + Register service
├── ingest/                  Per-appliance worker pool (gRPC/gNMI clients)
├── normalize/               Protobuf -> Redis schema translation
├── store/                   go-redis wrapper + key prefixes + indexes
├── read/                    Read Engine (cache + --live bypass)
├── write/                   Write Engine (validate -> stage -> commit)
├── compute/                 ACL match / route resolve / trace
├── reconcile/               Periodic drift correction
├── events/                  Redis Streams event bus
├── invalidate/              compute:* invalidator
├── cluster/                 Raft + memberlist (build tag: cluster)
├── telemetry/               OTel + Prometheus
└── config/                  Viper config + signal handling
```

## Config

See [configs/dashd.example.yaml](configs/dashd.example.yaml) and
[configs/appliances.example.yaml](configs/appliances.example.yaml).
