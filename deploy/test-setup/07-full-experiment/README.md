# 07-full-experiment — 50 DPU At-Scale Experiment Topology

> **Scale**: 50 DPU sims + 3 HA controllers + 2-node etcd cluster + web console
> **Objects**: ~449 (25 VNets + 5 ServiceTunnels + 120 ENIs + 240 VnetMappings + 17 RoutePolicies + 32 AclPolicies + 10 HaSets)
> **Tenants**: 10 (bank, retail, media, iot, analytics, telecom, health, fintech, gaming, logistics)
> **ENIs per DPU**: 2–3 (1 tenant + 1 mgmt + optional 1 dmz on web/api DPUs)
> **Shared resources**: vnet-mgmt, vnet-dmz, vnet-monitoring, vnet-backup, vnet-shared-svc
> **Shared policies**: platform-ssh, platform-monitor, 5 tier-specific ACLs, 5 staging-restricted
> **Purpose**: at-scale experiments, stress testing, multi-tenant + shared-policy validation

## Architecture

```
                    ┌─────────────────────────┐
                    │   Browser :3000 (dashw)  │
                    └────────────┬────────────┘
                                 │
          ┌──────────────────────┼──────────────────────┐
          │                      │                      │
     dashd-1 :28443         dashd-2 :28453         dashd-3 :28463
     (leader-elected)        (standby)              (standby)
          │                      │                      │
          └──────────┬───────────┴──────────┬───────────┘
                     │                      │
              etcd-1 :12379          etcd-2 :12380
              (2-node cluster for redundancy)
                     │
     ┌───────────────┼───────────────┐
     │               │               │
  50 × dash-sim DPUs (dc-exp-sim-01..50)
  25 appliances × 2 slots each
  5 zones: us-west-2a/b, us-east-1a/b, eu-west-1a
```

## Quick start

```powershell
cd deploy/test-setup/07-full-experiment

# 1. Build + start (takes ~2 min for 56 containers)
pwsh ./start-fleet.ps1 -WithConsole

# 2. Provision ~160 objects
pwsh ./provision.ps1

# 3. Open web console
start http://localhost:3000

# 4. Verify
dashctl dpu list -o table --endpoint http://localhost:28443 --insecure
dashctl get eni -o wide --endpoint http://localhost:28443 --insecure
```

## Ports

| Service | Host port | Container port |
|---|---|---|
| dashd-1 REST | 28443 | 8443 |
| dashd-1 gRPC | 29443 | 9443 |
| dashd-1 Admin | 27443 | 7443 |
| dashd-2 REST | 28453 | 8443 |
| dashd-2 gRPC | 29453 | 9443 |
| dashd-2 Admin | 27453 | 7443 |
| dashd-3 REST | 28463 | 8443 |
| dashd-3 gRPC | 29463 | 9443 |
| dashd-3 Admin | 27463 | 7443 |
| dashw Console | 3000 | 3000 |
| etcd-1 | 12379 | 2379 |
| etcd-2 | 12380 | 2379 |
| DPU sims | (internal) | 50051 each |

## Object distribution

| Kind | Count | Design |
|---|---|---|
| VNets | 25 | 10 tenant-prod + 10 tenant-staging + 5 shared (mgmt, monitoring, backup, shared-svc, dmz) |
| ServiceTunnels | 5 | egress, cross-DC, backup, monitoring, mgmt VPN |
| ENIs | 120 | 50 tenant (1/DPU) + 50 mgmt (1/DPU) + 20 dmz (web+api DPUs get 3) |
| VnetMappings | 240 | 2 overlay IPs per tenant ENI + 2 per mgmt ENI + 2 per dmz ENI |
| RoutePolicies | 17 | 10 tenant + 1 shared-mgmt + 1 shared-dmz + 5 per-tier |
| AclPolicies | 32 | 10 tenant-in + 10 tenant-out + 2 platform (ssh+monitor) + 5 tier (web/api/db/cache/worker) + 5 staging-restricted |
| HaSets | 10 | 5 intra-appliance (active_standby) + 5 cross-zone (active_active) |
| **Total** | **449** | |

