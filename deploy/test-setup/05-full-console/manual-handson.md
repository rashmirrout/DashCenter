# DashCenter Web Console — Manual Hands-On Lab

> **Duration:** ~30 minutes
> **Prerequisites:** Docker Desktop (or Docker Engine + Compose v2),
> a web browser, ~4GB free RAM, ~2GB free disk.
> **Result:** A fully operational DashCenter fleet with 10 simulated
> DPUs, 3 HA controllers, and a web console — all running locally.

---

## Lab Overview

In this lab you will:

1. **Deploy** the full DashCenter stack with one command
2. **Explore** the web console — Dashboard, Fleet, DPU, Vnet views
3. **Create resources** — Vnets, ENIs, ACL policies through the UI
4. **Visualize** the Vnet dual-plane canvas with overlay/underlay
5. **Trace a flow** — simulate a packet through the DASH pipeline
6. **Test HA** — kill a dashd leader and watch failover
7. **Use the Command View** — execute dashctl commands from the browser
8. **Inspect the Debug tools** — raw API caller, admin endpoints
9. **Clean up** — tear down the entire stack

---

## Step 1: Deploy the Full Stack

### 1.1 Start the fleet

```bash
# Navigate to the deployment directory
cd deploy/test-setup/05-full-console

# Build all images and start (first run takes 3-5 minutes for image builds)
docker compose up -d --build
```

**What happens:**
- etcd starts and waits for health check (5s)
- 10 DPU simulators (`dash-sim-01` through `dash-sim-10`) start
- 3 dashd controllers start, connect to etcd, and elect a leader (~10s)
- dashw web console starts, connects to dashd-1

### 1.2 Verify the fleet is healthy

```bash
# Wait for leader election
sleep 15

# Check fleet health
curl -s http://localhost:27443/admin/health | python -m json.tool
```

**Expected output:**
```json
{
    "status": "ok",
    "leader": true,
    "leader_id": "dashd-1",
    "dpus": [
        {"id": "dpu-sim-01", "state": "DPU_STATE_UP", "last_seen": "..."},
        {"id": "dpu-sim-02", "state": "DPU_STATE_UP", "last_seen": "..."},
        ...
        {"id": "dpu-sim-10", "state": "DPU_STATE_UP", "last_seen": "..."}
    ]
}
```

All 10 DPUs should show `DPU_STATE_UP`.

### 1.3 Open the Web Console

Open your browser and navigate to:

> **http://localhost:3000**

You should see the DashCenter Web Console landing page with the
dark "Network Dark" theme.

---

## Step 2: Explore the Dashboard

The Dashboard is the first view you see. It shows:

| Panel | What to look for |
|---|---|
| **Fleet Heartbeat** | 10 DPU segments in the ring, all green |
| **Stats Cards** | 10 DPUs, 0 ENIs, 0 Vnets (empty fleet) |
| **Capacity Gauges** | All at 0% (nothing deployed yet) |
| **Mini Topology** | 10 hexagonal DPU nodes, all green |

**✅ Checkpoint:** All 10 DPUs show as healthy (green).

---

## Step 3: Explore the Fleet View

Click **"Fleet"** in the sidebar (or press `G F`).

### 3.1 Topology Graph

