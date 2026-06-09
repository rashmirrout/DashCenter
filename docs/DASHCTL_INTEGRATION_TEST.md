# dashctl — Integration Test & Operations Guide

> **Audience.** Engineers exercising `dashctl` end-to-end against a
> running `dashd` and a fleet of `dash-sim` agents. Also serves as the
> reference scenarios for the `//go:build integration` test suite in
> `src/impl-go/dashctl/test/integration/`.
>
> **Status notice.** `dashctl` is being implemented per
> [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) and
> [`specs/LLD/dashctl-lld.md`](../specs/LLD/dashctl-lld.md), tracked in
> [`specs/Impl-Plan/dashctl-impl-phases.md`](../specs/Impl-Plan/dashctl-impl-phases.md).
> Outputs in this document are **the expected contract** for Phase 1 (REST)
> and Phase 2 (gRPC). Every command listed here is an integration-test
> obligation: a Phase gate is not closed until the command in this doc
> produces the output shown.
>
> **Phase markers in this doc**:
> - `[P1]` — REST backend; available as soon as dashctl Phase 1 ships against the dashd Phase 1B REST surface (current).
> - `[P2]` — gRPC backend / streaming / Operations / HA / Migration / Diagnostics; unlocks as dashd Phase 2 milestones land.
> - `[Admin]` — uses dashd's admin HTTP surface (`:7443`), available today.

---

## Table of contents

