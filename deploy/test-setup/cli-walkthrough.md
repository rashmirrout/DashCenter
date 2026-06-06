# `dash-sim-client` — Manual CLI Walkthrough

Drive a running DashCenter fleet with `dash-sim-client` by hand,
command by command. Works unchanged against any of the three test
topologies — only the `--target` host/port differs, and those come
straight from your [fleet.yaml](fleet.yaml) / [fleet.json](fleet.json).

> **Where's the binary?**
> Native: `src/impl-go/bin/dash-sim-client.exe` after `go build`.
> Container: `dashcenter/dash-sim-client:dev` (built by topology 02's
> `build-images.{ps1,sh}` or as part of topology 03's compose).

For convenience, every PowerShell block below assumes:

```powershell
$c    = "..\..\src\impl-go\bin\dash-sim-client.exe"   # run from deploy/test-setup/
$sim1 = "127.0.0.1:50051"   # dpu-sim-01 — match your fleet config
$sim2 = "127.0.0.1:50052"   # dpu-sim-02
$sim3 = "127.0.0.1:50053"   # dpu-sim-03
$ada  = "127.0.0.1:52051"   # dash-redis-adapter
```

Linux/WSL:

```bash
c="../../src/impl-go/bin/dash-sim-client"
sim1="127.0.0.1:50051"; sim2="127.0.0.1:50052"; sim3="127.0.0.1:50053"
ada="127.0.0.1:52051"
```

If you changed ports in `fleet.yaml`, update these variables to match.

The CLI exposes nine subcommands. All take `--target <host:port>` and
(where applicable) `-o table|json|yaml`.

| Verb        | What it does                                              | Works against |
|-------------|-----------------------------------------------------------|---------------|
| `ping`      | Liveness check — returns the device id.                   | sim + adapter |
| `kinds`     | List the 29 supported DASH object kinds + their key parts.| sim + adapter |
| `apply`     | Create or update an object (JSON / YAML / protojson).     | sim + adapter |
| `get`       | Fetch one object by `--kind` + `--key`.                   | sim + adapter |
| `list`      | List objects of a `--kind`.                               | sim + adapter |
| `delete`    | Remove one object by `--kind` + `--key`.                  | sim + adapter |
| `counters`  | Read per-object counter ticks.                            | sim (adapter empty) |
| `subscribe` | Stream object change events (optional snapshot first).    | sim + adapter |
| `simulate`  | Send one packet through the pipeline; get a `Decision`.   | **sim only**  |

---

## 0. Sanity ping every endpoint

```powershell
$sim1, $sim2, $sim3, $ada | ForEach-Object {
  Write-Host -NoNewline "$_  -> "
  & $c --target $_ ping
}
```

If a port returns `connection refused`, that component isn't running
on that port — check the topology's README and re-confirm `fleet.yaml`.

---

## 1. Discover what kinds you can drive

```powershell
& $c --target $sim1 kinds -o table
```

Prints the full DASH object kind catalog (29 entries) and the **key
parts** each kind expects.

| Kind            | Key parts                       |
|-----------------|---------------------------------|
| `vnet`          | `<id>`                          |
| `eni`           | `<id>`                          |
| `acl_rule`      | `<group_id>:<rule_num>`         |
| `acl_in`        | `<eni_id>:<stage>`              |
| `vnet_mapping`  | `<vnet_id>:<overlay_ip>`        |
| `route`         | `<group_id>:<prefix>`           |
| `eni_route`     | `<eni_id>`                      |

Pass **all** key parts (`:`-joined) to `apply` / `get` / `delete`.

---

## 2. Create a VNet on dpu-sim-01

```powershell
& $c --target $sim1 apply --kind vnet --key vnet-prod   --value '{"vni":1001}'
& $c --target $sim1 apply --kind vnet --key vnet-stage  --value '{"vni":1002}'
& $c --target $sim1 list  --kind vnet -o table
```

The same commands against `$sim2` create a *different* VNet on a
*different* device — `dash-sim` instances are wholly independent.

---

## 3. ENI + ACL

```powershell
& $c --target $sim1 apply --kind eni --key eni-001 --value '{
  "eni_id":"11111111-1111-1111-1111-111111111111",
  "mac_address":"ABEiM0RV",
  "vnet":"vnet-prod",
  "admin_state":"STATE_ENABLED"
}'

& $c --target $sim1 apply --kind acl_group --key acl-prod-in --value '{
  "ip_version":"IP_VERSION_IPV4"
}'

& $c --target $sim1 apply --kind acl_rule --key "acl-prod-in:100" --value '{
  "priority":100, "action":"ACTION_PERMIT", "terminating":true
}'

& $c --target $sim1 apply --kind acl_in --key "eni-001:1" --value '{
  "v4_acl_group_id":"acl-prod-in"
}'
```

Enum names use upstream protojson (e.g. `STATE_ENABLED`), bytes are base64.

---

## 4. Routes + vnet_mapping

```powershell
& $c --target $sim1 apply --kind route_group --key rg-prod --value '{"version":"v1"}'

& $c --target $sim1 apply --kind route --key "rg-prod:10.1.0.0/16" --value '{
  "routing_type":"ROUTING_TYPE_VNET", "vnet":"vnet-stage"
}'

& $c --target $sim1 apply --kind eni_route --key eni-001 --value '{"group_id":"rg-prod"}'

& $c --target $sim1 apply --kind vnet_mapping --key "vnet-prod:10.0.0.20" --value '{
  "underlay_ip":{"ipv4": 343798372},
  "mac_address":"AKqrzN3u",
  "routing_type":"ROUTING_TYPE_VNET"
}'
```

> If you launched with the default `scenarios/dpu-base.yaml` preload, all
> of the above is already present — feel free to `list` instead.

---

## 5. Run a packet through the pipeline (sim only)

```powershell
# Outbound — should hit vnet_mapping and ENCAP.
& $c --target $sim1 simulate `
  --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 80 --trace

# Inbound — should DELIVER after ACL_IN.
& $c --target $sim1 simulate `
  --direction inbound --eni eni-001 --vni 1001 `
  --src-ip 100.64.0.5 --dst-ip 10.0.0.4 --trace
```

`simulate` against `$ada` returns `Unimplemented` — Redis APP_DB has no
behavioural pipeline.

---

## 6. Watch object changes live

```powershell
& $c --target $sim1 subscribe --snapshot --kinds vnet,eni
```

In another shell:

```powershell
& $c --target $sim1 apply  --kind vnet --key vnet-watch --value '{"vni":1234}'
& $c --target $sim1 delete --kind vnet --key vnet-watch
```

Both events appear in the subscribe stream.

---

## 7. Counters

```powershell
& $c --target $sim1 counters --kind eni --key eni-001 -o table
```

`dash-sim` ticks counters once per `defaults.tickInterval` (1s in the
example config). The adapter returns empty.

---

## 8. Compare sim vs adapter via the same CLI

```powershell
& $c --target $sim1 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target $ada  apply --kind vnet --key vnet-prod --value '{"vni":1001}'

& $c --target $sim1 get --kind vnet --key vnet-prod -o json
& $c --target $ada  get --kind vnet --key vnet-prod -o json
```

Topology 03 only — inspect the adapter's Redis state directly:

```powershell
docker exec -it dc-redis-fleet redis-cli KEYS 'DASH_VNET_TABLE:*'
docker exec -it dc-redis-fleet redis-cli HGETALL DASH_VNET_TABLE:vnet-prod
```

Wire format follows what SONiC's DASH orchagent reads:
`DASH_<KIND>_TABLE:<joined-key>` → HASH `{pb: <binary protobuf>, meta: <json>}`.

---

## 9. Multi-DPU fan-out

```powershell
$targets = @($sim1, $sim2, $sim3)

foreach ($t in $targets) {
  & $c --target $t apply --kind vnet --key vnet-fleet --value '{"vni":9001}'
}

foreach ($t in $targets) {
  Write-Host "==> $t"
  & $c --target $t get --kind vnet --key vnet-fleet -o yaml
}
```

Smallest possible multi-DPU integration test — same write, N
independent reads, identical results.

---

## 10. Troubleshooting

| Symptom                                          | Likely cause / fix                                                                   |
|--------------------------------------------------|--------------------------------------------------------------------------------------|
| `connection refused`                             | Endpoint not running on that port. Check `docker ps` / `.fleet-state.json`.          |
| `apply: kind X expects N key parts ...`          | `--key` doesn't have N `:`-separated parts. Run `kinds` to see KEY_PARTS.            |
| `decode value: unknown field ...`                | JSON field name doesn't match upstream protojson. Check the generated `.pb.go`.      |
| `simulate ... Unimplemented`                     | You're hitting `dash-redis-adapter`. Use `$sim1/$sim2/$sim3` for the pipeline.       |
| `bind: ... forbidden by its access permissions`  | Windows reserved that port. Pick another in `fleet.yaml`.                            |
| `redis: connection refused` (adapter logs)       | Topology 03 only — redis container failed. `docker compose logs redis`.              |
| `fleet config invalid: dpus[1].grpcPort N conflicts with ...` | Validator caught a duplicate port. Edit `fleet.yaml` and re-run.            |
| `bad interpreter: /usr/bin/env\r` in WSL         | Bash script got CRLF line endings. `git checkout -- deploy/test-setup` after `.gitattributes` is in. |