You should see an interactive topology with:
- 10 DPU hexagons (all green = healthy)
- No Vnet or ENI nodes yet (we haven't created any)

**Try:**
- Zoom in/out with mouse wheel
- Pan by clicking and dragging the background
- Click a DPU hexagon → navigates to DPU Detail View

### 3.2 Fleet Table

Below the topology, the fleet table shows:

```
│ DPU        │ State │ IP      │ Capacity │ ENIs │ Last Seen │
│────────────┼───────┼─────────┼──────────┼──────┼───────────│
│ dpu-sim-01 │ ● UP  │ (sim)   │ 0%       │ 0/8  │ 2s ago    │
│ dpu-sim-02 │ ● UP  │ (sim)   │ 0%       │ 0/8  │ 1s ago    │
│ ...        │       │         │          │      │           │
│ dpu-sim-10 │ ● UP  │ (sim)   │ 0%       │ 0/8  │ 3s ago    │
```

**✅ Checkpoint:** All 10 DPUs visible in table and topology.

---

## Step 4: Create Resources Through the Console

### 4.1 Create a Vnet

1. Click **"Admin Ops"** in the sidebar
2. Select resource type: **Vnet**
3. Fill in the form:
   - Namespace: `default`
   - Name: `vnet-prod`
   - VNI: `10001`
4. Click **"Apply"**

**Expected:** Toast notification "Vnet created (gen: 1)" + Vnet
appears in the Dashboard stats.

### 4.2 Create ENIs

Repeat for 4 ENIs (2 per DPU across 2 DPUs):

| ENI | Vnet | MAC | Placement hint |
|---|---|---|---|
| `eni-web-01` | `vnet-prod` | `aa:bb:cc:00:00:01` | `dpu-sim-01` |
| `eni-web-02` | `vnet-prod` | `aa:bb:cc:00:00:02` | `dpu-sim-01` |
| `eni-app-01` | `vnet-prod` | `aa:bb:cc:00:01:01` | `dpu-sim-02` |
| `eni-app-02` | `vnet-prod` | `aa:bb:cc:00:01:02` | `dpu-sim-02` |

For each:
1. Select resource type: **ENI**
2. Fill in: namespace, name, vnet_name, mac_address, admin_state: `up`
3. Add placement_hint_dpu_ids: the DPU ID
4. Click **"Apply"**

### 4.3 Create a Service Tunnel

1. Select resource type: **ServiceTunnel**
2. Fill in:
   - Name: `tunnel-prod-01`
   - local_underlay_ip: `10.0.0.1`
   - remote_underlay_ip: `10.0.0.2`
   - VNI: `10001`
3. Click **"Apply"**

### 4.4 Create an ACL Policy

1. Select resource type: **AclPolicy**
2. Fill in:
   - Name: `web-acl`
   - Stage: `inbound`
   - ENI names: `eni-web-01`
   - Add rules:
     - Priority 10, Action: `allow`, dst_ports: `80,443`, protocols: `tcp`
     - Priority 999, Action: `deny` (default deny)
3. Click **"Apply"**

**✅ Checkpoint:** Navigate back to Dashboard — should show:
10 DPUs, 4 ENIs, 1 Vnet. Capacity gauges show ~25% ENI usage on DPU-1 and DPU-2.

---

## Step 5: Visualize the Vnet — Dual-Plane Canvas

1. Click **"Fleet"** → click the **"vnet-prod"** card

You should see the **Dual-Plane Interactive Canvas**:

### Overlay Plane (top)
- Glowing cyan Vnet circle: "vnet-prod"
- Pulsing amber VNI badge: "VNI: 10001"
- 4 ENI nodes cascading below (eni-web-01, eni-web-02, eni-app-01, eni-app-02)

### Layer Divider
- Animated dashed line: "OVERLAY │ UNDERLAY"

### Underlay Plane (bottom)
- 2 DPU hexagons (dpu-sim-01, dpu-sim-02) — only DPUs hosting
  this Vnet's ENIs are shown
- Each hexagon shows 2 ENI sub-badges inside
- Tunnel edge between DPUs (if ServiceTunnel exists)
  with animated cyan particles

### Interactions to try:
- **Hover** a DPU hexagon → see tooltip with capacity bars
- **Click** a DPU hexagon → navigates to DPU Detail View
- **Click** a tunnel edge → opens Tunnel Detail Drawer
  (overlay/underlay sections)
- **Zoom** in/out, **pan** the canvas

### Data tabs below canvas:
- **ENIs** tab: table of 4 ENIs with MAC, DPU placement
- **Tunnels** tab: table showing tunnel-prod-01

**✅ Checkpoint:** You can see the overlay/underlay split, ENIs
connected to DPUs through the layer divider, and tunnel particles
flowing.

---

## Step 6: Trace a Flow

1. Click **"Flow Trace"** in the sidebar (or press `G` then `F T`)
2. Fill in the trace form:
   - Direction: `OUTBOUND`
   - ENI: `eni-web-01`
   - Src IP: `10.0.0.5`
   - Dst IP: `10.0.1.10`
   - Protocol: `tcp`
   - Src Port: `8080`
   - Dst Port: `443`
3. Click **"Simulate Flow"**

**Expected:** The animated pipeline shows the packet traversing:
1. **ENI Ingress** → eni-web-01 selected
2. **ACL Evaluation** → web-acl rule 10 ALLOWS (port 443 match)
3. **Route Lookup** → (if route policy exists)
4. **Vnet Mapping** → (if mapping exists)
5. **Verdict**: ALLOW or ENCAP

The result panel shows matched ACL rule, route, and final verdict.

**Try:** Change the dst_port to `22` (SSH) → should hit rule 999
(default deny) → verdict: DROP_ACL.

**✅ Checkpoint:** Flow trace works, shows different results for
different inputs.

---

## Step 7: Test HA — Kill the Leader

### 7.1 Find the current leader

Navigate to **"Health"** view in the sidebar. The Leader Panel shows
which dashd instance is the current leader (e.g., "dashd-1").

Or from the terminal:
```bash
for p in 27443 27453 27463; do
  echo "dashd @$p: $(curl -s http://localhost:$p/admin/leader)"
done
```

### 7.2 Kill the leader

```bash
# Kill dashd-1 (assuming it's the leader)
docker stop dc-console-dashd-1
```

### 7.3 Watch failover in the console

Within ~12 seconds (8s lease TTL + buffer):
- The **Health View** will show a new leader (dashd-2 or dashd-3)
- The **Dashboard** will briefly show a warning banner, then recover
- All data is preserved (etcd-backed state survives leader loss)

### 7.4 Verify data survived

Navigate to **Fleet View** → still shows 10 DPUs, 4 ENIs, 1 Vnet.

### 7.5 Restart the killed dashd

```bash
docker start dc-console-dashd-1
# dashd-1 rejoins as follower (does NOT steal leadership)
```

**✅ Checkpoint:** HA failover works. Data survives leader loss.
Console auto-recovers.

---

## Step 8: Use the Command View

1. Click **"Commands"** in the sidebar
2. In the command catalog (left panel), find **"get"**
3. Select it → the builder shows:
   - Kind: select `vnet`
   - Click **"Execute"**

**Expected:** The output panel shows your `vnet-prod` resource in
table/JSON/YAML format.

**Try more commands:**
- `get eni` → shows all 4 ENIs
- `get aclpolicy` → shows `web-acl`
- `inventory get` → shows all 10 DPUs

Each command shows the exact `dashctl` CLI equivalent in the preview
panel — useful for learning the CLI.

**✅ Checkpoint:** Command View lets you execute any dashctl command
from the browser.

---

## Step 9: Debug Tools

1. Click **"Debug"** in the sidebar

### 9.1 Raw API Caller
- Method: `GET`
- URL: `/api/admin/health`
- Click **"Send"**
- See the raw JSON response with status, leader, DPUs

### 9.2 Admin Endpoints
- Click **"Health"** → fleet health
- Click **"Inventory"** → DPU list with endpoints
- Click **"Drift"** → declared-vs-observed diff (should be empty)

### 9.3 Sim Inspector
- Select a simulator (e.g., `dash-sim-01`)
- Click **"Dump"** → see all objects on this DPU
- Click **"Health"** → sim health status

**✅ Checkpoint:** Debug tools provide raw access to all APIs.

---

## Step 10: Bulk Operations

### 10.1 Batch Apply via Admin Ops

1. Go to **Admin Ops** → **Batch** tab
2. Drag and drop a YAML file, or paste:

```yaml
apiVersion: dashcenter.v1
kind: Vnet
metadata:
  name: vnet-staging
  namespace: default
spec:
  vni: 20001
---
apiVersion: dashcenter.v1
kind: Eni
metadata:
  name: eni-staging-01
  namespace: default
spec:
  vnet_name: vnet-staging
  mac_address: "bb:cc:dd:00:00:01"
  admin_state: up
  placement_hint_dpu_ids: ["dpu-sim-03"]
```

3. Click **"Preview"** → see parsed resources
4. Click **"Apply All"**

**Expected:** Both resources created. Fleet now shows 2 Vnets, 5 ENIs.

---

## Step 11: Reconcile

If you ever see drift (declared ≠ observed):

1. Navigate to **Admin Ops** → **Reconcile** tab
2. Click **"Force Reconcile"**
3. Or scope it: select a specific DPU → "Reconcile DPU"

The reconciler pushes declared state to all DPUs, resolving any drift.

---

## Step 12: Clean Up

### Full tear down (removes everything including data)

```bash
cd deploy/test-setup/05-full-console
docker compose down -v
```

### Keep data for next session

```bash
docker compose down        # stops containers, keeps volumes
# Next time:
docker compose up -d       # restarts with existing state
```

### Deep clean (also remove images)

```bash
docker compose down -v --rmi local
```

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `http://localhost:3000` shows blank page | dashw image not built yet (Phase A not implemented) | Build: `cd src/impl-go/console && make docker-console` |
| All DPUs show as OFFLINE | dashd hasn't finished probing yet | Wait 15s, refresh |
| Console shows "dashd unreachable" | dashd containers crashed | `docker compose logs dashd-1` to diagnose |
| "Port 3000 already in use" | Another service on port 3000 | Change `DASHW_LISTEN` in docker-compose.yml or stop the other service |
| Build fails on `src/impl-go/console/Dockerfile` | Console code not yet implemented | This is expected until Phase A is complete |
| Vnet canvas shows no DPUs | No ENIs created for this Vnet | Create ENIs with placement hints first |
| Flow trace returns "no matching ENI" | ENI name doesn't exist | Check ENI name in the form matches a created ENI |
| HA failover takes >30s | etcd slow or network issues | Check `docker compose logs etcd` |

---

## What's Next

After completing this lab:

1. **Explore more views** — Routing (prefix tree), Tunnel (overlay/underlay
   toggle), Policy (expandable ACL rules), Audit Log (event stream)
2. **Create more complex scenarios** — multiple namespaces, ECMP routes,
   HA sets with switchover
3. **Try the dashctl CLI** — `docker compose run --rm dashctl get vnet`
4. **Read the architecture docs** —
   [`specs/HLD/dashw-web-hld.md`](../../../specs/HLD/dashw-web-hld.md)

---

## Architecture Recap

What you just deployed:

```
Browser (http://localhost:3000)
    │
    ▼
dashw BFF (Go, go:embed SPA)
    │
    ├─ REST proxy ─── dashd-1 (:8443) ─┐
    ├─ Admin proxy ── dashd-1 (:7443)  ├── etcd (state store)
    └─ gRPC bridge ── dashd-1 (:9443)  │
                      dashd-2 (:8443) ──┤
                      dashd-3 (:8443) ──┘
                           │
                           ▼
              10 DPU simulators (dash-sim)
              dpu-sim-01 through dpu-sim-10
```

- **dashw** is stateless — restart it anytime, no data loss
- **dashd** state lives in etcd — kill any dashd, state survives
- **DPU sims** are in-memory — restart clears their observed state
  (dashd reconciles automatically)