# dashd Phase 1B — Manual Integration Testing Guide

## Prerequisites

- Go 1.22+ installed at `C:\Users\rashmirout\go-sdk\go\bin\go.exe`
- dash-sim source at `src/impl-go/dash-sim/`
- dashd source at `src/impl-go/dashd/`

---

## 1. Single dash-sim Instance

### Terminal 1: Start dash-sim
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dash-sim
# Listens on :50051
```

### Terminal 2: Start dashd
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --config configs/dashd.example.yaml
# REST :8443, gRPC :9443, Admin :7443
```

### Terminal 3: Exercise the API

```powershell
# 1. Check health
curl http://localhost:7443/admin/health

# 2. Register DPU
curl -X PUT http://localhost:8443/v1/inventory -d "{\"dpus\":[{\"id\":\"dpu-0\",\"endpoint\":\"localhost:50051\"}]}"

# 3. Create VNet
curl -X PUT http://localhost:8443/v1/vnets/vnet-1 -d "{\"name\":\"vnet-1\",\"vni\":100}"

# 4. Read VNet back
curl http://localhost:8443/v1/vnets/vnet-1

# 5. Create ENI
curl -X PUT http://localhost:8443/v1/enis/eni-1 -d "{\"name\":\"eni-1\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:01\",\"underlay_ip\":\"10.1.1.1\"}"

# 6. Read ENI back
curl http://localhost:8443/v1/enis/eni-1

# 7. List VNets
curl http://localhost:8443/v1/vnets

# 8. List ENIs
curl http://localhost:8443/v1/enis

# 9. Check drift (should converge to empty)
curl "http://localhost:7443/admin/drift?dpu=dpu-0"

# 10. Create ACL Policy (namespace-scoped)
curl -X PUT http://localhost:8443/v1/default/acl-policies/pol-1 -d "{\"name\":\"pol-1\",\"stage\":\"inbound\",\"eni_names\":[\"eni-1\"]}"

# 11. Create Route Policy
curl -X PUT http://localhost:8443/v1/default/route-policies/rp-1 -d "{\"name\":\"rp-1\",\"eni_names\":[\"eni-1\"]}"

# 12. Force reconcile
curl -X POST http://localhost:8443/v1/reconcile

# 13. Delete ENI
curl -X DELETE http://localhost:8443/v1/enis/eni-1

# 14. Verify deletion
curl http://localhost:8443/v1/enis/eni-1
# Should return 404
```

---

## 2. Two dash-sim Instances

### Terminal 1: dash-sim on port 50051
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dash-sim
```

### Terminal 2: dash-sim on port 50052
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dash-sim --port 50052
```

### Terminal 3: dashd
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --config configs/dashd.example.yaml
```

### Terminal 4: Exercise multi-DPU
```powershell
# Register both DPUs
curl -X PUT http://localhost:8443/v1/inventory -d "{\"dpus\":[{\"id\":\"dpu-0\",\"endpoint\":\"localhost:50051\"},{\"id\":\"dpu-1\",\"endpoint\":\"localhost:50052\"}]}"

# Check inventory
curl http://localhost:7443/admin/inventory

# Create VNet (replicated to both DPUs)
curl -X PUT http://localhost:8443/v1/vnets/vnet-1 -d "{\"name\":\"vnet-1\",\"vni\":100}"

# Create ENI on dpu-0
curl -X PUT http://localhost:8443/v1/enis/eni-1 -d "{\"name\":\"eni-1\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:01\",\"underlay_ip\":\"10.1.1.1\"}"

# Create ENI on dpu-1 (via placement hint)
curl -X PUT http://localhost:8443/v1/enis/eni-2 -d "{\"name\":\"eni-2\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:02\",\"underlay_ip\":\"10.1.1.2\"}"

# Check drift on each DPU
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
curl "http://localhost:7443/admin/drift?dpu=dpu-1"

# Health check (both DPUs should be UP)
curl http://localhost:7443/admin/health
```

---

## 3. Dry-Run Mode

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --dry-run --config configs/dashd.example.yaml
# Should print config status, inventory, spec counts, then exit 0
```

---

## 4. Docker Variant (2 dash-sim Containers)

If you have Docker available:

