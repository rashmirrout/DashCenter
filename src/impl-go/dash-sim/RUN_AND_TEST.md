# dash-sim & dash-sim-client — Run + Test Guide (Phase 1)

This guide covers the **upstream-schema** build of DashCenter:

- `dash-sim` is a behavioural simulator implementing the gRPC service
  `dashapi.v1.DashApi`. Every object type comes verbatim from
  [sonic-net/sonic-dash-api](https://github.com/sonic-net/sonic-dash-api)
  (vendored under `proto/vendor/sonic-dash-api/`, pinned commit recorded in
  `proto/vendor/sonic-dash-api/VERSION`).
- `dash-sim-client` is the operator CLI for that service. The SAME binary
  will work, unchanged, against any future server that implements
  `dashapi.v1.DashApi` (planned phase 3: a Redis APP_DB adapter that drives
  real SONiC DASH hardware).

Both binaries live in `src/impl-go/bin/` after a build.

---

## 1. Toolchain (Windows, per shell session)

```powershell
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"
$env:GOBIN="$env:USERPROFILE\go\bin"

go version          # go1.22.x
protoc --version    # libprotoc 25.x
```

---

## 2. Regenerate protos (only when upstream changes)

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter
pwsh -File .\scripts\vendor-protos.ps1     # snapshot upstream into proto/vendor/
pwsh -File .\scripts\codegen-go.ps1        # regenerate src/impl-go/gen/go/
```

The codegen script writes 32 `.pb.go` files: 30 upstream message packages
under `gen/go/dash/<area>/` plus the `dashapi.v1` service envelope.

---

## 3. Build

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null
go build -o bin\dash-sim.exe        .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe .\dash-sim-client\cmd\dash-sim-client
```

---

## 4. Run the simulator

```powershell
.\bin\dash-sim.exe `
  --grpc-listen  :50051 `
  --admin-listen :8080  `
  --device-id    dpu-sim-01
```

Flags:

| Flag             | Default      | Description                                  |
|------------------|--------------|----------------------------------------------|
| `--grpc-listen`  | `:50051`     | DashApi gRPC bind address                    |
| `--admin-listen` | `:8080`      | Admin HTTP bind address                      |
| `--device-id`    | `dpu-sim-01` | Synthetic device id                          |
| `--scenario`     | (none)       | Optional YAML scenario to preload            |
| `--tick-interval`| `1s`         | Per-object counter tick                      |

`Ctrl+C` for graceful shutdown.

### Preload a scenario

```powershell
.\bin\dash-sim.exe --scenario .\dash-sim\testdata\scenarios\small.yaml
```

---

## 5. Drive the CLI

All commands accept `--target host:port` (default `localhost:50051`) and
`-o json|yaml|table` (default `json`).

The CLI is **generic** — every DASH object kind goes through the same
`apply / get / delete / list / subscribe / counters` commands. The complete
list of supported kinds:

```powershell
dash-sim-client kinds -o table
```

Will print something like:

```
NAME                      ENUM                                     KEY_PARTS
appliance                 OBJECT_KIND_APPLIANCE                    appliance_id
vnet                      OBJECT_KIND_VNET                         vnet_name
eni                       OBJECT_KIND_ENI                          eni
eni_route                 OBJECT_KIND_ENI_ROUTE                    eni
acl_group                 OBJECT_KIND_ACL_GROUP                    group_id
acl_rule                  OBJECT_KIND_ACL_RULE                     group_id,rule_num
acl_in                    OBJECT_KIND_ACL_IN                       eni,stage
acl_out                   OBJECT_KIND_ACL_OUT                      eni,stage
route                     OBJECT_KIND_ROUTE                        group_id,prefix
route_group               OBJECT_KIND_ROUTE_GROUP                  group_id
route_rule                OBJECT_KIND_ROUTE_RULE                   eni,vni,prefix_or_tag,priority
route_type                OBJECT_KIND_ROUTE_TYPE                   routing_type
routing_appliance         OBJECT_KIND_ROUTING_APPLIANCE            appliance_id
prefix_tag                OBJECT_KIND_PREFIX_TAG                   tag_name
vnet_mapping              OBJECT_KIND_VNET_MAPPING                 vnet,ip_address
tunnel                    OBJECT_KIND_TUNNEL                       tunnel_name
pa_validation             OBJECT_KIND_PA_VALIDATION                vni
qos                       OBJECT_KIND_QOS                          qos_name
meter                     OBJECT_KIND_METER                        eni,metering_class_id
meter_policy              OBJECT_KIND_METER_POLICY                 meter_policy_id
meter_rule                OBJECT_KIND_METER_RULE                   meter_policy_id,rule_num
outbound_port_map         OBJECT_KIND_OUTBOUND_PORT_MAP            map_id
outbound_port_map_range   OBJECT_KIND_OUTBOUND_PORT_MAP_RANGE      map_id,start_port,end_port
ha_scope                  OBJECT_KIND_HA_SCOPE                     ha_scope_id
ha_scope_config           OBJECT_KIND_HA_SCOPE_CONFIG              vdpu_id,ha_scope_id
ha_scope_state            OBJECT_KIND_HA_SCOPE_STATE               ha_scope_id
ha_set                    OBJECT_KIND_HA_SET                       ha_set_id
ha_set_config             OBJECT_KIND_HA_SET_CONFIG                ha_set_id
ha_set_state              OBJECT_KIND_HA_SET_STATE                 ha_set_id
```

### 5.1 Apply (create or replace)

Composite keys are joined with `:`. Values are JSON using upstream protojson
field names — base64 for bytes, enum string names for enums, network-byte-order
int for `fixed32` IP addresses.

```powershell
dash-sim-client apply --kind vnet --key vnet-prod  --value '{"vni":1001}'
dash-sim-client apply --kind eni  --key eni-001    --value '{
  "eni_id":      "11111111-1111-1111-1111-111111111111",
  "mac_address": "ABEiM0RV",
  "vnet":        "vnet-prod",
  "admin_state": "STATE_ENABLED"
}'
dash-sim-client apply --kind acl_group   --key acl-prod-in      --value '{"ip_version":"IP_VERSION_IPV4"}'
dash-sim-client apply --kind acl_rule    --key 'acl-prod-in:100' --value '{"priority":100,"action":"ACTION_PERMIT","terminating":true}'
dash-sim-client apply --kind acl_in      --key 'eni-001:1'       --value '{"v4_acl_group_id":"acl-prod-in"}'
dash-sim-client apply --kind vnet_mapping --key 'vnet-prod:10.0.0.20' --value '{
  "underlay_ip":   {"ipv4": 343798372},
  "routing_type":  "ROUTING_TYPE_VNET"
}'
```

Or load a YAML file with many entries:

```powershell
dash-sim-client apply -f .\dash-sim\testdata\scenarios\small.yaml
```

### 5.2 Get / List / Delete

```powershell
dash-sim-client list   --kind vnet -o table
dash-sim-client list   --kind eni  -o yaml
dash-sim-client get    --kind eni  --key eni-001
dash-sim-client delete --kind vnet --key vnet-prod
```

### 5.3 Subscribe (snapshot + live)

```powershell
dash-sim-client subscribe --snapshot                       # all kinds
dash-sim-client subscribe --snapshot --kinds vnet,eni      # filtered
```

`Ctrl+C` to stop. Each event prints as a one-line JSON object containing
`{kind, key, value, type, txn_id, server_ts_ns}`.

### 5.4 Counters

```powershell
dash-sim-client counters --kind eni --key eni-001 -o table
```

### 5.5 Ping (connectivity)

```powershell
dash-sim-client ping
```

---

## 6. Admin HTTP API

```powershell
# Health (per-kind sizes, subscribers, dropped events)
Invoke-RestMethod http://localhost:8080/admin/health | ConvertTo-Json -Depth 5

