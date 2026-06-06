# Windows Developer Guide — DashCenter

> Copy-paste-ready build / run / test / clean recipes for Windows
> developers exploring DashCenter for the first time. Everything below has
> been validated on **Windows 10/11 + PowerShell 7** with Go 1.22, protoc
> 25, and no admin rights required.
>
> For the cross-platform tutorial, see
> [docs/tutorial/](../tutorial/). For the canonical CLI reference, see
> [docs/CLI_GUIDE.md](../CLI_GUIDE.md).

---

## Table of contents

1. [What you need installed](#1-what-you-need-installed)
2. [One-time PATH setup (per shell)](#2-one-time-path-setup-per-shell)
3. [Verify the toolchain](#3-verify-the-toolchain)
4. [Build all three binaries](#4-build-all-three-binaries)
5. [Run the simulator](#5-run-the-simulator)
6. [Drive it with the CLI](#6-drive-it-with-the-cli)
7. [Run the SONiC Redis adapter](#7-run-the-sonic-redis-adapter)
8. [Run tests](#8-run-tests)
9. [Regenerate proto stubs (only if proto changes)](#9-regenerate-proto-stubs)
10. [Clean recipes](#10-clean-recipes)
11. [One-liner for everything](#11-one-liner-for-everything)
12. [Troubleshooting](#12-troubleshooting)

---

## 1. What you need installed

| Tool | Version | Recommended install path |
|---|---|---|
| Go | 1.22+ | `%USERPROFILE%\go-sdk\go\` (portable zip; no admin) |
| protoc | 25.x | `%USERPROFILE%\protoc\` (portable zip; no admin) |
| protoc-gen-go | v1.34.x | `%USERPROFILE%\go\bin\` (installed by `go install`) |
| protoc-gen-go-grpc | v1.5.x | `%USERPROFILE%\go\bin\` (installed by `go install`) |
| Git | any modern | default installer location |
| PowerShell | **7+** | required to run `.ps1` codegen / install-check scripts |
| (optional) Docker Desktop | 24+ | only for the Docker Compose fleet |

Detailed install steps (download URLs, install commands, no-admin paths):
[docs/tutorial/03-build-setup.md](../tutorial/03-build-setup.md).

---

## 2. One-time PATH setup (per shell)

Open PowerShell 7 and prepend the toolchain to `PATH`:

```powershell
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"
$env:GOBIN="$env:USERPROFILE\go\bin"
```

> **Tip:** Add the three lines to your `$PROFILE` so every new shell has them
> automatically. Run `notepad $PROFILE` to edit it.

---

## 3. Verify the toolchain

```powershell
pwsh -NoProfile -File C:\WorkSpace\PS\PublicRepo\DashCenter\docs\tutorial\scripts\install-check.ps1
```

Expected tail:

```
[OK]   go                     go version go1.22.10 windows/amd64
[OK]   protoc                 libprotoc 25.3
[OK]   protoc-gen-go          protoc-gen-go.exe v1.34.2
[OK]   protoc-gen-go-grpc     protoc-gen-go-grpc 1.5.1
[OK]   git                    git version 2.x
[OK]   pwsh                   7.x
[OK]   PATH includes GOBIN    C:\Users\<you>\go\bin

=== All required checks passed ===
```

If anything fails, fix it before continuing — the build will not work.

---

## 4. Build all three binaries

Takes ~30 seconds on a fresh checkout, near-instant on subsequent builds.

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null

go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter

Get-ChildItem bin
```

Expected output:

```
Name                     Length    LastWriteTime
----                     ------    -------------
dash-redis-adapter.exe   ~23 MB   ...
dash-sim-client.exe      ~16 MB   ...
dash-sim.exe             ~17 MB   ...
```

All three are statically linked, single-file binaries — copy them anywhere
that has a TCP stack. No runtime dependency on the source tree.

---

## 5. Run the simulator

**Terminal A.** Start `dash-sim` and preload the reference scenario:

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
.\bin\dash-sim.exe `
    --grpc-listen :50051 `
    --admin-listen :8080 `
    --device-id dpu-sim-01 `
    --scenario .\dash-sim\testdata\scenarios\small.yaml
```

Expected stdout:

```
dash-sim: loaded scenario "...\small.yaml" (sizes=map[...])
dash-sim: admin HTTP listening on :8080
dash-sim: gRPC listening on :50051 (device=dpu-sim-01)
```

Leave it running. `Ctrl+C` for graceful shutdown.

> **Port issues?** Some Windows builds reserve ports in the 50050–50070
> range. If you see `bind: ... forbidden by its access permissions`, pick
> a free port like `:52051`.

---

## 6. Drive it with the CLI

**Terminal B.** Run a representative session against the simulator:

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
$c = ".\bin\dash-sim-client.exe"

# 6.1 Discovery
& $c kinds -o table              # list all 29 supported DASH object kinds
& $c ping                        # connectivity check

# 6.2 Create / list / get
& $c apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c list  --kind vnet -o table
& $c get   --kind vnet --key vnet-prod -o yaml

# 6.3 Update (re-apply with new value)
& $c apply --kind vnet --key vnet-prod --value '{"vni":1099,"version":"v2"}'

# 6.4 Counters
& $c counters --kind eni --key eni-001 -o table

# 6.5 SimulatePacket — run a packet through the DASH pipeline
& $c apply --kind vnet_mapping --key 'vnet-stage:10.1.0.10' `
           --value '{"underlay_ip":{"ipv4":167862884},"routing_type":"ROUTING_TYPE_VNET"}'
& $c simulate --direction outbound --eni eni-001 `
              --src-ip 10.0.0.1 --dst-ip 10.1.0.10 `
              --protocol 6 --src-port 1024 --dst-port 80 --trace

# 6.6 Subscribe (snapshot + live events). Ctrl+C to stop.
& $c subscribe --snapshot --kinds vnet,eni

# 6.7 Delete
& $c delete --kind vnet --key vnet-prod
```

All flags + every subcommand: [docs/CLI_GUIDE.md](../CLI_GUIDE.md).

---

## 7. Run the SONiC Redis adapter

**Terminal C (optional).** Start the Redis-backed backend with an
in-process miniredis (no external Redis required):

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
.\bin\dash-redis-adapter.exe --grpc-listen :52051 --embedded-redis
```

Expected stdout:

```
dash-redis-adapter: started embedded miniredis at 127.0.0.1:<random>
dash-redis-adapter: connected to Redis at 127.0.0.1:<random> (db=0)
dash-redis-adapter: gRPC listening on :52051
```

Drive it with **the same CLI binary** — just point `--target` at the
adapter:

```powershell
$c = ".\bin\dash-sim-client.exe"
& $c --target localhost:52051 ping
& $c --target localhost:52051 apply --kind vnet --key vnet-prod --value '{"vni":1001}'
& $c --target localhost:52051 list  --kind vnet -o table
```

> **Note:** `simulate` against the Redis adapter returns
> `code = Unimplemented` — Redis APP_DB has no behavioural pipeline. Use
> `dash-sim` for any packet-tracing work.

---

## 8. Run tests

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
go test .\dash-sim\... .\dash-redis-adapter\...
```

Expected:

```
ok  ...dash-sim/internal/sim/pipeline    1.9s     (8 conformance tests)
ok  ...dash-redis-adapter/internal/adapter 2.4s   (5 integration tests via miniredis)
```

For race-detector + coverage:

```powershell
go test -race -cover .\dash-sim\... .\dash-redis-adapter\...
```

---

## 9. Regenerate proto stubs

Only needed if you edit
[`proto/dashapi/v1/dashapi.proto`](../../proto/dashapi/v1/dashapi.proto)
or re-vendor a new `sonic-dash-api` snapshot.

```powershell
# (Optional) bump the upstream snapshot
pwsh -NoProfile -File C:\WorkSpace\PS\PublicRepo\DashCenter\scripts\vendor-protos.ps1

# Regenerate Go stubs (32 files under src/impl-go/gen/go/)
pwsh -NoProfile -File C:\WorkSpace\PS\PublicRepo\DashCenter\scripts\codegen-go.ps1
```

After regenerating, rebuild the binaries (§4) and run tests (§8).

---

## 10. Clean recipes

### 10.1 Remove built binaries

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
Remove-Item bin -Recurse -Force -ErrorAction SilentlyContinue
```

### 10.2 Clear Go build + test cache

```powershell
go clean -cache -testcache
```

### 10.3 Stop a running binary (if launched into the background)

```powershell
Get-Process dash-sim, dash-sim-client, dash-redis-adapter -ErrorAction SilentlyContinue |
    Stop-Process -Force
```

### 10.4 Full nuke (binaries + Go module download cache)

> Forces re-download of every dependency on next build.

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
Remove-Item bin -Recurse -Force -ErrorAction SilentlyContinue
go clean -cache -testcache -modcache
```

### 10.5 Wipe regenerated proto stubs

```powershell
Remove-Item C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\gen\go\dash    -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\gen\go\dashapi -Recurse -Force -ErrorAction SilentlyContinue
# then re-run §9 codegen.
```

### 10.6 Discard local git changes

> Destructive — only use if you're sure.

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter
git clean -fdx --dry-run           # preview what would be deleted
git clean -fdx                     # actually delete untracked + ignored files
git reset --hard                   # discard tracked-file edits
```

### 10.7 Docker fleet cleanup

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\deploy\compose
docker compose down                # stop + remove containers + network
docker compose down --volumes      # also drop redis data
docker compose build --no-cache    # force-rebuild images on next up
```

---

## 11. One-liner for everything

Sometimes you just want a fresh, tested build:

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go; `
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"; `
Remove-Item bin -Recurse -Force -ErrorAction SilentlyContinue; `
go clean -testcache; `
go build -o bin\dash-sim.exe           .\dash-sim\cmd\dash-sim; `
go build -o bin\dash-sim-client.exe    .\dash-sim-client\cmd\dash-sim-client; `
go build -o bin\dash-redis-adapter.exe .\dash-redis-adapter\cmd\dash-redis-adapter; `
go test  .\dash-sim\... .\dash-redis-adapter\...
```

If you see "ok" lines for both test packages and three `.exe` files in
`bin\`, you have a healthy build.

---

## 12. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `go: command not found` | PATH doesn't include the Go bin dir for this session | Re-run §2 PATH setup |
| `protoc-gen-go: program not found or is not executable` | `GOBIN` (`%USERPROFILE%\go\bin`) not on PATH | Re-run §2 PATH setup |
| `bind: ... forbidden by its access permissions` | Windows reserved port range | Use a port outside 50050–50070 (e.g. `:52051`) |
| `bind: address already in use` | Another process owns the port | `netstat -ano \| findstr :50051` and stop the offending PID |
| `connection refused` from the CLI | Server not started, or wrong `--target` | Check the `gRPC listening on ...` log line |
| `pattern ./...: directory prefix . does not contain modules listed in go.work` | Running `go build ./...` at the workspace root | Build each module explicitly, as in §4 |
| `decode value: unknown field "vnetId"` on apply | Used camelCase instead of upstream snake_case | Use `vnet_id`, `admin_state`, etc. |
| `code = Unimplemented` on `simulate` against `dash-redis-adapter` | By design — adapter has no pipeline | Use `dash-sim` for behavioural simulation |
| Subscribe shows no live events | Subscriber buffer (256) full | Check `/admin/health.dropped_events`; resubscribe with `--snapshot` |
| `winget install ... no applicable installer` | Some packages don't have a winget manifest | Use the manual portable-zip path in [03 — Build setup](../tutorial/03-build-setup.md) |

For anything not covered here, open an issue on the repo and reference
this guide.

---

## What to read next

- **Full CLI reference** (every subcommand, every flag, real outputs):
  [docs/CLI_GUIDE.md](../CLI_GUIDE.md).
- **Behavioural simulator internals**:
  [specs/LLD/dash-sim.md](../../specs/LLD/dash-sim.md).
- **SONiC adapter wire format**:
  [specs/LLD/dash-redis-adapter.md](../../specs/LLD/dash-redis-adapter.md).
- **Contributor roadmap** (30+ tagged starter tasks):
  [docs/roadmap.md](../roadmap.md).