### ENIs per DPU

| DPUs | Tenant tier | ENIs/DPU | Types |
|---|---|---|---|
| sim-01..10 | bank+retail web/api | **3** | 1 tenant + 1 mgmt + 1 dmz |
| sim-11..20 | media+iot web/api+db | **2–3** | 1 tenant + 1 mgmt (+ 1 dmz on web/api) |
| sim-21..50 | db/cache/worker tiers | **2** | 1 tenant + 1 mgmt |

### Shared policy design

| Policy | Scope | Bound to | Rules |
|---|---|---|---|
| `acl-platform-ssh` | all mgmt ENIs (50) | SSH access mgmt only | port 22+2222 from 10.10.0.0/16 |
| `acl-platform-monitor` | all mgmt ENIs (50) | SNMP+ICMP+Prometheus | 161/udp + icmp + 9090+9100/tcp |
| `acl-tier-web` | all web-tier ENIs (10) | HTTP/HTTPS/8080-8089 | 3 allow + deny |
| `acl-tier-api` | all api-tier ENIs (10) | Internal API ports | 8443+9443 from internal |
| `acl-tier-db` | all db-tier ENIs (10) | Database ports | PostgreSQL+MySQL from 192.168/16 |
| `acl-tier-cache` | all cache-tier ENIs (10) | Cache ports | Redis+Memcached from 192.168/16 |
| `acl-tier-worker` | all worker-tier ENIs (10) | Message queue ports | Kafka+RabbitMQ from 10/8 |
| `rp-platform-mgmt` | all mgmt ENIs (50) | Mgmt routing | 10.10/16 → mgmt, 10.20/16 → monitoring |
| `rp-platform-dmz` | all dmz ENIs (20) | DMZ routing | 172.16/12 → dmz, 0/0 → egress tunnel |

## etcd redundancy

This topology uses a **2-node etcd cluster** instead of a single instance.
Both etcd nodes are configured as peers (`--initial-cluster=etcd-1=...,etcd-2=...`).

- **Normal operation**: both nodes serve reads/writes; dashd connects to both endpoints
- **One node failure**: surviving node continues serving (but cannot elect a new leader if the other was leader — 2-node clusters need manual intervention for split-brain)
- **For production**: use 3 or 5 etcd nodes for proper quorum-based fault tolerance

Test etcd health:
```powershell
docker exec dc-exp-etcd-1 etcdctl endpoint health --endpoints=http://etcd-1:2379,http://etcd-2:2379
```

## Scripts

| Script | Purpose |
|---|---|
| `start-fleet.ps1` | Build + start 56 containers + wait for leader |
| `stop-fleet.ps1` | Tear down (with volume cleanup) |
| `provision.ps1` | Apply ~160 objects in dependency order |
| `cleanup-data.ps1` | Delete all provisioned objects |
| `show-leader.ps1` | Show which dashd is the leader |

## Resource requirements

- **RAM**: ~6–8 GB (50 sims × ~50 MB + 3 dashd × ~100 MB + etcd + dashw)
- **CPU**: 4+ cores recommended
- **Disk**: ~3 GB for images
- **Docker**: Desktop or Engine + Compose v2

## Differences from 05-full-console

| Aspect | 05-full-console | 07-full-experiment |
|---|---|---|
| DPU sims | 10 | **50** |
| etcd nodes | 1 | **2** (clustered) |
| Tenants | 8 | **10** |
| ENIs | 30 | **120** (2-3 per DPU) |
| VnetMappings | ~30 | **240** (2 per ENI) |
| AclPolicies | ~24 | **32** (tenant+platform+tier+staging) |
| RoutePolicies | ~12 | **17** (tenant+platform+tier) |
| HaSets | 3 | **10** (intra+cross-zone) |
| Total objects | ~157 | **~449** |
| Shared policies | 0 | **9** (ssh, monitor, 5 tiers, mgmt, dmz) |
| Appliances | 5 | **25** |
| Zones | 3 | **5** |
