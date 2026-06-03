# dash-sim & dash-sim-client — Run + Test Guide

This guide covers running the DASH simulator (`dash-sim`) and driving it with
the operator CLI (`dash-sim-client`).

> Both binaries live in `src/impl-go/bin/` after a build. They are pure Go and
> have no runtime dependencies.

---

## 1. One-time toolchain setup (Windows)

If you are starting a fresh PowerShell session, prepend the toolchain to
`PATH`:

```powershell
$env:PATH="$env:USERPROFILE\go-sdk\go\bin;$env:USERPROFILE\go\bin;$env:USERPROFILE\protoc\bin;$env:PATH"
$env:GOPATH="$env:USERPROFILE\go"
$env:GOBIN="$env:USERPROFILE\go\bin"
```

Verify:

```powershell
go version       # go1.22.x
protoc --version # libprotoc 25.x
```

---

## 2. Build

From the Go workspace root:

```powershell
cd C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go
New-Item -ItemType Directory -Path bin -Force | Out-Null
go build -o bin\dash-sim.exe        .\dash-sim\cmd\dash-sim
go build -o bin\dash-sim-client.exe .\dash-sim-client\cmd\dash-sim-client
```

Outputs:

- `bin\dash-sim.exe`        — the simulator (gRPC + admin HTTP)
- `bin\dash-sim-client.exe` — the operator CLI

---

## 3. Run the simulator

```powershell
.\bin\dash-sim.exe `
  --grpc-listen  :50051 `
  --admin-listen :8080  `
  --device-id    dpu-sim-01
```

Flags:

| Flag              | Default       | Description                                |
| ----------------- | ------------- | ------------------------------------------ |
| `--grpc-listen`   | `:50051`      | gRPC service bind address                  |
| `--admin-listen` | `:8080`       | admin HTTP API bind address                |
| `--device-id`     | `dpu-sim-01`  | synthetic device id                        |
| `--scenario`      | (none)        | optional path to a YAML scenario to preload |
| `--tick-interval` | `1s`          | per-object counter tick interval           |

Press `Ctrl+C` for graceful shutdown.

### Preload a scenario at start

```powershell
.\bin\dash-sim.exe `
  --scenario .\dash-sim\testdata\scenarios\small.yaml
```

---

## 4. Drive it with the CLI

All commands accept `--target host:port` (defaults to `localhost:50051`) and
`-o json|yaml|table` (defaults to `json`).

### 4.1 Sanity check

```powershell
.\bin\dash-sim-client.exe ping
# -> ok: target=localhost:50051 vnets=0
```

### 4.2 VNETs

```powershell
.\bin\dash-sim-client.exe vnet create vnet-prod --vni 1001
.\bin\dash-sim-client.exe vnet create vnet-dev  --vni 1002 --label env=dev --label tier=frontend
.\bin\dash-sim-client.exe vnet list -o table
.\bin\dash-sim-client.exe vnet get  vnet-prod
.\bin\dash-sim-client.exe vnet delete vnet-dev
```

### 4.3 ENIs

```powershell
.\bin\dash-sim-client.exe eni create eni-001 `
  --vnet-id vnet-prod --mac 00:11:22:33:44:55 `
  --address 10.0.0.10 --address 10.0.0.11

.\bin\dash-sim-client.exe eni list -o table
.\bin\dash-sim-client.exe eni update eni-001 --vnet-id vnet-prod --admin-state down
.\bin\dash-sim-client.exe eni delete eni-001
```

### 4.4 ACL groups + rules

```powershell
.\bin\dash-sim-client.exe acl group add acl-prod-in --stage INBOUND
.\bin\dash-sim-client.exe acl rule  add `
  --group-id acl-prod-in --num 100 --action ALLOW `
  --src-prefix 0.0.0.0/0 --dst-prefix 10.0.0.0/24
.\bin\dash-sim-client.exe acl rule  list -o table
.\bin\dash-sim-client.exe acl rule  delete acl-prod-in/100
.\bin\dash-sim-client.exe acl group delete acl-prod-in    # cascades to its rules
```

### 4.5 Routes

```powershell
.\bin\dash-sim-client.exe route add `
  --table vnet-prod --dst-prefix 10.1.0.0/16 `
  --action FORWARD  --next-hop-ip 10.0.0.1
.\bin\dash-sim-client.exe route list -o table
.\bin\dash-sim-client.exe route delete vnet-prod/10.1.0.0/16
```

### 4.6 VNET mappings

