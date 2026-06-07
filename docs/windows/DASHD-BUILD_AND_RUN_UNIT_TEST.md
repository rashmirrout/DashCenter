# Windows Developer Guide — dashd (Build, Unit Test & Run)

> Copy-paste-ready recipes for building, testing, and running the **dashd**
> daemon on **Windows 10/11 + PowerShell 7** with Go 1.22+.
>
> For the dash-sim / dash-sim-client / dash-redis-adapter build guide, see
> [DASH-SIM-BUILD_AND_RUN.md](DASH-SIM-BUILD_AND_RUN.md).

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [One-time PATH setup (per shell)](#2-one-time-path-setup-per-shell)
3. [Verify Go installation](#3-verify-go-installation)
4. [Resolve dependencies](#4-resolve-dependencies)
5. [Build dashd](#5-build-dashd)
6. [Run unit tests](#6-run-unit-tests)
7. [Run individual package tests](#7-run-individual-package-tests)
8. [Run dashd](#8-run-dashd)
9. [Interact with dashd (REST API)](#9-interact-with-dashd-rest-api)
10. [Admin & health endpoints](#10-admin--health-endpoints)
11. [Configuration](#11-configuration)
12. [Clean recipes](#12-clean-recipes)
13. [One-liner: build + test](#13-one-liner-build--test)
14. [Troubleshooting](#14-troubleshooting)
15. [Architecture overview](#15-architecture-overview)

---

## 1. Prerequisites

| Tool | Version | Where |
|------|---------|-------|
| Go | 1.22+ | `%USERPROFILE%\go-sdk\go\` (portable zip; no admin) |
| Git | any modern | default installer location |
| PowerShell | **7+** | `C:\Program Files\PowerShell\7\pwsh.exe` |

> **Note:** dashd does not require `protoc`, Docker, or any external
> database for Phase 1. It uses a file-backed store only.

---

## 2. One-time PATH setup (per shell)

Open **PowerShell 7** and set the Go toolchain path:

```powershell
# Adjust the first path to wherever your Go SDK is installed
$env:PATH   = "$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:PATH"
$env:GOPATH = "$env:USERPROFILE\go"
```

> **Tip:** Add these lines to your `$PROFILE` so every new shell has them
> automatically. Run `notepad $PROFILE` to edit it.

---

## 3. Verify Go installation

```powershell
go version
```

Expected output:

```
go version go1.22.10 windows/amd64
```

If you see `go: The term 'go' is not recognized`, re-check the PATH
setup in §2.

---

## 4. Resolve dependencies

Run this once after cloning or whenever `go.mod` changes:

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
go mod tidy
```

This downloads all required Go modules and generates `go.sum`. Takes
~10–30 seconds on first run depending on network speed.

---

## 5. Build dashd

### 5.1 Build all packages (compile check)

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
go build ./...
```

If no output is printed, the build succeeded. Any compilation error
will be printed with file/line references.

### 5.2 Build the dashd binary

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
go build -o dashd.exe ./cmd/dashd
```

This produces a single `dashd.exe` binary in the current directory.

### 5.3 Verify the binary

```powershell
.\dashd.exe --version
```

Expected:

```
dashd 0.1.0-phase1
```

---

## 6. Run unit tests

### 6.1 Run all tests

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
go test -count=1 ./...
```

Expected output (all packages should show `ok`):

```
?   .../dashd/cmd/dashd             [no test files]
?   .../dashd/internal/store         [no test files]
ok  .../dashd/internal/config        1.9s
ok  .../dashd/internal/dispatch      2.9s
ok  .../dashd/internal/inventory     3.1s
ok  .../dashd/internal/model         1.5s
ok  .../dashd/internal/placement     2.4s
ok  .../dashd/internal/reconciler    3.3s
ok  .../dashd/internal/server/admin  2.7s
ok  .../dashd/internal/server/rest   2.5s
ok  .../dashd/internal/store/file    1.7s
ok  .../dashd/internal/subscribe     1.5s
```

### 6.2 Run tests with verbose output

```powershell
go test -count=1 -v ./...
```

This prints every individual test name and PASS/FAIL status.

### 6.3 Run tests with race detector (requires CGO)

> **Note:** The `-race` flag requires CGO, which requires a C compiler
> (e.g., GCC via MinGW or MSYS2). If CGO is not available, skip this step.

```powershell
$env:CGO_ENABLED = "1"
go test -race -count=1 ./...
```

### 6.4 Run tests with coverage

```powershell
go test -count=1 -cover ./...
```

This prints per-package coverage percentages alongside pass/fail status.

### 6.5 Generate HTML coverage report

```powershell
go test -count=1 -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
# Open in browser:
Start-Process coverage.html
```

---

## 7. Run individual package tests

Each package can be tested independently. Useful for debugging a specific
module:

| Package | Command | Test Count |
|---------|---------|-----------|
| Config | `go test -v ./internal/config/` | 9 |
| File Store | `go test -v ./internal/store/file/` | 18 |
| Inventory | `go test -v ./internal/inventory/` | 24 |
| Model (ObsCache) | `go test -v ./internal/model/` | 14 |
| Placement | `go test -v ./internal/placement/` | 26 |
| Subscribe | `go test -v ./internal/subscribe/` | 6 |
| Dispatch | `go test -v ./internal/dispatch/` | 8 |
| Reconciler | `go test -v ./internal/reconciler/` | 6 |
| REST Server | `go test -v ./internal/server/rest/` | 6 |
| Admin Server | `go test -v ./internal/server/admin/` | 7 |

### Run a single test by name

```powershell
go test -v -run TestPutGetVnet ./internal/server/rest/
```

---

## 8. Run dashd

### 8.1 Run with default configuration

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
.\dashd.exe --config configs\dashd.example.yaml
```

Expected output:

```
level=INFO msg="dashd starting" version=0.1.0-phase1
level=INFO msg="rest: listening" addr=:8443
level=INFO msg="admin: listening" addr=:7443
level=INFO msg="dashd ready" rest=:8443 admin=:7443
```

### 8.2 Run with custom state directory

Create a config YAML or override via flags. Example minimal config:

```yaml
# my-dashd.yaml
listen:
  rest_addr: ":8443"
  admin_addr: ":7443"
storage:
  backend: file
  file:
    state_dir: C:\temp\dashd-state
inventory:
  source: api
log:
  level: debug
  format: text
```

```powershell
.\dashd.exe --config my-dashd.yaml
```

### 8.3 Graceful shutdown

Press **Ctrl+C** in the terminal. dashd will shut down in order:

1. Stop REST server (drain in-flight requests)
2. Stop Admin server
3. Stop all subscribe pumps
4. Stop dispatch manager (flush pending syncs)
5. Close the store
6. Log `dashd stopped`

---

## 9. Interact with dashd (REST API)

While dashd is running (§8), open a **second terminal** and use `curl`
or PowerShell `Invoke-RestMethod`:

### 9.1 Create a VNET

```powershell
Invoke-RestMethod -Method PUT -Uri http://localhost:8443/v1/vnets/vnet-prod `
  -ContentType "application/json" `
  -Body '{"vni":1001,"guid":"abc-123"}'
```

Response:

```json
{"accepted":true,"generation":1}
```

### 9.2 Get a VNET

```powershell
Invoke-RestMethod -Uri http://localhost:8443/v1/vnets/vnet-prod
```

### 9.3 List all VNETs

```powershell
Invoke-RestMethod -Uri http://localhost:8443/v1/vnets
```

### 9.4 Create an ENI

```powershell
Invoke-RestMethod -Method PUT -Uri http://localhost:8443/v1/enis/eni-001 `
  -ContentType "application/json" `
  -Body '{"vnet_name":"vnet-prod","mac_address":"00:11:22:33:44:55","underlay_ip":"10.0.0.1","admin_state":"enabled"}'
```

### 9.5 Delete a resource

```powershell
Invoke-RestMethod -Method DELETE -Uri http://localhost:8443/v1/vnets/vnet-prod
```

Returns HTTP 204 (No Content) on success.

### 9.6 Register DPU inventory via API

```powershell
Invoke-RestMethod -Method PUT -Uri http://localhost:8443/v1/inventory `
  -ContentType "application/json" `
  -Body '{"dpus":[{"id":"dpu-01","endpoint":"10.0.1.1:50051"},{"id":"dpu-02","endpoint":"10.0.1.2:50051"}]}'
```

### 9.7 Get inventory

```powershell
Invoke-RestMethod -Uri http://localhost:8443/v1/inventory
```

### 9.8 Trigger reconciliation

```powershell
Invoke-RestMethod -Method POST -Uri http://localhost:8443/v1/reconcile
```

### 9.9 Using curl instead of PowerShell

```powershell
curl -X PUT http://localhost:8443/v1/vnets/vnet-prod -H "Content-Type: application/json" -d "{\"vni\":1001}"
curl http://localhost:8443/v1/vnets/vnet-prod
curl http://localhost:8443/v1/vnets
curl -X DELETE http://localhost:8443/v1/vnets/vnet-prod
curl -X POST http://localhost:8443/v1/reconcile
```

---

## 10. Admin & health endpoints

The admin server runs on a separate port (default `:7443`):

### 10.1 Health check

```powershell
Invoke-RestMethod -Uri http://localhost:7443/admin/health
```

Returns JSON with DPU states, store status, and overall health.

### 10.2 Inventory snapshot

```powershell
Invoke-RestMethod -Uri http://localhost:7443/admin/inventory
```

### 10.3 Desired state dump

```powershell
Invoke-RestMethod -Uri http://localhost:7443/admin/desired?kind=vnet
```

### 10.4 Observed state dump

```powershell
Invoke-RestMethod -Uri http://localhost:7443/admin/observed?dpu=dpu-01
```

### 10.5 Drift report

```powershell
Invoke-RestMethod -Uri http://localhost:7443/admin/drift
```

### 10.6 Force reconcile

```powershell
Invoke-RestMethod -Method POST -Uri http://localhost:7443/admin/reconcile
```

---

## 11. Configuration

dashd uses a YAML configuration file. All fields have sensible defaults.

### Full configuration reference

```yaml
listen:
  grpc_addr: ":9443"     # gRPC listen address (Phase 2)
  rest_addr: ":8443"     # REST HTTP listen address
  admin_addr: ":7443"    # Admin HTTP listen address

storage:
  backend: file           # only "file" in Phase 1
  file:
    state_dir: /var/lib/dashd   # where specs are persisted as JSON

inventory:
  source: api             # "api" (via REST) or "file" (static YAML)
  file: ""                # path to inventory YAML (when source=file)

reconcile:
  tick_interval: 30s      # periodic reconciliation interval
  per_dpu_inbox_size: 1   # coalescing inbox size per DPU worker
  apply_rate_limit: 100   # max apply ops/sec across all DPUs
  error_budget_per_min: 10 # errors before backing off

log:
  level: info             # debug | info | warn | error
  format: json            # json | text
```

### Inventory file format (when `source: file`)

```yaml
# configs/inventory.example.yaml
dpus:
  - id: dpu-01
    endpoint: "10.0.1.1:50051"
    labels:
      rack: rack-01
      region: us-west-2
  - id: dpu-02
    endpoint: "10.0.1.2:50051"
    labels:
      rack: rack-02
      region: us-west-2
```

---

## 12. Clean recipes

### 12.1 Remove built binary

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
Remove-Item dashd.exe -Force -ErrorAction SilentlyContinue
```

### 12.2 Clear Go build + test cache

```powershell
go clean -cache -testcache
```

### 12.3 Remove state directory (wipe persisted specs)

```powershell
Remove-Item C:\temp\dashd-state -Recurse -Force -ErrorAction SilentlyContinue
```

### 12.4 Full nuke (binary + caches + module downloads)

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
Remove-Item dashd.exe -Force -ErrorAction SilentlyContinue
go clean -cache -testcache -modcache
```

### 12.5 Stop a running dashd

```powershell
Get-Process dashd -ErrorAction SilentlyContinue | Stop-Process -Force
```

---

## 13. One-liner: build + test

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd; `
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:PATH"; `
go mod tidy; `
go build ./...; `
go test -count=1 ./...; `
go build -o dashd.exe ./cmd/dashd; `
Write-Host "Build & test complete. Binary: dashd.exe"
```

If all test packages show `ok` and `dashd.exe` is produced, you have a
healthy build.

---

## 14. Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `go: command not found` | Go not on PATH | Re-run §2 PATH setup |
| `go mod tidy` downloads nothing | Already resolved | Normal — means `go.sum` is up to date |
| `go build` prints nothing | Build succeeded | Expected — Go only prints errors |
| `-race requires cgo` | CGO not available | Skip `-race` or install MinGW/MSYS2 GCC |
| `config: validate: inventory.file is required` | Config has `source: file` but no path | Set `source: api` or provide `file:` path |
| `bind: address already in use` | Another process on same port | Change `rest_addr` / `admin_addr` in config |
| `store: not found` on GET/DELETE | Resource doesn't exist | PUT it first, or check the name/kind |
| `store: generation mismatch` on PUT | Optimistic concurrency conflict | Retry with current generation from GET |
| Test timeout | Slow I/O on first run | Run again — Go caches compiled test binaries |

---

## 15. Architecture overview

```
┌─────────────────────────────────────────────────┐
│                    dashd                        │
│                                                 │
│  ┌──────────┐  ┌──────────┐  ┌──────────────┐  │
│  │  REST    │  │  Admin   │  │  gRPC (P2)   │  │
│  │  :8443   │  │  :7443   │  │  :9443       │  │
│  └────┬─────┘  └────┬─────┘  └──────────────┘  │
│       │              │                           │
│  ┌────▼──────────────▼──────────────────────┐   │
│  │         DesiredStore (FileStore)          │   │
│  │    <state_dir>/<ns>/<kind>/<name>.json    │   │
│  └────┬─────────────────────────────────────┘   │
│       │                                          │
│  ┌────▼─────┐  ┌────────────┐  ┌────────────┐  │
│  │Reconciler│─▶│ Placement  │─▶│ Dispatch   │  │
│  │  (tick)  │  │ (translate)│  │ (per-DPU)  │  │
│  └──────────┘  └────────────┘  └─────┬──────┘  │
│                                       │          │
│  ┌────────────┐  ┌────────────┐  ┌───▼───────┐ │
│  │ Inventory  │  │ Subscribe  │  │  Workers  │ │
│  │ (DPU reg)  │  │ (pumps)    │  │(dashapi)  │ │
│  └────────────┘  └────────────┘  └───────────┘ │
│                                                  │
│  ┌──────────────────────────────────────────┐   │
│  │      ObsCache (observed state)           │   │
│  └──────────────────────────────────────────┘   │
└─────────────────────────────────────────────────┘
```

### Package summary (10 test packages)

| Package | Responsibility | Tests |
|---------|---------------|-------|
| `config` | YAML config loader with defaults + validation | 9 |
| `store/file` | File-backed desired-state persistence | 18 |
| `inventory` | DPU registry + health prober | 24 |
| `model` | ObsCache (observed state) + diff | 14 |
| `placement` | Translate northbound→southbound + tier ordering | 26 |
| `subscribe` | Per-DPU gNMI subscribe pumps (stub) | 6 |
| `dispatch` | Per-DPU worker goroutines + rate limiting | 8 |
| `reconciler` | Select loop: store watch + dirty + tick | 6 |
| `server/rest` | REST HTTP gateway (PUT/GET/DELETE/LIST) | 6 |
| `server/admin` | Admin HTTP (health, inventory, drift, reconcile) | 7 |

---

## What to read next

- **Implementation plan:** [specs/Impl-Plan/impl-plan-basic.md](../../specs/Impl-Plan/impl-plan-basic.md)
- **Implementation tracker:** [specs/Impl-Plan/impl-phases.md](../../specs/Impl-Plan/impl-phases.md)
- **High-level design:** [specs/HLD/dashd-hld.md](../../specs/HLD/dashd-hld.md)
- **Low-level design:** [specs/LLD/dashd-lld.md](../../specs/LLD/dashd-lld.md)
- **dash-sim build guide:** [DASH-SIM-BUILD_AND_RUN.md](DASH-SIM-BUILD_AND_RUN.md)