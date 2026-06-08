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
```

---

## Phase 1B Gate Verification Checklist

| Gate | Command | Expected |
|------|---------|----------|
| P1B-G1 Build | `go build ./...` | Zero errors |
| P1B-G2 Vet | `go vet ./...` | Zero warnings |
| P1B-G5 Service layer | Both REST+gRPC use `service.NewControlPlane` | Verified in main.go |
| P1B-G6 gRPC server | gRPC listens on :9443 | Verified via log output |
| P1B-G7 REST parity | All 7 spec kinds have PUT routes | Verified in router() |
| P1B-G8 Dry-run | `go run ./cmd/dashd --dry-run` | Exits 0 |
| P1B-G11 REST E2E | PutVnet via REST → convergence | Manual test above |