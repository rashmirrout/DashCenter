# DashCenter — Run + Test Guide (Phases 1, 2, 3)

This guide covers all three DashCenter binaries:

| Binary | Role | Storage |
|---|---|---|
| `dash-sim` | Behavioural DASH-DPU simulator (full packet pipeline) | in-memory |
| `dash-redis-adapter` | Same `dashapi.v1.DashApi` over SONiC APP_DB | Redis (real or embedded miniredis) |
| `dash-sim-client` | Operator CLI — works against either backend unchanged | n/a |

All three speak the same wire contract: `dashapi.v1.DashApi` over upstream
`sonic-net/sonic-dash-api` proto types, vendored under
`proto/vendor/sonic-dash-api/` (pinned commit in `proto/vendor/sonic-dash-api/VERSION`).

---

## 1. Toolchain (Windows, per session)

```powershell
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"
$env:GOBIN="$env:USERPROFILE\go\bin"
```

## 2. Regenerate protos (only when upstream changes)

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter
pwsh -File .\scripts\vendor-protos.ps1
pwsh -File .\scripts\codegen-go.ps1
```

## 3. Build all three binaries

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null
go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter
```

## 4. Test suite

```powershell
go test .\dash-sim\... .\dash-redis-adapter\...
```

Pipeline conformance: 8 test cases covering outbound ENCAP, route DIRECT, route DROP, ACL deny, disabled-ENI drop, missing-route drop, inbound deliver, inbound mac lookup, inbound no-rule drop.

Redis adapter conformance: 5 test cases covering Apply/Get/Delete/Update round-trip, ordered List, Subscribe snapshot+live, ENI bytes round-trip, SimulatePacket-Unimplemented.

---

## 5. Run dash-sim (in-memory backend, with behavioural pipeline)

```powershell
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8080 --device-id dpu-sim-01
```

Optional preload:

```powershell
.\bin\dash-sim.exe --scenario .\dash-sim\testdata\scenarios\small.yaml
```

## 6. Run dash-redis-adapter (Redis APP_DB backend)

Self-contained demo (no Redis required — uses embedded miniredis):

```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --embedded-redis
```

Against real Redis:

```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --redis localhost:6379 --redis-db 0
```

Wire format (matches SONiC orchagent expectations):

```
Key:   "DASH_<KIND>_TABLE:<joined-key>"   e.g. "DASH_VNET_TABLE:vnet-prod"
Value: HASH { pb: <binary protobuf>, meta: <json {created_ts_ns, updated_ts_ns}> }
```

Subscribe uses Redis Pub/Sub on channel `dashapi.events` (no keyspace
notifications needed).

`SimulatePacket` returns `code = Unimplemented` against the Redis backend —
Redis APP_DB has no behavioural pipeline. Use `dash-sim` for packet
simulations.

## 7. CLI works against either backend

The same `dash-sim-client.exe` drives both. Switch with `--target`.

```powershell
$c = ".\bin\dash-sim-client.exe"

# Against dash-sim:
& $c --target localhost:50051 kinds -o table
& $c --target localhost:50051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'

# Against dash-redis-adapter:
& $c --target localhost:52051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:52051 list  --kind vnet -o table
```

## 8. Pipeline (Phase 2) — SimulatePacket

Only `dash-sim` implements this. Walks
**direction → ENI → ACL_OUT/IN stages 1..5 → route LPM / route_rule →
vnet_mapping encap / service_tunnel / appliance → counter ticks**.

```powershell
# Outbound encap (after applying the prerequisite vnet_mapping):
.\bin\dash-sim-client.exe --target localhost:50051 simulate `
  --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 80 --trace

# Inbound deliver (route_rule lookup + ACL_IN):
.\bin\dash-sim-client.exe --target localhost:50051 simulate `
  --direction inbound --eni eni-001 --vni 1001 `
  --src-ip 100.64.0.5 --dst-ip 10.0.0.4 --trace
```

The Decision returned includes `action`, `reason`, `out_eni`,
`out_underlay_ip`, `out_vni`, `out_routing_type`, `matched_acl_stage`,
`matched_acl_priority`, `matched_route_prefix`, and (with `--trace`) a
per-step pipeline trace.

## 9. Admin HTTP (dash-sim only)

```powershell
Invoke-RestMethod http://localhost:8080/admin/health     | ConvertTo-Json -Depth 5
Invoke-RestMethod http://localhost:8080/admin/kinds      | ConvertTo-Json -Depth 3
Invoke-RestMethod http://localhost:8080/admin/dump       | ConvertTo-Json -Depth 6

# Inject a one-shot Apply failure
$body = @{ op = "Apply"; mode = "error"; count = 1; message = "injected" } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/faults -Body $body -ContentType application/json

# Preload a scenario at runtime
$body = @{ path = "...\testdata\scenarios\small.yaml"; reset = $true } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/scenario -Body $body -ContentType application/json
```

## 10. Object kinds

```powershell
.\bin\dash-sim-client.exe kinds -o table
```

29 upstream DASH kinds: appliance, vnet, eni, eni_route, acl_group, acl_rule,
acl_in, acl_out, route, route_group, route_rule, route_type,
routing_appliance, prefix_tag, vnet_mapping, tunnel, pa_validation, qos,
meter, meter_policy, meter_rule, outbound_port_map, outbound_port_map_range,
ha_scope, ha_scope_config, ha_scope_state, ha_set, ha_set_config,
ha_set_state.

## 11. Troubleshooting

| Symptom | Fix |
|---|---|
| `bind: ... forbidden by its access permissions` | Pick a port not in Windows reserved ranges (e.g. 50051, 52051 work; 50061 is often reserved). |
| `apply: kind X expects N key parts ...` | Joined `--key` doesn't have N `:`-separated parts. Run `kinds` to see KEY_PARTS. |
| `decode value: unknown field` | YAML field name doesn't match upstream protojson; consult the generated `.pb.go` file for that kind. |
| `SimulatePacket ... Unimplemented` | You're talking to `dash-redis-adapter` — use `dash-sim` for the pipeline. |
| Adapter `connection refused` | Make sure Redis is up at `--redis <addr>`, or use `--embedded-redis` for a self-contained demo. |
