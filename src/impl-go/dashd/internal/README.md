# `internal/` — dashd subsystems (scaffold)

Each subdirectory has a `doc.go` describing its purpose. Implementations land
in this order (see [README](../README.md)):

1. `store/`        — Redis client + key prefixes
2. `ingest/`       — per-appliance gRPC workers
3. `normalize/`    — protobuf → Redis schema
4. `api/grpc/`     — front door (server)
5. `api/rest/`     — REST gateway
6. `read/`         — cache reads
7. `write/`        — validate→stage→commit
8. `events/`       — Redis Streams bus
9. `invalidate/`   — compute-cache invalidator
10. `compute/`     — ACL/route/trace
11. `reconcile/`   — drift correction
12. `inventory/`   — appliance discovery
13. `telemetry/`   — OTel + Prom
14. `cluster/`     — Raft + memberlist (Model 2)
15. `api/ws/`      — WebSocket watch stream

`config/` is loaded by `cmd/dashd/main.go` once it stops being a stub.
