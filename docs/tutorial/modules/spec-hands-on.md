# Spec Hands-On — Learn Every DashCenter Config Kind

> **Duration**: ~30 minutes (all 11 experiments) or ~3 minutes each
> **Prerequisites**: a running DashCenter fleet (any topology)
> **What you'll learn**: every spec kind and bundle kind, field by field,
> with wrong configs that fail (and why), right configs that succeed,
> create-vs-modify detection, and dependency ordering.

---

## Table of contents

| # | Kind | Tier | Dependencies | Time |
|---|---|---|---|---|
| 1 | [Vnet](#1-vnet) | 0 | none | 2 min |
| 2 | [ServiceTunnel](#2-service-tunnel) | 0 | none | 2 min |
| 3 | [Eni](#3-eni) | 1 | Vnet | 3 min |
| 4 | [VnetMapping](#4-vnet-mapping) | 1 | Vnet | 2 min |
| 5 | [AclPolicy](#5-acl-policy) | — | ENI | 3 min |
| 6 | [RoutePolicy](#6-route-policy) | — | ENI + Vnet/ServiceTunnel | 3 min |
| 7 | [HaSet](#7-ha-set) | 0 | DPU inventory | 2 min |
| 8 | [EniBundle](#8-eni-bundle) | all | auto-expanded | 3 min |
| 9 | [AclBundle](#9-acl-bundle) | all | auto-expanded | 2 min |
| 10 | [RouteBundle](#10-route-bundle) | all | auto-expanded | 2 min |
| 11 | [HaBundle](#11-ha-bundle) | 0 | auto-expanded | 1 min |

**Scenario files**: all YAML used below is in
[`deploy/test-setup/scenarios/hands-on/`](../../../deploy/test-setup/scenarios/hands-on/).

---

## Setup

```powershell
$env:DASHCTL_ENDPOINT = "http://localhost:28443"
$env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:27443"
$bin = "src\impl-go\dashctl\bin\dashctl.exe"
```

---

## 1. Vnet

**Tier 0** — no dependencies. Always succeeds on create.

**What is it**: a virtual network identified by a VNI (VXLAN Network
Identifier). Every ENI, VnetMapping, and RoutePolicy ultimately
references a VNet.

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | ✅ | unique identifier |
| `vni` | uint32 | ✅ | VXLAN Network Identifier (1–16777215) |
| `labels` | map | ❌ | operator tags for filtering |

**Experiment**: apply, re-apply (blocked), force-modify, delete protection.

```powershell
dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/vnet-experiments.yaml
# → vnet/vnet-exp-1 CREATE (generation 1)

dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/vnet-experiments.yaml
# → BLOCKED — already exists; use --force

dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/vnet-experiments.yaml --force
# → vnet/vnet-exp-1 MODIFY (generation 2)
```

See the [vnet-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/vnet-experiments.yaml)
file for the delete orphan protection experiment.

---

## 2. Service Tunnel

**Tier 0** — no dependencies.

**What is it**: an encapsulation endpoint for inter-site traffic
(e.g., Internet egress, cross-datacenter links).

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `name` | string | ✅ | unique identifier |
| `local_underlay_ip` | string | ✅ | this site's tunnel endpoint IP |
| `remote_underlay_ip` | string | ✅ | remote site's tunnel endpoint IP |
| `vni` | uint32 | ✅ | VXLAN identifier for this tunnel |
| `params` | map | ❌ | tunnel-specific parameters |

**Experiment**: create, then use as a route target, then try to delete
while referenced.

```powershell
dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/service-tunnel-experiments.yaml
# → CREATE st-exp-1
```

See [service-tunnel-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/service-tunnel-experiments.yaml).

---

## 3. Eni

**Tier 1** — depends on a VNet (`vnet_name` field).

**What is it**: an Elastic Network Interface — a virtual NIC attached
to a VM or container. The most important object in the DASH pipeline.

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `vnet_name` | string | ✅ | FK → which VNet this ENI belongs to |
| `mac_address` | string | ✅ | virtual MAC (aa:bb:cc:xx:xx:xx) |
| `underlay_ip` | string | ✅ | physical IP of the host |
| `admin_state` | string | ✅ | "up" or "down" |
| `placement_hint_dpu_ids` | []string | ❌ | preferred DPU(s) |
| `resimulate_flows` | bool | ❌ | force flow re-evaluation |
| `labels` | map | ❌ | operator tags |

**Wrong config** — apply without creating the VNet first:

```powershell
# This FAILS — vnet "vnet-does-not-exist" doesn't exist
dashctl apply -f - <<'EOF'
apiVersion: dashcenter.v1
kind: Eni
metadata: { name: eni-exp-bad }
spec: { vnet_name: vnet-does-not-exist, mac_address: "00:00:00:00:00:01", underlay_ip: "10.0.99.1", admin_state: up }
EOF
# → ERROR: eni.vnet_name="vnet-does-not-exist": not found in this namespace
```

**Why it fails**: ENI's `vnet_name` is a foreign key. dashd looks up
the VNet in the same namespace — it doesn't exist, so the PUT is rejected.

**Right config** — create the VNet first, then the ENI:

```powershell
dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/vnet-experiments.yaml
dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/eni-experiments.yaml
# → eni/eni-exp-ok CREATE (generation 1)
```

See [eni-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/eni-experiments.yaml)
for the admin_state toggle experiment.

---

## 4. VNet Mapping

**Tier 1** — depends on a VNet (`vnet_name` field).

**What is it**: tells the DPU how to encapsulate traffic for a specific
overlay IP within a VNet — "overlay IP X in VNet Y → send to underlay Z".

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `vnet_name` | string | ✅ | FK → which VNet owns this mapping |
| `ip_address` | string | ✅ | overlay IP (what the VM sees) |
| `underlay_ip` | string | ✅ | physical IP (where packets go) |
| `mac_address` | string | ✅ | destination MAC for encap |
| `action` | string | ✅ | "vnet_encap" / "service_tunnel" / "drop" |

**Experiment**: see [vnet-mapping-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/vnet-mapping-experiments.yaml).

---

## 5. ACL Policy

**Policy object** — depends on ENIs (`eni_names[]` field).

**What is it**: a firewall rule set bound to one or more ENIs.
Rules are evaluated by priority (lower = evaluated first).

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `stage` | string | ✅ | "inbound" or "outbound" |
| `priority` | int | ✅ | policy priority (lower = first) |
| `eni_names` | []string | ✅ | FK[] → which ENIs this applies to |
| `rules[]` | array | ✅ | match/action pairs (see sub-fields below) |

**Rule sub-fields**:

| Field | Type | Meaning |
|---|---|---|
| `priority` | int | rule priority within this policy |
| `action` | string | "allow" or "deny" |
| `src_prefix` | string | source CIDR ("10.0.0.0/8") — omit for any |
| `dst_prefix` | string | destination CIDR — omit for any |
| `src_port` | string | source port or range ("1024-65535") |
| `dst_port` | string | destination port ("443") |
| `protocol` | string | "tcp" / "udp" / "icmp" / "6" / "17" |

**Experiment**: wrong config (missing ENI) → right config → delete protection.

See [acl-policy-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/acl-policy-experiments.yaml).

---

## 6. Route Policy

**Policy object** — depends on ENIs + VNets/ServiceTunnels.

**What is it**: a routing table bound to ENIs. Each route maps a prefix
to a next-hop action (VNet encap, service tunnel, direct, or drop).

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `eni_names` | []string | ✅ | FK[] → which ENIs use this routing table |
| `routes[]` | array | ✅ | prefix→action pairs |

**Route sub-fields**:

| Field | Type | Meaning |
|---|---|---|
| `prefix` | string | IP CIDR to match ("192.168.0.0/16", "0.0.0.0/0") |
| `next_hop_type` | string | "vnet" / "service_tunnel" / "direct" / "drop" |
| `next_hop_target` | string | FK → Vnet or ServiceTunnel name (when type=vnet/service_tunnel) |
| `metric` | uint32 | tie-break for overlapping prefixes (lower wins) |

**Experiment**: missing service_tunnel → ECMP demo.

See [route-policy-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/route-policy-experiments.yaml).

---

## 7. HA Set

**Tier 0** — depends on DPU inventory (not other spec kinds).

**What is it**: a high-availability group of DPUs. Supports
active/standby (one serves, one waits) or active/active (both serve).

**Fields**:

| Field | Type | Required | Meaning |
|---|---|---|---|
| `mode` | string | ✅ | "active_standby" or "active_active" |
| `member_dpu_ids` | []string | ✅ | FK[] → DPU IDs in inventory |
| `virtual_ip` | string | ❌ | shared IP that floats between members |
| `flow_sync_endpoints` | []string | ❌ | flow replication addresses |

**Experiment**: wrong DPU IDs → right DPU IDs.

See [ha-set-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/ha-set-experiments.yaml).

---

## 8. EniBundle

**Bundle** — auto-expands to 5 specs: Vnet → ENI → VnetMapping →
RoutePolicy → AclPolicy.

**What is it**: a single YAML document that defines a complete ENI
pipeline and all its dependencies. The CLI auto-wires FK references
(`eni.vnet_name`, `route_policy.eni_names`) and creates objects in
the correct tier order.

**Experiment**:

```powershell
dashctl apply -f deploy/test-setup/scenarios/hands-on/config-specs/eni-bundle-experiments.yaml
# → 5 CREATE (vnet → eni → mapping → route → acl)
```

See [eni-bundle-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/eni-bundle-experiments.yaml).

---

## 9. AclBundle

**Bundle** — auto-expands to: Vnet → ENI → AclPolicy.

See [acl-bundle-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/acl-bundle-experiments.yaml).

---

## 10. RouteBundle

**Bundle** — auto-expands to: Vnet → ServiceTunnel → ENI → RoutePolicy.

See [route-bundle-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/route-bundle-experiments.yaml).

---

## 11. HaBundle

**Bundle** — auto-expands to: HaSet.

See [ha-bundle-experiments.yaml](../../../deploy/test-setup/scenarios/hands-on/config-specs/ha-bundle-experiments.yaml).

---

## Dependency quick reference

```
Tier 0 (create first):   vnet, service_tunnel, ha_set
Tier 1 (refs Tier 0):    eni→vnet, vnet_mapping→vnet
Policies (refs Tier 1):  acl_policy→eni, route_policy→eni+vnet+service_tunnel

Delete order: policies → Tier 1 → Tier 0 (reverse of create)
```

## See also

- [CLI_GUIDE.md §6](../../CLI_GUIDE.md) — bundle format + apply --force
- [CLI_GUIDE.md §10a](../../CLI_GUIDE.md) — delete orphan protection
- [referential-integrity-validation.md](../../dashd-features/referential-integrity-validation.md) — FK design doc
- [scenarios/hands-on/README.md](../../../deploy/test-setup/scenarios/hands-on/README.md) — file listing + clean-up
