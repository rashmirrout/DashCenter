# Conformance tests

Language-neutral black-box tests that point at **any** running `dash-sim`
binary (Go, Rust, future C#, ...) and validate that it correctly implements
the `proto/dashsim/v1/DashSim` service.

## Usage

```powershell
# Run against a sim already listening on :50051
./run.ps1 --target localhost:50051

# Or spin one up first
docker compose -f ../../deploy/compose/docker-compose.yml up -d dash-sim
./run.ps1 --target localhost:50051
```

## What it checks

* CRUD semantics for VNET, ENI, ACL, route, mapping.
* `Subscribe(snapshot_first=true)` returns every existing object before live events.
* `Ack.txn_id` correlates with the `Event.txn_id` delivered on Subscribe.
* Fault-injection knobs (admin HTTP) produce the documented error codes.
