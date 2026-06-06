# DashCenter test-setup — scenario library

Every `dash-sim` in the [deploy/test-setup/](..) topologies boots with a
**scenario** preloaded into its in-memory store. The scenario is just a
YAML list of `(kind, key, value)` triples; the dash-sim loader
(`src/impl-go/dash-sim/internal/sim/scenarios/loader.go`) replays them
through the same gRPC `Apply` path the CLI uses.

Three scenarios ship in this folder, in increasing complexity. Pick one
via `defaults.scenario` (or `dpus[i].scenario`) in your
[`fleet.yaml` / `fleet.json`](../fleet.example.yaml).

| File | Size | One-line purpose |
|---|---:|---|
| [`dpu-base.yaml`](dpu-base.yaml) | ~10 objects | Minimal working snapshot. **Default.** |
| [`dpu-all-kinds.yaml`](dpu-all-kinds.yaml) | 30 objects | One example of every supported DASH kind — explorer / API smoke. |
| [`dpu-medium.yaml`](dpu-medium.yaml) | ~50 objects | Realistic mid-scale fleet with multi-vnet, multi-ACL, multi-prefix LPM. |

---

## `dpu-base.yaml` — minimal working snapshot

Contents: 1 appliance, 2 vnets, 2 ENIs, 1 ACL group + rule + binding,
1 vnet_mapping, 1 route_group + route, 1 eni_route.

Use it when you just want **something to query** so the fleet isn't
empty after boot. It's also the default preload for every topology and
the scenario assumed by the [hands-on guides](../01-host-multi-port/manual-handson.md).

```powershell
& $c --target $sim list --kind vnet -o table
& $c --target $sim list --kind eni  -o table
```

Both return real rows immediately.

> The `simulate` command on this scenario will **DROP** with
> `reason: vnet_mapping vnet-stage/10.1.0.10: not found` — that's a
> learning moment, not a bug. The scenario has a route to `vnet-stage`
> but no corresponding `vnet_mapping`, so the pipeline correctly says
> "no mapping". See topology 02's hands-on guide for the optional
> follow-up `apply` that converts it into a FORWARD.

---

## `dpu-all-kinds.yaml` — one example of all 29 DASH kinds

Contents: exactly **one** object of every supported kind (with a second
`vnet` so peering can be referenced). Values are chosen for *syntactic
completeness*, not pipeline correctness — the HA / outbound-port-map
families are populated but won't drive a meaningful packet flow.

Use it when you want to:

- Learn the protojson shape of every DASH kind hands-on.
- Smoke-test every CLI verb (`kinds`, `list`, `get`, `apply`, `delete`,
  `subscribe`) against every type.
- Debug the loader / store after a code change.

```powershell
# All 29 kinds return at least one row.
$kinds = & $c --target $sim kinds -o json | ConvertFrom-Json
foreach ($k in $kinds.name) {
  $n = @(& $c --target $sim list --kind $k -o json | ConvertFrom-Json).Count
  Write-Host ("{0,-25} {1}" -f $k, $n)
}
```

---

## `dpu-medium.yaml` — realistic mid-scale fleet

Contents (~50 objects):

| Family | Count | Highlights |
|---|---:|---|
| Appliance | 1 | |
| VNet | 3 | `vnet-prod` (1001), `vnet-stage` (1002), `vnet-mgmt` (1003) |
| ENI | 5 | `eni-001`/`002`/`003` (prod; 003 is `STATE_DISABLED`), `eni-100` (stage), `eni-200` (mgmt) |
| ACL group | 3 | `acl-prod-in` (deny-by-port + deny-by-src + catch-all permit), `acl-prod-out` (port-range permit + catch-all deny), `acl-mgmt-in` |
| ACL rule | 6 | priority 10, 20, 100, 100, 200; protocol filters + port ranges |
| ACL binding | 5 | `acl_in` on stages 1+2 of `eni-001`, plus `acl_out` on stage 1 |
| Route group | 2 | `rg-prod` (4 routes), `rg-mgmt` (2 routes) |
| Route | 6 | broad `/8` catch + `/16` VNET peering + surgical `/24` DROP hole + cross-vnet ROUTE |
| `vnet_mapping` | 3 | two in `vnet-stage`, one in `vnet-mgmt` |
| Inbound `route_rule` | 1 | DECAP for VNI 1001 on `eni-001` |
| QoS / meter | 2+1+2 | `qos-prod`, `qos-mgmt`, `mp-prod` with 2 rules |
| Prefix tag, tunnel | 1+1 | scaffolding |

### What the medium scenario teaches via `simulate`

The bottom of [`dpu-medium.yaml`](dpu-medium.yaml) has seven
copy-pasteable `dash-sim-client simulate` invocations. Each one is
chosen to land on a *different* terminal pipeline outcome — together
they walk every meaningful path through the DASH packet pipeline.

> Replace `127.0.0.1:50051` with the gRPC port from your `fleet.yaml`
> if you changed it. All seven take `--trace` so the per-step reasoning
> is in the output.

#### 1. ACL_OUT catch-all DENY (port 80 below the permit's `[1024..65535]` range)

```powershell
& $c --target $sim simulate `
  --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 80 --trace
