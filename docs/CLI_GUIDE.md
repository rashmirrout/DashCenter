# DashCenter CLI Guide

End-to-end walkthrough: build the binaries, start a backend, and drive it with
`dash-sim-client`. Every command below was run on Windows PowerShell against
the live binaries in this repo; sample outputs are real.

The CLI speaks the `dashapi.v1.DashApi` gRPC service over the **upstream
sonic-net/sonic-dash-api** proto types. The same `dash-sim-client.exe` works
against both backends:

| Backend | Binary | Storage | Pipeline (SimulatePacket) |
|---|---|---|---|
| Behavioural simulator | `dash-sim` | in-memory | YES |
| SONiC-compatible adapter | `dash-redis-adapter` | Redis APP_DB | NO (returns Unimplemented) |

---

## 1. Prerequisites (Windows, per shell session)

```powershell
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"
$env:GOBIN="$env:USERPROFILE\go\bin"

go version          # go1.22.x
protoc --version    # libprotoc 25.x
```

## 2. Build

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null

go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter
```

To regenerate protos after a `sonic-dash-api` upstream bump:

```powershell
pwsh -File ..\..\scripts\vendor-protos.ps1     # snapshot upstream
pwsh -File ..\..\scripts\codegen-go.ps1        # regenerate src/impl-go/gen/go/
```

## 3. Run unit + integration tests

```powershell
go test .\dash-sim\... .\dash-redis-adapter\...
```

Expected output:
```
ok      github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/pipeline    1.9s
ok      github.com/rashmirrout/DashCenter/src/impl-go/dash-redis-adapter/internal/adapter       2.4s
```

## 4. Start a backend

### Option A — behavioural simulator

```powershell
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8080 --device-id dpu-sim-01
```

Sample stdout:
```
2026/06/04 03:00:00 dash-sim: admin HTTP listening on :8080
2026/06/04 03:00:00 dash-sim: gRPC listening on :50051 (device=dpu-sim-01)
```

Optional: preload a scenario.
```powershell
.\bin\dash-sim.exe --scenario .\dash-sim\testdata\scenarios\small.yaml
```

### Option B — SONiC-compatible Redis adapter

Self-contained (no external Redis needed):
```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --embedded-redis
```

Sample stdout:
```
2026/06/04 03:01:00 dash-redis-adapter: started embedded miniredis at 127.0.0.1:55580
2026/06/04 03:01:00 dash-redis-adapter: connected to Redis at 127.0.0.1:55580 (db=0)
2026/06/04 03:01:00 dash-redis-adapter: gRPC listening on :52051
```

Against a real Redis:
```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --redis localhost:6379 --redis-db 0
```

> **Note:** All CLI examples below use `--target localhost:50051` (dash-sim).
> Swap to `:52051` to talk to the Redis adapter — same commands, same output
> shape.

---

## 5. CLI essentials

```powershell
$c = ".\bin\dash-sim-client.exe"
```

Global flags (apply to every subcommand):

| Flag | Default | Purpose |
|---|---|---|
| `--target` | `localhost:50051` | gRPC endpoint |
| `-o, --output` | `json` | `json`, `yaml`, or `table` |
| `--timeout` | `10s` | per-RPC timeout |
| `--insecure` | `true` | plaintext gRPC |

### 5.1 List available object kinds

```powershell
& $c kinds -o table
```

Sample output (first lines):
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
... (29 kinds total)
```

Composite keys are joined with `:` on the command line. e.g. `acl_rule` key
`acl-prod-in:100` means `group_id=acl-prod-in, rule_num=100`.

### 5.2 Ping

```powershell
& $c ping
```
Output:
```
ok: target=localhost:50051 vnets=0
```

---

## 6. Apply (create or replace)

Apply is idempotent — first call CREATEs, subsequent calls UPDATE.

### 6.1 Create a VNET

```powershell
& $c apply --kind vnet --key vnet-prod --value '{"vni":1001}'
```

Output:
```
OK txn=tx-1780520339732152200-1 ts=1780520339732152200
```

Now create a few more:

```powershell
& $c apply --kind vnet --key vnet-dev   --value '{"vni":1002}'
& $c apply --kind vnet --key vnet-stage --value '{"vni":1003}'
```

