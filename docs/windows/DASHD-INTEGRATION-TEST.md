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