# → action=DROP, reason=acl_out stage=1 priority=200 deny
```

The priority-10 permit only covers `dst_port` 1024–65535; port 80 falls
through to the priority-200 catch-all DENY. **Teaches: ACL evaluation
happens before route lookup; rule order matters.**

#### 2. Happy-path ENCAP (everything aligns)

```powershell
& $c --target $sim simulate `
  --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 8080 --trace
# → action=FORWARD, ENCAP via 100.64.0.20
```

Port 8080 lands in the permit range → LPM hits `10.1.0.0/16` →
`ROUTING_TYPE_VNET` to `vnet-stage` → `vnet_mapping` resolves to
underlay `100.64.0.20`. **Teaches: full outbound encap pipeline; this
is the "everything works" baseline.**

#### 3. LPM surgical hole (broad route + targeted DROP inside it)

```powershell
& $c --target $sim simulate `
  --direction outbound --eni eni-001 `
  --src-ip 10.0.0.1 --dst-ip 10.1.1.5 `
  --protocol 6 --src-port 1024 --dst-port 8080 --trace
# → action=DROP, reason=route ROUTING_TYPE_DROP
```

Same ACL permit as case 2, but LPM picks the more-specific
`10.1.1.0/24 → ROUTING_TYPE_DROP` over the broader `10.1.0.0/16`.
**Teaches: longest-prefix match is per-prefix, not per-route-group;
DROP routes are first-class.**

#### 4. ENI admin-state DISABLED — instant DROP

```powershell
& $c --target $sim simulate `
  --direction outbound --eni eni-003 `
  --src-ip 10.0.0.3 --dst-ip 10.1.0.10 `
  --protocol 6 --src-port 1024 --dst-port 8080 --trace
# → action=DROP, reason=eni "eni-003" admin_state=STATE_DISABLED
```

The pipeline short-circuits at the ENI stage — no ACL or route lookup
happens. **Teaches: admin state is the first gate; useful for blast-radius
isolation in fleet upgrades.**

#### 5. ACL_IN DENY on TCP/22 (protocol + port filter)

```powershell
& $c --target $sim simulate `
  --direction inbound --eni eni-001 --vni 1001 `
  --src-ip 100.64.0.5 --dst-ip 10.0.0.4 `
  --protocol 6 --src-port 50000 --dst-port 22 --trace
# → action=DROP, reason=acl_in stage=1 priority=10 deny
```

Priority-10 deny on `protocol=[6]` + `dst_port=22` fires regardless of
source. **Teaches: inbound ACLs are evaluated against the inner packet
after decap; protocol filters apply per-rule.**

#### 6. ACL_IN DENY by source CIDR `172.16.0.0/24`

```powershell
& $c --target $sim simulate `
  --direction inbound --eni eni-001 --vni 1001 `
  --src-ip 172.16.0.42 --dst-ip 10.0.0.4 `
  --protocol 6 --src-port 50000 --dst-port 8080 --trace
# → action=DROP, reason=acl_in stage=1 priority=20 deny
```

Same destination as case 7 (which DELIVERs) — only the source CIDR
changed. **Teaches: ACL `src_addr` prefix matching; same flow can pass
or drop based solely on src.**

#### 7. Happy-path inbound DELIVER

```powershell
& $c --target $sim simulate `
  --direction inbound --eni eni-001 --vni 1001 `
  --src-ip 100.64.0.5 --dst-ip 10.0.0.4 `
  --protocol 6 --src-port 50000 --dst-port 8080 --trace
# → action=DELIVER
```

ACL_IN priority-100 permit catches it → inbound `route_rule` for
`eni-001` / VNI 1001 / `10.0.0.0/24` performs DECAP → packet delivered
to the ENI. **Teaches: inbound `route_rule` is the inbound-side analog
of `route` + `vnet_mapping`; without it, well-formed inbound packets
still drop.**

### Suggested learning loop

Run cases in this order for the smoothest "I get it now" curve:

1. **#2 (happy ENCAP)** — see the full successful path first.
2. **#4 (admin disabled)** — see the earliest pipeline short-circuit.
3. **#1 (ACL_OUT DENY)** — see ACLs gating routes.
4. **#3 (LPM hole)** — see how more-specific prefixes win.
5. **#7 (inbound DELIVER)** — flip to the inbound side.
6. **#5 and #6 (inbound DENY)** — see protocol-filter vs. source-CIDR matching.

---

## Writing your own scenario

The on-disk format is documented in
[`src/impl-go/dash-sim/internal/sim/scenarios/loader.go`](../../../src/impl-go/dash-sim/internal/sim/scenarios/loader.go).
Quick reminders:

- Enums use their **full name** (`STATE_ENABLED`, `ROUTING_TYPE_VNET`).
- Bytes are **base64**.
- `IpAddress.ipv4` is `fixed32` in **network byte order**. Useful
  values: `10.0.0.1 = 16777226`, `10.0.0.20 = 335544330`,
  `100.64.0.5 = 92274276`, `100.64.0.20 = 343798372`.
- `IpPrefix` is `{ ip: <IpAddress>, mask: <IpAddress> }`.

If a kind's protojson shape is unclear, the upstream proto under
[`proto/vendor/sonic-dash-api/`](../../../proto/vendor/sonic-dash-api/)
is the source of truth (every kind has one `<name>.proto` file there).

Drop your new YAML in this folder and point at it from `fleet.yaml`:

```yaml
defaults:
  scenario: scenarios/my-new-scenario.yaml
```

Every script picks it up — no other change required.
