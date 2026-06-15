# 06-fleet-ui-diagnostics — Full DashCenter Stack + Diagnostics Lab

> **What it is**: a clone of [`05-full-console`](../05-full-console/)
> with **three extra fixtures** layered on top of the 157-object
> superset and a dedicated **Lab 13** that drives every PE-1
> `DiagnosticsService` RPC end-to-end. Same single-command Docker
> fleet (10 simulated DPUs + 3 HA-elected dashd controllers + dashw),
> different ports so it can run **alongside 05** without conflict.
>
> **Result**: `http://localhost:3001` opens the same web console as 05
> but with the diagnostics-friendly objects (`acl-diag-chain`,
> `acl-diag-dead-rules`, `rp-diag-overlap`) pre-loaded so every
> diagnostic RPC returns interesting, deterministic output.
>
> **When to use it**: learning the `DiagnosticsService` surface,
> operator training on `trace-flow` / `explain-match` /
> `explain-drift` / `acl-hit-stats` / `trigger-resimulation`,
> integration testing diagnostic clients without contaminating the
> 05 fleet.
>
> **dashctl note**: the `dashctl diag *` subcommands are still in
> flight. Every example in [manual-handson.md](manual-handson.md)
> Lab 13 uses `curl` against the REST surface; dashctl equivalents
> will be added once they land.

---

## Topology

```
┌──────────────────────────────────────────────────────────────────┐
│                    Host Machine                                  │
│                                                                  │
│  Browser → http://localhost:3001 ─────────────────────────┐      │
│                                                           │      │
│  ┌─────────────────────────────────────────────────────── │ ──┐  │
│  │                 Docker Network: dc-diag-net         │   │  │
│  │                                                       ▼   │  │
│  │  ┌──────────────────────────────────────────────────────┐ │  │
│  │  │         dashw (Web Console BFF + SPA)                │ │  │
│  │  │         :3000 → go:embed SPA + REST proxy            │ │  │
│  │  │         + aggregation + WS↔gRPC bridge               │ │  │
│  │  └────────┬────────────────┬────────────────┬───────────┘ │  │
│  │           │ REST           │ Admin          │ gRPC        │  │
│  │           ▼                ▼                ▼             │  │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐          │  │
│  │  │  dashd-1   │  │  dashd-2   │  │  dashd-3   │          │  │
│  │  │  (leader?) │  │  (follower)│  │  (follower)│          │  │
│  │  │  REST 8443 │  │  REST 8443 │  │  REST 8443 │          │  │
│  │  │  Admin 7443│  │  Admin 7443│  │  Admin 7443│          │  │
│  │  │  gRPC 9443 │  │  gRPC 9443 │  │  gRPC 9443 │          │  │
│  │  └──────┬─────┘  └──────┬─────┘  └──────┬─────┘          │  │
│  │         │               │               │                │  │
│  │         └───────────────┼───────────────┘                │  │
│  │                         ▼                                 │  │
│  │                   ┌──────────┐                            │  │
│  │                   │   etcd   │                            │  │
│  │                   │   :2379  │                            │  │
│  │                   └──────────┘                            │  │
│  │                                                           │  │
│  │  ┌──────────────────────────────────────────────────────┐ │  │
│  │  │              10 Simulated DPUs                       │ │  │
│  │  │  sim-01  sim-02  sim-03  sim-04  sim-05              │ │  │
│  │  │  sim-06  sim-07  sim-08  sim-09  sim-10              │ │  │
│  │  │  (each: gRPC :50051 + admin :8080, internal only)    │ │  │
│  │  └──────────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

## Host Port Map

| Service | Host URL | Purpose |
|---|---|---|
| **dashw console** | `http://localhost:3001` | **← This is all you need** |
| dashd-1 REST | `http://localhost:38443` | Direct dashd access (optional) |
| dashd-1 Admin | `http://localhost:37443` | Direct admin access (optional) |
| dashd-2 REST | `http://localhost:38453` | |
| dashd-3 REST | `http://localhost:38463` | |
| etcd | `http://localhost:13379` | Direct etcd access (optional) |

## Quick Start (3 scripts)

The fleet has the same lifecycle pattern as `04-ha-fleet`, packaged as three
script pairs:

| Phase | Script (PS / sh) | What it does |
|---|---|---|
| **Start** | `start-fleet.{ps1,sh}` | builds images, brings up fleet, waits for leader, sets dashctl context |
| **Provision** | `provision.{ps1,sh}` | applies `manifest/*.yaml` (00–11) via `dashctl apply -R -f` (or `bootstrap.py` fallback) — including the 3 diagnostics-lab fixtures in `11-diagnostics-fixtures.yaml` |
| **Stop** | `stop-fleet.{ps1,sh}` | `docker compose down [-v] [--rmi local]` |

Plus two helpers:

