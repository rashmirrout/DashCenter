# 08 — Docker Compose

Run the entire DashCenter fleet under containers — works the same on
Windows (Docker Desktop) and Linux (Docker Engine). By the end of this
page you will have:

- Three long-running services: `redis`, `dash-sim`, `dash-redis-adapter`
- A one-shot CLI container (`cli` service) for running `dash-sim-client`
  commands inside the docker network
- Examples that mirror the bare-metal `apply / get / list / subscribe`
  flow against both backends

> **Prerequisite**: Docker Engine 24+ (Linux) or Docker Desktop (Windows /
> macOS). See [03 — Build setup §3.5 / §4](03-build-setup.md).

---

## 1. Files involved

```
deploy/compose/
├── docker-compose.yml       <- the fleet definition
└── scenarios/               <- mounted into dash-sim if you wire it
src/impl-go/
├── dash-sim/Dockerfile
├── dash-redis-adapter/Dockerfile
└── dash-sim-client/Dockerfile
```

All three Dockerfiles use **multi-stage builds** producing **distroless
nonroot** images (no shell, no package manager — only the binary).

---

## 2. Start the fleet

From the repo root:

```powershell
# Windows
cd C:\WorkSpace\PS\PublicRepo\DashCenter\deploy\compose
docker compose up -d redis dash-sim dash-redis-adapter
```

```bash
# Linux
cd ~/work/DashCenter/deploy/compose
docker compose up -d redis dash-sim dash-redis-adapter
```

First-time output (build + pull):
```
[+] Building 2/2
 ✔ dash-sim                Built              45.2s
 ✔ dash-redis-adapter      Built              48.1s
[+] Running 4/4
 ✔ Network compose_default            Created
 ✔ Container dc-redis                 Started
 ✔ Container dc-dash-sim              Started
 ✔ Container dc-dash-redis-adapter    Started
```

Subsequent starts only do `Started`. Use `--build` to force a rebuild after
changing source.

### Check status

```bash
docker compose ps
```
Sample output:
```
NAME                       IMAGE                            STATUS         PORTS
dc-redis                   redis:7-alpine                   Up 5s          0.0.0.0:6379->6379/tcp
dc-dash-sim                dashcenter/dash-sim:dev          Up 5s          0.0.0.0:50051->50051/tcp, 0.0.0.0:8080->8080/tcp
dc-dash-redis-adapter      dashcenter/dash-redis-adapter:dev Up 5s         0.0.0.0:52051->52051/tcp
```

### Watch logs

```bash
docker compose logs -f dash-sim
docker compose logs -f dash-redis-adapter
```

You should see the same `gRPC listening on :50051` / `:52051` lines as in
the bare-metal runs.

---

## 3. Drive the fleet from the host's CLI

If you've built `dash-sim-client.exe` (or `dash-sim-client`) locally, point
it at the host-mapped ports — same commands as page [07](07-dash-sim-client.md):

```powershell
$c = "C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\bin\dash-sim-client.exe"
& $c --target localhost:50051 ping                            # dash-sim (container)
& $c --target localhost:52051 ping                            # dash-redis-adapter (container)
& $c --target localhost:50051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:52051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:50051 list  --kind vnet -o table
& $c --target localhost:52051 list  --kind vnet -o table

# PE-3a / PE-G8 typed per-DPU rollup (works against either dash-sim;
# Redis adapter returns Unimplemented for now):
& $c --target localhost:50051 dpu-counters --include-enis -o table
& $c --target localhost:50051 dpu-counters --watch --interval 2s
```

---

## 4. Drive the fleet from the `cli` container (no local binaries needed)

Use this when you don't want to install Go on the host at all. The compose
file defines a one-shot **`cli`** service (under the `cli` profile) that
launches `dash-sim-client` inside the docker network — so it talks to the
peer services by **DNS name** (`dash-sim:50051`, `dash-redis-adapter:52051`).

### 4.1 First time — build the CLI image

