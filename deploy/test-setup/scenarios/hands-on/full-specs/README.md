# Full-Spec Library

Production-style, self-contained DASH manifests. Each file is centred on
one primary resource kind and includes **every dependency inline** in
correct tier order so a single `dashctl apply -f <file>` brings the whole
scenario up — and a single `dashctl delete -f <file>` tears it down.

These complement `../config-specs/` (which are intentionally minimal,
didactic experiments) and `../bootstrap/` (a tier-split baseline). Use
these full specs to exercise feature coverage, integration tests, smoke
tests, demos, and load templates.

## Files

| File | Primary kind | Objects | Highlights |
|---|---|---|---|
| `eni-full.yaml` | ENI | 10 | VNet + 2 tunnels (NAT + privatelink) + 3 mappings + route policy + 2 ACLs |
| `vnet-full.yaml` | VNet | 12 | 3-tier app (web×2, app×1) + privatelink + 2 route policies + ACL |
| `route-full.yaml` | RoutePolicy | 8 | Every `next_hop_type` (vnet, service_tunnel, direct, drop, ECMP) |
| `mapping-full.yaml` | VnetMapping | 10 | Every `action` value (vnet_encap, service_tunnel, drop) |
| `acl-full.yaml` | AclPolicy | 6 | Every rule match field + inbound + outbound + protocol-number form |
| `service-tunnel-full.yaml` | ServiceTunnel | 10 | All 6 actions: nat / inspect / privatelink / ipsec / scrub / vxlan_peer |
| `private-link-full.yaml` | (PrivateLink pattern) | 8 | App tier locked to managed-SQL privatelink only |
| `ha-full.yaml` | HaSet | 8 | Active/standby HA pair + 2 ENIs placed on the members |

## Coverage matrix

| Kind | eni | vnet | route | map | acl | st | pl | ha |
|---|---|---|---|---|---|---|---|---|
| Vnet           | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| ServiceTunnel  | ✓ | ✓ | ✓ | ✓ |   | ✓ | ✓ | ✓ |
| Eni            | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| VnetMapping    | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| RoutePolicy    | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| AclPolicy      | ✓ | ✓ |   |   | ✓ |   | ✓ | ✓ |
| HaSet          |   |   |   |   |   |   |   | ✓ |

`private-link-full.yaml` does not introduce a new kind — PrivateLink in
DASH is modelled as `ServiceTunnel` with `params.action: privatelink`.
The spec is included as a pattern example.

## Naming convention

All resource names are prefixed with `full-<kind>-` so they never collide
with the bootstrap or config-spec experiments. Label `demo=<kind>-full`
is set on every resource for easy filtering:

```bash
dashctl get vnet,service-tunnel,eni,vnet-mapping,route-policy,acl-policy,ha-set \
  -l demo=eni-full -o wide
```

## Prerequisites

A running DashCenter fleet with at least 9 DPUs in inventory
(`dpu-sim-01` … `dpu-sim-09`). Any of the test-setup topologies works:

```powershell
cd deploy/test-setup/07-full-experiment
pwsh ./start-fleet.ps1
```

Set CLI endpoints:

```powershell
$env:DASHCTL_ENDPOINT       = "http://localhost:28443"
$env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:27443"
```

(Adjust ports for your topology — `05-full-console` and `04-ha-fleet` use
the same defaults; `dashd-e2e` uses `localhost:8443/7443`.)

## One-shot apply / delete

Apply every full spec:

```powershell
pwsh ./test-roundtrip.ps1 -Action apply
```

Verify (each spec carries a unique `demo=<kind>-full` label):

```powershell
pwsh ./test-roundtrip.ps1 -Action verify
```

Tear down:

```powershell
pwsh ./test-roundtrip.ps1 -Action delete
```

End-to-end self-test (apply → verify → delete → verify-gone):

```powershell
pwsh ./test-roundtrip.ps1 -Action test
```

Linux/macOS equivalent:

```bash
./test-roundtrip.sh apply
./test-roundtrip.sh verify
./test-roundtrip.sh delete
./test-roundtrip.sh test
```

## Per-spec apply

Per-spec usage with raw dashctl (apply with file, delete by label):

```bash
# Apply a single spec
dashctl apply  -f deploy/test-setup/scenarios/hands-on/full-specs/eni-full.yaml --force

# Inspect what got created
dashctl get vnet,service-tunnel,eni,vnet-mapping,route-policy,acl-policy,ha-set \
  -l demo=eni-full -o wide

# Tear it down — dashctl delete is kind/name, not -f, so use the helper:
pwsh ./test-roundtrip.ps1 -Action delete
# or per-label manually (delete policies first to satisfy FK protection):
for kind in acl-policy route-policy ha-set vnet-mapping eni service-tunnel vnet; do
  for n in $(dashctl get $kind -l demo=eni-full -o name); do
    dashctl delete "$kind" "${n##*/}" --ignore-not-found
  done
done
```

## See also

- [../config-specs/](../config-specs/) — per-kind didactic experiments
- [../bootstrap/](../bootstrap/) — tier-split baseline manifest set
- [../../README.md](../README.md) — hands-on scenario library overview
- [../../../../../docs/CLI_GUIDE.md](../../../../../docs/CLI_GUIDE.md) — dashctl reference