- `show-leader.{ps1,sh}` — print leader of all 3 dashd nodes
- `cleanup-data.{ps1,sh}` — wipe the loaded dataset without tearing the fleet down

### Linux / macOS / WSL

```bash
cd deploy/test-setup/06-fleet-ui-diagnostics

# 1. Start the core fleet (etcd + 10 sims + 3 dashd). Add --with-console for dashw.
./start-fleet.sh                          # build + up + wait + dashctl context
# (use --skip-build for cached images, --with-console to also start the dashw web BFF)

# 2. Confirm leader election on all 3 controllers
./show-leader.sh

# 3. Load the rich 160-object superset (157 from 05 + 3 diagnostics fixtures)
./provision.sh                            # dashctl preferred, bootstrap.py fallback

# 4. (optional) Open the web console (only if you launched with --with-console)
xdg-open http://localhost:3001

# Iterate: wipe data + reload, fleet stays up (saves ~3 min of restart)
./cleanup-data.sh && ./provision.sh

# Clean teardown
./stop-fleet.sh                           # remove volumes (clean slate)
./stop-fleet.sh --keep-volumes            # keep etcd state across restarts
```

### Windows / PowerShell

```powershell
cd deploy\test-setup\06-fleet-ui-diagnostics

# 1. Start the core fleet (add -WithConsole for dashw)
pwsh ./start-fleet.ps1                    # build + up + wait + dashctl context
# (use -SkipBuild for cached images, -WithConsole to also start the dashw web BFF)

# 2. Confirm leader election
pwsh ./show-leader.ps1

# 3. Load the rich 160-object superset (157 from 05 + 3 diagnostics fixtures)
pwsh ./provision.ps1                      # dashctl preferred, bootstrap.py fallback

# 4. (optional) Open the web console
Start-Process http://localhost:3001

# Iterate: wipe data + reload
pwsh ./cleanup-data.ps1; pwsh ./provision.ps1

# Clean teardown
pwsh ./stop-fleet.ps1                     # remove volumes
pwsh ./stop-fleet.ps1 -KeepVolumes        # keep etcd state
```

### Minimal one-liner (if you don't care about iteration)

```bash
./start-fleet.sh --with-console && ./provision.sh && open http://localhost:3001
```

```powershell
pwsh ./start-fleet.ps1 -WithConsole; pwsh ./provision.ps1; Start-Process http://localhost:3001
```

## Pre-loaded Data

`bootstrap.py` PUTs a **rich superset of 160 objects across 3 namespaces**
(`default`, `edge`, `staging`) — exercising every dashw view and resource kind.

| Kind          | Count | Highlights |
|---|---|---|
| **Vnet**          | **17** | 14 `default` (5 tenants × bank/retail/media/iot/analytics + gaming + shared) + 2 `edge/cdn-pop`,`cdn-origin` + 1 `staging/bank-staging-web`. VNIs 1001–3001 |
| **ENI**           | **46** | 40 `default` + 5 multi-ns + **1 quarantine** (`admin_state: down`) + **1 gen=2** (`eni-bank-web-04` re-PUT with `resimulate_flows: true`) |
| **VnetMapping**   | **45** | Mix of `vnet_encap` + `service_tunnel` actions across the underlay |
| **RoutePolicy**   | **20** | Per-tenant defaults + **3-way ECMP** (weighted 50/30/20) + gaming blackhole (drop precedence over scrub) + **`rp-diag-overlap`** (4 overlapping prefixes — /0, /8, /24, /32 — across all 4 next-hop types for the longest-prefix demo) |
| **AclPolicy**     | **22** | Tenant inbound/outbound + multi-ns + advanced: numeric protos (`6`,`1`,`17`,`58`), port ranges (`7777-7800`,`1024-65535`), `src_ports`, `allow_and_continue` chains, **+ `acl-diag-chain` (multi-stage explain-match demo) + `acl-diag-dead-rules` (every rule covers TEST-NET ranges)** |
| **ServiceTunnel** | **6**  | `nat` / `inspect` / `privatelink` / `ipsec` / `scrub` / `vxlan_peer` |
| **HaSet**         | **4**  | 2 active/standby (bank, retail) + 1 active/active (shared-services) + 1 cross-rack (gaming, sim-09 ↔ sim-10) |

### What this exercises in the console