### 6.2 Create an ENI

`mac_address` is upstream `bytes` → base64 in JSON. `ABEiM0RV` =
`00:11:22:33:44:55`.

```powershell
& $c apply --kind eni --key eni-001 --value '{
  "eni_id":      "11111111-1111-1111-1111-111111111111",
  "mac_address": "ABEiM0RV",
  "vnet":        "vnet-prod",
  "admin_state": "STATE_ENABLED"
}'
```
Output:
```
OK txn=tx-1780520339888845000-2 ts=1780520339888845000
```

### 6.3 Create an ACL group + rule + bind to inbound stage 1

```powershell
& $c apply --kind acl_group --key acl-prod-in --value '{"ip_version":"IP_VERSION_IPV4"}'
& $c apply --kind acl_rule  --key 'acl-prod-in:100' --value '{"priority":100,"action":"ACTION_PERMIT","terminating":true}'
& $c apply --kind acl_in    --key 'eni-001:1' --value '{"v4_acl_group_id":"acl-prod-in"}'
```

### 6.4 Create a route_group + route, then bind the ENI to it

```powershell
& $c apply --kind route_group --key rg-prod --value '{"version":"v1"}'
& $c apply --kind route       --key 'rg-prod:10.1.0.0/16' --value '{"routing_type":"ROUTING_TYPE_VNET","vnet":"vnet-stage"}'
& $c apply --kind eni_route   --key eni-001 --value '{"group_id":"rg-prod"}'
```

### 6.5 Create a VNET mapping (overlay → underlay)

```powershell
& $c apply --kind vnet_mapping --key 'vnet-stage:10.1.0.10' --value '{
  "underlay_ip":  {"ipv4": 167862884},
  "routing_type": "ROUTING_TYPE_VNET"
}'
```

> `ipv4` is the upstream `fixed32` field, **network-byte-order**. For
> `100.64.0.10` that integer is `167862884`. There's a helper formula:
> `ip = a + b*256 + c*65536 + d*16777216` for `a.b.c.d`.

### 6.6 Bulk apply from a scenario file

```powershell
& $c apply -f .\dash-sim\testdata\scenarios\small.yaml
```

Output (one Ack per object):
```
OK txn=tx-... ts=...
OK txn=tx-... ts=...
... (one per spec entry)
```

---

## 7. Get

### 7.1 JSON (default)

```powershell
& $c get --kind vnet --key vnet-prod
```
Output:
```json
{
  "kind": "vnet",
  "key": [
    "vnet-prod"
  ],
  "value": {
    "vni": 1001
  }
}
```

### 7.2 YAML

```powershell
& $c get --kind vnet --key vnet-prod -o yaml
```
Output:
```yaml
key:
    - vnet-prod
kind: vnet
value:
    vni: 1001
```

### 7.3 Table

```powershell
& $c get --kind eni --key eni-001 -o table
```
Output:
```
KIND  KEY      VALUE
eni   eni-001  {"eni_id":"11111111-1111-1111-1111-111111111111", "mac_address":"ABEiM0RV", "admin_state":"STATE_ENABLED", "vnet":"vnet-prod"}
```

### 7.4 Get a composite-keyed object

```powershell
& $c get --kind acl_rule --key 'acl-prod-in:100'
```
Output:
```json
{
  "kind": "acl_rule",
  "key": ["acl-prod-in", "100"],
  "value": {
    "priority": 100,
    "action": "ACTION_PERMIT",
    "terminating": true
  }
}
```

### 7.5 Get a missing object

```powershell
& $c get --kind vnet --key does-not-exist
```
Output:
```
Error: rpc error: code = NotFound desc = not found
```

---

## 8. Update (re-apply with new value)

`apply` on an existing key replaces it (the server emits an `UPDATED` event).

```powershell
& $c apply --kind vnet --key vnet-prod --value '{"vni":1099,"version":"v2"}'
```
Output:
```
OK txn=tx-1780520400000000000-7 ts=1780520400000000000
```

Verify:
```powershell
& $c get --kind vnet --key vnet-prod
```
Output:
```json
{
  "kind": "vnet",
  "key": ["vnet-prod"],
  "value": {
    "vni": 1099,
    "version": "v2"
  }
}
```