```powershell
# Build dash-sim image
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim
docker build -t dash-sim:dev .

# Run two instances
docker run -d -p 50051:50051 --name sim0 dash-sim:dev
docker run -d -p 50052:50051 --name sim1 dash-sim:dev

# Run dashd on host
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --config configs/dashd.example.yaml

# Register DPUs (use host.docker.internal or localhost)
curl -X PUT http://localhost:8443/v1/inventory -d "{\"dpus\":[{\"id\":\"dpu-0\",\"endpoint\":\"localhost:50051\"},{\"id\":\"dpu-1\",\"endpoint\":\"localhost:50052\"}]}"

# Cleanup
docker rm -f sim0 sim1
```

---

## 5. Build & Test Commands

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd

# Build
C:\Users\rashmirout\go-sdk\go\bin\go.exe build ./...

# Unit tests
C:\Users\rashmirout\go-sdk\go\bin\go.exe test -count=1 ./...

# Vet
C:\Users\rashmirout\go-sdk\go\bin\go.exe vet ./...

# Coverage report
C:\Users\rashmirout\go-sdk\go\bin\go.exe test -coverprofile=coverage.out ./...
C:\Users\rashmirout\go-sdk\go\bin\go.exe tool cover -func=coverage.out

# Placement benchmarks (CPU/allocs across small/medium/large fleets)
C:\Users\rashmirout\go-sdk\go\bin\go.exe test -bench=. -benchmem ./internal/placement/...
```

---

## 6. Automated Integration Suite

The `test/integration/` package contains a Go-test harness that spins up
its own `dashd` + `dash-sim` pair per scenario on dynamically chosen
ports, registers the DPU, runs an assertion, and tears everything down.
Per-test stdout/stderr is captured to a log file under `t.TempDir()` so
failures are easy to triage.

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd

# Run the full integration suite (builds dashd + dash-sim on demand)
C:\Users\rashmirout\go-sdk\go\bin\go.exe test -tags=integration -v -timeout 5m ./test/integration/...

# Run a single scenario for focused debugging
C:\Users\rashmirout\go-sdk\go\bin\go.exe test -tags=integration -v -run TestIntegration_PutEni_Converges_REST -timeout 2m ./test/integration/...

# Vet the integration package without running it
C:\Users\rashmirout\go-sdk\go\bin\go.exe vet -tags=integration ./test/integration/...
```

### Scenarios shipped (all REST today; gRPC harness is plug-in ready)

| # | Test | What it proves |
|---|------|----------------|
| 1 | `TestIntegration_DaemonStartsClean` | dashd + dash-sim come up; first reconcile converges with empty store |
| 2 | `TestIntegration_PutVnet_Converges_REST` | PUT vnet via REST → drift goes to zero → GET round-trips |
| 3 | `TestIntegration_PutEni_Converges_REST` | PUT eni → `/admin/eni-placement` reports `observed:true` once subscribe pump catches the CREATED event |
| 4 | `TestIntegration_EditEni_Reconverges` | Mutating an ENI MAC triggers an UPDATE Apply and the system converges again |
| 5 | `TestIntegration_DeleteEni_Reconciles` | DELETE eni → subsequent GET returns 404 and dispatch issues Delete on the sim |
| 6 | `TestIntegration_RestartPersistsState` | Kill dashd → restart with the same state dir → previously-applied specs survive |
| 7 | `TestIntegration_ForceReconcile_OK` | `POST /admin/reconcile` returns 200 |
| 8 | `TestIntegration_DriftEnvelope_Shape` | `/admin/drift` returns a JSON envelope with `items` and `summary` |
| 9 | `TestIntegration_EniPlacement_EmptyStore` | `/admin/eni-placement` returns `count:0` on an empty store |

### Developer-friendly tips for manual exploration

- Every scenario writes its harness log to `<TestTempDir>/{dashd,dash-sim}.log`.
  When a scenario fails, the test logger prints the path; `cat` (or `Get-Content`) those files for the full child-process output.
- Need to repro a failure by hand? Each scenario uses fresh random ports; the harness prints them at the top of `dashd.log`. Use the same `--config` and `go run` command from the log to re-launch identical processes.
- Want to keep dashd running after a scenario? Wrap the offending test in `t.Skip(...)` and use the standalone "Section 1" commands above with a fixed port set you control.

---

## 7. Phase 1B — What Was Shipped

Phase 1B replaced three stub subsystems with real production code and added a live southbound path. Treat this as the "tour map" for everything that follows.