| dashw view | What you'll see |
|---|---|
| Dashboard | 10/10 DPUs UP, 46 ENIs, 17 Vnets, 20 ACLs, 19 routes |
| VnetView | Dual-plane canvas for any vnet, multi-DPU underlay hexagons |
| RoutingView | ECMP fan-out, blackhole vs fallback metric precedence |
| PolicyView | Numeric proto badges, port-range pills, `allow_and_continue` chains |
| TunnelView | 6 distinct tunnel categories with overlay/underlay toggle |
| FleetView | Uneven capacity heatmap (dpu-sim-06 at 6 ENIs, others 4–5) |
| DpuView | dpu-sim-06 hosts default + staging (mixed ns); dpu-sim-08 has the quarantine ENI |
| HealthView | dashd leader + 4 HA sets with member roles + VIPs |
| AdminOpsView | CRUD form, batch YAML paste, generation tracking |
| Multi-namespace | Per-ns filter narrows the fleet; cross-ns refs are admission-rejected |
| CommandView | `dashctl` catalog with live CLI preview |
| FlowTraceView | Happy path, quarantine drop, port-range allow/deny |
| DebugView | Raw `/api/v1/*`, `/api/admin/*`, `/api/sim/*` callers |
| AuditView | Stream of every bootstrap PUT with diffable generations |

See [manual-handson.md](manual-handson.md) for the 13-lab guided tour, including the **PE-1 DiagnosticsService deep dive (Lab 13)**.

### Reset state without restarting the fleet

```bash
# Wipe just the application objects (default + edge + staging namespaces),
# then re-load — the fleet, etcd, sims stay running.
./cleanup-data.sh && ./provision.sh                # bash
pwsh ./cleanup-data.ps1; pwsh ./provision.ps1      # PowerShell
```

Or re-run `provision.{ps1,sh}` directly — `PUT`s are idempotent (objects already
present get their generation bumped, nothing is destroyed).

## Tear Down

```bash
./stop-fleet.sh                     # graceful down + remove volumes (clean slate)
./stop-fleet.sh --keep-volumes      # keep etcd state for next ./start-fleet.sh
./stop-fleet.sh --remove-images     # deep clean (also drops built images)
```

```powershell
pwsh ./stop-fleet.ps1
pwsh ./stop-fleet.ps1 -KeepVolumes
pwsh ./stop-fleet.ps1 -RemoveImages
```

## Script Inventory

| Script (PS / sh) | Purpose | Knobs |
|---|---|---|
| `start-fleet.{ps1,sh}` | build images, bring up etcd + 10 sims + 3 dashd, wait for leader, set dashctl context | `-WithConsole` / `--with-console`, `-SkipBuild`, `-SkipContext`, `-ReadyTimeoutSec` |
| `show-leader.{ps1,sh}` | print `LEADER` table for the 3 dashd controllers | — |
| `provision.{ps1,sh}` | load manifests via `dashctl apply -R -f manifest/` (fallback: `bootstrap.py`) | `-UseBootstrap`, `-DryRun`, `-Endpoint` |
| `cleanup-data.{ps1,sh}` | reverse of provision (fleet stays up); `dashctl delete -R -f manifest/` with REST fallback | `-UseRest`, `-Endpoint` |
| `stop-fleet.{ps1,sh}` | `docker compose down [-v] [--rmi local]` | `-KeepVolumes`, `-RemoveImages` |

All scripts auto-discover a `dashctl` binary in this directory → sibling
`04-ha-fleet/` → `PATH`. If none is found, the bootstrap.py / raw REST fallback
kicks in so the workflow still works end-to-end.

## See Also

- [Manual Hands-On Lab](manual-handson.md) — 12-lab guided tour (~45 min)
- [`manifest/bootstrap.py`](manifest/bootstrap.py) — Python loader for the 160-object superset (used as provision.sh fallback)
- [`manifest/*.yaml`](manifest/) — dashctl-ready YAML manifests (00–10), applied recursively by provision.sh
- [04-ha-fleet](../04-ha-fleet/README.md) — same HA controllers, no console (this repo's scripts mirror that one's pattern)

## Docker Exec — in-container diagnostics

Both `dash-sim` and `dashd` images ship their respective operator CLIs
inside the container (Alpine-based runtime with shell access).

### dash-sim containers

```bash
# Enter any sim container:
docker exec -it dc-diag-sim-01 sh

# Inside — dash-sim-client talks to localhost:50051:
dash-sim-client ping --target localhost:50051
dash-sim-client dpu-counters --target localhost:50051 -o table
dash-sim-client dpu-counters --include-enis --target localhost:50051
dash-sim-client reset-counters --target localhost:50051
dash-sim-client kinds --target localhost:50051 -o table

# One-liner (no shell entry needed):
docker exec dc-diag-sim-01 dash-sim-client reset-counters --target localhost:50051
```

### dashd containers

```bash
# Enter any dashd container:
docker exec -it dc-diag-dashd-1 sh

# Inside — dashctl talks to localhost:8443:
dashctl version --endpoint http://localhost:8443 --insecure
dashctl counters --endpoint http://localhost:8443 --insecure
dashctl counters details --dpu=dpu-sim-01 --endpoint http://localhost:8443 --insecure
dashctl counters clear --reset-sim --endpoint http://localhost:8443 --insecure

# One-liner:
docker exec dc-diag-dashd-1 dashctl counters --endpoint http://localhost:8443 --insecure
```