```bash
docker compose --profile cli build cli
```

### 4.2 Discovery

```bash
docker compose run --rm cli kinds -o table
docker compose run --rm cli ping
```

The container's default `--target` is `dash-sim:50051`. Override per
invocation:

```bash
docker compose run --rm cli --target dash-redis-adapter:52051 ping
```

### 4.3 Apply / get / list / delete

```bash
docker compose run --rm cli apply --kind vnet --key vnet-prod --value '{"vni":1001}'
docker compose run --rm cli get   --kind vnet --key vnet-prod -o yaml
docker compose run --rm cli list  --kind vnet -o table
docker compose run --rm cli delete --kind vnet --key vnet-prod
```

Output is identical to the bare-metal runs.

### 4.4 Subscribe + parallel apply

Two-terminal demo:

Terminal A (subscribe):
```bash
docker compose run --rm cli subscribe --snapshot --kinds vnet,eni
```

Terminal B (mutate):
```bash
docker compose run --rm cli apply --kind vnet --key vnet-live --value '{"vni":9999}'
```

Terminal A prints one `SNAPSHOT` event per pre-existing object, then the
new `CREATED` event.

### 4.5 Pipeline simulation (sim only)

```bash
# Seed prerequisite state
docker compose run --rm cli apply --kind vnet --key vnet-prod --value '{"vni":1001}'
docker compose run --rm cli apply --kind eni --key eni-001 --value '{
  "eni_id":"11111111-1111-1111-1111-111111111111",
  "mac_address":"ABEiM0RV",
  "vnet":"vnet-prod",
  "admin_state":"STATE_ENABLED"
}'
docker compose run --rm cli apply --kind route_group --key rg-prod --value '{"version":"v1"}'
docker compose run --rm cli apply --kind eni_route --key eni-001 --value '{"group_id":"rg-prod"}'
docker compose run --rm cli apply --kind route --key 'rg-prod:10.1.0.0/16' --value '{"routing_type":"ROUTING_TYPE_VNET","vnet":"vnet-stage"}'
docker compose run --rm cli apply --kind vnet --key vnet-stage --value '{"vni":1002}'
docker compose run --rm cli apply --kind vnet_mapping --key 'vnet-stage:10.1.0.10' --value '{"underlay_ip":{"ipv4":167862884},"routing_type":"ROUTING_TYPE_VNET"}'

# Simulate
docker compose run --rm cli simulate --direction outbound --eni eni-001 \
    --src-ip 10.0.0.1 --dst-ip 10.1.0.10 --protocol 6 --src-port 1024 --dst-port 80 --trace
```

Expected:
```json
{ "action": "ENCAP", "out_underlay_ip": "100.64.0.10", ... }
```

### 4.6 Bonus — run any other CLI image flag

The `cli` service's `entrypoint` is `dash-sim-client --target dash-sim:50051`,
so anything after `run --rm cli ...` is appended as args. To run **without**
the default `--target`, override the entrypoint:

```bash
docker compose run --rm --entrypoint /usr/local/bin/dash-sim-client cli \
    --target dash-redis-adapter:52051 list --kind vnet -o table
```

### 4.7 PE-3a — typed per-DPU counter rollup (`dpu-counters`)

The new typed rollup RPC and its `dpu-counters` subcommand work in the
container exactly like on bare metal. The simulator's deterministic
tick loop runs by default at `1s`, so a freshly-started fleet will
still report meaningful per-ENI / per-VNET breakdowns within a few
seconds.

```bash
# One-shot DPU bucket (always populated):
docker compose run --rm cli dpu-counters -o table

# Include per-ENI + per-VNET rollups, sorted alphabetically:
docker compose run --rm cli dpu-counters \
    --include-enis --include-vnets -o table

# Live tail every 2s (Ctrl-C exits cleanly):
docker compose run --rm cli dpu-counters --watch --interval 2s --include-enis

# Pipe a one-shot CSV out of the container into a host file
# (note: must use the cli service which doesn't auto-add --target;
#  see 4.6 for the entrypoint override pattern):
docker compose run --rm cli dpu-counters \
    --include-enis --include-vnets -o csv > snapshot.csv
head -3 snapshot.csv
```