### 7.1 Subsystems added or rewritten

| Package | Before Phase 1B | After Phase 1B |
|---------|------------------|----------------|
| `internal/dpuclient` | (did not exist) | `DpuClient` interface (`Apply` / `Delete` / `Subscribe`) + real gRPC implementation + `MockClient` for tests (≥ 98 % coverage) |
| `internal/subscribe/pump` | Empty loop, never called dash-sim | Real `dashapi.Subscribe` stream; snapshot-first ObsCache reset; exponential reconnect backoff (1 s → 30 s cap) |
| `internal/dispatch/worker` | Logged `TODO` only | Real `reconcilePass`: `placement.LoadDesiredSpecs` → resolve placement → diff vs observed → `Apply` / `Delete` per DPU via cached client |
| `internal/placement/load` | (did not exist) | Shared `LoadDesiredSpecs(ctx, store)` helper + 5 micro-benchmarks (`internal/placement/bench_test.go`) |
| `internal/server/admin` | `/admin/drift` returned `[]` stub | Live `/admin/drift?dpu=<id>` (real declared-vs-observed delta) + brand-new `/admin/eni-placement` endpoint with `observed:true/false` flag |

### 7.2 Bug fixes shipped with Phase 1B

- gRPC `HandlerType` must be an interface pointer (not the concrete struct) — fixed in `internal/server/grpc/control_plane.go` and `internal/server/grpc/observability.go`. Without this, registering services panicked at runtime.
- `go vet` "copies lock value" on proto `Event` struct copy in `internal/dpuclient/mock.go` — switched to field-by-field copy.

### 7.3 Admin endpoints reference (port `:7443`)

| Endpoint | Status in Phase 1B | What it returns |
|----------|---------------------|------------------|
| `GET  /admin/health` | unchanged | DPU health roll-up |
| `GET  /admin/inventory` | unchanged | Registered DPUs |
| `POST /admin/reconcile` | unchanged | Forces a reconcile pass; returns 200 |
| `GET  /admin/desired?kind=<kind>` | unchanged | Desired specs by kind |
| `GET  /admin/observed?dpu=<id>` | unchanged | Observed state snapshot for a DPU |
| `GET  /admin/drift?dpu=<id>` | **upgraded — now live** | Real declared-vs-observed delta. `items:[]` means fully converged. Omit `?dpu=` for fleet-wide view. |
| `GET  /admin/eni-placement?eni=<name>` | **new** | ENI → DPU assignment with `observed:true/false`. Omit `?eni=` to list all. |

---

## 8. Phase 1B Manual Integration Test (Setup → Deploy → Test → Explore)

A guided tour through every Phase 1B capability using three terminals. Allow ~10 minutes end-to-end.

### 8.1 Setup

- Go 1.22+ at `C:\Users\rashmirout\go-sdk\go\bin\go.exe`
- Working tree at `C:\WorkSpace\PS\PublicRepo\DashCenter`
- Config file: `src/impl-go/dashd/configs/dashd.example.yaml` (default state dir survives restarts)

**Port map (after dashd starts)**

| Port | Surface |
|------|---------|
| 8443 | REST API |
| 9443 | gRPC control plane |
| 7443 | Admin (drift / placement / health / reconcile) |
| 50051 | dash-sim gRPC |

### 8.2 Deploy

**Terminal 1 — dash-sim**
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dash-sim
# Expect: "dash-sim listening :50051"
```

**Terminal 2 — dashd**
```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --config configs/dashd.example.yaml
# Expect: "rest listening :8443", "grpc listening :9443", "admin listening :7443"
```

Leave both running. Use **Terminal 3** for everything below.

### 8.3 Test sequence (Phase 1B happy path)

Each step has an "expect" comment so you can spot regressions immediately.

```powershell
# Step 1 - health
curl http://localhost:7443/admin/health
# Expect: {"status":"ok",...}

# Step 2 - register the DPU (without this nothing reconciles)
curl -X PUT http://localhost:8443/v1/inventory -d "{\"dpus\":[{\"id\":\"dpu-0\",\"endpoint\":\"localhost:50051\"}]}"
# Expect: 200; subscribe pump opens stream to dash-sim within ~1s