Disable an ENI (full-object update):

```powershell
& $c apply --kind eni --key eni-001 --value '{
  "eni_id":"11111111-1111-1111-1111-111111111111",
  "mac_address":"ABEiM0RV",
  "vnet":"vnet-prod",
  "admin_state":"STATE_DISABLED"
}'
```

---

## 9. List

### 9.1 List every object of a kind

```powershell
& $c list --kind vnet -o table
```
Output:
```
KIND  KEY         VALUE
vnet  vnet-dev    {"vni":1002}
vnet  vnet-prod   {"vni":1099,"version":"v2"}
vnet  vnet-stage  {"vni":1003}
```

### 9.2 Filter by joined-key prefix

```powershell
& $c list --kind acl_rule --prefix 'acl-prod-in:' -o table
```
Output:
```
KIND      KEY              VALUE
acl_rule  acl-prod-in:100  {"priority":100, "action":"ACTION_PERMIT", "terminating":true}
```

### 9.3 As YAML stream

```powershell
& $c list --kind vnet -o yaml
```
Output:
```yaml
---
key:
    - vnet-dev
kind: vnet
value:
    vni: 1002
---
key:
    - vnet-prod
kind: vnet
value:
    vni: 1099
    version: v2
---
key:
    - vnet-stage
kind: vnet
value:
    vni: 1003
```

---

## 10. Delete

```powershell
& $c delete --kind vnet --key vnet-dev
```
Output:
```
OK txn=tx-1780520500000000000-9 ts=1780520500000000000
```

Verify gone:
```powershell
& $c get --kind vnet --key vnet-dev
# Error: rpc error: code = NotFound desc = not found
```

---

## 11. Subscribe (snapshot + live)

```powershell
& $c subscribe --snapshot --kinds vnet,eni
```

Sample output (one JSON line per event):
```json
{"key":["vnet-dev"],"kind":"vnet","server_ts_ns":1780520355317286900,"txn_id":"","type":"SNAPSHOT","value":{"vni":1002}}
{"key":["vnet-prod"],"kind":"vnet","server_ts_ns":1780520355317286900,"txn_id":"","type":"SNAPSHOT","value":{"vni":1099,"version":"v2"}}
{"key":["eni-001"],"kind":"eni","server_ts_ns":1780520355317286900,"txn_id":"","type":"SNAPSHOT","value":{"eni_id":"11111111-1111-1111-1111-111111111111","mac_address":"ABEiM0RV","admin_state":"STATE_DISABLED","vnet":"vnet-prod"}}
```

From another terminal, mutate to see live events flow:
```powershell
& $c apply --kind vnet --key vnet-live --value '{"vni":9999}'
```
The subscribe terminal will print:
```json
{"key":["vnet-live"],"kind":"vnet","server_ts_ns":1780520357325754100,"txn_id":"tx-1780520357325754100-12","type":"CREATED","value":{"vni":9999}}
```

`Ctrl+C` to stop the stream.

---

## 12. Counters

```powershell
& $c counters --kind eni --key eni-001 -o table
```
Output:
```
COUNTER      VALUE
bytes_in     995328
bytes_out    566784
drops        0
packets_in   576
packets_out  1008
```

---

## 13. SimulatePacket (dash-sim only)

Walks the full DASH pipeline:
**direction → ENI → ACL 1..5 → route LPM → vnet_mapping encap / service_tunnel / appliance → counters**.

### 13.1 Outbound — ENCAP via vnet_mapping

```powershell
& $c simulate --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 80 --trace
```
Output:
```json
{
  "action": "ENCAP",
  "matched_acl_priority": 0,
  "matched_acl_stage": 0,
  "matched_route_prefix": "10.1.0.0/16",
  "out_eni": "eni-001",
  "out_routing_type": "VNET",
  "out_underlay_ip": "100.64.0.10",
  "out_vni": 0,
  "reason": "encap",
  "trace": [
    "input dir=DIRECTION_OUTBOUND eni=\"eni-001\" vni=0 src=10.0.0.1:1024->10.1.0.10:80 proto=6 len=64",
    "outbound: eni=\"eni-001\" admin_state=ENABLED vnet=vnet-prod",
    "outbound: route_group=\"rg-prod\"",
    "route LPM dst=10.1.0.10 -> prefix=10.1.0.0/16",
    "vnet_mapping hit vnet=vnet-stage ip=10.1.0.10",
    "ENCAP eni=eni-001 underlay=100.64.0.10 vni=0"
  ]
}
```