```powershell
.\bin\dash-sim-client.exe mapping add `
  --vnet-id vnet-prod --overlay-ip 10.0.0.20 `
  --underlay-ip 100.64.0.20 --mac 00:aa:bb:cc:dd:ee --vni 1001
.\bin\dash-sim-client.exe mapping list -o table
.\bin\dash-sim-client.exe mapping delete vnet-prod/10.0.0.20
```

### 4.7 Counters

```powershell
.\bin\dash-sim-client.exe counters get eni-001 -o table
```

### 4.8 Subscribe (snapshot + live updates)

```powershell
# Stream all kinds, current state first:
.\bin\dash-sim-client.exe subscribe --snapshot

# Filter by kind:
.\bin\dash-sim-client.exe subscribe --snapshot --kinds vnet,eni

# Then, from another shell, mutate and watch the events arrive live:
.\bin\dash-sim-client.exe vnet create vnet-live --vni 9999
```

Press `Ctrl+C` to end the stream.

---

## 5. Admin HTTP API (`--admin-listen`)

Quick `curl` / `Invoke-RestMethod` examples:

```powershell
# Health
Invoke-RestMethod http://localhost:8080/admin/health | ConvertTo-Json -Depth 5

# Full state dump
Invoke-RestMethod http://localhost:8080/admin/dump | ConvertTo-Json -Depth 6

# Reset state
Invoke-RestMethod -Method Post http://localhost:8080/admin/reset

# Load a scenario file (path is server-side)
$body = @{
  path  = "C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\dash-sim\testdata\scenarios\small.yaml"
  reset = $true
} | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/scenario -Body $body -ContentType application/json

# Inject a one-shot failure on the next CreateVnet call
$body = @{op="CreateVnet"; mode="error"; count=1; message="injected"} | ConvertTo-Json
Invoke-RestMethod -Method Post http://localhost:8080/admin/faults -Body $body -ContentType application/json

# List / clear faults
Invoke-RestMethod http://localhost:8080/admin/faults
Invoke-RestMethod -Method Delete http://localhost:8080/admin/faults
```

Fault modes:

| `mode`  | Behavior                                            | Required fields           |
| ------- | --------------------------------------------------- | ------------------------- |
| `error` | Return `Ack{accepted:false, error:<message>}`       | `op`, `message` optional  |
| `drop`  | Alias of `error` with default message `"dropped"`   | `op`                      |
| `delay` | Sleep `delay_ms` then continue normally             | `op`, `delay_ms`          |

`op="*"` matches every RPC. `count<=0` means infinite, `count=0` defaults to
`1` (one-shot).

---

## 6. Full smoke test (copy-paste)

Terminal A — start the sim:

```powershell
C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\bin\dash-sim.exe
```

Terminal B — exercise it:

```powershell
$c = "C:\WorkSpace\PS\PublicRepo\DashCenter\src\impl-go\bin\dash-sim-client.exe"

& $c ping
& $c vnet create vnet-prod --vni 1001
& $c eni  create eni-001 --vnet-id vnet-prod --mac 00:11:22:33:44:55 --address 10.0.0.10
& $c acl  group add acl-in --stage INBOUND
& $c acl  rule  add  --group-id acl-in --num 100 --action ALLOW --src-prefix 0.0.0.0/0 --dst-prefix 10.0.0.0/24
& $c route add  --table vnet-prod --dst-prefix 10.1.0.0/16 --action FORWARD --next-hop-ip 10.0.0.1
& $c mapping add --vnet-id vnet-prod --overlay-ip 10.0.0.20 --underlay-ip 100.64.0.20 --mac 00:aa:bb:cc:dd:ee --vni 1001
& $c vnet list    -o table
& $c eni  list    -o table
& $c acl  rule list -o table
& $c route list   -o table
& $c mapping list -o table
& $c counters get eni-001 -o table
& $c subscribe --snapshot --kinds vnet,eni
```

Press `Ctrl+C` on the subscribe command, then `Ctrl+C` on the sim.

---

## 7. Troubleshooting

| Symptom                                              | Fix                                                                    |
| ---------------------------------------------------- | ---------------------------------------------------------------------- |
| `go: ... not recognized`                             | Re-run the `$env:PATH=` block in §1.                                   |
| `connection refused` on the client                    | sim isn't running on `--target`; check `Get-NetTCPConnection -LocalPort 50051`. |
| Subscribe shows no live events                        | Subscriber buffer full (256). Check `/admin/health` `dropped_events`.   |
| Scenario load fails with "vnet \"X\" does not exist" | YAML order matters; ENIs/mappings reference VNETs and must come after. |