# Step 3 - create a VNet (first thing reconcile will Apply on dash-sim)
curl -X PUT http://localhost:8443/v1/vnets/vnet-1 -d "{\"name\":\"vnet-1\",\"vni\":100}"
# Expect: 200

# Step 4 - drift goes briefly non-empty, then converges
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
# Expect (within ~1s): {"items":[],"summary":{...}}  -- empty items == fully converged

# Step 5 - create an ENI
curl -X PUT http://localhost:8443/v1/enis/eni-1 -d "{\"name\":\"eni-1\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:01\",\"underlay_ip\":\"10.1.1.1\"}"
# Expect: 200

# Step 6 - watch placement.observed flip true once subscribe pump confirms CREATED
curl "http://localhost:7443/admin/eni-placement?eni=eni-1"
# Expect (within ~1s): {"items":[{"eni":"eni-1","dpu":"dpu-0","observed":true}],"count":1}

# Step 7 - drift is empty again
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
# Expect: {"items":[],...}

# Step 8 - mutate the ENI (change MAC) -> triggers an UPDATE Apply
curl -X PUT http://localhost:8443/v1/enis/eni-1 -d "{\"name\":\"eni-1\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:99\",\"underlay_ip\":\"10.1.1.1\"}"
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
# Expect: items[] briefly contains the update, then empty after reconverge

# Step 9 - delete the ENI -> triggers Delete RPC
curl -X DELETE http://localhost:8443/v1/enis/eni-1
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
# Expect: {"items":[],...}
curl "http://localhost:7443/admin/eni-placement"
# Expect: {"items":[],"count":0}
```

### 8.4 Explore — introspection endpoints

```powershell
# What dashd thinks should exist
curl "http://localhost:7443/admin/desired?kind=vnet"
curl "http://localhost:7443/admin/desired?kind=eni"
curl "http://localhost:7443/admin/desired?kind=acl_policy"

# What each DPU actually reported (via the Subscribe stream)
curl "http://localhost:7443/admin/observed?dpu=dpu-0"

# Inventory
curl http://localhost:7443/admin/inventory

# Force a reconcile (useful after editing state files out-of-band)
curl -X POST http://localhost:7443/admin/reconcile

# Fleet-wide drift (no ?dpu=)
curl http://localhost:7443/admin/drift

# All placements
curl http://localhost:7443/admin/eni-placement
```

### 8.5 Fault injection — reconnect / backoff

Proves the new exponential backoff loop and snapshot-first cache reset.

1. With dash-sim and dashd both running and an ENI applied, **Ctrl+C the dash-sim terminal**.
2. In the dashd log you should see repeated `subscribe reconnect attempt` messages with delays growing 1 s → 2 s → 4 s → 8 s → 16 s → 30 s (cap).
3. During this window, `/admin/drift?dpu=dpu-0` may show stale items (last-known observed). That is expected.
4. Restart dash-sim (`go run ./cmd/dash-sim` again in Terminal 1).
5. dashd reconnects, the pump clears ObsCache, replays the SNAPSHOT, dispatch reconciles, and:
   ```powershell
   curl "http://localhost:7443/admin/drift?dpu=dpu-0"
   # Expect: {"items":[],...} within a few seconds
   curl "http://localhost:7443/admin/eni-placement?eni=eni-1"
   # Expect: observed:true again
   ```

This is the most revealing single experiment in Phase 1B — it exercises pump reconnect, snapshot replay, ObsCache reset, dispatch diff, Apply RPC, and the live drift endpoint, end-to-end.

### 8.6 Restart persistence

Proves desired state survives a dashd restart.

```powershell
# Put some specs (steps 3-6 above), then:
# Ctrl+C dashd in Terminal 2

# Restart it
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --config configs/dashd.example.yaml

# Specs are still there
curl http://localhost:8443/v1/vnets
curl http://localhost:8443/v1/enis

# Drift auto-reconverges to empty
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
```

---

## 9. Phase 1B Manual Integration Test with Docker Sim

Same flow as Section 8 but with two dash-sim containers, so per-DPU drift and per-DPU eni-placement views become meaningful.

### 9.1 Build & launch

```powershell
# Build dash-sim image once
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim
docker build -t dash-sim:phase1b .

# Two sim containers on different host ports
docker run -d --name sim0 -p 50051:50051 dash-sim:phase1b
docker run -d --name sim1 -p 50052:50051 dash-sim:phase1b

