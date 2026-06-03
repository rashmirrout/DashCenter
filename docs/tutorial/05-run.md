# 05 — Run

How to start the binaries on Windows and Linux, end-to-end. Two backends are
available: **`dash-sim`** (behavioural simulator, in-memory) and
**`dash-redis-adapter`** (SONiC-compatible, Redis-backed).

> **Prerequisite**: [04 — Build](04-build.md) — binaries exist under
> `src/impl-go/bin/`.

---

## 1. Set the working directory

```powershell
# Windows
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
```

```bash
# Linux
cd ~/work/DashCenter/src/impl-go
```

Throughout this page, `bin\dash-sim.exe` / `bin/dash-sim` etc. refer to
binaries from this directory.

---

## 2. Run `dash-sim` (behavioural simulator)

### 2.1 Defaults

```powershell
.\bin\dash-sim.exe
```
```bash
./bin/dash-sim
```

Sample stdout:
```
2026/06/04 03:10:00 dash-sim: admin HTTP listening on :8080
2026/06/04 03:10:00 dash-sim: gRPC listening on :50051 (device=dpu-sim-01)
```

`Ctrl+C` for graceful shutdown.

### 2.2 All flags

| Flag | Default | Meaning |
|---|---|---|
| `--grpc-listen` | `:50051` | DashApi gRPC bind address. |
| `--admin-listen` | `:8080` | Admin HTTP bind address. |
| `--device-id` | `dpu-sim-01` | Synthetic identifier reported on `/admin/health`. |
| `--scenario` | (none) | Optional path to a YAML scenario to preload (relative to working dir or absolute). |
| `--tick-interval` | `1s` | Counter advancement period. |

### 2.3 Preload a scenario

```powershell
.\bin\dash-sim.exe --scenario .\dash-sim\testdata\scenarios\small.yaml
```
```bash
./bin/dash-sim --scenario ./dash-sim/testdata/scenarios/small.yaml
```

Sample stdout:
```
2026/06/04 03:10:00 dash-sim: loaded scenario ".../small.yaml" (sizes=map[acl_group:1 acl_in:1 acl_rule:1 appliance:1 eni:2 eni_route:1 route:1 route_group:1 vnet:2 vnet_mapping:1])
2026/06/04 03:10:00 dash-sim: admin HTTP listening on :8080
2026/06/04 03:10:00 dash-sim: gRPC listening on :50051 (device=dpu-sim-01)
```

### 2.4 Run on a different port

```powershell
.\bin\dash-sim.exe --grpc-listen :50052 --admin-listen :8081
```

> Some Windows builds reserve ports in the 50050–50070 range. If you see
> `bind: ... forbidden by its access permissions`, pick a free port like
> `:52051`.

### 2.5 Run in the background (Linux)

```bash
nohup ./bin/dash-sim --grpc-listen :50051 > dash-sim.log 2>&1 &
echo $!         # PID for later kill
```

### 2.6 Run as a Windows background process

```powershell
$proc = Start-Process -FilePath .\bin\dash-sim.exe `
    -ArgumentList '--grpc-listen',':50051','--admin-listen',':8080' `
    -RedirectStandardOutput dash-sim.log -RedirectStandardError dash-sim.err `
    -PassThru -NoNewWindow
$proc.Id   # remember the PID
```

To stop:
```powershell
Stop-Process -Id $proc.Id
```

---

## 3. Run `dash-redis-adapter` (SONiC APP_DB backend)

### 3.1 Self-contained (embedded miniredis)

```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --embedded-redis
```
```bash
./bin/dash-redis-adapter --grpc-listen :52051 --embedded-redis
```

Sample stdout:
```
2026/06/04 03:11:00 dash-redis-adapter: started embedded miniredis at 127.0.0.1:55580
2026/06/04 03:11:00 dash-redis-adapter: connected to Redis at 127.0.0.1:55580 (db=0)
2026/06/04 03:11:00 dash-redis-adapter: gRPC listening on :52051
```

### 3.2 Against a real Redis

```powershell
# Windows — start a real Redis with Docker
docker run --rm -d --name redis -p 6379:6379 redis:7-alpine
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --redis localhost:6379
```
```bash
# Linux
sudo systemctl start redis           # or: docker run -d -p 6379:6379 redis:7-alpine
./bin/dash-redis-adapter --grpc-listen :52051 --redis localhost:6379
```

Sample stdout:
```
2026/06/04 03:12:00 dash-redis-adapter: connected to Redis at localhost:6379 (db=0)
2026/06/04 03:12:00 dash-redis-adapter: gRPC listening on :52051
```

### 3.3 All flags

| Flag | Default | Meaning |
|---|---|---|
| `--grpc-listen` | `:52051` | DashApi gRPC bind address. |
| `--redis` | `localhost:6379` | Redis address. |
| `--redis-db` | `0` | Redis logical DB. |
| `--redis-password` | (empty) | Optional Redis password. |
| `--embedded-redis` | `false` | If set, start an in-process miniredis and ignore `--redis`. |

---

## 4. Drive either backend with the CLI

The same `dash-sim-client` binary works against both. Just point `--target`
at the gRPC port.

```powershell
.\bin\dash-sim-client.exe --target localhost:50051 ping     # against dash-sim
.\bin\dash-sim-client.exe --target localhost:52051 ping     # against adapter
```

See [07 — dash-sim-client](07-dash-sim-client.md) for the full command
reference.

---

## 5. Two backends side by side

Real-world workflow: run **both** backends and use the CLI to send the same
config to each. Then verify the simulator's behavioural responses
(`simulate`) and the adapter's APP_DB layout (`redis-cli HGETALL ...`)
match expectations.

Terminal 1 — start the sim:
```powershell
.\bin\dash-sim.exe --grpc-listen :50051 --admin-listen :8080
```

Terminal 2 — start the adapter:
```powershell
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --embedded-redis
```

Terminal 3 — drive both:
```powershell
$c = ".\bin\dash-sim-client.exe"
& $c --target localhost:50051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:52051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:50051 list  --kind vnet -o table
& $c --target localhost:52051 list  --kind vnet -o table
```

Both lists should show the same single row.

---

## 6. Shutdown

| Backend | Windows | Linux |
|---|---|---|
| dash-sim | `Ctrl+C` (foreground) or `Stop-Process` | `Ctrl+C` or `kill <pid>` |
| dash-redis-adapter | same | same |
| Embedded miniredis | dies with the adapter | dies with the adapter |
| External Redis (docker) | `docker rm -f redis` | `docker rm -f redis` |

Both binaries do a graceful gRPC stop on `SIGINT`/`SIGTERM`.

---

## 7. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `bind: ... forbidden by its access permissions` (Windows) | port reserved by `netsh int ipv4 show excludedportrange protocol=tcp` | Pick another port (e.g. `:52051`, `:50071`). |
| `bind: address already in use` | another process owns the port | `netstat -ano \| findstr :50051` (Windows) / `ss -tlnp \| grep 50051` (Linux). |
| `connection refused` from CLI | server not started OR wrong `--target` | Confirm the server's "gRPC listening on ..." log line. |
| `redis ping ... connection refused` | adapter started before Redis | Start Redis first, or use `--embedded-redis`. |
| Subscribe shows no live events | subscriber buffer (256) full | Re-subscribe with `--snapshot=false` to skip backlog; check `/admin/health` `dropped_events`. |

---

## Where to go next

- → [06 — Test](06-test.md) — verify everything programmatically.
- → [07 — CLI](07-dash-sim-client.md) — exercise the running services.