### 13.2 Outbound — DROP (no route)

```powershell
& $c simulate --direction outbound --eni eni-001 --src-ip 10.0.0.1 --dst-ip 172.16.5.5
```
Output:
```json
{
  "action": "DROP",
  "reason": "route lookup: no route matches dst=172.16.5.5 in group=rg-prod",
  ...
}
```

### 13.3 Outbound — DROP (admin_state=DISABLED)

```powershell
& $c simulate --direction outbound --eni eni-001 --src-ip 10.0.0.1 --dst-ip 10.1.0.10
```
Output (after the §8 disable update):
```json
{
  "action": "DROP",
  "reason": "eni \"eni-001\" admin_state=STATE_DISABLED",
  ...
}
```

### 13.4 Inbound deliver (with a route_rule and ACL_IN permit set up)

Setup:
```powershell
& $c apply --kind route_rule --key 'eni-001:1001:0.0.0.0/0:100' --value '{"action_type":"ACTION_TYPE_DECAP","vnet":"vnet-prod"}'
```

Simulate:
```powershell
& $c simulate --direction inbound --eni eni-001 --vni 1001 --src-ip 100.64.0.5 --dst-ip 10.0.0.4 --trace
```
Output:
```json
{
  "action": "FORWARD",
  "out_eni": "eni-001",
  "reason": "inbound deliver",
  "trace": [
    "input dir=DIRECTION_INBOUND eni=\"eni-001\" vni=1001 src=100.64.0.5:0->10.0.0.4:0 proto=0 len=64",
    "route_rule matched priority=100",
    "inbound: matched route_rule action_type=ACTION_TYPE_DECAP",
    "acl stage=1 group=\"acl-prod-in\"",
    "  matched rule priority=100 action=ACTION_PERMIT terminating=true",
    "FORWARD eni=eni-001 (inbound deliver)"
  ]
}
```

> Against `dash-redis-adapter` this returns
> `code = Unimplemented; use the dash-sim binary instead`.

---

## 14. Admin HTTP (dash-sim only) — bonus

```powershell
Invoke-RestMethod http://localhost:8080/admin/health | ConvertTo-Json -Depth 5
```
Sample output (truncated):
```json
{
  "device_id": "dpu-sim-01",
  "dropped_events": 0,
  "sizes": {
    "vnet": 3,
    "eni":  1,
    "acl_group": 1,
    ...
  },
  "status": "ok",
  "subscribers": 0
}
```

Inject a one-shot Apply failure:
```powershell
$body = @{ op = "Apply"; mode = "error"; count = 1; message = "injected" } | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/faults -Body $body -ContentType application/json
```

Try Apply — first call rejected, second accepted:
```powershell
& $c apply --kind vnet --key vnet-x --value '{"vni":111}'   # REJECTED txn=  error=injected
& $c apply --kind vnet --key vnet-x --value '{"vni":111}'   # OK  txn=tx-... ts=...
```

---

## 15. Quick reference card

```powershell
$c = ".\bin\dash-sim-client.exe"
& $c kinds -o table
& $c ping
& $c apply  --kind <k> --key <a:b:...> --value '<JSON>'
& $c apply  -f <file.yaml|file.json>
& $c get    --kind <k> --key <a:b:...>            [-o json|yaml|table]
& $c list   --kind <k> [--prefix <p>] [--limit N] [-o json|yaml|table]
& $c delete --kind <k> --key <a:b:...>
& $c subscribe [--snapshot] [--kinds <k1,k2>]
& $c counters --kind <k> --key <a:b:...>          [-o json|yaml|table]
& $c simulate --direction outbound|inbound --eni <e> \
              [--vni <n>] [--src-mac ...] [--dst-mac ...] \
              [--src-ip ...] [--dst-ip ...] [--protocol ...] \
              [--src-port ...] [--dst-port ...] [--trace]
```