# Confirm both are up
docker ps --filter name=sim
docker logs sim0 --tail 5
docker logs sim1 --tail 5

# dashd on host
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dashd
C:\Users\rashmirout\go-sdk\go\bin\go.exe run ./cmd/dashd --config configs/dashd.example.yaml
```

### 9.2 Register both DPUs

```powershell
curl -X PUT http://localhost:8443/v1/inventory -d "{\"dpus\":[{\"id\":\"dpu-0\",\"endpoint\":\"localhost:50051\"},{\"id\":\"dpu-1\",\"endpoint\":\"localhost:50052\"}]}"

curl http://localhost:7443/admin/inventory
# Expect: both dpu-0 and dpu-1 listed
```

### 9.3 Drive specs and watch per-DPU drift

```powershell
# Shared VNet (gets replicated to both DPUs by reconcile)
curl -X PUT http://localhost:8443/v1/vnets/vnet-1 -d "{\"name\":\"vnet-1\",\"vni\":100}"

# ENI #1 - placement will put this on one of the DPUs
curl -X PUT http://localhost:8443/v1/enis/eni-1 -d "{\"name\":\"eni-1\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:01\",\"underlay_ip\":\"10.1.1.1\"}"

# ENI #2
curl -X PUT http://localhost:8443/v1/enis/eni-2 -d "{\"name\":\"eni-2\",\"vnet_name\":\"vnet-1\",\"mac_address\":\"aa:bb:cc:dd:ee:02\",\"underlay_ip\":\"10.1.1.2\"}"

# Per-DPU drift - observe convergence on each side
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
curl "http://localhost:7443/admin/drift?dpu=dpu-1"

# Fleet-wide drift
curl http://localhost:7443/admin/drift

# Placement view - which DPU got which ENI, plus observed flag
curl http://localhost:7443/admin/eni-placement
# Expect: items[] lists eni-1 and eni-2 with their assigned dpu and observed:true
```

### 9.4 Fault injection in Docker

```powershell
# Kill one sim
docker stop sim0

# dashd logs should show backoff for dpu-0; dpu-1 keeps working
docker logs sim0 --tail 5
curl "http://localhost:7443/admin/drift?dpu=dpu-1"
# Expect: still {"items":[],...}

# Bring sim0 back
docker start sim0

# dpu-0 reconverges
curl "http://localhost:7443/admin/drift?dpu=dpu-0"
# Expect: {"items":[],...} within a few seconds
```

### 9.5 Cleanup

```powershell
docker rm -f sim0 sim1

# Optional: nuke the state dir to start from scratch next run
# Remove-Item -Recurse -Force <state_dir from configs/dashd.example.yaml>
```

### 9.6 Common Docker debugging commands

```powershell
docker logs -f sim0                # live tail
docker exec sim0 ss -ltn           # confirm sim is listening
docker inspect sim0 --format '{{.NetworkSettings.IPAddress}}'
docker network ls
```

---

## Phase 1B Gate Verification Checklist

| Gate | Command | Expected |
|------|---------|----------|
| P1B-G1 Build | `go build ./...` | Zero errors |
| P1B-G2 Vet | `go vet ./...` | Zero warnings |
| P1B-G4 Coverage | `go test -cover ./...` | New packages ≥ 90% |
| P1B-G5 Service layer | Both REST+gRPC use `service.NewControlPlane` | Verified in main.go |
| P1B-G6 gRPC server | gRPC listens on :9443 | Verified via log output |
| P1B-G7 REST parity | All 7 spec kinds have PUT routes | Verified in router() |
| P1B-G8 Dry-run | `go run ./cmd/dashd --dry-run` | Exits 0 |
| P1B-G9 Integration | `go test -tags=integration ./test/integration/...` | All scenarios pass |
| P1B-G11 Southbound wiring | Subscribe pump + dispatch worker call Apply/Delete | Verified via `/admin/eni-placement?eni=…` observed flag |
| P1B-G12 DpuClient | `go test -cover ./internal/dpuclient/...` | ≥ 98% |
| P1B-G14 Restart | `TestIntegration_RestartPersistsState` | Passes |
| P1B-G15 Drift live | `/admin/drift` returns real items, not `[]` stub | Passes after `PutVnet` |
| P1B-G16 Bench | `go test -bench=. ./internal/placement/...` | Establishes baseline |
