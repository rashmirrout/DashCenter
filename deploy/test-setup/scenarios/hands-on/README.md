# Hands-On Scenario Library

Copy-paste-ready YAML experiments for every DashCenter spec kind and
bundle kind. Each file contains wrong config (shows the error + why),
right config (explains every field), and clean-up instructions.

## Prerequisites

A running DashCenter fleet. Any topology works:

```powershell
# Quick start with 05-full-console:
cd deploy/test-setup/05-full-console
pwsh ./start-fleet.ps1
pwsh ./provision.ps1
```

Set up your CLI:

```powershell
$env:DASHCTL_ENDPOINT = "http://localhost:28443"
$env:DASHCTL_ADMIN_ENDPOINT = "http://localhost:27443"
```

---

## Folder structure

```
hands-on/
├── README.md                  ← this file
├── bootstrap/                 ← tier-ordered manifests (run once to set up a baseline)
│   ├── 00-vnets.yaml          Tier 0: 2 VNets
│   ├── 01-service-tunnels.yaml Tier 0: 1 ServiceTunnel
│   ├── 02-enis.yaml           Tier 1: 3 ENIs (depend on VNets)
│   ├── 03-vnet-mappings.yaml  Tier 1: 3 VnetMappings (depend on VNets)
│   ├── 04-route-policies.yaml Policies: 2 RoutePolicies (depend on ENIs + VNets)
│   ├── 05-acl-policies.yaml   Policies: 3 AclPolicies (depend on ENIs)
│   └── 06-ha-sets.yaml        HA: 1 HaSet (depends on DPU inventory)
└── config-specs/              ← per-kind experiments (run individually)
    ├── vnet-experiments.yaml
    ├── eni-experiments.yaml
    ├── vnet-mapping-experiments.yaml
    ├── acl-policy-experiments.yaml
    ├── route-policy-experiments.yaml
    ├── service-tunnel-experiments.yaml
    ├── ha-set-experiments.yaml
    ├── eni-bundle-experiments.yaml
    ├── acl-bundle-experiments.yaml
    ├── route-bundle-experiments.yaml
    └── ha-bundle-experiments.yaml
└── full-specs/                ← production-style dependency-rich scenarios
    ├── eni-full.yaml
    ├── vnet-full.yaml
    ├── route-full.yaml
    ├── mapping-full.yaml
    ├── acl-full.yaml
    ├── service-tunnel-full.yaml
    ├── private-link-full.yaml
    ├── ha-full.yaml
    ├── test-roundtrip.ps1
    ├── test-roundtrip.sh
    └── README.md
```

---

## Quick start: bootstrap

Apply the baseline in order (Tier 0 → Tier 1 → Policies → HA):

```powershell
$base = "deploy/test-setup/scenarios/hands-on/bootstrap"
dashctl apply -f $base/00-vnets.yaml
dashctl apply -f $base/01-service-tunnels.yaml
dashctl apply -f $base/02-enis.yaml
dashctl apply -f $base/03-vnet-mappings.yaml
dashctl apply -f $base/04-route-policies.yaml
dashctl apply -f $base/05-acl-policies.yaml
dashctl apply -f $base/06-ha-sets.yaml
```

Or apply the entire directory at once:

```powershell
dashctl apply -f deploy/test-setup/scenarios/hands-on/bootstrap/
```

Verify:

```powershell
dashctl get vnet -o table           # 2 VNets
dashctl get eni -o wide             # 3 ENIs
dashctl get vnet-mapping -o table   # 3 mappings
dashctl get route-policy -o table   # 2 policies
dashctl get acl-policy -o table     # 3 policies
dashctl get ha-set -o table         # 1 set
```

---

## Per-kind experiments

Each file in `config-specs/` is self-contained. Read the comments at
the top of each file for instructions.

| File | What you learn | Key experiment |
|---|---|---|
| `vnet-experiments.yaml` | VNet basics + delete orphan protection | Delete a VNet while an ENI refs it → error |
| `eni-experiments.yaml` | ENI→VNet FK + admin_state toggle | Create ENI without VNet → error, then fix |
| `vnet-mapping-experiments.yaml` | Overlay→underlay mapping | Wrong vnet_name → error |
| `acl-policy-experiments.yaml` | ACL rules + ENI binding + delete protection | Delete ENI while ACL refs it → error |
| `route-policy-experiments.yaml` | Routes + next_hop types + ECMP | Missing service_tunnel target → error |
| `service-tunnel-experiments.yaml` | Tunnel as route target + delete protection | Delete tunnel while route refs it → error |
| `ha-set-experiments.yaml` | HA modes + DPU inventory FK | DPU ID not in inventory → error |
| `eni-bundle-experiments.yaml` | Full ENI pipeline in 1 file (5 objects) | --force overwrite + auto-wiring |
| `acl-bundle-experiments.yaml` | ACL + deps in 1 file (3 objects) | Auto-wired eni_names |
| `route-bundle-experiments.yaml` | Route + tunnel + deps (4 objects) | ServiceTunnel in a bundle |
| `ha-bundle-experiments.yaml` | HA set in 1 file | Simplest bundle |

---

## Full-spec experiments

Use `full-specs/` when you want larger, realistic manifests with dependencies
inlined (VNet + tunnel + ENI + mapping + route + ACL + HA where applicable).

Quick run:

```powershell
# PowerShell roundtrip (apply → verify → delete)
pwsh deploy/test-setup/scenarios/hands-on/full-specs/test-roundtrip.ps1 -Action test
```

```bash
# Bash roundtrip (apply → verify → delete)
./deploy/test-setup/scenarios/hands-on/full-specs/test-roundtrip.sh test
```

Key files:

| File | Focus |
|---|---|
| `eni-full.yaml` | ENI + all core dependencies |
| `vnet-full.yaml` | VNet-centric 3-tier scenario |
| `route-full.yaml` | all route next-hop variants (incl. ECMP) |
| `mapping-full.yaml` | all mapping actions (`vnet_encap`, `service_tunnel`, `drop`) |
| `acl-full.yaml` | inbound/outbound ACL coverage |
| `service-tunnel-full.yaml` | all tunnel actions (`nat`, `inspect`, `privatelink`, `ipsec`, `scrub`, `vxlan_peer`) |
| `private-link-full.yaml` | PrivateLink pattern via ServiceTunnel |
| `ha-full.yaml` | HA pair with dependencies |

---

## Clean up everything

```powershell
# Delete in reverse tier order (policies → Tier 1 → Tier 0)
dashctl delete acl-policy acl-lab-web-inbound acl-lab-web-outbound acl-lab-db-inbound --ignore-not-found
dashctl delete route-policy rp-lab-web rp-lab-db --ignore-not-found
dashctl delete ha-set ha-lab-web --ignore-not-found
dashctl delete vnet-mapping lab-web-192.168.50.1 lab-web-192.168.50.2 lab-db-192.168.51.1 --ignore-not-found
dashctl delete eni eni-lab-web-01 eni-lab-web-02 eni-lab-db-01 --ignore-not-found
dashctl delete service-tunnel st-internet-egress --ignore-not-found
dashctl delete vnet lab-web lab-db --ignore-not-found
```

---

## See also

- [Tutorial: spec-hands-on.md](../../../docs/tutorial/modules/spec-hands-on.md) — prose walkthrough of each experiment
- [CLI_GUIDE.md §6](../../../docs/CLI_GUIDE.md) — bundle spec format + apply --force docs
- [referential-integrity-validation.md](../../../docs/dashd-features/referential-integrity-validation.md) — FK design doc