Expected for `--include-enis`:

```text
DEVICE  dpu-sim-01
TIME    2026-06-14T20:14:34Z (ns=...)

DPU TOTALS
SCOPE  PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
dpu    1251        2492         87308     174616     12

PER-ENI
SCOPE    PACKETS_IN  PACKETS_OUT  BYTES_IN  BYTES_OUT  DROPS
eni-001  412         824          28832     57664      4
eni-002  421         842          29462     58924      4
```

Fault-inject the new RPC the same way as any other op — the admin
port is the same HTTP server:

```bash
docker compose exec dash-sim sh -c "wget -qO- --post-data='{\"op\":\"GetDpuCounters\",\"mode\":\"error\",\"count\":1,\"message\":\"demo\"}' --header='Content-Type: application/json' http://localhost:8080/admin/faults"

docker compose run --rm cli dpu-counters
# Expected on the first call: "rpc error: code = Unavailable desc = demo"

docker compose run --rm cli dpu-counters
# Second call succeeds — the fault was a one-shot.
```

Deep dive (filter flags, JSON envelope shape, scope membership rules):
[`docs/dashd-features/dash-sim-counter-rollups.md`](../dashd-features/dash-sim-counter-rollups.md).

---

## 5. Scale the simulator

```bash
docker compose up -d --scale dash-sim=3
```

Each replica binds to a host-assigned port (visible via `docker compose ps`).
Connect to a specific replica from the CLI container by service IP or
container name (DNS resolves both):

```bash
docker compose run --rm cli --target dc-dash-sim:50051 ping
```

---

## 6. Stop, restart, clean up

```bash
docker compose stop                 # stop (keep state)
docker compose start                # resume
docker compose down                 # stop + remove containers + network
docker compose down --volumes       # also drop Redis data
docker compose build --no-cache     # full rebuild
```

---

## 7. Differences vs. bare-metal

| Bare metal | Container |
|---|---|
| `--target localhost:50051` | `--target dash-sim:50051` (inside the docker network) or `--target localhost:50051` (from the host) |
| Embedded miniredis (`--embedded-redis`) | Real Redis service in `redis` container |
| Logs to stdout | `docker compose logs -f <svc>` |
| `Ctrl+C` to stop | `docker compose stop` |

---

## 8. Troubleshooting

| Symptom | Fix |
|---|---|
| `Bind for 0.0.0.0:50051 failed: port is already allocated` | Another process owns the port. Stop it or change the host-side mapping in `docker-compose.yml`. |
| `dash-redis-adapter: connect: connection refused` | Redis container didn't come up. Check `docker compose logs redis`. |
| Build fails with `package ... is not in std` | Old Go base image. Pull `golang:1.22-alpine` explicitly: `docker pull golang:1.22-alpine`. |
| CLI container can't resolve `dash-sim` | You started CLI with `docker run` instead of `docker compose run` (no shared network). |
| Slow first build | First multi-stage build downloads every Go module. Use `docker build --build-arg GOPROXY=...` if you're behind a proxy. |
| Distroless container errors on shell-like commands | Distroless has no `/bin/sh`. You can only run the binary. Use `--entrypoint /usr/local/bin/<bin>` to add args. |

---

## Where to go next

> **Want more than one DPU?** Go to
> [09 — Multi-DPU test infra](09-multi-dpu-test-infra.md). It introduces
> a config-driven test-setup that spins up N independent DPUs in any of
> three topologies (native procs, single container, full compose fleet),
> and links to step-by-step hands-on walkthroughs for each.

- → [modules/](modules/) — deep dives into each binary's internals.
- → [docs/CLI_GUIDE.md](../CLI_GUIDE.md) — the canonical CLI reference.
