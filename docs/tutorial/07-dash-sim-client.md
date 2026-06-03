# 07 — dash-sim-client (CLI)

This page is intentionally compact. The **comprehensive** CLI reference —
every subcommand, every flag, copy-paste input/output for each — lives in
[docs/CLI_GUIDE.md](../CLI_GUIDE.md). This page summarizes what's there and
adds tutorial-style tips you won't find in the reference.

---

## 1. Where the full reference lives

→ **[docs/CLI_GUIDE.md](../CLI_GUIDE.md)** — read this end to end on first
contact. It covers:

- prerequisites + build + tests
- starting both backends
- `kinds` and `ping` discovery commands
- `apply` (inline + from `-f file`)
- `get` (json / yaml / table)
- update via re-apply
- `list` (with `--prefix`, `--limit`)
- `delete`
- `subscribe` (snapshot + live)
- `counters`
- `simulate` (outbound + inbound with `--trace`)
- admin HTTP bonus
- quick reference card

---

## 2. 30-second cheat sheet

```powershell
$c = ".\bin\dash-sim-client.exe"     # Windows
# or: c=./bin/dash-sim-client          # Linux

# Discovery
& $c kinds -o table
& $c ping

# Generic CRUD (works for any of the 29 kinds)
& $c apply  --kind <k> --key <a:b:...> --value '<JSON>'
& $c apply  -f <scenario.yaml>
& $c get    --kind <k> --key <a:b:...>     [-o json|yaml|table]
& $c list   --kind <k> [--prefix <p>] [--limit N] [-o json|yaml|table]
& $c delete --kind <k> --key <a:b:...>

# Streaming
& $c subscribe [--snapshot] [--kinds <k1,k2>]
& $c counters --kind <k> --key <a:b:...>

# Behavioural pipeline (dash-sim only)
& $c simulate --direction outbound|inbound --eni <e> [...flags...] [--trace]
```

---

## 3. Tutorial tips not in the reference

### 3.1 Composite keys

For any kind whose `KEY_PARTS` length is > 1 (run `dash-sim-client kinds`
to see), join parts with `:`. Examples from the registry:

```
acl_rule                  group_id,rule_num             →  acl-prod-in:100
acl_in / acl_out          eni,stage                     →  eni-001:1
route                     group_id,prefix               →  rg-prod:10.1.0.0/16
route_rule                eni,vni,prefix_or_tag,priority →  eni-001:1001:0.0.0.0/0:100
vnet_mapping              vnet,ip_address               →  vnet-prod:10.0.0.10
meter                     eni,metering_class_id          →  eni-001:42
outbound_port_map_range   map_id,start_port,end_port    →  pm-1:1000:2000
ha_scope_config           vdpu_id,ha_scope_id           →  vdpu-1:eni-001
```

### 3.2 Encoding upstream `bytes` and `IpAddress`

Upstream DASH types use:

- **`bytes`** for MAC addresses, GUIDs — encode as **base64** in JSON.
  ```
  mac 00:11:22:33:44:55  →  "mac_address": "ABEiM0RV"
  ```
- **`fixed32`** for IPv4 — encode as the **network-byte-order int**.
  Formula: `ip(int) = a + b*256 + c*65536 + d*16777216` for `a.b.c.d`.
  ```
  10.0.0.10       →  167772170
  100.64.0.10     →  167862884
  0.0.0.0         →  0
  ```
- **enum** — use the **full enum name string**:
  ```
  "routing_type": "ROUTING_TYPE_VNET"
  "admin_state":  "STATE_ENABLED"
  ```

A handy PowerShell helper:
```powershell
function ToIpv4Int([string]$ip) {
  $b = ([System.Net.IPAddress]::Parse($ip)).GetAddressBytes()
  return $b[0] + $b[1]*256 + $b[2]*65536 + $b[3]*16777216
}
ToIpv4Int "100.64.0.10"   # -> 167862884
```

### 3.3 YAML scenarios beat one-shot `apply` for repeatable runs

`dash-sim-client apply -f scenario.yaml` accepts a multi-doc YAML where each
entry is `{kind, key, value}`. A worked example lives at
[`testdata/scenarios/small.yaml`](../../src/impl-go/dash-sim/testdata/scenarios/small.yaml).

### 3.4 The `--target` flag is the only switch between backends

Same binary, same commands. Just change the port:

```powershell
& $c --target localhost:50051 apply --kind vnet --key v --value '{"vni":1}'   # sim
& $c --target localhost:52051 apply --kind vnet --key v --value '{"vni":1}'   # adapter
```

### 3.5 `simulate` is sim-only

The `simulate` subcommand calls `DashApi.SimulatePacket`. The Redis adapter
returns `code = Unimplemented` because real APP_DB has no pipeline state.
Use `dash-sim` for any behavioural testing.

### 3.6 `subscribe --snapshot` gives you the current world for free

When a fresh consumer connects it has no idea what's already configured.
Always start with `--snapshot` so you receive one event per existing object
before live updates begin.

### 3.7 `-o table` truncates JSON

The `table` output prints a compact one-line JSON in the `VALUE` column for
readability. For full structure, use `-o json` (default) or `-o yaml`.

---

## 4. Common pitfalls

| Symptom | Cause | Fix |
|---|---|---|
| `apply: kind X expects N key parts ...` | Wrong number of `:`-separated key parts | Run `kinds -o table` and count `KEY_PARTS`. |
| `decode value: unknown field "vnetId"` | YAML camelCase not protojson snake_case | Use `vnet_id`, `admin_state`, etc. |
| `bind: ... forbidden by its access permissions` (server side) | Windows reserved port | Pick `:52051` or another free port. |
| `code = Unimplemented` on `simulate` | Talking to adapter | Switch `--target` to the sim's port. |
| Subscribe quiet | Filtered out by `--kinds` | Drop the filter to receive all events. |

---

## Where to go next

- → [docs/CLI_GUIDE.md](../CLI_GUIDE.md) — the full copy-paste reference.
- → [08 — Docker Compose](08-docker-compose.md) — stand the same fleet up
  under containers.