1. [Setup](#1-setup)
2. [Build](#2-build)
3. [Topology A — host-only (single binary smoke test)](#3-topology-a--host-only-single-binary-smoke-test)
4. [Topology B — single Docker container (dashd + dash-sim co-located)](#4-topology-b--single-docker-container-dashd--dash-sim-co-located)
5. [Topology C — full fleet (1 dashd + 5 dash-sim + dashctl from host and from container)](#5-topology-c--full-fleet-1-dashd--5-dash-sim--dashctl-from-host-and-from-container)
6. [Comprehensive command catalog with outputs](#6-comprehensive-command-catalog-with-outputs)
7. [Drift, reconciliation, and packet-path validation](#7-drift-reconciliation-and-packet-path-validation)
8. [Failure-injection scenarios](#8-failure-injection-scenarios)
9. [CI integration](#9-ci-integration)
10. [Reference — files referenced by this guide](#10-reference--files-referenced-by-this-guide)

---

## 1. Setup

### 1.1 Prerequisites

| Tool | Minimum version | Used for |
|---|---|---|
| Go | 1.22 | building `dashctl`, `dashd`, `dash-sim` from source |
| Docker Desktop / Engine | 24.x | container topologies |
| Docker Compose (v2 plugin) | 2.20 | `docker compose` syntax |
| PowerShell | 7.x (Windows) | host shell examples below |
| bash | 5.x (Linux/macOS) | host shell examples below |
| `curl` | any | direct REST verification |
| `jq` | 1.6+ | pretty-printing JSON in samples |

### 1.2 Repo layout used by this guide

```
DashCenter/
├── deploy/
│   └── compose/
│       ├── docker-compose.yml              # existing dev compose (dash-sim + redis adapter)
│       └── docker-compose.dashctl-it.yml   # NEW — Topology C (this doc)
├── docs/
│   └── DASHCTL_INTEGRATION_TEST.md         # this file
├── src/impl-go/
│   ├── dashd/        Dockerfile, configs/
│   ├── dash-sim/     Dockerfile
│   └── dashctl/      Dockerfile             # NEW — added in Phase 1 Step 26
└── test/dashctl-it/                         # NEW — test fixtures used here
    ├── manifests/
    │   ├── 00-inventory.yaml
    │   ├── 10-vnets.yaml
    │   ├── 20-enis.yaml
    │   ├── 30-mappings.yaml
    │   ├── 40-acl.yaml
    │   └── 50-routes.yaml
    └── golden/                              # expected outputs (golden files)
```

> **Note**: Anything tagged `# NEW` in the tree above is created the
> first time you run through this guide. The `docker-compose.dashctl-it.yml`
> and `dashctl/Dockerfile` are reproduced verbatim in §§4–5.

### 1.3 Environment variables (used across topologies)

| Var | Default | Purpose |
|---|---|---|
| `DASHCTL_ENDPOINT` | `http://localhost:8443` | dashd REST endpoint (Phase 1) |
| `DASHCTL_ADMIN_ENDPOINT` | `http://localhost:7443` | dashd admin HTTP |
| `DASHCTL_TRANSPORT` | `rest` | switch to `grpc` after dashctl Phase 2 |
| `DASHCTL_NAMESPACE` | `default` | spec namespace |
| `DASHCTL_OUTPUT` | (auto) | `table` on TTY, `json` otherwise |
| `DASHCTL_TIMEOUT` | `30s` | per-RPC timeout |

> Throughout this doc, where a command shows `dashctl ...`, the host-shell
> setup assumes `bin/dashctl` is on `$PATH`. For convenience define
> `$d = ".\bin\dashctl.exe"` (PowerShell) or `d=./bin/dashctl` (bash) and
> substitute `$d` / `$d`.

---

## 2. Build

### 2.1 Build everything from source (host)

PowerShell (Windows):

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
$env:CGO_ENABLED = "0"
New-Item -ItemType Directory -Path bin -Force | Out-Null

go build -trimpath -ldflags="-s -w" -o bin\dashd.exe        .\dashd\cmd\dashd
go build -trimpath -ldflags="-s -w" -o bin\dash-sim.exe     .\dash-sim\cmd\dash-sim
go build -trimpath -ldflags="-s -w" -o bin\dashctl.exe      .\dashctl\cmd\dashctl
```

bash (Linux/macOS):

```bash
cd ~/DashCenter/src/impl-go
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/dashd       ./dashd/cmd/dashd
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/dash-sim    ./dash-sim/cmd/dash-sim
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/dashctl     ./dashctl/cmd/dashctl
```

Verify:

```
$ ./bin/dashctl version --client
Client: dashctl v0.1.0 (commit abc1234, built 2026-06-09T13:00:00Z)
```

### 2.2 Build container images

```bash
cd ~/DashCenter

docker build -f src/impl-go/dashd/Dockerfile     -t dashcenter/dashd:dev       .
docker build -f src/impl-go/dash-sim/Dockerfile  -t dashcenter/dash-sim:dev    .
docker build -f src/impl-go/dashctl/Dockerfile   -t dashcenter/dashctl:dev     .
```

Verify image sizes (distroless static):

```
$ docker images | grep dashcenter
dashcenter/dashd      dev   <hash>   2 minutes ago    25.3MB
dashcenter/dash-sim   dev   <hash>   2 minutes ago    19.1MB
dashcenter/dashctl    dev   <hash>   2 minutes ago    18.4MB
```

### 2.3 dashctl Dockerfile (drop-in)

Place at `src/impl-go/dashctl/Dockerfile`:

```dockerfile
# Multi-stage Dockerfile for dashctl.
# Built from the repo root:
#   docker build -f src/impl-go/dashctl/Dockerfile -t dashcenter/dashctl .

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dashctl ./dashctl/cmd/dashctl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dashctl /usr/local/bin/dashctl
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/dashctl"]
```

---

## 3. Topology A — host-only (single-binary smoke test)

Goal: prove `dashctl ↔ dashd ↔ dash-sim` round-trip works on the local machine with no Docker.

### 3.1 Start dash-sim

Terminal 1:

```powershell
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8081 --device-id dpu-sim-0
```

```
2026/06/09 13:05:00 dash-sim: admin HTTP listening on :8081
2026/06/09 13:05:00 dash-sim: gRPC listening on :50051 (device=dpu-sim-0)
```

### 3.2 Start dashd

Create `local/dashd.yaml`:

```yaml
listen:
  grpc_addr:  ":9443"
  rest_addr:  ":8443"
  admin_addr: ":7443"
storage:
  backend: file
  file: { state_dir: local/state }
inventory:
  source: file
  file: local/inventory.yaml
reconcile:
  tick_interval: 30s
  apply_rate_limit: 100
  error_budget_per_min: 10
log:
  level: info
  format: json
```

`local/inventory.yaml`:

```yaml
dpus:
  - id: dpu-0
    endpoint: localhost:50051
    labels: { rack: A1 }
```

Terminal 2:

```powershell
.\bin\dashd.exe --config local\dashd.yaml
```

```
{"time":"2026-06-09T13:05:10Z","level":"INFO","msg":"dashd starting","version":"0.2.0-phase1b"}
{"time":"2026-06-09T13:05:10Z","level":"INFO","msg":"rest: listening","addr":":8443"}
{"time":"2026-06-09T13:05:10Z","level":"INFO","msg":"grpc: listening","addr":":9443"}
{"time":"2026-06-09T13:05:10Z","level":"INFO","msg":"admin: listening","addr":":7443"}
{"time":"2026-06-09T13:05:10Z","level":"INFO","msg":"dashd ready","rest":":8443","grpc":":9443","admin":":7443"}
```

### 3.3 First `dashctl` round-trip

Terminal 3:

```powershell
$env:DASHCTL_ENDPOINT="http://localhost:8443"
.\bin\dashctl.exe version
```

Expected:
```
Client: dashctl v0.1.0 (commit abc1234, built 2026-06-09T13:00:00Z)
Server: dashd  v0.2.0-phase1b (REST :8443)
```

```powershell
.\bin\dashctl.exe dpu list
```

Expected (table):
```
ID       ENDPOINT          STATE     LAST_SEEN
dpu-0    localhost:50051   UP        2026-06-09T13:05:25Z
```

This confirms the entire pipeline (dashctl → dashd REST → dashd inventory → dash-sim TCP probe → dashctl render).

---

## 4. Topology B — single Docker container (dashd + dash-sim co-located)

Goal: one container hosts both `dashd` and one `dash-sim`. Useful for
lightweight CI jobs that need a self-contained DashCenter in one image.

### 4.1 Co-located Dockerfile

Create `deploy/compose/dashd-allinone.Dockerfile`:

```dockerfile
# All-in-one container: dashd + dash-sim. Demo / CI use only — for production,
# always run dashd and dash-sim agents in separate processes/containers.
FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dashd     ./dashd/cmd/dashd && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dash-sim  ./dash-sim/cmd/dash-sim

FROM alpine:3.20
RUN apk add --no-cache tini
COPY --from=build /out/dashd     /usr/local/bin/dashd
COPY --from=build /out/dash-sim  /usr/local/bin/dash-sim
COPY deploy/compose/dashd-allinone-entrypoint.sh /usr/local/bin/entrypoint.sh
COPY deploy/compose/dashd-allinone.yaml          /etc/dashd/config.yaml
COPY deploy/compose/dashd-allinone-inventory.yaml /etc/dashd/inventory.yaml
RUN chmod +x /usr/local/bin/entrypoint.sh
EXPOSE 8443 9443 7443
ENTRYPOINT ["/sbin/tini","--","/usr/local/bin/entrypoint.sh"]
```

`deploy/compose/dashd-allinone-entrypoint.sh`:

```sh
#!/bin/sh
set -e
mkdir -p /var/lib/dashd
/usr/local/bin/dash-sim --grpc-listen :50051 --admin-listen :8080 --device-id dpu-allinone-0 &
DASH_SIM_PID=$!
sleep 1   # let dash-sim bind ports
exec /usr/local/bin/dashd --config /etc/dashd/config.yaml
```

`deploy/compose/dashd-allinone.yaml`:

```yaml
listen: { grpc_addr: ":9443", rest_addr: ":8443", admin_addr: ":7443" }
storage: { backend: file, file: { state_dir: /var/lib/dashd } }
inventory: { source: file, file: /etc/dashd/inventory.yaml }
reconcile: { tick_interval: 30s, apply_rate_limit: 100, error_budget_per_min: 10 }
log: { level: info, format: json }
```

`deploy/compose/dashd-allinone-inventory.yaml`:

```yaml
dpus:
  - id: dpu-allinone-0
    endpoint: localhost:50051
    labels: { rack: docker-allinone }
```

### 4.2 Build and run

```bash
cd ~/DashCenter
docker build -f deploy/compose/dashd-allinone.Dockerfile -t dashcenter/dashd-allinone:dev .

docker run --rm -d --name dc-allinone \
  -p 8443:8443 -p 9443:9443 -p 7443:7443 \
  dashcenter/dashd-allinone:dev
```

### 4.3 Drive from host

```bash
export DASHCTL_ENDPOINT=http://localhost:8443
dashctl dpu list
```

Expected:
```
ID                ENDPOINT          STATE   LAST_SEEN
dpu-allinone-0    localhost:50051   UP      2026-06-09T13:10:00Z
```

```bash
dashctl apply -f test/dashctl-it/manifests/10-vnets.yaml
dashctl get vnet
```

Expected:
```
NAMESPACE   NAME        VNI    GENERATION
default     vnet-app    1001   1
default     vnet-db     1002   1
```

### 4.4 Tear down

```bash
docker stop dc-allinone
```

---

## 5. Topology C — full fleet (1 dashd + 5 dash-sim + dashctl from host and from container)

This is the **flagship integration scenario**: a realistic five-DPU fleet
exercised end-to-end. It is the basis for the §6 command catalog and the
`//go:build integration` test suite.

### 5.1 Compose file

Create `deploy/compose/docker-compose.dashctl-it.yml`:

```yaml
# dashctl integration-test topology.
#   - 5 dash-sim agents (dpu-0 .. dpu-4)
#   - 1 dashd (file store; inventory mounted from ./inventory.yaml below)
#   - 1 dashctl one-shot CLI container (entrypoint dashctl; user supplies args)
#
# Bring up:
#   docker compose -f deploy/compose/docker-compose.dashctl-it.yml up -d
# Verify:
#   curl http://localhost:7443/admin/health | jq
#   dashctl dpu list
# Tear down:
#   docker compose -f deploy/compose/docker-compose.dashctl-it.yml down -v

services:
  dpu-0:
    image: dashcenter/dash-sim:dev
    container_name: it-dpu-0
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-0"]
    networks: [dcnet]
  dpu-1:
    image: dashcenter/dash-sim:dev
    container_name: it-dpu-1
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-1"]
    networks: [dcnet]
  dpu-2:
    image: dashcenter/dash-sim:dev
    container_name: it-dpu-2
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-2"]
    networks: [dcnet]
  dpu-3:
    image: dashcenter/dash-sim:dev
    container_name: it-dpu-3
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-3"]
    networks: [dcnet]
  dpu-4:
    image: dashcenter/dash-sim:dev
    container_name: it-dpu-4
    command: ["--grpc-listen", ":50051", "--admin-listen", ":8080", "--device-id", "dpu-4"]
    networks: [dcnet]

  dashd:
    image: dashcenter/dashd:dev
    container_name: it-dashd
    depends_on: [dpu-0, dpu-1, dpu-2, dpu-3, dpu-4]
    command: ["--config", "/etc/dashd/config.yaml"]
    volumes:
      - ./it-dashd.yaml:/etc/dashd/config.yaml:ro
      - ./it-inventory.yaml:/etc/dashd/inventory.yaml:ro
      - dashd-state:/var/lib/dashd
    ports:
      - "8443:8443"   # REST
      - "9443:9443"   # gRPC (Phase 2 use)
      - "7443:7443"   # Admin HTTP
    networks: [dcnet]

  # One-shot dashctl. Use:
  #   docker compose -f ... run --rm dashctl dpu list
  dashctl:
    image: dashcenter/dashctl:dev
    container_name: it-dashctl
    profiles: [cli]
    environment:
      DASHCTL_ENDPOINT: "http://dashd:8443"
      DASHCTL_ADMIN_ENDPOINT: "http://dashd:7443"
    entrypoint: ["/usr/local/bin/dashctl"]
    networks: [dcnet]

networks:
  dcnet:

volumes:
  dashd-state:
```

Companion files (same directory):

`deploy/compose/it-dashd.yaml`:

```yaml
listen: { grpc_addr: ":9443", rest_addr: ":8443", admin_addr: ":7443" }
storage: { backend: file, file: { state_dir: /var/lib/dashd } }
inventory: { source: file, file: /etc/dashd/inventory.yaml }
reconcile: { tick_interval: 30s, apply_rate_limit: 100, error_budget_per_min: 10 }
log: { level: info, format: json }
```

`deploy/compose/it-inventory.yaml`:

```yaml
dpus:
  - { id: dpu-0, endpoint: "dpu-0:50051", labels: { rack: R1 } }
  - { id: dpu-1, endpoint: "dpu-1:50051", labels: { rack: R1 } }
  - { id: dpu-2, endpoint: "dpu-2:50051", labels: { rack: R2 } }
  - { id: dpu-3, endpoint: "dpu-3:50051", labels: { rack: R2 } }
  - { id: dpu-4, endpoint: "dpu-4:50051", labels: { rack: R3 } }
```

### 5.2 Bring the fleet up

```bash
cd ~/DashCenter
docker compose -f deploy/compose/docker-compose.dashctl-it.yml up -d
```

Wait for dashd to mark all DPUs UP (≤ 15s):

```bash
curl -s http://localhost:7443/admin/health | jq
```

Expected:
```json
{
  "status": "ok",
  "leader": true,
  "dpus": [
    {"id":"dpu-0","state":"DPU_STATE_UP","last_seen":"2026-06-09T13:20:11Z"},
    {"id":"dpu-1","state":"DPU_STATE_UP","last_seen":"2026-06-09T13:20:11Z"},
    {"id":"dpu-2","state":"DPU_STATE_UP","last_seen":"2026-06-09T13:20:11Z"},
    {"id":"dpu-3","state":"DPU_STATE_UP","last_seen":"2026-06-09T13:20:11Z"},
    {"id":"dpu-4","state":"DPU_STATE_UP","last_seen":"2026-06-09T13:20:11Z"}
  ]
}
```

### 5.3 Drive from the host

```bash
export DASHCTL_ENDPOINT=http://localhost:8443
export DASHCTL_ADMIN_ENDPOINT=http://localhost:7443

dashctl dpu list
```

Expected:
```
ID       ENDPOINT       STATE   RACK   LAST_SEEN
dpu-0    dpu-0:50051    UP      R1     2026-06-09T13:20:11Z
dpu-1    dpu-1:50051    UP      R1     2026-06-09T13:20:11Z
dpu-2    dpu-2:50051    UP      R2     2026-06-09T13:20:11Z
dpu-3    dpu-3:50051    UP      R2     2026-06-09T13:20:11Z
dpu-4    dpu-4:50051    UP      R3     2026-06-09T13:20:11Z
```

### 5.4 Drive from a CLI container

```bash
docker compose -f deploy/compose/docker-compose.dashctl-it.yml \
  run --rm dashctl dpu list
```

Same table; proves the in-cluster (dcnet) path works identically.

A throwaway interactive shell with dashctl on `PATH`:

```bash
docker compose -f deploy/compose/docker-compose.dashctl-it.yml \
  run --rm --entrypoint /bin/sh dashctl
# (inside) dashctl get vnet -A
```

---

## 6. Comprehensive command catalog with outputs

All examples assume **Topology C** is up. Manifests live under
`test/dashctl-it/manifests/` (reproduced inline below). For each
section: *purpose*, *manifest (if any)*, *command*, *expected output*.

### 6.1 `dashctl apply -f` — declarative provisioning of the fleet  [P1]

Apply the full sample set in dependency order:

```bash
dashctl apply -f test/dashctl-it/manifests/
```

Expected:
```
KIND           NAMESPACE   NAME          OP        GENERATION   RESULT
inventory      -           (5 dpus)      replace   1            ok
vnet           default     vnet-app      create    1            ok
vnet           default     vnet-db       create    1            ok
eni            default     eni-app-01    create    1            ok
eni            default     eni-app-02    create    1            ok
eni            default     eni-db-01     create    1            ok
eni            default     eni-db-02     create    1            ok
eni            default     eni-db-03     create    1            ok
vnet-mapping   default     map-app-01    create    1            ok
vnet-mapping   default     map-db-01     create    1            ok
acl-policy     default     acl-app-in    create    1            ok
acl-policy     default     acl-db-in     create    1            ok
route-policy   default     rt-default    create    1            ok

Applied 13 specs across 1 namespace.
```

#### Manifest set (`test/dashctl-it/manifests/`)

`00-inventory.yaml`:
```yaml
apiVersion: dashcenter.v1
kind: Inventory
spec:
  dpus:
    - { id: dpu-0, endpoint: "dpu-0:50051", labels: { rack: R1 } }
    - { id: dpu-1, endpoint: "dpu-1:50051", labels: { rack: R1 } }
    - { id: dpu-2, endpoint: "dpu-2:50051", labels: { rack: R2 } }
    - { id: dpu-3, endpoint: "dpu-3:50051", labels: { rack: R2 } }
    - { id: dpu-4, endpoint: "dpu-4:50051", labels: { rack: R3 } }
```

`10-vnets.yaml`:
```yaml
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: vnet-app, namespace: default, labels: { tier: app } }
spec:     { vni: 1001 }
---
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: vnet-db, namespace: default, labels: { tier: db } }
spec:     { vni: 1002 }
```

`20-enis.yaml` (sample of two; full file has five ENIs spread across racks):
```yaml
apiVersion: dashcenter.v1
kind: Eni
metadata: { name: eni-app-01, namespace: default, labels: { tier: app } }
spec:
  vnetName: vnet-app
  macAddress: "00:11:22:00:00:01"
  underlayIp: "10.0.5.11"
  adminState: "up"
  placementHintDpuIds: ["dpu-0"]
---
apiVersion: dashcenter.v1
kind: Eni
metadata: { name: eni-app-02, namespace: default, labels: { tier: app } }
spec:
  vnetName: vnet-app
  macAddress: "00:11:22:00:00:02"
  underlayIp: "10.0.5.12"
  adminState: "up"
  placementHintDpuIds: ["dpu-1"]
```

`30-mappings.yaml`, `40-acl.yaml`, `50-routes.yaml` follow the same envelope shape; see the spec messages in [`proto/dashcenter/v1/control_plane.proto`](../proto/dashcenter/v1/control_plane.proto) for fields.

### 6.2 `dashctl get` — read [P1]

List:
```bash
dashctl get vnet
```
```
NAMESPACE   NAME        VNI    GENERATION   LABELS
default     vnet-app    1001   1            tier=app
default     vnet-db     1002   1            tier=db
```

Single:
```bash
dashctl get eni eni-app-01 -o yaml
```
```yaml
apiVersion: dashcenter.v1
kind: Eni
metadata:
  namespace: default
  name: eni-app-01
  generation: 1
  labels:
    tier: app
spec:
  vnetName: vnet-app
  macAddress: "00:11:22:00:00:01"
  underlayIp: "10.0.5.11"
  adminState: "up"
  placementHintDpuIds: ["dpu-0"]
```

Wide:
```bash
dashctl get eni -o wide
```
```
NAMESPACE   NAME         VNET       MAC                  UNDERLAY     ADMIN   PLACED-ON   GEN
default     eni-app-01   vnet-app   00:11:22:00:00:01    10.0.5.11    up      dpu-0       1
default     eni-app-02   vnet-app   00:11:22:00:00:02    10.0.5.12    up      dpu-1       1
default     eni-db-01    vnet-db    00:11:22:00:00:03    10.0.6.11    up      dpu-2       1
default     eni-db-02    vnet-db    00:11:22:00:00:04    10.0.6.12    up      dpu-3       1
default     eni-db-03    vnet-db    00:11:22:00:00:05    10.0.6.13    up      dpu-4       1
```

Label selector:
```bash
dashctl get eni -l tier=db
```
```
NAMESPACE   NAME         VNET      GENERATION
default     eni-db-01    vnet-db   1
default     eni-db-02    vnet-db   1
default     eni-db-03    vnet-db   1
```

All namespaces:
```bash
dashctl get all -A
```
```
NAMESPACE   KIND          NAME           GENERATION
default     vnet          vnet-app       1
default     vnet          vnet-db        1
default     eni           eni-app-01     1
... (truncated)
```

Output formats:
```bash
dashctl get vnet vnet-app -o json
dashctl get vnet vnet-app -o name           # → vnet/vnet-app
dashctl get vnet vnet-app -o jsonpath='{.spec.vni}'   # → 1001
```

### 6.3 `dashctl describe` — human detail [P1]

```bash
dashctl describe eni eni-app-01
```
```
Name:        eni-app-01
Namespace:   default
Generation:  1
Labels:      tier=app
Spec:
  Vnet:        vnet-app
  MAC:         00:11:22:00:00:01
  UnderlayIP:  10.0.5.11
  AdminState:  up
  Placement:   dpu-0  (hinted)
Status (live from dashd):
  Placed-on:   dpu-0
  Observed:    yes
  Drift:       0 add / 0 update / 0 remove
Last reconcile: 2026-06-09T13:21:00Z   (drift cleared)
```

### 6.4 `dashctl diff` — preview a change [P1]

```bash
# operator edits 10-vnets.yaml to change vnet-app vni: 1001 → 1010
dashctl diff -f test/dashctl-it/manifests/10-vnets.yaml
```
```
KIND   NAMESPACE   NAME      FIELD          OLD     NEW
vnet   default     vnet-app  spec.vni       1001    1010

1 spec would change.
```

`dashctl apply --dry-run=server -f ...` [P2 — uses `SimulateApply`] returns the same diff plus a per-DPU capacity preview.

### 6.5 `dashctl edit` — interactive [P1]

```bash
EDITOR=vim dashctl edit eni eni-app-01
```
Opens the YAML envelope (with `metadata.generation: 1`) in `$EDITOR`.
On save:
```
Eni/eni-app-01 updated (generation 2).
```
On conflict (someone else updated between Get and Put):
```
Error: generation mismatch (have 1, want 2)
Code: FAILED_PRECONDITION
Hint: re-run `dashctl edit eni eni-app-01` to fetch latest
exit 4
```

### 6.6 `dashctl delete` [P1]

```bash
dashctl delete eni eni-app-02
```
```
Eni/eni-app-02 deleted.
```

Idempotent:
```bash
dashctl delete eni eni-app-02 --ignore-not-found
# (no output; exit 0)
```

CAS:
```bash
dashctl delete eni eni-db-01 --expected-generation=99
```
```
Error: generation mismatch (have 1, want 99)
Code: FAILED_PRECONDITION
exit 4
```

### 6.7 `dashctl reconcile` [P1]

```bash
dashctl reconcile
```
```
Triggered reconcile on 5 DPUs.
```

Targeted:
```bash
dashctl reconcile --dpu dpu-0 --dpu dpu-3
```
```
Triggered reconcile on 2 DPUs: dpu-0, dpu-3
```

### 6.8 `dashctl dpu drift` [P1, Admin]

After a fresh apply with everything converged:
```bash
dashctl dpu drift --dpu dpu-0
```
```
DPU     OP      KIND          KEY                       REASON
(none)
0 drift items.
```

If you stop `dpu-3` and wait one probe window:
```bash
docker stop it-dpu-3
sleep 6
dashctl dpu drift --dpu dpu-3
```
```
Error: dpu dpu-3 is OFFLINE (last seen 2026-06-09T13:22:05Z)
Code: UNAVAILABLE
exit 7
```

### 6.9 `dashctl dpu status` [P1 snapshot, P2 stream]

Phase 1 (snapshot):
```bash
dashctl dpu status
```
```
ID       STATE   ENIs   ACL_RULES   ROUTES   PPS_IN   PPS_OUT
dpu-0    UP      1      2           1        0        0
dpu-1    UP      1      2           1        0        0
dpu-2    UP      1      0           1        0        0
dpu-3    UP      1      0           1        0        0
dpu-4    UP      1      0           1        0        0
```

Phase 2 (streaming with deltas):
```bash
dashctl dpu status --watch
```
```
2026-06-09T13:25:00Z  dpu-0  UP   enis=1  pps_in=0     pps_out=0
2026-06-09T13:25:05Z  dpu-0  UP   enis=1  pps_in=12    pps_out=12
2026-06-09T13:25:10Z  dpu-3  UP   enis=1  pps_in=0     pps_out=0
^C
stream cancelled by signal — 47 events delivered
```

### 6.10 `dashctl events --watch` [P1 long-poll, P2 native]

```bash
dashctl events --watch -o json &
dashctl apply -f test/dashctl-it/manifests/10-vnets.yaml
```
The first command stream prints:
```json
{"ts":"2026-06-09T13:26:01Z","kind":"vnet","namespace":"default","name":"vnet-app","op":"PUT","generation":2,"actor":"local"}
{"ts":"2026-06-09T13:26:01Z","kind":"vnet","namespace":"default","name":"vnet-db","op":"PUT","generation":2,"actor":"local"}
```

### 6.11 `dashctl describe` on a DPU — inventory + observed + drift [P1, Admin]

```bash
dashctl describe dpu dpu-0
```
```
Name:        dpu-0
Endpoint:    dpu-0:50051
State:       UP   (last_seen 2026-06-09T13:26:05Z)
Labels:      rack=R1
Placed ENIs:
  - eni-app-01    (vnet=vnet-app)
Observed objects: 6
Drift:            0 add / 0 update / 0 remove
Capacity (Phase 2): unknown (dashd PB pending)
```

### 6.12 Phase 2 — Operations and HA  [P2]

```bash
dashctl dpu cordon dpu-2
# DPU dpu-2 cordoned (no new ENI placements).

dashctl dpu drain dpu-2 --parallel 2
# Streaming progress:
2026-06-09T13:30:00Z  PLANNING   target=dpu-2 enis=1
2026-06-09T13:30:01Z  MIGRATING  eni-db-01 → dpu-3
2026-06-09T13:30:09Z  DRAINING   target=dpu-2 remaining=0
2026-06-09T13:30:10Z  COMPLETE   target=dpu-2 (now CORDONED)

dashctl dpu uncordon dpu-2
# DPU dpu-2 uncordoned.
```

HA switchover (requires two DPUs in an `HaSet` from a manifest):
```bash
dashctl ha switchover ha-app --to dpu-1
```
```
2026-06-09T13:32:00Z  ha-app  switchover requested: active → dpu-1
2026-06-09T13:32:01Z  ha-app  draining flows on old-active (dpu-0)
2026-06-09T13:32:04Z  ha-app  promoted dpu-1 to ACTIVE
2026-06-09T13:32:04Z  ha-app  switchover complete (duration=4s)
```

### 6.13 Phase 2 — Diagnostics (packet path / TraceFlow) [P2]

`dashctl trace flow` walks the cached ACL/route policy chain for a synthetic
packet entirely in dashd's memory — no packets ever hit the DPU dataplane.

```bash
dashctl trace flow \
  --src 10.0.5.11 --dst 10.0.6.11 \
  --src-port 32000 --dst-port 443 --protocol tcp \
  --eni eni-app-01
```
Expected (verbatim contract for the test):
```
Trace: eni-app-01  (placed on dpu-0)
  Stage 1 (ingress ACL acl-app-in):
    matched rule #100  action=allow   "permit-app-to-db"
  Stage 2 (route lookup rt-default):
    matched prefix 10.0.6.0/24  next-hop=vnet vnet-db
  Stage 3 (vnet mapping map-db-01):
    target=10.0.6.11 → underlay 10.0.6.11 mac=00:11:22:00:00:03
  Egress (ENI eni-db-01 on dpu-2): admitted
Verdict: PERMIT
Hops:    2 (dpu-0 → dpu-2)
```

Deny verdict (no matching rule):
```bash
dashctl trace flow \
  --src 192.0.2.1 --dst 10.0.6.11 --src-port 1 --dst-port 443 --protocol tcp \
  --eni eni-app-01
```
```
Trace: eni-app-01  (placed on dpu-0)
  Stage 1 (ingress ACL acl-app-in):
    no match — default policy DENY
Verdict: DENY
```

`dashctl trace explain` shows per-rule reasoning:
```bash
dashctl trace explain --acl-rule acl-app-in:100 \
  --src 10.0.5.11 --dst 10.0.6.11 --dst-port 443 --protocol tcp
```
```
Rule acl-app-in:100  action=allow  priority=100
  src 10.0.5.11      ∈  10.0.5.0/24    MATCH
  dst 10.0.6.11      ∈  10.0.6.0/24    MATCH
  src_port any                          MATCH
  dst_port 443       ∈  443,8443        MATCH
  proto    tcp       =  tcp             MATCH
Result: MATCH → allow
```

ACL hit stats (dead-rule detection):
```bash
dashctl trace acl-stats --zero-only
```
```
DPU      POLICY          RULE   HITS   LAST_HIT
dpu-2    acl-db-in       200    0      never
dpu-3    acl-db-in       200    0      never
2 zero-hit rules.
```

### 6.14 Bulk apply via stdin

```bash
cat test/dashctl-it/manifests/10-vnets.yaml \
  | dashctl apply -f -
```
Same output as §6.1 row for the two vnets.

### 6.15 `dashctl explain` (offline) [P1]

```bash
dashctl explain Eni.spec.placementHintDpuIds
```
```
FIELD:   spec.placementHintDpuIds
TYPE:    repeated string
DESC:    Optional placement hint: comma-separated DPU IDs the operator prefers.
         dashd may override if capacity/capabilities don't match.
```

### 6.16 `dashctl config` [P1]

```bash
dashctl config view
dashctl config get-contexts
dashctl config use-context dev-local
dashctl config set-context prod-east --endpoint=https://dashd.east.example.com:8443 --transport=rest
```

Sample output of `view`:
```yaml
apiVersion: dashctl/v1
kind: Config
current-context: dev-local
contexts:
  dev-local:
    endpoint: http://localhost:8443
    transport: rest
    namespace: default
    auth: { mode: none }
  prod-east:
    endpoint: https://dashd.east.example.com:8443
    transport: rest
    namespace: default
    auth: { mode: token, token-env: DASHCTL_TOKEN_EAST }
```

### 6.17 `dashctl version`

```bash
dashctl version
```
```
Client: dashctl v0.1.0 (commit abc1234, built 2026-06-09T13:00:00Z)
Server: dashd  v0.2.0-phase1b (REST :8443) leader=true
```

Offline (dashd unreachable):
```
Client: dashctl v0.1.0 (commit abc1234, built 2026-06-09T13:00:00Z)
Server: unavailable (dial http://localhost:8443: connection refused)
exit 0
```

### 6.18 `dashctl completion`

```bash
dashctl completion bash       # paste into ~/.bashrc
dashctl completion zsh        # > ~/.zsh/completions/_dashctl
dashctl completion pwsh       # | Out-String | Invoke-Expression
```

---

## 7. Drift, reconciliation, and packet-path validation

### 7.1 Convergence smoke test

Sequence to assert after fleet bring-up:

```bash
dashctl apply -f test/dashctl-it/manifests/
for dpu in dpu-0 dpu-1 dpu-2 dpu-3 dpu-4; do
  dashctl dpu drift --dpu $dpu
done
```

Expected: every DPU reports `0 drift items.` within 30s (one reconciler tick).

### 7.2 Self-healing on DPU restart

```bash
docker restart it-dpu-3
sleep 8                              # wait for probe + reconnect + reconcile
dashctl dpu drift --dpu dpu-3
```
Expected: `0 drift items.` (dashd's subscribe pump re-snapshotted observed
state, the diff matched declared state, no further action needed).

### 7.3 Packet-path validation summary

| Capability | Phase | Command |
|---|---|---|
| Declared-vs-observed drift | [P1] | `dashctl dpu drift [--dpu ID]` |
| ENI placement view | [P1, Admin] | `dashctl get eni -o wide` (`PLACED-ON`) |
| Synthetic packet trace | [P2] | `dashctl trace flow` |
| Per-rule match explanation | [P2] | `dashctl trace explain` |
| Dead-rule detection | [P2] | `dashctl trace acl-stats --zero-only` |
| Drift narrative | [P2] | `dashctl trace drift-explain` |
| Force re-evaluation of in-flight flows | [P2] | `dashctl trace resimulate --dpu ID` |

> **Important.** Packet-flow / path-trace capabilities run in dashd's
> in-memory cache against declared+observed state. They do **not**
> require live traffic and do **not** impact the DPU dataplane.

---

## 8. Failure-injection scenarios

These are the must-have entries in the dashctl integration suite.

| # | Scenario | Setup | Verify |
|---|---|---|---|
| F1 | dashd unreachable | `docker stop it-dashd` | `dashctl get vnet` exits 7 with `UNAVAILABLE`; `dashctl version` still prints client section, exits 0 |
| F2 | One DPU offline | `docker stop it-dpu-2` | within 15s `dashctl dpu list` shows `dpu-2 OFFLINE`; other DPUs unaffected |
| F3 | DPU reconnect | restart `it-dpu-2` | within 10s `dpu-2 UP`; `drift` returns 0 after reconcile |
| F4 | Apply with stale generation | edit `eni-app-01` in two terminals | second `dashctl apply` exits 4 with `FAILED_PRECONDITION` |
| F5 | Bad token | `dashctl --token=bogus get vnet` (Phase 2 PD onwards) | exits 6 with `UNAUTHENTICATED` |
| F6 | Bad endpoint | `dashctl --endpoint=http://nowhere:8443 get vnet` | exits 7 with `UNAVAILABLE` |
| F7 | Manifest with unknown kind | `dashctl apply -f bad-kind.yaml` | exits 5 with `invalid spec: unknown kind FooBar` |
| F8 | Streaming Ctrl-C | `dashctl events --watch` → `Ctrl-C` | exits 0 within 250 ms with `stream cancelled by signal — N events` |
| F9 | Drain (Phase 2) | `dashctl dpu drain dpu-4` while `dpu-4` hosts `eni-db-03` | progress stream visible; ENI migrates; final state `CORDONED` |
| F10 | Cancel mid-drain (Phase 2) | `Ctrl-C` during F9 | dashd cancels gracefully; CLI exits 0; drift may show partial migration which next reconcile heals |

---

## 9. CI integration

### 9.1 Local one-shot suite

```bash
make dashctl-integration            # invoked by CI from repo root
```
Implemented as (target lives in `src/impl-go/dashctl/Makefile`):
```makefile
.PHONY: dashctl-integration
dashctl-integration:
	docker compose -f $(REPO)/deploy/compose/docker-compose.dashctl-it.yml up -d --build
	@trap 'docker compose -f $(REPO)/deploy/compose/docker-compose.dashctl-it.yml down -v' EXIT; \
	  DASHCTL_ENDPOINT=http://localhost:8443 \
	  DASHCTL_ADMIN_ENDPOINT=http://localhost:7443 \
	  go test -tags=integration -count=1 -timeout=600s ./test/integration/...
```

### 9.2 GitHub Actions skeleton

```yaml
name: dashctl-integration
on: [push, pull_request]
jobs:
  it:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.22' }
      - name: Build images
        run: |
          docker build -f src/impl-go/dashd/Dockerfile     -t dashcenter/dashd:dev     .
          docker build -f src/impl-go/dash-sim/Dockerfile  -t dashcenter/dash-sim:dev  .
          docker build -f src/impl-go/dashctl/Dockerfile   -t dashcenter/dashctl:dev   .
      - name: Bring up fleet
        run: docker compose -f deploy/compose/docker-compose.dashctl-it.yml up -d
      - name: Wait for ready
        run: |
          for i in $(seq 1 30); do
            curl -fs http://localhost:7443/admin/health && break
            sleep 2
          done
      - name: Run integration tests
        env:
          DASHCTL_ENDPOINT: http://localhost:8443
          DASHCTL_ADMIN_ENDPOINT: http://localhost:7443
        run: |
          cd src/impl-go/dashctl
          go test -tags=integration -count=1 -timeout=600s ./test/integration/...
      - name: Capture logs on failure
        if: failure()
        run: docker compose -f deploy/compose/docker-compose.dashctl-it.yml logs > docker-logs.txt
      - uses: actions/upload-artifact@v4
        if: failure()
        with: { name: docker-logs, path: docker-logs.txt }
      - name: Tear down
        if: always()
        run: docker compose -f deploy/compose/docker-compose.dashctl-it.yml down -v
```

### 9.3 Golden-file regeneration

When intentional output changes happen:
```bash
go test -tags=integration ./test/integration/... -update-golden
```
Reviewer must diff `testdata/golden/` in the PR.

---

## 10. Reference — files referenced by this guide

| File | Purpose | Status |
|---|---|---|
| [`specs/HLD/dashctl-hld.md`](../specs/HLD/dashctl-hld.md) | HLD | exists |
| [`specs/LLD/dashctl-lld.md`](../specs/LLD/dashctl-lld.md) | LLD | exists |
| [`specs/Impl-Plan/dashctl-impl-phases.md`](../specs/Impl-Plan/dashctl-impl-phases.md) | Phase tracker | exists |
| [`specs/HLD/dashd-hld.md`](../specs/HLD/dashd-hld.md) | dashd HLD | exists |
| [`proto/dashcenter/v1/`](../proto/dashcenter/v1) | northbound proto | exists |
| `src/impl-go/dashctl/Dockerfile` | dashctl image | created in Phase 1 Step 26 |
| `deploy/compose/docker-compose.dashctl-it.yml` | Topology C compose | created on first run of §5.1 |
| `deploy/compose/it-dashd.yaml` | dashd config for IT | created on first run of §5.1 |
| `deploy/compose/it-inventory.yaml` | inventory for IT | created on first run of §5.1 |
| `deploy/compose/dashd-allinone.Dockerfile` | Topology B image | created on first run of §4.1 |
| `test/dashctl-it/manifests/` | integration-test manifests | created on first run of §6.1 |
| `test/dashctl-it/golden/` | golden outputs | created by `-update-golden` |

---

> **Maintenance contract**: every new `dashctl` command added in Phase 1
> or Phase 2 MUST land in this document with (a) purpose, (b) command,
> (c) expected output. PRs that add commands without updating this guide
> fail review.
