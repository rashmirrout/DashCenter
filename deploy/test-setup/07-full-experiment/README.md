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

# 2. Provision ~449 objects (PowerShell wrapper)
pwsh ./provision.ps1 -MaxRetries 10 -MaxWaitSeconds 180 -Force

# Optional: directly run the Bash provisioner with explicit retry controls
bash ./provision.sh --endpoint http://localhost:28443 --max-retries 10 --max-wait-seconds 180 --force

# 3. Open web console
start http://localhost:3000

# 4. Verify
dashctl dpu list -o table --endpoint http://localhost:28443 --insecure
dashctl get eni -o wide --endpoint http://localhost:28443 --insecure
```

Linux/macOS quick start:

```bash
cd deploy/test-setup/07-full-experiment

./start-fleet.sh --with-console

# Provision with retries for transient leader flips / restarts
./provision.sh --endpoint http://localhost:28443 --max-retries 10 --max-wait-seconds 180 --force

./show-leader.sh
```

## Experimenter Flow

Use this sequence when running workshops, hands-on sessions, or validation
loops on the 07-full-experiment topology.

### 1) Bring up fleet

PowerShell:

```powershell
cd deploy/test-setup/07-full-experiment
pwsh ./start-fleet.ps1 -WithConsole
```

Linux/macOS:

```bash
cd deploy/test-setup/07-full-experiment
./start-fleet.sh --with-console
```

### 2) Provision with retry-safe apply

Use the retry flags so transient leader flips or short restarts do not fail
the full run.

```bash
./provision.sh --endpoint http://localhost:28443 --max-retries 10 --max-wait-seconds 180 --force
```

### 3) Verify baseline

```bash
dashctl get vnet -o table --endpoint http://localhost:28443 --insecure
dashctl get eni -o wide --endpoint http://localhost:28443 --insecure
dashctl get route-policy -o table --endpoint http://localhost:28443 --insecure
dashctl get acl-policy -o table --endpoint http://localhost:28443 --insecure
dashctl get ha-set -o table --endpoint http://localhost:28443 --insecure
```

### 4) Run guided spec experiments

Move to the shared hands-on library and run either per-kind learning specs or
full dependency-rich specs:

- Per-kind: `deploy/test-setup/scenarios/hands-on/config-specs/`
- Full specs: `deploy/test-setup/scenarios/hands-on/full-specs/`

Recommended order for new experimenters:

1. `vnet-experiments.yaml`
2. `eni-experiments.yaml`
3. `vnet-mapping-experiments.yaml`
4. `route-policy-experiments.yaml`
5. `acl-policy-experiments.yaml`
6. `service-tunnel-experiments.yaml`
7. `ha-set-experiments.yaml`

For integrated scenarios, run:

```bash
cd deploy/test-setup/scenarios/hands-on/full-specs
pwsh ./test-roundtrip.ps1 -Action test
# or
./test-roundtrip.sh test
```

### 5) Leader/health check during experiments

If apply operations are flaky, verify leader and admin health before retrying:

```bash
curl -s http://localhost:27443/admin/health
curl -s http://localhost:27443/admin/leader
```

### 6) Cleanup/reset

Object cleanup only:

```bash
./cleanup-data.sh
# or
pwsh ./cleanup-data.ps1
```

Full teardown:

```bash
./stop-fleet.sh
# or
pwsh ./stop-fleet.ps1
```

## Provision.sh options

`provision.sh` supports explicit flags (and keeps backward compatibility for
positional endpoint + env vars):

```bash
./provision.sh [endpoint] [--force]
./provision.sh --endpoint URL --admin-endpoint URL --force \
               --max-retries N --max-wait-seconds N
```

- `--endpoint`: dashd REST endpoint (default: `http://localhost:28443`)
- `--admin-endpoint`: health-check endpoint used before each apply attempt
- `--force`: pass `--force` to `dashctl apply`
- `--max-retries`: retry count per manifest for transient network errors (default `6`)
- `--max-wait-seconds`: max wait for `/admin/health` before each attempt (default `90`)

Transient errors retried automatically include:
- `connection reset by peer`
- `EOF`
- `i/o timeout`
- `connection refused`

This is intended for at-scale provisioning where leader election or short
container restarts can interrupt long apply runs.

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
| `start-fleet.sh` | Linux/macOS equivalent of `start-fleet.ps1` |
| `stop-fleet.ps1` | Tear down (with volume cleanup) |
| `stop-fleet.sh` | Linux/macOS equivalent of `stop-fleet.ps1` |
| `provision.ps1` | PowerShell wrapper to apply ~449 objects in dependency order |
| `provision.sh` | Bash provisioner with health-gated retry options (`--max-retries`, `--max-wait-seconds`) |
| `cleanup-data.ps1` | Delete all provisioned objects |
| `cleanup-data.sh` | Linux/macOS equivalent of `cleanup-data.ps1` |
| `show-leader.ps1` | Show which dashd is the leader |
| `show-leader.sh` | Linux/macOS equivalent of `show-leader.ps1` |

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
