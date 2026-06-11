# 05-full-console — Full DashCenter Stack with Web Console

> **What it is**: a single-command Docker fleet that runs the **complete
> DashCenter stack**: 10 simulated DPUs, 3 HA-elected dashd controllers
> (etcd-backed), and the **dashw web console** — all on one host.
>
> **Result**: `http://localhost:3000` opens the full web console with
> live fleet visibility, topology, CRUD admin ops, flow trace,
> command view, and all 20 visualization views.
>
> **When to use it**: full end-to-end demo, operator training,
> integration testing the web console against a realistic fleet.

---

## Topology

```
┌──────────────────────────────────────────────────────────────────┐
│                    Host Machine                                  │
│                                                                  │
│  Browser → http://localhost:3000 ─────────────────────────┐      │
│                                                           │      │
│  ┌─────────────────────────────────────────────────────── │ ──┐  │
│  │                 Docker Network: dc-console-net         │   │  │
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
| **dashw console** | `http://localhost:3000` | **← This is all you need** |
| dashd-1 REST | `http://localhost:28443` | Direct dashd access (optional) |
| dashd-1 Admin | `http://localhost:27443` | Direct admin access (optional) |
| dashd-2 REST | `http://localhost:28453` | |
| dashd-3 REST | `http://localhost:28463` | |
| etcd | `http://localhost:12379` | Direct etcd access (optional) |

## Quick Start

### Linux / macOS / WSL

```bash
cd deploy/test-setup/05-full-console

# Build all images and start the fleet
docker compose up -d --build

# Wait for leader election (~15s)
echo "Waiting for fleet..."
sleep 15

# Verify
curl -s http://localhost:27443/admin/health | jq .status
# → "ok"

# Open the web console
open http://localhost:3000    # macOS
xdg-open http://localhost:3000  # Linux
```

### Windows / PowerShell

```powershell
cd deploy\test-setup\05-full-console

# Build and start
docker compose up -d --build

# Wait for leader
Start-Sleep 15

# Verify
(Invoke-RestMethod http://127.0.0.1:27443/admin/health).status
# → "ok"

# Open the web console
Start-Process http://localhost:3000
```

## Pre-loaded Data

The fleet ships with a **rich scenario** of ~230 objects across 2 namespaces:

| Kind | Count | Highlights |
|---|---|---|
| Vnet | 12 | 5 tenants + shared services, VNIs 1001–1902 |
| ENI | 40 | Distributed across 10 DPUs (4 per DPU, 2 vnets each) |
| VnetMapping | 40 | vnet_encap + service_tunnel actions |
| RoutePolicy | 15 | vnet/service_tunnel/direct/drop + ECMP |
| AclPolicy | 15 | 150 rules — inbound/outbound, allow/deny |
| ServiceTunnel | 6 | NAT, NSG, PrivateLink, VPN |
| HaSet | 4 | 2 active/standby + 1 active/active + 1 cross-rack |

## Tear Down

```bash
docker compose down -v        # Stop + remove volumes
docker compose down -v --rmi local  # Also remove images
```

## See Also

- [Manual Hands-On Lab](manual-handson.md) — step-by-step tutorial
- [04-ha-fleet](../04-ha-fleet/README.md) — HA-only fleet (no console)
- [dashw HLD](../../../specs/HLD/dashw-web-hld.md)