# Full dump of every object across every kind
Invoke-RestMethod http://localhost:8080/admin/dump | ConvertTo-Json -Depth 6

# Reset model
Invoke-RestMethod -Method Post http://localhost:8080/admin/reset

# Load a server-side scenario
$body = @{ path = "...\testdata\scenarios\small.yaml"; reset = $true } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/scenario -Body $body -ContentType application/json

# Inject a one-shot Apply failure
$body = @{ op = "Apply"; mode = "error"; count = 1; message = "injected" } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/faults -Body $body -ContentType application/json

# List / clear faults
Invoke-RestMethod http://localhost:8080/admin/faults
Invoke-RestMethod -Method Delete http://localhost:8080/admin/faults

# List supported kinds (also returns key_parts)
Invoke-RestMethod http://localhost:8080/admin/kinds | ConvertTo-Json -Depth 3
```

Fault op names match DashApi RPCs: `Apply`, `Delete`, `Get`, `List`,
`Subscribe`, `GetCounters`. `*` matches any.

Fault modes:

| Mode    | Behavior                                            |
|---------|-----------------------------------------------------|
| `error` | Return `Ack{accepted:false, error:<message>}`       |
| `drop`  | Alias of `error` with default message `"dropped"`   |
| `delay` | Sleep `delay_ms` then continue normally             |

`count<=0` = infinite, `count=0` defaults to `1` (one-shot).

---

## 7. Scenario YAML reference

A scenario is a YAML doc with `apiVersion`, `kind: Scenario`, `metadata`,
and a `spec` list of `{kind, key, value}` entries. Each `value` follows
upstream protojson rules (enum names, base64 bytes, network-byte-order
fixed32 ints for IP addresses, nested message dicts).

A reference scenario covering 9 different upstream kinds — appliance, vnet,
eni, acl_group, acl_rule, acl_in, vnet_mapping, route_group, route, eni_route —
is at:

```
src/impl-go/dash-sim/testdata/scenarios/small.yaml
```

---

## 8. Phase roadmap

| Phase | Scope                                                                     | Status |
|-------|---------------------------------------------------------------------------|--------|
| 1     | Schema parity (all 29 upstream object kinds + DashApi service)            | done |
| 2     | Behavioral parity per DASH HLDs (5-stage ACL pipeline, routing, metering, HA) | next |
| 3     | Hardware bridge: `dashd-redis-adapter` (writes SONiC APP_DB), `dashd-gnmi-adapter` | follow-up |

When phase 3 lands, **the same `dash-sim-client` binary** will drive real
DASH-compliant DPUs because the wire contract (`dashapi.v1.DashApi` over
upstream proto types) is unchanged.

---

## 9. Troubleshooting

| Symptom                                                            | Fix                                                                |
|--------------------------------------------------------------------|--------------------------------------------------------------------|
| `go: ... not recognized`                                           | Re-run the `$env:PATH=` block in §1.                               |
| `protoc: not found` when running codegen                           | Same — protoc lives at `$env:USERPROFILE\protoc\bin`.              |
| `connection refused` from client                                   | sim isn't running on `--target`; check `Get-NetTCPConnection -LocalPort 50051`. |
| `decode value: ... unknown field` on apply                         | YAML field name doesn't match upstream protojson; run `dash-sim-client kinds` and inspect the generated `.pb.go` for that kind. |
| `kind X expects N key parts ...`                                   | Joined `--key` doesn't have the right `:`-separated count for that kind. See `dash-sim-client kinds`. |
| Subscribe shows no live events                                     | Subscriber buffer full (256). Check `/admin/health` `dropped_events`. |
